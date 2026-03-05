package storage

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

// ---------------------------------------------------------------------------
// Benchmark helpers
// ---------------------------------------------------------------------------

// newBenchStorage creates an in-memory StorageLayer for benchmarks.
// Uses b.Cleanup for teardown.
func newBenchStorage(b *testing.B) *StorageLayer {
	b.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		b.Fatalf("sql.Open failed: %v", err)
	}
	s, err := NewWithDB(db)
	if err != nil {
		b.Fatalf("NewWithDB failed: %v", err)
	}
	b.Cleanup(func() { s.Close() })
	return s
}

// seedNotes inserts n notes with cross-links and tags into the storage.
// Returns the paths of all inserted notes.
func seedNotes(b *testing.B, s *StorageLayer, n int) []string {
	b.Helper()
	ctx := context.Background()
	paths := make([]string, n)

	types := []string{"plan", "task", "summary", "report", "exploration", "decision", "pattern", "learning"}
	statuses := []string{"active", "pending", "completed", "in_progress", "draft"}

	for i := 0; i < n; i++ {
		path := fmt.Sprintf("projects/bench/task/note-%04d.md", i)
		shortID := fmt.Sprintf("bn%05d", i)
		title := fmt.Sprintf("Benchmark Note %d: %s implementation", i, types[i%len(types)])
		body := fmt.Sprintf("This is the body content for benchmark note %d. It contains various keywords like authentication, database, API, middleware, testing, deployment, configuration, and monitoring. Note %d references concepts from software engineering.", i, i)
		noteType := types[i%len(types)]
		status := statuses[i%len(statuses)]
		projectID := "bench"
		created := fmt.Sprintf("2024-01-%02dT10:00:00Z", (i%28)+1)
		modified := created

		_, err := s.db.ExecContext(ctx, `
			INSERT INTO notes (path, short_id, title, body, raw_content, type, status, priority, project_id, created, modified, metadata)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '{}')
		`, path, shortID, title, body, body, noteType, status, "medium", projectID, created, modified)
		if err != nil {
			b.Fatalf("seed note %d: %v", i, err)
		}
		paths[i] = path
	}

	// Add tags (3 tags per note on average)
	tagPool := []string{"go", "api", "database", "testing", "auth", "middleware", "config", "deploy", "monitor", "cache"}
	for i := 0; i < n; i++ {
		var noteID int64
		err := s.db.QueryRowContext(ctx, "SELECT id FROM notes WHERE path = ?", paths[i]).Scan(&noteID)
		if err != nil {
			b.Fatalf("get note id %d: %v", i, err)
		}

		for j := 0; j < 3; j++ {
			tag := tagPool[(i+j)%len(tagPool)]
			_, err := s.db.ExecContext(ctx, "INSERT INTO tags (note_id, tag) VALUES (?, ?)", noteID, tag)
			if err != nil {
				b.Fatalf("seed tag %d/%d: %v", i, j, err)
			}
		}
	}

	// Add cross-links (each note links to 2 other notes)
	for i := 0; i < n; i++ {
		var sourceID int64
		err := s.db.QueryRowContext(ctx, "SELECT id FROM notes WHERE path = ?", paths[i]).Scan(&sourceID)
		if err != nil {
			b.Fatalf("get source id %d: %v", i, err)
		}

		for j := 1; j <= 2; j++ {
			targetIdx := (i + j) % n
			targetPath := paths[targetIdx]
			var targetID int64
			err := s.db.QueryRowContext(ctx, "SELECT id FROM notes WHERE path = ?", targetPath).Scan(&targetID)
			if err != nil {
				b.Fatalf("get target id %d: %v", targetIdx, err)
			}

			_, err = s.db.ExecContext(ctx, `
				INSERT INTO links (source_id, target_path, target_id, title, href, type, snippet)
				VALUES (?, ?, ?, ?, ?, 'markdown', '')
			`, sourceID, targetPath, targetID, fmt.Sprintf("Link to note %d", targetIdx), targetPath)
			if err != nil {
				b.Fatalf("seed link %d->%d: %v", i, targetIdx, err)
			}
		}
	}

	return paths
}

