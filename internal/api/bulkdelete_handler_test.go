package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// newBulkDeleteRouter wires the entries routes plus an optional task service,
// which is what supplies the live-claim guard.
func newBulkDeleteRouter(brain *mockBrainService, tasks TaskService) *chi.Mux {
	opts := []HandlerOption{WithHub(realtime.NewHub())}
	if tasks != nil {
		opts = append(opts, WithTaskService(tasks))
	}
	h := NewHandler(brain, opts...)
	r := chi.NewRouter()
	r.Route("/entries", func(r chi.Router) {
		r.Post("/bulk-delete", h.HandleBulkDelete)
		r.Delete("/*", h.HandleDeleteEntry)
	})
	return r
}

func bdPostJSON(t *testing.T, srv *httptest.Server, path string, body any) *http.Response {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

func bdPostRaw(t *testing.T, srv *httptest.Server, path, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

func TestHandleBulkDelete_Success(t *testing.T) {
	var got types.BulkDeleteRequest
	brain := &mockBrainService{
		bulkDeleteFunc: func(_ context.Context, req types.BulkDeleteRequest) (*types.BulkDeleteResponse, error) {
			got = req
			return &types.BulkDeleteResponse{
				Deleted: 2,
				Total:   2,
				Results: []types.BulkUpdateResult{
					{Path: "projects/p/task/a.md", ID: "a", Status: "ok"},
					{Path: "projects/p/task/b.md", ID: "b", Status: "ok"},
				},
			}, nil
		},
		recallFunc: func(_ context.Context, _ string) (*types.BrainEntry, error) {
			return nil, fmt.Errorf("not found")
		},
	}
	srv := httptest.NewServer(newBulkDeleteRouter(brain, nil))
	defer srv.Close()

	resp := bdPostJSON(t, srv, "/entries/bulk-delete", map[string]any{
		"filter": map[string]any{"feature_id": "feat-1", "project": "p"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out types.BulkDeleteResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Deleted != 2 {
		t.Errorf("Deleted = %d, want 2", out.Deleted)
	}
	if got.Filter == nil || got.Filter.FeatureID == nil || *got.Filter.FeatureID != "feat-1" {
		t.Error("filter did not reach the service intact")
	}
}

func TestHandleBulkDelete_RejectsNeitherFilterNorPaths(t *testing.T) {
	srv := httptest.NewServer(newBulkDeleteRouter(&mockBrainService{}, nil))
	defer srv.Close()

	resp := bdPostJSON(t, srv, "/entries/bulk-delete", map[string]any{})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
}

func TestHandleBulkDelete_RejectsBothFilterAndPaths(t *testing.T) {
	srv := httptest.NewServer(newBulkDeleteRouter(&mockBrainService{}, nil))
	defer srv.Close()

	resp := bdPostJSON(t, srv, "/entries/bulk-delete", map[string]any{
		"filter": map[string]any{"project": "p"},
		"paths":  []string{"projects/p/task/a.md"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
}

// An unconstrained filter would match the whole brain. Deleting everything
// must never be one forgotten key away.
func TestHandleBulkDelete_RejectsEmptyFilter(t *testing.T) {
	called := false
	brain := &mockBrainService{
		bulkDeleteFunc: func(_ context.Context, _ types.BulkDeleteRequest) (*types.BulkDeleteResponse, error) {
			called = true
			return &types.BulkDeleteResponse{}, nil
		},
	}
	srv := httptest.NewServer(newBulkDeleteRouter(brain, nil))
	defer srv.Close()

	resp := bdPostJSON(t, srv, "/entries/bulk-delete", map[string]any{
		"filter": map[string]any{},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
	if called {
		t.Error("service was called with an unconstrained filter")
	}
}

// A misspelled filter key must fail loudly. Silently ignoring it would
// widen the match — on a delete that is unrecoverable.
func TestHandleBulkDelete_RejectsUnknownFilterField(t *testing.T) {
	srv := httptest.NewServer(newBulkDeleteRouter(&mockBrainService{}, nil))
	defer srv.Close()

	resp := bdPostRaw(t, srv, "/entries/bulk-delete",
		`{"filter":{"feature_id":"f","projct":"typo"}}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var body types.ErrorResponse
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body.Message == "" {
		t.Error("error message did not name the unknown field")
	}
}

func TestHandleBulkDelete_RejectsUnknownTopLevelField(t *testing.T) {
	srv := httptest.NewServer(newBulkDeleteRouter(&mockBrainService{}, nil))
	defer srv.Close()

	resp := bdPostRaw(t, srv, "/entries/bulk-delete",
		`{"paths":["projects/p/task/a.md"],"drop_everything":true}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// A dry run must not be blocked by a live claim — a preview should show
// what is there, and the guard fires on the real attempt.
func TestHandleBulkDelete_DryRunSkipsLiveClaimGuard(t *testing.T) {
	brain := &mockBrainService{
		bulkDeleteFunc: func(_ context.Context, req types.BulkDeleteRequest) (*types.BulkDeleteResponse, error) {
			if !req.DryRun {
				t.Error("expected only a dry-run call")
			}
			return &types.BulkDeleteResponse{DryRun: true, Total: 1}, nil
		},
	}
	tasks := &mockTaskService{
		getLiveClaimFunc: func(_ context.Context, _, _ string) (*types.LiveClaim, error) {
			return &types.LiveClaim{Live: true, RunnerID: "runner-1"}, nil
		},
	}
	srv := httptest.NewServer(newBulkDeleteRouter(brain, tasks))
	defer srv.Close()

	resp := bdPostJSON(t, srv, "/entries/bulk-delete", map[string]any{
		"filter":  map[string]any{"feature_id": "f"},
		"dry_run": true,
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a dry run", resp.StatusCode)
	}
}

// A live claim on any target fails the whole request, so the user deals
// with the running task rather than being left a half-deleted feature.
func TestHandleBulkDelete_LiveClaimBlocksWholeRequest(t *testing.T) {
	brain := &mockBrainService{
		bulkDeleteFunc: func(_ context.Context, req types.BulkDeleteRequest) (*types.BulkDeleteResponse, error) {
			if !req.DryRun {
				t.Error("live run reached the service despite a live claim")
			}
			return &types.BulkDeleteResponse{
				DryRun: true,
				Total:  1,
				Results: []types.BulkUpdateResult{
					{Path: "projects/p/task/a.md", ID: "a", Status: "ok"},
				},
			}, nil
		},
		recallFunc: func(_ context.Context, _ string) (*types.BrainEntry, error) {
			return &types.BrainEntry{
				ID: "a", Path: "projects/p/task/a.md", Type: "task", ProjectID: "p",
			}, nil
		},
	}
	tasks := &mockTaskService{
		getLiveClaimFunc: func(_ context.Context, _, _ string) (*types.LiveClaim, error) {
			return &types.LiveClaim{Live: true, RunnerID: "runner-7"}, nil
		},
	}
	srv := httptest.NewServer(newBulkDeleteRouter(brain, tasks))
	defer srv.Close()

	resp := bdPostJSON(t, srv, "/entries/bulk-delete", map[string]any{
		"filter": map[string]any{"feature_id": "f"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	var body types.ErrorResponse
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if !bytes.Contains([]byte(body.Message), []byte("runner-7")) {
		t.Errorf("message %q does not name the blocking runner", body.Message)
	}
}

func TestHandleBulkDelete_ForceOverridesLiveClaim(t *testing.T) {
	liveRun := false
	brain := &mockBrainService{
		bulkDeleteFunc: func(_ context.Context, req types.BulkDeleteRequest) (*types.BulkDeleteResponse, error) {
			if !req.DryRun {
				liveRun = true
			}
			return &types.BulkDeleteResponse{Deleted: 1, Total: 1}, nil
		},
		recallFunc: func(_ context.Context, _ string) (*types.BrainEntry, error) {
			return &types.BrainEntry{ID: "a", Type: "task", ProjectID: "p"}, nil
		},
	}
	tasks := &mockTaskService{
		getLiveClaimFunc: func(_ context.Context, _, _ string) (*types.LiveClaim, error) {
			return &types.LiveClaim{Live: true, RunnerID: "runner-7"}, nil
		},
	}
	srv := httptest.NewServer(newBulkDeleteRouter(brain, tasks))
	defer srv.Close()

	resp := bdPostJSON(t, srv, "/entries/bulk-delete?force=true", map[string]any{
		"filter": map[string]any{"feature_id": "f"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 with force=true", resp.StatusCode)
	}
	if !liveRun {
		t.Error("force=true did not reach the live delete")
	}
}

// ─── Single-entry delete guard ─────────────────────────────────────

func TestHandleDeleteEntry_BlocksWhenClaimIsLive(t *testing.T) {
	deleted := false
	brain := &mockBrainService{
		recallFunc: func(_ context.Context, _ string) (*types.BrainEntry, error) {
			return &types.BrainEntry{
				ID: "abc12def", Path: "projects/p/task/abc12def.md",
				Type: "task", ProjectID: "p",
			}, nil
		},
		deleteFunc: func(_ context.Context, _ string) error {
			deleted = true
			return nil
		},
	}
	tasks := &mockTaskService{
		getLiveClaimFunc: func(_ context.Context, _, _ string) (*types.LiveClaim, error) {
			return &types.LiveClaim{Live: true, RunnerID: "runner-3"}, nil
		},
	}
	srv := httptest.NewServer(newBulkDeleteRouter(brain, tasks))
	defer srv.Close()

	req, _ := http.NewRequest("DELETE",
		srv.URL+"/entries/projects/p/task/abc12def.md?confirm=true", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	if deleted {
		t.Error("entry was deleted despite a live claim")
	}
}

// A stale claim from a crashed runner is precisely what users are cleaning
// up. It must not block deletion.
func TestHandleDeleteEntry_AllowsWhenClaimIsNotLive(t *testing.T) {
	deleted := false
	brain := &mockBrainService{
		recallFunc: func(_ context.Context, _ string) (*types.BrainEntry, error) {
			return &types.BrainEntry{
				ID: "abc12def", Path: "projects/p/task/abc12def.md",
				Type: "task", ProjectID: "p",
			}, nil
		},
		deleteFunc: func(_ context.Context, _ string) error {
			deleted = true
			return nil
		},
	}
	tasks := &mockTaskService{
		getLiveClaimFunc: func(_ context.Context, _, _ string) (*types.LiveClaim, error) {
			return &types.LiveClaim{Live: false}, nil
		},
	}
	srv := httptest.NewServer(newBulkDeleteRouter(brain, tasks))
	defer srv.Close()

	req, _ := http.NewRequest("DELETE",
		srv.URL+"/entries/projects/p/task/abc12def.md?confirm=true", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if !deleted {
		t.Error("delete did not run")
	}
}

func TestHandleDeleteEntry_ForceOverridesLiveClaim(t *testing.T) {
	deleted := false
	brain := &mockBrainService{
		recallFunc: func(_ context.Context, _ string) (*types.BrainEntry, error) {
			return &types.BrainEntry{
				ID: "abc12def", Path: "projects/p/task/abc12def.md",
				Type: "task", ProjectID: "p",
			}, nil
		},
		deleteFunc: func(_ context.Context, _ string) error {
			deleted = true
			return nil
		},
	}
	tasks := &mockTaskService{
		getLiveClaimFunc: func(_ context.Context, _, _ string) (*types.LiveClaim, error) {
			return &types.LiveClaim{Live: true, RunnerID: "runner-3"}, nil
		},
	}
	srv := httptest.NewServer(newBulkDeleteRouter(brain, tasks))
	defer srv.Close()

	req, _ := http.NewRequest("DELETE",
		srv.URL+"/entries/projects/p/task/abc12def.md?confirm=true&force=true", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if !deleted {
		t.Error("force=true did not reach the delete")
	}
}

// Non-task entries have no claims. The guard must not query for them.
func TestHandleDeleteEntry_NonTaskSkipsGuard(t *testing.T) {
	queried := false
	brain := &mockBrainService{
		recallFunc: func(_ context.Context, _ string) (*types.BrainEntry, error) {
			return &types.BrainEntry{
				ID: "note1", Path: "projects/p/pattern/note1.md",
				Type: "pattern", ProjectID: "p",
			}, nil
		},
		deleteFunc: func(_ context.Context, _ string) error { return nil },
	}
	tasks := &mockTaskService{
		getLiveClaimFunc: func(_ context.Context, _, _ string) (*types.LiveClaim, error) {
			queried = true
			return &types.LiveClaim{Live: true, RunnerID: "r"}, nil
		},
	}
	srv := httptest.NewServer(newBulkDeleteRouter(brain, tasks))
	defer srv.Close()

	req, _ := http.NewRequest("DELETE",
		srv.URL+"/entries/projects/p/pattern/note1.md?confirm=true", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if queried {
		t.Error("claim guard ran for a non-task entry")
	}
}

// With no task service wired the guard must fail open, not block deletes.
func TestHandleDeleteEntry_NoTaskServiceFailsOpen(t *testing.T) {
	deleted := false
	brain := &mockBrainService{
		recallFunc: func(_ context.Context, _ string) (*types.BrainEntry, error) {
			return &types.BrainEntry{
				ID: "abc12def", Path: "projects/p/task/abc12def.md",
				Type: "task", ProjectID: "p",
			}, nil
		},
		deleteFunc: func(_ context.Context, _ string) error {
			deleted = true
			return nil
		},
	}
	srv := httptest.NewServer(newBulkDeleteRouter(brain, nil))
	defer srv.Close()

	req, _ := http.NewRequest("DELETE",
		srv.URL+"/entries/projects/p/task/abc12def.md?confirm=true", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if !deleted {
		t.Error("delete was blocked with no task service wired")
	}
}

// A registry error must also fail open — a guard that blocks every delete
// during a storage blip is worse than one that occasionally lets a racing
// delete through.
func TestHandleDeleteEntry_ClaimLookupErrorFailsOpen(t *testing.T) {
	deleted := false
	brain := &mockBrainService{
		recallFunc: func(_ context.Context, _ string) (*types.BrainEntry, error) {
			return &types.BrainEntry{
				ID: "abc12def", Path: "projects/p/task/abc12def.md",
				Type: "task", ProjectID: "p",
			}, nil
		},
		deleteFunc: func(_ context.Context, _ string) error {
			deleted = true
			return nil
		},
	}
	tasks := &mockTaskService{
		getLiveClaimFunc: func(_ context.Context, _, _ string) (*types.LiveClaim, error) {
			return nil, fmt.Errorf("storage unavailable")
		},
	}
	srv := httptest.NewServer(newBulkDeleteRouter(brain, tasks))
	defer srv.Close()

	req, _ := http.NewRequest("DELETE",
		srv.URL+"/entries/projects/p/task/abc12def.md?confirm=true", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if !deleted {
		t.Error("a claim lookup error blocked the delete; guard should fail open")
	}
}

// ─── Automation-run finalization ───────────────────────────────────
//
// createRunAudit writes automation_run entries as "queued" and nothing
// ever updated them — the audit trail could not answer whether an
// automation's work succeeded. Terminal task statuses now close the run.

func finalizeHarness(t *testing.T, runID string) (*chi.Mux, *map[string]string) {
	t.Helper()
	updates := map[string]string{}
	brain := &mockBrainService{
		updateFunc: func(_ context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error) {
			if req.Status != nil {
				updates[pathOrID] = *req.Status
			}
			return &types.BrainEntry{
				ID: "t1", Path: "projects/p/task/t1.md", Type: "task",
				ProjectID: "p", Status: derefOr(req.Status, "completed"),
				AutomationRunID: runID,
			}, nil
		},
		recallFunc: func(_ context.Context, _ string) (*types.BrainEntry, error) {
			return &types.BrainEntry{
				ID: "t1", Path: "projects/p/task/t1.md", Type: "task",
				ProjectID: "p", Status: "in_progress",
				AutomationRunID: runID,
			}, nil
		},
	}
	h := NewHandler(brain, WithHub(realtime.NewHub()))
	r := chi.NewRouter()
	r.Patch("/entries/*", h.HandleUpdateEntry)
	return r, &updates
}

func derefOr(s *string, d string) string {
	if s != nil {
		return *s
	}
	return d
}

func patchStatus(t *testing.T, srv *httptest.Server, status string) {
	t.Helper()
	req, _ := http.NewRequest("PATCH",
		srv.URL+"/entries/projects/p/task/t1.md",
		bytes.NewReader([]byte(`{"status":"`+status+`"}`)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("PATCH %s -> %d: %s", status, resp.StatusCode, b)
	}
	resp.Body.Close()
}

func TestFinalizeAutomationRun_CompletedTaskClosesRun(t *testing.T) {
	router, updates := finalizeHarness(t, "run123")
	srv := httptest.NewServer(router)
	defer srv.Close()

	patchStatus(t, srv, "completed")
	if got := (*updates)["run123"]; got != "completed" {
		t.Errorf("run status = %q, want completed", got)
	}
}

func TestFinalizeAutomationRun_BlockedAndCancelledMap(t *testing.T) {
	for task, want := range map[string]string{
		"blocked":   "blocked",
		"cancelled": "cancelled",
	} {
		router, updates := finalizeHarness(t, "run-"+task)
		srv := httptest.NewServer(router)
		patchStatus(t, srv, task)
		srv.Close()
		if got := (*updates)["run-"+task]; got != want {
			t.Errorf("task %s -> run status %q, want %q", task, got, want)
		}
	}
}

// A task with no automation_run_id (the overwhelmingly common case) must
// not produce any extra Update call.
func TestFinalizeAutomationRun_NoRunIDIsNoOp(t *testing.T) {
	router, updates := finalizeHarness(t, "")
	srv := httptest.NewServer(router)
	defer srv.Close()

	patchStatus(t, srv, "completed")
	if len(*updates) > 1 { // 1 = the task's own status update
		t.Errorf("unexpected extra updates: %v", *updates)
	}
	if _, ok := (*updates)[""]; ok {
		t.Error("finalizer ran with an empty run id")
	}
}

// Non-terminal transitions leave the run open.
func TestFinalizeAutomationRun_InProgressLeavesRunQueued(t *testing.T) {
	router, updates := finalizeHarness(t, "run123")
	srv := httptest.NewServer(router)
	defer srv.Close()

	patchStatus(t, srv, "in_progress")
	if _, ok := (*updates)["run123"]; ok {
		t.Error("non-terminal status finalized the run")
	}
}
