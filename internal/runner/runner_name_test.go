package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeRunnerName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "empty is the default runner", input: "", want: DefaultRunnerName},
		{name: "whitespace only is the default runner", input: "  ", want: DefaultRunnerName},
		{name: "trims surrounding space", input: " worker-a ", want: "worker-a"},
		{name: "dots and underscores allowed", input: "worker_a.1", want: "worker_a.1"},
		{name: "path separator rejected", input: "a/b", wantErr: true},
		{name: "parent traversal rejected", input: "..", wantErr: true},
		{name: "leading dash rejected", input: "-worker", wantErr: true},
		{name: "space inside rejected", input: "worker a", wantErr: true},
		{name: "too long rejected", input: strings.Repeat("a", maxRunnerNameLen+1), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeRunnerName(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NormalizeRunnerName(%q) = %q, want error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeRunnerName(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("NormalizeRunnerName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRunnerStateDir(t *testing.T) {
	base := "/state/brain-runner"
	if got := RunnerStateDir(base, DefaultRunnerName); got != base {
		t.Errorf("default runner state dir = %q, want %q (unnamed runners must keep their existing id)", got, base)
	}
	if got := RunnerStateDir(base, ""); got != base {
		t.Errorf("empty name state dir = %q, want %q", got, base)
	}
	want := filepath.Join(base, "worker-a")
	if got := RunnerStateDir(base, "worker-a"); got != want {
		t.Errorf("named runner state dir = %q, want %q", got, want)
	}
}

func TestResolveRunnerIdentity(t *testing.T) {
	base := t.TempDir()

	t.Run("named runner gets its own state dir and label", func(t *testing.T) {
		cfg := RunnerConfig{Name: "worker-a", StateDir: base}
		if err := ResolveRunnerIdentity(&cfg); err != nil {
			t.Fatalf("ResolveRunnerIdentity: %v", err)
		}
		if cfg.Name != "worker-a" {
			t.Errorf("Name = %q, want worker-a", cfg.Name)
		}
		if want := filepath.Join(base, "worker-a"); cfg.StateDir != want {
			t.Errorf("StateDir = %q, want %q", cfg.StateDir, want)
		}
		if cfg.Labels[RunnerNameLabel] != "worker-a" {
			t.Errorf("labels[%s] = %q, want worker-a", RunnerNameLabel, cfg.Labels[RunnerNameLabel])
		}
	})

	t.Run("unnamed runner keeps the base state dir", func(t *testing.T) {
		cfg := RunnerConfig{StateDir: base}
		if err := ResolveRunnerIdentity(&cfg); err != nil {
			t.Fatalf("ResolveRunnerIdentity: %v", err)
		}
		if cfg.Name != DefaultRunnerName {
			t.Errorf("Name = %q, want %q", cfg.Name, DefaultRunnerName)
		}
		if cfg.StateDir != base {
			t.Errorf("StateDir = %q, want %q", cfg.StateDir, base)
		}
	})

	t.Run("empty state dir falls back to the default root", func(t *testing.T) {
		cfg := RunnerConfig{Name: "worker-b"}
		if err := ResolveRunnerIdentity(&cfg); err != nil {
			t.Fatalf("ResolveRunnerIdentity: %v", err)
		}
		if want := filepath.Join(DefaultStateDir(), "worker-b"); cfg.StateDir != want {
			t.Errorf("StateDir = %q, want %q", cfg.StateDir, want)
		}
	})

	t.Run("explicit name label wins", func(t *testing.T) {
		cfg := RunnerConfig{Name: "worker-a", StateDir: base, Labels: map[string]string{RunnerNameLabel: "custom"}}
		if err := ResolveRunnerIdentity(&cfg); err != nil {
			t.Fatalf("ResolveRunnerIdentity: %v", err)
		}
		if cfg.Labels[RunnerNameLabel] != "custom" {
			t.Errorf("labels[%s] = %q, want custom", RunnerNameLabel, cfg.Labels[RunnerNameLabel])
		}
	})

	t.Run("invalid name is rejected", func(t *testing.T) {
		cfg := RunnerConfig{Name: "../escape", StateDir: base}
		if err := ResolveRunnerIdentity(&cfg); err == nil {
			t.Fatalf("ResolveRunnerIdentity accepted %q, state dir became %q", "../escape", cfg.StateDir)
		}
	})
}

// Two runners on one machine are only distinct because their state dirs are.
// A shared dir hands both processes the same runner id, and they then fight
// over every dispatch the scheduler sends to it.
func TestResolveRunnerID_DistinctPerNamedStateDir(t *testing.T) {
	base := t.TempDir()

	a := ResolveRunnerID(RunnerStateDir(base, "worker-a"))
	b := ResolveRunnerID(RunnerStateDir(base, "worker-b"))
	if a == "" || b == "" {
		t.Fatalf("empty runner id: a=%q b=%q", a, b)
	}
	if a == b {
		t.Fatalf("named runners share a runner id: %q", a)
	}

	if again := ResolveRunnerID(RunnerStateDir(base, "worker-a")); again != a {
		t.Errorf("runner id not stable across restarts: %q then %q", a, again)
	}
	if peeked := PeekRunnerID(RunnerStateDir(base, "worker-a")); peeked != a {
		t.Errorf("PeekRunnerID = %q, want %q", peeked, a)
	}
	if peeked := PeekRunnerID(RunnerStateDir(base, "never-started")); peeked != "" {
		t.Errorf("PeekRunnerID minted an id for an unstarted runner: %q", peeked)
	}
}

func TestNextFreeRunnerName(t *testing.T) {
	base := t.TempDir()

	// Nothing running: the first auto name is runner-2, since the unnamed
	// default runner is slot 1.
	name, err := NextFreeRunnerName(base, nil)
	if err != nil || name != "runner-2" {
		t.Fatalf("NextFreeRunnerName = %q, %v; want runner-2, nil", name, err)
	}

	// Slots the caller reports as taken are skipped, in order.
	taken := map[string]bool{"runner-2": true, "runner-3": true}
	name, err = NextFreeRunnerName(base, func(n string) bool { return taken[n] })
	if err != nil || name != "runner-4" {
		t.Fatalf("NextFreeRunnerName = %q, %v; want runner-4, nil", name, err)
	}

	// A live runner in a name's state dir takes that name even with no daemon
	// pid file — that is the foreground/TUI case.
	live := RunnerStateDir(base, "runner-2")
	sm := NewStateManager(live, "demo")
	if err := os.MkdirAll(live, 0o755); err != nil {
		t.Fatal(err)
	}
	sm.Save(RunnerStatusPolling, nil, RunnerStats{}, time.Now())
	sm.SavePid(os.Getpid())
	name, err = NextFreeRunnerName(base, nil)
	if err != nil || name != "runner-3" {
		t.Fatalf("NextFreeRunnerName = %q, %v; want runner-3 (runner-2 is live), nil", name, err)
	}

	// A dead runner's slot is reused, so its persisted runner id survives a
	// crash instead of leaking a new identity per restart.
	sm.SavePid(999999)
	name, err = NextFreeRunnerName(base, nil)
	if err != nil || name != "runner-2" {
		t.Fatalf("NextFreeRunnerName = %q, %v; want runner-2 (stale slot reused), nil", name, err)
	}

	// Exhaustion is an error naming the fix, never a silent fallback to the
	// default runner's state dir.
	if _, err = NextFreeRunnerName(base, func(string) bool { return true }); err == nil {
		t.Fatal("NextFreeRunnerName returned a name with every slot taken")
	}
}
