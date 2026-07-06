package identity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withConfigDir overrides XDG_CONFIG_HOME to point at a temp dir for the
// duration of the test, so ConfigDir() and ResolveMachineID() write into
// the sandbox.
func withConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return filepath.Join(dir, "brain")
}

func TestConfigDir_HonorsXDG(t *testing.T) {
	want := withConfigDir(t)
	if got := ConfigDir(); got != want {
		t.Fatalf("ConfigDir() = %q, want %q", got, want)
	}
}

func TestLoadOrCreateID_CreatesWhenMissing(t *testing.T) {
	dir := withConfigDir(t)
	path := filepath.Join(dir, "test-id")

	id := LoadOrCreateID(path, "test_")
	if !strings.HasPrefix(id, "test_") {
		t.Fatalf("id %q does not start with prefix test_", id)
	}
	// 8 hex chars after prefix.
	if got := len(id) - len("test_"); got != 8 {
		t.Fatalf("id %q has %d hex chars, want 8", id, got)
	}

	// File should exist with the same id plus trailing newline.
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected id file at %q: %v", path, err)
	}
	if got := strings.TrimSpace(string(b)); got != id {
		t.Fatalf("file contents = %q, want %q", got, id)
	}
	if !strings.HasSuffix(string(b), "\n") {
		t.Fatalf("file contents missing trailing newline: %q", string(b))
	}
}

func TestLoadOrCreateID_ReadsExisting(t *testing.T) {
	dir := withConfigDir(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "test-id")
	want := "test_deadbeef"
	if err := os.WriteFile(path, []byte(want+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := LoadOrCreateID(path, "test_")
	if got != want {
		t.Fatalf("LoadOrCreateID = %q, want %q", got, want)
	}
}

func TestLoadOrCreateID_RegeneratesOnEmpty(t *testing.T) {
	dir := withConfigDir(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "test-id")
	if err := os.WriteFile(path, []byte("   \n"), 0o644); err != nil {
		t.Fatal(err)
	}

	id := LoadOrCreateID(path, "test_")
	if id == "" || strings.TrimSpace(id) == "" {
		t.Fatalf("LoadOrCreateID returned empty id for empty file")
	}
	if !strings.HasPrefix(id, "test_") {
		t.Fatalf("id %q missing prefix", id)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(b)); got != id {
		t.Fatalf("file should be rewritten with new id: got %q want %q", got, id)
	}
}

func TestResolveMachineID_FormatAndStability(t *testing.T) {
	withConfigDir(t)

	first := ResolveMachineID()
	if !strings.HasPrefix(first, "machine_") {
		t.Fatalf("ResolveMachineID = %q, want machine_<hex>", first)
	}
	if len(first) != len("machine_")+8 {
		t.Fatalf("ResolveMachineID length = %d, want %d", len(first), len("machine_")+8)
	}

	second := ResolveMachineID()
	if first != second {
		t.Fatalf("ResolveMachineID is unstable: %q vs %q", first, second)
	}
}

func TestResolveMachineID_FallsBackWhenConfigDirMissing(t *testing.T) {
	// Force ConfigDir() to return "" by clearing both XDG_CONFIG_HOME and
	// HOME. ResolveMachineID should still return a non-empty ephemeral id.
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	id := ResolveMachineID()
	if id == "" {
		t.Fatal("ResolveMachineID returned empty id when ConfigDir is empty")
	}
	if !strings.HasPrefix(id, "machine_") {
		t.Fatalf("ephemeral id %q missing machine_ prefix", id)
	}
}
