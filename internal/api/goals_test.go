package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Mock GoalService
// =============================================================================

type mockGoalService struct {
	createFunc   func(ctx context.Context, req types.CreateGoalRequest) (*types.GoalSummary, error)
	updateFunc   func(ctx context.Context, goalID string, req types.UpdateGoalRequest) (*types.GoalSummary, error)
	listFunc     func(ctx context.Context, project, featureID string) ([]types.GoalSummary, error)
	runFunc      func(ctx context.Context, goalID string) (*types.GoalReconcileAudit, error)
	progressFunc func(ctx context.Context, goalID string) (*types.GoalProgressResponse, error)
	auditFunc    func(ctx context.Context, goalID string, limit int) ([]types.GoalReconcileAudit, error)

	// Captured args for assertions.
	lastProject   string
	lastFeatureID string
	lastGoalID    string
	lastLimit     int
}

func (m *mockGoalService) CreateGoal(ctx context.Context, req types.CreateGoalRequest) (*types.GoalSummary, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, req)
	}
	return &types.GoalSummary{EntryID: "entry-1", GoalID: req.Config.ID, Title: req.Title, Status: "active"}, nil
}

func (m *mockGoalService) UpdateGoal(ctx context.Context, goalID string, req types.UpdateGoalRequest) (*types.GoalSummary, error) {
	m.lastGoalID = goalID
	if m.updateFunc != nil {
		return m.updateFunc(ctx, goalID, req)
	}
	return &types.GoalSummary{EntryID: "entry-1", GoalID: goalID, Status: "active"}, nil
}

func (m *mockGoalService) ListGoals(ctx context.Context, project, featureID string) ([]types.GoalSummary, error) {
	m.lastProject = project
	m.lastFeatureID = featureID
	if m.listFunc != nil {
		return m.listFunc(ctx, project, featureID)
	}
	return []types.GoalSummary{{EntryID: "entry-1", GoalID: "g1", Status: "active"}}, nil
}

func (m *mockGoalService) RunGoal(ctx context.Context, goalID string) (*types.GoalReconcileAudit, error) {
	m.lastGoalID = goalID
	if m.runFunc != nil {
		return m.runFunc(ctx, goalID)
	}
	return &types.GoalReconcileAudit{GoalID: goalID, Decision: types.ReconcileNoop, Reason: "ok"}, nil
}

func (m *mockGoalService) GoalProgress(ctx context.Context, goalID string) (*types.GoalProgressResponse, error) {
	m.lastGoalID = goalID
	if m.progressFunc != nil {
		return m.progressFunc(ctx, goalID)
	}
	return &types.GoalProgressResponse{GoalID: goalID, FeatureStatus: "pending"}, nil
}

func (m *mockGoalService) GoalAuditHistory(ctx context.Context, goalID string, limit int) ([]types.GoalReconcileAudit, error) {
	m.lastGoalID = goalID
	m.lastLimit = limit
	if m.auditFunc != nil {
		return m.auditFunc(ctx, goalID, limit)
	}
	return []types.GoalReconcileAudit{{GoalID: goalID, Decision: types.ReconcileComplete}}, nil
}

// =============================================================================
// Helper: create goal test router
// =============================================================================

func newGoalTestRouter() (*chi.Mux, *mockGoalService) {
	gs := &mockGoalService{}
	h := NewHandler(
		&mockBrainService{},
		WithGoalService(gs),
	)
	r := chi.NewRouter()
	r.Get("/goals", h.HandleListGoals)
	r.Post("/goals", h.HandleCreateGoal)
	r.Patch("/goals/{goalId}", h.HandleUpdateGoal)
	r.Post("/goals/{goalId}/run", h.HandleRunGoal)
	r.Get("/goals/{goalId}/progress", h.HandleGoalProgress)
	r.Get("/goals/{goalId}/audit", h.HandleGoalAudit)
	return r, gs
}

// =============================================================================
// POST /goals (create)
// =============================================================================

