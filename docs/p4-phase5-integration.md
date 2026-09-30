# j9amjg42 phase5 — pinned main integration

## Scope and source

P4 base `1db5035ab2b9ca2ba7a61876c87a5bb37f74b63b`, pending merge of
`cd22b4bdc3b5229621169fe5b214d7ffd12a6015` into this worktree. This is not a
merge-to-main, commit, deployment or multi-mode activation. Main's untracked
`sdk-implementation-plan.md` and `sdk-multitenant-integration.md` remain untouched;
the latter's immutable authorized client/shared-SQLite ownership contract remains
binding. No SDK implementation or Brain task-status change is included.

## Source and caller reconciliation

- Preserve main's newer web, runner, MCP, Assistant/voice, delivery, revision CAS,
  exclusive/no-op Move, literal list wildcard handling, bulk recovery, supervisor
  receipts/budgets and sync/device APIs. Scoped P4 receivers win over main's
  unowned/embedded implementation, not by deleting the features' callers.
- Public admission is read-only before constructor PRAGMAs. Ledger/sync/tenant
  artifacts and version29/30 require the complete exact source classifier; private
  tenant catalogs, unknown profiles and lowered-stamp hybrids are refused. Genuine
  historical legacy paths without newer artifacts retain their documented policy.
  Runtime initialization produces main30-devices using its **unchanged exact pin**.
- `migrateAdmittedSchema` is the private migration body called only after admission;
  InitSchema does not try to re-admit its own temporarily incomplete bootstrap DDL.
  No version is relabelled to pass classification. Legacy migration tests use
  archived source fixtures rather than today's initializer plus a lowered number.
- Runtime30 `initEntrySync` retains main's INSERT OR IGNORE seeding behavior,
  including possible sequence consumption on ordinary startup. Dormant successor
  migration still preserves supplied epoch, receipts, tombstones and high-water
  state without reseeding existing history. These are separate test expectations.
- Context-bearing ledger and sync calls require their bound tenant context.
  Delivery verification CAS now includes tenant ownership in the note UPDATE;
  colliding IDs and stale/mismatched requests have a regression test and a workload
  manifest entry. The public tenant search receiver now routes tenant31 correctly.

## Ordinary readiness versus offline verification

The initial integrated HTTP test exposed a P4 boundary defect: after removing A's
CAS file, B's otherwise independent download failed because every tenant31 receiver
called the global migration validator. The user approved the following correction:

- `validateSuccessorReceiver` validates immutable provenance, exact shared catalog,
  indexes, ownership definitions and FTS mapping structure. It then validates only
  the bound tenant's child-row FKs, sync identity/high-water relationship, FTS
  content/index and filesystem/CAS readiness.
- FK anti-joins use the catalog's already-validated composite definitions and an
  explicit child `tenant_id` predicate. No global row integrity/FK sweep runs on an
  ordinary workload request. Shared engine/catalog corruption can still fail reads.
- FTS integrity-check has INSERT syntax. Ordinary validation runs it **only on the
  bound tenant's FTS table**, never a foreign table. This is not a rebuild. Whole
  database snapshots in the regressions remain unchanged. It still cannot run under
  SQLite `query_only`; no fully write-free SQL execution claim is made.
- Filesystem readiness stats/hashes only bound roots and attachment rows. The
  existing resolver retains all durable root mappings for exclusion/alias policy;
  overlapping mappings or canonical-root drift are not authorized by this change.
  Check-then-open filesystem races and hostile host users remain outside the model.
- `validateSuccessorSchema` and `validateTenantMigrationFiles` retain **full global**
  row/FK/integrity, FTS, roots and CAS checks for dormant migration and explicit
  migration reopen/idempotency verification. The private control/install-claim
  path also retains full verification. Public constructors still refuse tenant31.

