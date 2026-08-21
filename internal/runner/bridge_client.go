package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
	"github.com/huynle/brain-api/internal/bridge"
	"github.com/huynle/brain-api/internal/types"
)

// adhocInstance tracks an ad-hoc OpenCode instance spawned via the bridge.
type adhocInstance struct {
	Instance types.OpencodeInstance `json:"instance"`
	proc     Process
}

// adhocStateFileName persists ad-hoc instances across runner restarts so
// they can be re-adopted (runner IDs are ephemeral; the file is not).
const adhocStateFileName = "adhoc-instances.json"

// BridgeClient maintains the outbound WebSocket tunnel to brain-api,
// executes proxied requests against local OpenCode instances, pumps their
// event streams upstream, and spawns/kills ad-hoc instances.
type BridgeClient struct {
	runner *TaskRunner

	ctx context.Context

	wsMu    sync.Mutex
	ws      *websocket.Conn
	writeMu sync.Mutex

	mu      sync.Mutex
	adhoc   map[string]*adhocInstance
	pumps   map[string]context.CancelFunc
	streams map[string]bool // instanceID → full stream requested

	// execs tracks in-flight runner-shell commands so exec_signal frames can
	// reach them. Guarded by its own mutex: an exec lives much longer than
	// any bc.mu critical section and must never contend with instance state.
	execMu sync.Mutex
	execs  map[string]*execProcess

	// externalListeners caches the result of `lsof -c opencode -sTCP:LISTEN`
	// briefly. The audit UI's history fetch falls back to scanning every
	// localhost OpenCode HTTP server when the session was hosted by a TUI
	// brain didn't spawn (its messages may still only live in that server's
	// memory). Cached for externalListenersTTL to avoid hammering lsof.
	externalListenersMu     sync.Mutex
	externalListenersCached []OpencodeListener
	externalListenersAt     time.Time

	httpClient *http.Client
}

// externalListenersTTL bounds how long DiscoverOpencodeListeners output is
// reused. Short enough that a freshly started TUI shows up promptly, long
// enough to coalesce bursts of history requests.
const externalListenersTTL = 5 * time.Second

// NewBridgeClient creates a bridge client for the given runner.
func NewBridgeClient(tr *TaskRunner) *BridgeClient {
	return &BridgeClient{
		runner:  tr,
		adhoc:   make(map[string]*adhocInstance),
		pumps:   make(map[string]context.CancelFunc),
		streams: make(map[string]bool),
		execs:   make(map[string]*execProcess),
		httpClient: &http.Client{
			Timeout: time.Duration(bridge.DefaultTimeoutMs) * time.Millisecond,
		},
	}
}

// Start runs the bridge connection loop until the context is cancelled.
// Reconnects with exponential backoff + jitter; re-sends hello (with the
// current instance list) on every reconnect so the server reconciles.
func (bc *BridgeClient) Start(ctx context.Context) {
	bc.ctx = ctx
	bc.readoptAdhocInstances()

	backoff := initialBackoff
	for ctx.Err() == nil {
		err := bc.runConnection(ctx)
		if ctx.Err() != nil {
			break
		}
		if err != nil {
			slog.Debug("bridge client: connection ended", "error", err)
		}
		jitter := time.Duration(rand.Int63n(int64(backoff) / 2))
		select {
		case <-ctx.Done():
		case <-time.After(backoff + jitter):
		}
		backoff *= backoffMultiplier
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
	bc.shutdownPumps()
	bc.shutdownExecs()
}

// bridgeURL converts the Brain API base URL to the runner's bridge endpoint.
func (bc *BridgeClient) bridgeURL() string {
	base := strings.TrimRight(bc.runner.config.BrainAPIURL, "/")
	return fmt.Sprintf("%s/api/v1/runners/%s/bridge", base, bc.runner.runnerID)
}

func (bc *BridgeClient) runConnection(ctx context.Context) error {
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	headers := http.Header{}
	if bc.runner.config.APIToken != "" {
		headers.Set("Authorization", "Bearer "+bc.runner.config.APIToken)
	}
	ws, _, err := websocket.Dial(dialCtx, bc.bridgeURL(), &websocket.DialOptions{
		HTTPHeader: headers,
	})
	if err != nil {
		return fmt.Errorf("bridge dial: %w", err)
	}
	ws.SetReadLimit(bridge.MaxFrameBytes)

	bc.wsMu.Lock()
	bc.ws = ws
	bc.wsMu.Unlock()
	defer func() {
		bc.wsMu.Lock()
		bc.ws = nil
		bc.wsMu.Unlock()
		ws.Close(websocket.StatusNormalClosure, "reconnecting")
	}()

	slog.Info("bridge client: connected", "runner_id", bc.runner.runnerID)

	// Hello with current instance snapshot so the server reconciles.
	bc.sendFrame(bridge.Frame{
		Type:      bridge.FrameHello,
		RunnerID:  bc.runner.runnerID,
		Proto:     bridge.ProtoVersion,
		Instances: bc.runner.instanceSnapshot(),
	})

	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			return err
		}
		var f bridge.Frame
		if err := json.Unmarshal(data, &f); err != nil {
			slog.Warn("bridge client: bad frame", "error", err)
			continue
		}
		bc.handleFrame(f)
	}
}

func (bc *BridgeClient) sendFrame(f bridge.Frame) error {
	bc.wsMu.Lock()
	ws := bc.ws
	bc.wsMu.Unlock()
	if ws == nil {
		return errors.New("bridge not connected")
	}
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	bc.writeMu.Lock()
	defer bc.writeMu.Unlock()
	return ws.Write(ctx, websocket.MessageText, b)
}

