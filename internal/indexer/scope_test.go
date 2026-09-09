package indexer

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGlobMarkdownFiles_ContentRootsOnly(t *testing.T) {
	root := createBrainDir(t, map[string]string{
		"projects/demo/note/project.md":                noteContent("Project"),
		"global/note/global.md":                        noteContent("Global"),
		"root.md":                                      noteContent("Root"),
		"attachments/ab/cd/" + strings.Repeat("a", 64): "blob",
		"attachments/ab/cd/decoy.md":                   noteContent("Attachment"),
		".git/objects/ab/decoy.md":                     noteContent("Git"),
		".brain-data/cache/decoy.md":                   noteContent("Data"),
		".zk/decoy.md":                                 noteContent("Legacy"),
		"arbitrary/projects/decoy.md":                  noteContent("Sibling"),
		"projects-backup/decoy.md":                     noteContent("Prefix"),
		"global-backup/decoy.md":                       noteContent("Prefix"),
	})
	got, err := globMarkdownFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"global/note/global.md", "projects/demo/note/project.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discovered %v, want %v", got, want)
	}
}

// An unreadable excluded directory proves pruning, rather than filtering files
// after walking the excluded subtree. Root can bypass permissions, so skip there.
func TestGlobMarkdownFiles_DoesNotDescendExcludedDirectories(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	for _, sibling := range []string{"attachments", ".git", ".brain-data", "arbitrary"} {
		t.Run(sibling, func(t *testing.T) {
			root := createBrainDir(t, map[string]string{sibling + "/nested/decoy.md": noteContent("Excluded")})
			path := filepath.Join(root, sibling)
			if err := os.Chmod(path, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(path, 0o755) })
			got, err := globMarkdownFiles(root)
			if err != nil || len(got) != 0 {
				t.Fatalf("excluded subtree traversed: files=%v err=%v", got, err)
			}
		})
	}
}

func TestScopedDiscovery_StaleCleanup(t *testing.T) {
	for _, rebuild := range []bool{false, true} {
		t.Run(map[bool]string{false: "incremental", true: "rebuild"}[rebuild], func(t *testing.T) {
			store := newTestStorage(t)
			root := createBrainDir(t, map[string]string{
				"root.md":                       noteContent("Previously indexed root"),
				"arbitrary/old.md":              noteContent("Previously indexed sibling"),
				"projects/demo/note/project.md": noteContent("Project"),
				"global/note/global.md":         noteContent("Global"),
			})
			idx := NewIndexer(root, store)
			// Direct indexing remains containment-only, not discovery-scoped.
			for _, path := range []string{"root.md", "arbitrary/old.md"} {
				if err := idx.IndexFile(path); err != nil {
					t.Fatal(err)
				}
			}
			health, err := idx.GetHealth()
			if err != nil {
				t.Fatal(err)
			}
			if health.TotalFiles != 2 || health.TotalIndexed != 2 || health.StaleCount != 2 {
				t.Errorf("out-of-scope rows must be stale even while files exist: %+v", health)
			}
			var result *IndexResult
			if rebuild {
				result, err = idx.RebuildAll()
			} else {
				result, err = idx.IndexChanged()
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.Added != 2 || result.Deleted != 2 || len(result.Errors) != 0 || countNotes(t, store) != 2 {
				t.Fatalf("scoped scan must index content and delete stale rows: %+v", result)
			}
			for _, path := range []string{"root.md", "arbitrary/old.md"} {
				if _, err := os.Stat(filepath.Join(root, path)); err != nil {
					t.Fatalf("cleanup changed disk: %v", err)
				}
			}
		})
	}
}
