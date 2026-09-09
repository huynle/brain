package runner

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/huynle/brain-api/internal/gitremote"
)

// gitCredentials resolves operator-owned environment variables. Legacy tokens
// are bound ONLY to github.com, regardless of any allowlist. An explicit map
// entry (even one whose environment variable is unset) overrides legacy auth.
func (cfg RunnerConfig) gitCredentials() (map[string]string, error) {
	tokens := map[string]string{}
	legacy := cfg.GitToken
	if legacy == "" {
		legacy = os.Getenv(cfg.GitTokenEnv)
	}
	if legacy != "" {
		tokens["github.com"] = legacy
	}
	seen := map[string]bool{}
	for raw, env := range cfg.GitHostTokenEnv {
		host, err := gitremote.Authority(raw)
		if err != nil {
			return nil, err
		}
		if seen[host] {
			return nil, fmt.Errorf("duplicate git credential host %q", host)
		}
		seen[host] = true
		if !validChildEnvKey(env) {
			return nil, fmt.Errorf("invalid token_env for git host %q", host)
		}
		tokens[host] = os.Getenv(env)
	}
	allowed, err := gitremote.Hosts(cfg.GitAllowedHosts)
	if err != nil {
		return nil, err
	}
	for host, token := range tokens {
		for _, c := range token {
			if c <= 32 || c >= 127 {
				return nil, fmt.Errorf("invalid git token for host %q", host)
			}
		}
		if token == "" || (cfg.GitAllowedHosts != nil && !slices.Contains(allowed, host)) {
			delete(tokens, host)
		}
	}
	return tokens, nil
}

// CredentialedGitHosts is the Phase 2 registry interface: sorted exact
// authorities with a currently nonempty credential AND allowlist permission.
// No token or environment-variable name leaves this function.
func (cfg RunnerConfig) CredentialedGitHosts() ([]string, error) {
	tokens, err := cfg.gitCredentials()
	if err != nil {
		return nil, err
	}
	hosts := make([]string, 0, len(tokens))
	for host := range tokens {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	return hosts, nil
}

func authorizeGitRemote(remote string, cfg RunnerConfig) (*url.URL, string, error) {
	tokens, err := cfg.gitCredentials()
	if err != nil {
		return nil, "", err
	}
	hosts := make([]string, 0, len(tokens))
	for host := range tokens {
		hosts = append(hosts, host)
	}
	// Anonymous use is opt-in twice: the flag AND an explicit host list.
	if cfg.AllowUnauthenticatedHTTPS {
		hosts = append(hosts, cfg.GitAllowedHosts...)
	}
	u, err := gitremote.Validate(remote, hosts)
	if err != nil {
		return nil, "", err
	}
	return u, tokens[u.Host], nil
}

func validateTaskGitRemote(remote string, cfg RunnerConfig) error {
	if remote == "" {
		return nil
	}
	_, _, err := authorizeGitRemote(remote, cfg)
	return err
}

// isolatedGitCommand never inherits Git, proxy, loader, tracing, credential,
// HOME/netrc, or shell startup environment. The executable/PATH and optional CA
// file are operator trust inputs. Network calls run only in a private fresh
// directory: -c cannot disable arbitrary local url.*.insteadOf/include rules.
func isolatedGitCommand(factory CommandFactory, dir, protocol, ca string, args ...string) *exec.Cmd {
	prefix := []string{"-c", "http.followRedirects=false", "-c", "http.sslVerify=true", "-c", "credential.helper=", "-c", "core.hooksPath=" + os.DevNull, "-c", "protocol.allow=never", "-c", "protocol." + protocol + ".allow=always"}
	if ca != "" {
		prefix = append(prefix, "-c", "http.sslCAInfo="+ca)
	}
	cmd := factory("git", append(prefix, args...)...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "XDG_CONFIG_HOME=" + dir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_SYSTEM=" + os.DevNull, "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=" + protocol, "GIT_CEILING_DIRECTORIES=" + filepath.Dir(dir), "LC_ALL=C"}
	return cmd
}

func gitRemoteArgs(u *url.URL, token string, args ...string) []string {
	if token == "" {
		return args
	}
	return append([]string{"-c", "http." + u.Scheme + "://" + u.Host + "/.extraheader=Authorization: Bearer " + token}, args...)
}

func transferGitRemote(u *url.URL, token, repo string, cfg RunnerConfig, factory CommandFactory, cached bool) error {
	private, err := os.MkdirTemp("", "brain-git-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(private)
	run := func(dir, protocol, ca string, args ...string) error {
		cmd := isolatedGitCommand(factory, dir, protocol, ca, args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %w", redactGitToken(strings.TrimSpace(string(out)), token), err)
		}
		return nil
	}
	if !cached {
		return run(private, u.Scheme, cfg.GitSSLCAInfo, gitRemoteArgs(u, token, "clone", "--template=", "--", u.String(), repo)...)
	}
	// Fetch NEVER reads cache config or origin while it has a credential. A
	// fresh bare staging repo costs a full transfer but gives a small auditable
	// isolation boundary. Import into the cache is credential-free/file-only.
	staging := filepath.Join(private, "incoming.git")
	if err := run(private, "file", "", "init", "--bare", "--template=", staging); err != nil {
		return err
	}
	if err := run(private, u.Scheme, cfg.GitSSLCAInfo, gitRemoteArgs(u, token, "--git-dir="+staging, "fetch", "--no-recurse-submodules", "--", u.String(), "+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*")...); err != nil {
		return err
	}
	return run(private, "file", "", "-C", repo, "fetch", "--no-recurse-submodules", "--no-auto-maintenance", "--prune", "--", staging, "+refs/heads/*:refs/remotes/origin/*", "+refs/tags/*:refs/tags/*")
}
