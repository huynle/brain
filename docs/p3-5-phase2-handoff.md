# P3.5 phase 2 handoff — uc09pikj

Worktree: `.worktrees/mt-p3-tenant-core`. No commit, merge, deployment, or Brain
task mutation was performed. This phase builds on the pre-existing phase 1
working-tree changes; it is not a replacement for them.

## Result and boundaries

- All production workload storage holders and their constructors use
  `*storage.TenantStore`; fixtures bind `tenant.Local` through `ForTenant`.
- Server composition passes the tenant view to workload services, scheduler
  leases, assignment cleanup and visibility. Identity/password/OAuth/MCP
  validators receive explicitly allowlisted, unembedded `ControlStore`.
- Registry and general token administration require the existing opaque
  deployment-operator capability. Unattended startup and offline CLI use the
  explicit OS database read/write authorization seam, not a bearer scope or a
  new required password. Its only production callers are
  `apiserver/storage.go:openSingleModeStorage` and
  `tokens/direct.go:openDatabase`. Ordinary handlers get no such capability.
- The supervisor-approved `SingleModeTokenStore` preserves single-install HTTP
  token management without granting operator authority. Construction requires
  explicit single mode; zero/nil/multi adapters deny. Replace this temporary API
  authentication exception in P6/P7 **before** multi enablement.
- P1 bootstrap safeguards were ported narrowly from `5611563`: actual loopback
  peer checks, exact remote-bootstrap environment opt-in, permanent install
  claim, atomic single winner, password/OAuth closure, credential-write and
  startup backfill, and exclusion of the claim marker from tracked-entry stats.
  Unrelated main filesystem/CORS/bind-auth changes were not merged.
- Storage remains one shared schema/pool. No per-tenant registry, DB stamp, or
  migration-per-view was introduced. Operational multi mode remains refused.

The detailed ownership rules and exact method allowlists are documented in
`docs/architecture.md` and tested by `internal/storage/ownership_test.go`,
`control_test.go`, and `single_mode_test.go`. The ownership policy is not the
P3.7 data-plane method ratchet.

## Verification evidence

Fresh commands run after implementation:

| Command | Result |
| --- | --- |
| `CI=1 go test -json ./... -count=1` | Exit 0; 35 tested packages pass; 4,347 top-level tests / 6,502 test-and-subtest outcomes pass; 0 failures. Fixture-only `storagetest` has no test files. |
| `CI=1 go build ./...` | Exit 0, including after building embedded web assets. |
| `CI=1 go vet ./...` | Exit 0. |
| `just lint` | Exit 0; `golangci-lint run ./...`: **0 issues**. |
| `CI=1 go test -race ./internal/storage ./internal/auth ./internal/api ./internal/apiserver ./internal/tokens ./internal/doctor -count=1` | All six packages pass, exit 0; storage took 263.016s. Initial 120-second tool invocation timed out; the completed rerun used a persistent terminal. |
| `CI=1 npm test` in `web/` | 1,101 tests pass, 0 failed/cancelled/skipped. Test output includes Zustand storage-unavailable warnings. |
| `CI=1 npm run typecheck` in `web/` | Exit 0. |
| `CI=1 npm run build` in `web/` | Exit 0; Vite transformed 2,611 modules and generated PWA assets. |
| `git diff --check` | Exit 0. |

Observed failing-first evidence included remote bootstrap returning 201 instead
of 403, missing single-mode compatibility factory, rejected local-owner
authentication stub, and production ownership policy identifying every raw
workload holder before migration. The imported P1 suite exercises concurrent
bootstrap, persistent closure, legacy credential backfill and rollback behavior.
New runtime tests exercise shared backing, constructor bindings, control identity
consumption/replay, zero/multi denials, offline multi refusal, and HTTP admin/read
scope compatibility.

An independent read-only review of the diff, new files, ownership policy and
wiring tests reported no critical or important findings. The review made no
edits, Brain calls, task-tracking changes or test runs.

