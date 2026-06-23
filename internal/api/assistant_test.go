package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

type stubAssistantPlanner struct {
	resp AssistantPlanResponse
	err  error
	req  *AssistantPlanRequest
}

func (s stubAssistantPlanner) Plan(ctx context.Context, req AssistantPlanRequest) (AssistantPlanResponse, error) {
	if s.req != nil {
		*s.req = req
	}
	return s.resp, s.err
}

func newAssistantTestRouter(svc *AssistantService) *chi.Mux {
	h := NewHandler(&mockBrainService{}, WithAssistantService(svc))
	r := chi.NewRouter()
	r.Get("/assistant/status", h.HandleAssistantStatus)
	r.Post("/assistant/chat", h.HandleAssistantChat)
	r.Post("/assistant/goal-draft", h.HandleAssistantGoalDraft)
	return r
}

func TestHandleAssistantStatus(t *testing.T) {
	svc := NewAssistantService(AssistantServiceOptions{Enabled: true, Provider: "openrouter", Model: "anthropic/claude"})
	router := newAssistantTestRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/assistant/status", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got AssistantStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Available || got.Mode != "direct_llm" || got.Provider != "openrouter" || got.Model != "anthropic/claude" {
		t.Fatalf("status response = %#v, want direct_llm openrouter anthropic/claude", got)
	}
}

func TestHandleAssistantChat_ExplicitCreateTaskExecutesImmediately(t *testing.T) {
	brain := &mockBrainService{}
	brain.saveFunc = func(ctx context.Context, req types.CreateEntryRequest) (*types.CreateEntryResponse, error) {
		if req.Type != "task" || req.Project != "brain-api" || req.Title != "Fix mobile nav" {
			t.Fatalf("save req = %#v, want task in brain-api titled Fix mobile nav", req)
		}
		return &types.CreateEntryResponse{ID: "abc12345", Path: "projects/brain-api/task/abc12345.md", Title: req.Title, Type: req.Type, Status: "pending"}, nil
	}
	svc := NewAssistantService(AssistantServiceOptions{
		Enabled: true,
		Planner: stubAssistantPlanner{resp: AssistantPlanResponse{
			Reply: "Creating task.",
			Actions: []AssistantAction{{
				Type:     "create_task",
				Explicit: true,
				Payload:  map[string]any{"project": "brain-api", "title": "Fix mobile nav", "content": "Fix the mobile navigation."},
			}},
		}},
		Brain: brain,
	})
	h := NewHandler(brain, WithAssistantService(svc))
	r := chi.NewRouter()
	r.Post("/assistant/chat", h.HandleAssistantChat)

	body := []byte(`{"project":"brain-api","message":"Create a task to fix the mobile nav."}`)
	req := httptest.NewRequest(http.MethodPost, "/assistant/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got AssistantChatResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.ExecutedActions) != 1 || got.ExecutedActions[0].Type != "create_task" || got.ExecutedActions[0].Status != "completed" {
		t.Fatalf("executed_actions = %#v, want completed create_task", got.ExecutedActions)
	}
	if len(got.ProposedActions) != 0 {
		t.Fatalf("proposed_actions = %#v, want none for explicit create", got.ProposedActions)
	}
}

func TestHandleAssistantChat_PassesModelOverrideToPlanner(t *testing.T) {
	var gotReq AssistantPlanRequest
	svc := NewAssistantService(AssistantServiceOptions{
		Enabled: true,
		Planner: stubAssistantPlanner{
			req:  &gotReq,
			resp: AssistantPlanResponse{Reply: "Using selected model."},
		},
	})
	router := newAssistantTestRouter(svc)

	body := []byte(`{"project":"brain-api","message":"Use the selected model.","model":"openai/gpt-4o-mini"}`)
	req := httptest.NewRequest(http.MethodPost, "/assistant/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if gotReq.Model != "openai/gpt-4o-mini" {
		t.Fatalf("planner model = %q, want %q", gotReq.Model, "openai/gpt-4o-mini")
	}
}

