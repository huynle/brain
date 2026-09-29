# P4.9 phase 3 — acceptance evidence and limits

Base `ce103b162a1cc29e228f329be2b2f489881f1f60`, with preserved uncommitted phases
1/2 plus phase-3 test/document additions. Measured 2026-09-12 on Darwin arm64,
Apple M4 Max, Go 1.25.0, benchmark GOMAXPROCS=14. No production database, external paid
API, SDK implementation, Brain mutation, commit, deployment or activation.

## Fixture and assertions

`tenant_acceptance_test.go` opens **one real SQLite file/pool**, binds a v28
owner, and invokes the private storage migration through a test-only subprocess
(`tenant_graph_fixture_test.go`). This follows the existing migratedNoteStores
pattern: bind before staging, execution observes published schema 29. The child
refuses nonempty note fixtures/non-v28 files and restores the real FTS mapping
triggers after test-only provisioning. There is no new production migration
entry point, version override, mock repository or per-tenant database. Public
constructors and `CurrentSchemaVersion=28` are unchanged.

- `TestTenantAcceptanceConcurrentHTTP`: two loopback HTTP servers bind explicit
  trusted contexts to the same graph middleware/manager (fixture authority, NOT
  P6 authentication). Eight concurrent clients, three rounds with forced graph
  invalidation/reconstruction, 14 observations per client per round: **336 HTTP
  responses**, six real graph builds before unsupported-route probes. A forged
  `X-Brain-Tenant` header cannot change the listener's context. Both tenants reuse
  `shared`, short IDs `same0001`/`same0002`, and identical entry/target paths.
  Positive GET/path/ID/list/FTS/backlink/outlink/attachment metadata/reference/raw/
  derived reads must contain the own marker; all reject foreign markers. Foreign
  attachment metadata/content/text IDs return 404. Physical integer note and
  attachment IDs are globally allocated by this schema: they cannot collide;
  logical IDs/paths and content digests do collide. Claims to the contrary would
  be false fixture evidence.
- Unsupported POST entry writes, bulk, uploads/extraction, Assistant jobs/voice,
  push, tasks, config, tokens and MCP return 501; PATCH/PUT/DELETE entry,
  attachment and config probes also return 501, and note counts stay unchanged.
  Existing `TestTenantHTTPRealFactoryAndUnsupportedSurfaces` separately covers
  GET/POST task/control/identity/event/operator exclusions and mismatched handles.
  This is not CRUD acceptance for the deliberately unavailable write surfaces.
- `TestTenantAcceptanceLegacyParentAndReusedPaths`: legacy local owns the parent
  root; new tenant is beneath `tenants/tenant-001`. Direct foreign IndexFile and
  Recall are denied. Real local incremental/rebuild scans retain two local notes,
  not the foreign subtree. Delete/recreate the same local logical ID/path; fresh
  graph reads replacement content, while the foreign graph/index/file stays
  unchanged. Equal bytes occupy the preserved legacy two-shard CAS and distinct
  new-tenant CAS. Removing local bytes does not affect the foreign HTTP download.
- `TestTenantAcceptanceHeldHTTPAndSuspension`: block a real response Write after
  real handler/service work. At capacity 1, invalidation retains the leased retiring
  graph and admits no second construction before drain. Releasing output allows
  the other graph to build without closing the shared pool. SQLite tenant status
  is checked at admission; suspension denies with 503 and no content, reactivation
  supplies a new test lifecycle generation and builds a fresh graph. This tests
  lifecycle behavior, **not** final P6 400/401/403 protocol/authentication.
- `TestTenantSingleBootOneGraphAndCompatibility`: AST checks exactly one direct
  graph construction in buildHTTPHandler, no cache or closure-based construction;
  full production single router supports a write and 100 repeat reads without new
  tenant headers, preserves markdown/DB paths and returns content after restart.
  Existing configured-auth/bootstrap/attachment tests also run in the full suite.

## Lifecycle limitations (do not promote these into guarantees)

