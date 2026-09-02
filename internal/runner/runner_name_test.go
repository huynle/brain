package runner

import (
	"path/filepath"
	"strings"
	"testing"
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
