// Tests for the config read/write handlers.

package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/config"
	"gopkg.in/yaml.v3"
)

// minimalValidConfig returns a UnifiedConfig that passes Validate.
func minimalValidConfig() config.UnifiedConfig {
	return config.UnifiedConfig{
		Server: config.ServerConfig{
			Port:       3333,
			Host:       "localhost",
			BrainDir:   "/tmp/brain-test",
			LogLevel:   "info",
			CORSOrigin: "*",
		},
		Runner: config.RunnerConfig{
			BrainAPIURL:            "http://localhost:3333",
			MaxParallel:            5,
			PollInterval:           5,
			TaskPollInterval:       5,
			APITimeout:             5000,
			MaxTotalProcesses:      10,
			MemoryThresholdPercent: 10,
		},
	}
}

// writeConfigFile writes cfg to path in YAML form for the handler to
// pick up on GET.
func writeConfigFile(t *testing.T, path string, cfg config.UnifiedConfig) {
	t.Helper()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// setupConfigServer mounts the ConfigHandler on a chi router bound
// to a temp config file and returns the server + config path.
func setupConfigServer(t *testing.T, cfg config.UnifiedConfig) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeConfigFile(t, path, cfg)

	// Simple hot-reloader for tests: pretends nothing hot-reloads,
	// reports the fields that actually changed as requires-restart.
	reloader := func(prev, next *config.UnifiedConfig) api.HotReloadResult {
		var restart []string
		if prev.Server.Port != next.Server.Port {
			restart = append(restart, "server.port")
		}
		return api.HotReloadResult{RequiresRestart: restart}
	}
	h := api.NewConfigHandler(path, reloader)

	r := chi.NewRouter()
	r.Get("/api/v1/config", h.HandleGet)
	r.Get("/api/v1/config/schema", h.HandleGetSchema)
	r.Put("/api/v1/config", h.HandlePut)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, path
}

func TestConfigHandler_GetReturnsYamlAsJson(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Runner.APIToken = "super-secret"
	cfg.Server.JWTSecret = "hmac-secret"

	srv, _ := setupConfigServer(t, cfg)
	resp, err := http.Get(srv.URL + "/api/v1/config")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d body=%s", resp.StatusCode, body)
	}
	var payload struct {
		Config map[string]any `json:"config"`
		Path   string         `json:"path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Config should use snake_case keys.
	server, _ := payload.Config["server"].(map[string]any)
	runner, _ := payload.Config["runner"].(map[string]any)
	if server == nil || runner == nil {
		t.Fatalf("expected snake_case server/runner sections: %v", payload.Config)
	}
	// Secrets redacted.
	if runner["api_token"] == "super-secret" {
		t.Errorf("runner.api_token was NOT redacted: %v", runner["api_token"])
	}
	if server["jwt_secret"] == "hmac-secret" {
		t.Errorf("server.jwt_secret was NOT redacted: %v", server["jwt_secret"])
	}
	// Non-secrets survive.
	// yaml unmarshal-to-map produces int for numeric fields.
	if port, _ := server["port"].(int); port != 3333 {
		if fport, _ := server["port"].(float64); int(fport) != 3333 {
			t.Errorf("port survived redaction: got %v", server["port"])
		}
	}
	if runner["brain_api_url"] != "http://localhost:3333" {
		t.Errorf("brain_api_url survived: got %q", runner["brain_api_url"])
	}
	if payload.Path == "" {
		t.Errorf("path should be set")
	}
}

func TestConfigHandler_GetMissingFileReturns500(t *testing.T) {
	// Point handler at a non-existent path.
	h := api.NewConfigHandler("/nonexistent/config.yaml", nil)
	r := chi.NewRouter()
	r.Get("/api/v1/config", h.HandleGet)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/config")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 500 {
		t.Errorf("expected 500 for missing file, got %d", resp.StatusCode)
	}
}

func TestConfigHandler_PutValidatesBeforeWrite(t *testing.T) {
	cfg := minimalValidConfig()
	srv, path := setupConfigServer(t, cfg)

	// Send an invalid config (port 0) in the snake_case map form.
	badMap := configToMap(t, cfg)
	server, _ := badMap["server"].(map[string]any)
	server["port"] = 0
	body, _ := json.Marshal(map[string]any{"config": badMap})
	req, _ := http.NewRequest("PUT", srv.URL+"/api/v1/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 400 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400 for invalid config, got %d body=%s", resp.StatusCode, respBody)
	}
	// File on disk should be unchanged.
	on, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(on), "port: 3333") {
		t.Errorf("file was mutated on validation failure: %s", on)
	}
}

func TestConfigHandler_PutAtomicallyReplacesFile(t *testing.T) {
	cfg := minimalValidConfig()
	srv, path := setupConfigServer(t, cfg)

	nextMap := configToMap(t, cfg)
	server, _ := nextMap["server"].(map[string]any)
	server["port"] = 4444
	server["log_level"] = "debug"
	body, _ := json.Marshal(map[string]any{"config": nextMap})
	req, _ := http.NewRequest("PUT", srv.URL+"/api/v1/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, b)
	}
	var out struct {
		HotReloaded     []string `json:"hot_reloaded"`
		RequiresRestart []string `json:"requires_restart"`
		BackupPath      string   `json:"backup_path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !contains(out.RequiresRestart, "server.port") {
		t.Errorf("expected server.port in requires_restart, got %v", out.RequiresRestart)
	}
	on, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(on), "port: 4444") {
		t.Errorf("port not persisted: %s", on)
	}
	if out.BackupPath == "" {
		t.Errorf("no backup path returned")
	}
	if _, err := os.Stat(out.BackupPath); err != nil {
		t.Errorf("backup missing: %v", err)
	}
}

