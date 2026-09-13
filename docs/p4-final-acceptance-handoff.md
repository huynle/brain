# P4 final acceptance handoff — preparation only

**Current bounded implementation evidence:** [j9amjg42 phase5](p4-phase5-integration.md)
now reconciles pinned main and its caller/storage surfaces. The runtime is
single-mode30 with private29/tenant31 still publicly refused. This historical
preparation ledger does not override that update; its real-copy, load, physical,
authorization and release-composition obligations remain open.

`jr1xs3a3`, phase 2, 2026-09-12. No migration/activation, schema edits, merge,
commit, Brain task access or production/remote access. P4 HEAD:
`f9c68205abb3803430a77f915342073405496888`; audited main:
`cd22b4bdc3b5229621169fe5b214d7ffd12a6015`. Main findings below are the supplied
read-only audit, not a new deployment inspection. Preserve runtime **28** and
dormant private **29**. Main **29 = bulk jobs**, **30 = budgets/supervisor**;
numeric 29 is NOT a compatible ownership schema. Never relabel a main DB as v28
or private29, drop unknown objects, or widen catalog allowances to force admission.

Read and preserved `/Users/huy/projects/brain-api/docs/sdk-multitenant-integration.md`
(SHA-256 `167db7f8f9d78b9f233b6ca49e742a2d44d1b87ef19393d2c7fb34289d449d4d`).
Its shared-SQLite topology, immutable authorized tenant clients and release
boundaries apply; no per-tenant database registry shortcut.

## Ownership and bounded supervisor repair scopes (proposed, not dispatched)

Each row is a separately reviewable scope. Preserve existing task dependencies;
do not reset whole features, broaden project filters, trigger work or mark gates
complete from this document. Any future task edits require exact selected IDs and
reviewed revisions; no such edits were made here.

| Scope / owner | Exact reconciliation work and exit evidence |
|---|---|
| P4 schema reconciliation | Review `bulk_jobs`, `bulk_job_items`; `execution_budgets`, `budget_reservations`; `supervisor_checkpoints`, `supervisor_checkpoint_versions`, `supervisor_operations`; `entry_sync_devices`, `entry_sync_identity`, `entry_sync_changes`, `entry_sync_operations` (**11 tables**) plus all sync note triggers. Classify ownership/control exceptions and scoped PK/unique/FK/query semantics. Design a distinct successor/version provenance check covering genuine main29/main30 and private29; preserve v28 compatibility. Add exact catalog, populated backfill, sequence, rollback/reopen, own/foreign snapshots and fail-closed unknown-object tests before revising manifests. No migration implementation in this phase. |
| P4 relational / P8 effect receipts | Bulk items retain attempts, fingerprints and uncertain outcomes; budgets retain windows, parents, committed reservations and revisions; checkpoint current/history retain payloads and revisions; supervisor receipts retain `(tenant, actor, id)`, digest, state and detail. Test collision/late-failure rollback and no duplicate effect after restart. Never infer successful execution from accepted/delivered or automatically replay `outcome_unknown`. Scope repair to these ledgers and their callers, not runner-wide resume. |
| P4 sync storage / P6 auth / P9 offline lifecycle | Devices and operations require authorized tenant/principal ownership; changes require tenant-qualified paths and trigger behavior. Review whether identity/epoch is control state versus tenant cursor generation. Preserve deletion tombstones (including paths absent from notes), conflict/idempotency receipts, sequence watermarks and epoch across copy/reopen. Never rebuild from live notes alone or reset epoch silently. Test old cursor rejection/resnapshot without lost pending edits or resurrected deletes. |
| P4 sidecar inventory / P6 delegation / P8 jobs / P9 lifecycle | `assistant-jobs/jobs.db`: `assistant_jobs`, `conversations`. Preserve single-mode path, delegated credential provenance, job history and ambiguous execution state in protected coordinated snapshots. Define shared-SQLite tenant-qualified import/ownership, export/erasure and revocation fences; quarantine stale delegation and unknown execution rather than resuming it. Test two-tenant conversation/job IDs, worker restart and late callbacks. Credentials must never appear in evidence. |
| P4 sidecar inventory / P6 ownership / P8 push / P9 opt-in | `push/notifications.db`: `keys`, `devices`, `deliveries`. VAPID keys are protected installation/control material unless explicitly redesigned; devices/subscriptions and deliveries need tenant/principal ownership, tenant opt-in, retry/dedup state and send-time lifecycle checks. Preserve key recovery and delivery uncertainty; no restored broadcast or blind retry. Already accepted provider payloads cannot be recalled. Test export/restore/deletion and two-tenant delivery denial before admission. |
| P6/P8 transport / P9 client / SDK | Main routes: `GET /api/v1/assistant/jobs`; `POST /api/v1/assistant/speech`, `/api/v1/assistant/transcribe`, `/api/v1/assistant/voice-diagnostics`. Keep sealed in multi mode. Assistant's fixed persistence key is NOT tenant binding: partition by origin, authorized principal/tenant and grant generation; cancel old streams and discard late results on switch. Test actual handlers/delegation/providers later; fixture 501 coverage added here is rejection evidence only. |

