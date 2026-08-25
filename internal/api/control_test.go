package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// mockBridgeService records proxied calls and returns canned responses.
type mockBridgeService struct {
	mu          sync.Mutex
	doCalls     []string // "METHOD path"
	lastReqBody []byte   // body of the most recent Do() call
	doStatus    int
	doBody      []byte
	doErr       error

	spawnFunc  func(ctx context.Context, runnerID string, spec types.SpawnInstanceSpec) (*types.OpencodeInstance, error)
	killErr    error
	pending    []json.RawMessage
	historyOut []byte
	historyErr error
	abortCalls []string
	abortErr   error

	execCalls  []execCall
	execFunc   func(ctx context.Context, runnerID, execID, command, workdir string, timeoutMs int) error
	execErr    error
	signalCall []execSignalCall
	signalErr  error

	// Exec bookkeeping, mirroring what bridge.Hub records per command.
	notConnected bool
	execOutcome  types.ExecOutcome
	execTracked  bool
	execReleased []string
}

// execCall records one StartExec invocation.
type execCall struct {
	runnerID  string
	execID    string
	command   string
	workdir   string
	timeoutMs int
}

// execSignalCall records one SignalExec invocation.
type execSignalCall struct {
	runnerID string
	execID   string
	signal   string
}

func (m *mockBridgeService) DecorateInstances(instances []types.OpencodeInstance) {}

func (m *mockBridgeService) ServeBridge(w http.ResponseWriter, r *http.Request, runnerID string) {}

func (m *mockBridgeService) Connected(runnerID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.notConnected
}

func (m *mockBridgeService) Do(ctx context.Context, runnerID, instanceID, method, path string, body []byte) (int, []byte, error) {
	m.mu.Lock()
	m.doCalls = append(m.doCalls, method+" "+path)
	m.lastReqBody = body
	m.mu.Unlock()
	if m.doErr != nil {
		return 0, nil, m.doErr
	}
	status := m.doStatus
	if status == 0 {
		status = http.StatusOK
	}
	return status, m.doBody, nil
}

func (m *mockBridgeService) SpawnInstance(ctx context.Context, runnerID string, spec types.SpawnInstanceSpec) (*types.OpencodeInstance, error) {
	if m.spawnFunc != nil {
		return m.spawnFunc(ctx, runnerID, spec)
	}
	return &types.OpencodeInstance{
		InstanceID: "inst_new",
		RunnerID:   runnerID,
		Kind:       types.InstanceKindAdhoc,
		Workdir:    spec.Workdir,
		Status:     types.InstanceStatusIdle,
	}, nil
}

func (m *mockBridgeService) KillInstance(ctx context.Context, runnerID, instanceID string) error {
	return m.killErr
}

func (m *mockBridgeService) AbortTask(ctx context.Context, runnerID, taskID string) error {
	m.mu.Lock()
	m.abortCalls = append(m.abortCalls, runnerID+":"+taskID)
	m.mu.Unlock()
	return m.abortErr
}

func (m *mockBridgeService) FetchHistory(ctx context.Context, runnerID, sessionID string) ([]byte, error) {
	if m.historyErr != nil {
		return nil, m.historyErr
	}
	return m.historyOut, nil
}

func (m *mockBridgeService) AcquireStream(runnerID, instanceID string) (func(), error) {
	return func() {}, nil
}

func (m *mockBridgeService) PendingPermissions(runnerID, instanceID string) []json.RawMessage {
	return m.pending
}

func (m *mockBridgeService) StartExec(ctx context.Context, runnerID, execID, command, workdir string, timeoutMs int) error {
	m.mu.Lock()
	m.execCalls = append(m.execCalls, execCall{
		runnerID: runnerID, execID: execID, command: command,
		workdir: workdir, timeoutMs: timeoutMs,
	})
	fn, err := m.execFunc, m.execErr
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, runnerID, execID, command, workdir, timeoutMs)
	}
	return err
}

func (m *mockBridgeService) SignalExec(ctx context.Context, runnerID, execID, signal string) error {
	m.mu.Lock()
	m.signalCall = append(m.signalCall, execSignalCall{runnerID: runnerID, execID: execID, signal: signal})
	err := m.signalErr
	m.mu.Unlock()
	return err
}

