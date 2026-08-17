package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// Live-claim guard coverage for HandleBulkUpdate — mirrors the bulk-delete
// guard tests in bulkdelete_handler_test.go. The rule is deliberately the
// same as delete's: ANY live update touching a task with a live claim fails
// the whole request with 409; dry runs skip the guard; force bypasses it;
// uncertainty fails open.

// newBulkUpdateRouter wires the bulk-update route plus an optional task
// service, which is what supplies the live-claim guard.
func newBulkUpdateRouter(brain *mockBrainService, tasks TaskService) *chi.Mux {
	opts := []HandlerOption{WithHub(realtime.NewHub())}
	if tasks != nil {
		opts = append(opts, WithTaskService(tasks))
	}
	h := NewHandler(brain, opts...)
	r := chi.NewRouter()
	r.Post("/entries/bulk-update", h.HandleBulkUpdate)
	return r
}

// guardedBulkUpdateBrain returns a mock whose BulkUpdate answers dry-run
// previews with a single ok task result and records whether a live (non-dry)
// run ever reached the service.
func guardedBulkUpdateBrain(liveRun *bool) *mockBrainService {
	return &mockBrainService{
		bulkUpdateFunc: func(_ context.Context, req types.BulkUpdateRequest) (*types.BulkUpdateResponse, error) {
			if !req.DryRun {
				*liveRun = true
			}
			return &types.BulkUpdateResponse{
				DryRun: req.DryRun,
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
}

func liveClaimTaskService(runnerID string) *mockTaskService {
	return &mockTaskService{
		getLiveClaimFunc: func(_ context.Context, _, _ string) (*types.LiveClaim, error) {
			return &types.LiveClaim{Live: true, RunnerID: runnerID}, nil
		},
	}
}

// A live claim on any target fails the whole request — same semantics as
// bulk-delete, so the user deals with the running task first instead of
// racing the runner's own writes.
func TestHandleBulkUpdate_LiveClaimBlocksWholeRequest(t *testing.T) {
	liveRun := false
	brain := guardedBulkUpdateBrain(&liveRun)
	srv := httptest.NewServer(newBulkUpdateRouter(brain, liveClaimTaskService("runner-7")))
	defer srv.Close()

	resp := bdPostJSON(t, srv, "/entries/bulk-update", map[string]any{
		"filter":  map[string]any{"feature_id": "f"},
		"updates": map[string]any{"status": "pending"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	if liveRun {
		t.Error("live update reached the service despite a live claim")
	}
	var body types.ErrorResponse
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if !bytes.Contains([]byte(body.Message), []byte("runner-7")) {
		t.Errorf("message %q does not name the blocking runner", body.Message)
	}
}

// The guard covers explicit-entries mode too — a live claim must block a
// targeted update just like a filter-matched one.
func TestHandleBulkUpdate_EntriesModeLiveClaimBlocks(t *testing.T) {
	liveRun := false
	brain := guardedBulkUpdateBrain(&liveRun)
	srv := httptest.NewServer(newBulkUpdateRouter(brain, liveClaimTaskService("runner-9")))
	defer srv.Close()

	resp := bdPostJSON(t, srv, "/entries/bulk-update", map[string]any{
		"entries": []map[string]any{
			{"path": "projects/p/task/a.md", "updates": map[string]any{"status": "pending"}},
		},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	if liveRun {
		t.Error("live update reached the service despite a live claim")
	}
}

// A dry run must not be blocked by a live claim — a preview should show
// what is there, and the guard fires on the real attempt.
func TestHandleBulkUpdate_DryRunSkipsLiveClaimGuard(t *testing.T) {
	liveRun := false
	brain := guardedBulkUpdateBrain(&liveRun)
	srv := httptest.NewServer(newBulkUpdateRouter(brain, liveClaimTaskService("runner-7")))
	defer srv.Close()

	resp := bdPostJSON(t, srv, "/entries/bulk-update", map[string]any{
		"filter":  map[string]any{"feature_id": "f"},
		"updates": map[string]any{"status": "pending"},
		"dry_run": true,
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a dry run", resp.StatusCode)
	}
	if liveRun {
		t.Error("dry run reached the service as a live run")
	}
}

// Body "force": true bypasses the guard. This also proves "force" is an
// accepted field, not rejected by the strict unknown-field check.
func TestHandleBulkUpdate_BodyForceOverridesLiveClaim(t *testing.T) {
	liveRun := false
	brain := guardedBulkUpdateBrain(&liveRun)
	srv := httptest.NewServer(newBulkUpdateRouter(brain, liveClaimTaskService("runner-7")))
	defer srv.Close()

	resp := bdPostJSON(t, srv, "/entries/bulk-update", map[string]any{
		"filter":  map[string]any{"feature_id": "f"},
		"updates": map[string]any{"status": "pending"},
		"force":   true,
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 with force in body", resp.StatusCode)
	}
	if !liveRun {
		t.Error("force=true did not reach the live update")
	}
}

// Non-task entries have no claims; the guard must let them through and
// never query the claim registry for them.
func TestHandleBulkUpdate_NonTaskSkipsGuard(t *testing.T) {
	liveRun := false
	brain := guardedBulkUpdateBrain(&liveRun)
	brain.recallFunc = func(_ context.Context, _ string) (*types.BrainEntry, error) {
		return &types.BrainEntry{
			ID: "note1", Path: "projects/p/pattern/note1.md", Type: "pattern", ProjectID: "p",
		}, nil
	}
	queried := false
	tasks := &mockTaskService{
		getLiveClaimFunc: func(_ context.Context, _, _ string) (*types.LiveClaim, error) {
			queried = true
			return &types.LiveClaim{Live: true, RunnerID: "r"}, nil
		},
	}
	srv := httptest.NewServer(newBulkUpdateRouter(brain, tasks))
	defer srv.Close()

	resp := bdPostJSON(t, srv, "/entries/bulk-update", map[string]any{
		"filter":  map[string]any{"feature_id": "f"},
		"updates": map[string]any{"status": "pending"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 for non-task targets", resp.StatusCode)
	}
	if queried {
		t.Error("claim guard ran for a non-task entry")
	}
	if !liveRun {
		t.Error("live update never ran")
	}
}

// A registry error must fail open — matching taskHasLiveClaim's contract. A
// guard that blocks every bulk update during a storage blip is worse than
// one that occasionally lets a racing update through.
func TestHandleBulkUpdate_ClaimLookupErrorFailsOpen(t *testing.T) {
	liveRun := false
	brain := guardedBulkUpdateBrain(&liveRun)
	tasks := &mockTaskService{
		getLiveClaimFunc: func(_ context.Context, _, _ string) (*types.LiveClaim, error) {
			return nil, fmt.Errorf("storage unavailable")
		},
	}
	srv := httptest.NewServer(newBulkUpdateRouter(brain, tasks))
	defer srv.Close()

	resp := bdPostJSON(t, srv, "/entries/bulk-update", map[string]any{
		"filter":  map[string]any{"feature_id": "f"},
		"updates": map[string]any{"status": "pending"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (fail open on registry error)", resp.StatusCode)
	}
	if !liveRun {
		t.Error("a claim lookup error blocked the update; guard should fail open")
	}
}

// With no task service wired the guard must fail open, not block updates.
func TestHandleBulkUpdate_NoTaskServiceFailsOpen(t *testing.T) {
	liveRun := false
	brain := guardedBulkUpdateBrain(&liveRun)
	srv := httptest.NewServer(newBulkUpdateRouter(brain, nil))
	defer srv.Close()

	resp := bdPostJSON(t, srv, "/entries/bulk-update", map[string]any{
		"filter":  map[string]any{"feature_id": "f"},
		"updates": map[string]any{"status": "pending"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 with no task service wired", resp.StatusCode)
	}
	if !liveRun {
		t.Error("update was blocked with no task service wired")
	}
}
