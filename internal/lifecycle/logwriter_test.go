package lifecycle

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// newTestWriter creates a RotatingWriter with a small byte threshold by
// reaching into the struct, since NewRotatingWriter's MB granularity is too
// coarse for tests.
func newTestWriter(t *testing.T, path string, maxBytes int64, maxBackups int) *RotatingWriter {
	t.Helper()
	w, err := NewRotatingWriter(path, 1, maxBackups)
	if err != nil {
		t.Fatalf("NewRotatingWriter failed: %v", err)
	}
	w.maxBytes = maxBytes
	t.Cleanup(func() { w.Close() })
	return w
}

func TestRotatingWriter_AppendsToExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	if err := os.WriteFile(path, []byte("existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	w, err := NewRotatingWriter(path, 0, 0)
	if err != nil {
		t.Fatalf("NewRotatingWriter failed: %v", err)
	}
	defer w.Close()

	if _, err := w.Write([]byte("new line\n")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "existing\nnew line\n" {
		t.Errorf("expected appended content, got %q", data)
	}
}

func TestRotatingWriter_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deeper", "test.log")

	w, err := NewRotatingWriter(path, 0, 0)
	if err != nil {
		t.Fatalf("NewRotatingWriter failed: %v", err)
	}
	defer w.Close()

	if _, err := w.Write([]byte("hello\n")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected log file to exist: %v", err)
	}
}

func TestRotatingWriter_RotatesAtThreshold(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	w := newTestWriter(t, path, 100, 3)

	line := bytes.Repeat([]byte("x"), 40)
	line = append(line, '\n')

	// 3 writes of 41 bytes: third write would exceed 100 bytes → rotation.
	for i := 0; i < 3; i++ {
		if _, err := w.Write(line); err != nil {
			t.Fatalf("Write %d failed: %v", i, err)
		}
	}

	backup, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("expected backup file after rotation: %v", err)
	}
	if len(backup) != 82 {
		t.Errorf("expected 82 bytes in backup, got %d", len(backup))
	}

	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(current) != 41 {
		t.Errorf("expected 41 bytes in current log, got %d", len(current))
	}
}

func TestRotatingWriter_ShiftsBackups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	w := newTestWriter(t, path, 10, 2)

	// Each write is 11 bytes > threshold, so every subsequent write rotates.
	for i := 0; i < 4; i++ {
		if _, err := w.Write([]byte("0123456789\n")); err != nil {
			t.Fatalf("Write %d failed: %v", i, err)
		}
	}

	for _, backup := range []string{path + ".1", path + ".2"} {
		if _, err := os.Stat(backup); err != nil {
			t.Errorf("expected backup %s to exist: %v", backup, err)
		}
	}
	// maxBackups=2 → .3 must never be created.
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Errorf("expected no backup beyond maxBackups, found %s.3", path)
	}
}

func TestRotatingWriter_ReopenAfterExternalRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	w := newTestWriter(t, path, 1<<20, 3)

	if _, err := w.Write([]byte("before\n")); err != nil {
		t.Fatal(err)
	}

	// Simulate external rotation (e.g. SIGHUP handler or logrotate).
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	if err := w.Reopen(); err != nil {
		t.Fatalf("Reopen failed: %v", err)
	}

	if _, err := w.Write([]byte("after\n")); err != nil {
		t.Fatal(err)
	}

	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected recreated log file: %v", err)
	}
	if !strings.Contains(string(current), "after") {
		t.Errorf("expected post-reopen writes at configured path, got %q", current)
	}
	if strings.Contains(string(current), "before") {
		t.Errorf("expected pre-rotation content only in backup, got %q", current)
	}
}

func TestRotatingWriter_RecoversFromNilFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	w := newTestWriter(t, path, 1<<20, 3)

	// Simulate the state left by a rotation whose reopen failed: handle closed
	// and cleared. Write must not nil-deref; it must reopen and succeed.
	w.mu.Lock()
	_ = w.file.Close()
	w.file = nil
	w.mu.Unlock()

	if _, err := w.Write([]byte("after recovery\n")); err != nil {
		t.Fatalf("Write after nil file failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "after recovery") {
		t.Errorf("expected recovered write in log, got %q", data)
	}
}

func TestRotatingWriter_ConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	w := newTestWriter(t, path, 500, 3)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if _, err := w.Write([]byte("concurrent line\n")); err != nil {
					t.Errorf("Write failed: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	// All 200 lines must land intact across current file + backups.
	total := 0
	for _, p := range []string{path, path + ".1", path + ".2", path + ".3"} {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		total += strings.Count(string(data), "concurrent line\n")
	}
	if total == 0 {
		t.Fatal("expected log lines to be written")
	}
}