func (bc *BridgeClient) handleFrame(f bridge.Frame) {
	switch f.Type {
	case bridge.FrameReq:
		go bc.handleReq(f)
	case bridge.FrameStreamOpen:
		bc.mu.Lock()
		bc.streams[f.InstanceID] = true
		bc.mu.Unlock()
		bc.EnsurePump(f.InstanceID)
	case bridge.FrameStreamClose:
		bc.mu.Lock()
		delete(bc.streams, f.InstanceID)
		bc.mu.Unlock()
	case bridge.FrameSpawn:
		go bc.handleSpawn(f)
	case bridge.FrameKill:
		go bc.handleKill(f)
	case bridge.FrameAbortTask:
		go bc.handleAbortTask(f)
	case bridge.FrameHistory:
		go bc.handleHistory(f)
	case bridge.FrameExecStart:
		go bc.handleExecStart(f)
	case bridge.FrameExecSignal:
		go bc.handleExecSignal(f)
	default:
		slog.Debug("bridge client: unhandled frame", "type", f.Type)
	}
}

// ---------------------------------------------------------------------------
// Proxied requests
// ---------------------------------------------------------------------------

// resolveInstancePort maps an instance ID to its localhost port. The port
// NEVER comes from the wire — only from local process state (D3).
func (bc *BridgeClient) resolveInstancePort(instanceID string) (int, error) {
	bc.mu.Lock()
	ad := bc.adhoc[instanceID]
	bc.mu.Unlock()
	if ad != nil {
		if ad.Instance.Port == 0 {
			return 0, errors.New("instance port not yet discovered")
		}
		return ad.Instance.Port, nil
	}
	for _, info := range bc.runner.processMgr.GetAll() {
		if info.Task.InstanceID == instanceID {
			if info.Task.OpencodePort == 0 {
				return 0, errors.New("instance port not yet discovered")
			}
			return info.Task.OpencodePort, nil
		}
	}
	return 0, fmt.Errorf("unknown instance %q", instanceID)
}

func (bc *BridgeClient) handleReq(f bridge.Frame) {
	status, body, err := bc.proxyRequest(f)
	res := bridge.Frame{Type: bridge.FrameRes, ID: f.ID, Status: status, Body: body}
	if err != nil {
		res.Error = err.Error()
	}
	if sendErr := bc.sendFrame(res); sendErr != nil {
		slog.Debug("bridge client: failed to send response", "error", sendErr)
	}
}

func (bc *BridgeClient) proxyRequest(f bridge.Frame) (int, []byte, error) {
	// Defense in depth: the API enforces the allowlist too.
	if !bridge.AllowedRequest(f.Method, f.Path) {
		return 0, nil, fmt.Errorf("path %s %s not on bridge allowlist", f.Method, f.Path)
	}
	if len(f.Body) > bridge.MaxBodyBytes {
		return 0, nil, fmt.Errorf("request body exceeds %d bytes", bridge.MaxBodyBytes)
	}
	port, err := bc.resolveInstancePort(f.InstanceID)
	if err != nil {
		return 0, nil, err
	}

	timeout := time.Duration(f.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = time.Duration(bridge.DefaultTimeoutMs) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(bc.baseContext(), timeout)
	defer cancel()

	var bodyReader io.Reader
	if len(f.Body) > 0 {
		bodyReader = bytes.NewReader(f.Body)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, f.Path)
	req, err := http.NewRequestWithContext(ctx, f.Method, url, bodyReader)
	if err != nil {
		return 0, nil, err
	}
	if len(f.Body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := bc.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, bridge.MaxBodyBytes))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, respBody, nil
}

func (bc *BridgeClient) baseContext() context.Context {
	if bc.ctx != nil {
		return bc.ctx
	}
	return context.Background()
}

// ---------------------------------------------------------------------------
// Event pumps (per instance)
// ---------------------------------------------------------------------------

// EnsurePump starts the event pump for an instance if not already running.
// Control-class events are always forwarded; full events only while a
// browser stream is open.
func (bc *BridgeClient) EnsurePump(instanceID string) {
	if instanceID == "" || bc.ctx == nil {
		return
	}
	bc.mu.Lock()
	if _, exists := bc.pumps[instanceID]; exists {
		bc.mu.Unlock()
		return
	}
	pumpCtx, cancel := context.WithCancel(bc.ctx)
	bc.pumps[instanceID] = cancel
	bc.mu.Unlock()

	go bc.runPump(pumpCtx, instanceID)
}