func TestHandleCreateGoal_Success(t *testing.T) {
	router, _ := newGoalTestRouter()
	srv := httptest.NewServer(router)
	defer srv.Close()

	req := types.CreateGoalRequest{
		Project: "proj-1",
		Title:   "Ship feature",
		Config:  types.GoalConfig{ID: "g1", Criteria: "done"},
		Action:  types.AutomationAction{Type: "prompt", DirectPrompt: "do it"},
	}
	body, _ := json.Marshal(req)
	resp, err := http.Post(srv.URL+"/goals", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /goals failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	var summary types.GoalSummary
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if summary.GoalID != "g1" {
		t.Errorf("goal_id = %q, want %q", summary.GoalID, "g1")
	}
}

func TestHandleCreateGoal_BadJSON(t *testing.T) {
	router, _ := newGoalTestRouter()
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/goals", "application/json", strings.NewReader("{not json"))
	if err != nil {
		t.Fatalf("POST /goals failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandleCreateGoal_NilService(t *testing.T) {
	h := NewHandler(&mockBrainService{}) // no WithGoalService -> nil
	r := chi.NewRouter()
	r.Post("/goals", h.HandleCreateGoal)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/goals", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST /goals failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotImplemented)
	}
}

// =============================================================================
// PATCH /goals/{goalId} (update)
// =============================================================================

func TestHandleUpdateGoal_Success(t *testing.T) {
	router, gs := newGoalTestRouter()
	srv := httptest.NewServer(router)
	defer srv.Close()

	newTitle := "Updated"
	body, _ := json.Marshal(types.UpdateGoalRequest{Title: &newTitle})
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/goals/g1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /goals/g1 failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if gs.lastGoalID != "g1" {
		t.Errorf("goalID passed = %q, want %q", gs.lastGoalID, "g1")
	}
}

func TestHandleUpdateGoal_NotFound(t *testing.T) {
	router, gs := newGoalTestRouter()
	gs.updateFunc = func(ctx context.Context, goalID string, req types.UpdateGoalRequest) (*types.GoalSummary, error) {
		return nil, types.ErrGoalNotFound
	}
	srv := httptest.NewServer(router)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/goals/missing", strings.NewReader("{}"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /goals/missing failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

// =============================================================================
// GET /goals (list)
// =============================================================================

func TestHandleListGoals_Success(t *testing.T) {
	router, gs := newGoalTestRouter()
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/goals?project=proj-1&feature_id=feat-1")
	if err != nil {
		t.Fatalf("GET /goals failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if gs.lastProject != "proj-1" || gs.lastFeatureID != "feat-1" {
		t.Errorf("filters = (%q,%q), want (proj-1,feat-1)", gs.lastProject, gs.lastFeatureID)
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if count, ok := result["count"].(float64); !ok || int(count) != 1 {
		t.Errorf("count = %v, want 1", result["count"])
	}
}

func TestHandleListGoals_NilService(t *testing.T) {
	h := NewHandler(&mockBrainService{})
	r := chi.NewRouter()
	r.Get("/goals", h.HandleListGoals)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/goals")
	if err != nil {
		t.Fatalf("GET /goals failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotImplemented)
	}
}

// =============================================================================
// POST /goals/{goalId}/run
// =============================================================================

func TestHandleRunGoal_Success(t *testing.T) {
	router, gs := newGoalTestRouter()
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/goals/g1/run", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /goals/g1/run failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if gs.lastGoalID != "g1" {
		t.Errorf("goalID passed = %q, want %q", gs.lastGoalID, "g1")
	}
	var audit types.GoalReconcileAudit
	if err := json.NewDecoder(resp.Body).Decode(&audit); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if audit.GoalID != "g1" {
		t.Errorf("audit goal_id = %q, want %q", audit.GoalID, "g1")
	}
}

func TestHandleRunGoal_NotFound(t *testing.T) {
	router, gs := newGoalTestRouter()
	gs.runFunc = func(ctx context.Context, goalID string) (*types.GoalReconcileAudit, error) {
		return nil, types.ErrGoalNotFound
	}
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/goals/missing/run", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /goals/missing/run failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

// =============================================================================
// GET /goals/{goalId}/progress
// =============================================================================

func TestHandleGoalProgress_Success(t *testing.T) {
	router, gs := newGoalTestRouter()
	gs.progressFunc = func(ctx context.Context, goalID string) (*types.GoalProgressResponse, error) {
		return &types.GoalProgressResponse{
			GoalID:        goalID,
			FeatureStatus: "in_progress",
			Total:         3,
			Completed:     1,
		}, nil
	}
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/goals/g1/progress")
	if err != nil {
		t.Fatalf("GET /goals/g1/progress failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var progress types.GoalProgressResponse
	if err := json.NewDecoder(resp.Body).Decode(&progress); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if progress.Total != 3 || progress.Completed != 1 || progress.FeatureStatus != "in_progress" {
		t.Errorf("progress = %+v, want total=3 completed=1 status=in_progress", progress)
	}
}

// =============================================================================
// GET /goals/{goalId}/audit
// =============================================================================

func TestHandleGoalAudit_Success(t *testing.T) {
	router, gs := newGoalTestRouter()
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/goals/g1/audit?limit=25")
	if err != nil {
		t.Fatalf("GET /goals/g1/audit failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if gs.lastLimit != 25 {
		t.Errorf("limit passed = %d, want 25", gs.lastLimit)
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if count, ok := result["count"].(float64); !ok || int(count) != 1 {
		t.Errorf("count = %v, want 1", result["count"])
	}
}

func TestHandleGoalAudit_InvalidLimit(t *testing.T) {
	router, _ := newGoalTestRouter()
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/goals/g1/audit?limit=abc")
	if err != nil {
		t.Fatalf("GET /goals/g1/audit failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// =============================================================================
// Router integration
// =============================================================================

func TestGoalsRoutes_Registered(t *testing.T) {
	gs := &mockGoalService{}
	h := NewHandler(
		&mockBrainService{},
		WithGoalService(gs),
	)

	cfg := testConfig()
	router := NewRouter(cfg, WithHandler(h))
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/goals")
	if err != nil {
		t.Fatalf("GET /api/v1/goals failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		t.Error("GET /api/v1/goals should be registered (got 404)")
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /api/v1/goals status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestGoalsRoutes_NotImplementedWithoutHandler(t *testing.T) {
	cfg := testConfig()
	router := NewRouter(cfg)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/goals")
	if err != nil {
		t.Fatalf("GET /api/v1/goals failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotImplemented)
	}
}
