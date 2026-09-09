package indexer

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/huynle/brain-api/internal/tenantfs"
)

// defaultIgnorePatterns are always ignored by the file watcher.
var defaultIgnorePatterns = []string{".brain-data/", ".zk/", "node_modules/"}

// FileWatcherOptions configures the file watcher.
type FileWatcherOptions struct {
	DebounceMs     int
	IgnorePatterns []string
}

// FileWatcher watches brainDir's projects/ and global/ trees for markdown changes
// and triggers incremental indexing via the Indexer.
type FileWatcher struct {
	brainDir       string
	indexer        *Indexer
	watcher        *fsnotify.Watcher
	running        bool
	debounceMs     int
	ignorePatterns []string
	pendingChanges map[string]string // path → action: "index"/"remove"
	mu             sync.Mutex
	stopCh         chan struct{}
	debounceTimer  *time.Timer
	stopRootWatch  func()
	lifecycleMu    sync.Mutex // Serializes Start/Stop, never held by event-loop cleanup.
	loopDone       chan struct{}
	flushWG        sync.WaitGroup
}

// NewFileWatcher creates a new FileWatcher.
func NewFileWatcher(brainDir string, indexer *Indexer, opts *FileWatcherOptions) (*FileWatcher, error) {
	debounceMs := 100
	var extraPatterns []string

	if opts != nil {
		if opts.DebounceMs > 0 {
			debounceMs = opts.DebounceMs
		}
		extraPatterns = opts.IgnorePatterns
	}

	patterns := make([]string, 0, len(defaultIgnorePatterns)+len(extraPatterns))
	patterns = append(patterns, defaultIgnorePatterns...)
	patterns = append(patterns, extraPatterns...)

	return &FileWatcher{
		brainDir:       brainDir,
		indexer:        indexer,
		debounceMs:     debounceMs,
		ignorePatterns: patterns,
		pendingChanges: make(map[string]string),
	}, nil
}

