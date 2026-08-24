package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/types"
)

func TestHandleListAutomationRunsFiltersAndReturnsRuns(t *testing.T) {
	mock := &mockBrainService{
		listFunc: func(ctx context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error) {
			if req.Type != "automation_run" {
				t.Fatalf("List Type = %q, want automation_run", req.Type)
			}
			if req.Project != "proj-a" {
				t.Fatalf("List Project = %q, want proj-a", req.Project)
			}
			return &types.ListEntriesResponse{Entries: []types.BrainEntry{
				{
					ID:        "run1",
					Path:      "projects/proj-a/automation_run/run1.md",
					Title:     "Automation Run: auto1",
					Type:      "automation_run",
					Status:    "queued",
					ProjectID: "proj-a",
					Content:   "automation_id: auto1\n### Generated Tasks\n- task1\n",
				},
				{
					ID:        "run2",
					Path:      "projects/proj-a/automation_run/run2.md",
					Title:     "Automation Run: auto2",
					Type:      "automation_run",
					Status:    "failed",
					ProjectID: "proj-a",
					Content:   "automation_id: auto2\nerror: boom\n",
				},
			}, Total: 2}, nil
		},
	}
	router := NewRouter(config.Config{}, WithHandler(NewHandler(mock)))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/automation-runs?project=proj-a&automation_id=auto1&status=queued", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := decodeJSON[types.ListEntriesResponse](t, rec.Result())
	if len(resp.Entries) != 1 {
		t.Fatalf("expected one filtered run, got %d", len(resp.Entries))
	}
	if resp.Entries[0].ID != "run1" || resp.Entries[0].Status != "queued" {
		t.Fatalf("unexpected run response: %#v", resp.Entries[0])
	}
}

func TestHandleGetAutomationRunReturnsRunDetail(t *testing.T) {
	mock := &mockBrainService{
		recallFunc: func(ctx context.Context, pathOrID string) (*types.BrainEntry, error) {
			if pathOrID != "run1" {
				t.Fatalf("Recall pathOrID = %q, want run1", pathOrID)
			}
			return &types.BrainEntry{
				ID:      "run1",
				Path:    "projects/proj-a/automation_run/run1.md",
				Title:   "Automation Run: auto1",
				Type:    "automation_run",
				Status:  "queued",
				Content: "automation_id: auto1\n### Generated Tasks\n- task1\n",
			}, nil
		},
	}
	router := NewRouter(config.Config{}, WithHandler(NewHandler(mock)))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/automation-runs/run1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	entry := decodeJSON[types.BrainEntry](t, rec.Result())
	if entry.ID != "run1" || entry.Type != "automation_run" {
		t.Fatalf("unexpected run detail: %#v", entry)
	}
}

// TestListAutomationRuns_FilterFindsRunsBeyondThePage pins that filtering by
// automation_id searches past the first page.
//
// automation_id lives in the run's markdown body, not a column, so it can
// only be matched after fetching. The handler used to fetch exactly `limit`
// rows and filter afterwards — asking for the N most recent runs across ALL
// automations and keeping the few that matched. On a store where
// automation_run is ~95% of all entries, one automation's runs are almost
// never in that page, so the query returned nothing while thousands of its
// runs existed. Indistinguishable from "this automation has never run".
func TestListAutomationRuns_FilterFindsRunsBeyondThePage(t *testing.T) {
	// 50 runs for other automations, then the one we want — so a naive
	// limit=5 fetch would never see it.
	var entries []types.BrainEntry
	for i := 0; i < 50; i++ {
		entries = append(entries, types.BrainEntry{
			ID: fmt.Sprintf("other%03d", i), Type: "automation_run",
			Content: "automation_id: someone-else\n",
		})
	}
	entries = append(entries, types.BrainEntry{
		ID: "wanted01", Type: "automation_run",
		Content: "automation_id: wanted-automation\n",
	})

	h := &Handler{brain: &mockBrainService{
		listFunc: func(ctx context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error) {
			out := entries
			if req.Limit > 0 && req.Limit < len(out) {
				out = out[:req.Limit]
			}
			return &types.ListEntriesResponse{Entries: out, Total: len(out)}, nil
		},
	}}

	req := httptest.NewRequest(http.MethodGet, "/automation-runs?automation_id=wanted-automation&limit=5", nil)
	rec := httptest.NewRecorder()
	h.HandleListAutomationRuns(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := decodeJSON[types.ListEntriesResponse](t, rec.Result())
	if len(body.Entries) != 1 {
		t.Fatalf("found %d runs; the filter did not look past the first page", len(body.Entries))
	}
	if body.Entries[0].ID != "wanted01" {
		t.Errorf("wrong run returned: %s", body.Entries[0].ID)
	}
}