func (m *mockBridgeService) ExecOutcome(runnerID, execID string) (types.ExecOutcome, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.execOutcome, m.execTracked
}

func (m *mockBridgeService) ReleaseExec(runnerID, execID string) {
	m.mu.Lock()
	m.execReleased = append(m.execReleased, execID)
	m.execTracked = false
	m.mu.Unlock()
}

// fanOutExec mirrors what bridge.Hub does with a real runner's frames: fan
// each chunk out to the exec topic, count what the fan-out could not deliver,
// and record the terminal outcome BEFORE publishing it (the record is what
// lets the handler end a stream whose exec_exit was dropped).
func (m *mockBridgeService) fanOutExec(
	hub *realtime.Hub, runnerID, execID string,
	chunks []string, exitCode int, exitErr string, publishExit bool,
) {
	topic := realtime.ExecTopic(runnerID, execID)

	m.mu.Lock()
	m.execTracked = true
	m.mu.Unlock()

	for _, chunk := range chunks {
		_, dropped := hub.PublishTracked(topic, "exec_data", map[string]any{
			"exec_id": execID, "stream": "stdout", "chunk": chunk,
		})
		if dropped > 0 {
			m.mu.Lock()
			m.execOutcome.DroppedChunks++
			m.execOutcome.DroppedBytes += len(chunk)
			m.mu.Unlock()
		}
	}

	m.mu.Lock()
	m.execOutcome.Done = true
	m.execOutcome.ExitCode = exitCode
	m.execOutcome.Error = exitErr
	m.mu.Unlock()

	if publishExit {
		hub.Publish(topic, "exec_exit", map[string]any{
			"exec_id": execID, "exit_code": exitCode, "error": exitErr,
		})
	}
}

// releasedExecs returns the exec ids the handler released.
func (m *mockBridgeService) releasedExecs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.execReleased...)
}

// lastExec returns the most recent StartExec call, or false if none.
func (m *mockBridgeService) lastExec() (execCall, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.execCalls) == 0 {
		return execCall{}, false
	}
	return m.execCalls[len(m.execCalls)-1], true
}

// signals returns a copy of the recorded SignalExec calls.
func (m *mockBridgeService) signals() []execSignalCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]execSignalCall(nil), m.signalCall...)
}

func (m *mockBridgeService) lastCall() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.doCalls) == 0 {
		return ""
	}
	return m.doCalls[len(m.doCalls)-1]
}

func newControlTestRouter(mock *mockBridgeService, registry *mockRunnerRegistryService) *chi.Mux {
	router, _ := newControlTestRouterWithHub(mock, registry)
	return router
}

// newControlTestRouterWithHub also returns the realtime hub the handler
// subscribes on, so streaming tests can publish into an exec topic.
func newControlTestRouterWithHub(mock *mockBridgeService, registry *mockRunnerRegistryService) (*chi.Mux, *realtime.Hub) {
	hub := realtime.NewHub()
	opts := []HandlerOption{WithBridgeService(mock), WithHub(hub)}
	if registry != nil {
		opts = append(opts, WithRunnerRegistryService(registry))
	}
	h := NewHandler(&mockBrainService{}, opts...)
	r := chi.NewRouter()
	r.Post("/control/runners/{runnerId}/tasks/{taskId}/abort", h.HandleControlAbortTask)
	r.Post("/control/runners/{runnerId}/exec", h.HandleControlExec)
	r.Post("/control/runners/{runnerId}/exec/{execId}/signal", h.HandleControlExecSignal)
	r.Route("/control/runners/{runnerId}/instances", func(r chi.Router) {
		r.Post("/", h.HandleControlSpawn)
		r.Route("/{instanceId}", func(r chi.Router) {
			r.Delete("/", h.HandleControlKill)
			r.Get("/sessions", h.HandleControlListSessions)
			r.Post("/sessions", h.HandleControlCreateSession)
			r.Get("/sessions/{sessionId}/messages", h.HandleControlListMessages)
			r.Post("/sessions/{sessionId}/prompt", h.HandleControlPrompt)
			r.Post("/sessions/{sessionId}/abort", h.HandleControlAbort)
			r.Post("/sessions/{sessionId}/permissions/{permissionId}", h.HandleControlPermission)
			r.Get("/permissions", h.HandleControlPendingPermissions)
		})
	})
	return r, hub
}