// Start watches content directories plus a root anchor for newly created roots.
// Idempotent — calling Start() when already running is a no-op.
func (fw *FileWatcher) Start() error {
	fw.lifecycleMu.Lock()
	defer fw.lifecycleMu.Unlock()
	fw.mu.Lock()
	defer fw.mu.Unlock()

	if fw.running {
		return nil
	}
	if fw.loopDone != nil {
		// A fatal loop exit may still be cleaning up. Do not reuse its fields
		// or WaitGroup until that generation has completely drained.
		done := fw.loopDone
		fw.mu.Unlock()
		<-done
		fw.mu.Lock()
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	fw.watcher = w
	fw.stopCh = make(chan struct{})
	var rootChanges <-chan error
	var stopRootWatch func()

	// Keep brainDir watched even when projects/global do not yet exist. Prune
	// all other siblings before WalkDir reads their contents.
	err = filepath.WalkDir(fw.brainDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(fw.brainDir, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if rel != "." && !inContentScope(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if err := fw.indexer.admit(rel, false); err != nil {
			if rel == "." || !errors.Is(err, tenantfs.ErrDenied) {
				return err
			}
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if rel == "." {
				// Admit the root, then register before WalkDir snapshots any
				// children. Changes during the initial scan remain buffered.
				rootChanges, stopRootWatch, err = watchRootChanges(path)
				if err != nil {
					return err
				}
			}
			relPath, relErr := filepath.Rel(fw.brainDir, path)
			if relErr != nil {
				return relErr
			}
			relPath = filepath.ToSlash(relPath)
			// Skip ignored directories
			if relPath != "." && (!inContentScope(relPath) || fw.shouldIgnoreDir(relPath)) {
				return filepath.SkipDir
			}
			addErr := w.Add(path)
			// kqueue registers the root before opening its children. A dangling
			// excluded sibling can fail that internal scan. Re-adding an already
			// registered root preserves the anchor; this walk still independently
			// admits and watches every content directory below it.
			if rel == "." && errors.Is(addErr, os.ErrNotExist) {
				return w.Add(path)
			}
			return addErr
		}
		return nil
	})
	if err != nil {
		w.Close()
		if stopRootWatch != nil {
			stopRootWatch()
		}
		return err
	}
	fw.stopRootWatch = stopRootWatch
	fw.running = true
	fw.loopDone = make(chan struct{})

	// Capture channels before starting goroutine — watcher may be nilled by Stop()
	events := w.Events
	errors := w.Errors
	stopCh := fw.stopCh

	// Start event loop in background goroutine
	go fw.eventLoop(events, errors, stopCh, rootChanges)

	return nil
}

// Stop signals shutdown and drains the event loop and its debounce callbacks.
// Holding lifecycleMu prevents Start from reusing the generation during cleanup.
func (fw *FileWatcher) Stop() {
	fw.lifecycleMu.Lock()
	defer fw.lifecycleMu.Unlock()
	fw.mu.Lock()
	fw.signalStopLocked()
	done := fw.loopDone
	fw.mu.Unlock()
	if done != nil {
		<-done
	}
}

// Must be called with mu held. A stopped timer cannot run its deferred Done.
func (fw *FileWatcher) signalStopLocked() {
	if fw.running {
		fw.running = false
		close(fw.stopCh)
	}
	if fw.debounceTimer != nil {
		if fw.debounceTimer.Stop() {
			fw.flushWG.Done()
		}
		fw.debounceTimer = nil
	}
}

// The event loop owns cleanup, including fatal exits. It never calls Stop or
// acquires lifecycleMu, so an external Stop can join it without a self-join.
func (fw *FileWatcher) finishWatch() {
	fw.mu.Lock()
	fw.signalStopLocked()
	w := fw.watcher
	fw.watcher = nil
	fw.pendingChanges = make(map[string]string)
	stopRootWatch := fw.stopRootWatch
	fw.stopRootWatch = nil
	fw.mu.Unlock()

	// Close outside the lock to avoid deadlock with event loop
	if w != nil {
		w.Close()
	}
	if stopRootWatch != nil {
		stopRootWatch()
	}
	fw.flushWG.Wait()
	close(fw.loopDone)
}

// IsRunning returns true if the watcher is currently active.
func (fw *FileWatcher) IsRunning() bool {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	return fw.running
}

// eventLoop processes fsnotify events until Stop() is called.
func (fw *FileWatcher) eventLoop(events chan fsnotify.Event, errors chan error, stopCh chan struct{}, rootChanges <-chan error) {
	defer fw.finishWatch()
	for {
		select {
		case <-stopCh:
			return
		case err, ok := <-rootChanges:
			if !ok {
				return
			}
			select {
			case <-stopCh:
				return
			default:
			}
			if err == nil {
				err = fw.rescanContentRoots()
			}
			if err != nil {
				slog.Error("content root watch failed", "error", err)
				return
			}
		case event, ok := <-events:
			if !ok {
				return
			}
			fw.handleEvent(event)
		case _, ok := <-errors:
			if !ok {
				return
			}
			// Log errors but don't crash
		}
	}
}

// A root notification can arrive without fsnotify child events on kqueue.
// Never enumerate unrelated siblings here, and never turn admission errors
// into absent content. Lstat preserves the no-directory-symlink-recursion rule.
func (fw *FileWatcher) rescanContentRoots() error {
	if err := fw.indexer.admit(".", false); err != nil {
		return err
	}
	for _, name := range []string{"projects", "global"} {
		if fw.shouldIgnoreDir(name) {
			continue
		}
		path := filepath.Join(fw.brainDir, name)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err := fw.indexer.admit(name, false); err != nil {
			return err
		}
		if info.IsDir() {
			if err := fw.addDirRecursive(path); err != nil {
				return err
			}
		}
	}
	return nil
}

// handleEvent processes a single fsnotify event.
func (fw *FileWatcher) handleEvent(event fsnotify.Event) {
	// Get relative path
	relPath, err := filepath.Rel(fw.brainDir, event.Name)
	if err != nil {
		return
	}
	relPath = filepath.ToSlash(relPath)
	if !inContentScope(relPath) {
		return
	}
	if err := fw.indexer.admit(relPath, true); err != nil {
		return
	}

	// Classify created directories before filtering file extensions: a
	// directory can itself end in .md. Lstat avoids following directory symlinks.
	if event.Has(fsnotify.Create) {
		info, err := os.Lstat(event.Name)
		if err == nil && info.IsDir() {
			if !fw.shouldIgnoreDir(relPath) {
				_ = fw.addDirRecursive(event.Name) // Existing async Create handling is best-effort.
			}
			return
		}
	}

	if !fw.indexableMarkdown(relPath) {
		return
	}

	// Determine action: if file exists on disk, index it; otherwise remove it
	var action string
	if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
		action = "remove"
	} else {
		// Create or Write — check if file exists
		if _, err := os.Stat(event.Name); os.IsNotExist(err) {
			action = "remove"
		} else {
			action = "index"
		}
	}

	fw.queueChange(relPath, action)
}

// addDirRecursive watches absDir and every subdirectory under it, and queues
// any markdown already inside for indexing.
//
// A Create event names only the directory that was created, but the OS can
// populate a whole tree beneath it before the event is handled — `git pull`
// and `mkdir -p` both outrun the watcher. Adding just the named directory
// leaves a pulled `projects/foo/note/` subtree unwatched and its files
// unindexed, which is the exact case this watcher exists to cover.
//
// This runs on the event loop goroutine, so a very large new tree delays
// subsequent events. That is preferable to the alternative of losing the
// subtree entirely.
func (fw *FileWatcher) addDirRecursive(absDir string) error {
	return filepath.WalkDir(absDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, relErr := filepath.Rel(fw.brainDir, path)
		if relErr != nil {
			return relErr
		}
		relPath = filepath.ToSlash(relPath)
		if !inContentScope(relPath) || (d.IsDir() && fw.shouldIgnoreDir(relPath)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if err := fw.indexer.admit(relPath, false); err != nil {
			return err
		}

		if d.IsDir() {
			// Stop signals running=false before joining this walk. Do not
			// register further watches while shutdown waits for it to drain.
			fw.mu.Lock()
			if !fw.running {
				fw.mu.Unlock()
				return filepath.SkipAll
			}
			addErr := fw.watcher.Add(path)
			fw.mu.Unlock()
			return addErr
		}

		if fw.indexableMarkdown(relPath) {
			fw.queueChange(relPath, "index")
		}
		return nil
	})
}

// indexableMarkdown reports whether a relative path is markdown the watcher
// should track — not under an ignore pattern, not a dotfile or editor backup.
func (fw *FileWatcher) indexableMarkdown(relativePath string) bool {
	if !inContentScope(relativePath) || !strings.HasSuffix(relativePath, ".md") {
		return false
	}
	if fw.shouldIgnore(relativePath) {
		return false
	}
	base := filepath.Base(relativePath)
	return !strings.HasPrefix(base, ".") && !strings.HasSuffix(base, "~")
}

// queueChange records a pending action for relativePath and (re)arms the
// debounce timer.
func (fw *FileWatcher) queueChange(relativePath, action string) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if !fw.running {
		return
	}
	fw.pendingChanges[relativePath] = action
	fw.scheduleDebouncedFlush()
}

