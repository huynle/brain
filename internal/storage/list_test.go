package storage

import (
	"context"
	"fmt"
	"testing"
)

// ---------------------------------------------------------------------------
// Helper: seed notes for list/filter tests
// ---------------------------------------------------------------------------

// seedListNotes inserts a diverse set of notes for list/filter testing.
// Returns the inserted notes keyed by a short label.
func seedListNotes(t *testing.T, s *StorageLayer) map[string]*NoteRow {
	t.Helper()
	ctx := context.Background()

	strPtr := func(s string) *string { return &s }

	notes := []struct {
		label string
		note  *NoteRow
	}{
		{
			label: "plan-active",
			note: &NoteRow{
				Path: "projects/alpha/plan/roadmap.md", ShortID: "plan0001",
				Title: "Alpha Roadmap", Metadata: "{}",
				Type: strPtr("plan"), Status: strPtr("active"),
				Priority: strPtr("high"), ProjectID: strPtr("alpha"),
				FeatureID: strPtr("feat-auth"),
				Created:   strPtr("2025-01-01T00:00:00Z"), Modified: strPtr("2025-01-10T00:00:00Z"),
			},
		},
		{
			label: "task-active",
			note: &NoteRow{
				Path: "projects/alpha/task/implement.md", ShortID: "task0001",
				Title: "Implement Auth", Metadata: "{}",
				Type: strPtr("task"), Status: strPtr("active"),
				Priority: strPtr("medium"), ProjectID: strPtr("alpha"),
				FeatureID: strPtr("feat-auth"),
				Created:   strPtr("2025-01-02T00:00:00Z"), Modified: strPtr("2025-01-09T00:00:00Z"),
			},
		},
		{
			label: "summary-completed",
			note: &NoteRow{
				Path: "projects/alpha/summary/review.md", ShortID: "summ0001",
				Title: "Code Review Summary", Metadata: "{}",
				Type: strPtr("summary"), Status: strPtr("completed"),
				Priority: strPtr("low"), ProjectID: strPtr("alpha"),
				FeatureID: strPtr("feat-api"),
				Created:   strPtr("2025-01-03T00:00:00Z"), Modified: strPtr("2025-01-08T00:00:00Z"),
			},
		},
		{
			label: "idea-draft",
			note: &NoteRow{
				Path: "projects/beta/idea/cache.md", ShortID: "idea0001",
				Title: "Caching Strategy", Metadata: "{}",
				Type: strPtr("idea"), Status: strPtr("draft"),
				Priority: strPtr("medium"), ProjectID: strPtr("beta"),
				FeatureID: strPtr("feat-perf"),
				Created:   strPtr("2025-01-04T00:00:00Z"), Modified: strPtr("2025-01-07T00:00:00Z"),
			},
		},
		{
			label: "report-completed",
			note: &NoteRow{
				Path: "projects/beta/report/perf.md", ShortID: "rept0001",
				Title: "Performance Report", Metadata: "{}",
				Type: strPtr("report"), Status: strPtr("completed"),
				Priority: strPtr("high"), ProjectID: strPtr("beta"),
				FeatureID: strPtr("feat-perf"),
				Created:   strPtr("2025-01-05T00:00:00Z"), Modified: strPtr("2025-01-06T00:00:00Z"),
			},
		},
	}

	result := make(map[string]*NoteRow, len(notes))
	for _, item := range notes {
		inserted, err := s.InsertNote(ctx, item.note)
		if err != nil {
			t.Fatalf("seed note %q: %v", item.label, err)
		}
		result[item.label] = inserted
	}

	// Add tags for tag-filter tests.
	// plan-active: go, tdd
	// task-active: go, storage
	// summary-completed: review
	// idea-draft: go, cache
	// report-completed: perf
	if err := s.SetTags(ctx, "projects/alpha/plan/roadmap.md", []string{"go", "tdd"}); err != nil {
		t.Fatalf("set tags plan-active: %v", err)
	}
	if err := s.SetTags(ctx, "projects/alpha/task/implement.md", []string{"go", "storage"}); err != nil {
		t.Fatalf("set tags task-active: %v", err)
	}
	if err := s.SetTags(ctx, "projects/alpha/summary/review.md", []string{"review"}); err != nil {
		t.Fatalf("set tags summary-completed: %v", err)
	}
	if err := s.SetTags(ctx, "projects/beta/idea/cache.md", []string{"go", "cache"}); err != nil {
		t.Fatalf("set tags idea-draft: %v", err)
	}
	if err := s.SetTags(ctx, "projects/beta/report/perf.md", []string{"perf"}); err != nil {
		t.Fatalf("set tags report-completed: %v", err)
	}

	return result
}

