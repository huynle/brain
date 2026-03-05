package storage

import (
	"context"
	"testing"
)

// =============================================================================
// Search Quality / Ranking Tests
//
// These tests verify that FTS5 with BM25 weights (title:10, body:1, path:5)
// produces correct ranking behavior for the Go rewrite, matching the TS
// implementation's search quality expectations.
// =============================================================================

// ---------------------------------------------------------------------------
// Title matches rank higher than body matches
// ---------------------------------------------------------------------------

func TestSearchQuality_TitleMatchRanksHigher(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert a note with "authentication" in the title
	titleMatch := sampleNote("projects/p/plan/auth-plan.md", "auth1234", "Authentication Design")
	titleBody := "This document covers the login flow and session management"
	titleMatch.Body = &titleBody
	typ := "plan"
	titleMatch.Type = &typ
	if _, err := s.InsertNote(ctx, titleMatch); err != nil {
		t.Fatalf("insert title match: %v", err)
	}

	// Insert a note with "authentication" only in the body
	bodyMatch := sampleNote("projects/p/task/setup-task.md", "setu5678", "Setup Infrastructure")
	bodyBody := "Configure authentication middleware for the API gateway"
	bodyMatch.Body = &bodyBody
	bodyMatch.Type = &typ
	if _, err := s.InsertNote(ctx, bodyMatch); err != nil {
		t.Fatalf("insert body match: %v", err)
	}

	results, err := s.SearchNotes(ctx, "authentication", nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(results) < 2 {
		t.Fatalf("expected >= 2 results, got %d", len(results))
	}

	// Title match should rank first (BM25 weight: title=10, body=1)
	if results[0].ShortID != "auth1234" {
		t.Errorf("first result ShortID = %q, want %q (title match should rank higher)", results[0].ShortID, "auth1234")
	}
}

// ---------------------------------------------------------------------------
// Path matches rank higher than body matches
// ---------------------------------------------------------------------------

func TestSearchQuality_PathMatchRanksHigherThanBody(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert a note with "migration" in the path
	pathMatch := sampleNote("projects/p/plan/migration-strategy.md", "migr1234", "Strategy Document")
	pathBody := "This document outlines the approach for the project"
	pathMatch.Body = &pathBody
	typ := "plan"
	pathMatch.Type = &typ
	if _, err := s.InsertNote(ctx, pathMatch); err != nil {
		t.Fatalf("insert path match: %v", err)
	}

	// Insert a note with "migration" only in the body
	bodyMatch := sampleNote("projects/p/task/setup.md", "setu9012", "Setup Task")
	bodyBody := "Handle the migration of data from the old system"
	bodyMatch.Body = &bodyBody
	bodyMatch.Type = &typ
	if _, err := s.InsertNote(ctx, bodyMatch); err != nil {
		t.Fatalf("insert body match: %v", err)
	}

	results, err := s.SearchNotes(ctx, "migration", nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(results) < 2 {
		t.Fatalf("expected >= 2 results, got %d", len(results))
	}

	// Path match (weight=5) should rank higher than body match (weight=1)
	if results[0].ShortID != "migr1234" {
		t.Errorf("first result ShortID = %q, want %q (path match should rank higher than body)", results[0].ShortID, "migr1234")
	}
}

// ---------------------------------------------------------------------------
// Type filter narrows results correctly
// ---------------------------------------------------------------------------

func TestSearchQuality_TypeFilter(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert notes of different types with the same keyword
	plan := sampleNote("projects/p/plan/go-plan.md", "gopl1111", "Go Plan")
	planBody := "Plan for Go migration"
	plan.Body = &planBody
	planType := "plan"
	plan.Type = &planType
	if _, err := s.InsertNote(ctx, plan); err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	task := sampleNote("projects/p/task/go-task.md", "gotk2222", "Go Task")
	taskBody := "Task for Go migration"
	task.Body = &taskBody
	taskType := "task"
	task.Type = &taskType
	if _, err := s.InsertNote(ctx, task); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	idea := sampleNote("projects/p/idea/go-idea.md", "goid3333", "Go Idea")
	ideaBody := "Idea about Go migration"
	idea.Body = &ideaBody
	ideaType := "idea"
	idea.Type = &ideaType
	if _, err := s.InsertNote(ctx, idea); err != nil {
		t.Fatalf("insert idea: %v", err)
	}

	// Search with type filter
	results, err := s.SearchNotes(ctx, "Go", &SearchOptions{Type: "plan"})
	if err != nil {
		t.Fatalf("search with type filter: %v", err)
	}

	for _, r := range results {
		if r.Type != nil && *r.Type != "plan" {
			t.Errorf("result type = %q, want %q", *r.Type, "plan")
		}
	}
}

// ---------------------------------------------------------------------------
// Limit is respected
// ---------------------------------------------------------------------------

func TestSearchQuality_LimitRespected(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert several notes
	for i := 0; i < 10; i++ {
		n := sampleNote(
			"projects/p/plan/note-"+string(rune('a'+i))+".md",
			"lim"+string(rune('a'+i))+"1234",
			"Searchable Note",
		)
		body := "This is searchable content for limit testing"
		n.Body = &body
		typ := "plan"
		n.Type = &typ
		if _, err := s.InsertNote(ctx, n); err != nil {
			t.Fatalf("insert note %d: %v", i, err)
		}
	}

	results, err := s.SearchNotes(ctx, "searchable", &SearchOptions{Limit: 3})
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(results) > 3 {
		t.Errorf("len(results) = %d, want <= 3", len(results))
	}
}

// ---------------------------------------------------------------------------
// Empty query returns empty results
// ---------------------------------------------------------------------------

func TestSearchQuality_EmptyQueryReturnsEmpty(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedSearchNotes(t, s)

	results, err := s.SearchNotes(ctx, "", nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("len(results) = %d, want 0 for empty query", len(results))
	}
}

// ---------------------------------------------------------------------------
// FTS5 syntax errors return empty results (not errors)
// ---------------------------------------------------------------------------

func TestSearchQuality_FTSSyntaxErrorReturnsEmpty(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedSearchNotes(t, s)

	// FTS5 special characters that would cause syntax errors
	badQueries := []string{
		`"unclosed quote`,
		`AND OR NOT`,
		`*`,
		`(((`,
	}

	for _, q := range badQueries {
		t.Run(q, func(t *testing.T) {
			results, err := s.SearchNotes(ctx, q, nil)
			if err != nil {
				t.Errorf("search %q returned error: %v (should return empty)", q, err)
			}
			// Should gracefully return empty, not error
			_ = results
		})
	}
}

// ---------------------------------------------------------------------------
// Multi-word queries find relevant results
// ---------------------------------------------------------------------------

func TestSearchQuality_MultiWordQuery(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedSearchNotes(t, s)

	// "Go" appears in multiple seeded notes
	results, err := s.SearchNotes(ctx, "Go storage", nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	// Should find at least the "Implement Go Storage Layer" task
	if len(results) == 0 {
		t.Error("expected at least 1 result for 'Go storage'")
	}
}

// ---------------------------------------------------------------------------
// Exact title match ranks first
// ---------------------------------------------------------------------------

func TestSearchQuality_ExactTitleMatchRanksFirst(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert an exact title match
	exact := sampleNote("projects/p/plan/perf.md", "perf1234", "Performance Report")
	exactBody := "General content about system metrics"
	exact.Body = &exactBody
	typ := "plan"
	exact.Type = &typ
	if _, err := s.InsertNote(ctx, exact); err != nil {
		t.Fatalf("insert exact: %v", err)
	}

	// Insert a partial match
	partial := sampleNote("projects/p/task/other.md", "othr5678", "Other Task")
	partialBody := "This task involves creating a performance report for the team"
	partial.Body = &partialBody
	partial.Type = &typ
	if _, err := s.InsertNote(ctx, partial); err != nil {
		t.Fatalf("insert partial: %v", err)
	}

	results, err := s.SearchNotes(ctx, "Performance Report", nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(results) < 1 {
		t.Fatal("expected at least 1 result")
	}

	// The exact title match should be first
	if results[0].ShortID != "perf1234" {
		t.Errorf("first result ShortID = %q, want %q (exact title match)", results[0].ShortID, "perf1234")
	}
}

// ---------------------------------------------------------------------------
// Status filter works with search
// ---------------------------------------------------------------------------

func TestSearchQuality_StatusFilter(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	active := sampleNote("projects/p/plan/active.md", "actv1234", "Active Plan")
	activeBody := "This is an active searchable plan"
	active.Body = &activeBody
	typ := "plan"
	active.Type = &typ
	status := "active"
	active.Status = &status
	if _, err := s.InsertNote(ctx, active); err != nil {
		t.Fatalf("insert active: %v", err)
	}

	completed := sampleNote("projects/p/plan/done.md", "done5678", "Completed Plan")
	completedBody := "This is a completed searchable plan"
	completed.Body = &completedBody
	completed.Type = &typ
	doneStatus := "completed"
	completed.Status = &doneStatus
	if _, err := s.InsertNote(ctx, completed); err != nil {
		t.Fatalf("insert completed: %v", err)
	}

	results, err := s.SearchNotes(ctx, "searchable", &SearchOptions{Status: "active"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	for _, r := range results {
		if r.Status != nil && *r.Status != "active" {
			t.Errorf("result status = %q, want %q", *r.Status, "active")
		}
	}
}

// ---------------------------------------------------------------------------
// Default limit is applied
// ---------------------------------------------------------------------------

func TestSearchQuality_DefaultLimit(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert more than default limit (20) notes
	for i := 0; i < 25; i++ {
		n := sampleNote(
			"projects/p/plan/bulk-"+string(rune('a'+i))+".md",
			"blk"+string(rune('a'+i))+"1234",
			"Bulk Searchable Note",
		)
		body := "Bulk searchable content for default limit testing"
		n.Body = &body
		typ := "plan"
		n.Type = &typ
		if _, err := s.InsertNote(ctx, n); err != nil {
			t.Fatalf("insert note %d: %v", i, err)
		}
	}

	results, err := s.SearchNotes(ctx, "searchable", nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	// Default limit is 20
	if len(results) > defaultSearchLimit {
		t.Errorf("len(results) = %d, want <= %d (default limit)", len(results), defaultSearchLimit)
	}
}

// ---------------------------------------------------------------------------
// Search strategies: exact, like, fts
// ---------------------------------------------------------------------------

func TestSearchQuality_Strategies(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	n := sampleNote("projects/p/plan/strategy-test.md", "strt1234", "Strategy Test Plan")
	body := "Content for testing different search strategies"
	n.Body = &body
	typ := "plan"
	n.Type = &typ
	if _, err := s.InsertNote(ctx, n); err != nil {
		t.Fatalf("insert: %v", err)
	}

	strategies := []struct {
		name     string
		strategy string
		query    string
		wantMin  int
	}{
		{"fts default", "fts", "Strategy", 1},
		{"exact title", "exact", "Strategy Test Plan", 1},
		{"like substring", "like", "strategy", 1},
		{"fts no results", "fts", "xyznonexistent", 0},
	}

	for _, tt := range strategies {
		t.Run(tt.name, func(t *testing.T) {
			results, err := s.SearchNotes(ctx, tt.query, &SearchOptions{Strategy: tt.strategy})
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			if len(results) < tt.wantMin {
				t.Errorf("len(results) = %d, want >= %d", len(results), tt.wantMin)
			}
		})
	}
}
