package storage

import (
	"context"
	"fmt"
	"testing"
)

// ---------------------------------------------------------------------------
// Helper: seed notes for search tests
// ---------------------------------------------------------------------------

// seedSearchNotes inserts a set of notes useful for search testing.
// Returns the inserted notes keyed by a short label.
func seedSearchNotes(t *testing.T, s *StorageLayer) map[string]*NoteRow {
	t.Helper()
	ctx := context.Background()

	notes := map[string]*NoteRow{
		"go-plan": func() *NoteRow {
			n := sampleNote("projects/myproj/plan/go-plan.md", "gopl1234", "Go Migration Plan")
			body := "Migrate the TypeScript codebase to Go for better performance"
			n.Body = &body
			typ := "plan"
			n.Type = &typ
			status := "active"
			n.Status = &status
			return n
		}(),
		"ts-summary": func() *NoteRow {
			n := sampleNote("projects/myproj/summary/ts-summary.md", "tsum5678", "TypeScript Summary")
			body := "Summary of the TypeScript architecture and patterns"
			n.Body = &body
			typ := "summary"
			n.Type = &typ
			status := "completed"
			n.Status = &status
			return n
		}(),
		"rust-idea": func() *NoteRow {
			n := sampleNote("projects/other/idea/rust-idea.md", "rust9012", "Rust Exploration")
			body := "Exploring Rust as an alternative to Go for systems programming"
			n.Body = &body
			typ := "idea"
			n.Type = &typ
			status := "draft"
			n.Status = &status
			return n
		}(),
		"go-task": func() *NoteRow {
			n := sampleNote("projects/myproj/task/go-task.md", "gotk3456", "Implement Go Storage Layer")
			body := "Task to implement the storage layer in Go with SQLite"
			n.Body = &body
			typ := "task"
			n.Type = &typ
			status := "active"
			n.Status = &status
			return n
		}(),
		"perf-report": func() *NoteRow {
			n := sampleNote("projects/myproj/report/perf.md", "perf7890", "Performance Report")
			body := "Benchmarks show Go is 3x faster than TypeScript for this workload"
			n.Body = &body
			typ := "report"
			n.Type = &typ
			status := "completed"
			n.Status = &status
			return n
		}(),
	}

	result := make(map[string]*NoteRow, len(notes))
	for label, note := range notes {
		inserted, err := s.InsertNote(ctx, note)
		if err != nil {
			t.Fatalf("seed note %q: %v", label, err)
		}
		result[label] = inserted
	}
	return result
}

// ---------------------------------------------------------------------------
// SearchNotes: empty/blank query returns empty slice
// ---------------------------------------------------------------------------