func TestHandleControlAbortTask(t *testing.T) {
	mock := &mockBridgeService{}
	router := newControlTestRouter(mock, nil)

	req := httptest.NewRequest(http.MethodPost, "/control/runners/runner-1/tasks/task-1/abort", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("abort task status = %d body=%s", rec.Code, rec.Body.String())
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.abortCalls) != 1 || mock.abortCalls[0] != "runner-1:task-1" {
		t.Fatalf("abort calls = %v, want [runner-1:task-1]", mock.abortCalls)
	}
}

func TestControlProxy_SessionsAndMessages(t *testing.T) {
	mock := &mockBridgeService{doBody: []byte(`[{"id":"ses_1"}]`)}
	router := newControlTestRouter(mock, nil)

	req := httptest.NewRequest(http.MethodGet, "/control/runners/r1/instances/i1/sessions", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ses_1") {
		t.Fatalf("sessions: got %d %s", rec.Code, rec.Body.String())
	}
	if mock.lastCall() != "GET /session" {
		t.Errorf("proxied call = %q, want GET /session", mock.lastCall())
	}

	req = httptest.NewRequest(http.MethodGet, "/control/runners/r1/instances/i1/sessions/ses_1/messages?limit=50", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("messages: got %d", rec.Code)
	}
	if mock.lastCall() != "GET /session/ses_1/message?limit=50" {
		t.Errorf("proxied call = %q", mock.lastCall())
	}
}

func TestControlPrompt_TranslatesBody(t *testing.T) {
	mock := &mockBridgeService{doStatus: http.StatusNoContent}
	router := newControlTestRouter(mock, nil)

	body := `{"text":"do the thing","agent":"build","model":{"providerID":"anthropic","modelID":"claude-fable-5"}}`
	req := httptest.NewRequest(http.MethodPost, "/control/runners/r1/instances/i1/sessions/ses_1/prompt", strings.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if mock.lastCall() != "POST /session/ses_1/prompt_async" {
		t.Errorf("proxied call = %q", mock.lastCall())
	}

	// Empty text AND no files is rejected before hitting the bridge.
	req = httptest.NewRequest(http.MethodPost, "/control/runners/r1/instances/i1/sessions/ses_1/prompt", strings.NewReader(`{"text":"  "}`))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
		t.Errorf("expected validation error, got %d", rec.Code)
	}
}

