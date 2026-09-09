package indexer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/brainpath"
)

func TestIndexChanged_RejectsOutsideMarkdownSymlink(t *testing.T) {
	store := newTestStorage(t)
	root := createBrainDir(t, map[string]string{"projects/demo/note/safe.md": noteContent("Safe")})
	outside := createBrainDir(t, map[string]string{"secret.md": noteContent("External secret", "external-tag")})
	const link = "projects/demo/note/escape.md"
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(root, link)); err != nil {
		t.Fatal(err)
	}
	idx := NewIndexer(root, store)
	for pass := 0; pass < 2; pass++ {
		result, err := idx.IndexChanged()
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Errors) != 1 || result.Errors[0].Path != link || !strings.Contains(result.Errors[0].Error, brainpath.ErrContainment.Error()) {
			t.Errorf("expected per-file containment failure for %s, got %+v", link, result.Errors)
		}
		if result.Added != 1-pass || result.Skipped != pass || result.Updated != 0 || result.Deleted != 0 {
			t.Errorf("incorrect indexing counters on pass %d: %+v", pass, result)
		}
		if got := countNotes(t, store); got != 1 {
			t.Errorf("indexed %d notes, want only safe note", got)
		}
		note, err := store.GetNoteByPath(context.Background(), link)
		if err != nil {
			t.Fatal(err)
		}
		if note != nil {
			t.Error("external symlink content entered the index")
		}
	}
}
