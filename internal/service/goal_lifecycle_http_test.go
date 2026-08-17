package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Goal lifecycle through the HTTP handlers
//
// These tests run the REAL GoalService behind the REAL api.Handler goal
// endpoints (no mocks), proving the full pause -> resume -> run round-trip
// works over HTTP. This is the regression surface for the old lifecycle bug:
// findGoalByID only saw ACTIVE goals, so a paused (status=blocked) goal
// 404'd on every endpoint and could never be resumed.
// =============================================================================

// newGoalLifecycleServer wires a real brain + goal service into the goal HTTP
// handlers and returns the test server.
func newGoalLifecycleServer(t *testing.T) (*httptest.Server, *GoalService) {
	t.Helper()
	brain, store, _ := newTestBrainService(t)
	goalSvc := NewGoalService(brain, &goalScopeTaskLister{}, store)

	h := api.NewHandler(brain, api.WithGoalService(goalSvc))
	r := chi.NewRouter()
	r.Get("/goals", h.HandleListGoals)
	r.Post("/goals", h.HandleCreateGoal)
	r.Patch("/goals/{goalId}", h.HandleUpdateGoal)
	r.Delete("/goals/{goalId}", h.HandleDeleteGoal)
	r.Post("/goals/{goalId}/run", h.HandleRunGoal)
	r.Get("/goals/{goalId}/progress", h.HandleGoalProgress)
	r.Get("/goals/{goalId}/audit", h.HandleGoalAudit)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, goalSvc
}

// doJSON performs a JSON request and decodes the response into out (when
// non-nil), returning the status code.
func doJSON(t *testing.T, method, url string, body any, out any) int {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s %s response: %v", method, url, err)
		}
	}
	return resp.StatusCode
}

func TestGoalLifecycle_PauseResumeRun_HTTP(t *testing.T) {
	srv, _ := newGoalLifecycleServer(t)

	// Create (server generates the goal id from the title).
	var created types.GoalSummary
	status := doJSON(t, http.MethodPost, srv.URL+"/goals", types.CreateGoalRequest{
		Project: "proj",
		Title:   "Ship OAuth Login!",
		Config:  types.GoalConfig{Criteria: "login works"},
		Action:  types.AutomationAction{Type: "prompt", DirectPrompt: "do it"},
	}, &created)
	if status != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", status)
	}
	if created.GoalID != "ship-oauth-login" {
		t.Fatalf("generated goal id = %q, want ship-oauth-login", created.GoalID)
	}
	goalURL := srv.URL + "/goals/" + created.GoalID

	// Pause (status -> blocked).
	blocked := "blocked"
	var paused types.GoalSummary
	if status := doJSON(t, http.MethodPatch, goalURL, types.UpdateGoalRequest{Status: &blocked}, &paused); status != http.StatusOK {
		t.Fatalf("pause status = %d, want 200", status)
	}
	if paused.Status != "blocked" {
		t.Errorf("paused status = %q, want blocked", paused.Status)
	}

	// The paused goal is still listed (default set includes blocked)...
	var list struct {
		Goals []types.GoalSummary `json:"goals"`
		Count int                 `json:"count"`
	}
	if status := doJSON(t, http.MethodGet, srv.URL+"/goals?project=proj", nil, &list); status != http.StatusOK {
		t.Fatalf("list status = %d, want 200", status)
	}
	if list.Count != 1 || list.Goals[0].Status != "blocked" {
		t.Fatalf("default list = %+v, want the blocked goal visible", list)
	}

	// ...its progress endpoint still resolves it...
	if status := doJSON(t, http.MethodGet, goalURL+"/progress", nil, nil); status != http.StatusOK {
		t.Fatalf("progress on paused goal status = %d, want 200 (was the 404 lifecycle bug)", status)
	}

	// Resume (status -> active). Previously 404: the paused goal was invisible.
	active := "active"
	var resumed types.GoalSummary
	if status := doJSON(t, http.MethodPatch, goalURL, types.UpdateGoalRequest{Status: &active}, &resumed); status != http.StatusOK {
		t.Fatalf("resume status = %d, want 200", status)
	}
	if resumed.Status != "active" {
		t.Errorf("resumed status = %q, want active", resumed.Status)
	}

	// Run a manual reconcile on the resumed goal.
	var audit types.GoalReconcileAudit
	if status := doJSON(t, http.MethodPost, goalURL+"/run", nil, &audit); status != http.StatusOK {
		t.Fatalf("run status = %d, want 200", status)
	}
	if audit.GoalID != created.GoalID {
		t.Errorf("run audit goal id = %q, want %q", audit.GoalID, created.GoalID)
	}
	if audit.Decision != types.ReconcileNeedWork {
		t.Errorf("run decision = %q, want need_work (no linked tasks)", audit.Decision)
	}
}