// configToMap round-trips a UnifiedConfig through yaml to get the
// snake_case map[string]any shape the API accepts on PUT.
func configToMap(t *testing.T, cfg config.UnifiedConfig) map[string]any {
	t.Helper()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]any
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func TestConfigHandler_PutSentinelPreservesSecret(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Runner.APIToken = "keep-me-safe"
	srv, path := setupConfigServer(t, cfg)

	// GET first — server responds with snake_case map, secret redacted.
	resp, err := http.Get(srv.URL + "/api/v1/config")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var payload struct {
		Config map[string]any `json:"config"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	_ = resp.Body.Close()
	// Sanity: sentinel should be present under runner.api_token.
	runner, _ := payload.Config["runner"].(map[string]any)
	if runner == nil {
		t.Fatalf("no runner section in GET response: %v", payload.Config)
	}
	if runner["api_token"] != "__brain_unchanged__" {
		t.Errorf("expected redacted sentinel, got %v", runner["api_token"])
	}

	// PUT the redacted-form payload back — sentinel should preserve
	// the original token on disk.
	body, _ := json.Marshal(map[string]any{"config": payload.Config})
	req, _ := http.NewRequest("PUT", srv.URL+"/api/v1/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	putResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	putBody, _ := io.ReadAll(putResp.Body)
	_ = putResp.Body.Close()
	if putResp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d body=%s", putResp.StatusCode, putBody)
	}
	// On-disk should still have the original secret, NOT the sentinel.
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "keep-me-safe") {
		t.Errorf("secret was overwritten by sentinel: %s", data)
	}
	if strings.Contains(string(data), "__brain_unchanged__") {
		t.Errorf("sentinel leaked into on-disk file: %s", data)
	}
}

func TestConfigHandler_GetSchemaEnumeratesFields(t *testing.T) {
	cfg := minimalValidConfig()
	srv, _ := setupConfigServer(t, cfg)

	resp, err := http.Get(srv.URL + "/api/v1/config/schema")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var out struct {
		Fields []api.ConfigField `json:"fields"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Fields) < 30 {
		t.Errorf("expected >= 30 fields, got %d", len(out.Fields))
	}
	// Spot-check a few known paths and their metadata.
	byPath := make(map[string]api.ConfigField)
	for _, f := range out.Fields {
		byPath[f.Path] = f
	}
	if f, ok := byPath["server.port"]; !ok {
		t.Error("server.port missing")
	} else {
		if f.Kind != "int" {
			t.Errorf("server.port kind = %q, want int", f.Kind)
		}
		if !f.RequiresRestart {
			t.Error("server.port should require restart")
		}
	}
	if f, ok := byPath["runner.api_token"]; !ok {
		t.Error("runner.api_token missing")
	} else if !f.Secret {
		t.Error("runner.api_token should be marked secret")
	}
	if f, ok := byPath["server.log_level"]; !ok {
		t.Error("server.log_level missing")
	} else if len(f.Enum) == 0 {
		t.Error("server.log_level should have enum values")
	}
}

func contains(list []string, needle string) bool {
	for _, s := range list {
		if s == needle {
			return true
		}
	}
	return false
}