The held-output test intentionally demonstrates draining, not retroactive output
revocation: data read before invalidation can still reach the writer. P6 must
serialize final output/effect/commit authority checks with revocation. Cancellation
alone is not that fence. Test lifecycle generations are in-memory; durable online
principal/grant epochs, absent-cache invalidation, stream per-frame checks and
five-second idle closure remain P6/P8. Hijacked streams and detached runtime
jobs are not exposed in the read-only graph router. A blocked writer/provider
can hold a slot indefinitely; no bounded eviction latency is claimed. Shutdown
must drain before the owner closes shared SQLite. Quiet-tenant maintenance is
not implemented by the LRU; see the resource audit's explicit P8 ownership.

## Repeatable construction benchmarks

```sh
CI=1 go test ./internal/apiserver -run '^$' \
  -bench '^BenchmarkTenantGraphLoad$' -benchtime=300x -count=1 -timeout=45m
CI=1 go test ./internal/apiserver -run '^$' \
  -bench '^BenchmarkTenantSingleConstruction$' -benchtime=10x -count=1
```

Graph load provisions 300 actual tenant identities, FTS mappings/tables and nested
filesystem roots in shared SQLite, with empty content and providers disabled.
No root resolver/graph/service mock. Provisioning and a one-graph initialization
warmup are outside timing. Every miss constructs the whole real HTTP/service
graph and validates authoritative roots. Every hit uses manager Acquire. Capacity
counts include retiring/closing graphs, not just reusable entries. Cold forces
invalidation; uniform is a deterministic stride-137 permutation of 300 identities
(one visit each at 300x; this is **not** IID random traffic). Hot-set visits 18 hot
IDs interspersed with 30 cold IDs over 300 operations. Construction is timed inside
the factory; `B/op`/allocs include manager acquisition overhead. Retained bytes are
post-GC heap delta divided by resident graphs after one initialization warmup,
not RSS, peak allocation, populated-hub memory, SQLite disk or a hard quota.
The benchmark's lifecycle callback is the constant active test permit; it does
not measure P6 online identity authorization. Suspension tests separately use
the persisted tenant status. Hits measure acquisition, **not** complete HTTP
reads at 300 tenants: per-operation filesystem policy checks still validate all
root mappings even with a warm graph. The two-tenant HTTP tests cannot establish
the approved 100/500-tenant search/writer-fairness gate.

An initial run with an unnecessarily large 300-graph warmup exceeded the outer
command timeout after its first result. It was not a completed benchmark suite.
It measured cap-16 cold at 828.334 ms construction, 140,751,016 B/op,
3,386,067 allocs/op, 254,528 retained B/graph, 0% hits and 16 max-live. The retained
warmup has been reduced to one graph; final results below supersede that probe.

Final run: **PASS**, 1393.625 s including fixture setup and benchmark harness warmups.
Each table row is 300 measured acquisitions. All capacities include building,
leased, retiring and closing slots.

| Cap / traffic | ns/acquire | Builds | construct ns/miss | Hit % | Max live | Retained B/graph | Allocated B/acquire | Allocs/acquire |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 16 / cold | 950868794 | 300 | 950843770 | 0 | 16 | 262734 | 140767658 | 3386139 |
| 16 / uniform | 819928974 | 300 | 819906285 | 0 | 16 | 256058 | 140753929 | 3386067 |
| 16 / hot-set | 847079344 | 300 | 847055890 | 0 | 16 | 253780 | 140755075 | 3386096 |
| 32 / cold | 826691590 | 300 | 826666452 | 0 | 32 | 254826 | 140703904 | 3386060 |
| 32 / uniform | 823323558 | 300 | 823299064 | 0 | 32 | 253888 | 140713387 | 3386060 |
| 32 / hot-set | 130076942 | 48 | 812954592 | 84 | 32 | 255178 | 22517121 | 541785 |

**Capacity choice: 32 for the staged graph-cache composition**, approximately
7.8 MiB idle retained graph heap at the hot-set observation, rather than 16
(approximately 3.9 MiB but thrashing on this 18-hot-identity workload). This is an
evidence-based starting choice, not a universal optimum or a production setting:
the internal manager still requires an explicit capacity and single mode does
not use it. No automatic cache default or multi-mode activation was added.
Larger caps cannot eliminate cold misses and cannot bound populated coordination
state, blocked leases, provider buffers or database/index memory.

