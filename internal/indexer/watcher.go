package indexer

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// defaultIgnorePatterns are always ignored by the file watcher.
var defaultIgnorePatterns = []string{".zk/", "node_modules/"}

// FileWatcherOptions configures the file watcher.
type FileWatcherOptions struct {
	DebounceMs     int
	IgnorePatterns []string
}

// FileWatcher watches brainDir recursively for markdown file changes
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

// Start begins watching brainDir recursively for .md file changes.
// Idempotent — calling Start() when already running is a no-op.
func (fw *FileWatcher) Start() error {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	if fw.running {
		return nil
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	fw.watcher = w
	fw.stopCh = make(chan struct{})

	// Add brainDir and all subdirectories recursively.
	err = filepath.WalkDir(fw.brainDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			relPath, relErr := filepath.Rel(fw.brainDir, path)
			if relErr != nil {
				return relErr
			}
			relPath = filepath.ToSlash(relPath)
			// Skip ignored directories
			if relPath != "." && fw.shouldIgnoreDir(relPath) {
				return filepath.SkipDir
			}
			return w.Add(path)
		}
		return nil
	})
	if err != nil {
		w.Close()
		return err
	}

	fw.running = true

	// Capture channels before starting goroutine — watcher may be nilled by Stop()
	events := w.Events
	errors := w.Errors
	stopCh := fw.stopCh

	// Start event loop in background goroutine
	go fw.eventLoop(events, errors, stopCh)

	return nil
}

// Stop stops watching and clears pending changes.
func (fw *FileWatcher) Stop() {
	fw.mu.Lock()
	if !fw.running {
		fw.mu.Unlock()
		return
	}

	// Signal the event loop to stop
	close(fw.stopCh)

	// Stop the debounce timer
	if fw.debounceTimer != nil {
		fw.debounceTimer.Stop()
		fw.debounceTimer = nil
	}

	// Close the watcher — this unblocks the event loop's select
	w := fw.watcher
	fw.watcher = nil
	fw.pendingChanges = make(map[string]string)
	fw.running = false
	fw.mu.Unlock()

	// Close outside the lock to avoid deadlock with event loop
	if w != nil {
		w.Close()
	}
}

// IsRunning returns true if the watcher is currently active.
func (fw *FileWatcher) IsRunning() bool {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	return fw.running
}

// eventLoop processes fsnotify events until Stop() is called.
func (fw *FileWatcher) eventLoop(events chan fsnotify.Event, errors chan error, stopCh chan struct{}) {
	for {
		select {
		case <-stopCh:
			return
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

// handleEvent processes a single fsnotify event.
func (fw *FileWatcher) handleEvent(event fsnotify.Event) {
	// Get relative path
	relPath, err := filepath.Rel(fw.brainDir, event.Name)
	if err != nil {
		return
	}
	relPath = filepath.ToSlash(relPath)

	// Only .md files
	if !strings.HasSuffix(relPath, ".md") {
		// If a new directory was created, add it to the watcher
		if event.Has(fsnotify.Create) {
			info, err := os.Stat(event.Name)
			if err == nil && info.IsDir() && !fw.shouldIgnoreDir(relPath) {
				fw.watcher.Add(event.Name)
			}
		}
		return
	}

	// Filter: ignore patterns
	if fw.shouldIgnore(relPath) {
		return
	}

	// Filter: temp files (dotfiles or backup files ending with ~)
	base := filepath.Base(relPath)
	if strings.HasPrefix(base, ".") || strings.HasSuffix(base, "~") {
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

	fw.mu.Lock()
	fw.pendingChanges[relPath] = action
	fw.scheduleDebouncedFlush()
	fw.mu.Unlock()
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
		fw.debounceTimer.Stop()
	}
	fw.debounceTimer = time.AfterFunc(time.Duration(fw.debounceMs)*time.Millisecond, func() {
		fw.flushPendingChanges()
	})
}

// flushPendingChanges processes all accumulated changes.
func (fw *FileWatcher) flushPendingChanges() {
	fw.mu.Lock()
	// Snapshot and clear pending changes
	changes := make(map[string]string, len(fw.pendingChanges))
	for k, v := range fw.pendingChanges {
		changes[k] = v
	}
	fw.pendingChanges = make(map[string]string)
	fw.debounceTimer = nil
	fw.mu.Unlock()

	for relativePath, action := range changes {
		if action == "index" {
			_ = fw.indexer.IndexFile(relativePath)
		} else {
			_ = fw.indexer.RemoveFile(relativePath)
		}
	}
}