// StopPump stops the event pump for an instance.
func (bc *BridgeClient) StopPump(instanceID string) {
	bc.mu.Lock()
	cancel := bc.pumps[instanceID]
	delete(bc.pumps, instanceID)
	delete(bc.streams, instanceID)
	bc.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (bc *BridgeClient) shutdownPumps() {
	bc.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(bc.pumps))
	for _, cancel := range bc.pumps {
		cancels = append(cancels, cancel)
	}
	bc.pumps = make(map[string]context.CancelFunc)
	bc.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (bc *BridgeClient) runPump(ctx context.Context, instanceID string) {
	defer bc.StopPump(instanceID)
	for ctx.Err() == nil {
		port, err := bc.resolveInstancePort(instanceID)
		if err != nil || port == 0 {
			// Unknown instance entirely → give up; port pending → retry.
			if err != nil && strings.HasPrefix(err.Error(), "unknown instance") {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
			continue
		}

		if err := bc.tailEvents(ctx, instanceID, port); err != nil && ctx.Err() == nil {
			slog.Debug("bridge client: event stream dropped",
				"instance_id", instanceID, "error", err)
		}
		// Instance gone? Stop. Otherwise reconnect after a short pause.
		if _, err := bc.resolveInstancePort(instanceID); err != nil {
			bc.sendFrame(bridge.Frame{
				Type:       bridge.FrameStreamClosed,
				ID:         "s:" + instanceID,
				InstanceID: instanceID,
				Reason:     "instance_exited",
			})
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

// isControlEvent classifies OpenCode events that must always flow upstream
// (permission prompts, session lifecycle) vs. the full message firehose.
func isControlEvent(eventType string) bool {
	return strings.HasPrefix(eventType, "permission.") ||
		strings.HasPrefix(eventType, "session.")
}

// tailEvents tails GET /event on a local instance and forwards events.
func (bc *BridgeClient) tailEvents(ctx context.Context, instanceID string, port int) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/event", port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")

	// No timeout: this is a long-lived stream bounded by ctx.
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET /event: status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if raw == "" || !json.Valid([]byte(raw)) {
			continue
		}
		bc.forwardEvent(instanceID, json.RawMessage(raw))
	}
	if ctx.Err() != nil {
		return nil
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return errors.New("event stream closed")
}

func (bc *BridgeClient) forwardEvent(instanceID string, raw json.RawMessage) {
	var head struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(raw, &head)

	if isControlEvent(head.Type) {
		bc.sendFrame(bridge.Frame{
			Type:       bridge.FrameInstanceEvent,
			InstanceID: instanceID,
			Event:      raw,
		})
		return
	}

	bc.mu.Lock()
	active := bc.streams[instanceID]
	bc.mu.Unlock()
	if active {
		bc.sendFrame(bridge.Frame{
			Type:       bridge.FrameStreamEvent,
			ID:         "s:" + instanceID,
			InstanceID: instanceID,
			Event:      raw,
		})
	}
}

// ---------------------------------------------------------------------------
// Ad-hoc instance spawn / kill
// ---------------------------------------------------------------------------

// allowedWorkdirRoots returns the configured spawn roots, defaulting to the
// user's home directory.
func (bc *BridgeClient) allowedWorkdirRoots() []string {
	roots := bc.runner.config.Control.AllowedWorkdirRoots
	if len(roots) > 0 {
		return roots
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return []string{home}
	}
	return nil
}

func (bc *BridgeClient) validateSpawnWorkdir(workdir string) error {
	if workdir == "" || !filepath.IsAbs(workdir) {
		return errors.New("workdir must be an absolute path")
	}
	info, err := os.Stat(workdir)
	if err != nil {
		return fmt.Errorf("workdir: %w", err)
	}
	if !info.IsDir() {
		return errors.New("workdir is not a directory")
	}
	roots := bc.allowedWorkdirRoots()
	if len(roots) == 0 {
		return errors.New("no allowed workdir roots configured")
	}
	cleaned := filepath.Clean(workdir)
	for _, root := range roots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		absRoot = filepath.Clean(absRoot)
		if cleaned == absRoot || strings.HasPrefix(cleaned, absRoot+string(filepath.Separator)) {
			return nil
		}
	}
	return fmt.Errorf("workdir %q is not under any allowed root: %v", workdir, roots)
}

func (bc *BridgeClient) opencodeBin() string {
	if bin := bc.runner.config.Opencode.Bin; bin != "" {
		return bin
	}
	return "opencode"
}

func (bc *BridgeClient) handleSpawn(f bridge.Frame) {
	inst, err := bc.spawnAdhoc(f.Spec)
	res := bridge.Frame{Type: bridge.FrameRes, ID: f.ID}
	if err != nil {
		res.Error = err.Error()
	} else {
		body, mErr := json.Marshal(inst)
		if mErr != nil {
			res.Error = mErr.Error()
		} else {
			res.Status = http.StatusOK
			res.Body = body
		}
	}
	bc.sendFrame(res)
}

func (bc *BridgeClient) spawnAdhoc(spec *types.SpawnInstanceSpec) (*types.OpencodeInstance, error) {
	if spec == nil {
		return nil, errors.New("missing spawn spec")
	}
	if err := bc.validateSpawnWorkdir(spec.Workdir); err != nil {
		return nil, err
	}

	instanceID := generateInstanceID()
	args := []string{"serve", "--port", "0"}

	logPath := filepath.Join(bc.runner.config.StateDir, fmt.Sprintf("adhoc_%s.log", instanceID))
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("create adhoc log: %w", err)
	}

	cmd := exec.Command(bc.opencodeBin(), args...)
	cmd.Dir = spec.Workdir
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("start opencode serve: %w", err)
	}
	proc := NewOsProcess(cmd)
	go func() {
		<-proc.Done()
		logFile.Close()
	}()

	// Discover the random port (opencode logs it; lsof is executor-agnostic).
	var port int
	for attempt := 0; attempt < 10; attempt++ {
		if proc.Exited() {
			return nil, fmt.Errorf("opencode serve exited during startup (code %d)", proc.ExitCode())
		}
		port, err = DiscoverPort(proc.Pid())
		if err == nil && port > 0 {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if port == 0 {
		proc.Kill(syscall.SIGTERM)
		return nil, errors.New("failed to discover port for ad-hoc instance")
	}

	title := spec.Title
	if title == "" {
		title = filepath.Base(spec.Workdir)
	}
	inst := types.OpencodeInstance{
		InstanceID: instanceID,
		RunnerID:   bc.runner.runnerID,
		Hostname:   runnerHostname(),
		Kind:       types.InstanceKindAdhoc,
		Title:      title,
		Workdir:    spec.Workdir,
		Port:       port,
		PID:        proc.Pid(),
		Status:     types.InstanceStatusIdle,
		Executor:   "opencode",
		StartedAt:  time.Now().UnixMilli(),
		LastSeen:   time.Now().UnixMilli(),
	}

	bc.mu.Lock()
	bc.adhoc[instanceID] = &adhocInstance{Instance: inst, proc: proc}
	bc.mu.Unlock()
	bc.persistAdhocState()

	bc.runner.reportInstance(inst)
	bc.EnsurePump(instanceID)

	slog.Info("bridge client: spawned ad-hoc instance",
		"instance_id", instanceID, "workdir", spec.Workdir, "port", port, "pid", proc.Pid())
	return &inst, nil
}

func (bc *BridgeClient) handleKill(f bridge.Frame) {
	err := bc.killAdhoc(f.InstanceID)
	res := bridge.Frame{Type: bridge.FrameRes, ID: f.ID, Status: http.StatusOK}
	if err != nil {
		res.Error = err.Error()
		res.Status = 0
	}
	bc.sendFrame(res)
}

func (bc *BridgeClient) killAdhoc(instanceID string) error {
	bc.mu.Lock()
	ad := bc.adhoc[instanceID]
	delete(bc.adhoc, instanceID)
	bc.mu.Unlock()
	if ad == nil {
		// Task instances are owned by the task lifecycle — refuse.
		return fmt.Errorf("instance %q is not an ad-hoc instance", instanceID)
	}

	bc.StopPump(instanceID)
	if !ad.proc.Exited() {
		ad.proc.Kill(syscall.SIGTERM)
		// Give it a moment, then force.
		go func(p Process) {
			time.Sleep(5 * time.Second)
			if !p.Exited() {
				p.Kill(syscall.SIGKILL)
			}
		}(ad.proc)
	}
	bc.persistAdhocState()
	bc.runner.removeInstance(instanceID)
	slog.Info("bridge client: killed ad-hoc instance", "instance_id", instanceID)
	return nil
}

// ---------------------------------------------------------------------------
// Session history (hybrid: live instance proxy, else on-disk read)
// ---------------------------------------------------------------------------

func (bc *BridgeClient) handleAbortTask(f bridge.Frame) {
	err := bc.abortTask(f.TaskID)
	res := bridge.Frame{Type: bridge.FrameRes, ID: f.ID, Status: http.StatusOK}
	if err != nil {
		res.Error = err.Error()
		res.Status = 0
	}
	bc.sendFrame(res)
}

func (bc *BridgeClient) abortTask(taskID string) error {
	if taskID == "" {
		return errors.New("task id is required")
	}
	info := bc.runner.processMgr.Get(taskID)
	if info == nil {
		return fmt.Errorf("task %q is not running", taskID)
	}
	bc.runner.processMgr.Kill(bc.ctx, taskID)
	bc.runner.processMgr.Remove(taskID)
	bc.runner.removeInstance(info.Task.InstanceID)
	if err := bc.runner.client.UpdateTaskStatus(bc.ctx, info.Task.Path, "pending"); err != nil {
		return fmt.Errorf("reset task status: %w", err)
	}
	bc.runner.cleanupTaskTmux(info.Task)
	bc.runner.releaseDispatchLease(bc.ctx, info.Task.ProjectID, taskID)
	bc.runner.emitEvent(RunnerEvent{
		Type:      EventTaskReleased,
		TaskID:    taskID,
		ProjectID: info.Task.ProjectID,
		TaskPath:  info.Task.Path,
		FeatureID: info.Task.FeatureID,
		Reason:    "aborted by control",
	})
	slog.Info("bridge client: aborted task instance", "task_id", taskID, "instance_id", info.Task.InstanceID)
	return nil
}

func (bc *BridgeClient) handleHistory(f bridge.Frame) {
	body, err := bc.fetchSessionHistory(f.SessionID)
	res := bridge.Frame{Type: bridge.FrameRes, ID: f.ID}
	if err != nil {
		res.Error = err.Error()
	} else {
		res.Status = http.StatusOK
		res.Body = body
	}
	bc.sendFrame(res)
}

// fetchSessionHistory returns the transcript for a session as the same JSON
// array OpenCode's GET /session/:id/message produces. It first looks for a
// live instance that already hosts the session (so an in-flight session is
// served from its running server); failing that it reads the messages from
// OpenCode's on-disk storage, which survives the instance's exit.
//
// If neither brain-tracked instances nor on-disk storage have the session,
// this falls back to scanning every localhost OpenCode HTTP server (TUIs
// the user started outside of brain). OpenCode does not flush every
// session's message/<sid>/ to disk eagerly, so a TUI's in-memory transcript
// is often the only place a recently-completed session can still be read.
func (bc *BridgeClient) fetchSessionHistory(sessionID string) ([]byte, error) {
	if sessionID == "" {
		return nil, errors.New("missing session id")
	}
	if port := bc.portForSession(sessionID); port > 0 {
		path := "/session/" + sessionID + "/message"
		if status, body, err := bc.httpGet(port, path); err == nil && status == http.StatusOK {
			return body, nil
		}
		// Fall through to on-disk read if the live server can't answer.
	}
	if body, err := readSessionHistorySQLite(sessionID); err == nil {
		return body, nil
	}
	if body, err := readSessionHistory(sessionID); err == nil {
		return body, nil
	}
	// Last resort: a TUI brain never spawned may still hold the messages in
	// memory. Probe every localhost OpenCode listener.
	if port := bc.portForExternalSession(sessionID); port > 0 {
		path := "/session/" + sessionID + "/message"
		if status, body, err := bc.httpGet(port, path); err == nil && status == http.StatusOK {
			return body, nil
		}
	}
	// Re-run readSessionHistory so the caller gets its (well-shaped) error.
	return readSessionHistory(sessionID)
}

// portForSession returns the localhost port of a live instance whose session
// set includes sessionID, or 0 if none. Ports never come from the wire.
func (bc *BridgeClient) portForSession(sessionID string) int {
	bc.mu.Lock()
	for _, ad := range bc.adhoc {
		for _, sid := range ad.Instance.SessionIDs {
			if sid == sessionID && ad.Instance.Port > 0 {
				bc.mu.Unlock()
				return ad.Instance.Port
			}
		}
	}
	bc.mu.Unlock()
	for _, info := range bc.runner.processMgr.GetAll() {
		if info.Task.SessionID == sessionID && info.Task.OpencodePort > 0 {
			return info.Task.OpencodePort
		}
	}
	return 0
}

// portForExternalSession asks every OpenCode HTTP server on this host
// whether it currently hosts sessionID. It returns the first port whose
// GET /session/<sid>/message responds 200 with a non-empty body, or 0 if
// none does. The listener list is cached for externalListenersTTL.
//
// This is a slow path — used only after fast in-memory checks and on-disk
// storage have both come up empty. It exists so the audit UI can still
// review sessions from user-started TUIs, since OpenCode does not
// guarantee that message/<sid>/ is on disk before the instance exits.
//
// Each probe uses a tight timeout so a stalled or non-OpenCode listener
// can't block the whole audit request when several listeners are present.
func (bc *BridgeClient) portForExternalSession(sessionID string) int {
	listeners := bc.externalListeners()
	if len(listeners) == 0 {
		return 0
	}
	path := "/session/" + sessionID + "/message"
	for _, l := range listeners {
		status, body, err := bc.httpGetFast(l.Port, path, externalProbeTimeout)
		if err != nil || status != http.StatusOK {
			continue
		}
		// Empty array means the server replied but doesn't actually have
		// any messages for this session — keep looking.
		trimmed := strings.TrimSpace(string(body))
		if trimmed == "" || trimmed == "[]" {
			continue
		}
		return l.Port
	}
	return 0
}

// externalProbeTimeout bounds each per-listener probe. OpenCode's
// /session/<id>/message is an in-memory lookup, so a few hundred ms is
// generous; making this too long lets a stuck listener block the whole
// audit request.
const externalProbeTimeout = 750 * time.Millisecond

// externalListeners returns the cached lsof-discovered OpenCode listeners,
// refreshing the cache when it's older than externalListenersTTL.
func (bc *BridgeClient) externalListeners() []OpencodeListener {
	bc.externalListenersMu.Lock()
	defer bc.externalListenersMu.Unlock()
	if time.Since(bc.externalListenersAt) < externalListenersTTL && bc.externalListenersCached != nil {
		return bc.externalListenersCached
	}
	bc.externalListenersCached = DiscoverOpencodeListeners()
	bc.externalListenersAt = time.Now()
	return bc.externalListenersCached
}

// httpGet performs a bounded GET against a localhost instance port.
func (bc *BridgeClient) httpGet(port int, path string) (int, []byte, error) {
	return bc.httpGetFast(port, path, time.Duration(bridge.DefaultTimeoutMs)*time.Millisecond)
}

// httpGetFast is like httpGet but with a caller-supplied per-request
// timeout. Used by external-listener probes to keep a stuck listener from
// blocking the whole audit request.
func (bc *BridgeClient) httpGetFast(port int, path string, timeout time.Duration) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(bc.baseContext(), timeout)
	defer cancel()
	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	resp, err := bc.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, bridge.MaxBodyBytes))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

