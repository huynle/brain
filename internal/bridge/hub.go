package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// Sentinel errors returned by hub operations.
var (
	ErrRunnerNotConnected = errors.New("runner bridge not connected")
	ErrPathNotAllowed     = errors.New("path not on bridge allowlist")
	ErrTooManyInFlight    = errors.New("too many in-flight bridge requests")
	ErrBridgeTimeout      = errors.New("bridge request timed out")
)

// Hub holds one bridge connection per runner and multiplexes proxied
// requests and event streams over it. Events are fanned out to browser SSE
// subscribers via the realtime hub on topic instance:{runnerID}:{instanceID}.
type Hub struct {
	rt *realtime.Hub

	mu    sync.Mutex
	conns map[string]*runnerConn
}

// NewHub creates a bridge hub that publishes instance events to rt.
func NewHub(rt *realtime.Hub) *Hub {
	return &Hub{
		rt:    rt,
		conns: make(map[string]*runnerConn),
	}
}

// instanceLive caches always-on control state per instance so pending
// permissions and busy/idle status are visible without a browser attached.
type instanceLive struct {
	status      string
	pendingPerm map[string]json.RawMessage // permission id → raw event
}

type runnerConn struct {
	runnerID string
	ws       *websocket.Conn
	hub      *Hub

	writeMu sync.Mutex

	mu      sync.Mutex
	pending map[string]chan Frame
	nextID  uint64
	streams map[string]int // instanceID → subscriber refcount
	live    map[string]*instanceLive
	closed  bool
}

// ServeBridge upgrades the request to a WebSocket and serves the runner's
// bridge connection until it drops. Implements api.BridgeService.
func (h *Hub) ServeBridge(w http.ResponseWriter, r *http.Request, runnerID string) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Runners are not browsers; origin checks don't apply.
		InsecureSkipVerify: true,
	})
	if err != nil {
		slog.Warn("bridge: websocket accept failed", "runner_id", runnerID, "error", err)
		return
	}
	ws.SetReadLimit(MaxFrameBytes)

	conn := &runnerConn{
		runnerID: runnerID,
		ws:       ws,
		hub:      h,
		pending:  make(map[string]chan Frame),
		streams:  make(map[string]int),
		live:     make(map[string]*instanceLive),
	}

	h.register(conn)
	defer h.unregister(conn)

	slog.Info("bridge: runner connected", "runner_id", runnerID)
	conn.readLoop(r.Context())
	slog.Info("bridge: runner disconnected", "runner_id", runnerID)
}

func (h *Hub) register(conn *runnerConn) {
	h.mu.Lock()
	old := h.conns[conn.runnerID]
	h.conns[conn.runnerID] = conn
	h.mu.Unlock()
	if old != nil {
		old.close("replaced by new connection")
	}
}

func (h *Hub) unregister(conn *runnerConn) {
	h.mu.Lock()
	if h.conns[conn.runnerID] == conn {
		delete(h.conns, conn.runnerID)
	}
	h.mu.Unlock()
	conn.close("connection closed")
}

func (h *Hub) conn(runnerID string) *runnerConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conns[runnerID]
}

// Connected reports whether a runner currently has a live bridge connection.
func (h *Hub) Connected(runnerID string) bool {
	return h.conn(runnerID) != nil
}

// Do proxies one HTTP request to an instance on a connected runner and
// returns the upstream status and body.
func (h *Hub) Do(ctx context.Context, runnerID, instanceID, method, path string, body []byte) (int, []byte, error) {
	if !AllowedRequest(method, path) {
		return 0, nil, ErrPathNotAllowed
	}
	if len(body) > MaxBodyBytes {
		return 0, nil, fmt.Errorf("request body exceeds %d bytes", MaxBodyBytes)
	}
	conn := h.conn(runnerID)
	if conn == nil {
		return 0, nil, ErrRunnerNotConnected
	}
	res, err := conn.roundTrip(ctx, Frame{
		Type:       FrameReq,
		InstanceID: instanceID,
		Method:     method,
		Path:       path,
		Body:       body,
		TimeoutMs:  DefaultTimeoutMs,
	})
	if err != nil {
		return 0, nil, err
	}
	if res.Error != "" {
		return res.Status, nil, errors.New(res.Error)
	}
	return res.Status, res.Body, nil
}

