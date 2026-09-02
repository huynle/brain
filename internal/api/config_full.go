// Package api — configuration read/write endpoints.
//
// GET  /api/v1/config   returns the full ~/.config/brain/config.yaml
//                        parsed into JSON, with secrets redacted.
// PUT  /api/v1/config   accepts the same JSON shape, validates it,
//                        atomically writes the file (with backup),
//                        and dispatches hot-reload for the subset of
//                        fields that can be applied without a restart.
// GET  /api/v1/config/schema   returns a JSON description of every
//                        field (path, type, required, redacted,
//                        requires_restart) so the frontend can render
//                        an accurate form.
//
// Secret redaction: fields whose YAML tag is exactly `api_token`,
// `jwt_secret`, or `oauth_pin` are replaced with a fixed sentinel
// string on GET. On PUT, the same sentinel is interpreted as
// "leave the persisted value unchanged"; any other value is written
// through. Environment-variable-based tokens (`api_token_env`,
// `api_key_env`) are NOT redacted — they're pointers to env vars,
// not secrets themselves.

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/huynle/brain-api/internal/config"
	"gopkg.in/yaml.v3"
)

// sentinelUnchanged is written into secret fields on GET responses
// and interpreted as "keep the persisted value" on PUT.
const sentinelUnchanged = "__brain_unchanged__"

// HotReloadResult reports which fields the server was able to apply
// without a restart. Everything not listed here requires the user to
// bounce the binary. Exported so external callers can implement the
// hot-reload dispatcher.
type HotReloadResult struct {
	HotReloaded     []string `json:"hot_reloaded"`
	RequiresRestart []string `json:"requires_restart"`
}

// ConfigHandler wires read/write of the unified config file.
type ConfigHandler struct {
	// path is the on-disk config location (~/.config/brain/config.yaml).
	path string
	// hotReloader is invoked with the newly-loaded config after a
	// successful write. It's expected to compute the diff against the
	// live server state and mutate whatever subsystems can be swapped
	// at runtime (log level, task defaults, runner include/exclude,
	// etc.), returning the field list for the response.
	hotReloader func(prev, next *config.UnifiedConfig) HotReloadResult
}

// NewConfigHandler resolves the config path via the same rules the
// server uses at startup. hotReloader may be nil in tests.
func NewConfigHandler(
	path string,
	hotReloader func(prev, next *config.UnifiedConfig) HotReloadResult,
) *ConfigHandler {
	if path == "" {
		path = config.UnifiedConfigPath()
	}
	return &ConfigHandler{path: path, hotReloader: hotReloader}
}

// HandleGet returns the current config with secrets redacted.
//
// GET /api/v1/config → 200 { config: {...}, path: "..." }
//
// The config object uses snake_case keys (matching yaml tags) because
// that's the source of truth on disk. We marshal the struct to YAML
// then unmarshal into a generic map so the JSON response uses the
// yaml key names verbatim.
func (h *ConfigHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	cfg, err := loadFromDisk(h.path)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "config_read_failed", err.Error())
		return
	}
	redactSecrets(cfg)
	generic, err := toGeneric(cfg)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "config_marshal_failed", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"config": generic,
		"path":   h.path,
	})
}