`TestReceiverReadinessIsTenantLocal` covers missing CAS, corrupt CAS, corrupt FTS
segments, missing sync identity and dangling owned FK rows. Healthy B must succeed
through content/search/sync/budget receivers, damaged A must fail, foreign/global
table snapshots must remain unchanged, and global reopen verification must refuse
each damaged database. The existing real HTTP
`TestTenantAcceptanceLegacyParentAndReusedPaths` now passes unchanged in its
cross-tenant CAS assertion. Both tests were observed failing before the correction.

## Graph and sidecar lifetime

TenantStore remains private-backed and unembedded. No DB accessor, raw forwarding,
new raw/control method or exported storage package-function allowance is added.
The independent workload method manifest has 183 entries; binding/routing methods
are excluded as before. Main's inherited syntactic call-site inventory changes
track scoped caller additions/moves, not new raw storage access. Historical-base
review is recorded in the phase6 corrective addendum below.

Assistant jobs/conversations live in `internal/assistantjobs`; push in
`internal/phonepush`. Their private SQL operation interfaces expose neither DB
handles nor a registry. Existing single-mode paths remain `assistant-jobs/jobs.db`
and `push/notifications.db`. Exact export/constructor/call-site guards accompany
their existing persistence and worker tests. This relocation does not implement
shared-DB tenant import, delegation, revocation or tenant push ownership.

Main's bulk index-ready barrier uses P4's joined scan completion. Supervisor bridge
event processing starts in the boot-owned worker group and joins on cancellation,
not during graph/cache construction. Assistant job, push and bulk cleanup runs
before graph/DB shutdown; existing P4 asyncWork and indexer boundaries remain.

The tenant HTTP read-only allowlist is unchanged. New bulk, supervision, sync,
Assistant, push, delivery/resume/control and MCP paths remain denied. The phase5
route regression uses a real graph and compares content, events, all seven ledgers
and all four sync tables before/after denials. It is not proof of future delegated
provider authorization or send-time revocation.

## Verification (2026-09-12, uncommitted candidate)

- `CI=1 go test ./... -json -count=1 -timeout=10m`: **40/40 packages passed**;
  **10,708 passing test/subtest results** (4,947 top-level, 5,761 nested),
  **10 skipped**, **0 failures**. These counts include executable manifest wrappers
  re-running named tests; they are not counts of unique independent behaviors.
- Skips: three built-PWA-dependent tests; one intentional project-directory scan
  case; four private fixture/crash subprocess entry points; two unsupplied
  historical-baseline tests. No failing case was skipped to obtain success.
- Focused race run: **10/10 top-level tests, 17/17 total test/subtest results,
  2/2 packages**, no skips/failures or reported races. Storage 75.106s; apiserver
  221.039s. Includes new receiver readiness, delivery CAS, sync CAS, claim contention,
  exact raw/package and real type guards, concurrent HTTP, reused-path/CAS isolation,
  real factory/unsupported routes and phase5 zero-state-effect denials.
- The first race invocation exceeded the shell's 120s timeout; it is not counted as
  verification. A fresh uncached rerun with a 600s shell bound completed as above.
- `go build ./...` and `go vet ./...`: exit 0 following the completed race run.
- Final full-suite rerun after sidecar lint cleanup retained the same counts
  (storage 77.004s, apiserver 17.720s). Scoped `golangci-lint run` over storage,
  apiserver, assistantjobs and phonepush: **0 issues**. Both staged and unstaged
  `git diff --check` passed. This is not the aggregate phase6 `just check`.

## Phase6 / release boundaries

Run actual `just check` (including web typecheck/tests/lint), trusted-base ratchets,
independent semantic review and final manifest/documentation review before commit.
The absence of a supplied historical baseline in this run is explicit, not a waiver.
Review upstream formatting-only changes separately from semantic integration.
No production-copy recovery, physical CAS intent protocol, throughput/load SLO,
principal/grant lifecycle, hosted executor isolation or multi-mode readiness is
certified by these synthetic fixtures. The relational-readiness milestone remains
distinct from final coordinated-copy/load/release composition; do not bypass that
gate or introduce circular downstream dependencies.

## Phase6 corrective provenance reconciliation