**Scalability limitation discovered, not fixed by inflating capacity:** cold
construction takes 0.81–0.95 seconds and allocates roughly 140.7 MB per miss with 300
registered roots. Source inspection shows `tenantfs.Resolver.snapshot/validate`
re-reading/canonicalizing every root and comparing every root pair (quadratic),
invoked repeatedly by graph binding/CAS path validation. This is a source-backed
suspect, not a CPU-profile attribution percentage. P5/P10 must design and verify
an optimization preserving authoritative mapping and drift checks, then measure
real 300/500-tenant HTTP throughput/contention. Do not remove security checks or
cache stale root authority to produce favorable numbers. High transient churn
and continued per-file policy validation make this **not** a demonstrated
production-scalable 300-tenant server, despite bounded idle graph count and an 84%
hot-set acquisition hit rate. The approved 500 ms-p95 search/fairness gate remains
unverified here; acquisition microbenchmarks cannot satisfy it.

Single-mode extraction baseline (10 iterations): graph-only **0.377750 ms**, 302,148
B/op, 840 allocs/op; existing-empty full boot-ready composition **3.005087 ms**,
1,034,926 B/op, 9,225 allocs/op. The graph is about 12.6% of that measured boot-ready
time; this ratio is not a measured regression. Whole boot includes open/schema/
root/router/worker admission, excludes joined cleanup, and launches the scan
asynchronously; concurrent worker allocations can appear in Go's process-wide
allocation accounting. A fresh-empty functional boot probe was 17.056 ms. Immediate
benchmark shutdown can log expected context-cancelled first-sweep warnings.
That historical component comparison did not establish unchanged startup. The
matched pre-extraction comparison below closes that specific evidence gap; neither
measurement establishes large-tree startup, socket/TLS readiness or production SLOs.

## Matched pre-extraction single-mode boot comparison

Measured 2026-09-12: baseline detached **ce103b162a1cc29e228f329be2b2f489881f1f60**
versus the current P4 worktree at that HEAD with its preserved phase 1–3 edits.
No production source was changed for this comparison. Apple M4 Max, 36 GiB RAM,
Darwin arm64, Go 1.25.0, GOMAXPROCS=14. Both test binaries were compiled before
measurement; no test/build commands from this session overlapped sampling. This
remains a shared development workstation, not isolated hardware.

`single_boot_comparison_test.go` is overlaid **byte-identically** into both
checkouts (SHA-256
`0efca599e05583c5277bc69fcaf818acf6dea382152fce59a3a3e423bb6d2c98`). The inspected old
and new signature is `buildHTTPHandler(context.Context, ServerOptions)
(http.Handler, string, func(), error)`. Timing starts immediately before that call
and stops immediately after return. Both sides include the entire single-mode
storage/schema/root/service/router/worker-admission composition. Both exclude
fixture setup, health validation, cancellation, cleanup, process startup and
compilation. Both launch the boot scan asynchronously; neither waits for indexing
completion. This is **whole-versus-whole boot-ready composition**, not a graph-only
comparison or full network listener startup.

Fixtures use the same explicit options: loopback host, single tenancy, auth off,
watcher off, embedding/extraction/Assistant providers off, feature checkout off,
otherwise zero-value ServerOptions. Each process has the same scrubbed environment
(PATH, temporary HOME/TMPDIR, CI=1, GOMAXPROCS=14 only), no inherited credentials or
user config. Each sample uses a new temporary root on the same filesystem:

- **fresh-empty:** no DB; schema/bootstrap work is timed.
- **existing-empty:** initialize real v28 storage and the local Brain/CAS mapping
  outside timing, close storage, then time handler assembly against that initialized
  empty DB. No previous handler or workers are used to seed this fixture. Logical
  fixtures/configuration match; temporary absolute paths necessarily differ.

For each fixture: two predefined unrecorded warmup pairs, then **30 pairs** in
alternating baseline/current and current/baseline order. Every observation runs
`-test.benchtime=1x -test.count=1` in a **fresh process**, avoiding old unjoined
workers contaminating later samples. All 120 measured observations are retained;
no outlier trimming. This warms OS caches, not a claim of cold-disk performance.
The benchmark checks the expected legacy DB path and HTTP 200 from
`/api/v1/health` outside timing on every run.