func TestOpenRouterAssistantPlanner_UsesRequestModelOverride(t *testing.T) {
	t.Setenv("BRAIN_TEST_OPENROUTER_KEY", "test-key")
	var gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q, want /chat/completions", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Fatalf("authorization = %q, want bearer test key", auth)
		}
		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotModel = payload.Model
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"reply\":\"ok\",\"actions\":[]}"}}]}`))
	}))
	defer srv.Close()

	planner := NewOpenRouterAssistantPlanner("openrouter", srv.URL, "BRAIN_TEST_OPENROUTER_KEY", "anthropic/claude-sonnet-4", 0)
	if _, err := planner.Plan(context.Background(), AssistantPlanRequest{Message: "hello", Model: "openai/gpt-4o-mini"}); err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if gotModel != "openai/gpt-4o-mini" {
		t.Fatalf("request model = %q, want %q", gotModel, "openai/gpt-4o-mini")
	}
}

type streamingAssistantPlanner struct {
	chunks []string
	resp   AssistantPlanResponse
	req    *AssistantPlanRequest
}

func (s streamingAssistantPlanner) Plan(ctx context.Context, req AssistantPlanRequest) (AssistantPlanResponse, error) {
	return s.resp, nil
}

func (s streamingAssistantPlanner) StreamPlan(ctx context.Context, req AssistantPlanRequest, onDelta func(string) error) (AssistantPlanResponse, error) {
	if s.req != nil {
		*s.req = req
	}
	for _, chunk := range s.chunks {
		if err := onDelta(chunk); err != nil {
			return AssistantPlanResponse{}, err
		}
	}
	return s.resp, nil
}

func TestHandleAssistantChatStreamEmitsDeltasThenDone(t *testing.T) {
	svc := NewAssistantService(AssistantServiceOptions{
		Enabled: true,
		Planner: streamingAssistantPlanner{
			chunks: []string{"Hel", "lo"},
			resp:   AssistantPlanResponse{Reply: "Hello", Actions: []AssistantAction{{Type: "create_task", Explicit: false, Payload: map[string]any{"title": "Draft"}}}},
		},
	})
	h := NewHandler(&mockBrainService{}, WithAssistantService(svc))
	r := chi.NewRouter()
	r.Post("/assistant/chat/stream", h.HandleAssistantChatStream)

	body := []byte(`{"project":"brain-api","message":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/assistant/chat/stream", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/x-ndjson" {
		t.Fatalf("content-type = %q, want application/x-ndjson", ct)
	}
	lines := bytes.Split(bytes.TrimSpace(rec.Body.Bytes()), []byte("\n"))
	if len(lines) != 3 {
		t.Fatalf("stream lines = %d (%s), want 3", len(lines), rec.Body.String())
	}
	var first AssistantStreamEvent
	if err := json.Unmarshal(lines[0], &first); err != nil {
		t.Fatalf("decode first event: %v", err)
	}
	if first.Type != "delta" || first.Delta != "Hel" {
		t.Fatalf("first event = %#v, want delta Hel", first)
	}
	var done AssistantStreamEvent
	if err := json.Unmarshal(lines[2], &done); err != nil {
		t.Fatalf("decode done event: %v", err)
	}
	if done.Type != "done" || done.Reply != "Hello" || len(done.ProposedActions) != 1 {
		t.Fatalf("done event = %#v, want done reply with proposed action", done)
	}
}

func TestExtractAssistantReplyPrefixFromPartialJSON(t *testing.T) {
	prefix := extractAssistantReplyPrefix(`{"reply":"Hello\nwor`)
	if prefix != "Hello\nwor" {
		t.Fatalf("prefix = %q, want decoded partial reply", prefix)
	}
	prefix = extractAssistantReplyPrefix(`{"actions":[],"reply":"A \"quoted\" reply"}`)
	if prefix != `A "quoted" reply` {
		t.Fatalf("prefix = %q, want decoded quoted reply", prefix)
	}
}

func TestHandleAssistantGoalDraftReturnsProposedGoalFields(t *testing.T) {
	svc := NewAssistantService(AssistantServiceOptions{
		Enabled: true,
		Planner: stubAssistantPlanner{resp: AssistantPlanResponse{
			Reply: "Drafted goal.",
			Actions: []AssistantAction{{
				Type:     "create_goal",
				Explicit: false,
				Payload:  map[string]any{"project": "brain-api", "title": "Add built-in assistant", "criteria": "Assistant can create Brain objects.", "validation": "go test ./..."},
			}},
		}},
	})
	router := newAssistantTestRouter(svc)

	body := []byte(`{"project":"brain-api","message":"Help me draft a goal for the PWA assistant."}`)
	req := httptest.NewRequest(http.MethodPost, "/assistant/goal-draft", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got AssistantGoalDraftResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Draft.Title != "Add built-in assistant" || got.Draft.Criteria != "Assistant can create Brain objects." || got.Draft.Validation != "go test ./..." {
		t.Fatalf("draft = %#v, want assistant goal fields", got.Draft)
	}
}