// AdhocInstances returns instance records for all live ad-hoc instances,
// merged into heartbeat snapshots.
func (bc *BridgeClient) AdhocInstances(hostname string) []types.OpencodeInstance {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	out := make([]types.OpencodeInstance, 0, len(bc.adhoc))
	for _, ad := range bc.adhoc {
		inst := ad.Instance
		inst.Hostname = hostname
		inst.RunnerID = bc.runner.runnerID
		inst.LastSeen = time.Now().UnixMilli()
		if ad.proc.Exited() {
			inst.Status = types.InstanceStatusExited
		}
		out = append(out, inst)
	}
	return out
}

// ---------------------------------------------------------------------------
// Runner shell (exec)
// ---------------------------------------------------------------------------

// execDrainGrace bounds how long the runner keeps reading a finished
// command's pipes. The child has already exited at that point; the only
// thing that can still hold the pipe open is a backgrounded grandchild that
// inherited the fd (`some-daemon &`). Without this bound such a command
// would never produce an exec_exit and the UI would hang forever.
const execDrainGrace = 2 * time.Second

// execTimeoutExitCode mirrors coreutils `timeout`: 124 means "killed
// because it ran out of time", distinct from any code the command itself
// could return.
const execTimeoutExitCode = 124

// execUnknownExitCode is reported when the command's status could not be
// determined at all (a wait error that is not an *exec.ExitError).
const execUnknownExitCode = -1

