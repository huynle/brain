package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// runsTestStore models the store for /automation-runs: a Tags filter keeps
// only entries carrying that tag, List applies Limit, and entries are
// returned in the order given (callers pass newest first).
func runsTestStore(entries []types.BrainEntry, calls *[]types.ListEntriesRequest) *mockBrainService {
	return &mockBrainService{listFunc: func(_ context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error) {
		*calls = append(*calls, req)
		out := make([]types.BrainEntry, 0, len(entries))
		for _, e := range entries {
			if req.Tags != "" && !runsHasTag(e.Tags, req.Tags) {
				continue
			}
			out = append(out, e)
		}
		if req.Limit > 0 && len(out) > req.Limit {
			out = out[:req.Limit]
		}
		return &types.ListEntriesResponse{Entries: out, Total: len(out)}, nil
	}}
}

func runsHasTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

// taggedRun builds a run audit as the server now writes it: tagged, with the
// automation_id body line also present.
func taggedRun(id, automationID, created string) types.BrainEntry {
	return types.BrainEntry{
		ID:      id,
		Type:    "automation_run",
		Created: created,
		Tags:    []string{"automation_run", "automation:" + automationID},
		Content: "automation_id: " + automationID + "\n",
	}
}

// legacyRun builds a run audit written before the tag existed: body only.
func legacyRun(id, automationID, created string) types.BrainEntry {
	return types.BrainEntry{
		ID:      id,
		Type:    "automation_run",
		Created: created,
		Tags:    []string{"automation_run"},
		Content: "automation_id: " + automationID + "\n",
	}
}

func listRunsFor(t *testing.T, h *Handler, query string) (types.ListEntriesResponse, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/automation-runs?"+query, nil)
	rec := httptest.NewRecorder()
	h.HandleListAutomationRuns(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	return decodeJSON[types.ListEntriesResponse](t, rec.Result()), rec
}

// TestListAutomationRuns_TaggedPageNeedsNoBodyScan pins that a full tagged
// page is answered by the tag query alone: one List call, filtered by tag,
// newest first.
func TestListAutomationRuns_TaggedPageNeedsNoBodyScan(t *testing.T) {
	entries := []types.BrainEntry{
		taggedRun("t3", "auto1", "2026-10-09T12:02:00Z"),
		taggedRun("x2", "auto2", "2026-10-09T12:01:30Z"),
		taggedRun("t2", "auto1", "2026-10-09T12:01:00Z"),
		taggedRun("t1", "auto1", "2026-10-09T12:00:00Z"),
	}
	var calls []types.ListEntriesRequest
	h := &Handler{brain: runsTestStore(entries, &calls)}

	resp, _ := listRunsFor(t, h, "automation_id=auto1&limit=2&project=proj-a")

	if len(calls) != 1 {
		t.Fatalf("List calls = %d, want 1 (tag query only, no body scan)", len(calls))
	}
	if calls[0].Tags != "automation:auto1" {
		t.Errorf("first query Tags = %q, want automation:auto1", calls[0].Tags)
	}
	if calls[0].SortBy != "created" || calls[0].SortOrder != "desc" {
		t.Errorf("first query sort = %q/%q, want created/desc", calls[0].SortBy, calls[0].SortOrder)
	}
	if calls[0].Project != "proj-a" || calls[0].Type != "automation_run" {
		t.Errorf("first query scope = type %q project %q", calls[0].Type, calls[0].Project)
	}
	if got := runIDs(resp.Entries); fmt.Sprint(got) != "[t3 t2]" {
		t.Errorf("entries = %v, want [t3 t2]", got)
	}
}

// TestListAutomationRuns_MergesLegacyAndDedupes pins that a short tagged page
// is completed from legacy body-only audits, that a run found by both paths
// appears once, and that the merged page is newest first.
func TestListAutomationRuns_MergesLegacyAndDedupes(t *testing.T) {
	entries := []types.BrainEntry{
		taggedRun("t3", "auto1", "2026-10-09T12:02:00Z"),
		legacyRun("l2", "auto1", "2026-10-08T09:00:00Z"),
		taggedRun("x9", "auto2", "2026-10-09T12:01:00Z"),
		legacyRun("l1", "auto1", "2026-10-07T09:00:00Z"),
	}
	var calls []types.ListEntriesRequest
	h := &Handler{brain: runsTestStore(entries, &calls)}

	resp, _ := listRunsFor(t, h, "automation_id=auto1&limit=5")

	if len(calls) != 2 {
		t.Fatalf("List calls = %d, want 2 (tag query, then body-scan fallback)", len(calls))
	}
	if calls[1].Tags != "" {
		t.Errorf("fallback query Tags = %q, want empty (body scan)", calls[1].Tags)
	}
	if got := runIDs(resp.Entries); fmt.Sprint(got) != "[t3 l2 l1]" {
		t.Errorf("entries = %v, want [t3 l2 l1] (deduped, newest first)", got)
	}
	if resp.Total != 3 {
		t.Errorf("Total = %d, want 3", resp.Total)
	}
}

// TestListAutomationRuns_TruncatedOnlyWhenScanWindowExhausted pins the
// truncation semantics: a short page is "truncated" only when the body-scan
// window was filled, so the caller can tell "no more runs" from "older than
// the window".
func TestListAutomationRuns_TruncatedOnlyWhenScanWindowExhausted(t *testing.T) {
	// Exhausted: 100 non-matching bodies == the scan window for limit=5.
	var many []types.BrainEntry
	for i := 0; i < 150; i++ {
		many = append(many, types.BrainEntry{
			ID: fmt.Sprintf("o%03d", i), Type: "automation_run",
			Content: "automation_id: someone-else\n",
		})
	}
	var calls []types.ListEntriesRequest
	resp, _ := listRunsFor(t, &Handler{brain: runsTestStore(many, &calls)}, "automation_id=wanted&limit=5")
	if len(resp.Entries) != 0 || !resp.Truncated {
		t.Errorf("exhausted window: entries=%d truncated=%v, want 0/true", len(resp.Entries), resp.Truncated)
	}

	// Not exhausted: the store has fewer rows than the window, so "no runs".
	calls = nil
	resp, _ = listRunsFor(t, &Handler{brain: runsTestStore(many[:10], &calls)}, "automation_id=wanted&limit=5")
	if len(resp.Entries) != 0 || resp.Truncated {
		t.Errorf("short store: entries=%d truncated=%v, want 0/false", len(resp.Entries), resp.Truncated)
	}
}

func runIDs(entries []types.BrainEntry) []string {
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	return ids
}
