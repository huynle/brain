package runner

import (
	"os"
	"testing"
	"time"
)

// clearStallEnv unsets the env vars that influence StallTimeout resolution so
// the default path is exercised cleanly.
func clearStallEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"RUNNER_STALL_TIMEOUT", "RUNNER_IDLE_THRESHOLD"} {
		os.Unsetenv(key)
	}
}

// StallTimeout defaults to 600000 (10m) when neither env nor file set it.
func TestLoadConfig_StallTimeoutDefault(t *testing.T) {
	clearStallEnv(t)

	cfg, err := LoadConfigFrom("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.StallTimeout != 600000 {
		t.Errorf("StallTimeout = %d, want 600000", cfg.StallTimeout)
	}
}

// RUNNER_STALL_TIMEOUT overrides the default.
func TestLoadConfig_StallTimeoutEnvOverride(t *testing.T) {
	clearStallEnv(t)
	t.Setenv("RUNNER_STALL_TIMEOUT", "120000")

	cfg, err := LoadConfigFrom("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.StallTimeout != 120000 {
		t.Errorf("StallTimeout = %d, want 120000", cfg.StallTimeout)
	}
}

// A negative StallTimeout fails validation, mirroring the
// idleDetectionThreshold < 0 check.
func TestValidateConfig_NegativeStallTimeout(t *testing.T) {
	cfg := RunnerConfig{
		PollInterval:      1,
		MaxParallel:       2,
		HeartbeatInterval: 30,
		StallTimeout:      -1,
	}
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for stallTimeout < 0")
	}
}

// A zero StallTimeout is valid (disables the feature).
func TestValidateConfig_ZeroStallTimeoutOK(t *testing.T) {
	cfg := RunnerConfig{
		PollInterval:      1,
		MaxParallel:       2,
		HeartbeatInterval: 30,
		StallTimeout:      0,
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("unexpected error for stallTimeout == 0: %v", err)
	}
}

// stallTimeout() returns 0 when disabled and the ms-scaled duration otherwise.
func TestStallTimeoutAccessor(t *testing.T) {
	tests := []struct {
		name string
		cfg  int
		want time.Duration
	}{
		{"disabled zero", 0, 0},
		{"negative disabled", -5, 0},
		{"ten minutes", 600000, 10 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &TaskRunner{config: RunnerConfig{StallTimeout: tt.cfg}}
			if got := tr.stallTimeout(); got != tt.want {
				t.Errorf("stallTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}
