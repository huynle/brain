package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/runner"
	"gopkg.in/yaml.v3"
)

func TestGitPolicyUnifiedRoundTripAndMigration(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		for _, allowed := range []string{"", "git_allowed_hosts: []\n", "git_allowed_hosts: [forge.invalid]\n"} {
			t.Run(allowed+map[bool]string{true: "legacy", false: "unified"}[legacy], func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("XDG_CONFIG_HOME", home)
				t.Setenv("RUNNER_GIT_TOKEN", "")
				t.Setenv("RUNNER_GIT_TOKEN_ENV", "")
				t.Setenv("PHASE2_FORGE_TOKEN", "forge-dummy")
				t.Setenv("RUNNER_ALLOW_UNAUTHENTICATED_HTTPS", "")
				body := "git_token: legacy-dummy\ngit_token_env: LEGACY_ALIAS\ngit_host_token_env:\n  forge.invalid: PHASE2_FORGE_TOKEN\ngit_ssl_ca_info: /operator/ca.pem\nrepo_cache_dir: /operator/cache\nallow_unauthenticated_https: true\n" + allowed
				dir := filepath.Join(home, "brain-runner")
				if !legacy {
					dir = filepath.Join(home, "brain")
					var fields map[string]interface{}
					if err := yaml.Unmarshal([]byte(body), &fields); err != nil {
						t.Fatal(err)
					}
					data, err := yaml.Marshal(map[string]interface{}{"runner": fields})
					if err != nil {
						t.Fatal(err)
					}
					body = string(data)
				}
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				cfg, err := config.LoadConfig()
				if err != nil {
					t.Fatal(err)
				}
				data, err := yaml.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				roundtrip := filepath.Join(home, "roundtrip.yaml")
				if err := os.WriteFile(roundtrip, data, 0600); err != nil {
					t.Fatal(err)
				}
				got, err := runner.LoadConfigFrom(roundtrip)
				if err != nil {
					t.Fatal(err)
				}
				if got.GitToken != "legacy-dummy" || got.GitTokenEnv != "LEGACY_ALIAS" || got.GitSSLCAInfo != "/operator/ca.pem" || got.RepoCacheDir != "/operator/cache" || !got.AllowUnauthenticatedHTTPS {
					t.Errorf("git transport configuration lost in unified mapping")
				}
				want := []string{"forge.invalid", "github.com"}
				if allowed == "git_allowed_hosts: []\n" {
					want = []string{}
				}
				if allowed == "git_allowed_hosts: [forge.invalid]\n" {
					want = []string{"forge.invalid"}
				}
				hosts, err := got.CredentialedGitHosts()
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(hosts, want) {
					t.Fatalf("mapped hosts=%v, want %v", hosts, want)
				}
				if allowed == "" && got.GitAllowedHosts != nil {
					t.Fatal("omitted allowlist became explicit deny")
				}
				if allowed != "" && got.GitAllowedHosts == nil {
					t.Fatal("explicit allowlist lost")
				}
			})
		}
	}
}