func strPtr(s string) *string {
	return &s
}

// ---------------------------------------------------------------------------
// Storage Benchmarks: Note CRUD
// ---------------------------------------------------------------------------

func BenchmarkNoteSave(b *testing.B) {
	s := newBenchStorage(b)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := fmt.Sprintf("projects/bench/task/save-%d.md", i)
		note := &NoteRow{
			Path:     path,
			ShortID:  fmt.Sprintf("sv%06d", i),
			Title:    fmt.Sprintf("Save Benchmark Note %d", i),
			Body:     strPtr("Body content for save benchmark"),
			Metadata: "{}",
			Type:     strPtr("task"),
			Status:   strPtr("active"),
			Priority: strPtr("medium"),
		}
		_, err := s.InsertNote(ctx, note)
		if err != nil {
			b.Fatalf("InsertNote: %v", err)
		}
	}
}

func BenchmarkNoteSaveBatch(b *testing.B) {
	s := newBenchStorage(b)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Each iteration inserts a batch of 100 notes
		for j := 0; j < 100; j++ {
			idx := i*100 + j
			path := fmt.Sprintf("projects/bench/task/batch-%d.md", idx)
			note := &NoteRow{
				Path:     path,
				ShortID:  fmt.Sprintf("bt%06d", idx),
				Title:    fmt.Sprintf("Batch Note %d", idx),
				Body:     strPtr("Batch body content"),
				Metadata: "{}",
				Type:     strPtr("task"),
				Status:   strPtr("pending"),
				Priority: strPtr("low"),
			}
			_, err := s.InsertNote(ctx, note)
			if err != nil {
				b.Fatalf("InsertNote batch: %v", err)
			}
		}
	}
}

func BenchmarkNoteRecall(b *testing.B) {
	s := newBenchStorage(b)
	ctx := context.Background()
	paths := seedNotes(b, s, 500)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := paths[i%len(paths)]
		_, err := s.GetNoteByPath(ctx, path)
		if err != nil {
			b.Fatalf("GetNoteByPath: %v", err)
		}
	}
}

func BenchmarkNoteRecallByShortID(b *testing.B) {
	s := newBenchStorage(b)
	ctx := context.Background()
	seedNotes(b, s, 500)

	shortIDs := make([]string, 500)
	for i := 0; i < 500; i++ {
		shortIDs[i] = fmt.Sprintf("bn%05d", i)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sid := shortIDs[i%len(shortIDs)]
		_, err := s.GetNoteByShortID(ctx, sid)
		if err != nil {
			b.Fatalf("GetNoteByShortID: %v", err)
		}
	}
}

func BenchmarkUpdateNote(b *testing.B) {
	s := newBenchStorage(b)
	ctx := context.Background()
	paths := seedNotes(b, s, 500)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := paths[i%len(paths)]
		updates := map[string]interface{}{
			"status":   "completed",
			"modified": "2024-06-15T12:00:00Z",
		}
		_, err := s.UpdateNote(ctx, path, updates)
		if err != nil {
			b.Fatalf("UpdateNote: %v", err)
		}
	}
}