## Remaining concerns / required follow-on work

1. TenantStore intentionally still embeds StorageLayer until **P4.10**. Its raw
   pointer, `DB`, `ValidateToken`, `Close`, and other promoted methods remain.
   This phase provides neither SQL predicates nor tenant isolation.
2. The host-ownership seam relies on OS read/write authorization to the existing
   regular DB and trusted deployment paths/process. It is not proof of a UID,
   an inode-bound credential, or protection against hostile same-process code.
   The source ownership policy constrains trusted production call sites.
3. Single-mode `admin:*` token management and legacy auth-disabled local behavior
   remain an explicitly temporary API-auth limitation. They must not be reused
   for multi mode. General Control/TokenAdmin capability checks remain intact.
4. The existing check-then-use filesystem policy and doctor database-path choice
   are unchanged. No export, purge, quota, tenant lifecycle or multi deployment
   readiness is claimed.

## Exact phase 2 file inventory

Paths below are relative to the worktree. This includes phase 1 files extended
for phase 2 (`operator.go`, `control.go`, control tests/interfaces, architecture),
but excludes untouched phase 1 files: `internal/storage/schema.go`,
`internal/storage/handles.go`, `internal/storage/handles_test.go`, and
`internal/auth/operator_test.go`. Those remain present with the user's phase 1
changes. This handoff file is also part of phase 2.

```text
docs/architecture.md
docs/p3-5-phase2-handoff.md
internal/api/auth_login.go
internal/api/bootstrap_integration_test.go
internal/api/bootstrap_phase2_test.go
internal/api/router.go
internal/api/tokens.go
internal/api/tokens_test.go
internal/apiserver/bootstrap_test.go
internal/apiserver/server.go
internal/apiserver/storage.go
internal/apiserver/storage_test.go
internal/auth/local_owner.go
internal/auth/local_owner_test.go
internal/auth/operator.go
internal/doctor/checks.go
internal/doctor/checks_scope_test.go
internal/events/cron_source.go
internal/events/cron_source_wiring_test.go
internal/indexer/indexer.go
internal/indexer/indexer_test.go
internal/indexer/storage_wiring_test.go
internal/indexer/tenant_policy_test.go
internal/indexer/watcher_test.go
internal/oauth/sqlstore.go
internal/service/attachments.go
internal/service/attachments_test.go
internal/service/brain.go
internal/service/brain_search_semantic_test.go
internal/service/brain_test.go
internal/service/checkout_mode_roundtrip_test.go
internal/service/client_context.go
internal/service/client_context_test.go
internal/service/features_test.go
internal/service/goal_service.go
internal/service/goal_service_test.go
internal/service/liveclaim_test.go
internal/service/project_placement.go
internal/service/project_placement_test.go
internal/service/reminder_service.go
internal/service/reminder_service_test.go
internal/service/resume_task_test.go
internal/service/runner.go
internal/service/runner_registry.go
internal/service/runner_registry_test.go
internal/service/runner_test.go
internal/service/storage_wiring_test.go
internal/service/task.go
internal/service/task_machine_affinity_test.go
internal/service/task_test.go
internal/service/tenant_policy_test.go
internal/service/trigger_store_adapter.go
internal/service/webhook_service.go
internal/service/webhook_service_test.go
internal/storage/bootstrap.go
internal/storage/bootstrap_test.go
internal/storage/control.go
internal/storage/control_identity.go
internal/storage/control_identity_test.go
internal/storage/control_interfaces_test.go
internal/storage/control_test.go
internal/storage/oauth.go
internal/storage/ownership_test.go
internal/storage/single_mode.go
internal/storage/single_mode_test.go
internal/storage/stats.go
internal/storage/storage.go
internal/storage/storagetest/store.go
internal/storage/tokens.go
internal/tokens/direct.go
internal/tokens/direct_scope_test.go
```
