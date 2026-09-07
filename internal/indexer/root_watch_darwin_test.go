package indexer

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/tenantfs"
)

// The real repository seam controls an admission inside the traversal, without
// adding a production startup/lifecycle hook. All coordination is channel-based.
type blockedWatchRoots struct {
	tenantfs.Repository
	mu      sync.Mutex
	calls   int
	blockAt int
	entered chan struct{}
	release chan struct{}
}

func (r *blockedWatchRoots) ListTenantRoots(ctx context.Context) ([]tenantfs.Mapping, error) {
	r.mu.Lock()
	r.calls++
	block := r.calls == r.blockAt
	r.mu.Unlock()
	if block {
		close(r.entered)
		<-r.release
	}
	return r.Repository.ListTenantRoots(ctx)
}

func awaitWatchSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for watcher interleaving")
	}
}

func TestFileWatcher_RootCreatedDuringInitialScan(t *testing.T) {
	root := t.TempDir()
	store := newTestStorage(t)
	repo := &blockedWatchRoots{Repository: store, entered: make(chan struct{}), release: make(chan struct{})}
	roots, err := tenantfs.New(repo, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := roots.ProvisionLocal(context.Background(), root, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "global"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "aaa")); err != nil {
		t.Fatal(err)
	}
	staged := t.TempDir()
	if err := os.MkdirAll(filepath.Join(staged, "p/note"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, "p/note/first.md"), []byte(noteContent("First")), 0600); err != nil {
		t.Fatal(err)
	}
	fw, err := NewFileWatcher(root, NewIndexer(root, store, roots.Brain(tenant.Local)), &FileWatcherOptions{DebounceMs: 20})
	if err != nil {
		t.Fatal(err)
	}
	// Admission #2 is global, after WalkDir has snapshotted the root's
	// children (aaa, global). A subsequently created projects is absent from
	// that snapshot, and aaa prevents fsnotify from delivering its Create.
	repo.mu.Lock()
	repo.calls, repo.blockAt = 0, 2
	repo.mu.Unlock()
	var release sync.Once
	defer func() { release.Do(func() { close(repo.release) }); fw.Stop() }()
	started := make(chan error, 1)
	go func() { started <- fw.Start() }()
	awaitWatchSignal(t, repo.entered)
	if err := os.Rename(staged, filepath.Join(root, "projects")); err != nil {
		t.Fatal(err)
	}
	release.Do(func() { close(repo.release) })
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	waitForNoteCount(t, store, 1)
	if err := os.WriteFile(filepath.Join(root, "projects/p/note/second.md"), []byte(noteContent("Second")), 0600); err != nil {
		t.Fatal(err)
	}
	waitForNoteCount(t, store, 2)
}

func TestFileWatcher_StopDrainsWorkBeforeRestart(t *testing.T) {
	for _, work := range []string{"rescan", "debounce flush"} {
		t.Run(work, func(t *testing.T) {
			root := t.TempDir()
			store := newTestStorage(t)
			repo := &blockedWatchRoots{Repository: store, entered: make(chan struct{}), release: make(chan struct{})}
			roots, err := tenantfs.New(repo, root)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := roots.ProvisionLocal(context.Background(), root, t.TempDir()); err != nil {
				t.Fatal(err)
			}
			path := "projects/p/note/first.md"
			if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, path), []byte(noteContent("First")), 0600); err != nil {
				t.Fatal(err)
			}
			fw, err := NewFileWatcher(root, NewIndexer(root, store, roots.Brain(tenant.Local)), &FileWatcherOptions{DebounceMs: 20})
			if err != nil {
				t.Fatal(err)
			}
			if err := fw.Start(); err != nil {
				t.Fatal(err)
			}
			var release sync.Once
			defer func() { release.Do(func() { close(repo.release) }); fw.Stop() }()
			repo.mu.Lock()
			repo.blockAt = repo.calls + 1
			repo.mu.Unlock()
			if work == "rescan" {
				// Only the independent root notification handles this excluded
				// file. Block the actual event-loop rescan at its root admission.
				if err := os.WriteFile(filepath.Join(root, "trigger"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				// Exercise the watcher's owned AfterFunc callback, not a manual
				// IndexFile invocation. Its registry read remains blocked.
				fw.queueChange(path, "index")
			}
			awaitWatchSignal(t, repo.entered)
			stopped := make(chan struct{})
			go func() { fw.Stop(); close(stopped) }()
			deadline := time.Now().Add(5 * time.Second)
			for fw.IsRunning() {
				if time.Now().After(deadline) {
					t.Fatal("Stop did not signal shutdown")
				}
				time.Sleep(time.Millisecond)
			}
			// Shutdown has been signaled, and registry work is still held by
			// our channel. Neither Stop nor a concurrent Start may complete.
			earlyStop := false
			select {
			case <-stopped:
				earlyStop = true
				t.Error("Stop returned while registry work was still blocked")
			case <-time.After(50 * time.Millisecond):
			}
			started := make(chan error, 1)
			go func() { started <- fw.Start() }()
			earlyStart := false
			select {
			case err := <-started:
				earlyStart = true
				t.Errorf("Start completed before old work drained: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			release.Do(func() { close(repo.release) })
			if !earlyStop {
				awaitWatchSignal(t, stopped)
			}
			if !earlyStart {
				select {
				case err := <-started:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("restart did not finish after releasing registry")
				}
			}
			fw.mu.Lock()
			if len(fw.pendingChanges) != 0 || fw.debounceTimer != nil {
				t.Error("new generation inherited old pending work or timer")
			}
			fw.mu.Unlock()
			want := 0
			if work == "debounce flush" {
				want = 1 // The in-flight write finishes BEFORE Stop returns.
			}
			if got := countNotes(t, store); got != want {
				t.Errorf("indexed count after drain = %d, want %d", got, want)
			}
			if err := os.WriteFile(filepath.Join(root, path), []byte(noteContent("Restarted")), 0600); err != nil {
				t.Fatal(err)
			}
			deadline = time.Now().Add(5 * time.Second)
			for {
				row, err := store.GetNoteByPath(context.Background(), path)
				if err != nil {
					t.Fatal(err)
				}
				if row != nil && row.Title == "Restarted" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("new generation did not index subsequent edit")
				}
				time.Sleep(20 * time.Millisecond)
			}
		})
	}
}

func TestFileWatcher_RootRecoveryAdmissionFailureStopsWatch(t *testing.T) {
	root := t.TempDir()
	store := newTestStorage(t)
	roots, err := tenantfs.New(store, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := roots.ProvisionLocal(context.Background(), root, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	fw, err := NewFileWatcher(root, NewIndexer(root, store, roots.Brain(tenant.Local)), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := fw.Start(); err != nil {
		t.Fatal(err)
	}
	defer fw.Stop()
	// The new root exists lexically, but its target is missing. That is a
	// policy failure, not an absent requested directory or an ignored sibling.
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "projects")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for fw.IsRunning() {
		if time.Now().After(deadline) {
			t.Fatal("root recovery admission failure left watcher running")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if countNotes(t, store) != 0 {
		t.Fatal("failed recovery indexed content")
	}
}

func TestRootWatchStopIsIdempotent(t *testing.T) {
	for i := 0; i < 10; i++ {
		changes, stop, err := watchRootChanges(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		stop()
		stop()
		if _, ok := <-changes; ok {
			t.Fatal("root watch did not stop")
		}
	}
}
