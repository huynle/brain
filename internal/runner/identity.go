package runner

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
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

// PeekRunnerID returns the runner id already persisted in stateDir, or "" if
// none exists yet. Unlike ResolveRunnerID it never creates one: inspection
// commands must not mint an id for a runner that has never started.
func PeekRunnerID(stateDir string) string {
	if strings.TrimSpace(stateDir) == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(stateDir, runnerIDFileName))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
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

// =============================================================================
// Runner Names (multiple runners on one machine)
// =============================================================================

// DefaultRunnerName is the implicit name of an unnamed runner. It deliberately
// maps back to the historical single-runner paths — state dir, daemon pid file,
// daemon log file — so an existing deployment keeps the runner id it already
// persisted instead of re-registering as a brand-new runner.
const DefaultRunnerName = "default"

// RunnerNameLabel is the label key under which the runner name is advertised to
// the scheduler. Runner ids are random hex, so on a host with several runners
// the name is the only human-readable way to tell two rows apart.
const RunnerNameLabel = "name"

// maxRunnerNameLen bounds a name that becomes a path segment.
const maxRunnerNameLen = 64

// runnerNamePattern accepts names that are safe as both a path segment and a
// label value.
var runnerNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// NormalizeRunnerName validates a runner name and returns its canonical form;
// an empty name is the default runner.
//
// Names become path segments (state dir, pid file, log file), so anything that
// could escape its directory is REJECTED rather than sanitized: a silently
// rewritten name would resolve to a different runner id than the operator
// typed, and "my runner stopped picking up work" is a miserable way to find
// that out.
func NormalizeRunnerName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return DefaultRunnerName, nil
	}
	if len(trimmed) > maxRunnerNameLen {
		return "", fmt.Errorf("invalid runner name %q: longer than %d characters", trimmed, maxRunnerNameLen)
	}
	if !runnerNamePattern.MatchString(trimmed) {
		return "", fmt.Errorf("invalid runner name %q: start with a letter or digit and use only letters, digits, '.', '_' or '-'", trimmed)
	}
	return trimmed, nil
}

// RunnerStateDir returns the state directory for the named runner: base for the
// default runner, base/<name> for any other.
//
// Distinct state dirs are what make two runners on one host distinct.
// ResolveRunnerID persists the runner id inside the state dir, so a shared dir
// hands both processes the SAME id — they then register as one runner, both
// subscribe to that id's dispatch stream, and race each other for every command
// the scheduler sends.
func RunnerStateDir(base, name string) string {
	if name == "" || name == DefaultRunnerName {
		return base
	}
	return filepath.Join(base, name)
}

// ResolveRunnerIdentity fills in the name-derived parts of a runner config: the
// canonical name, the per-runner state dir, and the name label advertised at
// registration.
//
// Call it EXACTLY ONCE per config, after CLI flags have been merged onto the
// file/env values. It is not idempotent — a second call would nest another
// <name> segment under the state dir. runnercli.prepareRunnerConfig is that
// single call site, the same way NewExecutorRegistry is the single site that
// derives MachineID.
func ResolveRunnerIdentity(cfg *RunnerConfig) error {
	name, err := NormalizeRunnerName(cfg.Name)
	if err != nil {
		return err
	}
	cfg.Name = name

	if strings.TrimSpace(cfg.StateDir) == "" {
		cfg.StateDir = DefaultStateDir()
	}
	cfg.StateDir = RunnerStateDir(cfg.StateDir, name)

	// An explicit label wins: an operator who set labels.name meant it.
	if cfg.Labels == nil {
		cfg.Labels = map[string]string{}
	}
	if strings.TrimSpace(cfg.Labels[RunnerNameLabel]) == "" {
		cfg.Labels[RunnerNameLabel] = name
	}
	return nil
}

// =============================================================================
// Auto-named runners (`--new`)
// =============================================================================

// autoRunnerNamePrefix is the stem of a name handed out by `--new`. The unnamed
// default runner is slot 1, so allocation starts at 2 and the number reads as
// "the Nth runner on this machine".
const autoRunnerNamePrefix = "runner-"

// maxAutoRunnerSlots bounds the search. Well past any plausible number of
// runners on one host, and it keeps a bug from spinning forever.
const maxAutoRunnerSlots = 64

// RunnerNameLive reports whether a runner is currently running out of the named
// runner's state dir, judged by the per-project pid files the runner writes
// there itself. This is what covers foreground, TUI and embedded runners, which
// write no daemon pid file — handing one of them a name already in use would
// put two processes on one runner id.
func RunnerNameLive(baseStateDir, name string) bool {
	dir := RunnerStateDir(baseStateDir, name)
	for _, st := range FindAllRunnerStates(dir) {
		if NewStateManager(dir, st.ProjectID).IsPidRunning() {
			return true
		}
	}
	return false
}

// NextFreeRunnerName returns the lowest unused "runner-N" (N >= 2) under
// baseStateDir. inUse, when non-nil, is consulted alongside the state-dir check
// so a caller can add its own notion of "taken" (the daemon adds its pid files).
//
// A slot whose runner is dead is REUSED rather than skipped: its state dir still
// holds that runner's id, so reusing it keeps the id stable across a crash
// instead of leaking a fresh identity and an orphaned directory every restart.
func NextFreeRunnerName(baseStateDir string, inUse func(name string) bool) (string, error) {
	if strings.TrimSpace(baseStateDir) == "" {
		baseStateDir = DefaultStateDir()
	}
	for n := 2; n <= maxAutoRunnerSlots; n++ {
		name := fmt.Sprintf("%s%d", autoRunnerNamePrefix, n)
		if RunnerNameLive(baseStateDir, name) {
			continue
		}
		if inUse != nil && inUse(name) {
			continue
		}
		return name, nil
	}
	return "", fmt.Errorf("no free runner name: %s2..%s%d are all in use; name this runner explicitly with --name",
		autoRunnerNamePrefix, autoRunnerNamePrefix, maxAutoRunnerSlots)
}
