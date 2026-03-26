package indexer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Constructor tests
// ---------------------------------------------------------------------------

func TestNewFileWatcher_ReturnsWatcher(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
	})
	idx := NewIndexer(brainDir, store)

	fw, err := NewFileWatcher(brainDir, idx, nil)
	if err != nil {
		t.Fatalf("NewFileWatcher failed: %v", err)
	}
	if fw == nil {
		t.Fatal("NewFileWatcher returned nil")
	}
}

func TestNewFileWatcher_WithOptions(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
	})
	idx := NewIndexer(brainDir, store)

	opts := &FileWatcherOptions{
		DebounceMs:     200,
		IgnorePatterns: []string{"tmp/"},
	}
	fw, err := NewFileWatcher(brainDir, idx, opts)
	if err != nil {
		t.Fatalf("NewFileWatcher failed: %v", err)
	}
	if fw == nil {
		t.Fatal("NewFileWatcher returned nil")
	}
	if fw.debounceMs != 200 {
		t.Errorf("debounceMs = %d, want 200", fw.debounceMs)
	}
}

// ---------------------------------------------------------------------------
// Start/Stop lifecycle tests
// ---------------------------------------------------------------------------

func TestFileWatcher_StartStop(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
	})
	idx := NewIndexer(brainDir, store)

	fw, err := NewFileWatcher(brainDir, idx, nil)
	if err != nil {
		t.Fatalf("NewFileWatcher failed: %v", err)
	}

	if fw.IsRunning() {
		t.Error("should not be running before Start()")
	}

	if err := fw.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !fw.IsRunning() {
		t.Error("should be running after Start()")
	}

	fw.Stop()

	if fw.IsRunning() {
		t.Error("should not be running after Stop()")
	}
}

func TestFileWatcher_StartIdempotent(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
	})
	idx := NewIndexer(brainDir, store)

	fw, err := NewFileWatcher(brainDir, idx, nil)
	if err != nil {
		t.Fatalf("NewFileWatcher failed: %v", err)
	}
	defer fw.Stop()

	// Start twice should not error
	if err := fw.Start(); err != nil {
		t.Fatalf("first Start failed: %v", err)
	}
	if err := fw.Start(); err != nil {
		t.Fatalf("second Start failed: %v", err)
	}

	if !fw.IsRunning() {
		t.Error("should still be running after double Start()")
	}
}

// ---------------------------------------------------------------------------
// File change detection tests
// ---------------------------------------------------------------------------

func TestFileWatcher_DetectsNewFile(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
	})
	idx := NewIndexer(brainDir, store)

	// Index initial file
	_, err := idx.RebuildAll()
	if err != nil {
		t.Fatalf("RebuildAll failed: %v", err)
	}

	fw, err := NewFileWatcher(brainDir, idx, &FileWatcherOptions{DebounceMs: 50})
	if err != nil {
		t.Fatalf("NewFileWatcher failed: %v", err)
	}
	if err := fw.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer fw.Stop()

	// Create a new file
	newFile := filepath.Join(brainDir, "note2.md")
	if err := os.WriteFile(newFile, []byte(noteContent("Note Two")), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Wait for debounce + processing
	time.Sleep(500 * time.Millisecond)

	// Verify the new file was indexed
	if countNotes(t, store) != 2 {
		t.Errorf("DB note count = %d, want 2 (new file should be indexed)", countNotes(t, store))
	}
}

func TestFileWatcher_DetectsModifiedFile(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
	})
	idx := NewIndexer(brainDir, store)

	// Index initial file
	_, err := idx.RebuildAll()
	if err != nil {
		t.Fatalf("RebuildAll failed: %v", err)
	}

	fw, err := NewFileWatcher(brainDir, idx, &FileWatcherOptions{DebounceMs: 50})
	if err != nil {
		t.Fatalf("NewFileWatcher failed: %v", err)
	}
	if err := fw.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer fw.Stop()

	// Modify the file
	if err := os.WriteFile(filepath.Join(brainDir, "note1.md"), []byte(noteContent("Note One Updated")), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Wait for debounce + processing
	time.Sleep(500 * time.Millisecond)

	// Verify the file was re-indexed with new title
	note, err := store.GetNoteByPath(t.Context(), "note1.md")
	if err != nil {
		t.Fatalf("GetNoteByPath failed: %v", err)
	}
	if note == nil {
		t.Fatal("note1.md not found after modification")
	}
	if note.Title != "Note One Updated" {
		t.Errorf("Title = %q, want %q", note.Title, "Note One Updated")
	}
}

func TestFileWatcher_DetectsDeletedFile(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
		"note2.md": noteContent("Note Two"),
	})
	idx := NewIndexer(brainDir, store)

	// Index initial files
	_, err := idx.RebuildAll()
	if err != nil {
		t.Fatalf("RebuildAll failed: %v", err)
	}

	fw, err := NewFileWatcher(brainDir, idx, &FileWatcherOptions{DebounceMs: 50})
	if err != nil {
		t.Fatalf("NewFileWatcher failed: %v", err)
	}
	if err := fw.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer fw.Stop()

	// Delete a file
	os.Remove(filepath.Join(brainDir, "note2.md"))

	// Wait for debounce + processing
	time.Sleep(500 * time.Millisecond)

	// Verify the file was removed from index
	if countNotes(t, store) != 1 {
		t.Errorf("DB note count = %d, want 1 (deleted file should be removed)", countNotes(t, store))
	}
}