## Reconciliation order and phase boundaries

1. Pin both catalogs and synthetic populated fixtures; resolve version collision
   and all eleven table/trigger contracts on a separately reviewed integration
   candidate. Keep fail-closed compatibility tests; do not merge blind.
2. Close **P4 relational-ready** evidence: complete scoped receivers/catalog,
   root/claim/value preservation, FTS composition, old/new-schema refusal and
   precommit/postcommit recovery on synthetic fixtures. Existing private29
   evidence is useful but does not cover main additions. This milestone is not
   permission to invoke the migration at runtime.
3. Hand that stable relational contract forward to **P5 physical CAS** (exclusive
   bytes, durable staging/GC/re-upload intents, digest quarantine) and **P6 auth**
   (membership/delegation/epoch fences), then P7/P8/P9 execution/transport/client
   lifecycle integration. P4 readiness must not depend on completing P5/P6 when
   those phases depend on P4; reserve their composition checks for final release.
4. After the complete candidate exists and separate copy authorization is given,
   follow [the recovery runbook](p4-migration-recovery.md): fence every DB/file/
   sidecar writer, take one encrypted coordinated generation, restore without
   egress/live roots, validate the exact catalog and bytes, rehearse interruptions
   and restoration, reconcile current security tombstones/revocations and unknown
   effects. Never restart ordinary services just to inspect the copy.
5. P10/final release review collects composition, load and authorized Amos-copy
   evidence. Public activation/credential cutover remains separately approved;
   no hosted execution without its separate VM-isolation gate.

## Phase-1 recovery truth

Preserved untracked `internal/storage/schema_recovery_regression_test.go`, SHA-256
`ee1b32ea93ef5aa546ca361f2d68d02de68320ceb96a6f4d8cb47add077ad6c6`.
`TestSchemaCompatibilityMainAdditionsBeforeMutation` checks constructors against
synthetic main29/30; `TestTenantMigrationRejectsMainSyncCatalog` checks v28 catalog
refusal. Neither imports main into private29. `TestTenantMigrationPostcommitExitReopen`
now covers an actual returned commit followed by subprocess `os.Exit(0)` with
committed WAL, no Close/cleanup, public-constructor refusal, private reopen and
two no-op validations. It compares committed catalog/rows and independent legacy
projections, controls/claim/sequences, roots/file identities and bytes, integrity
and FKs. The child helper skips in an ordinary top-level run by design. This is
not the precommit `published` checkpoint, a power-loss simulation, sidecar restore,
post-new-write rollback or real Amos evidence.

## Bounded graph preparation measurements

Parameterization retains capacities **16/32**, cold invalidation, uniform stride
**137** (coprime to 100/300/500), 20-tenant hot set and every-tenth cold selection.
Each fixture has one SQLite DB with 100/300/500 mapped FTS tenants; provisioning,
warmup, cleanup and post-timing GC are outside the per-operation timer. A timed
operation is graph acquisition, not search or HTTP. No providers or embeddings.

Hardware: Apple M4 Max, 14 logical CPUs, 36 GiB RAM, Darwin/arm64 macOS
26.3.1 (25D771280a), Go 1.25.0. Benchmark default GOMAXPROCS=14 (name suffix
`-14`); serial loop, not 14 request workers. Command from this P4 worktree:

```sh
CI=1 GOPROXY=off GOTOOLCHAIN=local go test ./internal/apiserver -run '^$' \
  -bench '^BenchmarkTenantGraphLoad$/^tenants(100|500)$/' \
  -benchtime=30x -count=1 -timeout=20m
```

Exit 0, **12/12 scenarios**, 30 timed acquisitions each, package elapsed
**856.318s** (includes expensive guarded fixture preparation). Exact reported
metrics, except ms columns rounded to three decimals:

| Tenants/cap/pattern | ms/op | construction ms/miss | builds | hit % | max live | retained B/graph | B/op | allocs/op |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| 100/16/cold | 99.398 | 99.381 | 30 | 0 | 16 | 257494 | 19449893 | 409033 |
| 100/16/uniform | 99.342 | 99.323 | 30 | 0 | 16 | 255144 | 19445107 | 409014 |
| 100/16/hot-set | 98.200 | 98.187 | 30 | 0 | 16 | 257042 | 19461472 | 409057 |
| 100/32/cold | 99.386 | 99.371 | 30 | 0 | 30 | 255127 | 19464295 | 408964 |
| 100/32/uniform | 99.562 | 99.549 | 30 | 0 | 30 | 255335 | 19466690 | 408964 |
| 100/32/hot-set | 69.860 | 99.786 | 21 | 30 | 21 | 255940 | 13633417 | 286343 |
| 500/16/cold | 3502.782 | 3502.761 | 30 | 0 | 16 | 257555 | 374607201 | 9242700 |
| 500/16/uniform | 3016.426 | 3016.404 | 30 | 0 | 16 | 253610 | 374605157 | 9242693 |
| 500/16/hot-set | 2696.908 | 2696.890 | 30 | 0 | 16 | 253966 | 374641355 | 9243123 |
| 500/32/cold | 2623.868 | 2623.852 | 30 | 0 | 30 | 255365 | 374557902 | 9242625 |
| 500/32/uniform | 2635.866 | 2635.847 | 30 | 0 | 30 | 254133 | 374615818 | 9242628 |
| 500/32/hot-set | 1806.910 | 2581.274 | 21 | 30 | 21 | 255501 | 262268541 | 6470190 |

**Limitations:** 300 is parameterized but not rerun; historical 300 results live
in [the graph performance handoff](p4-tenant-graph-performance-handoff.md). Thirty
operations do not traverse all tenants or reach capacity 32 on cold/uniform runs;
they do exercise capacity-16 eviction and capacity-32 hot reuse. The nominal
20-slot hot pattern replaces slots 0/10 with cold selections. Fixtures have empty
content/FTS, not a populated search corpus. This is one sequential workstation
sample, no repetition/confidence intervals, controlled thermal state, CPU/allocation
profile, RSS/peak sampling, latency histogram or disk/WAL measurement. No concurrent
verification jobs were launched by this session during the benchmark; other host
load was not controlled. Construction accounts for nearly all measured miss time;
this does not independently prove its internal allocation attribution. Retained
heap after GC is not allocation traffic or peak memory. **NOT 100 req/s search
acceptance**, full load, hybrid V10/V11 or real AMOS acceptance.

## Fresh local verification

All commands use `CI=1 GOPROXY=off GOTOOLCHAIN=local GOMAXPROCS=2` (except
the benchmark above); no dependency downloads or external dataset were used.

```sh
go test ./... -count=1 -timeout=15m
go test ./internal/storage -run '^(TestSchemaCompatibilityMainAdditionsBeforeMutation|TestTenantMigrationRejectsMainSyncCatalog|TestTenantMigrationPostcommitExitReopen)$' -count=1 -v
go test -race ./internal/apiserver -run '^TestTenantAcceptanceConcurrentHTTP$' -count=1 -timeout=5m
go vet ./...
go build ./...
golangci-lint run ./internal/apiserver/... ./internal/storage/...
git diff --check
```

Focused recovery: **3/3 top-level + 4/4 constructor subtests passed**, 0.868s.
Concurrent HTTP including the four new sealed routes: race run **passed**, 6.332s,
no reported race. Vet/build/diff checks exited 0; scoped lint: **0 issues**.
Full Go suite: **37/37 packages passed**, no reported failures (storage 64.058s,
apiserver 5.855s). Full-suite output is package-level, not an individual test count;
opt-in/helper and unsupplied historical-base skips are not claimed as exercised.
Full repository race, web checks, aggregate `just check`, hosted CI and the load/
external-copy ledger were not run. Final hashes of both preserved files match
the values above; only two apiserver test files and three P4 docs changed.

## Remaining acceptance ledger — all open unless narrowly stated above

| Gate | Missing evidence / owner |
|---|---|
| Integrity/FK/value preservation | P4 integrated main-additions catalog plus independent full-row/sequence/trigger/control checks before/after commit, reopen, rollback and coordinated restore; synthetic current-P4 tests are not this successor proof. |
| Timing/capacity | Full candidate migration and restart/RPO/RTO; continuously sampled allocated disk, DB/WAL/SHM/temp/sidecar peaks, checkpoints and long-reader writer-lock waits, per-volume free space. Boundary file lengths are not peaks. |
| External coordinated copy | Separately authorized real Amos 78,952-note/~1.2 GB copy, fresh titles/triggers, all credentials/claim/roots/local bytes, sidecars and security journal; none accessed here. |
| Concurrent graph/hybrid | 100/500 populated tenants, simultaneous misses, churn/rebuild/delete, hot-tenant interference, peak RSS/heap/GC and cancellation/fairness; lexical AND hybrid A score/order/count/snippet invariance under changed B. Full V10/V11 remains unclaimed. |
| Search latency | Sustained **100 aggregate requests/s**, **p95 <500 ms**, actual HTTP/search plus concurrent writers, documented corpus/vectors/hardware, errors and latency distributions. Serial graph ns/op does not establish any part of that SLO. |
| Physical/auth release | P5 CAS intents and P6/P8 lifecycle/revocation/effect/output fences, P9 client/cache and tenant export/erase, protected sidecar recovery. SQL readiness alone does not close these. |