// execProcess tracks one running runner-shell command so exec_signal frames
// can reach it. The process is spawned in its own process group, so signals
// are delivered to the whole child tree rather than just to bash.
type execProcess struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	pgid   int
	cancel context.CancelFunc
}

// signal delivers sig to the command's process group, falling back to the
// direct child if the group send fails.
func (ep *execProcess) signal(sig syscall.Signal) error {
	ep.mu.Lock()
	cmd, pgid := ep.cmd, ep.pgid
	ep.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return errors.New("exec has not started yet")
	}
	if pgid > 0 {
		if err := syscall.Kill(-pgid, sig); err == nil {
			return nil
		}
	}
	return cmd.Process.Signal(sig)
}

func (bc *BridgeClient) handleExecStart(f bridge.Frame) {
	if err := bc.startExec(f); err != nil {
		// Pre-spawn failure: reply with the error and send NO exec_exit —
		// the API turns this into a plain JSON error before the SSE stream
		// is opened.
		if sendErr := bc.sendFrame(bridge.Frame{
			Type: bridge.FrameRes, ID: f.ID, Error: err.Error(),
		}); sendErr != nil {
			slog.Debug("bridge client: failed to send exec_start error", "error", sendErr)
		}
		return
	}
	// Mandatory ack: the API blocks on this before opening the stream.
	if err := bc.sendFrame(bridge.Frame{
		Type: bridge.FrameRes, ID: f.ID, Status: http.StatusOK,
	}); err != nil {
		slog.Debug("bridge client: failed to ack exec_start", "error", err)
	}
}