func TestControlPrompt_WithImageAttachment(t *testing.T) {
	mock := &mockBridgeService{doStatus: http.StatusNoContent}
	router := newControlTestRouter(mock, nil)

	// A pasted image arrives as a data: URL with no text — must be accepted
	// and forwarded as a file part alongside any text.
	dataURL := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAAAAAA="
	body := `{"text":"what is this?","files":[{"mime":"image/png","url":"` + dataURL + `","filename":"shot.png"}]}`
	req := httptest.NewRequest(http.MethodPost, "/control/runners/r1/instances/i1/sessions/ses_1/prompt", strings.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	var upstream struct {
		Parts []map[string]interface{} `json:"parts"`
	}
	if err := json.Unmarshal(mock.lastReqBody, &upstream); err != nil {
		t.Fatalf("decode forwarded body: %v (%s)", err, mock.lastReqBody)
	}
	if len(upstream.Parts) != 2 {
		t.Fatalf("expected text + file parts, got %d: %+v", len(upstream.Parts), upstream.Parts)
	}
	if upstream.Parts[0]["type"] != "text" || upstream.Parts[0]["text"] != "what is this?" {
		t.Errorf("first part should be the text: %+v", upstream.Parts[0])
	}
	file := upstream.Parts[1]
	if file["type"] != "file" || file["mime"] != "image/png" || file["url"] != dataURL || file["filename"] != "shot.png" {
		t.Errorf("file part not forwarded correctly: %+v", file)
	}
}

func TestControlPrompt_ImageOnlyNoText(t *testing.T) {
	mock := &mockBridgeService{doStatus: http.StatusNoContent}
	router := newControlTestRouter(mock, nil)

	body := `{"text":"","files":[{"mime":"image/jpeg","url":"data:image/jpeg;base64,AAAA"}]}`
	req := httptest.NewRequest(http.MethodPost, "/control/runners/r1/instances/i1/sessions/ses_1/prompt", strings.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("image-only prompt should be accepted, got %d: %s", rec.Code, rec.Body.String())
	}
	var upstream struct {
		Parts []map[string]interface{} `json:"parts"`
	}
	json.Unmarshal(mock.lastReqBody, &upstream)
	if len(upstream.Parts) != 1 || upstream.Parts[0]["type"] != "file" {
		t.Errorf("expected a single file part, got %+v", upstream.Parts)
	}
}

func TestControlBridgeErrors(t *testing.T) {
	mock := &mockBridgeService{doErr: errContains("runner bridge not connected")}
	router := newControlTestRouter(mock, nil)

	req := httptest.NewRequest(http.MethodGet, "/control/runners/r1/instances/i1/sessions", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502 for disconnected runner, got %d", rec.Code)
	}
}

type stringError string

func (e stringError) Error() string { return string(e) }

func errContains(s string) error { return stringError(s) }

func TestControlSpawn(t *testing.T) {
	var gotSpec types.SpawnInstanceSpec
	mock := &mockBridgeService{
		spawnFunc: func(ctx context.Context, runnerID string, spec types.SpawnInstanceSpec) (*types.OpencodeInstance, error) {
			gotSpec = spec
			return &types.OpencodeInstance{
				InstanceID: "inst_new", RunnerID: runnerID,
				Kind: types.InstanceKindAdhoc, Workdir: spec.Workdir,
			}, nil
		},
	}
	var upserted *types.OpencodeInstance
	registry := &mockRunnerRegistryService{
		upsertInstanceFunc: func(ctx context.Context, runnerID string, inst types.OpencodeInstance) error {
			upserted = &inst
			return nil
		},
	}
	router := newControlTestRouter(mock, registry)

	req := httptest.NewRequest(http.MethodPost, "/control/runners/r1/instances",
		strings.NewReader(`{"workdir":"/home/user/proj","agent":"build","title":"hack"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotSpec.Workdir != "/home/user/proj" || gotSpec.Agent != "build" {
		t.Errorf("spec not passed through: %+v", gotSpec)
	}
	if upserted == nil || upserted.InstanceID != "inst_new" {
		t.Error("spawned instance was not upserted into the registry")
	}

	// Relative workdir rejected before hitting the bridge.
	req = httptest.NewRequest(http.MethodPost, "/control/runners/r1/instances",
		strings.NewReader(`{"workdir":"relative/path"}`))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code == http.StatusCreated {
		t.Error("expected relative workdir to be rejected")
	}
}

func TestControlKill_RefusesTaskInstances(t *testing.T) {
	mock := &mockBridgeService{}
	registry := &mockRunnerRegistryService{
		getInstanceFunc: func(ctx context.Context, runnerID, instanceID string) (*types.OpencodeInstance, error) {
			return &types.OpencodeInstance{
				InstanceID: instanceID, RunnerID: runnerID, Kind: types.InstanceKindTask,
			}, nil
		},
	}
	router := newControlTestRouter(mock, registry)

	req := httptest.NewRequest(http.MethodDelete, "/control/runners/r1/instances/inst_task", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409 for task instance kill, got %d", rec.Code)
	}
}

// recordingEventService captures Ingest calls and validates event types the
// way the real EventService does, so this catches unregistered control.*
// types (which the mock bridge tests can't surface).
type recordingEventService struct {
	mu        sync.Mutex
	ingested  []types.Event
	rejectErr error
}

func (s *recordingEventService) Ingest(_ context.Context, events []types.Event) error {
	for _, e := range events {
		if !types.IsValidEventType(e.Type) {
			s.rejectErr = errContains("invalid event type " + e.Type)
			return s.rejectErr
		}
	}
	s.mu.Lock()
	s.ingested = append(s.ingested, events...)
	s.mu.Unlock()
	return nil
}
func (s *recordingEventService) Recent(_ context.Context, _ int, _ map[string]string) ([]types.Event, error) {
	return nil, nil
}
func (s *recordingEventService) Subscribe(_ context.Context, _ map[string]string) (<-chan types.Event, func()) {
	ch := make(chan types.Event)
	return ch, func() {}
}
func (s *recordingEventService) CheckFeatureCompletion(_ context.Context, _, _, _ string) {}

func TestControlAudit_EmitsValidEvents(t *testing.T) {
	mock := &mockBridgeService{doStatus: http.StatusNoContent}
	events := &recordingEventService{}
	h := NewHandler(&mockBrainService{}, WithBridgeService(mock), WithEventService(events))
	r := chi.NewRouter()
	r.Post(
		"/control/runners/{runnerId}/instances/{instanceId}/sessions/{sessionId}/prompt",
		h.HandleControlPrompt,
	)

	req := httptest.NewRequest(http.MethodPost,
		"/control/runners/r1/instances/i1/sessions/ses_1/prompt",
		strings.NewReader(`{"text":"hi"}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if events.rejectErr != nil {
		t.Fatalf("audit event rejected by EventService: %v", events.rejectErr)
	}
	if len(events.ingested) != 1 || events.ingested[0].Type != types.EventControlPromptSent {
		t.Fatalf("expected one control.prompt_sent event, got %+v", events.ingested)
	}
	if events.ingested[0].Source != types.EventSourceAPI {
		t.Errorf("audit event source = %q, want %q", events.ingested[0].Source, types.EventSourceAPI)
	}
}

func TestControlPendingPermissions(t *testing.T) {
	mock := &mockBridgeService{
		pending: []json.RawMessage{json.RawMessage(`{"type":"permission.updated","properties":{"id":"perm_1"}}`)},
	}
	router := newControlTestRouter(mock, nil)

	req := httptest.NewRequest(http.MethodGet, "/control/runners/r1/instances/i1/permissions", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp struct {
		Total int `json:"total"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Total != 1 {
		t.Errorf("expected 1 pending permission, got %d", resp.Total)
	}
}

// ─── Runner shell ────────────────────────────────────────────────

func TestControlExec_RejectsEmptyCommand(t *testing.T) {
	mock := &mockBridgeService{}
	router := newControlTestRouter(mock, nil)

	for _, body := range []string{`{"command":""}`, `{"command":"   "}`, `{}`} {
		req := httptest.NewRequest(http.MethodPost, "/control/runners/r1/exec", strings.NewReader(body))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: expected 400, got %d (%s)", body, rec.Code, rec.Body.String())
		}
	}
	if calls, ok := mock.lastExec(); ok {
		t.Fatalf("bridge should not be reached for an empty command, got %+v", calls)
	}
}

func TestControlExec_BridgeErrorBeforeStream(t *testing.T) {
	mock := &mockBridgeService{execErr: errContains("runner bridge not connected")}
	router := newControlTestRouter(mock, nil)

	req := httptest.NewRequest(http.MethodPost, "/control/runners/r1/exec",
		strings.NewReader(`{"command":"echo hi"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// No SSE has been written yet, so this is a normal JSON error.
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for disconnected runner, got %d (%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); strings.Contains(ct, "event-stream") {
		t.Errorf("error response should not be an SSE stream, got Content-Type %q", ct)
	}
}

func TestControlExec_StreamsOutputAndEndsOnExit(t *testing.T) {
	mock := &mockBridgeService{}
	router, hub := newControlTestRouterWithHub(mock, nil)

	// Publish synchronously from inside StartExec: this is exactly the race
	// the handler guards against by subscribing first. If the subscription
	// happened after StartExec, these would be dropped and the stream would
	// hang until the client timeout.
	mock.execFunc = func(_ context.Context, runnerID, execID, _, _ string, _ int) error {
		topic := realtime.ExecTopic(runnerID, execID)
		hub.Publish(topic, "exec_data", map[string]any{
			"exec_id": execID, "stream": "stdout", "chunk": "hello\n",
		})
		hub.Publish(topic, "exec_exit", map[string]any{
			"exec_id": execID, "exit_code": 0, "error": "",
		})
		return nil
	}

	srv := httptest.NewServer(router)
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(srv.URL+"/control/runners/r1/exec", "application/json",
		strings.NewReader(`{"command":"echo hello","workdir":"/tmp","timeout_ms":1000}`))
	if err != nil {
		t.Fatalf("exec request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	// exec_exit must terminate the stream, so ReadAll returns rather than
	// blocking until the client timeout.
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("stream did not terminate on exec_exit: %v", err)
	}
	body := string(raw)

	call, ok := mock.lastExec()
	if !ok {
		t.Fatal("StartExec was never called")
	}
	if call.command != "echo hello" || call.workdir != "/tmp" || call.timeoutMs != 1000 {
		t.Errorf("StartExec args not passed through: %+v", call)
	}
	if call.execID == "" || !strings.HasPrefix(call.execID, "exec_") {
		t.Errorf("exec id = %q, want a server-generated exec_* id", call.execID)
	}
	for _, want := range []string{
		"event: started",
		`"exec_id":"` + call.execID + `"`,
		"event: exec_data",
		"hello",
		"event: exec_exit",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("stream missing %q:\n%s", want, body)
		}
	}
}

// execStreamBody runs one command against srv and returns the whole SSE
// stream. The client timeout is deliberately short: a regression that leaves
// the stream open must fail this test quickly, not hang CI.
func execStreamBody(t *testing.T, srv *httptest.Server, body string) string {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(srv.URL+"/control/runners/r1/exec", "application/json",
		strings.NewReader(body))
	if err != nil {
		t.Fatalf("exec request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("exec stream never terminated: %v", err)
	}
	return string(raw)
}

// A command that outpaces the browser must not lose output. The fan-out is
// non-blocking, so every chunk published while the handler is still inside
// StartExec has to fit in the subscription's buffer — with the shared default
// (64) a multi-megabyte command arrived as a few hundred bytes AND lost its
// terminal event, wedging the terminal.
//
// The volume is the one from the original bug report — 3 MB of output, which
// at bridge.ExecChunkBytes is 192 chunks — published before the handler reads
// a single message. It is deliberately a FIXED number rather than a function
// of execStreamBuffer: a volume derived from the constant under test shrinks
// along with it and passes trivially at any buffer size, which is no test at
// all. The guard below keeps the two honest in the other direction.
// (Over-budget loss is a different contract —
// TestControlExec_ReportsDroppedOutputExplicitly covers it.)
func TestControlExec_HighVolumeOutputIsNotSilentlyTruncated(t *testing.T) {
	mock := &mockBridgeService{}
	router, hub := newControlTestRouterWithHub(mock, nil)

	const chunks = 3 * 1024 * 1024 / execStreamChunkBytes // 192
	if chunks > execStreamBuffer {
		t.Fatalf("the exec backlog budget (%d chunks) no longer covers the %d-chunk "+
			"case this feature was reported broken on", execStreamBuffer, chunks)
	}
	lines := make([]string, chunks)
	for i := range lines {
		lines[i] = fmt.Sprintf("line-%d\n", i)
	}
	mock.execFunc = func(_ context.Context, runnerID, execID, _, _ string, _ int) error {
		mock.fanOutExec(hub, runnerID, execID, lines, 0, "", true)
		return nil
	}

	srv := httptest.NewServer(router)
	defer srv.Close()
	body := execStreamBody(t, srv, `{"command":"seq 100000"}`)

	if !strings.Contains(body, "event: exec_exit") {
		t.Fatal("stream ended without an exec_exit event")
	}
	if got := strings.Count(body, "event: exec_data"); got != chunks {
		t.Errorf("delivered %d of %d output chunks", got, chunks)
	}
	for _, want := range []string{"line-0", fmt.Sprintf("line-%d", chunks-1)} {
		if !strings.Contains(body, want) {
			t.Errorf("output missing %q", want)
		}
	}
	if strings.Contains(body, "event: exec_truncated") {
		t.Error("nothing should have been dropped at this volume")
	}
	if rel := mock.releasedExecs(); len(rel) != 1 {
		t.Errorf("per-exec bridge state released %d times, want once", len(rel))
	}
}

// Beyond what any buffer can absorb, loss is unavoidable — but it must be
// announced. A transcript that is silently missing output is worse than a
// slow one, because the user cannot tell.
func TestControlExec_ReportsDroppedOutputExplicitly(t *testing.T) {
	mock := &mockBridgeService{}
	router, hub := newControlTestRouterWithHub(mock, nil)

	const chunks = execStreamBuffer + 500
	lines := make([]string, chunks)
	for i := range lines {
		lines[i] = fmt.Sprintf("line-%d\n", i)
	}
	mock.execFunc = func(_ context.Context, runnerID, execID, _, _ string, _ int) error {
		mock.fanOutExec(hub, runnerID, execID, lines, 0, "", true)
		return nil
	}

	srv := httptest.NewServer(router)
	defer srv.Close()
	body := execStreamBody(t, srv, `{"command":"cat huge"}`)

	if !strings.Contains(body, "event: exec_truncated") {
		t.Fatalf("dropped output was not reported to the client:\n%s", tail(body))
	}
	if !strings.Contains(body, `"dropped_chunks":`) || !strings.Contains(body, `"dropped_bytes":`) {
		t.Error("truncation marker carries no counts")
	}
	if !strings.Contains(body, "event: exec_exit") {
		t.Fatal("stream ended without an exec_exit event")
	}
	// The marker is useless after the client has stopped reading at the exit.
	if strings.Index(body, "event: exec_truncated") > strings.LastIndex(body, "event: exec_exit") {
		t.Error("truncation marker came after the terminal event")
	}
}

// The terminal event rides the same droppable fan-out as the output. When it
// is lost the handler must still end the stream — and only after everything
// buffered ahead of it has been written.
func TestControlExec_TerminatesWhenExitEventIsLost(t *testing.T) {
	mock := &mockBridgeService{}
	router, hub := newControlTestRouterWithHub(mock, nil)

	mock.execFunc = func(_ context.Context, runnerID, execID, _, _ string, _ int) error {
		// publishExit=false: the runner reported an exit, the fan-out ate it.
		mock.fanOutExec(hub, runnerID, execID, []string{"first\n", "last\n"}, 3, "", false)
		return nil
	}

	srv := httptest.NewServer(router)
	defer srv.Close()
	body := execStreamBody(t, srv, `{"command":"exit 3"}`)

	if !strings.Contains(body, "event: exec_exit") {
		t.Fatalf("stream never terminated after a lost exec_exit:\n%s", tail(body))
	}
	if !strings.Contains(body, `"exit_code":3`) {
		t.Errorf("synthesised exit did not carry the recorded exit code:\n%s", tail(body))
	}
	exit := strings.Index(body, "event: exec_exit")
	if last := strings.Index(body, "last"); last == -1 || last > exit {
		t.Error("exec_exit overtook output that was still buffered")
	}
}

// A SIGKILLed runner never sends an exec_exit at all. Without a fallback the
// handler heartbeats forever, leaking a goroutine and an HTTP connection per
// stuck command while the terminal stays stuck in "running".
func TestControlExec_TerminatesWhenRunnerDisconnects(t *testing.T) {
	mock := &mockBridgeService{}
	router, hub := newControlTestRouterWithHub(mock, nil)

	mock.execFunc = func(_ context.Context, runnerID, execID, _, _ string, _ int) error {
		hub.Publish(realtime.ExecTopic(runnerID, execID), "exec_data", map[string]any{
			"exec_id": execID, "stream": "stdout", "chunk": "partial\n",
		})
		// The runner dies mid-command: no outcome is ever recorded.
		mock.mu.Lock()
		mock.notConnected = true
		mock.mu.Unlock()
		return nil
	}

	srv := httptest.NewServer(router)
	defer srv.Close()
	body := execStreamBody(t, srv, `{"command":"sleep 300"}`)

	if !strings.Contains(body, "event: exec_exit") {
		t.Fatalf("stream hung after the runner disconnected:\n%s", tail(body))
	}
	if !strings.Contains(body, "runner disconnected") {
		t.Errorf("terminal event does not explain why the command ended:\n%s", tail(body))
	}
	if !strings.Contains(body, "partial") {
		t.Error("output received before the disconnect was dropped")
	}
}

// tail trims a stream dump to something readable in a failure message.
func tail(body string) string {
	const max = 600
	if len(body) <= max {
		return body
	}
	return "…" + body[len(body)-max:]
}

func TestControlExec_SignalsRunnerOnClientDisconnect(t *testing.T) {
	mock := &mockBridgeService{}
	router, _ := newControlTestRouterWithHub(mock, nil)

	// The command never exits on its own; only the disconnect ends it.
	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/control/runners/r1/exec",
		strings.NewReader(`{"command":"sleep 300"}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("exec request: %v", err)
	}

	// Wait for the stream to open before hanging up.
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil || !strings.Contains(line, "started") {
		cancel()
		_ = resp.Body.Close()
		t.Fatalf("first SSE line = %q err=%v, want the started event", line, err)
	}

	cancel()
	_ = resp.Body.Close()

	deadline := time.Now().Add(2 * time.Second)
	for {
		sigs := mock.signals()
		if len(sigs) > 0 {
			if sigs[0].signal != "term" {
				t.Errorf("disconnect signal = %q, want term", sigs[0].signal)
			}
			if _, ok := mock.lastExec(); ok && sigs[0].execID == "" {
				t.Error("signal carried no exec id")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("abandoned command was never signalled on client disconnect")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestControlExec_AuditEvent(t *testing.T) {
	mock := &mockBridgeService{}
	events := &recordingEventService{}
	hub := realtime.NewHub()
	h := NewHandler(&mockBrainService{},
		WithBridgeService(mock), WithHub(hub), WithEventService(events))
	mock.execFunc = func(_ context.Context, runnerID, execID, _, _ string, _ int) error {
		hub.Publish(realtime.ExecTopic(runnerID, execID), "exec_exit", map[string]any{
			"exec_id": execID, "exit_code": 0,
		})
		return nil
	}

	r := chi.NewRouter()
	r.Post("/control/runners/{runnerId}/exec", h.HandleControlExec)
	srv := httptest.NewServer(r)
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(srv.URL+"/control/runners/r1/exec", "application/json",
		strings.NewReader(`{"command":"ls -la"}`))
	if err != nil {
		t.Fatalf("exec request: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	if events.rejectErr != nil {
		t.Fatalf("audit event rejected by EventService: %v", events.rejectErr)
	}
	events.mu.Lock()
	defer events.mu.Unlock()
	if len(events.ingested) != 1 || events.ingested[0].Type != types.EventControlExecStarted {
		t.Fatalf("expected one control.exec_started event, got %+v", events.ingested)
	}
	if got := events.ingested[0].Metadata["command"]; got != "ls -la" {
		t.Errorf("audit command = %q, want %q", got, "ls -la")
	}
	if events.ingested[0].RunnerID != "r1" {
		t.Errorf("audit runner_id = %q, want r1", events.ingested[0].RunnerID)
	}
}

func TestControlExecSignal(t *testing.T) {
	mock := &mockBridgeService{}
	router := newControlTestRouter(mock, nil)

	req := httptest.NewRequest(http.MethodPost, "/control/runners/r1/exec/exec_abc/signal",
		strings.NewReader(`{"signal":"int"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	sigs := mock.signals()
	if len(sigs) != 1 || sigs[0] != (execSignalCall{runnerID: "r1", execID: "exec_abc", signal: "int"}) {
		t.Fatalf("signal calls = %+v", sigs)
	}
}

func TestControlExecSignal_RejectsUnknownSignal(t *testing.T) {
	mock := &mockBridgeService{}
	router := newControlTestRouter(mock, nil)

	for _, body := range []string{`{"signal":"hup"}`, `{"signal":""}`, `{}`} {
		req := httptest.NewRequest(http.MethodPost, "/control/runners/r1/exec/exec_abc/signal",
			strings.NewReader(body))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: expected 400, got %d (%s)", body, rec.Code, rec.Body.String())
		}
	}
	if sigs := mock.signals(); len(sigs) != 0 {
		t.Fatalf("bridge should not be reached for an invalid signal, got %+v", sigs)
	}
}

func TestControlExecSignal_BridgeError(t *testing.T) {
	mock := &mockBridgeService{signalErr: errContains("runner bridge not connected")}
	router := newControlTestRouter(mock, nil)

	req := httptest.NewRequest(http.MethodPost, "/control/runners/r1/exec/exec_abc/signal",
		strings.NewReader(`{"signal":"kill"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// Coverage satisfies EventService. Returns a plausible full-ring window so the
// renderer's coverage line is exercised rather than skipped in tests.
func (m *recordingEventService) Coverage() types.EventCoverage {
	return types.EventCoverage{Buffered: 10, Capacity: 1000}
}