The historical call-site gate initially failed against both independently supplied
sources. Equal aggregate counts did not establish provenance. The correction in
`unscoped_baseline_test.go` keeps the requested base's actual source and canonical
goldens, including independent no-golden bootstrap, as the comparison baseline.
It does **not** baseline this candidate or union either source's inventory.

Only comparisons resolving to these exact immutable commits have the following
reviewed site exceptions:

| Comparison base | Inherited source | Exact additional site keys |
|---|---|---|
| `014d3d1d5f0a62ef210fab83a10760ef55094c41` (P4) | `cd22b4bdc3b5229621169fe5b214d7ffd12a6015` (main) | `internal/apiserver/live_injector.go:bridgeLiveInjector.findTaskInstance ListAllInstances#1`; `internal/service/resume_with_context.go:TaskServiceImpl.ResumeTaskWithContext MergeMetadata#1`; `internal/service/resume_with_context.go:TaskServiceImpl.runResumeGate ClearDispatchLease#1 GetClaim#1 GetRunner#1 ReleaseClaim#1`; `internal/service/scheduler.go:SchedulerService.candidateRunners ListRunners#1` |
| `cd22b4bdc3b5229621169fe5b214d7ffd12a6015` (main) | `014d3d1d5f0a62ef210fab83a10760ef55094c41` (P4) | `internal/service/attachments.go:AttachmentServiceImpl.Create GetAttachmentByDigest#1` |

Before subtracting any of those **eight exact keys** from site additions, the gate
loads immutable Git object bytes and verifies the source really contains that key.
It compares the complete enclosing AST declaration, including signature, receiver,
arguments and control flow, plus referenced import identities, with the current
source. The P4 attachment spelling is explicitly included in **both** source scans:
P4 moved that method before adding the pre-CAS scope check, so P4's own golden no
longer supplied its vocabulary. No current golden is enlarged by this correction.

Receiver evidence is separate from main provenance: the task, attachment and runner
registry `storage` fields are pinned to P4's `*storage.TenantStore` declarations.
The registry constructor/delegates, injector interface/constructor and scheduler
interface/response-adapter chain are pinned too. `candidateRunners` uses
`runnerListResponseAdapter` over the tenant-backed registry, not a platform control
registry. The real `newTenantGraph` wiring test checks pointer identity through
both interface paths and the task/registry tenant stores. Existing constructor,
ownership, real-package type-surface, control and package-function guards remain
independent and unchanged. This is not arbitrary interface data-flow analysis,
SQL-predicate proof, principal authorization, or permission to expose new routes.

All other site additions remain forbidden; raw-method, control and package-function
allowances do not grow. A different/future PR base receives **no exceptions** and
still uses the actual `pull_request.base.sha` supplied by CI. The named historical
regression tests continue checking both reviewed sources, fail if required objects
are unavailable, and require explicit re-review when attested declarations change.
They do not fetch history or accept a mutable branch as substitute provenance.

Negative controls exercise changed calls/arguments/receivers/import identities,
raw holders, adapter/delegate drift, new sites/raw DB escapes both inside and
outside attested functions, and the exact reviewed name appearing against an
unrelated actual baseline. The original historical growth/shrink/bootstrap tests
are retained. Delivery CAS now checks the **exact decoded metadata object** (revision
1 and unrelated metadata preserved), populated foreign sync state and all five
foreign FTS shadow tables, plus whole-database invariance on stale/denied CAS.
Its old nonempty-metadata oracle was observed accepting six invalid mutations
before replacement. No production feature, schema, workflow or task status changes
are part of this correction; parent aggregate `just check` remains required.

Corrective verification on 2026-09-12:

- Both `TestProductionUnscopedStorageBaseline` and
  `TestProductionStoragePackageFunctionsBaseline` passed with each full SHA above
  explicitly supplied as `BRAIN_STORAGE_RATCHET_BASE` (4/4 gate invocations).
- Full affected storage package, uncached with the P4 baseline: passed in 91.935s.
  This includes the unchanged ownership/control/type and historical regression
  suites, not a rerun of the entire application suite.
