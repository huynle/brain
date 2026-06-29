// Package identity provides stable, persistent identifiers for the brain
// runner, the MCP server, and the host machine they run on.
//
// These identifiers used to live in internal/runner/identity.go. They were
// extracted into this package so the MCP stdio server can share the same
// machine-id file and format as the runner. This keeps Phase 2/3 affinity
// matching between MCP-stamped tasks and runner-claimed tasks correct.
//
// The contract that other packages depend on:
//
//   - ResolveMachineID() returns a stable id of the form "machine_<8 hex>"
//     persisted at $XDG_CONFIG_HOME/brain/machine-id (falling back to
//     ~/.config/brain/machine-id). All processes on the same host see the
//     same value.
//   - LoadOrCreateID(path, prefix) is the generic helper used to back both
//     machine-id and any per-component id files (e.g. mcp_client_id).
//   - ConfigDir() returns the resolved $XDG_CONFIG_HOME/brain (or
//     ~/.config/brain) directory, useful for callers that need to derive
//     additional file paths under the same config root.
//
// Failures during create/persist degrade to a process-ephemeral id rather
// than blocking startup; the runner has always done this and we preserve
// that behavior here.
package identity

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const (
	// machineIDFileName is the base name of the machine-wide id file under
	// the brain config dir.
	machineIDFileName = "machine-id"
)

// randomID returns prefix + 8 random hex chars (4 bytes).
func randomID(prefix string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// ConfigDir returns the directory that holds brain's user-level config files,
// honoring XDG_CONFIG_HOME and falling back to ~/.config/brain. Returns ""
// only when neither XDG_CONFIG_HOME nor a user home directory can be
// resolved.
func ConfigDir() string {
	if cfg := os.Getenv("XDG_CONFIG_HOME"); cfg != "" {
		return filepath.Join(cfg, "brain")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".config", "brain")
	}
	return ""
}

// ResolveMachineID returns a stable, machine-wide identifier shared by all
// brain processes on this host. It is created once and reused thereafter.
// If the config dir cannot be determined or written, a process-ephemeral id
// is returned so callers always get a non-empty value.
func ResolveMachineID() string {
	dir := ConfigDir()
	if dir == "" {
		return randomID("machine_")
	}
	return LoadOrCreateID(filepath.Join(dir, machineIDFileName), "machine_")
}

// LoadOrCreateID reads a trimmed id from path, or generates one with the
// given prefix and persists it. Failures degrade to an ephemeral id rather
// than blocking startup.
func LoadOrCreateID(path, prefix string) string {
	if b, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(b)); id != "" {
			return id
		}
	}
	id := randomID(prefix)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		slog.Warn("identity: cannot create dir for id file; using ephemeral id",
			"path", path, "error", err)
		return id
	}
	if err := os.WriteFile(path, []byte(id+"\n"), 0o644); err != nil {
		slog.Warn("identity: cannot persist id; using ephemeral id",
			"path", path, "error", err)
	}
	return id
}
