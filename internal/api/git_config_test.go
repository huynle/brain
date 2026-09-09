package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGitConfigSecretRoundTrip(t *testing.T) {
	cfg := minimalValidConfig()
	if err := yaml.Unmarshal([]byte("runner:\n  git_token: dummy-git-secret\n  git_token_env: NAMED_TOKEN\n  git_allowed_hosts: []\n"), &cfg); err != nil {
		t.Fatal(err)
	}
	srv, path := setupConfigServer(t, cfg)
	resp, err := http.Get(srv.URL + "/api/v1/config")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "dummy-git-secret") {
		t.Fatal("config GET leaked git secret")
	}
	var payload struct {
		Config map[string]interface{} `json:"config"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	r := payload.Config["runner"].(map[string]interface{})
	if r["git_token"] == nil || r["git_token"] == "" {
		t.Fatal("git_token not mapped/redacted")
	}
	body, _ := json.Marshal(map[string]interface{}{"config": payload.Config})
	req, _ := http.NewRequest("PUT", srv.URL+"/api/v1/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	updated, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer updated.Body.Close()
	if updated.StatusCode != 200 {
		t.Fatalf("PUT status %d", updated.StatusCode)
	}
	on, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(on), "dummy-git-secret") || !strings.Contains(string(on), "git_allowed_hosts: []") {
		t.Fatalf("secret or explicit deny did not survive GET/PUT")
	}
}