func TestGoalCreate_ServerSideIDGeneration_HTTP(t *testing.T) {
	srv, _ := newGoalLifecycleServer(t)

	create := func(title string) (int, types.GoalSummary) {
		var summary types.GoalSummary
		status := doJSON(t, http.MethodPost, srv.URL+"/goals", types.CreateGoalRequest{
			Project: "proj",
			Title:   title,
			Action:  types.AutomationAction{Type: "prompt", DirectPrompt: "work"},
		}, &summary)
		return status, summary
	}

	// Slug derivation: lowercase, [a-z0-9-], dashes collapsed.
	status, first := create("Fix   The API!!")
	if status != http.StatusCreated {
		t.Fatalf("first create status = %d, want 201", status)
	}
	if first.GoalID != "fix-the-api" {
		t.Errorf("first goal id = %q, want fix-the-api", first.GoalID)
	}

	// De-dupe with -2/-3 suffixes.
	if _, second := create("Fix The API"); second.GoalID != "fix-the-api-2" {
		t.Errorf("second goal id = %q, want fix-the-api-2", second.GoalID)
	}
	if _, third := create("fix the api"); third.GoalID != "fix-the-api-3" {
		t.Errorf("third goal id = %q, want fix-the-api-3", third.GoalID)
	}

	// 400 only when goal id AND title are both empty.
	status = doJSON(t, http.MethodPost, srv.URL+"/goals", types.CreateGoalRequest{
		Project: "proj",
		Action:  types.AutomationAction{Type: "prompt", DirectPrompt: "work"},
	}, nil)
	if status != http.StatusBadRequest {
		t.Errorf("empty id+title create status = %d, want 400", status)
	}
}

func TestGoalList_StatusFilter_HTTP(t *testing.T) {
	srv, _ := newGoalLifecycleServer(t)

	mk := func(title, wantStatus string) string {
		var created types.GoalSummary
		if status := doJSON(t, http.MethodPost, srv.URL+"/goals", types.CreateGoalRequest{
			Project: "proj",
			Title:   title,
			Action:  types.AutomationAction{Type: "prompt", DirectPrompt: "work"},
		}, &created); status != http.StatusCreated {
			t.Fatalf("create %q status = %d", title, status)
		}
		if wantStatus != "active" {
			s := wantStatus
			if status := doJSON(t, http.MethodPatch, srv.URL+"/goals/"+created.GoalID, types.UpdateGoalRequest{Status: &s}, nil); status != http.StatusOK {
				t.Fatalf("set %q status=%q: %d", title, wantStatus, status)
			}
		}
		return created.GoalID
	}

	mk("goal active", "active")
	mk("goal paused", "blocked")
	mk("goal done", "completed")
	mk("goal gone", "archived")

	fetch := func(query string) []string {
		var list struct {
			Goals []types.GoalSummary `json:"goals"`
		}
		if status := doJSON(t, http.MethodGet, srv.URL+"/goals"+query, nil, &list); status != http.StatusOK {
			t.Fatalf("list %q status = %d", query, status)
		}
		out := make([]string, 0, len(list.Goals))
		for _, g := range list.Goals {
			out = append(out, fmt.Sprintf("%s:%s", g.GoalID, g.Status))
		}
		return out
	}

	// Default: active + blocked + completed, archived hidden.
	got := strings.Join(fetch(""), ",")
	for _, want := range []string{"goal-active:active", "goal-paused:blocked", "goal-done:completed"} {
		if !strings.Contains(got, want) {
			t.Errorf("default list %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "goal-gone") {
		t.Errorf("default list %q must hide archived goals", got)
	}

	// ?status=archived reveals only archived.
	if got := fetch("?status=archived"); len(got) != 1 || got[0] != "goal-gone:archived" {
		t.Errorf("archived list = %v, want [goal-gone:archived]", got)
	}

	// ?status=all shows everything.
	if got := fetch("?status=all"); len(got) != 4 {
		t.Errorf("all list = %v, want 4 goals", got)
	}
}

func TestGoalDelete_HTTP(t *testing.T) {
	srv, _ := newGoalLifecycleServer(t)

	var created types.GoalSummary
	if status := doJSON(t, http.MethodPost, srv.URL+"/goals", types.CreateGoalRequest{
		Project: "proj",
		Title:   "Deletable goal",
		Action:  types.AutomationAction{Type: "prompt", DirectPrompt: "work"},
	}, &created); status != http.StatusCreated {
		t.Fatalf("create status = %d", status)
	}
	goalURL := srv.URL + "/goals/" + created.GoalID

	var deleted struct {
		Success bool   `json:"success"`
		GoalID  string `json:"goal_id"`
	}
	if status := doJSON(t, http.MethodDelete, goalURL, nil, &deleted); status != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", status)
	}
	if !deleted.Success || deleted.GoalID != created.GoalID {
		t.Errorf("delete response = %+v", deleted)
	}

	// Gone: subsequent lookups 404, deletion of unknown goals 404.
	if status := doJSON(t, http.MethodGet, goalURL+"/progress", nil, nil); status != http.StatusNotFound {
		t.Errorf("progress after delete status = %d, want 404", status)
	}
	if status := doJSON(t, http.MethodDelete, goalURL, nil, nil); status != http.StatusNotFound {
		t.Errorf("double delete status = %d, want 404", status)
	}
}
