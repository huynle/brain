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
}

func (s stubAssistantPlanner) Plan(ctx context.Context, req AssistantPlanRequest) (AssistantPlanResponse, error) {
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
