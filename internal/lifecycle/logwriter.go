package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Default rotation limits for the server log writer.
const (
	DefaultLogMaxSizeMB  = 100
	DefaultLogMaxBackups = 5
)

// RotatingWriter is an io.Writer that appends to a log file and rotates it
// in place once it exceeds a size threshold. Because the writer owns the
// file handle and reopens it after rotation, log output keeps flowing to the
// configured path — unlike inherited stdout/stderr redirection, where a
// rename leaves the process writing to the rotated backup forever.
//
// It is safe for concurrent use.
type RotatingWriter struct {
	mu         sync.Mutex
	path       string
	maxBytes   int64
	maxBackups int
	file       *os.File
	size       int64
}

// NewRotatingWriter opens (creating if needed) the log file at path for
// appending. maxSizeMB and maxBackups fall back to DefaultLogMaxSizeMB and
// DefaultLogMaxBackups when <= 0.
func NewRotatingWriter(path string, maxSizeMB, maxBackups int) (*RotatingWriter, error) {
	if maxSizeMB <= 0 {
		maxSizeMB = DefaultLogMaxSizeMB
	}
	if maxBackups <= 0 {
		maxBackups = DefaultLogMaxBackups
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	w := &RotatingWriter{
		path:       path,
		maxBytes:   int64(maxSizeMB) * 1024 * 1024,
		maxBackups: maxBackups,
	}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

// Write appends p to the log file, rotating first if the write would push
// the file past the size threshold. Rotation failures are non-fatal: the
// write proceeds against the current file so log output is never dropped.
func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Recover a missing handle FIRST. A prior rotation whose reopen failed
	// (e.g. transient fd exhaustion) leaves w.file == nil with w.size stuck at
	// its large pre-rotation value. open() re-stats and resets w.size to the
	// real file size, so the rotation check below sees the truth instead of
	// re-triggering rotation every write and marching backups off the end.
	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}

	if w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		// Best-effort rotation; keep writing to the current file on failure.
		// rotate() only clears w.file on a rename it already committed, so a
		// failure here still leaves us with a live handle to write through.
		_ = w.rotate()
		if w.file == nil {
			if err := w.open(); err != nil {
				return 0, err
			}
		}
	}

	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// Reopen closes and reopens the log file at the configured path. Call after
// an external tool (SIGHUP handler, logrotate) has moved the file so writes
// resume at the configured path instead of following the renamed inode.
func (w *RotatingWriter) Reopen() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	return w.open()
}

// Close closes the underlying file.
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

// Path returns the configured log file path.
func (w *RotatingWriter) Path() string {
	return w.path
}

// open opens the log file for appending and records its current size.
// Caller must hold w.mu.
func (w *RotatingWriter) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("failed to stat log file: %w", err)
	}
	w.file = f
	w.size = info.Size()
	return nil
}

// rotate shifts backups (.1 -> .2, ...), renames the current file to .1,
// and reopens a fresh file at the configured path. Caller must hold w.mu.
func (w *RotatingWriter) rotate() error {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}

	if err := removeOldBackups(w.path, w.maxBackups); err != nil {
		return errJoinReopen(w, err)
	}
	if err := shiftBackups(w.path, w.maxBackups); err != nil {
		return errJoinReopen(w, err)
	}
	if err := os.Rename(w.path, w.path+".1"); err != nil {
		return errJoinReopen(w, fmt.Errorf("failed to rename log file: %w", err))
	}
	return w.open()
}

// errJoinReopen reopens the current log file after a failed rotation step so
// the writer stays usable, then returns the original error.
func errJoinReopen(w *RotatingWriter, err error) error {
	if openErr := w.open(); openErr != nil {
		return fmt.Errorf("%w (and failed to reopen log: %v)", err, openErr)
	}
	return err
}
