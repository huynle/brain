package storage

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// Test graph helper: creates a graph of notes with links
//
//	A → B, A → C, B → C, D (isolated)
//
// Returns the inserted notes for assertions.
// ---------------------------------------------------------------------------

type testGraph struct {
	A, B, C, D *NoteRow
}

func setupTestGraph(t *testing.T, s *StorageLayer) testGraph {
	t.Helper()
	ctx := context.Background()

	noteA := sampleNote("projects/test/graph/a.md", "grpha001", "Note A")
	noteB := sampleNote("projects/test/graph/b.md", "grphb002", "Note B")
	noteC := sampleNote("projects/test/graph/c.md", "grphc003", "Note C")
	noteD := sampleNote("projects/test/graph/d.md", "grphd004", "Note D")

	// Give D a different type for orphan filtering tests.
	dType := "idea"
	noteD.Type = &dType

	a, err := s.InsertNote(ctx, noteA)
	if err != nil {
		t.Fatalf("insert A: %v", err)
	}
	b, err := s.InsertNote(ctx, noteB)
	if err != nil {
		t.Fatalf("insert B: %v", err)
	}
	c, err := s.InsertNote(ctx, noteC)
	if err != nil {
		t.Fatalf("insert C: %v", err)
	}
	d, err := s.InsertNote(ctx, noteD)
	if err != nil {
		t.Fatalf("insert D: %v", err)
	}

	// A → B (resolved link)
	err = s.SetLinks(ctx, a.Path, []LinkInput{
		{TargetPath: b.Path, Href: b.Path, Title: "Link to B"},
		{TargetPath: c.Path, Href: c.Path, Title: "Link to C"},
	})
	if err != nil {
		t.Fatalf("set links A: %v", err)
	}

	// B → C (resolved link)
	err = s.SetLinks(ctx, b.Path, []LinkInput{
		{TargetPath: c.Path, Href: c.Path, Title: "Link to C"},
	})
	if err != nil {
		t.Fatalf("set links B: %v", err)
	}

	return testGraph{A: a, B: b, C: c, D: d}
}

// ---------------------------------------------------------------------------
// GetBacklinks
// ---------------------------------------------------------------------------

