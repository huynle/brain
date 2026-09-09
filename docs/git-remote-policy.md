# Task Git remotes: credentialed-host admission

Staged runner hardening; not a deployment or release approval. This policy
governs the task's `git_remote` and the runner-managed clone/fetch path. It is
not a sandbox for arbitrary Git commands in agent code, hooks or scripts.

## Configuration

Unified `~/.config/brain/config.yaml` (respects `XDG_CONFIG_HOME`):

```yaml
runner:
  repo_cache_dir: /srv/brain-repo-cache
  git_host_token_env:
    github.com: GITHUB_TOKEN
    git.example.com: COMPANY_GIT_TOKEN
    git.example.com:8443: COMPANY_GIT_8443_TOKEN
  # Optional restriction. Omit to allow only hosts with configured credentials.
  git_allowed_hosts:
    - github.com
    - git.example.com
    - git.example.com:8443
  # Optional operator-owned CA bundle; use an absolute path on the runner.
  git_ssl_ca_info: /etc/brain/git-ca.pem
  allow_unauthenticated_https: false
  control:
    allowed_workdir_roots:
      - /srv/projects
      - /srv/brain-repo-cache
```

Flat/legacy files use the same keys without `runner:`:

```yaml
repo_cache_dir: /srv/brain-repo-cache
git_host_token_env:
  git.example.com: COMPANY_GIT_TOKEN
git_allowed_hosts: [git.example.com]
git_ssl_ca_info: /etc/brain/git-ca.pem
```

The environment-variable **names**, not token values, belong in
`git_host_token_env`. A nonempty token must be present in that runner's
environment. An explicit host mapping overrides legacy credentials even when
its environment variable is unset/empty. Duplicate canonical host mappings and
malformed authorities are configuration errors.

- **Omitted `git_allowed_hosts`**: effective permission is the credentialed-host
  set. There is no default arbitrary-host permission.
- **Explicit `git_allowed_hosts: []`**: deny all, including GitHub. This remains
  distinct from omission through unified config serialization, GET/PUT and
  legacy-config migration. YAML `null` is omission, not an empty list.
- A nonempty allowlist narrows credentialed support; listing a host alone does
  not supply a credential.
- Legacy `git_token` / `git_token_env` bind **only to `github.com`**, never to all
  allowed hosts. `git_token_env` defaults to `GITHUB_TOKEN`. `RUNNER_GIT_TOKEN`
  overrides the file token; `RUNNER_GIT_TOKEN_ENV` overrides the source name.
  A resolved nonempty legacy token takes precedence over the source variable.
  The config API redacts a literal `git_token` and preserves it on an unchanged
  GET/PUT round trip. Host-map values are nonsecret source names.
- `require_https` remains a legacy config key, but setting it false no longer
  permits HTTP. Remote admission is always HTTPS-only.
- Existing anonymous transport support requires **both**
  `allow_unauthenticated_https: true` (or `RUNNER_ALLOW_UNAUTHENTICATED_HTTPS`)
  and an explicit allowed host. **The task API still requires credentialed
  support**: anonymous-only hosts are not advertised or accepted for tasks.
- TLS verification stays enabled. Ambient Git/proxy/CA environment is not
  inherited by the isolated transport; use `git_ssl_ca_info` for a private CA.
  Workdir-root authorization also applies to cache destinations.

## Admission and dispatch

The runner recomputes `CredentialedGitHosts()` at registration and every
heartbeat. It advertises only canonical authorities as capabilities:

```text
git-credential-host:github.com
git-credential-host:git.example.com:8443
```

This reserved prefix is derived, not copied from generic configured
`capabilities`. Tokens and environment-variable names are never advertised.
Invalid credential configuration withholds all host advertisements. Heartbeat
`capabilities` is a full replacement: `[]` revokes old support; absent/null
preserves legacy heartbeat behavior. Registration replaces the full set. The
existing runner capabilities column is reused; no schema migration is needed.

`Save`, full `Update`, and `UpdateMetadata` validate task remotes before writes.
A status-only retirement to `archived`, `cancelled`, or `superseded` (optionally
with a note/completion stamp) may preserve an existing legacy remote without
re-admitting it. This makes old tasks removable from the active queue. It cannot
change execution configuration or the remote, and reopening the task still
requires normal admission. Bulk updates use the same rule.
Metadata checks include the merged DB value and the disk value that a durable
sync would re-index, so an unrelated status/execution patch cannot restore a
forbidden remote. Changing metadata `type` cannot disguise an existing task.
Explicitly clearing/replacing `git_remote` via full `Update` is the repair path
for a legacy task. Local paths and executor/execution-mode defaults are not
exemptions. Current task defaults do not define a `git_remote` field.

The direct `CheckoutFeature` writer validates the inherited feature remote
before creating or superseding a checkout task. Other generated task writers
that call `Save` use the same admission gate.

Admission uses the union of **registered configured support**, including
offline runners; it is not a live-availability check. With no advertisements,
a nonempty task remote is refused. Unsupported-host errors name the exact
authority. A registration is a trusted runner's assertion of configured
support, not proof that a token is valid or authorized for a particular repo.

Push scheduling, runner-filtered pull selection, and direct task claims use the
same host/liveness predicate. The chosen runner must itself advertise that host
and be online, unpaused and not draining. Existing push placement constraints
still apply. Unregistered runners cannot pull or claim remote-bearing tasks.
Unfiltered queue reads remain inspection, not authorization to execute.
Advertisements can become stale between heartbeats; the phase-1 executor gate
independently rechecks its local credential/host policy before setup/spawn.

## HTTPS transport and cache changes

Authorities match exactly: DNS case and port `443` normalize, but subdomains and
other ports do not inherit permission. SSH/SCP, HTTP, file/helper protocols,
embedded credentials, query strings and fragments are refused. Redirects are
disabled, including same-host redirects: configure the final canonical repo
URL rather than relying on a redirect. This prevents forwarding credentials
to a redirect target.

Runner-managed network Git commands execute from fresh private directories,
with isolated Git configuration/environment and authority-scoped authorization.
They do not authenticate against a cached checkout's `origin`, credential
helpers, URL rewrites or local includes. Cached fetch uses a fresh bare staging
repository, followed by a credential-free, file-only import into the cache.
**Tradeoff:** staging is recreated for each fetch, so network transfer is a
full fetch rather than an incremental fetch against the existing cache. This
cost is intentional to avoid trusting cache configuration while holding a token.

Cache names are SHA-256 of the canonical remote URL, replacing the old sanitized
host/path name. Existing old-name caches are not automatically moved or deleted;
the next remote setup creates a new cache and consumes additional disk/transfer.
Do not rename an old cache blindly: it can contain local work, linked worktrees
or untrusted Git configuration. Audit/back up and clean it only as a separate
operator action. No cache migration or deployment is performed by these changes.

## Verification boundary

Local tests cover pre-persistence refusal, metadata/disk bypasses, checkout
supersession, configured/offline admission, compatible dispatch/pull/claims,
heartbeat revocation, config round trips and transport isolation. They do not
prove real credential validity, private-CA compatibility with a production
server, real LLM operation, or production remediation. The standing-credential
and release restrictions in [runner-credential-boundary.md](runner-credential-boundary.md)
still apply. Known full-runner race failures in serve-process globals/concurrent
`Wait` are outside this change; targeted race testing is not a full-runner race
clearance.