// HandlePut atomically replaces the config file after validation.
//
// PUT /api/v1/config
//
//	body: { config: <snake_case keyed object> }
//	200 { hot_reloaded: [...], requires_restart: [...], backup_path: "..." }
//	400 { error: "validation", message: ... }
//	500 { error: ..., message: ... }
func (h *ConfigHandler) HandlePut(w http.ResponseWriter, r *http.Request) {
	// Accept the config as a raw map so the client can send whatever
	// keys the schema advertises. We route it through YAML to hydrate
	// into the strongly-typed UnifiedConfig — that gives us
	// yaml-tag-aware field mapping and rejects unknown keys.
	var body struct {
		Config map[string]any `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}

	next, err := fromGeneric(body.Config)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}

	// Load the on-disk version so we can (1) refill redacted secrets
	// and (2) diff for hot-reload.
	prev, err := loadFromDisk(h.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		WriteError(w, http.StatusInternalServerError, "config_read_failed", err.Error())
		return
	}
	if prev == nil {
		prev = &config.UnifiedConfig{}
	}

	restoreRedactedSecrets(next, prev)

	if err := next.Validate(); err != nil {
		WriteError(w, http.StatusBadRequest, "validation", err.Error())
		return
	}

	backup, err := writeToDisk(h.path, next)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "config_write_failed", err.Error())
		return
	}

	var result HotReloadResult
	if h.hotReloader != nil {
		result = h.hotReloader(prev, next)
	} else {
		// Without a reloader wired in tests, treat everything as
		// requires-restart.
		result = HotReloadResult{RequiresRestart: allFieldPaths()}
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"hot_reloaded":     result.HotReloaded,
		"requires_restart": result.RequiresRestart,
		"backup_path":      backup,
	})
}

// HandleGetSchema returns metadata about every field so the frontend
// can render a type-aware form.
//
// GET /api/v1/config/schema → 200 { fields: [...] }
func (h *ConfigHandler) HandleGetSchema(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]any{
		"fields": ConfigSchema(),
	})
}

// ─── helpers ────────────────────────────────────────────────────────

// toGeneric roundtrips a UnifiedConfig through YAML to produce a
// generic map[string]any keyed by the yaml tag names (snake_case).
// This is what we send to the frontend so keys match the schema
// paths and object-path helpers.
func toGeneric(cfg *config.UnifiedConfig) (map[string]any, error) {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// fromGeneric is the reverse: take the snake_case map the frontend
// sends and hydrate a strongly-typed UnifiedConfig. yaml.Unmarshal
// applies the yaml tags on UnifiedConfig fields, so unknown keys
// silently drop (no strict decode mode).
func fromGeneric(m map[string]any) (*config.UnifiedConfig, error) {
	data, err := yaml.Marshal(m)
	if err != nil {
		return nil, err
	}
	var cfg config.UnifiedConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("yaml decode: %w", err)
	}
	return &cfg, nil
}

// loadFromDisk reads and parses the config YAML. Returns a pointer so
// callers can pass it into hot-reload dispatch without extra copies.
func loadFromDisk(path string) (*config.UnifiedConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Seed from defaults, exactly as config.LoadConfig does at startup, then
	// let the file overlay it. Decoding into a ZERO struct reported whatever
	// the Go zero value happened to be for every key the file omits — which
	// is not what the running server is using.
	//
	// server.feature_checkout.enabled defaults to TRUE, so on any install
	// whose config.yaml never mentioned it, the Settings toggle rendered OFF
	// while the feature was ON. Worse, the field has no omitempty and
	// HandlePut writes the whole struct back, so the first save of ANY
	// unrelated field materialized `enabled: false` on disk and genuinely
	// disabled feature checkout at the next restart.
	cfg := config.DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("yaml parse: %w", err)
	}
	return &cfg, nil
}

// writeToDisk marshals cfg and atomically replaces the file, keeping
// a timestamped backup.
func writeToDisk(path string, cfg *config.UnifiedConfig) (string, error) {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}
	// Backup first.
	backupPath, err := config.BackupConfigFile(path)
	if err != nil {
		return "", fmt.Errorf("backup: %w", err)
	}
	// Atomic write: write to sibling temp, fsync, rename.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return backupPath, err
	}
	tmp, err := os.CreateTemp(dir, ".brain-config-*.tmp")
	if err != nil {
		return backupPath, err
	}
	tmpPath := tmp.Name()
	// Ensure temp is cleaned up if we fail before rename.
	success := false
	defer func() {
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return backupPath, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return backupPath, err
	}
	if err := tmp.Close(); err != nil {
		return backupPath, err
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return backupPath, err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return backupPath, err
	}
	success = true
	return backupPath, nil
}

// redactSecrets replaces sensitive field values with sentinelUnchanged
// so they're not exposed on GET responses.
//
// Redacted fields (matched by struct path):
//   - server.oauth_pin
//   - server.jwt_secret
//   - runner.api_token
//
// api_token_env / api_key_env are NOT redacted; they're env-var names,
// not secrets.
func redactSecrets(cfg *config.UnifiedConfig) {
	if cfg.Server.OAuthPIN != "" {
		cfg.Server.OAuthPIN = sentinelUnchanged
	}
	if cfg.Server.JWTSecret != "" {
		cfg.Server.JWTSecret = sentinelUnchanged
	}
	if cfg.Runner.APIToken != "" {
		cfg.Runner.APIToken = sentinelUnchanged
	}
}

// restoreRedactedSecrets copies the persisted secret from prev into next
// wherever next carries the sentinel. This lets a client GET (redacted)
// → PUT (unchanged) without exposing tokens on the wire.
func restoreRedactedSecrets(next, prev *config.UnifiedConfig) {
	if next.Server.OAuthPIN == sentinelUnchanged {
		next.Server.OAuthPIN = prev.Server.OAuthPIN
	}
	if next.Server.JWTSecret == sentinelUnchanged {
		next.Server.JWTSecret = prev.Server.JWTSecret
	}
	if next.Runner.APIToken == sentinelUnchanged {
		next.Runner.APIToken = prev.Runner.APIToken
	}
}
