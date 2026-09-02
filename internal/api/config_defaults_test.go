package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/huynle/brain-api/internal/config"
)

// loadFromDisk must report the config the server is actually RUNNING, which
// means defaults for every key the file omits.
//
// It used to decode into a zero-value struct, so an omitted key came back as
// its Go zero value. That is not a cosmetic reporting bug: HandleGet feeds
// the PWA, the Settings modal keeps the whole document as its working copy
// (SettingsModal `edited`), and HandlePut writes that whole document back.
// So one save of one unrelated field wrote every omitted key's ZERO to disk —
// server.port: 0, log_level: "", feature_checkout.enabled: false — and the
// next restart read them back.
func TestLoadFromDisk_OmittedKeysComeBackAsDefaultsNotZeros(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  brain_dir: /tmp/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := loadFromDisk(path)
	if err != nil {
		t.Fatalf("loadFromDisk: %v", err)
	}
	def := config.DefaultConfig()

	if got.Server.Port != def.Server.Port {
		t.Errorf("port = %d, want the default %d — a save would write this back",
			got.Server.Port, def.Server.Port)
	}
	if got.Server.LogLevel != def.Server.LogLevel {
		t.Errorf("log_level = %q, want %q", got.Server.LogLevel, def.Server.LogLevel)
	}
	// The one that made feature checkout look broken: it defaults to true, so
	// a zero-seeded read rendered the Settings toggle OFF while the feature
	// was ON, and the first save turned it genuinely off.
	if !got.Server.FeatureCheckout.Enabled {
		t.Error("feature_checkout.enabled = false, want the default true")
	}
	if got.Server.TaskDefaults.ExecutionMode != def.Server.TaskDefaults.ExecutionMode {
		t.Errorf("execution_mode = %q, want %q",
			got.Server.TaskDefaults.ExecutionMode, def.Server.TaskDefaults.ExecutionMode)
	}

	// And the file must still win over the default it overrides.
	if got.Server.BrainDir != "/tmp/x" {
		t.Errorf("brain_dir = %q, want the on-disk value /tmp/x", got.Server.BrainDir)
	}
}

// An explicit false in the file must survive, or the seeding would make the
// toggle impossible to turn off.
func TestLoadFromDisk_ExplicitFalseBeatsATrueDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "server:\n  feature_checkout:\n    enabled: false\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := loadFromDisk(path)
	if err != nil {
		t.Fatalf("loadFromDisk: %v", err)
	}
	if got.Server.FeatureCheckout.Enabled {
		t.Error("explicit `enabled: false` was overridden by the default")
	}
}
