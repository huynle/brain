package runner

import (
	"context"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/huynle/brain-api/internal/gitremote"
	"gopkg.in/yaml.v3"
)

func remoteConfig(t *testing.T, text string) RunnerConfig {
	t.Helper()
	cfg := testExecutorConfig()
	if err := yaml.Unmarshal([]byte(text), &cfg); err != nil {
		t.Fatal(err)
	}
	cfg.RepoCacheDir = t.TempDir()
	return cfg
}

// Skip only Git's known option pairs, preserving the full argv for assertions.
func gitOperationArgs(args []string, operation string) []string {
	for i := 0; i < len(args); i++ {
		if args[i] == "-c" || args[i] == "-C" {
			i++
			continue
		}
		if args[i] == operation {
			return args[i:]
		}
	}
	return nil
}

func assertGitTransportOptions(t *testing.T, args []string) {
	t.Helper()
	for _, want := range []string{"http.followRedirects=false", "http.sslVerify=true", "credential.helper=", "protocol.allow=never"} {
		if !containsArg(args, want) {
			t.Errorf("missing %s in %v", want, args)
		}
	}
}

func fixtureGit(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture git %v: %v %s", args, err, out)
	}
}

func TestGitTransportTLSIsolation(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	fixtureGit(t, "init", "-b", "main", src)
	fixtureGit(t, "-C", src, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "fixture")
	fixtureGit(t, "clone", "--bare", src, filepath.Join(root, "repo.git"))
	fixtureGit(t, "-C", filepath.Join(root, "repo.git"), "update-server-info")
	var destinationCalls, approvedCalls atomic.Int32
	destination := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationCalls.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("redirect/rewrite destination received Authorization")
		}
		http.Error(w, "no", http.StatusForbidden)
	}))
	defer destination.Close()
	var redirect atomic.Bool
	approved := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		approvedCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer scoped-secret" {
			t.Errorf("approved host missing scoped credential: %q", r.Header.Get("Authorization"))
		}
		if redirect.Load() {
			http.Redirect(w, r, destination.URL+r.URL.Path, http.StatusFound)
			return
		}
		http.FileServer(http.Dir(root)).ServeHTTP(w, r)
	}))
	defer approved.Close()
	ca := filepath.Join(root, "ca.pem")
	certs := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: approved.Certificate().Raw}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: destination.Certificate().Raw})...)
	if err := os.WriteFile(ca, certs, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCAL_GIT_TOKEN", "scoped-secret")
	authority := strings.TrimPrefix(approved.URL, "https://")
	cfg := remoteConfig(t, fmt.Sprintf("git_host_token_env:\n  %s: LOCAL_GIT_TOKEN\ngit_ssl_ca_info: %s\n", authority, ca))
	cfg.RequireHTTPS = true
	remote := approved.URL + "/repo.git"
	// Inherited global, system, command-line, and environment settings all try
	// to redirect an approved URL and/or disable TLS validation.
	attackConfig := filepath.Join(root, "attack.config")
	if err := os.WriteFile(attackConfig, []byte(fmt.Sprintf("[url %q]\n insteadOf = %s\n[http]\n sslVerify = false\n", destination.URL+"/", approved.URL+"/")), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", attackConfig)
	t.Setenv("GIT_CONFIG_SYSTEM", attackConfig)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url."+destination.URL+"/.insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", approved.URL+"/")
	t.Setenv("GIT_CONFIG_PARAMETERS", "'http.followRedirects=true'")
	t.Setenv("GIT_SSL_NO_VERIFY", "1")
	var commands []*exec.Cmd
	factory := func(name string, args ...string) *exec.Cmd {
		cmd := exec.Command(name, args...)
		commands = append(commands, cmd)
		return cmd
	}
	// An inherited TLS bypass must not make an untrusted certificate usable.
	untrusted := cfg
	untrusted.GitSSLCAInfo = ""
	if _, err := ensureCachedRemoteRepo(remote, untrusted, factory); err == nil {
		t.Fatal("inherited TLS bypass accepted untrusted certificate")
	}
	if approvedCalls.Load() != 0 {
		t.Fatal("token sent before TLS trust verification")
	}
	// A failed clone may have left an empty destination; use a fresh cache.
	cfg.RepoCacheDir = t.TempDir()
	cached, err := ensureCachedRemoteRepo(remote, cfg, factory)
	if err != nil {
		t.Fatalf("approved TLS clone: %v", err)
	}
	if approvedCalls.Load() == 0 {
		t.Fatal("no approved network request")
	}
	fixtureGit(t, "-C", cached, "config", "remote.origin.url", destination.URL+"/repo.git")
	fixtureGit(t, "-C", cached, "config", "url."+destination.URL+"/.insteadOf", approved.URL+"/")
	fixtureGit(t, "-C", cached, "config", "http.followRedirects", "true")
	fixtureGit(t, "-C", src, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "second")
	fixtureGit(t, "-C", filepath.Join(root, "repo.git"), "fetch", src, "+refs/heads/main:refs/heads/main")
	fixtureGit(t, "-C", filepath.Join(root, "repo.git"), "update-server-info")
	if _, err := ensureCachedRemoteRepo(remote, cfg, factory); err != nil {
		t.Fatalf("cached fetch with poisoned origin/config: %v", err)
	}
	gotRef, err := exec.Command("git", "-C", cached, "rev-parse", "refs/remotes/origin/main").Output()
	if err != nil {
		t.Fatal(err)
	}
	wantRef, err := exec.Command("git", "-C", src, "rev-parse", "HEAD").Output()
	if err != nil || string(gotRef) != string(wantRef) {
		t.Fatalf("cached ref not updated: got %s want %s err=%v", gotRef, wantRef, err)
	}
	if destinationCalls.Load() != 0 {
		t.Fatalf("rewrite destination contacted %d times", destinationCalls.Load())
	}
	foundExplicitFetch := false
	for _, cmd := range commands {
		joined := strings.Join(cmd.Args, " ")
		if strings.Contains(joined, "Bearer scoped-secret") {
			if cmd.Dir == cached {
				t.Error("credentialed Git ran in untrusted cache")
			}
			if !strings.Contains(joined, "http.followRedirects=false") {
				t.Error("redirects not disabled in argv")
			}
			if containsArg(cmd.Args, "fetch") && containsArg(cmd.Args, remote) && containsArg(cmd.Args, "+refs/heads/*:refs/heads/*") {
				foundExplicitFetch = true
			}
			for _, env := range cmd.Env {
				if strings.HasPrefix(env, "GIT_CONFIG_KEY_") || strings.HasPrefix(env, "GIT_SSL_NO_VERIFY=") || strings.HasPrefix(env, "GIT_CONFIG_PARAMETERS=") {
					t.Errorf("unsafe inherited env: %s", env)
				}
			}
		}
	}
	if !foundExplicitFetch {
		t.Error("cached network fetch must use explicit URL and refspec, not origin")
	}
	redirect.Store(true)
	for _, cachedAttempt := range []bool{true, false} {
		if !cachedAttempt {
			cfg.RepoCacheDir = t.TempDir()
		}
		if _, err := ensureCachedRemoteRepo(remote, cfg, factory); err == nil {
			t.Error("redirect should fail closed")
		}
	}
	if destinationCalls.Load() != 0 {
		t.Errorf("redirect destination contacted %d times", destinationCalls.Load())
	}
}

// Dumb HTTP Git can discover another object store through http-alternates,
// without an HTTP redirect. Both authorities are trusted here: refusal must
// come from transport policy, not a certificate error or an unusable fixture.
func TestGitTransportHTTPAlternatesIsolation(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	fixtureGit(t, "init", "-b", "main", src)
	fixtureGit(t, "-C", src, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "fixture")
	fixtureGit(t, "clone", "--bare", src, filepath.Join(root, "repo.git"))
	fixtureGit(t, "-C", filepath.Join(root, "repo.git"), "update-server-info")
	files := http.FileServer(http.Dir(root))
	var attackerCalls, attackerHeaders, alternatesCalls atomic.Int32
	attacker := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attackerCalls.Add(1)
		if r.Header.Get("Authorization") != "" {
			attackerHeaders.Add(1)
		}
		files.ServeHTTP(w, r)
	}))
	defer attacker.Close()
	approved := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer alternates-secret" {
			t.Errorf("approved host missing scoped credential: %q", r.Header.Get("Authorization"))
		}
		if r.URL.Path == "/repo.git/objects/info/http-alternates" {
			alternatesCalls.Add(1)
			fmt.Fprintln(w, attacker.URL+"/repo.git/objects")
			return
		}
		// Force object discovery through the advertised alternate; refs and
		// HEAD remain valid and the second authority serves the real objects.
		if suffix := strings.TrimPrefix(r.URL.Path, "/repo.git/objects/"); suffix != r.URL.Path && len(suffix) == 41 && suffix[2] == '/' {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	}))
	defer approved.Close()
	ca := filepath.Join(root, "ca.pem")
	certs := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: approved.Certificate().Raw}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: attacker.Certificate().Raw})...)
	if err := os.WriteFile(ca, certs, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ALTERNATES_GIT_TOKEN", "alternates-secret")
	authority := strings.TrimPrefix(approved.URL, "https://")
	for _, control := range []bool{false, true} {
		name := "production refuses"
		if control {
			name = "redirect-enabled fixture control"
		}
		t.Run(name, func(t *testing.T) {
			attackerCalls.Store(0)
			attackerHeaders.Store(0)
			alternatesCalls.Store(0)
			cfg := remoteConfig(t, fmt.Sprintf("git_host_token_env:\n  %s: ALTERNATES_GIT_TOKEN\ngit_ssl_ca_info: %s\n", authority, ca))
			_, err := ensureCachedRemoteRepo(approved.URL+"/repo.git", cfg, func(name string, args ...string) *exec.Cmd {
				if control {
					// Test-only mutation proves followRedirects=false is the gate.
					for i, arg := range args {
						if arg == "http.followRedirects=false" {
							args[i] = "http.followRedirects=true"
						}
					}
				}
				return exec.Command(name, args...)
			})
			if alternatesCalls.Load() == 0 {
				t.Fatal("fixture never advertised http-alternates")
			}
			if control {
				if err != nil || attackerCalls.Load() == 0 {
					t.Fatalf("control must retrieve trusted alternate objects: requests=%d err=%v", attackerCalls.Load(), err)
				}
			} else if err == nil || attackerCalls.Load() != 0 || attackerHeaders.Load() != 0 {
				t.Fatalf("production must refuse alternate without contacting it: requests=%d authorization=%d err=%v", attackerCalls.Load(), attackerHeaders.Load(), err)
			}
		})
	}
}