// SpawnInstance asks a runner to spawn a fresh ad-hoc OpenCode instance.
func (h *Hub) SpawnInstance(ctx context.Context, runnerID string, spec types.SpawnInstanceSpec) (*types.OpencodeInstance, error) {
	conn := h.conn(runnerID)
	if conn == nil {
		return nil, ErrRunnerNotConnected
	}
	res, err := conn.roundTrip(ctx, Frame{
		Type:      FrameSpawn,
		Spec:      &spec,
		TimeoutMs: 60_000, // spawn includes port discovery, slower than a proxy call
	})
	if err != nil {
		return nil, err
	}
	if res.Error != "" {
		return nil, errors.New(res.Error)
	}
	var inst types.OpencodeInstance
	if err := json.Unmarshal(res.Body, &inst); err != nil {
		return nil, fmt.Errorf("decode spawn response: %w", err)
	}
	return &inst, nil
}

// KillInstance asks a runner to terminate an ad-hoc instance.
func (h *Hub) KillInstance(ctx context.Context, runnerID, instanceID string) error {
	conn := h.conn(runnerID)
	if conn == nil {
		return ErrRunnerNotConnected
	}
	res, err := conn.roundTrip(ctx, Frame{
		Type:       FrameKill,
		InstanceID: instanceID,
		TimeoutMs:  DefaultTimeoutMs,
	})
	if err != nil {
		return err
	}
	if res.Error != "" {
		return errors.New(res.Error)
	}
	return nil
}

// AcquireStream enables full event forwarding for an instance (refcounted).
// The returned release function must be called when the subscriber detaches;
// the last release closes the upstream full stream. Control events keep
// flowing regardless of stream state.
func (h *Hub) AcquireStream(runnerID, instanceID string) (func(), error) {
	conn := h.conn(runnerID)
	if conn == nil {
		return nil, ErrRunnerNotConnected
	}

	conn.mu.Lock()
	conn.streams[instanceID]++
	first := conn.streams[instanceID] == 1
	conn.mu.Unlock()

	if first {
		conn.send(Frame{Type: FrameStreamOpen, ID: "s:" + instanceID, InstanceID: instanceID})
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			conn.mu.Lock()
			conn.streams[instanceID]--
			last := conn.streams[instanceID] <= 0
			if last {
				delete(conn.streams, instanceID)
			}
			closed := conn.closed
			conn.mu.Unlock()
			if last && !closed {
				conn.send(Frame{Type: FrameStreamClose, ID: "s:" + instanceID, InstanceID: instanceID})
			}
		})
	}
	return release, nil
}

// PendingPermissions returns the raw permission.* events currently awaiting
// a response on an instance, from the always-on control event cache.
func (h *Hub) PendingPermissions(runnerID, instanceID string) []json.RawMessage {
	conn := h.conn(runnerID)
	if conn == nil {
		return nil
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	live := conn.live[instanceID]
	if live == nil {
		return nil
	}
	out := make([]json.RawMessage, 0, len(live.pendingPerm))
	for _, raw := range live.pendingPerm {
		out = append(out, raw)
	}
	return out
}

// DecorateInstances merges live bridge state into instance records.
// Implements api.BridgeService.
func (h *Hub) DecorateInstances(instances []types.OpencodeInstance) {
	for i := range instances {
		inst := &instances[i]
		conn := h.conn(inst.RunnerID)
		if conn == nil {
			continue
		}
		inst.BridgeConnected = true
		conn.mu.Lock()
		if live := conn.live[inst.InstanceID]; live != nil {
			inst.PendingPermissions = len(live.pendingPerm)
			if live.status != "" && inst.Status != types.InstanceStatusExited {
				inst.Status = live.status
			}
		}
		conn.mu.Unlock()
	}
}

// ---------------------------------------------------------------------------
// Connection internals
// ---------------------------------------------------------------------------

// roundTrip sends a correlated frame and waits for its res frame.
func (c *runnerConn) roundTrip(ctx context.Context, f Frame) (Frame, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return Frame{}, ErrRunnerNotConnected
	}
	if len(c.pending) >= MaxInFlight {
		c.mu.Unlock()
		return Frame{}, ErrTooManyInFlight
	}
	c.nextID++
	f.ID = "r-" + strconv.FormatUint(c.nextID, 10)
	ch := make(chan Frame, 1)
	c.pending[f.ID] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, f.ID)
		c.mu.Unlock()
	}()

	if err := c.send(f); err != nil {
		return Frame{}, err
	}

	timeout := time.Duration(f.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = time.Duration(DefaultTimeoutMs) * time.Millisecond
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return Frame{}, ctx.Err()
	case <-timer.C:
		return Frame{}, ErrBridgeTimeout
	case res, ok := <-ch:
		if !ok {
			return Frame{}, ErrRunnerNotConnected
		}
		return res, nil
	}
}

