package runner

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Stable identity for runners and the machines they run on.
//
// Runner IDs used to be regenerated on every process start (4 random bytes),
// so a restarted runner looked brand-new and its instance/session records were
// orphaned. To make remote control able to find the machine that ran a past
// session, both identifiers are now persisted:
//
//   - machineID: machine-wide, shared by every runner on the host. Persisted
//     under the user's config dir so all runners (and restarts) agree on it.
//   - runnerID: per-runner, persisted in the runner's state dir. A given
//     deployment (one state dir) keeps the same runner ID across restarts;
//     separate state dirs on the same host get distinct IDs, which is how
//     "multiple runners per machine" stay individually addressable.

const (
	machineIDFileName = "machine-id"
	runnerIDFileName  = "runner-id"
)

// randomID returns prefix + 8 random hex chars (4 bytes).
func randomID(prefix string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// machineConfigDir returns the directory that holds the machine-wide id file,
// honoring XDG_CONFIG_HOME and falling back to ~/.config/brain.
func machineConfigDir() string {
	if cfg := os.Getenv("XDG_CONFIG_HOME"); cfg != "" {
		return filepath.Join(cfg, "brain")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".config", "brain")
	}
	return ""
}

// ResolveMachineID returns a stable, machine-wide identifier shared by all
// runners on this host. It is created once and reused thereafter. If the
// config dir cannot be determined or written, a process-ephemeral id is
// returned so callers always get a non-empty value.
func ResolveMachineID() string {
	dir := machineConfigDir()
	if dir == "" {
		return randomID("machine_")
	}
	return loadOrCreateID(filepath.Join(dir, machineIDFileName), "machine_")
}

// ResolveRunnerID returns a stable per-runner identifier persisted in the
// runner's state dir, so the same deployment re-registers under the same id
// across restarts. An empty stateDir (tests, transient runners) yields an
// ephemeral id.
func ResolveRunnerID(stateDir string) string {
	if strings.TrimSpace(stateDir) == "" {
		return randomID("runner_")
	}
	return loadOrCreateID(filepath.Join(stateDir, runnerIDFileName), "runner_")
}

// loadOrCreateID reads a trimmed id from path, or generates one with the given
// prefix and persists it. Failures degrade to an ephemeral id rather than
// blocking startup.
func loadOrCreateID(path, prefix string) string {
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