func TestGitHostPolicyRefusesBeforeCommand(t *testing.T) {
	for _, remote := range []string{"https://evil.invalid/repo", "https://github.com.evil.invalid/repo", "https://github.com:444/repo", "https://github.com./repo"} {
		t.Run(remote, func(t *testing.T) {
			cfg := remoteConfig(t, "git_token: legacy-secret\n")
			var argv []string
			_, err := ensureCachedRemoteRepo(remote, cfg, func(name string, args ...string) *exec.Cmd {
				argv = append(argv, args...)
				return exec.Command("false")
			})
			if len(argv) != 0 {
				t.Errorf("disallowed remote reached Git argv: %v", argv)
			}
			if err == nil || !strings.Contains(err.Error(), "host") {
				t.Errorf("want host refusal, got %v", err)
			}
		})
	}
}

func TestGitHostPolicyLocalAndOverride(t *testing.T) {
	cfg := remoteConfig(t, "git_token: legacy-secret\n")
	task := testResolvedTask("host-policy")
	task.GitRemote = "https://evil.invalid/repo"
	task.TargetWorkdir = t.TempDir()
	task.ExecutionMode = "current_branch"
	factory := func(string, ...string) *exec.Cmd { t.Error("unexpected command"); return exec.Command("false") }
	if _, err := CommonResolveWorkdir(task, cfg, factory); err == nil || !strings.Contains(err.Error(), "host") {
		t.Errorf("local workdir bypass: %v", err)
	}
	for _, executor := range []TaskExecutor{NewExecutor(cfg), NewPiExecutor(cfg)} {
		_, err := executor.Spawn(context.Background(), task, "test", SpawnOptions{Workdir: task.TargetWorkdir, Mode: "invalid"})
		if err == nil || !strings.Contains(err.Error(), "host") {
			t.Errorf("%T override bypass: %v", executor, err)
		}
	}
}