// ---------------------------------------------------------------------------
// ListNotes: nil options returns all with defaults
// ---------------------------------------------------------------------------

func TestListNotes_NilOptions(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedListNotes(t, s)

	notes, err := s.ListNotes(ctx, nil)
	if err != nil {
		t.Fatalf("ListNotes(nil) error: %v", err)
	}
	if len(notes) != 5 {
		t.Errorf("expected 5 notes, got %d", len(notes))
	}
}

// ---------------------------------------------------------------------------
// ListNotes: no filters returns all with default sort (modified desc)
// ---------------------------------------------------------------------------

func TestListNotes_NoFilters(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedListNotes(t, s)

	notes, err := s.ListNotes(ctx, &ListOptions{})
	if err != nil {
		t.Fatalf("ListNotes error: %v", err)
	}
	if len(notes) != 5 {
		t.Errorf("expected 5 notes, got %d", len(notes))
	}

	// Default sort is modified DESC — first note should have latest modified
	if len(notes) > 0 && notes[0].Title != "Alpha Roadmap" {
		t.Errorf("first note title = %q, want %q (latest modified)", notes[0].Title, "Alpha Roadmap")
	}
}

// ---------------------------------------------------------------------------
// ListNotes: filter by type
// ---------------------------------------------------------------------------

