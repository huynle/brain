# P3.8 phase 2 — reviewed integration inventory and combined regressions

Task `6aysuevx`. Phase 2 only, in `.worktrees/mt-p3-tenant-core`.
This is supplemental evidence, not a rewrite of the P0 inventory or P3.7 history
in [architecture.md](architecture.md#unscoped-storage-debt-ratchets-p37).
No production source, checker, CI workflow, historical policy, or activation
setting is changed by phase 2. P3/P4 integration remains subject to supervisor
review and phase 3 verification. No commit, push, install, restart or deployment.

## Revision boundary and review procedure

- Preserved phase-1 staged merge: HEAD `7a20876366bf040c4cbad78e1ed48c121bbc4104`;
  MERGE_HEAD/pinned main `4fa7210e1ba14113e61671db55cc33a84ceb9346`.
- Common ancestor `a1440e7f920ec96f41e7ab9c96c823322adfc6f4`.
- Incoming contracts `41e715d`, P1 `5611563`, P2 `4fa7210`.
- Phase-1 handoff: `/var/folders/x0/k6yjvm7d53l79z35btbzpg100000gn/T/opencode/mt-p3-phase1-handoff.md`.
- Ran the current AST inventory **before** editing goldens: 174 actual / 173
  golden methods; 319 actual / 315 golden site identities. Reviewed every
  reported addition and stale identity against `git show 4fa7210 -- <paths>`
  and the merged source. Applied only the explicit differences below with
  `apply_patch`; no generator/update switch or count-only waiver was used.
- P1 and contracts introduce **no additional golden deltas relative to P3.7**.
  P3 had already ported P1 bootstrap methods. This is an inventory observation,
  not a claim that P1 changed no production behavior.

## Exact-current baseline reconciliation

| Inventory | P3.7 | Added identities | Removed identities | Integrated |
|---|---:|---:|---:|---:|
| StorageLayer method names | 173 | 1 | 0 | **174** |
| External syntactic site tokens | 315 | 7 | 3 | **319** |

Every row below has source commit **`4fa7210e1ba14113e61671db55cc33a84ceb9346`
(P2)**. File/function/selector identities, not line numbers, are authoritative.

### Method addition

| Identity | Reviewed implementation / remaining obligation |
|---|---|
| `UpdateRunnerCapabilities` | `internal/storage/runners.go`, StorageLayer receiver. Replaces the nonsecret capabilities advertisement with `UPDATE runners SET capabilities = ? WHERE runner_id = ?`, avoiding a stale whole-row rewrite. This remains genuinely unscoped SQL, not tenant isolation. P4 must tenant-qualify it and move the receiver. |

### Site additions (seven individual identities)

| Exact identity | Review and provenance |
|---|---|
| `internal/runner/config.go:LoadConfigFrom Control#1` | Newly preserves `fileCfg.Control` when assembling RunnerConfig. Configuration field, not StorageLayer.Control; retained as intentional syntax-only false positive. |
| `internal/runner/workdir_policy.go:validateWorkdirPreflight Control#1` | Reads `config.Control.AllowedWorkdirRoots` in shared task/ad-hoc preflight; moved responsibility from BridgeClient.allowedWorkdirRoots below. Configuration field, not a storage factory. |
| `internal/service/git_remote.go:validateConfiguredGitRemote ListRunners#1` | Reads configured credential-host advertisements before persistence. Incoming helper accepted StorageLayer; phase 1 changed both Git helper arguments to TenantStore. Promoted ListRunners remains P4 query debt. |
| `internal/service/runner_registry.go:RunnerRegistryServiceImpl.Heartbeat UpdateRunnerCapabilities#1` | Updates/revokes advertisement before refreshing heartbeat, including explicit empty capabilities. Holder is TenantStore; SQL is still promoted/unscoped. |
| `internal/service/task.go:TaskServiceImpl.recordClaimPlacementDenial RecordPlacementReason#1` | Records named denial diagnostics without altering an existing claim/lease. TenantStore holder; P4 must scope diagnostic rows. |
| `internal/service/task.go:TaskServiceImpl.validateClaimAndGetFeatureID GetNoteByPath#1` | Existing featureIDForTaskClaim lookup moved to validation function; now feeds affinity/capability/Git checks before ownership. Identity move, not an eliminated lookup. |
| `internal/service/task.go:TaskServiceImpl.validateClaimAndGetFeatureID GetRunner#1` | New registered-runner preflight for direct claims/dispatch. Service-level validation also replaces the removed handler precheck below, but is not merely a textual move: it covers more entry paths. |

### Removed/moved sites (all three)

| Exact removed identity | Source and disposition |
|---|---|
| `internal/api/tasks.go:Handler.HandleDispatchTask GetRunner#1` | P2 removes the handler-only 404 short-circuit. TaskService validation now checks registration and records a named placement denial (403). See validateClaimAndGetFeatureID.GetRunner above. |
| `internal/runner/bridge_client.go:BridgeClient.allowedWorkdirRoots Control#1` | P2 removes this helper; shared validateWorkdirPreflight reads the same config field for task and ad-hoc execution. Remove stale identity, retain new location. |
| `internal/service/task.go:TaskServiceImpl.featureIDForTaskClaim GetNoteByPath#1` | P2 renames/expands helper to validateClaimAndGetFeatureID. Old allowance removed; lookup retained at new identity. |

No other golden identities changed. In particular, this does not remove DB
forwarding escapes, loosen the conservative selector vocabulary, or hide
unscoped operations behind an approved count. Later P4 must **shrink from the
integrated 174/319 baseline**, removing stale allowances and supplying its own
SQL isolation evidence. P4.10 still must remove TenantStore's embedding.

## Historical semantics and actual failures (not passes)

The exact-current test compares the **working tree** with working-tree goldens.
It passes after this review. The historical test independently compares both
source and goldens with the requested committed base. Updating a golden does
not make historical growth pass. The pending merge's index is not a historical
baseline; its source scan includes unstaged files.

All actual historical invocations used:

```sh
CI=1 BRAIN_STORAGE_RATCHET_BASE=<base> GOMAXPROCS=2 go test -p 2 ./internal/storage -run '^TestProductionUnscopedStorageBaseline$' -count=1 -v
```

| Base | Actual result | Base semantics |
|---|---|---|
| `7a20876366bf040c4cbad78e1ed48c121bbc4104` | **FAIL**, exit 1, 0.803s | Existing P3.7 goldens: 173 methods / 315 sites. |
| `b8ec49c` | **FAIL**, exit 1, 0.852s | Pre-P3.7 source: no goldens; seeds each missing inventory from actual base bytes, 173 / 315. |
| `4fa7210e1ba14113e61671db55cc33a84ceb9346` | **FAIL**, exit 1, 0.741s | Pinned integrated P1/P2 main: no goldens; actual base source under shared vocabulary is 168 / 312. |

For **both 7a20876 and b8ec49c**, the exact failure is:

- `methods source: head=174 base=173; additions forbidden` and the identical
  `methods golden` failure: `UpdateRunnerCapabilities`.
- `sites source: head=319 base=315; additions forbidden` and the identical
  `sites golden` failure: precisely the **seven identities** in the additions
  table above (including the moved GetNoteByPath). Removed sites do not cancel
  additions; comparisons are by identity, not net counts.

For **4fa7210**, the exact failure is:

- `methods source: head=174 base=168; additions forbidden` and identical
  `methods golden`: `Control`, `ForTenant`, `ListTenantRoots`,
  `RegisterTenantRoots`, `SingleModeTokens`, `listNotes`.
- `sites source: head=319 base=312; additions forbidden` and identical
  `sites golden`: the eight tokens below.

These are pre-existing P3 contributions, **not new phase-2 allowances**:

| Method/site identity rejected against main | P3 source commit / purpose |
|---|---|
| `Control`, `ForTenant`, `SingleModeTokens` | `3647218`, explicit shared-pool adapters and single-mode compatibility factory. |
| `ListTenantRoots`, `RegisterTenantRoots` | `9234c76`, authoritative durable root registry. |
| `listNotes` | `b8ec49c`, private implementation behind explicit query-scope validation. |
| `internal/apiserver/storage.go:openSingleModeStorage Control#1` | `3647218`, obtain identity/control view at audited composition. |
| `internal/apiserver/storage.go:openSingleModeStorage ForTenant#1` | `3647218`, bind local data view. |
| `internal/apiserver/storage.go:openSingleModeStorage MarkInstallClaimed#1` | `3647218`, guarded control-owner claim; originally P1 `5611563` in buildHTTPHandler, moved by P3 and retained by phase 1. |
| `internal/apiserver/storage.go:openSingleModeStorage SingleModeTokens#1` | `3647218`, explicit single-mode token adapter. |
| `internal/doctor/checks.go:loadAttachmentDigestChecksFromDatabase ForTenant#1` | `3647218`, offline local workload view. |
| `internal/tenantfs/resolver.go:Resolver.register RegisterTenantRoots#1` | `9234c76`, register immutable mapping. |
| `internal/tenantfs/resolver.go:Resolver.snapshot ListTenantRoots#1` | `9234c76`, re-read authoritative policy. |
| `internal/tokens/direct.go:openDatabase Control#1` | `3647218`, offline operator token adapter. |

Relative to main there are eight site additions and one removed/moved token:
`internal/apiserver/server.go:buildHTTPHandler MarkInstallClaimed#1` is now
the guarded composition call above (312 + 8 - 1 = 319).

The prior P3.7 pass against b8ec49c remains **historical evidence for the old
checkout**, not a pass for this integration. Initial seeding is not permission
to adopt head goldens: absent base goldens use actual base source, independently
for methods and sites. None of these pre-integration bases contains both lines
of work, so none proves monotonicity of this merge. No base was silently changed
to obtain green. CI's trusted PR base selection and checker remain unchanged;
a PR against pinned main would still fail the strict historical gate. Supervisor
integration review is unresolved, not waived by this document. Once trusted
integration is committed, subsequent P4 comparisons must use that integrated
base (and CI's actual PR base), not an old branch or a self-selected head.

## Combined regression coverage and constructor review

Read phase-1 `tenant_policy_test.go`, P1 `containment_test.go`, and P2
`git_remote_test.go` before adding `tenant_git_integration_test.go`.
Existing tests already cover transient registry/durable-sync failure, child
preflight, unbound containment, malformed/effective/stale-disk remote metadata,
and checkout supersession. They were retained, not duplicated wholesale.

New test `TestBoundTenantGitAdmissionBeforeMutation`: **24 leaf cases** across
Save, Update, UpdateMetadata (durable title/status plus Git metadata), and
CheckoutFeature (existing AI checkout requested as simple):

- supported remote + bound local policy succeeds and changes both disk and index;
- unsupported host alone fails Git input admission;
- outside-root alias alone fails filesystem admission;
- alias into reserved `tenants/foreign` alone fails tenant exclusion;
- each forbidden alias combined with unsupported host fails without requiring
  a particular guard precedence.

Every denial snapshots all indexed notes and both filesystem trees, including
directories and symlinks, so new files/directories, external writes, index
mutations and premature checkout supersession are observable. BrainService
mutation events are captured with the existing synchronous recordingBus and
must not increase. CheckoutFeature has no event-bus constructor seam. No real
Git transport/provider, server or runner is launched. This is stable-filesystem
single-mode composition evidence, not SQL tenant isolation or a symlink-race
sandbox. Fixtures initially had wrong IndexFile arguments and then an overly
narrow expected error: bound BrainService paths return tenantfs.ErrDenied while
checkout can reach brainpath.ErrContainment first. Corrected tests to match
those inspected contracts; no production changes or bug-fix RED/GREEN claim.

Reviewed merged `NewIndexer`, `NewBrainService`, `NewTaskService`, and
`NewRunnerRegistryService`: all take `*storage.TenantStore`. Both incoming Git
helpers (`validateConfiguredGitRemote`, `validateMetadataGitRemote`) likewise
take TenantStore; diff against main shows only their two parameter-type changes.
Server uses `views.tenant` for workload services and the same root/indexer;
identity/claim ownership stays with guarded ControlStore composition. This is
still the temporary promoted-method transition, not a compile-time isolation
claim. Existing production ownership checker also passes.

## Executed verification

All test runs used CI mode, `GOMAXPROCS=2`, `-p 2`, and `-count=1`.

- Initial exact inventory failed with the precise reviewed deltas above.
- `go test ./internal/storage -run 'Test(ProductionUnscopedStorage$|Unscoped|ProductionStorageOwnership|StorageOwnership)' -v`:
  **PASS**, 3.092s. Exact-current goldens, production ownership, AST checker and
  26 real-Git historical regression scenarios (13 with goldens, 13 bootstrap)
  all pass. These include coordinated growth, source/golden-only growth,
  same-count replacements, missing/empty/option refs, shrinkage, receiver moves,
  and archive-attribute resistance. Not an actual historical-base pass.
- `go test ./internal/service`: **PASS**, 11.595s (full service package).
- `go test -race ./internal/service -run '^TestBoundTenantGitAdmissionBeforeMutation$' -v`:
  **PASS**, 24/24 leaf cases, no skips/failures/race reports, 5.832s.
- Actual historical checks: **three failures remain**, explicitly recorded above.
- Final focused rerun: storage command above **PASS**, 3.043s;
  `go test ./internal/service -run 'Test(BoundTenantGit|TenantPolicyService|ServicePropagatesTransientPolicyFailures|DurableMetadataPolicy|TaskDirectoryReadErrors|GitRemote|Containment)'`
  **PASS**, 0.877s.
- `git diff --check`, `git diff --cached --check`: clean. `gofmt -l` on the new
  Go test: empty. No unresolved merge paths. Checker files, historical regression
  files, `.github/workflows`, and `docs/architecture.md` have no diff from HEAD.
- Final HEAD remains `7a20876`; MERGE_HEAD and main remain `4fa7210`. Original
  98 staged paths are preserved. Phase 2 leaves two golden edits unstaged and
  the new test/evidence files untracked; nothing was staged or committed.

Full repository build/vet/lint, web checks and broad runner/service race evidence
belong to phase 3 and are not claimed here. The task is not marked completed.
