package api

import (
	"context"
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