// shouldIgnore checks if a relative path matches any ignore pattern.
func (fw *FileWatcher) shouldIgnore(relativePath string) bool {
	for _, pattern := range fw.ignorePatterns {
		if strings.HasPrefix(relativePath, pattern) || strings.Contains(relativePath, "/"+pattern) {
			return true
		}
	}
	return false
}

// shouldIgnoreDir checks if a directory relative path matches any ignore pattern.
func (fw *FileWatcher) shouldIgnoreDir(relDir string) bool {
	dirWithSlash := relDir + "/"
	for _, pattern := range fw.ignorePatterns {
		if strings.HasPrefix(dirWithSlash, pattern) || strings.Contains(dirWithSlash, "/"+pattern) {
			return true
		}
	}
	return false
}

// scheduleDebouncedFlush resets the debounce timer.
// Must be called with fw.mu held.
func (fw *FileWatcher) scheduleDebouncedFlush() {
	if fw.debounceTimer != nil {
		if fw.debounceTimer.Stop() {
			fw.flushWG.Done()
		}
	}
	fw.flushWG.Add(1)
	var timer *time.Timer
	timer = time.AfterFunc(time.Duration(fw.debounceMs)*time.Millisecond, func() {
		defer fw.flushWG.Done()
		fw.mu.Lock()
		current := fw.debounceTimer == timer
		if current {
			fw.debounceTimer = nil
		}
		fw.mu.Unlock()
		if current {
			fw.flushPendingChanges()
		}
	})
	fw.debounceTimer = timer
}

// flushPendingChanges processes all accumulated changes.
func (fw *FileWatcher) flushPendingChanges() {
	fw.mu.Lock()
	// A timer that had already fired when Stop() ran would otherwise index
	// against a store the caller is about to close.
	if !fw.running {
		fw.pendingChanges = make(map[string]string)
		fw.mu.Unlock()
		return
	}
	// Snapshot and clear pending changes
	changes := make(map[string]string, len(fw.pendingChanges))
	for k, v := range fw.pendingChanges {
		changes[k] = v
	}
	fw.pendingChanges = make(map[string]string)
	fw.mu.Unlock()

	for relativePath, action := range changes {
		if action == "index" {
			_ = fw.indexer.IndexFile(relativePath)
		} else {
			_ = fw.indexer.RemoveFile(relativePath)
		}
	}
}