func TestListNotes_FilterByType(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedListNotes(t, s)

	tests := []struct {
		typ       string
		wantCount int
	}{
		{"plan", 1},
		{"task", 1},
		{"summary", 1},
		{"idea", 1},
		{"report", 1},
		{"nonexistent", 0},
	}

	for _, tt := range tests {
		t.Run(tt.typ, func(t *testing.T) {
			notes, err := s.ListNotes(ctx, &ListOptions{Type: tt.typ})
			if err != nil {
				t.Fatalf("ListNotes error: %v", err)
			}
			if len(notes) != tt.wantCount {
				t.Errorf("type=%q: got %d notes, want %d", tt.typ, len(notes), tt.wantCount)
			}
			for _, n := range notes {
				if n.Type == nil || *n.Type != tt.typ {
					t.Errorf("note type = %v, want %q", n.Type, tt.typ)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ListNotes: filter by status
// ---------------------------------------------------------------------------

func TestListNotes_FilterByStatus(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedListNotes(t, s)

	tests := []struct {
		status    string
		wantCount int
	}{
		{"active", 2},
		{"completed", 2},
		{"draft", 1},
		{"archived", 0},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			notes, err := s.ListNotes(ctx, &ListOptions{Status: tt.status})
			if err != nil {
				t.Fatalf("ListNotes error: %v", err)
			}
			if len(notes) != tt.wantCount {
				t.Errorf("status=%q: got %d notes, want %d", tt.status, len(notes), tt.wantCount)
			}
			for _, n := range notes {
				if n.Status == nil || *n.Status != tt.status {
					t.Errorf("note status = %v, want %q", n.Status, tt.status)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ListNotes: filter by project_id
// ---------------------------------------------------------------------------

func TestListNotes_FilterByProjectID(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedListNotes(t, s)

	tests := []struct {
		project   string
		wantCount int
	}{
		{"alpha", 3},
		{"beta", 2},
		{"gamma", 0},
	}

	for _, tt := range tests {
		t.Run(tt.project, func(t *testing.T) {
			notes, err := s.ListNotes(ctx, &ListOptions{ProjectID: tt.project})
			if err != nil {
				t.Fatalf("ListNotes error: %v", err)
			}
			if len(notes) != tt.wantCount {
				t.Errorf("project=%q: got %d notes, want %d", tt.project, len(notes), tt.wantCount)
			}
			for _, n := range notes {
				if n.ProjectID == nil || *n.ProjectID != tt.project {
					t.Errorf("note project_id = %v, want %q", n.ProjectID, tt.project)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ListNotes: filter by feature_id
// ---------------------------------------------------------------------------

func TestListNotes_FilterByFeatureID(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedListNotes(t, s)

	tests := []struct {
		feature   string
		wantCount int
	}{
		{"feat-auth", 2},
		{"feat-api", 1},
		{"feat-perf", 2},
		{"feat-none", 0},
	}

	for _, tt := range tests {
		t.Run(tt.feature, func(t *testing.T) {
			notes, err := s.ListNotes(ctx, &ListOptions{FeatureID: tt.feature})
			if err != nil {
				t.Fatalf("ListNotes error: %v", err)
			}
			if len(notes) != tt.wantCount {
				t.Errorf("feature=%q: got %d notes, want %d", tt.feature, len(notes), tt.wantCount)
			}
			for _, n := range notes {
				if n.FeatureID == nil || *n.FeatureID != tt.feature {
					t.Errorf("note feature_id = %v, want %q", n.FeatureID, tt.feature)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ListNotes: filter by path prefix
// ---------------------------------------------------------------------------

func TestListNotes_FilterByPathPrefix(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedListNotes(t, s)

	tests := []struct {
		prefix    string
		wantCount int
	}{
		{"projects/alpha/", 3},
		{"projects/beta/", 2},
		{"projects/alpha/plan/", 1},
		{"projects/", 5},
		{"nonexistent/", 0},
	}

	for _, tt := range tests {
		t.Run(tt.prefix, func(t *testing.T) {
			notes, err := s.ListNotes(ctx, &ListOptions{PathPrefix: tt.prefix})
			if err != nil {
				t.Fatalf("ListNotes error: %v", err)
			}
			if len(notes) != tt.wantCount {
				t.Errorf("prefix=%q: got %d notes, want %d", tt.prefix, len(notes), tt.wantCount)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ListNotes: filter by single tag (subquery)
// ---------------------------------------------------------------------------

func TestListNotes_FilterBySingleTag(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedListNotes(t, s)

	tests := []struct {
		tag       string
		wantCount int
	}{
		{"go", 3},       // plan-active, task-active, idea-draft
		{"tdd", 1},      // plan-active
		{"storage", 1},  // task-active
		{"review", 1},   // summary-completed
		{"cache", 1},    // idea-draft
		{"perf", 1},     // report-completed
		{"nonexist", 0}, // no match
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			notes, err := s.ListNotes(ctx, &ListOptions{Tag: tt.tag})
			if err != nil {
				t.Fatalf("ListNotes error: %v", err)
			}
			if len(notes) != tt.wantCount {
				t.Errorf("tag=%q: got %d notes, want %d", tt.tag, len(notes), tt.wantCount)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ListNotes: filter by multiple tags (all must match — HAVING COUNT)
// ---------------------------------------------------------------------------

func TestListNotes_FilterByMultipleTags(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedListNotes(t, s)

	tests := []struct {
		name      string
		tags      []string
		wantCount int
	}{
		{"go+tdd", []string{"go", "tdd"}, 1},                 // only plan-active has both
		{"go+storage", []string{"go", "storage"}, 1},         // only task-active has both
		{"go+cache", []string{"go", "cache"}, 1},             // only idea-draft has both
		{"go+perf", []string{"go", "perf"}, 0},               // no note has both
		{"tdd+storage", []string{"go", "tdd", "storage"}, 0}, // no note has all three
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notes, err := s.ListNotes(ctx, &ListOptions{Tags: tt.tags})
			if err != nil {
				t.Fatalf("ListNotes error: %v", err)
			}
			if len(notes) != tt.wantCount {
				t.Errorf("tags=%v: got %d notes, want %d", tt.tags, len(notes), tt.wantCount)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ListNotes: combined filters
// ---------------------------------------------------------------------------

func TestListNotes_CombinedFilters(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedListNotes(t, s)

	// Active notes in alpha project
	notes, err := s.ListNotes(ctx, &ListOptions{
		Status:    "active",
		ProjectID: "alpha",
	})
	if err != nil {
		t.Fatalf("ListNotes error: %v", err)
	}
	if len(notes) != 2 {
		t.Errorf("expected 2 active alpha notes, got %d", len(notes))
	}

	// Completed notes with path prefix projects/beta/
	notes, err = s.ListNotes(ctx, &ListOptions{
		Status:     "completed",
		PathPrefix: "projects/beta/",
	})
	if err != nil {
		t.Fatalf("ListNotes error: %v", err)
	}
	if len(notes) != 1 {
		t.Errorf("expected 1 completed beta note, got %d", len(notes))
	}
	if len(notes) > 0 && notes[0].Title != "Performance Report" {
		t.Errorf("title = %q, want %q", notes[0].Title, "Performance Report")
	}

	// Type=task, tag=go, project=alpha
	notes, err = s.ListNotes(ctx, &ListOptions{
		Type:      "task",
		Tag:       "go",
		ProjectID: "alpha",
	})
	if err != nil {
		t.Fatalf("ListNotes error: %v", err)
	}
	if len(notes) != 1 {
		t.Errorf("expected 1 task+go+alpha note, got %d", len(notes))
	}
	if len(notes) > 0 && notes[0].Title != "Implement Auth" {
		t.Errorf("title = %q, want %q", notes[0].Title, "Implement Auth")
	}
}

// ---------------------------------------------------------------------------
// ListNotes: sort by each column
// ---------------------------------------------------------------------------

func TestListNotes_SortByModified(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedListNotes(t, s)

	// DESC (default)
	notes, err := s.ListNotes(ctx, &ListOptions{SortBy: "modified", SortOrder: "desc"})
	if err != nil {
		t.Fatalf("ListNotes error: %v", err)
	}
	if len(notes) < 2 {
		t.Fatalf("expected at least 2 notes, got %d", len(notes))
	}
	// First should be latest modified: 2025-01-10 (plan-active)
	if notes[0].Title != "Alpha Roadmap" {
		t.Errorf("first note = %q, want %q", notes[0].Title, "Alpha Roadmap")
	}
	// Last should be earliest modified: 2025-01-06 (report-completed)
	if notes[len(notes)-1].Title != "Performance Report" {
		t.Errorf("last note = %q, want %q", notes[len(notes)-1].Title, "Performance Report")
	}
}

func TestListNotes_SortByCreated(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedListNotes(t, s)

	// ASC
	notes, err := s.ListNotes(ctx, &ListOptions{SortBy: "created", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListNotes error: %v", err)
	}
	if len(notes) < 2 {
		t.Fatalf("expected at least 2 notes, got %d", len(notes))
	}
	// First should be earliest created: 2025-01-01 (plan-active)
	if notes[0].Title != "Alpha Roadmap" {
		t.Errorf("first note = %q, want %q", notes[0].Title, "Alpha Roadmap")
	}
	// Last should be latest created: 2025-01-05 (report-completed)
	if notes[len(notes)-1].Title != "Performance Report" {
		t.Errorf("last note = %q, want %q", notes[len(notes)-1].Title, "Performance Report")
	}
}

func TestListNotes_SortByPriority(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedListNotes(t, s)

	// ASC: high < low < medium (alphabetical)
	notes, err := s.ListNotes(ctx, &ListOptions{SortBy: "priority", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListNotes error: %v", err)
	}
	if len(notes) < 2 {
		t.Fatalf("expected at least 2 notes, got %d", len(notes))
	}
	// Alphabetical ASC: "high" < "low" < "medium"
	if notes[0].Priority == nil || *notes[0].Priority != "high" {
		t.Errorf("first priority = %v, want 'high'", notes[0].Priority)
	}
}

func TestListNotes_SortByTitle(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedListNotes(t, s)

	// ASC
	notes, err := s.ListNotes(ctx, &ListOptions{SortBy: "title", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListNotes error: %v", err)
	}
	if len(notes) < 2 {
		t.Fatalf("expected at least 2 notes, got %d", len(notes))
	}
	// Alphabetical ASC: "Alpha Roadmap" < "Caching Strategy" < "Code Review Summary" < ...
	if notes[0].Title != "Alpha Roadmap" {
		t.Errorf("first title = %q, want %q", notes[0].Title, "Alpha Roadmap")
	}
}

// ---------------------------------------------------------------------------
// ListNotes: sort order asc/desc
// ---------------------------------------------------------------------------

func TestListNotes_SortOrderAscDesc(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedListNotes(t, s)

	// Title ASC
	ascNotes, err := s.ListNotes(ctx, &ListOptions{SortBy: "title", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListNotes ASC error: %v", err)
	}

	// Title DESC
	descNotes, err := s.ListNotes(ctx, &ListOptions{SortBy: "title", SortOrder: "desc"})
	if err != nil {
		t.Fatalf("ListNotes DESC error: %v", err)
	}

	if len(ascNotes) == 0 {
		t.Fatal("expected ASC notes, got 0")
	}
	if len(descNotes) == 0 {
		t.Fatal("expected DESC notes, got 0")
	}
	if len(ascNotes) != len(descNotes) {
		t.Fatalf("ASC count %d != DESC count %d", len(ascNotes), len(descNotes))
	}

	// First of ASC should be last of DESC
	if ascNotes[0].Title != descNotes[len(descNotes)-1].Title {
		t.Errorf("ASC first %q != DESC last %q", ascNotes[0].Title, descNotes[len(descNotes)-1].Title)
	}
	// Last of ASC should be first of DESC
	if ascNotes[len(ascNotes)-1].Title != descNotes[0].Title {
		t.Errorf("ASC last %q != DESC first %q", ascNotes[len(ascNotes)-1].Title, descNotes[0].Title)
	}
}

// ---------------------------------------------------------------------------
// ListNotes: invalid sort column defaults to modified
// ---------------------------------------------------------------------------

func TestListNotes_InvalidSortColumnDefaultsToModified(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedListNotes(t, s)

	// Invalid sort column should default to "modified"
	notes, err := s.ListNotes(ctx, &ListOptions{SortBy: "bogus_column", SortOrder: "desc"})
	if err != nil {
		t.Fatalf("ListNotes error: %v", err)
	}
	if len(notes) != 5 {
		t.Fatalf("expected 5 notes, got %d", len(notes))
	}

	// Should behave same as modified DESC
	modNotes, err := s.ListNotes(ctx, &ListOptions{SortBy: "modified", SortOrder: "desc"})
	if err != nil {
		t.Fatalf("ListNotes modified error: %v", err)
	}

	for i := range notes {
		if notes[i].Title != modNotes[i].Title {
			t.Errorf("note[%d] title = %q, want %q (same as modified desc)", i, notes[i].Title, modNotes[i].Title)
		}
	}
}

// ---------------------------------------------------------------------------
// ListNotes: limit and offset pagination
// ---------------------------------------------------------------------------

func TestListNotes_Limit(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedListNotes(t, s)

	notes, err := s.ListNotes(ctx, &ListOptions{Limit: 2})
	if err != nil {
		t.Fatalf("ListNotes error: %v", err)
	}
	if len(notes) != 2 {
		t.Errorf("expected 2 notes with Limit=2, got %d", len(notes))
	}
}

func TestListNotes_LimitAndOffset(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedListNotes(t, s)

	// Get all notes sorted by title ASC for predictable ordering
	allNotes, err := s.ListNotes(ctx, &ListOptions{SortBy: "title", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("ListNotes all error: %v", err)
	}

	// Page 1: offset=0, limit=2
	page1, err := s.ListNotes(ctx, &ListOptions{SortBy: "title", SortOrder: "asc", Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("ListNotes page1 error: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1: expected 2 notes, got %d", len(page1))
	}

	// Page 2: offset=2, limit=2
	page2, err := s.ListNotes(ctx, &ListOptions{SortBy: "title", SortOrder: "asc", Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListNotes page2 error: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("page2: expected 2 notes, got %d", len(page2))
	}

	// Page 3: offset=4, limit=2
	page3, err := s.ListNotes(ctx, &ListOptions{SortBy: "title", SortOrder: "asc", Limit: 2, Offset: 4})
	if err != nil {
		t.Fatalf("ListNotes page3 error: %v", err)
	}
	if len(page3) != 1 {
		t.Fatalf("page3: expected 1 note, got %d", len(page3))
	}

	// Verify pages don't overlap and cover all notes
	if page1[0].Title != allNotes[0].Title {
		t.Errorf("page1[0] = %q, want %q", page1[0].Title, allNotes[0].Title)
	}
	if page1[1].Title != allNotes[1].Title {
		t.Errorf("page1[1] = %q, want %q", page1[1].Title, allNotes[1].Title)
	}
	if page2[0].Title != allNotes[2].Title {
		t.Errorf("page2[0] = %q, want %q", page2[0].Title, allNotes[2].Title)
	}
	if page2[1].Title != allNotes[3].Title {
		t.Errorf("page2[1] = %q, want %q", page2[1].Title, allNotes[3].Title)
	}
	if page3[0].Title != allNotes[4].Title {
		t.Errorf("page3[0] = %q, want %q", page3[0].Title, allNotes[4].Title)
	}
}

// ---------------------------------------------------------------------------
// ListNotes: default limit is 100
// ---------------------------------------------------------------------------

func TestListNotes_DefaultLimit(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert 105 notes
	for i := 0; i < 105; i++ {
		n := &NoteRow{
			Path:     fmt.Sprintf("projects/test/plan/note-%03d.md", i),
			ShortID:  fmt.Sprintf("n%06d", i),
			Title:    fmt.Sprintf("Note %03d", i),
			Metadata: "{}",
		}
		_, err := s.InsertNote(ctx, n)
		if err != nil {
			t.Fatalf("insert note %d: %v", i, err)
		}
	}

	notes, err := s.ListNotes(ctx, &ListOptions{})
	if err != nil {
		t.Fatalf("ListNotes error: %v", err)
	}
	if len(notes) != 100 {
		t.Errorf("expected 100 notes (default limit), got %d", len(notes))
	}
}

// ---------------------------------------------------------------------------
// ListNotes: empty result returns non-nil empty slice
// ---------------------------------------------------------------------------

func TestListNotes_EmptyResult(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// No notes inserted
	notes, err := s.ListNotes(ctx, &ListOptions{})
	if err != nil {
		t.Fatalf("ListNotes error: %v", err)
	}
	if notes == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(notes) != 0 {
		t.Errorf("expected 0 notes, got %d", len(notes))
	}
}
