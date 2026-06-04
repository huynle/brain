package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// ---------------------------------------------------------------------------
// Goal API client tests
//
// Each test stands up an httptest.Server, points an APIClient at it via
// testConfig, exercises one goal client method, and asserts:
//   - correct HTTP method + path (+ query where relevant)
//   - decoded response fields
//   - error path (non-2xx returns an error)
// ---------------------------------------------------------------------------

func TestAPIClient_CreateGoal(t *testing.T) {
	var gotMethod, gotPath string
	var gotReq types.CreateGoalRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(&types.GoalSummary{
			EntryID: "entry-1",
			GoalID:  "ship-it",
			Title:   "Ship It",
			Project: "brain-api",
			Status:  "active",
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	req := types.CreateGoalRequest{
		Project: "brain-api",
		Title:   "Ship It",
		Config:  types.GoalConfig{ID: "ship-it", Criteria: "all tests pass"},
		Action:  types.AutomationAction{Type: "prompt", DirectPrompt: "do work"},
	}
	got, err := client.CreateGoal(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/goals" {
		t.Errorf("path = %q, want /api/v1/goals", gotPath)
	}
	if gotReq.Project != "brain-api" || gotReq.Config.ID != "ship-it" {
		t.Errorf("server received req = %+v", gotReq)
	}
	if got == nil || got.GoalID != "ship-it" || got.EntryID != "entry-1" {
		t.Errorf("result = %+v, want GoalID=ship-it EntryID=entry-1", got)
	}
}

func TestAPIClient_CreateGoal_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	_, err := client.CreateGoal(context.Background(), types.CreateGoalRequest{Project: "p"})
	if err == nil {
		t.Fatal("CreateGoal() error = nil, want error on non-2xx")
	}
}

func TestAPIClient_UpdateGoal(t *testing.T) {
	var gotMethod, gotPath string
	var gotReq types.UpdateGoalRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&types.GoalSummary{
			EntryID: "entry-1",
			GoalID:  "ship-it",
			Title:   "Renamed",
			Status:  "active",
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	newTitle := "Renamed"
	got, err := client.UpdateGoal(context.Background(), "ship-it", types.UpdateGoalRequest{Title: &newTitle})
	if err != nil {
		t.Fatalf("UpdateGoal() error = %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", gotMethod)
	}
	if gotPath != "/api/v1/goals/ship-it" {
		t.Errorf("path = %q, want /api/v1/goals/ship-it", gotPath)
	}
	if gotReq.Title == nil || *gotReq.Title != "Renamed" {
		t.Errorf("server received title = %v, want Renamed", gotReq.Title)
	}
	if got == nil || got.Title != "Renamed" {
		t.Errorf("result = %+v, want Title=Renamed", got)
	}
}

func TestAPIClient_UpdateGoal_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"goal not found"}`))
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	title := "x"
	_, err := client.UpdateGoal(context.Background(), "missing", types.UpdateGoalRequest{Title: &title})
	if err == nil {
		t.Fatal("UpdateGoal() error = nil, want error on non-2xx")
	}
}

func TestAPIClient_ListGoals(t *testing.T) {
	var gotMethod, gotPath, gotProject, gotFeature string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotProject = r.URL.Query().Get("project")
		gotFeature = r.URL.Query().Get("feature_id")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"goals": []types.GoalSummary{
				{EntryID: "e1", GoalID: "g1", Title: "One"},
				{EntryID: "e2", GoalID: "g2", Title: "Two"},
			},
			"count": 2,
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	got, err := client.ListGoals(context.Background(), "brain-api", "feat-1")
	if err != nil {
		t.Fatalf("ListGoals() error = %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/api/v1/goals" {
		t.Errorf("path = %q, want /api/v1/goals", gotPath)
	}
	if gotProject != "brain-api" || gotFeature != "feat-1" {
		t.Errorf("query project=%q feature_id=%q, want brain-api/feat-1", gotProject, gotFeature)
	}
	if len(got) != 2 || got[0].GoalID != "g1" || got[1].GoalID != "g2" {
		t.Errorf("result = %+v, want 2 goals g1,g2", got)
	}
}

func TestAPIClient_ListGoals_NoFilters(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"goals": []types.GoalSummary{}, "count": 0})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	got, err := client.ListGoals(context.Background(), "", "")
	if err != nil {
		t.Fatalf("ListGoals() error = %v", err)
	}
	if gotQuery != "" {
		t.Errorf("query = %q, want empty when no filters", gotQuery)
	}
	if len(got) != 0 {
		t.Errorf("result = %+v, want empty", got)
	}
}

func TestAPIClient_ListGoals_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	_, err := client.ListGoals(context.Background(), "p", "")
	if err == nil {
		t.Fatal("ListGoals() error = nil, want error on non-2xx")
	}
}

func TestAPIClient_RunGoal(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&types.GoalReconcileAudit{
			GoalID:          "ship-it",
			TriggeringEvent: "manual",
			Decision:        types.ReconcileNeedWork,
			Reason:          "needs more work",
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	got, err := client.RunGoal(context.Background(), "ship-it")
	if err != nil {
		t.Fatalf("RunGoal() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/goals/ship-it/run" {
		t.Errorf("path = %q, want /api/v1/goals/ship-it/run", gotPath)
	}
	if got == nil || got.GoalID != "ship-it" || got.Decision != types.ReconcileNeedWork {
		t.Errorf("result = %+v, want GoalID=ship-it Decision=need_work", got)
	}
}

func TestAPIClient_RunGoal_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	_, err := client.RunGoal(context.Background(), "ship-it")
	if err == nil {
		t.Fatal("RunGoal() error = nil, want error on non-2xx")
	}
}

func TestAPIClient_GoalProgress(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&types.GoalProgressResponse{
			GoalID:    "ship-it",
			EntryID:   "entry-1",
			Total:     3,
			Completed: 1,
			Blocked:   1,
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	got, err := client.GoalProgress(context.Background(), "ship-it")
	if err != nil {
		t.Fatalf("GoalProgress() error = %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/api/v1/goals/ship-it/progress" {
		t.Errorf("path = %q, want /api/v1/goals/ship-it/progress", gotPath)
	}
	if got == nil || got.GoalID != "ship-it" || got.Total != 3 || got.Completed != 1 {
		t.Errorf("result = %+v, want GoalID=ship-it Total=3 Completed=1", got)
	}
}

func TestAPIClient_GoalProgress_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	_, err := client.GoalProgress(context.Background(), "missing")
	if err == nil {
		t.Fatal("GoalProgress() error = nil, want error on non-2xx")
	}
}

func TestAPIClient_GoalAudit(t *testing.T) {
	var gotMethod, gotPath, gotLimit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotLimit = r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"audit": []types.GoalReconcileAudit{
				{GoalID: "ship-it", Decision: types.ReconcileComplete},
				{GoalID: "ship-it", Decision: types.ReconcileNeedWork},
			},
			"count": 2,
		})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	got, err := client.GoalAudit(context.Background(), "ship-it", 5)
	if err != nil {
		t.Fatalf("GoalAudit() error = %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/api/v1/goals/ship-it/audit" {
		t.Errorf("path = %q, want /api/v1/goals/ship-it/audit", gotPath)
	}
	if gotLimit != "5" {
		t.Errorf("limit query = %q, want 5", gotLimit)
	}
	if len(got) != 2 || got[0].Decision != types.ReconcileComplete {
		t.Errorf("result = %+v, want 2 audits", got)
	}
}

func TestAPIClient_GoalAudit_NoLimit(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"audit": []types.GoalReconcileAudit{}, "count": 0})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	got, err := client.GoalAudit(context.Background(), "ship-it", 0)
	if err != nil {
		t.Fatalf("GoalAudit() error = %v", err)
	}
	if gotQuery != "" {
		t.Errorf("query = %q, want empty when limit <= 0", gotQuery)
	}
	if len(got) != 0 {
		t.Errorf("result = %+v, want empty", got)
	}
}

func TestAPIClient_GoalAudit_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	_, err := client.GoalAudit(context.Background(), "ship-it", 10)
	if err == nil {
		t.Fatal("GoalAudit() error = nil, want error on non-2xx")
	}
}
