# Runner credential boundary — staged P2 hardening

Task `esdw5ybh`, feature `mt-p2-runner-hardening`. **Not a production
remediation or release approval.** Existing production is unmodified and remains
exposed. Deployment/remediation is blocked on P7 and public release gate `ewavusmp`.
There is no compatibility switch restoring standing-token export.

## Ownership and export audit

Runtime `RunnerConfig.StandingToken` / `StandingTokenEnv` belong to the runner's
API/bridge client, not agents. Persisted `api_token` / `api_token_env` keys and
the unified-config adapter retain their existing names.

The four explicit export points audited were:

1. `executor_common.go:CommonBuildEnvExports` — generated shell exports.
2. `executor_common.go:CommonBuildEnvMap` — formerly unused, duplicated the
   exporter (including its standing-token leak). Now the single policy used
   by exports, direct child environments, and generated launchers.
3. `executor.go:spawnScript` — formerly exported the token on top of
   `os.Environ`; now uses the shared policy.
4. `bridge_client.go:execEnv` — formerly exported the token and inherited the
   environment, including in its nil-runner fallback; now uses the policy.

Implicit inheritance was also removed from OpenCode's attached/fallback driver,
`startHeadlessServer`, legacy `spawnPi`, Pi's headless executor, bridge ad-hoc
serve, and directory/inline pre/post hooks. Hooks receive the runner config so
they can filter a custom source alias even without `BRAIN_API_TOKEN` set.
The default passthrough list no longer includes Brain credentials.

## Policy

- All ambient/passthrough/task-supplied `BRAIN_*` variables are reserved,
  including endpoint, runner, project, task, tenant, claim and credential names.
  Only the configured API endpoint is injected by the shared policy. Hooks add
  event context explicitly. No tenant or claim identity is invented.
- The configured standing source alias is removed regardless of its current
  value. Values containing the known standing credential, canonical token, or
  configured source token are discarded even under unrelated names. This is
  literal substring filtering, not encoded-secret detection.
- Useful ordinary environment (PATH, HOME, locale, tool/provider configuration)
  survives. Valid ordinary task env overrides still work; invalid shell keys,
  NUL values, and startup controls (`BASH_ENV`, `ENV`, `SHELLOPTS`, `BASHOPTS`,
  `ZDOTDIR`, `PS4`) do not. Explicit passthrough cannot bypass this policy.
- Generated tmux/dashboard launchers use `#!/bin/bash -p` to ignore inherited
  startup processing/functions, then `env -i` and a sanitized snapshot before
  starting the inner non-login shell. A stale tmux environment is discarded at
  the generated-script boundary, not just in the tmux client. Environment,
  workdir, executable, prompt path and generated argument values are shell-quoted.
  New and rewritten launcher files are owner-only; they contain no known standing credential,
  but can contain other forwarded configuration/provider secrets.
- Bridge shell execution is non-login to avoid profile-based reintroduction.
- `injectTaskCredential` is an explicit **empty P7 stub**. It neither mints nor
  forwards credentials and never falls back to the standing token. Authenticated
  queue-skill access is therefore not supplied by this staged change.

## Direct claim and dispatch placement boundary (q96yzqvh Phase 2)

The service enforces runner registration, machine affinity, required capabilities
and Git-remote eligibility before direct claims or dispatch create ownership.
Both HTTP endpoints use `errors.As` to map `types.PlacementDenialError` to the
existing JSON error envelope, for example:

```json
{"error":"Forbidden","message":"runner_unregistered"}
```

HTTP 403 reasons are `runner_unregistered`, `machine_affinity_mismatch`,
`machine_affinity_unresolved`, `required_capability_missing` and
`git_remote_ineligible`. Internal error causes are not exposed in this response.
Direct dispatch no longer returns a competing registry 404: the service records
the named denial in placement history. Denials create no claim/dispatch lease
and emit no task-claimed event or claim/dispatch SSE. Ownership conflicts remain
HTTP 409; legitimate direct dispatch retains its lease-bearing runner command.

**This is placement validation, not caller-to-runner authentication.**
`internal/api/middleware.go` exposes only an API-token `Name`, OAuth `ClientID`,
or JWT subject plus scope through `AuthResult`. Token and runner-registry storage
have no trusted principal-to-runner binding. A name, client ID or subject is not
a runner ID; deriving `runnerId` from one would invent a binding and break valid
clients without proving ownership. Claim therefore still accepts a caller-supplied
`runnerId` under the existing `runner:*` / `admin:*` scope gate. Admin dispatch
deliberately preserves selection of another runner via `targetRunnerId` under
`admin:*`. A scoped caller's ability to name another registered runner is **not
closed by this phase**. P7 must establish and validate trusted identity binding
and task/claim credentials before remediation or release is claimed.

The P2.3 [credentialed-host invariant](git-remote-policy.md) is unchanged:
task remote admission requires registered credentialed-host support, while the
selected runner itself must advertise that host and be online, unpaused and not
draining for remote-bearing dispatch/pull/direct claims. Generic host permission
or anonymous-only transport is not sufficient. This phase does not weaken that
predicate or implement new credential minting, tenant identity, or a broker.

Local endpoint regressions use the real task/registry services, storage, event
service and SSE hub behind the authenticated router. They cover named denials
and history, absent ownership/events/commands on refusal, legitimate claims and
lease-bearing admin dispatch, and preserved 409 conflicts. Test principals have
names distinct from runner IDs; these tests do not assert an identity binding or
constitute the live P7 authorization tests below.

## P7 exit/release gate: `ewavusmp`

Current `read:*` / `runner:*` scopes cannot perform queue entry writes without
admin. Do not solve that by injecting admin, building a broker here, or inventing
new authentication. Queue skill needs: assigned-task read, append notes, task
status writes, and resume logs. It does **not** need admin, runner control,
credential minting, or settings access.

Outstanding before deployment/remediation can be declared:

- Implement and validate a claim/task-bound credential path at the injection
  stub, including the agent-hosting serve process, expiry/revocation and lifecycle.
- Replace live runner/agent instances and stale launchers/environments; address
  previously exposed credentials according to the deployment rotation plan.
- Run live positive tests for assigned-task reads/notes/status/resume logs and
  positive admin/control tests using only the appropriate authorized principal.
- Run live negative tests proving task credentials cannot access admin/control,
  mint/settings, other assignments or unauthorized identities, including stale
  and expired claims. These tests are **outstanding**, not covered by local dummy
  executables or the current passing Go suite.

## Limits

This is environment hygiene, **not a sandbox**. Shared UID/HOME, readable Brain
config, agent/MCP configuration, filesystem/process access, trusted executables,
and explicit shell commands can independently supply credentials. Shell startup
before a tmux launcher is entered remains part of the shared-host trust boundary.
The nil-runner fallback removes reserved names and aliases of the canonical
token when known, but cannot identify a custom source or file-only secret without
runner configuration. Unknown/encoded secrets cannot be inferred.

This checkout has multi-project scheduling, not a real multi-tenancy mode.
Reserving tenant names does not implement tenant isolation.

Local tests execute dummy children at direct/script/Pi/serve/bridge/hook and
generated-launcher boundaries, with stale environments, custom source aliases,
reserved overrides, literal shell values and startup variables. They do not
launch a live tmux server, real agents, or production authorization tests.
