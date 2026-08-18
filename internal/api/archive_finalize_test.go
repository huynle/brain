package api

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// ─── Archive vs automation-run audit ───────────────────────────────
//
// Archiving settles a task without changing its outcome: a run that
// already finalized (e.g. completed) must keep its audit record, while
// a still-open (queued) run is closed as cancelled.

// archiveFinalizeHarness is finalizeHarness with a controllable run
// status behind Recall, so the archive guard can be exercised.
func archiveFinalizeHarness(t *testing.T, runID, runStatus string, recallErr error) (*chi.Mux, *map[string]string) {
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
		recallFunc: func(_ context.Context, pathOrID string) (*types.BrainEntry, error) {
			if pathOrID == runID {
				if recallErr != nil {
					return nil, recallErr
				}
				return &types.BrainEntry{
					ID: runID, Type: "automation_run", Status: runStatus,
				}, nil
			}
			return &types.BrainEntry{
				ID: "t1", Path: "projects/p/task/t1.md", Type: "task",
				ProjectID: "p", Status: "completed",
				AutomationRunID: runID,
			}, nil
		},
	}
	h := NewHandler(brain, WithHub(realtime.NewHub()))
	r := chi.NewRouter()
	r.Patch("/entries/*", h.HandleUpdateEntry)
	return r, &updates
}

func TestFinalizeAutomationRun_ArchiveKeepsFinalizedRun(t *testing.T) {
	// Every already-terminal run status must survive an archive untouched.
	for _, runStatus := range []string{"completed", "blocked", "cancelled"} {
		t.Run(runStatus, func(t *testing.T) {
			router, updates := archiveFinalizeHarness(t, "run123", runStatus, nil)
			srv := httptest.NewServer(router)
			defer srv.Close()

			patchStatus(t, srv, "archived")
			if got, ok := (*updates)["run123"]; ok {
				t.Errorf("archive rewrote %s run to %q, want untouched", runStatus, got)
			}
		})
	}
}

func TestFinalizeAutomationRun_ArchiveCancelsOpenRun(t *testing.T) {
	router, updates := archiveFinalizeHarness(t, "run123", "queued", nil)
	srv := httptest.NewServer(router)
	defer srv.Close()

	patchStatus(t, srv, "archived")
	if got := (*updates)["run123"]; got != "cancelled" {
		t.Errorf("run status = %q, want cancelled (archiving unfinished work retires the run)", got)
	}
}

func TestFinalizeAutomationRun_ArchiveRecallErrorFailsOpen(t *testing.T) {
	router, updates := archiveFinalizeHarness(t, "run123", "", fmt.Errorf("boom"))
	srv := httptest.NewServer(router)
	defer srv.Close()

	patchStatus(t, srv, "archived")
	if got := (*updates)["run123"]; got != "cancelled" {
		t.Errorf("run status = %q, want cancelled (best-effort fallback on recall error)", got)
	}
}
