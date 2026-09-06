package indexer

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/fsnotify/fsnotify"
)

func watchPaths(fw *FileWatcher) []string {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	var paths []string
	for _, path := range fw.watcher.WatchList() {
		rel, _ := filepath.Rel(fw.brainDir, path)
		paths = append(paths, filepath.ToSlash(rel))
	}
	sort.Strings(paths)
	return paths
}

func TestFileWatcher_ContentScope(t *testing.T) {
	for _, later := range []bool{false, true} {
		name := "startup"
		if later {
			name = "later"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			fw, err := NewFileWatcher(root, NewIndexer(root, newTestStorage(t)), &FileWatcherOptions{
				DebounceMs: 60000, IgnorePatterns: []string{"drafts/"},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer fw.Stop()
			if later {
				if err := fw.Start(); err != nil {
					t.Fatal(err)
				}
			}
			dirs := []string{
				"attachments/ab/cd/" + strings.Repeat("a", 64),
				".git/objects/ab", ".brain-data/cache", "arbitrary/deep",
				"projects-backup/deep", "global-old/deep",
				"projects/p/note", "global/note", "projects/p/drafts/deep",
			}
			for _, dir := range dirs {
				abs := filepath.Join(root, dir)
				if err := os.MkdirAll(abs, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(abs, "note.md"), []byte(noteContent(dir)), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if !later {
				if err := fw.Start(); err != nil {
					t.Fatal(err)
				}
			} else {
				// Deterministic delivery after population exercises the recursive sweep,
				// including direct calls naming a deep excluded shard.
				for _, dir := range dirs {
					fw.addDirRecursive(filepath.Join(root, dir))
					fw.handleEvent(fsnotify.Event{Name: filepath.Join(root, strings.Split(dir, "/")[0]), Op: fsnotify.Create})
				}
			}
			want := []string{".", "global", "global/note", "projects", "projects/p", "projects/p/note"}
			if got := watchPaths(fw); !reflect.DeepEqual(got, want) {
				t.Errorf("watched directories = %v, want %v (excluded trees must not be descended/watched)", got, want)
			}
			// Direct markdown events must obey the same scope, even if supplied
			// independently of watch registration (including remove/rename).
			for _, dir := range append(dirs, "") {
				for _, op := range []fsnotify.Op{fsnotify.Create, fsnotify.Write, fsnotify.Remove, fsnotify.Rename} {
					fw.handleEvent(fsnotify.Event{Name: filepath.Join(root, dir, "note.md"), Op: op})
				}
			}
			fw.mu.Lock()
			defer fw.mu.Unlock()
			for path := range fw.pendingChanges {
				if path != "projects/p/note/note.md" && path != "global/note/note.md" {
					t.Errorf("excluded descendant queued: %s", path)
				}
			}
		})
	}
}

func TestFileWatcher_ExcludedUnreadableTrees(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"attachments", ".git", ".brain-data", "arbitrary"} {
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		if _, err := os.ReadDir(dir); err == nil {
			t.Skip("requires unreadable directories")
		}
	}
	fw, _ := NewFileWatcher(root, nil, nil)
	defer fw.Stop()
	if err := fw.Start(); err != nil {
		t.Fatalf("descended into excluded unreadable tree: %v", err)
	}
	if got := watchPaths(fw); !reflect.DeepEqual(got, []string{"."}) {
		t.Fatalf("watches: %v", got)
	}
}

func TestFileWatcher_InitiallyAbsentRoots(t *testing.T) {
	for _, contentRoot := range []string{"projects", "global"} {
		t.Run(contentRoot, func(t *testing.T) {
			root := t.TempDir()
			store := newTestStorage(t)
			fw, _ := NewFileWatcher(root, NewIndexer(root, store), &FileWatcherOptions{DebounceMs: 20})
			if err := fw.Start(); err != nil {
				t.Fatal(err)
			}
			defer fw.Stop()
			if got := watchPaths(fw); !reflect.DeepEqual(got, []string{"."}) {
				t.Fatalf("anchor: %v", got)
			}
			// Rename an already populated subtree into the root: no child create
			// events can help the watcher find its existing note.
			staged := t.TempDir()
			if err := os.MkdirAll(filepath.Join(staged, "p/note"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(staged, "p/note/first.md"), []byte(noteContent("First")), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(staged, filepath.Join(root, contentRoot)); err != nil {
				t.Fatal(err)
			}
			waitForNoteCount(t, store, 1)
			if err := os.WriteFile(filepath.Join(root, contentRoot, "p/note/second.md"), []byte(noteContent("Second")), 0o644); err != nil {
				t.Fatal(err)
			}
			waitForNoteCount(t, store, 2)
		})
	}
}

func TestFileWatcher_PopulatedMarkdownSuffixDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	store := newTestStorage(t)
	fw, err := NewFileWatcher(root, NewIndexer(root, store), &FileWatcherOptions{DebounceMs: 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := fw.Start(); err != nil {
		t.Fatal(err)
	}
	defer fw.Stop()

	// Populate outside the watched tree, then move it in. Only a real
	// directory-create event can discover first.md; no child event helps.
	staged := t.TempDir()
	if err := os.Mkdir(filepath.Join(staged, "note"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, "note/first.md"), []byte(noteContent("First")), 0o644); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "projects/example.md")
	if err := os.Rename(staged, destination); err != nil {
		t.Fatal(err)
	}
	waitForNoteCount(t, store, 1)
	want := []string{".", "projects", "projects/example.md", "projects/example.md/note"}
	if got := watchPaths(fw); !reflect.DeepEqual(got, want) {
		t.Fatalf("watched directories = %v, want %v", got, want)
	}

	// A subsequent file write proves this was watched, not merely swept.
	if err := os.WriteFile(filepath.Join(destination, "note/second.md"), []byte(noteContent("Second")), 0o644); err != nil {
		t.Fatal(err)
	}
	waitForNoteCount(t, store, 2)
	if err := os.Remove(filepath.Join(destination, "note/first.md")); err != nil {
		t.Fatal(err)
	}
	waitForNoteCount(t, store, 1)
}

func TestFileWatcher_NoSymlinkDirectoryRecursion(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte(noteContent("Secret")), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "global/link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	fw, _ := NewFileWatcher(root, nil, &FileWatcherOptions{DebounceMs: 60000})
	if err := fw.Start(); err != nil {
		t.Fatal(err)
	}
	defer fw.Stop()
	fw.handleEvent(fsnotify.Event{Name: link, Op: fsnotify.Create})
	fw.addDirRecursive(link)
	if got := watchPaths(fw); !reflect.DeepEqual(got, []string{".", "global"}) {
		t.Fatalf("symlink watched: %v", got)
	}
}