func BenchmarkDeleteNote(b *testing.B) {
	s := newBenchStorage(b)
	ctx := context.Background()

	// Pre-create notes to delete
	for i := 0; i < b.N; i++ {
		path := fmt.Sprintf("projects/bench/task/del-%d.md", i)
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO notes (path, short_id, title, metadata)
			VALUES (?, ?, ?, '{}')
		`, path, fmt.Sprintf("dl%06d", i), fmt.Sprintf("Delete Note %d", i))
		if err != nil {
			b.Fatalf("seed delete note %d: %v", i, err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := fmt.Sprintf("projects/bench/task/del-%d.md", i)
		_, err := s.DeleteNote(ctx, path)
		if err != nil {
			b.Fatalf("DeleteNote: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// Storage Benchmarks: Search
// ---------------------------------------------------------------------------

func BenchmarkSearch(b *testing.B) {
	s := newBenchStorage(b)
	ctx := context.Background()
	seedNotes(b, s, 500)

	queries := []string{"authentication", "database", "API", "middleware", "testing", "deployment"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q := queries[i%len(queries)]
		_, err := s.SearchNotes(ctx, q, nil)
		if err != nil {
			b.Fatalf("SearchNotes: %v", err)
		}
	}
}

func BenchmarkSearchWithFilters(b *testing.B) {
	s := newBenchStorage(b)
	ctx := context.Background()
	seedNotes(b, s, 500)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		opts := &SearchOptions{
			Type:   "task",
			Status: "active",
			Limit:  20,
		}
		_, err := s.SearchNotes(ctx, "implementation", opts)
		if err != nil {
			b.Fatalf("SearchNotes with filters: %v", err)
		}
	}
}

func BenchmarkSearchExact(b *testing.B) {
	s := newBenchStorage(b)
	ctx := context.Background()
	seedNotes(b, s, 500)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		opts := &SearchOptions{
			Strategy: "exact",
			Limit:    20,
		}
		_, err := s.SearchNotes(ctx, "Benchmark Note 42", opts)
		if err != nil {
			b.Fatalf("SearchNotes exact: %v", err)
		}
	}
}

func BenchmarkSearchLike(b *testing.B) {
	s := newBenchStorage(b)
	ctx := context.Background()
	seedNotes(b, s, 500)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		opts := &SearchOptions{
			Strategy: "like",
			Limit:    20,
		}
		_, err := s.SearchNotes(ctx, "authentication", opts)
		if err != nil {
			b.Fatalf("SearchNotes like: %v", err)
		}
	}
}

// Scaled search benchmarks
func BenchmarkSearch_100Notes(b *testing.B)  { benchmarkSearchN(b, 100) }
func BenchmarkSearch_500Notes(b *testing.B)  { benchmarkSearchN(b, 500) }
func BenchmarkSearch_1000Notes(b *testing.B) { benchmarkSearchN(b, 1000) }

func benchmarkSearchN(b *testing.B, n int) {
	s := newBenchStorage(b)
	ctx := context.Background()
	seedNotes(b, s, n)

	queries := []string{"authentication", "database", "API", "middleware", "testing"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q := queries[i%len(queries)]
		_, err := s.SearchNotes(ctx, q, nil)
		if err != nil {
			b.Fatalf("SearchNotes: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// Storage Benchmarks: List
// ---------------------------------------------------------------------------

func BenchmarkList(b *testing.B) {
	s := newBenchStorage(b)
	ctx := context.Background()
	seedNotes(b, s, 500)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		opts := &ListOptions{
			Limit:  20,
			Offset: 0,
			SortBy: "modified",
		}
		_, err := s.ListNotes(ctx, opts)
		if err != nil {
			b.Fatalf("ListNotes: %v", err)
		}
	}
}

func BenchmarkListWithFilters(b *testing.B) {
	s := newBenchStorage(b)
	ctx := context.Background()
	seedNotes(b, s, 500)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		opts := &ListOptions{
			Type:      "task",
			Status:    "pending",
			ProjectID: "bench",
			Limit:     20,
			Offset:    0,
			SortBy:    "created",
		}
		_, err := s.ListNotes(ctx, opts)
		if err != nil {
			b.Fatalf("ListNotes with filters: %v", err)
		}
	}
}

func BenchmarkListWithTags(b *testing.B) {
	s := newBenchStorage(b)
	ctx := context.Background()
	seedNotes(b, s, 500)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		opts := &ListOptions{
			Tags:  []string{"go", "api"},
			Limit: 20,
		}
		_, err := s.ListNotes(ctx, opts)
		if err != nil {
			b.Fatalf("ListNotes with tags: %v", err)
		}
	}
}

func BenchmarkListPagination(b *testing.B) {
	s := newBenchStorage(b)
	ctx := context.Background()
	seedNotes(b, s, 500)

	offsets := []int{0, 50, 100, 200, 400}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		opts := &ListOptions{
			Limit:  20,
			Offset: offsets[i%len(offsets)],
			SortBy: "modified",
		}
		_, err := s.ListNotes(ctx, opts)
		if err != nil {
			b.Fatalf("ListNotes pagination: %v", err)
		}
	}
}

// Scaled list benchmarks
func BenchmarkList_100Notes(b *testing.B)  { benchmarkListN(b, 100) }
func BenchmarkList_500Notes(b *testing.B)  { benchmarkListN(b, 500) }
func BenchmarkList_1000Notes(b *testing.B) { benchmarkListN(b, 1000) }

func benchmarkListN(b *testing.B, n int) {
	s := newBenchStorage(b)
	ctx := context.Background()
	seedNotes(b, s, n)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		opts := &ListOptions{
			Limit:  20,
			Offset: 0,
			SortBy: "modified",
		}
		_, err := s.ListNotes(ctx, opts)
		if err != nil {
			b.Fatalf("ListNotes: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// Storage Benchmarks: Graph
// ---------------------------------------------------------------------------

func BenchmarkGraphBacklinks(b *testing.B) {
	s := newBenchStorage(b)
	ctx := context.Background()
	paths := seedNotes(b, s, 500)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := paths[i%len(paths)]
		_, err := s.GetBacklinks(ctx, path)
		if err != nil {
			b.Fatalf("GetBacklinks: %v", err)
		}
	}
}

func BenchmarkGraphOutlinks(b *testing.B) {
	s := newBenchStorage(b)
	ctx := context.Background()
	paths := seedNotes(b, s, 500)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := paths[i%len(paths)]
		_, err := s.GetOutlinks(ctx, path)
		if err != nil {
			b.Fatalf("GetOutlinks: %v", err)
		}
	}
}

func BenchmarkGraphRelated(b *testing.B) {
	s := newBenchStorage(b)
	ctx := context.Background()
	paths := seedNotes(b, s, 500)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := paths[i%len(paths)]
		_, err := s.GetRelated(ctx, path, 10)
		if err != nil {
			b.Fatalf("GetRelated: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// Storage Benchmarks: Links
// ---------------------------------------------------------------------------

func BenchmarkSetLinks(b *testing.B) {
	s := newBenchStorage(b)
	ctx := context.Background()
	paths := seedNotes(b, s, 100)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := paths[i%len(paths)]
		links := []LinkInput{
			{TargetPath: paths[(i+1)%len(paths)], Title: "Link 1", Href: paths[(i+1)%len(paths)], Type: "markdown"},
			{TargetPath: paths[(i+2)%len(paths)], Title: "Link 2", Href: paths[(i+2)%len(paths)], Type: "markdown"},
			{TargetPath: paths[(i+3)%len(paths)], Title: "Link 3", Href: paths[(i+3)%len(paths)], Type: "markdown"},
		}
		err := s.SetLinks(ctx, path, links)
		if err != nil {
			b.Fatalf("SetLinks: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// Storage Benchmarks: FTS Index Rebuild
// ---------------------------------------------------------------------------

func BenchmarkIndexRebuild(b *testing.B) {
	s := newBenchStorage(b)
	ctx := context.Background()
	seedNotes(b, s, 200)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Rebuild FTS index using the rebuild command
		_, err := s.db.ExecContext(ctx, "INSERT INTO notes_fts(notes_fts) VALUES('rebuild')")
		if err != nil {
			b.Fatalf("FTS rebuild: %v", err)
		}
	}
}