func TestGitHostBoundArgv(t *testing.T) {
	t.Setenv("HOST_A_TOKEN", "host-a-secret")
	t.Setenv("HOST_B_TOKEN", "host-b-secret")
	cfg := remoteConfig(t, "git_token: legacy-secret\ngit_host_token_env:\n  a.invalid: HOST_A_TOKEN\n  b.invalid: HOST_B_TOKEN\ngit_allowed_hosts: [a.invalid]\n")
	var argv []string
	_, _ = ensureCachedRemoteRepo("https://a.invalid/repo.git", cfg, func(_ string, args ...string) *exec.Cmd {
		argv = append(argv, args...)
		return exec.Command("false")
	})
	joined := strings.Join(argv, " ")
	for _, want := range []string{"http.https://a.invalid/.extraheader=Authorization: Bearer host-a-secret", "http.followRedirects=false", "http.sslVerify=true"} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q: %v", want, argv)
		}
	}
	if strings.Contains(joined, "legacy-secret") || strings.Contains(joined, "host-b-secret") {
		t.Errorf("wrong credential in argv: %v", argv)
	}
	for _, remote := range []string{"https://b.invalid/repo", "https://github.com/repo", "http://a.invalid/repo"} {
		calls := 0
		_, err := ensureCachedRemoteRepo(remote, cfg, func(_ string, _ ...string) *exec.Cmd { calls++; return exec.Command("false") })
		if err == nil || calls != 0 {
			t.Errorf("not refused before Git: %s calls=%d err=%v", remote, calls, err)
		}
	}
}