- Final focused provenance/CAS/composition run: 5/5 top-level tests and 24/24
  subtests passed, no failures/skips (storage 18.224s, apiserver 0.290s). The 15
  provenance mutations each test both source baselines. Scoped graph binding,
  workload-constructor, resume-with-context and runner-list tests also passed in
  apiserver/service.
- `go build ./...`, scoped `go vet` for storage/apiserver/service and
  `git diff --check`: exit 0. `golangci-lint run` for storage/apiserver: **0 issues**.
- No aggregate `just check`, commit, staging, push or Brain state mutation was
  performed by the corrective writer.

## Independent review and aggregate-check corrections

Independent review examined the four repair commits from `014d3d1d`, the complete
main integration candidate, migration/receiver corruption probes, route/effect
sealing, sidecar ownership, and the historical source guards. Its first verdict
was FAIL on the historical ratchet described above; the source-attested correction
was independently rerun against both immutable baselines and received PASS.
Review also prompted exact delivery metadata and foreign sync/FTS assertions.

The first parent `just check` passed its isolation/race gates, full Go tests and
vet, then failed full-project lint on three inherited assistant API issues. The
correction explicitly discards the already-ignored response encoding error,
removes a redundant `err = nil`, and uses `http.StatusUnauthorized` in the test.
Full-project lint then reported zero issues.

After synchronizing the pinned web dependencies, typechecking passed. Web tests
exposed an inherited same-millisecond query-cache eviction bug: `accessed DESC`
could retain an old page while evicting a newer one when timestamps tied. Both
affected files matched pinned main before correction. Cache hits and puts now
allocate persisted monotonic recency in SQLite, retaining the 40-page cap and
unopened-entry sync exclusion. Frozen-clock regressions failed before the fix;
the focused suite passed ten consecutive runs and all 1,209 web tests passed.
Independent review additionally verified backward clock movement, replacement
recency, and wrapper reconstruction over the same database. That is not an OPFS
crash-recovery claim or SDK implementation.

The late lint/cache changes received a separate independent PASS. Parent
`CI=1 GOPROXY=off GOTOOLCHAIN=local GOMAXPROCS=2 just build-all` also passed,
including the production PWA bundle and embedded Go binary.

## Final bounded relational-readiness evidence

On 2026-09-12 the parent ran the complete recipe after the corrections and PWA
build:

```sh
CI=1 GOPROXY=off GOTOOLCHAIN=local GOMAXPROCS=2 \
BRAIN_STORAGE_RATCHET_BASE=014d3d1d5f0a62ef210fab83a10760ef55094c41 \
just check
```

**Exit 0.** The recipe completed the uncached tenant isolation gate and race tests,
`go vet ./...`, the uncached full Go suite (40 packages), full-project lint
(zero issues), web typechecking, and **1,209/1,209 web tests**, zero web skips or
failures. Built-PWA tests ran after `just build-all`; the remaining Go skips are
the intentional directory-scan case and four fixture/subprocess entry points.
Both trusted-baseline gates were also independently verified against full main
SHA `cd22b4bdc3b5229621169fe5b214d7ffd12a6015`.

Representative elapsed times from this run: isolation storage 91.723s, race
storage 10.628s, race apiserver 152.363s, full-suite storage 92.293s, full-suite
apiserver 13.489s, web tests 3.291s. These are test-suite timings on synthetic
fixtures, **not request latency or 100req/s evidence**. Receiver readiness still
has bound-tenant scan/FTS/CAS costs and filesystem concurrency limitations.

This closes the bounded `j9amjg42` source/schema/receiver repair and establishes
the reviewed relational contract for downstream work. Supervisor graph action:
split the P5/P6 dependency milestone from final release composition so those
children can consume this verified contract without a circular dependency.
No other task or final gate is completed here. `jr1xs3a3` still requires real
coordinated-copy/load and release-composition evidence. Assistant/push shared-DB
import, delegation/auth, effects and client recovery remain explicit P6/P8/P9
implementation obligations. Public multi-mode activation remains refused.
