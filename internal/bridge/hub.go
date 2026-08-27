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
	execs map[string]*execState // runnerID|execID → in-flight shell command
}

// NewHub creates a bridge hub that publishes instance events to rt.
func NewHub(rt *realtime.Hub) *Hub {
	return &Hub{
		rt:    rt,
		conns: make(map[string]*runnerConn),
		execs: make(map[string]*execState),
	}
}

// execState is the hub's record of one runner-shell command, kept alongside
// the lossy realtime fan-out so the streaming handler has a second, reliable
// source for the two things it cannot afford to miss: that the command
// finished, and that output was lost on the way to the browser.
//
// Created by StartExec, dropped by ReleaseExec (the handler's defer), so the
// map holds at most one entry per in-flight command.
type execState struct {
	runnerID      string
	execID        string
	conn          *runnerConn // owner; a reconnect installs a different conn
	done          bool
	exitCode      int
	errMsg        string
	droppedChunks int
	droppedBytes  int
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

// FetchHistory asks a runner for the full transcript of a session by ID,
// even when no live instance hosts it: the runner serves it from a live
// instance if one has the session, otherwise reads it from OpenCode's on-disk
// storage. Returns the raw JSON body (an array of {info, parts}).
func (h *Hub) FetchHistory(ctx context.Context, runnerID, sessionID string) ([]byte, error) {
	conn := h.conn(runnerID)
	if conn == nil {
		return nil, ErrRunnerNotConnected
	}
	res, err := conn.roundTrip(ctx, Frame{
		Type:      FrameHistory,
		SessionID: sessionID,
		TimeoutMs: DefaultTimeoutMs,
	})
	if err != nil {
		return nil, err
	}
	if res.Error != "" {
		return nil, errors.New(res.Error)
	}
	return res.Body, nil
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

// AbortTask asks a runner to terminate a task-owned instance and reset the task to pending.
func (h *Hub) AbortTask(ctx context.Context, runnerID, taskID string) error {
	conn := h.conn(runnerID)
	if conn == nil {
		return ErrRunnerNotConnected
	}
	res, err := conn.roundTrip(ctx, Frame{
		Type:      FrameAbortTask,
		TaskID:    taskID,
		TimeoutMs: DefaultTimeoutMs,
	})
	if err != nil {
		return err
	}
	if res.Error != "" {
		return errors.New(res.Error)
	}
	return nil
}

// StartExec asks a runner to start a shell command and stream its output.
// It returns once the runner has spawned the process (or failed to); output
// then arrives asynchronously on realtime topic exec:{runnerID}:{execID} as
// "exec_data" events, terminated by exactly one "exec_exit". Callers must
// subscribe to that topic BEFORE calling this, or they will miss output.
func (h *Hub) StartExec(ctx context.Context, runnerID, execID, command, workdir string, timeoutMs int) error {
	if execID == "" {
		return errors.New("exec id required")
	}
	if command == "" {
		return errors.New("command required")
	}
	if len(command) > ExecMaxCommandBytes {
		return fmt.Errorf("command too large (max %d bytes)", ExecMaxCommandBytes)
	}
	conn := h.conn(runnerID)
	if conn == nil {
		return ErrRunnerNotConnected
	}
	if timeoutMs <= 0 {
		timeoutMs = ExecDefaultTimeoutMs
	}
	if timeoutMs > ExecMaxTimeoutMs {
		timeoutMs = ExecMaxTimeoutMs
	}
	// Register before the frame goes out: output frames can come back before
	// roundTrip returns, and they must find a state to account against.
	h.registerExec(conn, runnerID, execID)
	res, err := conn.roundTrip(ctx, Frame{
		Type:    FrameExecStart,
		ExecID:  execID,
		Command: command,
		Workdir: workdir,
		// The spawn ack is fast; the command's own budget rides in its own
		// field so the runner enforces it independently of how long we
		// wait for that ack.
		TimeoutMs:     DefaultTimeoutMs,
		ExecTimeoutMs: timeoutMs,
	})
	if err != nil {
		h.ReleaseExec(runnerID, execID)
		return err
	}
	if res.Error != "" {
		h.ReleaseExec(runnerID, execID)
		return errors.New(res.Error)
	}
	return nil
}

// execKey namespaces an exec id by its runner.
func execKey(runnerID, execID string) string { return runnerID + "|" + execID }

// registerExec starts tracking a command. An exec id is server-minted and
// single-use, so an existing entry is stale state from a refused duplicate
// and is replaced.
func (h *Hub) registerExec(conn *runnerConn, runnerID, execID string) {
	h.mu.Lock()
	h.execs[execKey(runnerID, execID)] = &execState{
		runnerID: runnerID, execID: execID, conn: conn,
	}
	h.mu.Unlock()
}

// ExecOutcome returns the hub's record of a command, and whether it is still
// tracked. Outcome.Done reports whether the command has ended; the Dropped
// counters are live and may grow while it runs.
func (h *Hub) ExecOutcome(runnerID, execID string) (types.ExecOutcome, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.execs[execKey(runnerID, execID)]
	if st == nil {
		return types.ExecOutcome{}, false
	}
	return types.ExecOutcome{
		Done:          st.done,
		ExitCode:      st.exitCode,
		Error:         st.errMsg,
		DroppedChunks: st.droppedChunks,
		DroppedBytes:  st.droppedBytes,
	}, true
}

// ReleaseExec drops a command's record. Called by the streaming handler once
// it has finished with the command, so the map never grows per command.
func (h *Hub) ReleaseExec(runnerID, execID string) {
	h.mu.Lock()
	delete(h.execs, execKey(runnerID, execID))
	h.mu.Unlock()
}

// recordExecDrop notes output that the fan-out could not deliver.
func (h *Hub) recordExecDrop(runnerID, execID string, bytes int) {
	h.mu.Lock()
	if st := h.execs[execKey(runnerID, execID)]; st != nil {
		st.droppedChunks++
		st.droppedBytes += bytes
	}
	h.mu.Unlock()
}

// finishExec records how a command ended. The first outcome wins: a runner
// that reports an exit and then drops must not have its exit overwritten by
// the disconnect.
func (h *Hub) finishExec(runnerID, execID string, exitCode int, errMsg string) {
	h.mu.Lock()
	if st := h.execs[execKey(runnerID, execID)]; st != nil && !st.done {
		st.done = true
		st.exitCode = exitCode
		st.errMsg = errMsg
	}
	h.mu.Unlock()
}

// failExecsForConn ends every command still running on a dropped connection.
// Without this a SIGKILLed runner leaves its exec streams waiting for an
// exec_exit that can never arrive.
func (h *Hub) failExecsForConn(conn *runnerConn, reason string) {
	var ended []*execState

	h.mu.Lock()
	for _, st := range h.execs {
		if st.conn != conn || st.done {
			continue
		}
		st.done = true
		st.exitCode = types.ExecExitUnknown
		st.errMsg = reason
		ended = append(ended, st)
	}
	h.mu.Unlock()

	// Publish outside the lock: a live handler unblocks immediately instead
	// of waiting for its next poll.
	for _, st := range ended {
		h.publishExecExit(st.runnerID, st.execID, types.ExecExitUnknown, reason)
	}
}

// publishExecExit fans a command's terminal event out to its stream topic.
func (h *Hub) publishExecExit(runnerID, execID string, exitCode int, errMsg string) {
	if h.rt == nil {
		return
	}
	h.rt.Publish(realtime.ExecTopic(runnerID, execID), "exec_exit", map[string]any{
		"exec_id":   execID,
		"exit_code": exitCode,
		"error":     errMsg,
	})
}

// SignalExec delivers a signal to a running exec ("int", "term" or "kill").
func (h *Hub) SignalExec(ctx context.Context, runnerID, execID, signal string) error {
	conn := h.conn(runnerID)
	if conn == nil {
		return ErrRunnerNotConnected
	}
	res, err := conn.roundTrip(ctx, Frame{
		Type:      FrameExecSignal,
		ExecID:    execID,
		Signal:    signal,
		TimeoutMs: DefaultTimeoutMs,
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
		// A send failure surfaces as the bridge connection dropping, which the
		// read loop already handles; there is no recovery at this call site.
		_ = conn.send(Frame{Type: FrameStreamOpen, ID: "s:" + instanceID, InstanceID: instanceID})
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
				// A send failure surfaces as the bridge connection dropping, which the
				// read loop already handles; there is no recovery at this call site.
				_ = conn.send(Frame{Type: FrameStreamClose, ID: "s:" + instanceID, InstanceID: instanceID})
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

// close fails all pending round-trips and in-flight execs, then closes the
// socket.
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

	// A shell command whose runner just vanished will never report an exit;
	// synthesise one so its stream ends instead of hanging.
	c.hub.failExecsForConn(c, "runner disconnected: "+reason)

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
			// A send failure surfaces as the bridge connection dropping, which the
			// read loop already handles; there is no recovery at this call site.
			_ = c.send(Frame{Type: FrameStreamOpen, ID: "s:" + instanceID, InstanceID: instanceID})
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

	case FrameExecData:
		c.publishExecData(f.ExecID, f.Stream, f.Chunk)

	case FrameExecExit:
		// Record before publishing: the fan-out may drop this event, and the
		// recorded outcome is what lets the handler end the stream anyway.
		if f.ExecID != "" {
			c.hub.finishExec(c.runnerID, f.ExecID, f.ExitCode, f.Error)
			c.hub.publishExecExit(c.runnerID, f.ExecID, f.ExitCode, f.Error)
		}

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

// publishExecData fans one chunk of command output out to the HTTP handler
// streaming that exec. The fan-out is non-blocking by design — this runs on
// the connection's read loop, and blocking it would stall every other frame
// for this runner — so a browser that falls too far behind loses the chunk.
// That loss is counted rather than swallowed: the handler turns it into a
// visible truncation marker, since a transcript silently missing output is
// worse than a slow one.
func (c *runnerConn) publishExecData(execID, stream, chunk string) {
	if c.hub.rt == nil || execID == "" {
		return
	}
	_, dropped := c.hub.rt.PublishTracked(realtime.ExecTopic(c.runnerID, execID), "exec_data", map[string]any{
		"exec_id": execID,
		"stream":  stream,
		"chunk":   chunk,
	})
	if dropped > 0 {
		c.hub.recordExecDrop(c.runnerID, execID, len(chunk))
	}
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