func TestGitRemoteCanonicalAndCacheIdentity(t *testing.T) {
	for _, remote := range []string{"https://github.com:/repo", "https://github.com:0/repo", "https://github.com:65536/repo", "https://github.com/repo?x=1", "https://github.com/repo#fragment", "https://github.com/repo%0a", "https://github.com\\evil/repo", "https://github.com./repo"} {
		if _, err := gitremote.Parse(remote); err == nil {
			t.Errorf("accepted ambiguous URL %q", remote)
		}
	}
	a, _ := url.Parse("https://example.com/a-b/c.git")
	b, _ := url.Parse("https://example.com/a/b-c.git")
	if cacheDirNameForRemote(a) == cacheDirNameForRemote(b) {
		t.Error("cache identities collide")
	}
}

func TestGitRuntimeConfigHostMap(t *testing.T) {
	t.Setenv("HOST_A_TOKEN", "host-a-secret")
	for _, nested := range []bool{false, true} {
		text := "git_host_token_env:\n  a.invalid: HOST_A_TOKEN\ngit_allowed_hosts: [a.invalid]\n"
		if nested {
			text = "runner:\n  " + strings.ReplaceAll(strings.TrimSuffix(text, "\n"), "\n", "\n  ") + "\n"
		}
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfigFrom(path)
		if err != nil {
			t.Fatal(err)
		}
		cfg.RepoCacheDir = t.TempDir()
		cfg.Control.AllowedWorkdirRoots = []string{cfg.RepoCacheDir}
		var argv []string
		_, _ = ensureCachedRemoteRepo("https://a.invalid/repo", cfg, func(_ string, args ...string) *exec.Cmd { argv = args; return exec.Command("false") })
		if !strings.Contains(strings.Join(argv, " "), "Bearer host-a-secret") {
			t.Errorf("nested=%v host config lost: %v", nested, argv)
		}
	}
}

