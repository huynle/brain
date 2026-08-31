package service

import (
	"context"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// seedScopedEntries lays down one entry per project plus one global entry,
// the shape the Entries browser sees when the sidebar hides most projects.
func seedScopedEntries(t *testing.T, svc *BrainServiceImpl) {
	t.Helper()
	ctx := context.Background()
	global := true
	for _, e := range []types.CreateEntryRequest{
		{Type: "plan", Title: "Alpha Plan", Content: "zebra alpha", Project: "alpha"},
		{Type: "plan", Title: "Beta Plan", Content: "zebra beta", Project: "beta"},
		{Type: "plan", Title: "Gamma Plan", Content: "zebra gamma", Project: "gamma"},
		{Type: "plan", Title: "Global Plan", Content: "zebra global", Global: &global},
	} {
		if _, err := svc.Save(ctx, e); err != nil {
			t.Fatalf("seed %q: %v", e.Title, err)
		}
	}
}

func titleSet(t *testing.T, titles []string) map[string]bool {
	t.Helper()
	out := make(map[string]bool, len(titles))
	for _, s := range titles {
		out[s] = true
	}
	return out
}

func TestList_ProjectsScope(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()
	seedScopedEntries(t, svc)

	tests := []struct {
		name  string
		req   types.ListEntriesRequest
		want  []string
		unset []string
	}{
		{
			name:  "several projects, no global",
			req:   types.ListEntriesRequest{Projects: []string{"alpha", "beta"}},
			want:  []string{"Alpha Plan", "Beta Plan"},
			unset: []string{"Gamma Plan", "Global Plan"},
		},
		{
			name:  "global token rides along",
			req:   types.ListEntriesRequest{Projects: []string{"alpha", "global"}},
			want:  []string{"Alpha Plan", "Global Plan"},
			unset: []string{"Beta Plan", "Gamma Plan"},
		},
		{
			// A scope that names a set is the thing Project cannot express,
			// so it has to win when a caller sends both.
			name:  "scope supersedes the single project field",
			req:   types.ListEntriesRequest{Project: "gamma", Projects: []string{"alpha"}},
			want:  []string{"Alpha Plan"},
			unset: []string{"Gamma Plan"},
		},
		{
			name:  "scope supersedes the global flag",
			req:   types.ListEntriesRequest{Global: &[]bool{true}[0], Projects: []string{"beta"}},
			want:  []string{"Beta Plan"},
			unset: []string{"Global Plan"},
		},
		{
			name:  "blanks and duplicates are tolerated",
			req:   types.ListEntriesRequest{Projects: []string{"alpha", "", "alpha", " beta "}},
			want:  []string{"Alpha Plan", "Beta Plan"},
			unset: []string{"Gamma Plan", "Global Plan"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := svc.List(ctx, tt.req)
			if err != nil {
				t.Fatalf("List failed: %v", err)
			}
			got := make([]string, len(resp.Entries))
			for i, e := range resp.Entries {
				got[i] = e.Title
			}
			set := titleSet(t, got)
			for _, w := range tt.want {
				if !set[w] {
					t.Errorf("missing %q; got %v", w, got)
				}
			}
			for _, u := range tt.unset {
				if set[u] {
					t.Errorf("unexpected %q; got %v", u, got)
				}
			}
		})
	}
}

func TestSearch_ProjectsScope(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()
	seedScopedEntries(t, svc)

	resp, err := svc.Search(ctx, types.SearchRequest{
		Query:    "zebra",
		Projects: []string{"alpha", "global"},
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	got := make([]string, len(resp.Results))
	for i, r := range resp.Results {
		got[i] = r.Title
	}
	set := titleSet(t, got)
	for _, w := range []string{"Alpha Plan", "Global Plan"} {
		if !set[w] {
			t.Errorf("missing %q; got %v", w, got)
		}
	}
	for _, u := range []string{"Beta Plan", "Gamma Plan"} {
		if set[u] {
			t.Errorf("unexpected %q; got %v", u, got)
		}
	}
}

func TestGetStats_ProjectsScope(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()
	seedScopedEntries(t, svc)

	scoped, err := svc.GetStats(ctx, false, "", []string{"alpha", "beta"})
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	if scoped.TotalEntries != 2 {
		t.Errorf("scoped TotalEntries = %d, want 2", scoped.TotalEntries)
	}
	if scoped.ByType["plan"] != 2 {
		t.Errorf("scoped plan count = %d, want 2", scoped.ByType["plan"])
	}

	withGlobal, err := svc.GetStats(ctx, false, "", []string{"alpha", "global"})
	if err != nil {
		t.Fatalf("GetStats(global) failed: %v", err)
	}
	if withGlobal.TotalEntries != 2 {
		t.Errorf("global-inclusive TotalEntries = %d, want 2", withGlobal.TotalEntries)
	}

	// The scope wins over project/global, same precedence as List.
	sup, err := svc.GetStats(ctx, true, "gamma", []string{"alpha"})
	if err != nil {
		t.Fatalf("GetStats(supersede) failed: %v", err)
	}
	if sup.TotalEntries != 1 {
		t.Errorf("superseding TotalEntries = %d, want 1", sup.TotalEntries)
	}
}

func TestParseProjectScope(t *testing.T) {
	ids, global := types.ParseProjectScope(
		[]string{" alpha ", "global", "alpha", "", "beta"},
	)
	if !global {
		t.Error("expected the global token to be recognized")
	}
	want := []string{"alpha", "beta"}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v, want %v", ids, want)
		}
	}

	if paths := ProjectScopePaths([]string{"alpha", "global"}); len(paths) != 2 ||
		paths[0] != "projects/alpha/" || paths[1] != "global/" {
		t.Errorf("ProjectScopePaths = %v", paths)
	}
	if paths := ProjectScopePaths(nil); paths != nil {
		t.Errorf("empty scope should mean no filter, got %v", paths)
	}
}