func TestGetBacklinks_HasBacklinks(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	g := setupTestGraph(t, s)

	// B should have backlink from A (A → B)
	got, err := s.GetBacklinks(ctx, g.B.Path)
	if err != nil {
		t.Fatalf("GetBacklinks(B) failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("GetBacklinks(B): got %d notes, want 1", len(got))
	}
	if got[0].Path != g.A.Path {
		t.Errorf("GetBacklinks(B)[0].Path = %q, want %q", got[0].Path, g.A.Path)
	}
}

func TestGetBacklinks_MultipleBacklinks(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	g := setupTestGraph(t, s)

	// C should have backlinks from A and B (A → C, B → C)
	got, err := s.GetBacklinks(ctx, g.C.Path)
	if err != nil {
		t.Fatalf("GetBacklinks(C) failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("GetBacklinks(C): got %d notes, want 2", len(got))
	}

	paths := map[string]bool{}
	for _, n := range got {
		paths[n.Path] = true
	}
	if !paths[g.A.Path] {
		t.Errorf("GetBacklinks(C) missing A")
	}
	if !paths[g.B.Path] {
		t.Errorf("GetBacklinks(C) missing B")
	}
}

func TestGetBacklinks_NoBacklinks(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	g := setupTestGraph(t, s)

	// A has no backlinks (nothing links to A)
	got, err := s.GetBacklinks(ctx, g.A.Path)
	if err != nil {
		t.Fatalf("GetBacklinks(A) failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("GetBacklinks(A): got %d notes, want 0", len(got))
	}
}

func TestGetBacklinks_UnresolvedLink(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Create a note that links to a path via target_path (unresolved — target_id is NULL).
	source := sampleNote("projects/test/graph/unresolved-src.md", "ursrc001", "Unresolved Source")
	inserted, err := s.InsertNote(ctx, source)
	if err != nil {
		t.Fatalf("insert source: %v", err)
	}

	target := sampleNote("projects/test/graph/unresolved-tgt.md", "urtgt001", "Unresolved Target")
	_, err = s.InsertNote(ctx, target)
	if err != nil {
		t.Fatalf("insert target: %v", err)
	}

	// Insert link with target_path but NULL target_id (simulating unresolved link).
	_, err = s.DB().ExecContext(ctx,
		"INSERT INTO links (source_id, target_path, target_id, href) VALUES (?, ?, NULL, ?)",
		inserted.ID, "projects/test/graph/unresolved-tgt.md", "projects/test/graph/unresolved-tgt.md",
	)
	if err != nil {
		t.Fatalf("insert unresolved link: %v", err)
	}

	// GetBacklinks should find the source via target_path match.
	got, err := s.GetBacklinks(ctx, "projects/test/graph/unresolved-tgt.md")
	if err != nil {
		t.Fatalf("GetBacklinks(unresolved-tgt) failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("GetBacklinks(unresolved-tgt): got %d notes, want 1", len(got))
	}
	if got[0].Path != inserted.Path {
		t.Errorf("got path %q, want %q", got[0].Path, inserted.Path)
	}
}

// ---------------------------------------------------------------------------
// GetOutlinks
// ---------------------------------------------------------------------------

func TestGetOutlinks_HasOutlinks(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	g := setupTestGraph(t, s)

	// A has outlinks to B and C
	got, err := s.GetOutlinks(ctx, g.A.Path)
	if err != nil {
		t.Fatalf("GetOutlinks(A) failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("GetOutlinks(A): got %d notes, want 2", len(got))
	}

	paths := map[string]bool{}
	for _, n := range got {
		paths[n.Path] = true
	}
	if !paths[g.B.Path] {
		t.Errorf("GetOutlinks(A) missing B")
	}
	if !paths[g.C.Path] {
		t.Errorf("GetOutlinks(A) missing C")
	}
}

func TestGetOutlinks_SingleOutlink(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	g := setupTestGraph(t, s)

	// B has outlink to C only
	got, err := s.GetOutlinks(ctx, g.B.Path)
	if err != nil {
		t.Fatalf("GetOutlinks(B) failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("GetOutlinks(B): got %d notes, want 1", len(got))
	}
	if got[0].Path != g.C.Path {
		t.Errorf("GetOutlinks(B)[0].Path = %q, want %q", got[0].Path, g.C.Path)
	}
}

func TestGetOutlinks_NoOutlinks(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	g := setupTestGraph(t, s)

	// D has no outlinks
	got, err := s.GetOutlinks(ctx, g.D.Path)
	if err != nil {
		t.Fatalf("GetOutlinks(D) failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("GetOutlinks(D): got %d notes, want 0", len(got))
	}
}

// ---------------------------------------------------------------------------
// GetRelated
// ---------------------------------------------------------------------------

func TestGetRelated_SharedTarget(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	g := setupTestGraph(t, s)

	// A and B both link to C, so they are related.
	// GetRelated(A) should return B (they share target C).
	got, err := s.GetRelated(ctx, g.A.Path, 10)
	if err != nil {
		t.Fatalf("GetRelated(A) failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("GetRelated(A): got %d notes, want 1", len(got))
	}
	if got[0].Path != g.B.Path {
		t.Errorf("GetRelated(A)[0].Path = %q, want %q", got[0].Path, g.B.Path)
	}
}

func TestGetRelated_Bidirectional(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	g := setupTestGraph(t, s)

	// GetRelated(B) should return A (they share target C).
	got, err := s.GetRelated(ctx, g.B.Path, 10)
	if err != nil {
		t.Fatalf("GetRelated(B) failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("GetRelated(B): got %d notes, want 1", len(got))
	}
	if got[0].Path != g.A.Path {
		t.Errorf("GetRelated(B)[0].Path = %q, want %q", got[0].Path, g.A.Path)
	}
}

func TestGetRelated_NoRelated(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	g := setupTestGraph(t, s)

	// D has no links, so no related notes.
	got, err := s.GetRelated(ctx, g.D.Path, 10)
	if err != nil {
		t.Fatalf("GetRelated(D) failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("GetRelated(D): got %d notes, want 0", len(got))
	}
}

func TestGetRelated_ExcludesSelf(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	g := setupTestGraph(t, s)

	// GetRelated(A) should NOT include A itself.
	got, err := s.GetRelated(ctx, g.A.Path, 10)
	if err != nil {
		t.Fatalf("GetRelated(A) failed: %v", err)
	}
	for _, n := range got {
		if n.Path == g.A.Path {
			t.Errorf("GetRelated(A) should not include A itself")
		}
	}
}

func TestGetRelated_RespectsLimit(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	g := setupTestGraph(t, s)

	// With limit=0, should return empty.
	// (A has 1 related note B, but limit 0 means none.)
	_ = g
	got, err := s.GetRelated(ctx, g.A.Path, 0)
	if err != nil {
		t.Fatalf("GetRelated(A, limit=0) failed: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("GetRelated(A, limit=0): got %d notes, want 0", len(got))
	}
}

// ---------------------------------------------------------------------------
// GetOrphans
// ---------------------------------------------------------------------------

func TestGetOrphans_FindsOrphans(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	g := setupTestGraph(t, s)

	// A and D are orphans (nothing links to them).
	// B has backlink from A, C has backlinks from A and B.
	got, err := s.GetOrphans(ctx, nil)
	if err != nil {
		t.Fatalf("GetOrphans failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("GetOrphans: got %d notes, want 2", len(got))
	}

	paths := map[string]bool{}
	for _, n := range got {
		paths[n.Path] = true
	}
	if !paths[g.A.Path] {
		t.Errorf("GetOrphans missing A (orphan)")
	}
	if !paths[g.D.Path] {
		t.Errorf("GetOrphans missing D (orphan)")
	}
}

func TestGetOrphans_WithTypeFilter(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	g := setupTestGraph(t, s)

	// D has type "idea", A has type "plan".
	// Filter by type "idea" should return only D.
	got, err := s.GetOrphans(ctx, &OrphanOptions{Type: "idea"})
	if err != nil {
		t.Fatalf("GetOrphans(type=idea) failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("GetOrphans(type=idea): got %d notes, want 1", len(got))
	}
	if got[0].Path != g.D.Path {
		t.Errorf("GetOrphans(type=idea)[0].Path = %q, want %q", got[0].Path, g.D.Path)
	}
}

func TestGetOrphans_WithLimit(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	_ = setupTestGraph(t, s)

	// There are 2 orphans (A and D). Limit to 1.
	got, err := s.GetOrphans(ctx, &OrphanOptions{Limit: 1})
	if err != nil {
		t.Fatalf("GetOrphans(limit=1) failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("GetOrphans(limit=1): got %d notes, want 1", len(got))
	}
}

func TestGetOrphans_DefaultLimit(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	_ = setupTestGraph(t, s)

	// nil options should use default limit (50) and return all orphans.
	got, err := s.GetOrphans(ctx, nil)
	if err != nil {
		t.Fatalf("GetOrphans(nil) failed: %v", err)
	}
	// We have 2 orphans, well under the default limit of 50.
	if got == nil {
		t.Fatal("expected non-nil slice, got nil")
	}
	if len(got) != 2 {
		t.Errorf("GetOrphans(nil): got %d notes, want 2", len(got))
	}
}

func TestGetOrphans_EmptyDB(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// No notes at all — should return empty slice, not nil.
	got, err := s.GetOrphans(ctx, nil)
	if err != nil {
		t.Fatalf("GetOrphans on empty DB failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("GetOrphans on empty DB: got %d notes, want 0", len(got))
	}
}

func TestGetOrphans_AllNotesAreOrphans(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Create notes with no links between them — all are orphans.
	_, err := s.InsertNote(ctx, sampleNote("projects/test/orphan/x.md", "orpx0001", "X"))
	if err != nil {
		t.Fatalf("insert X: %v", err)
	}
	_, err = s.InsertNote(ctx, sampleNote("projects/test/orphan/y.md", "orpy0002", "Y"))
	if err != nil {
		t.Fatalf("insert Y: %v", err)
	}

	got, err := s.GetOrphans(ctx, nil)
	if err != nil {
		t.Fatalf("GetOrphans failed: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("GetOrphans: got %d notes, want 2", len(got))
	}
}
