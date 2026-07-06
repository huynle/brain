package mcp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/identity"
)

// withConfigDir overrides XDG_CONFIG_HOME to point at a temp dir for the
// duration of the test, so identity files are written into a sandbox.
func withConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return filepath.Join(dir, "brain")
}

func TestLoadOrCreateMCPClientID_PersistsAndReuses(t *testing.T) {
	cfgDir := withConfigDir(t)

	first := LoadOrCreateMCPClientID()
	if !strings.HasPrefix(first, "mcp-") {
		t.Fatalf("client id %q does not have mcp- prefix", first)
	}
	if got := len(first) - len("mcp-"); got != 8 {
		t.Fatalf("client id %q has %d hex chars, want 8", first, got)
	}

	// File should be created at <cfgDir>/mcp_client_id.
	path := filepath.Join(cfgDir, "mcp_client_id")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected client id file at %q: %v", path, err)
	}
	if got := strings.TrimSpace(string(b)); got != first {
		t.Fatalf("file contents = %q, want %q", got, first)
	}

	// Second call returns the same id (no rewrite).
	second := LoadOrCreateMCPClientID()
	if second != first {
		t.Fatalf("client id is unstable: %q vs %q", first, second)
	}
}

func TestLoadOrCreateMCPClientID_DoesNotTouchPluginFile(t *testing.T) {
	cfgDir := withConfigDir(t)

	// Pre-seed the legacy plugin's opencode_client_id file. The MCP loader
	// must not read, write, or delete it — they coexist during the
	// plugin-to-MCP transition.
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pluginPath := filepath.Join(cfgDir, "opencode_client_id")
	pluginID := "host-pre-existing-id\n"
	if err := os.WriteFile(pluginPath, []byte(pluginID), 0o644); err != nil {
		t.Fatal(err)
	}

	mcpID := LoadOrCreateMCPClientID()
	if strings.TrimSpace(mcpID) == strings.TrimSpace(pluginID) {
		t.Fatalf("MCP client id %q must not equal plugin id %q", mcpID, pluginID)
	}

	got, err := os.ReadFile(pluginPath)
	if err != nil {
		t.Fatalf("plugin id file disappeared: %v", err)
	}
	if string(got) != pluginID {
		t.Fatalf("plugin id file was modified: got %q want %q", got, pluginID)
	}
}

func TestLoadOrCreateMCPClientID_DegradedWhenNoConfigDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	id := LoadOrCreateMCPClientID()
	if id == "" {
		t.Fatal("LoadOrCreateMCPClientID returned empty id with no config dir")
	}
	if !strings.HasPrefix(id, "mcp-") {
		t.Fatalf("ephemeral id %q missing mcp- prefix", id)
	}
}

func TestGetExecutionContext_PopulatesIdentityFields(t *testing.T) {
	withConfigDir(t)

	ctx := GetExecutionContext(t.TempDir())

	if ctx.ClientID == "" || !strings.HasPrefix(ctx.ClientID, "mcp-") {
		t.Errorf("ClientID = %q, want mcp-<hex>", ctx.ClientID)
	}
	if ctx.HostID == "" || !strings.HasPrefix(ctx.HostID, "machine_") {
		t.Errorf("HostID = %q, want machine_<hex>", ctx.HostID)
	}
	if ctx.HostID != identity.ResolveMachineID() {
		t.Errorf("HostID = %q does not match identity.ResolveMachineID() = %q",
			ctx.HostID, identity.ResolveMachineID())
	}
	if ctx.OS != runtime.GOOS {
		t.Errorf("OS = %q, want %q", ctx.OS, runtime.GOOS)
	}
	if ctx.Arch != runtime.GOARCH {
		t.Errorf("Arch = %q, want %q", ctx.Arch, runtime.GOARCH)
	}
	if ctx.Hostname == "" {
		t.Logf("Hostname is empty (some sandboxes return empty); not failing")
	}
	// Username and HomeDir are best-effort; they should be non-empty in a
	// normal test environment but we don't fail the test on a sandbox that
	// strips them.
	if ctx.HomeDir == "" {
		t.Logf("HomeDir is empty (sandbox)")
	}
}

func TestGetCachedContext_CachesIdentity(t *testing.T) {
	withConfigDir(t)

	// Ensure no stale cache from earlier tests.
	cachedContext = nil
	defer func() { cachedContext = nil }()

	first := GetCachedContext()
	second := GetCachedContext()

	if first.ClientID != second.ClientID {
		t.Errorf("ClientID changed across calls: %q vs %q",
			first.ClientID, second.ClientID)
	}
	if first.HostID != second.HostID {
		t.Errorf("HostID changed across calls: %q vs %q",
			first.HostID, second.HostID)
	}
}