**Tolerance declared before sampling:** meaningful regression requires a median
increase exceeding **both 10% and 0.5 ms**, with paired 95% bootstrap CI above zero.
The absolute floor avoids escalating sub-millisecond workstation jitter, while
the relative threshold catches material proportional changes. This is a local
engineering acceptance tolerance, not a previously approved production SLO.
CI uses 10,000 pair-resampled differences of medians, fixed seed 49, percentile
endpoints; p95 below is the nearest-rank sample statistic (29th of 30), not a
high-confidence tail SLO.

| Fixture | Baseline median ms | Current median ms | Delta | 95% CI delta ms | Baseline / current p95 ms |
| --- | ---: | ---: | ---: | --- | --- |
| fresh-empty | 10.358542 | 10.343000 | -0.1500% | [-0.284688, +0.157375] | 14.810375 / 11.261875 |
| existing-empty | 3.181687 | 3.117375 | -2.0213% | [-0.169772, +0.036209] | 3.494834 / 3.459583 |

| Fixture / version | Min ms | Max ms | Median B/op | Median allocs/op |
| --- | ---: | ---: | ---: | ---: |
| fresh / baseline | 9.491125 | 18.316917 | 1165760 | 10550 |
| fresh / current | 9.794584 | 12.216708 | 1126824 | 10128.5 |
| existing / baseline | 2.936834 | 3.598875 | 1131108 | 9866.5 |
| existing / current | 2.869500 | 3.493833 | 1092320 | 9472.5 |

Allocation counters include concurrent startup work scheduled within the timed
window, not total eventual worker allocation or retained memory; fractional
medians are the averages of the middle observations. Do not infer a memory
optimization from these counters.

All measured startup times, **integer nanoseconds**, paired in sampling order:

| Pair | Fresh baseline | Fresh current | Existing baseline | Existing current |
| ---: | ---: | ---: | ---: | ---: |
| 0 | 10279083 | 9830792 | 3090208 | 3418917 |
| 1 | 10221292 | 10093250 | 3494834 | 3411959 |
| 2 | 10008084 | 10166958 | 3162208 | 3335792 |
| 3 | 10265000 | 10069750 | 2936834 | 2959750 |
| 4 | 9491125 | 10121792 | 3186292 | 3064959 |
| 5 | 10465750 | 10567959 | 3153792 | 3145750 |
| 6 | 10470208 | 11261875 | 3121667 | 3169125 |
| 7 | 10213583 | 10841042 | 3419167 | 3039167 |
| 8 | 11292291 | 10718084 | 2962625 | 3493833 |
| 9 | 10454709 | 10131625 | 3313792 | 3096000 |
| 10 | 10297250 | 10989542 | 3329791 | 3131125 |
| 11 | 10551708 | 10355916 | 3240834 | 2979875 |
| 12 | 9910208 | 10219958 | 3220459 | 3103625 |
| 13 | 10208250 | 10372667 | 3161791 | 3346709 |
| 14 | 10454833 | 9794584 | 3093958 | 3338625 |
| 15 | 11830916 | 12216708 | 3598875 | 3060875 |
| 16 | 10257833 | 11073417 | 3355166 | 3033375 |
| 17 | 14810375 | 10454625 | 3187500 | 3003917 |
| 18 | 10452875 | 10184250 | 3003625 | 3171333 |
| 19 | 10599709 | 10666792 | 3118833 | 2920292 |
| 20 | 18316917 | 10707417 | 3179708 | 3169583 |
| 21 | 10135083 | 10914042 | 3159417 | 3198000 |
| 22 | 10501125 | 10081833 | 3256833 | 3075834 |
| 23 | 10176917 | 10387625 | 3313750 | 2935958 |
| 24 | 10271334 | 10387334 | 2999750 | 2982708 |
| 25 | 10559125 | 9924500 | 3258292 | 2869500 |
| 26 | 10017750 | 10330084 | 3254417 | 3459583 |
| 27 | 10277208 | 10180458 | 3183666 | 3253541 |
| 28 | 10419833 | 9967625 | 3061583 | 3259875 |
| 29 | 10816000 | 10066917 | 2937917 | 3003500 |