// startExec validates and spawns a shell command, then hands its output
// pipes to background goroutines. It returns only spawn-time errors; once it
// returns nil the command is running and exactly one exec_exit frame is
// guaranteed to follow.
//
// The shell is deliberately unrestricted: authorization happens once, at the
// control API route, and there is no command allow/blocklist or workdir
// confinement here.
func (bc *BridgeClient) startExec(f bridge.Frame) error {
	if f.ExecID == "" {
		return errors.New("exec id is required")
	}
	if f.Command == "" {
		return errors.New("command is required")
	}
	if len(f.Command) > bridge.ExecMaxCommandBytes {
		return fmt.Errorf("command exceeds %d bytes", bridge.ExecMaxCommandBytes)
	}

	workdir, err := bc.resolveExecWorkdir(f.Workdir)
	if err != nil {
		return err
	}

	// Reserve the ID before spawning so a duplicate exec_start can't race
	// two processes onto the same stream.
	ep := &execProcess{}
	bc.execMu.Lock()
	if _, exists := bc.execs[f.ExecID]; exists {
		bc.execMu.Unlock()
		return fmt.Errorf("exec %q is already running", f.ExecID)
	}
	bc.execs[f.ExecID] = ep
	bc.execMu.Unlock()

	timeout := execTimeout(f.ExecTimeoutMs)
	ctx, cancel := context.WithCancel(bc.baseContext())

	cmd := exec.CommandContext(ctx, "bash", "-lc", f.Command)
	cmd.Dir = workdir
	cmd.Env = bc.execEnv()
	// Own process group so a signal reaches the whole child tree, matching
	// how the daemon supervisor spawns processes.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Context cancellation (runner shutdown) must take the group down too,
	// not just bash.
	cmd.Cancel = func() error {
		err := signalProcessGroup(cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			// Already gone — os/exec treats this as a clean no-op.
			return os.ErrProcessDone
		}
		return err
	}

	// Explicit os.Pipe rather than cmd.StdoutPipe: os/exec closes the pipes
	// it owns inside Wait(), which would force us to finish reading before
	// waiting. With our own pipes the wait and the reads are independent, so
	// a grandchild holding the fd open can't stop us from reporting the exit.
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		cancel()
		bc.finishExec(f.ExecID)
		return fmt.Errorf("create stdout pipe: %w", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		stdoutR.Close()
		stdoutW.Close()
		cancel()
		bc.finishExec(f.ExecID)
		return fmt.Errorf("create stderr pipe: %w", err)
	}
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW

	if err := cmd.Start(); err != nil {
		stdoutR.Close()
		stdoutW.Close()
		stderrR.Close()
		stderrW.Close()
		cancel()
		bc.finishExec(f.ExecID)
		return fmt.Errorf("start command: %w", err)
	}
	// The child owns the write ends now.
	stdoutW.Close()
	stderrW.Close()

	pgid, pgErr := syscall.Getpgid(cmd.Process.Pid)
	if pgErr != nil || pgid <= 0 {
		pgid = cmd.Process.Pid
	}
	ep.mu.Lock()
	ep.cmd = cmd
	ep.pgid = pgid
	ep.cancel = cancel
	ep.mu.Unlock()

	slog.Info("bridge client: exec started",
		"exec_id", f.ExecID, "workdir", workdir, "pid", cmd.Process.Pid,
		"timeout", timeout, "command", f.Command)

	go bc.runExec(f.ExecID, ep, cmd, cancel, stdoutR, stderrR, timeout)
	return nil
}

