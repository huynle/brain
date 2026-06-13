package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
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
}

func (m *mockBridgeService) DecorateInstances(instances []types.OpencodeInstance) {}

func (m *mockBridgeService) ServeBridge(w http.ResponseWriter, r *http.Request, runnerID string) {}

func (m *mockBridgeService) Connected(runnerID string) bool { return true }

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

func (m *mockBridgeService) lastCall() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.doCalls) == 0 {
		return ""
	}
	return m.doCalls[len(m.doCalls)-1]
}

func newControlTestRouter(mock *mockBridgeService, registry *mockRunnerRegistryService) *chi.Mux {
	opts := []HandlerOption{WithBridgeService(mock)}
	if registry != nil {
		opts = append(opts, WithRunnerRegistryService(registry))
	}
	h := NewHandler(&mockBrainService{}, opts...)
	r := chi.NewRouter()
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
	return r
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