func TestSearchNotes_EmptyQuery(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedSearchNotes(t, s)

	tests := []struct {
		name  string
		query string
	}{
		{"empty string", ""},
		{"whitespace only", "   "},
		{"tabs and spaces", " \t\n "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := s.SearchNotes(ctx, tt.query, nil)
			if err != nil {
				t.Fatalf("SearchNotes(%q) error: %v", tt.query, err)
			}
			if len(results) != 0 {
				t.Errorf("SearchNotes(%q) returned %d results, want 0", tt.query, len(results))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SearchNotes: default strategy is FTS
// ---------------------------------------------------------------------------

func TestSearchNotes_DefaultStrategyIsFTS(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedSearchNotes(t, s)

	// nil options should default to FTS strategy
	results, err := s.SearchNotes(ctx, "Go", nil)
	if err != nil {
		t.Fatalf("SearchNotes error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results with default FTS strategy, got 0")
	}
}

// ---------------------------------------------------------------------------
// FTS search: finds by title
// ---------------------------------------------------------------------------

func TestSearchFTS_FindsByTitle(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedSearchNotes(t, s)

	results, err := s.SearchNotes(ctx, "Migration", &SearchOptions{Strategy: "fts"})
	if err != nil {
		t.Fatalf("SearchNotes error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for title match 'Migration'")
	}

	found := false
	for _, r := range results {
		if r.Title == "Go Migration Plan" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'Go Migration Plan' in results")
	}
}

// ---------------------------------------------------------------------------
// FTS search: finds by body
// ---------------------------------------------------------------------------

func TestSearchFTS_FindsByBody(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedSearchNotes(t, s)

	results, err := s.SearchNotes(ctx, "Benchmarks", &SearchOptions{Strategy: "fts"})
	if err != nil {
		t.Fatalf("SearchNotes error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for body match 'Benchmarks'")
	}

	found := false
	for _, r := range results {
		if r.Title == "Performance Report" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'Performance Report' in results for body search 'Benchmarks'")
	}
}

// ---------------------------------------------------------------------------
// FTS search: ranks title higher than body (BM25 weights)
// ---------------------------------------------------------------------------

func TestSearchFTS_TitleRankedHigher(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert two notes: one with "Go" in title, one with "Go" only in body
	titleNote := sampleNote("projects/test/plan/title-go.md", "titl1234", "Go Programming Guide")
	titleBody := "A comprehensive guide"
	titleNote.Body = &titleBody
	_, err := s.InsertNote(ctx, titleNote)
	if err != nil {
		t.Fatalf("insert title note: %v", err)
	}

	bodyNote := sampleNote("projects/test/plan/body-go.md", "body5678", "Programming Guide")
	bodyBody := "This guide covers Go and its ecosystem"
	bodyNote.Body = &bodyBody
	_, err = s.InsertNote(ctx, bodyNote)
	if err != nil {
		t.Fatalf("insert body note: %v", err)
	}

	results, err := s.SearchNotes(ctx, "Go", &SearchOptions{Strategy: "fts"})
	if err != nil {
		t.Fatalf("SearchNotes error: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}

	// First result should be the one with "Go" in the title (higher BM25 weight)
	if results[0].Title != "Go Programming Guide" {
		t.Errorf("first result title = %q, want %q (title match should rank higher)", results[0].Title, "Go Programming Guide")
	}
}

// ---------------------------------------------------------------------------
// FTS search: respects limit
// ---------------------------------------------------------------------------

func TestSearchFTS_RespectsLimit(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedSearchNotes(t, s)

	// "Go" appears in multiple notes; limit to 2
	results, err := s.SearchNotes(ctx, "Go", &SearchOptions{Strategy: "fts", Limit: 2})
	if err != nil {
		t.Fatalf("SearchNotes error: %v", err)
	}
	if len(results) > 2 {
		t.Errorf("expected at most 2 results with Limit=2, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// FTS search: default limit is 20
// ---------------------------------------------------------------------------

func TestSearchFTS_DefaultLimit(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert 25 notes all matching "common"
	for i := 0; i < 25; i++ {
		n := &NoteRow{
			Path:     fmt.Sprintf("projects/test/plan/note-%d.md", i),
			ShortID:  fmt.Sprintf("n%06d", i),
			Title:    fmt.Sprintf("Common Note %d", i),
			Metadata: "{}",
		}
		_, err := s.InsertNote(ctx, n)
		if err != nil {
			t.Fatalf("insert note %d: %v", i, err)
		}
	}

	results, err := s.SearchNotes(ctx, "Common", &SearchOptions{Strategy: "fts"})
	if err != nil {
		t.Fatalf("SearchNotes error: %v", err)
	}
	if len(results) != 20 {
		t.Errorf("expected 20 results (default limit), got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// FTS search with filters: path prefix
// ---------------------------------------------------------------------------

func TestSearchFTS_FilterPathPrefix(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedSearchNotes(t, s)

	// "Go" appears in notes under both projects/myproj and projects/other
	results, err := s.SearchNotes(ctx, "Go", &SearchOptions{
		Strategy:   "fts",
		PathPrefix: "projects/myproj/",
	})
	if err != nil {
		t.Fatalf("SearchNotes error: %v", err)
	}

	for _, r := range results {
		if r.Path[:len("projects/myproj/")] != "projects/myproj/" {
			t.Errorf("result path %q does not start with 'projects/myproj/'", r.Path)
		}
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result with path prefix filter")
	}
}

// ---------------------------------------------------------------------------
// FTS search with filters: type
// ---------------------------------------------------------------------------

func TestSearchFTS_FilterType(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedSearchNotes(t, s)

	results, err := s.SearchNotes(ctx, "Go", &SearchOptions{
		Strategy: "fts",
		Type:     "plan",
	})
	if err != nil {
		t.Fatalf("SearchNotes error: %v", err)
	}

	for _, r := range results {
		if r.Type == nil || *r.Type != "plan" {
			t.Errorf("result type = %v, want 'plan'", r.Type)
		}
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result with type filter")
	}
}

// ---------------------------------------------------------------------------
// FTS search with filters: status
// ---------------------------------------------------------------------------

func TestSearchFTS_FilterStatus(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedSearchNotes(t, s)

	results, err := s.SearchNotes(ctx, "Go", &SearchOptions{
		Strategy: "fts",
		Status:   "active",
	})
	if err != nil {
		t.Fatalf("SearchNotes error: %v", err)
	}

	for _, r := range results {
		if r.Status == nil || *r.Status != "active" {
			t.Errorf("result status = %v, want 'active'", r.Status)
		}
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result with status filter")
	}
}

// ---------------------------------------------------------------------------
// FTS search with combined filters
// ---------------------------------------------------------------------------

func TestSearchFTS_CombinedFilters(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedSearchNotes(t, s)

	results, err := s.SearchNotes(ctx, "Go", &SearchOptions{
		Strategy:   "fts",
		PathPrefix: "projects/myproj/",
		Type:       "task",
		Status:     "active",
	})
	if err != nil {
		t.Fatalf("SearchNotes error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result with combined filters, got %d", len(results))
	}
	if results[0].Title != "Implement Go Storage Layer" {
		t.Errorf("title = %q, want %q", results[0].Title, "Implement Go Storage Layer")
	}
}

// ---------------------------------------------------------------------------
// FTS search: bad syntax returns empty (not error)
// ---------------------------------------------------------------------------

func TestSearchFTS_BadSyntaxReturnsEmpty(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedSearchNotes(t, s)

	// FTS5 syntax errors (unmatched quotes, invalid operators)
	badQueries := []string{
		`"unclosed quote`,
		`AND OR NOT`,
		`(((`,
	}

	for _, q := range badQueries {
		t.Run(q, func(t *testing.T) {
			results, err := s.SearchNotes(ctx, q, &SearchOptions{Strategy: "fts"})
			if err != nil {
				t.Fatalf("SearchNotes(%q) should not return error, got: %v", q, err)
			}
			// Empty results are acceptable for bad syntax
			_ = results
		})
	}
}

// ---------------------------------------------------------------------------
// Exact search: exact title match
// ---------------------------------------------------------------------------

func TestSearchExact_TitleMatch(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedSearchNotes(t, s)

	results, err := s.SearchNotes(ctx, "Go Migration Plan", &SearchOptions{Strategy: "exact"})
	if err != nil {
		t.Fatalf("SearchNotes error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for exact title match")
	}

	found := false
	for _, r := range results {
		if r.Title == "Go Migration Plan" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'Go Migration Plan' in exact search results")
	}
}

// ---------------------------------------------------------------------------
// Exact search: body substring
// ---------------------------------------------------------------------------

func TestSearchExact_BodySubstring(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedSearchNotes(t, s)

	results, err := s.SearchNotes(ctx, "Benchmarks show", &SearchOptions{Strategy: "exact"})
	if err != nil {
		t.Fatalf("SearchNotes error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for body substring match")
	}

	found := false
	for _, r := range results {
		if r.Title == "Performance Report" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'Performance Report' in exact search results for body substring")
	}
}

// ---------------------------------------------------------------------------
// Exact search: with filters
// ---------------------------------------------------------------------------

func TestSearchExact_WithFilters(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedSearchNotes(t, s)

	// Search for "Go" in body — multiple notes have it, but filter to type=report
	results, err := s.SearchNotes(ctx, "Go", &SearchOptions{
		Strategy:   "exact",
		PathPrefix: "projects/myproj/",
		Type:       "report",
		Status:     "completed",
	})
	if err != nil {
		t.Fatalf("SearchNotes error: %v", err)
	}

	// Only the performance report should match (body contains "Go", type=report, status=completed)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "Performance Report" {
		t.Errorf("title = %q, want %q", results[0].Title, "Performance Report")
	}
}

// ---------------------------------------------------------------------------
// Like search: finds across title/body/path
// ---------------------------------------------------------------------------

func TestSearchLike_FindsByTitle(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedSearchNotes(t, s)

	results, err := s.SearchNotes(ctx, "Migration", &SearchOptions{Strategy: "like"})
	if err != nil {
		t.Fatalf("SearchNotes error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for LIKE title match")
	}

	found := false
	for _, r := range results {
		if r.Title == "Go Migration Plan" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'Go Migration Plan' in LIKE results")
	}
}

func TestSearchLike_FindsByBody(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedSearchNotes(t, s)

	results, err := s.SearchNotes(ctx, "Benchmarks", &SearchOptions{Strategy: "like"})
	if err != nil {
		t.Fatalf("SearchNotes error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for LIKE body match")
	}

	found := false
	for _, r := range results {
		if r.Title == "Performance Report" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'Performance Report' in LIKE body results")
	}
}

func TestSearchLike_FindsByPath(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedSearchNotes(t, s)

	// Search for a path fragment
	results, err := s.SearchNotes(ctx, "rust-idea", &SearchOptions{Strategy: "like"})
	if err != nil {
		t.Fatalf("SearchNotes error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for LIKE path match")
	}

	found := false
	for _, r := range results {
		if r.Title == "Rust Exploration" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'Rust Exploration' in LIKE path results")
	}
}

// ---------------------------------------------------------------------------
// Like search: with filters
// ---------------------------------------------------------------------------

func TestSearchLike_WithFilters(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedSearchNotes(t, s)

	results, err := s.SearchNotes(ctx, "Go", &SearchOptions{
		Strategy:   "like",
		PathPrefix: "projects/myproj/",
		Type:       "plan",
		Status:     "active",
	})
	if err != nil {
		t.Fatalf("SearchNotes error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result with combined LIKE filters, got %d", len(results))
	}
	if results[0].Title != "Go Migration Plan" {
		t.Errorf("title = %q, want %q", results[0].Title, "Go Migration Plan")
	}
}

// ---------------------------------------------------------------------------
// All strategies support nil options (defaults)
// ---------------------------------------------------------------------------

func TestSearchNotes_NilOptions(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedSearchNotes(t, s)

	results, err := s.SearchNotes(ctx, "TypeScript", nil)
	if err != nil {
		t.Fatalf("SearchNotes with nil opts error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results with nil options (default FTS)")
	}
}

// ---------------------------------------------------------------------------
// Unknown strategy defaults to FTS
// ---------------------------------------------------------------------------

func TestSearchNotes_UnknownStrategyDefaultsToFTS(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedSearchNotes(t, s)

	results, err := s.SearchNotes(ctx, "TypeScript", &SearchOptions{Strategy: "bogus"})
	if err != nil {
		t.Fatalf("SearchNotes with unknown strategy error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results with unknown strategy (should default to FTS)")
	}
}

// ---------------------------------------------------------------------------
// SearchNotes: filter by project_id
// ---------------------------------------------------------------------------

func TestSearchNotes_FilterByProjectID(t *testing.T) {
	s := newTestStorage(t)
	seedSearchNotes(t, s)

	// All seeded notes have "Go" or "TypeScript" in body.
	// "myproj" notes: go-plan, ts-summary, go-task, perf-report
	// "other" notes: rust-idea
	results, err := s.SearchNotes(context.Background(), "Go", &SearchOptions{
		ProjectID: "myproj",
	})
	if err != nil {
		t.Fatalf("SearchNotes with ProjectID error: %v", err)
	}
	for _, r := range results {
		if r.ProjectID == nil || *r.ProjectID != "myproj" {
			t.Errorf("expected project_id=myproj, got %v for path %s", r.ProjectID, r.Path)
		}
	}
	// rust-idea mentions "Go" but is in project "other" — should be excluded
	for _, r := range results {
		if r.ShortID == "rust9012" {
			t.Error("rust-idea should be excluded by ProjectID=myproj filter")
		}
	}
}

// ---------------------------------------------------------------------------
// SearchNotes: filter by feature_id
// ---------------------------------------------------------------------------

func TestSearchNotes_FilterByFeatureID(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Seed notes with feature_id set
	strPtr := func(s string) *string { return &s }
	notes := []*NoteRow{
		{
			Path: "projects/proj/task/a.md", ShortID: "feat0001",
			Title: "Feature A Task", Metadata: "{}",
			Type: strPtr("task"), Status: strPtr("active"),
			ProjectID: strPtr("proj"), FeatureID: strPtr("auth"),
		},
		{
			Path: "projects/proj/task/b.md", ShortID: "feat0002",
			Title: "Feature B Task", Metadata: "{}",
			Type: strPtr("task"), Status: strPtr("active"),
			ProjectID: strPtr("proj"), FeatureID: strPtr("billing"),
		},
	}
	for _, n := range notes {
		body := "This is a task for the feature"
		n.Body = &body
		if _, err := s.InsertNote(ctx, n); err != nil {
			t.Fatalf("insert note: %v", err)
		}
	}

	results, err := s.SearchNotes(ctx, "task", &SearchOptions{
		FeatureID: "auth",
	})
	if err != nil {
		t.Fatalf("SearchNotes with FeatureID error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ShortID != "feat0001" {
		t.Errorf("expected feat0001, got %s", results[0].ShortID)
	}
}

// ---------------------------------------------------------------------------
// SearchNotes: filter by priority
// ---------------------------------------------------------------------------

func TestSearchNotes_FilterByPriority(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	strPtr := func(s string) *string { return &s }
	notes := []*NoteRow{
		{
			Path: "projects/proj/task/hi.md", ShortID: "prio0001",
			Title: "High Priority Task", Metadata: "{}",
			Type: strPtr("task"), Status: strPtr("active"),
			Priority: strPtr("high"),
		},
		{
			Path: "projects/proj/task/lo.md", ShortID: "prio0002",
			Title: "Low Priority Task", Metadata: "{}",
			Type: strPtr("task"), Status: strPtr("active"),
			Priority: strPtr("low"),
		},
	}
	for _, n := range notes {
		body := "This is a priority task"
		n.Body = &body
		if _, err := s.InsertNote(ctx, n); err != nil {
			t.Fatalf("insert note: %v", err)
		}
	}

	results, err := s.SearchNotes(ctx, "priority task", &SearchOptions{
		Priority: "high",
	})
	if err != nil {
		t.Fatalf("SearchNotes with Priority error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ShortID != "prio0001" {
		t.Errorf("expected prio0001, got %s", results[0].ShortID)
	}
}

// ---------------------------------------------------------------------------
// SearchNotes: filter by tags
// ---------------------------------------------------------------------------

func TestSearchNotes_FilterByTags(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	strPtr := func(s string) *string { return &s }
	notes := []*NoteRow{
		{
			Path: "projects/proj/task/tagged1.md", ShortID: "tags0001",
			Title: "Tagged Task One", Metadata: "{}",
			Type: strPtr("task"), Status: strPtr("active"),
		},
		{
			Path: "projects/proj/task/tagged2.md", ShortID: "tags0002",
			Title: "Tagged Task Two", Metadata: "{}",
			Type: strPtr("task"), Status: strPtr("active"),
		},
	}
	for _, n := range notes {
		body := "This is a tagged task"
		n.Body = &body
		if _, err := s.InsertNote(ctx, n); err != nil {
			t.Fatalf("insert note: %v", err)
		}
	}

	// Set tags
	if err := s.SetTags(ctx, "projects/proj/task/tagged1.md", []string{"go", "api"}); err != nil {
		t.Fatalf("set tags: %v", err)
	}
	if err := s.SetTags(ctx, "projects/proj/task/tagged2.md", []string{"go", "cli"}); err != nil {
		t.Fatalf("set tags: %v", err)
	}

	// Filter by single tag "api" — should match only tagged1
	results, err := s.SearchNotes(ctx, "tagged task", &SearchOptions{
		Tags: []string{"api"},
	})
	if err != nil {
		t.Fatalf("SearchNotes with Tags error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for tag 'api', got %d", len(results))
	}
	if results[0].ShortID != "tags0001" {
		t.Errorf("expected tags0001, got %s", results[0].ShortID)
	}

	// Filter by tags ["go", "cli"] — should match only tagged2 (AND logic)
	results, err = s.SearchNotes(ctx, "tagged task", &SearchOptions{
		Tags: []string{"go", "cli"},
	})
	if err != nil {
		t.Fatalf("SearchNotes with multiple Tags error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for tags [go, cli], got %d", len(results))
	}
	if results[0].ShortID != "tags0002" {
		t.Errorf("expected tags0002, got %s", results[0].ShortID)
	}
}

func TestSearchNotes_FindsAttachmentDerivedText(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/proj/report/attachment-derived.md", "adtx0001", "Attachment Derived Entry")
	body := "body without the attachment-only token"
	note.Body = &body
	typ := "report"
	note.Type = &typ
	if _, err := s.InsertNote(ctx, note); err != nil {
		t.Fatalf("insert note: %v", err)
	}

	attachment, err := s.CreateAttachment(ctx, AttachmentInput{
		Digest:    "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Size:      128,
		MediaType: "application/pdf",
		Metadata:  `{"filename":"source.pdf","derived_text":"needleattachmenttoken appears only in extracted attachment text"}`,
	})
	if err != nil {
		t.Fatalf("CreateAttachment failed: %v", err)
	}
	if err := s.LinkAttachmentToEntry(ctx, note.Path, attachment.ID, "source"); err != nil {
		t.Fatalf("LinkAttachmentToEntry failed: %v", err)
	}

	results, err := s.SearchNotes(ctx, "needleattachmenttoken", nil)
	if err != nil {
		t.Fatalf("SearchNotes error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("SearchNotes returned %d results, want 1 attachment-derived match", len(results))
	}
	if results[0].ShortID != "adtx0001" {
		t.Fatalf("SearchNotes result short_id = %q, want adtx0001", results[0].ShortID)
	}

	if results[0].MatchSource != "attachment" {
		t.Fatalf("MatchSource = %q, want attachment", results[0].MatchSource)
	}
}