// runExec pumps both pipes upstream and reports the command's exit exactly
// once.
func (bc *BridgeClient) runExec(
	execID string,
	ep *execProcess,
	cmd *exec.Cmd,
	cancel context.CancelFunc,
	stdoutR, stderrR *os.File,
	timeout time.Duration,
) {
	defer cancel()
	defer stdoutR.Close()
	defer stderrR.Close()

	var once sync.Once
	sendExit := func(code int, reason string) {
		once.Do(func() {
			bc.finishExec(execID)
			if err := bc.sendFrame(bridge.Frame{
				Type:     bridge.FrameExecExit,
				ExecID:   execID,
				ExitCode: code,
				Error:    reason,
			}); err != nil {
				slog.Debug("bridge client: failed to send exec_exit",
					"exec_id", execID, "error", err)
			}
		})
	}
	// Belt and braces: the UI waits forever without an exec_exit, so even a
	// panic on the paths below still terminates the stream.
	defer sendExit(execUnknownExitCode, "exec terminated unexpectedly")

	var timedOut atomic.Bool
	timer := time.AfterFunc(timeout, func() {
		timedOut.Store(true)
		if err := ep.signal(syscall.SIGKILL); err != nil {
			slog.Debug("bridge client: exec timeout kill failed",
				"exec_id", execID, "error", err)
		}
	})
	defer timer.Stop()

	var wg sync.WaitGroup
	wg.Add(2)
	go bc.pumpExecStream(execID, bridge.ExecStreamStdout, stdoutR, &wg)
	go bc.pumpExecStream(execID, bridge.ExecStreamStderr, stderrR, &wg)
	drained := make(chan struct{})
	go func() {
		wg.Wait()
		close(drained)
	}()

	waitErr := cmd.Wait()
	timer.Stop()

	// The process is gone; give any grandchild that inherited the pipes a
	// short grace period to flush, then close the read ends to unblock the
	// pumps so exec_exit is never withheld.
	select {
	case <-drained:
	case <-time.After(execDrainGrace):
		stdoutR.Close()
		stderrR.Close()
		<-drained
	}

	// Note what is deliberately NOT done here: the process group is not
	// killed when the command ends. A job the caller backgrounded with `&`
	// therefore outlives its exec, exactly as it would in a terminal (POSIX
	// has a non-interactive shell's background jobs ignore SIGINT, so Ctrl+C
	// reaches the foreground process only). That keeps `nohup svc &` usable,
	// at the cost of a process the shell can no longer address — its exec id
	// is gone from the registry. The timeout path above is the exception: a
	// runaway command is a leak, so it SIGKILLs the whole group.
	code, reason := execExitResult(waitErr)
	if timedOut.Load() {
		code = execTimeoutExitCode
		reason = fmt.Sprintf("command timed out after %s", timeout)
	}
	slog.Debug("bridge client: exec finished",
		"exec_id", execID, "exit_code", code, "error", reason)
	sendExit(code, reason)
}

// pumpExecStream forwards output as it arrives — no line buffering, so an
// interactive command's partial line shows up immediately.
func (bc *BridgeClient) pumpExecStream(execID, stream string, r io.Reader, wg *sync.WaitGroup) {
	defer wg.Done()

	send := func(b []byte) {
		if len(b) == 0 {
			return
		}
		// Chunk rides the wire as a JSON string; invalid UTF-8 from a binary
		// command would otherwise be mangled by json.Marshal.
		if err := bc.sendFrame(bridge.Frame{
			Type:   bridge.FrameExecData,
			ExecID: execID,
			Stream: stream,
			Chunk:  strings.ToValidUTF8(string(b), "�"),
		}); err != nil {
			slog.Debug("bridge client: failed to send exec_data",
				"exec_id", execID, "stream", stream, "error", err)
		}
	}

	buf := make([]byte, bridge.ExecChunkBytes)
	// carry holds a multi-byte rune split across two reads so a chunk
	// boundary never turns valid output into replacement characters.
	var carry []byte
	for {
		n, err := r.Read(buf)
		if n > 0 {
			data := buf[:n]
			if len(carry) > 0 {
				data = append(carry, data...)
				carry = nil
			}
			if cut := trailingPartialRune(data); cut < len(data) {
				carry = append([]byte(nil), data[cut:]...)
				data = data[:cut]
			}
			send(data)
		}
		if err != nil {
			send(carry)
			return
		}
	}
}

func (bc *BridgeClient) handleExecSignal(f bridge.Frame) {
	res := bridge.Frame{Type: bridge.FrameRes, ID: f.ID, Status: http.StatusOK}
	if err := bc.signalExec(f.ExecID, f.Signal); err != nil {
		res.Error = err.Error()
		res.Status = 0
	}
	if err := bc.sendFrame(res); err != nil {
		slog.Debug("bridge client: failed to reply to exec_signal", "error", err)
	}
}

// signalExec delivers a signal to a running exec. The registry entry is NOT
// removed here — runExec drops it when the process actually exits, so a
// SIGINT that the command survives leaves it addressable.
func (bc *BridgeClient) signalExec(execID, name string) error {
	if execID == "" {
		return errors.New("exec id is required")
	}
	bc.execMu.Lock()
	ep := bc.execs[execID]
	bc.execMu.Unlock()
	if ep == nil {
		return fmt.Errorf("exec %q is not running", execID)
	}
	sig := execSignal(name)
	if err := ep.signal(sig); err != nil {
		return fmt.Errorf("signal exec: %w", err)
	}
	slog.Info("bridge client: signalled exec", "exec_id", execID, "signal", sig)
	return nil
}

// finishExec drops an exec from the registry. Safe to call more than once.
func (bc *BridgeClient) finishExec(execID string) {
	bc.execMu.Lock()
	delete(bc.execs, execID)
	bc.execMu.Unlock()
}

// shutdownExecs kills every still-running shell command. Called when the
// bridge client's context ends so a runner restart doesn't orphan a shell.
func (bc *BridgeClient) shutdownExecs() {
	bc.execMu.Lock()
	procs := make([]*execProcess, 0, len(bc.execs))
	for _, ep := range bc.execs {
		procs = append(procs, ep)
	}
	bc.execMu.Unlock()
	for _, ep := range procs {
		if err := ep.signal(syscall.SIGKILL); err != nil {
			slog.Debug("bridge client: exec shutdown kill failed", "error", err)
		}
		ep.mu.Lock()
		cancel := ep.cancel
		ep.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
}

// resolveExecWorkdir picks the directory a shell command runs in: the
// requested one, else the runner's configured work dir, else the user's
// home. There is no allowed-roots restriction on the shell by design.
func (bc *BridgeClient) resolveExecWorkdir(requested string) (string, error) {
	dir := strings.TrimSpace(requested)
	if dir == "" && bc.runner != nil {
		dir = strings.TrimSpace(bc.runner.config.WorkDir)
	}
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve default workdir: %w", err)
		}
		dir = home
	}
	if strings.HasPrefix(dir, "~") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("workdir: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("workdir: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workdir %q is not a directory", abs)
	}
	return abs, nil
}

