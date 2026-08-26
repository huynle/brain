package service

import (
	"context"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// ---------------------------------------------------------------------------
// Link graph, exercised through the service layer (task 5t174je3).
//
// The storage and markdown tests below this layer all passed while the graph
// was empty in production, because nothing tested the path an agent actually
// takes: Save an entry, then ask for its links. These tests run that path.
// ---------------------------------------------------------------------------

func saveEntry(t *testing.T, svc *BrainServiceImpl, req types.CreateEntryRequest) *types.CreateEntryResponse {
	t.Helper()
	resp, err := svc.Save(context.Background(), req)
	if err != nil {
		t.Fatalf("Save(%q): %v", req.Title, err)
	}
	return resp
}

// assertLinked checks that source→target shows up in both directions. Targets
// are often shared across subtests, so membership is asserted, not an exact
// count.
func assertLinked(t *testing.T, svc *BrainServiceImpl, source, target *types.CreateEntryResponse) {
	t.Helper()
	ctx := context.Background()

	out, err := svc.GetOutlinks(ctx, source.ID)
	if err != nil {
		t.Fatalf("GetOutlinks(%s): %v", source.ID, err)
	}
	if !containsPath(out, target.Path) {
		t.Errorf("GetOutlinks(%s) does not include %q; got %v", source.ID, target.Path, pathsOf(out))
	}

	back, err := svc.GetBacklinks(ctx, target.ID)
	if err != nil {
		t.Fatalf("GetBacklinks(%s): %v", target.ID, err)
	}
	if !containsPath(back, source.Path) {
		t.Errorf("GetBacklinks(%s) does not include %q; got %v", target.ID, source.Path, pathsOf(back))
	}
}

func pathsOf(entries []types.BrainEntry) []string {
	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		paths = append(paths, e.Path)
	}
	return paths
}

func containsPath(entries []types.BrainEntry, path string) bool {
	for _, e := range entries {
		if e.Path == path {
			return true
		}
	}
	return false
}

// TestSaveThenLinks_EveryHrefForm covers the four ways an entry can name
// another. Row A ([[wiki-link]]) is the one that never worked.
func TestSaveThenLinks_EveryHrefForm(t *testing.T) {
	svc, _, _ := newTestBrainService(t)

	target := saveEntry(t, svc, types.CreateEntryRequest{
		Type: "scratch", Title: "Target Entry", Content: "I am the target.", Project: "linktest",
	})

	forms := []struct {
		name string
		body func() string
	}{
		{"wiki_link", func() string { return "See [[Target Entry]] for details." }},
		{"wiki_link_alias", func() string { return "See [[Target Entry|the target]] for details." }},
		{"short_id", func() string { return "See [Target Entry](" + target.ID + ") for details." }},
		{"short_id_dot_md", func() string { return "See [Target Entry](" + target.ID + ".md) for details." }},
		{"full_path", func() string { return "See [Target Entry](" + target.Path + ") for details." }},
	}

	for _, f := range forms {
		t.Run(f.name, func(t *testing.T) {
			src := saveEntry(t, svc, types.CreateEntryRequest{
				Type: "scratch", Title: "Source " + f.name, Content: f.body(), Project: "linktest",
			})
			assertLinked(t, svc, src, target)
		})
	}
}

// A wiki-link written before its target exists must resolve when the target
// arrives — agents routinely reference an entry they are about to write.
func TestSaveThenLinks_WikiLinkResolvesWhenTargetArrives(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	src := saveEntry(t, svc, types.CreateEntryRequest{
		Type: "scratch", Title: "Source", Content: "Depends on [[Arrives Later]].", Project: "linktest",
	})

	out, err := svc.GetOutlinks(ctx, src.ID)
	if err != nil {
		t.Fatalf("GetOutlinks: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected no outlinks before the target exists, got %d", len(out))
	}

	target := saveEntry(t, svc, types.CreateEntryRequest{
		Type: "scratch", Title: "Arrives Later", Content: "Here now.", Project: "linktest",
	})
	assertLinked(t, svc, src, target)
}

// Links crossing the project/global boundary were never covered; the real-world
// report was exactly this shape (a project walkthrough and a global learning).
func TestSaveThenLinks_CrossScope(t *testing.T) {
	svc, _, _ := newTestBrainService(t)

	global := saveEntry(t, svc, types.CreateEntryRequest{
		Type: "learning", Title: "A Global Learning", Content: "Global body.", Global: boolPtr(true),
	})
	proj := saveEntry(t, svc, types.CreateEntryRequest{
		Type: "walkthrough", Title: "A Project Walkthrough",
		Content: "Builds on [[A Global Learning]].", Project: "google-keep",
	})

	assertLinked(t, svc, proj, global)
}

// Entries that document the link syntax must not populate the graph with the
// placeholder targets from their examples.
func TestSaveThenLinks_CodeExamplesAreNotLinks(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	target := saveEntry(t, svc, types.CreateEntryRequest{
		Type: "scratch", Title: "Real Target", Content: "body", Project: "linktest",
	})

	doc := "Reference an entry like this:\n\n```markdown\n[Title](" + target.ID + ")\nSee [[Real Target]]\n```\n\n" +
		"Inline, write `[Title](pattern-id)`. The actual link is [Real Target](" + target.ID + ")."

	src := saveEntry(t, svc, types.CreateEntryRequest{
		Type: "scratch", Title: "Syntax Docs", Content: doc, Project: "linktest",
	})

	// Only the one real link outside the code regions counts.
	assertLinked(t, svc, src, target)

	back, err := svc.GetBacklinks(ctx, target.ID)
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(back) != 1 {
		t.Errorf("GetBacklinks returned %d entries, want 1", len(back))
	}
}

// relatedEntries was accepted by the API and the MCP save tool and then
// discarded. It now writes real links.
func TestSave_RelatedEntriesProduceLinks(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	byTitle := saveEntry(t, svc, types.CreateEntryRequest{
		Type: "scratch", Title: "Related By Title", Content: "body", Project: "linktest",
	})
	byID := saveEntry(t, svc, types.CreateEntryRequest{
		Type: "scratch", Title: "Related By ID", Content: "body", Project: "linktest",
	})

	src := saveEntry(t, svc, types.CreateEntryRequest{
		Type: "scratch", Title: "Source With Related", Content: "Main body.", Project: "linktest",
		RelatedEntries: []string{"Related By Title", byID.ID, "  ", "Related By Title"},
	})

	out, err := svc.GetOutlinks(ctx, src.ID)
	if err != nil {
		t.Fatalf("GetOutlinks: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("GetOutlinks returned %d entries, want 2 (blank and duplicate dropped): %+v", len(out), out)
	}

	for _, want := range []string{byTitle.ID, byID.ID} {
		back, err := svc.GetBacklinks(ctx, want)
		if err != nil {
			t.Fatalf("GetBacklinks(%s): %v", want, err)
		}
		if len(back) != 1 {
			t.Errorf("GetBacklinks(%s) returned %d entries, want 1", want, len(back))
		}
	}
}

func TestSave_RelatedEntriesEmptyLeavesContentAlone(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	src := saveEntry(t, svc, types.CreateEntryRequest{
		Type: "scratch", Title: "No Related", Content: "Main body.", Project: "linktest",
		RelatedEntries: []string{"", "   "},
	})

	entry, err := svc.Recall(ctx, src.ID)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if got := entry.Content; strings.Contains(got, "## Related") {
		t.Errorf("content should carry no Related section, got:\n%s", got)
	}
}
