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
	"syscall"
	"time"

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

	httpClient *http.Client
}

// NewBridgeClient creates a bridge client for the given runner.
func NewBridgeClient(tr *TaskRunner) *BridgeClient {
	return &BridgeClient{
		runner:  tr,
		adhoc:   make(map[string]*adhocInstance),
		pumps:   make(map[string]context.CancelFunc),
		streams: make(map[string]bool),
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

// httpGet performs a bounded GET against a localhost instance port.
func (bc *BridgeClient) httpGet(port int, path string) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(bc.baseContext(),
		time.Duration(bridge.DefaultTimeoutMs)*time.Millisecond)
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