**Conclusion:** no meaningful single-mode boot regression in either matched
fixture; current medians are within the declared tolerance and both CIs cross
zero. This closes the P4.9 pre-extraction startup-comparison gap, without claiming
exact equality or a statistically established speedup. Large-tree indexing,
authenticated production deployments and socket/TLS startup remain outside this
experiment. The separately documented 300-root cold-graph cost is unchanged and
is not being treated as a P4.9 boot SLO blocker or optimized here.

Reproduce from the current worktree (verify parent first; use new output paths):

```sh
scratch=/var/folders/x0/k6yjvm7d53l79z35btbzpg100000gn/T/opencode
ls "$scratch"
git worktree add --detach "$scratch/p49-boot-baseline" ce103b16
python3 scripts/compare-single-boot.py \
  "$scratch/p49-boot-baseline" "$scratch/p49-boot-results-complete"
```

For this run those paths already exist: reuse the detached baseline and choose a
new output directory. The driver refuses overwriting results and does not modify
either checkout: Go overlays inject the current benchmark source into both builds.
`p49-boot-results-complete/results.json` retains all per-sample time/allocation
counters, source hash and summaries; adjacent text files retain all 128 successful
process outputs (including eight warmups). Binaries/overlays are retained there.
Earlier harness attempts are not final evidence: one failed its post-timer health
check at the wrong `/health` route; a second completed fresh samples but failed
existing-fixture setup because `.brain-data` had not been created before direct
storage open. Both were harness-only corrections; the final complete run restarted
all sampling, rather than combining selected portions of failed runs.

Fresh post-change verification: corrected focused race command below **PASS**, three
repetitions including `TestGraphManager`, 28.368 s; `CI=1 go test ./... -count=1`
**36 passing packages**, one no-test package, no failed packages (apiserver 8.471 s).
`go build ./...`, `go vet ./internal/apiserver`, and `git diff --check` exited 0;
`golangci-lint run ./internal/apiserver/...` reported **0 issues**. No production
source changes, commits, Brain mutations or main SDK documentation edits.

## Verification

```sh
CI=1 go test -race ./internal/apiserver \
  -run 'TestTenant(Acceptance|Single|Graph|HTTP)|TestGraphManager' -count=3 -timeout=5m
CI=1 go test ./... -count=1
go build ./...
go vet ./...
golangci-lint run ./internal/apiserver/... ./internal/storage/...
git diff --check
```

Historical phase-3 commands exited 0 (the original focused regex omitted
`TestGraphManager`; the corrected command was rerun above). Historical focused
race suite: 41.948 s. A fresh JSON-counted full
run (`CI=1 go test -json ./... -count=1`) reported 8,088 test/subtest passes,
8 skips, 36 passing packages, 1 no-tests package, 0 failures. The ordinary package
output was also read in full; lint: 0 issues. Additionally,
`CI=1 go test -race ./internal/apiserver ./internal/service ./internal/storage
-count=1 -timeout=10m` passed all three complete suites: 18.487 s / 190.697 s / 535.793 s.
Benchmark and verification work overlapped on this development workstation;
reported timings are observations under that load, not isolated-hardware SLOs.
After the final denial/path assertion additions, the four new acceptance tests
also passed under race three times (`-run '^TestTenant(Acceptance|Single)'`), 25.247 s;
focused lint again reported 0 issues.
No browser/SDK runtime changes were made; no new browser acceptance is claimed.
The test subprocess staging migration is not race-instrumented by the parent's
`-race`; real HTTP service/cache/SQLite access in the parent is instrumented.

## Acceptance disposition / downstream gates

P4.9 read-only scoped graph construction, real shared-persistence concurrent HTTP,
lease/eviction, staged lifecycle, nested legacy-root and single-mode compatibility
have concrete tests. This does not waive P4.10 un-embedding, remaining receiver
migration, P4.11 consolidated isolation or final atomic coordinated AMOS-copy
migration/recovery. No AMOS-copy rehearsal was attempted here. Newer-main Assistant
jobs/voice/push/offline/bulk/sidecar ownership is handed off explicitly to
P4.10/P6/P7/P8/P9/P10 in [the resource audit](p4-tenant-graph-resources.md).
SDK interfaces cannot supply server authority. D01–D13/V01–V16 public-readiness,
paid quotas, auth/stream fences, erasure/restore and activation are not complete.