// execEnv mirrors spawnScript's environment handling so a shell command sees
// the same Brain credentials a script task would.
func (bc *BridgeClient) execEnv() []string {
	env := os.Environ()
	if bc.runner == nil {
		return env
	}
	if url := bc.runner.config.BrainAPIURL; url != "" {
		env = append(env, "BRAIN_API_URL="+url)
	}
	if token := bc.runner.config.APIToken; token != "" {
		env = append(env, "BRAIN_API_TOKEN="+token)
	}
	return env
}

// execTimeout normalises a requested command budget into a duration.
func execTimeout(requestedMs int) time.Duration {
	ms := requestedMs
	if ms <= 0 {
		ms = bridge.ExecDefaultTimeoutMs
	}
	if ms > bridge.ExecMaxTimeoutMs {
		ms = bridge.ExecMaxTimeoutMs
	}
	return time.Duration(ms) * time.Millisecond
}

// execSignal maps a wire signal name onto a Unix signal. Unknown names fall
// back to SIGINT, the least destructive option.
func execSignal(name string) syscall.Signal {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "term":
		return syscall.SIGTERM
	case "kill":
		return syscall.SIGKILL
	default:
		return syscall.SIGINT
	}
}

// execExitResult derives the exit code and human reason from cmd.Wait().
func execExitResult(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			sig := ws.Signal()
			return 128 + int(sig), fmt.Sprintf("terminated by signal %s", sig)
		}
		if code := exitErr.ExitCode(); code >= 0 {
			return code, ""
		}
	}
	return execUnknownExitCode, err.Error()
}

// signalProcessGroup sends sig to pid's whole process group, falling back to
// the single process when the group send fails.
func signalProcessGroup(pid int, sig syscall.Signal) error {
	if pid <= 0 {
		return errors.New("invalid pid")
	}
	if pgid, err := syscall.Getpgid(pid); err == nil && pgid > 0 {
		if err := syscall.Kill(-pgid, sig); err == nil {
			return nil
		}
	}
	return syscall.Kill(pid, sig)
}

// trailingPartialRune returns the index where p's trailing incomplete UTF-8
// sequence begins, or len(p) when p does not end mid-rune.
func trailingPartialRune(p []byte) int {
	limit := utf8.UTFMax - 1
	if len(p) < limit {
		limit = len(p)
	}
	for i := 1; i <= limit; i++ {
		b := p[len(p)-i]
		if !utf8.RuneStart(b) {
			continue
		}
		need := 1
		switch {
		case b < 0x80:
			need = 1
		case b >= 0xF0:
			need = 4
		case b >= 0xE0:
			need = 3
		case b >= 0xC0:
			need = 2
		}
		if need > i {
			return len(p) - i
		}
		return len(p)
	}
	return len(p)
}

// ---------------------------------------------------------------------------
// Ad-hoc persistence / re-adoption
// ---------------------------------------------------------------------------

type adhocState struct {
	Instances []types.OpencodeInstance `json:"instances"`
}

func (bc *BridgeClient) adhocStatePath() string {
	return filepath.Join(bc.runner.config.StateDir, adhocStateFileName)
}

func (bc *BridgeClient) persistAdhocState() {
	bc.mu.Lock()
	state := adhocState{Instances: make([]types.OpencodeInstance, 0, len(bc.adhoc))}
	for _, ad := range bc.adhoc {
		state.Instances = append(state.Instances, ad.Instance)
	}
	bc.mu.Unlock()

	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(bc.adhocStatePath(), b, 0o644); err != nil {
		slog.Warn("bridge client: failed to persist adhoc state", "error", err)
	}
}

// readoptAdhocInstances re-adopts ad-hoc instances from a previous runner
// process: the PID must still be alive and the instance healthy. Stale
// entries are dropped.
func (bc *BridgeClient) readoptAdhocInstances() {
	b, err := os.ReadFile(bc.adhocStatePath())
	if err != nil {
		return
	}
	var state adhocState
	if err := json.Unmarshal(b, &state); err != nil {
		return
	}

	adopted := 0
	for _, inst := range state.Instances {
		if inst.PID <= 0 || !IsPidAlive(inst.PID) || !instanceHealthy(inst.Port) {
			continue
		}
		inst.RunnerID = bc.runner.runnerID
		inst.LastSeen = time.Now().UnixMilli()
		bc.mu.Lock()
		bc.adhoc[inst.InstanceID] = &adhocInstance{
			Instance: inst,
			proc:     NewPidProcess(inst.PID),
		}
		bc.mu.Unlock()
		bc.runner.reportInstance(inst)
		bc.EnsurePump(inst.InstanceID)
		adopted++
	}
	bc.persistAdhocState()
	if adopted > 0 {
		slog.Info("bridge client: re-adopted ad-hoc instances", "count", adopted)
	}
}

// instanceHealthy probes an instance's health endpoint on localhost.
func instanceHealthy(port int) bool {
	if port <= 0 {
		return false
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/global/health", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
