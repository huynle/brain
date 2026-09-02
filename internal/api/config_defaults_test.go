package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/huynle/brain-api/internal/config"
	"gopkg.in/yaml.v3"
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

// The config file is read by TWO structs: config.UnifiedConfig (what the API
// serves and writes) and runner.RunnerConfig (same path, models fields the
// API's struct has never heard of). A plain marshal of the API's struct
// deleted every one of those on any successful save — and the Settings modal
// PUTs the whole document back after an edit to a single unrelated field.
func TestWriteToDisk_KeepsKeysTheAPIStructDoesNotModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := "" +
		"# a comment\n" +
		"brain_api_url: \"http://localhost:3402\"\n" +
		"max_parallel: 2\n" +
		"labels:\n  role: web\n" +
		"script:\n  enabled: true\n  max_timeout: 120\n" +
		"server:\n  port: 4444\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadFromDisk(path)
	if err != nil {
		t.Fatalf("loadFromDisk: %v", err)
	}
	if _, err := writeToDisk(path, cfg); err != nil {
		t.Fatalf("writeToDisk: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(after, &got); err != nil {
		t.Fatalf("result is not valid yaml: %v", err)
	}

	// The runner-only keys must survive a round trip through the API.
	if got["brain_api_url"] != "http://localhost:3402" {
		t.Errorf("brain_api_url lost: %v", got["brain_api_url"])
	}
	if got["max_parallel"] != 2 {
		t.Errorf("max_parallel lost: %v", got["max_parallel"])
	}
	sc, _ := got["script"].(map[string]any)
	if sc == nil || sc["enabled"] != true {
		t.Errorf("script.enabled lost: %v", got["script"])
	}
	if sc != nil && sc["max_timeout"] != 120 {
		t.Errorf("script.max_timeout lost: %v", sc["max_timeout"])
	}
	lb, _ := got["labels"].(map[string]any)
	if lb == nil || lb["role"] != "web" {
		t.Errorf("labels lost: %v", got["labels"])
	}

	// And the modeled value the file DID set must still round-trip.
	srv, _ := got["server"].(map[string]any)
	if srv == nil || srv["port"] != 4444 {
		t.Errorf("server.port = %v, want the on-disk 4444", srv["port"])
	}
}

// A modeled field the user cleared must actually clear, not be resurrected by
// the preserving merge. Only one field in UnifiedConfig carries omitempty, so
// every other key is always present in the rendered map and always wins.
func TestWriteToDisk_ModeledValuesStillOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  host: oldhost\n  port: 1111\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadFromDisk(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.Host = "newhost"
	if _, err := writeToDisk(path, cfg); err != nil {
		t.Fatal(err)
	}
	reread, err := loadFromDisk(path)
	if err != nil {
		t.Fatal(err)
	}
	if reread.Server.Host != "newhost" {
		t.Errorf("host = %q, want the edit to win", reread.Server.Host)
	}
	if reread.Server.Port != 1111 {
		t.Errorf("port = %d, want the untouched 1111", reread.Server.Port)
	}
}