// send writes a frame to the websocket (single writer at a time).
func (c *runnerConn) send(f Frame) error {
	b, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("marshal frame: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.ws.Write(ctx, websocket.MessageText, b)
}

// close fails all pending round-trips and closes the socket.
func (c *runnerConn) close(reason string) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
	c.ws.Close(websocket.StatusNormalClosure, reason)
}

// readLoop processes inbound frames until the connection drops.
func (c *runnerConn) readLoop(ctx context.Context) {
	// Server-side keepalive pings.
	pingCtx, cancelPing := context.WithCancel(ctx)
	defer cancelPing()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-pingCtx.Done():
				return
			case <-ticker.C:
				pctx, cancel := context.WithTimeout(pingCtx, 10*time.Second)
				err := c.ws.Ping(pctx)
				cancel()
				if err != nil {
					return
				}
			}
		}
	}()

	for {
		_, data, err := c.ws.Read(ctx)
		if err != nil {
			return
		}
		var f Frame
		if err := json.Unmarshal(data, &f); err != nil {
			slog.Warn("bridge: bad frame from runner", "runner_id", c.runnerID, "error", err)
			continue
		}
		c.handleFrame(f)
	}
}

func (c *runnerConn) handleFrame(f Frame) {
	switch f.Type {
	case FrameHello:
		slog.Info("bridge: hello",
			"runner_id", c.runnerID, "proto", f.Proto, "instances", len(f.Instances))
		// Re-arm full streams that browsers still hold open across a
		// runner reconnect.
		c.mu.Lock()
		open := make([]string, 0, len(c.streams))
		for instanceID, refs := range c.streams {
			if refs > 0 {
				open = append(open, instanceID)
			}
		}
		c.mu.Unlock()
		for _, instanceID := range open {
			c.send(Frame{Type: FrameStreamOpen, ID: "s:" + instanceID, InstanceID: instanceID})
		}

	case FrameRes:
		c.mu.Lock()
		ch := c.pending[f.ID]
		c.mu.Unlock()
		if ch != nil {
			select {
			case ch <- f:
			default:
			}
		}

	case FrameInstanceEvent:
		c.trackControlEvent(f.InstanceID, f.Event)
		c.publishEvent(f.InstanceID, "instance_event", f.Event)

	case FrameStreamEvent:
		c.publishEvent(f.InstanceID, "instance_event", f.Event)

	case FrameStreamClosed:
		c.publishEvent(f.InstanceID, "stream_closed", json.RawMessage(
			fmt.Sprintf(`{"reason":%q}`, f.Reason)))

	default:
		slog.Debug("bridge: unhandled frame type", "type", f.Type, "runner_id", c.runnerID)
	}
}

func (c *runnerConn) publishEvent(instanceID, event string, data json.RawMessage) {
	if c.hub.rt == nil || instanceID == "" {
		return
	}
	c.hub.rt.Publish(realtime.InstanceTopic(c.runnerID, instanceID), event, data)
}

// opencodeEvent is the minimal shape needed to classify control events.
type opencodeEvent struct {
	Type       string `json:"type"`
	Properties struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Info   struct {
			ID string `json:"id"`
		} `json:"info"`
	} `json:"properties"`
}

// trackControlEvent updates the per-instance live cache (pending permissions
// and last known busy/idle status) from always-on control events.
func (c *runnerConn) trackControlEvent(instanceID string, raw json.RawMessage) {
	if instanceID == "" || len(raw) == 0 {
		return
	}
	var evt opencodeEvent
	if err := json.Unmarshal(raw, &evt); err != nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	live := c.live[instanceID]
	if live == nil {
		live = &instanceLive{pendingPerm: make(map[string]json.RawMessage)}
		c.live[instanceID] = live
	}

	permID := evt.Properties.ID
	if permID == "" {
		permID = evt.Properties.Info.ID
	}

	switch {
	case evt.Type == "permission.updated" || evt.Type == "permission.asked":
		if permID != "" {
			live.pendingPerm[permID] = raw
		}
	case evt.Type == "permission.replied":
		if permID != "" {
			delete(live.pendingPerm, permID)
		}
	case evt.Type == "session.idle":
		live.status = types.InstanceStatusIdle
	case evt.Type == "session.error":
		live.status = types.InstanceStatusIdle
	case evt.Type == "session.status" && evt.Properties.Status != "":
		live.status = evt.Properties.Status
	case evt.Type == "session.updated" || evt.Type == "message.updated":
		live.status = types.InstanceStatusBusy
	}
}