func TestGitRuntimeRejectsMalformedNestedPolicy(t *testing.T) {
	for _, text := range []string{"runner:\n  git_allowed_hosts: github.com\n", "runner:\n  git_host_token_env: [github.com]\n"} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfigFrom(path); err == nil {
			t.Errorf("silently discarded malformed runner policy: %s", text)
		}
	}
}

func TestCredentialedGitHosts(t *testing.T) {
	t.Setenv("TOKEN_A", "a-secret")
	t.Setenv("TOKEN_MISSING", "")
	tests := []struct {
		name string
		cfg  RunnerConfig
		want []string
		bad  bool
	}{
		{name: "empty", cfg: RunnerConfig{}, want: []string{}},
		{name: "legacy bound", cfg: RunnerConfig{GitToken: "legacy"}, want: []string{"github.com"}},
		{name: "legacy never distributed", cfg: RunnerConfig{GitToken: "legacy", GitAllowedHosts: []string{"a.invalid"}}, want: []string{}},
		{name: "map sorted canonical", cfg: RunnerConfig{GitToken: "legacy", GitHostTokenEnv: map[string]string{"A.invalid:443": "TOKEN_A", "missing.invalid": "TOKEN_MISSING"}}, want: []string{"a.invalid", "github.com"}},
		{name: "narrow", cfg: RunnerConfig{GitToken: "legacy", GitHostTokenEnv: map[string]string{"A.invalid": "TOKEN_A"}, GitAllowedHosts: []string{"a.invalid"}}, want: []string{"a.invalid"}},
		{name: "explicit empty deny", cfg: RunnerConfig{GitToken: "legacy", GitAllowedHosts: []string{}}, want: []string{}},
		{name: "missing explicit overrides legacy", cfg: RunnerConfig{GitToken: "legacy", GitHostTokenEnv: map[string]string{"github.com": "TOKEN_MISSING"}}, want: []string{}},
		{name: "duplicate canonical binding", cfg: RunnerConfig{GitHostTokenEnv: map[string]string{"A.invalid": "TOKEN_A", "a.invalid:443": "TOKEN_A"}}, bad: true},
		{name: "bad env name", cfg: RunnerConfig{GitHostTokenEnv: map[string]string{"a.invalid": "BAD=NAME"}}, bad: true},
		{name: "header injection", cfg: RunnerConfig{GitToken: "secret\r\nX-Leak: value"}, bad: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.cfg.CredentialedGitHosts()
			if (err != nil) != tt.bad || (!tt.bad && !reflect.DeepEqual(got, tt.want)) {
				t.Errorf("got %v,%v want %v bad=%v", got, err, tt.want, tt.bad)
			}
		})
	}
}

func TestGitHostPolicyAnonymousRequiresHTTPS(t *testing.T) {
	for _, authority := range []string{"public.invalid", "public.invalid:443", "public.invalid:80"} {
		t.Run(authority, func(t *testing.T) {
			cfg := remoteConfig(t, "allow_unauthenticated_https: true\nrequire_https: false\ngit_allowed_hosts: ["+authority+"]\n")
			calls := 0
			_, err := ensureCachedRemoteRepo("http://"+authority+"/repo", cfg, func(string, ...string) *exec.Cmd {
				calls++
				return exec.Command("true")
			})
			if err == nil || !strings.Contains(err.Error(), "HTTPS") || calls != 0 {
				t.Errorf("HTTP must be refused before Git: calls=%d err=%v", calls, err)
			}
		})
	}
}