// ---------------------------------------------------------------------------
// Ignore pattern tests
// ---------------------------------------------------------------------------

func TestFileWatcher_IgnoresZkDirectory(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
	})
	idx := NewIndexer(brainDir, store)

	// Index initial file
	_, err := idx.RebuildAll()
	if err != nil {
		t.Fatalf("RebuildAll failed: %v", err)
	}

	fw, err := NewFileWatcher(brainDir, idx, &FileWatcherOptions{DebounceMs: 50})
	if err != nil {
		t.Fatalf("NewFileWatcher failed: %v", err)
	}
	if err := fw.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer fw.Stop()

	// Create a file in .zk/ directory
	zkDir := filepath.Join(brainDir, ".zk")
	os.MkdirAll(zkDir, 0o755)
	os.WriteFile(filepath.Join(zkDir, "config.md"), []byte(noteContent("ZK Config")), 0o644)

	// Wait for debounce + processing
	time.Sleep(500 * time.Millisecond)

	// Should still be only 1 note (the .zk/ file should be ignored)
	if countNotes(t, store) != 1 {
		t.Errorf("DB note count = %d, want 1 (.zk/ files should be ignored)", countNotes(t, store))
	}
}

func TestFileWatcher_IgnoresNonMdFiles(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
	})
	idx := NewIndexer(brainDir, store)

	// Index initial file
	_, err := idx.RebuildAll()
	if err != nil {
		t.Fatalf("RebuildAll failed: %v", err)
	}

	fw, err := NewFileWatcher(brainDir, idx, &FileWatcherOptions{DebounceMs: 50})
	if err != nil {
		t.Fatalf("NewFileWatcher failed: %v", err)
	}
	if err := fw.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer fw.Stop()

	// Create a non-.md file
	os.WriteFile(filepath.Join(brainDir, "readme.txt"), []byte("not markdown"), 0o644)

	// Wait for debounce + processing
	time.Sleep(500 * time.Millisecond)

	// Should still be only 1 note
	if countNotes(t, store) != 1 {
		t.Errorf("DB note count = %d, want 1 (non-.md files should be ignored)", countNotes(t, store))
	}
}

func TestFileWatcher_IgnoresCustomPatterns(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
	})
	idx := NewIndexer(brainDir, store)

	// Index initial file
	_, err := idx.RebuildAll()
	if err != nil {
		t.Fatalf("RebuildAll failed: %v", err)
	}

	fw, err := NewFileWatcher(brainDir, idx, &FileWatcherOptions{
		DebounceMs:     50,
		IgnorePatterns: []string{"drafts/"},
	})
	if err != nil {
		t.Fatalf("NewFileWatcher failed: %v", err)
	}
	if err := fw.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer fw.Stop()

	// Create a file in drafts/ directory
	draftsDir := filepath.Join(brainDir, "drafts")
	os.MkdirAll(draftsDir, 0o755)
	os.WriteFile(filepath.Join(draftsDir, "draft.md"), []byte(noteContent("Draft")), 0o644)

	// Wait for debounce + processing
	time.Sleep(500 * time.Millisecond)

	// Should still be only 1 note
	if countNotes(t, store) != 1 {
		t.Errorf("DB note count = %d, want 1 (drafts/ files should be ignored)", countNotes(t, store))
	}
}

// ---------------------------------------------------------------------------
// shouldIgnore unit tests
// ---------------------------------------------------------------------------

func TestShouldIgnore(t *testing.T) {
	store := newTestStorage(t)
	brainDir := t.TempDir()
	idx := NewIndexer(brainDir, store)

	fw, err := NewFileWatcher(brainDir, idx, &FileWatcherOptions{
		IgnorePatterns: []string{"custom/"},
	})
	if err != nil {
		t.Fatalf("NewFileWatcher failed: %v", err)
	}

	tests := []struct {
		path   string
		ignore bool
	}{
		{".zk/config.md", true},
		{"node_modules/pkg/readme.md", true},
		{"custom/note.md", true},
		{"sub/custom/note.md", true},
		{"note.md", false},
		{"projects/plan/abc.md", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := fw.shouldIgnore(tt.path)
			if got != tt.ignore {
				t.Errorf("shouldIgnore(%q) = %v, want %v", tt.path, got, tt.ignore)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Debounce coalescing test
// ---------------------------------------------------------------------------

func TestFileWatcher_DebouncesRapidChanges(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
	})
	idx := NewIndexer(brainDir, store)

	// Index initial file
	_, err := idx.RebuildAll()
	if err != nil {
		t.Fatalf("RebuildAll failed: %v", err)
	}

	fw, err := NewFileWatcher(brainDir, idx, &FileWatcherOptions{DebounceMs: 200})
	if err != nil {
		t.Fatalf("NewFileWatcher failed: %v", err)
	}
	if err := fw.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer fw.Stop()

	// Rapidly modify the same file multiple times
	for i := 0; i < 5; i++ {
		os.WriteFile(filepath.Join(brainDir, "note1.md"),
			[]byte(noteContent("Note One v"+string(rune('0'+i)))),
			0o644)
		time.Sleep(20 * time.Millisecond)
	}

	// Wait for debounce to fire
	time.Sleep(600 * time.Millisecond)

	// Should still be exactly 1 note (debounce coalesced the changes)
	if countNotes(t, store) != 1 {
		t.Errorf("DB note count = %d, want 1", countNotes(t, store))
	}
}
