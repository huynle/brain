# P4 migration and recovery runbook — dormant component

Assignment `jr1xs3a3`, phase 3, 2026-09-07. **Pending v29 NOT ENABLED.**
`CurrentSchemaVersion = 28`. No CLI migration command, runtime call, version bump,
public activation, or production operation is introduced. This is a preparation
and isolated rehearsal procedure, not authorization to migrate a deployment.
See [approved contracts](multi-tenant-security-contracts.md) D07/D08/D11 and the
[refreshed inventory](multi-tenant-ownership-inventory.md).

## Stop/go prerequisites

**P4.3 Phase 2 update (2026-09-07):** the private outer owner described below
now exists as `migrateTenantSchema(ctx, db, checkpoint)` in
`internal/storage/schema_tenant_migration.go`. It is dormant and has **no runtime
caller**. The older component measurements below remain historical evidence;
the complete-composition evidence and exact handoff are recorded at the end.

**STOP for deployment today.** Finalized P0 and complete P3 are required; so are
the downstream tenant FTS map/rank isolation, tenant CAS metadata/root handling,
all scoped receivers (including raw-handle removal), and operator install-claim
reader/writer routing. One future outer owner must validate and atomically commit
the relational changes, FTS mapping, CAS metadata, receiver-routing selection,
root validation and claim cutover, then publish the schema version last in that
same transaction. Tenant FTS is **P4.3**, not P4.2. There must be no externally
visible intermediate state. Filesystem publication is not a SQLite transaction: retain legacy bytes in
place and reconcile any future durable blob intents before serving. No global FTS
fallback. This component alone must **never be committed in a deployment**.

`stageTenantRelationalSchema(*sql.Tx)` requires v28 and FK OFF before the outer
transaction. It owns only a savepoint; no pool, commit, flag or version stamp.
It rebuilds 26 workload tables, adds four registry/reference/claim tables,
validates counts, ownership, definitions, integrity and FKs, and leaves old FTS
for replacement in that SAME transaction. Repetition validates staged structure;
it is not a supported restart of a partially committed migration.

P4.2 `ly1dgyw8` Phase 2/2 adds independent acceptance evidence, not activation.
P4.1 already covered all seven project-bearing PKs: `task_claims`,
`task_dispatch_leases`, `feature_assignments`, `feature_pause_state`,
`feature_cascade_roots`, `project_pause_state`, `project_placement`. There are 13
literal `project_id` workload tables versus 26 total workload tables; see the
inventory for the ordered keys and complete distinction. Catalog/PRAGMA tests
verify their two-tenant `brain` coexistence and same-tenant duplicate rejection,
full named-index uniqueness/ordered columns/predicates and implicit unique keys.

Unknown tenant indexes fail closed, including simple lookup/unique/partial indexes,
until their definitions are reviewed into the manifest; do not drop them to force
an operator copy through validation. The composite chunk FK from
`note_embeddings_meta(tenant_id,note_id,chunk_index)` to
`note_embeddings(tenant_id,note_id,chunk_index)` uses `ON DELETE CASCADE` in addition
to the note FK. Legacy orphan chunk metadata is a validation failure, not data to
silently discard. Actual composite note/attachment/webhook FKs and durable
runner/client reference-key FKs do not establish active enrollment or authorization.
Derived/history project/task/feature/path references still require later scoped
receiver validation; no fictional parent rows are introduced to satisfy them.

## Prepare a coordinated recovery set (operator checklist, not executed here)

1. Keep public ingress/signup disabled. Fence **all writers**, not only runner
   pause dials: stop API/HTTP MCP and stdio/direct DB clients, runners/agents,
   schedulers, automations, goals/reminders, webhook/trigger dispatch, claim/lifecycle
   cleanup, extraction/embedding workers, boot indexers and watchers; drain in-flight
   writes. Stop git sync, filesystem editors, backup jobs that mutate source state,
   and offline token/doctor utilities. Verify process/service inventory and no
   remaining DB/file writers. Pausing dispatch does not stop these actors.
2. Record binary revision/hash, effective configuration and working directory.
   Record database location and each original **lexical, absolute and canonical**
   BrainDir/CAS root, symlink targets, layout, ownership and permissions. Preserve
   the independent blob root and `tenant_roots` values, including relative lexical
   strings; do not reinterpret them from the rehearsal cwd. Capture a checksummed
   markdown/blob manifest with counts/bytes, including authoritative non-content
   metadata. Legacy local paths/bytes must not move. A root_override string is
   not a substitute for the durable mapping.
3. While fenced, create an encrypted **SQLite online-backup API / sqlite3 .backup**
   snapshot and coordinated markdown/blob filesystem snapshot. Do not copy only
   the main .db of a WAL database or use immutable=1 to ignore committed WAL.
   If taking a raw filesystem DB snapshot instead, snapshot DB/WAL/SHM consistently
   with all handles closed or using a proven atomic volume snapshot. Never mix
   sidecars from different generations. Record snapshot ID/time/checksums.
4. Include protected credential metadata (API token revocations, OAuth clients,
   codes/access/refresh records, install claim, configured-password metadata),
   root registry, and any available current revocation/deletion journal in the
   same recovery record. Do not put secrets into logs or this runbook. Validate
   encryption-key recovery and restrict snapshot access. A DB is partly derived;
   reindexing markdown cannot reconstruct authoritative credentials/roots/claim.
5. Plan disk for source + backup + isolated copy + old/new relational tables and
   indexes + downstream FTS rebuild + retained WAL + SQLite temp spills + blob
   staging, with operating-system headroom. Measure free space on **each** involved
   volume. Long readers pin WAL; rollback does not necessarily shrink it. Sample
   DB/WAL/SHM/temp and total allocation throughout a full rehearsal. No fixed
   multiplier inferred from the small synthetic run is a capacity guarantee.

## Isolated copy only

Use a disposable host/VM with no live source mounts, network egress, runners,
credentials usable against production, or background services. Restore copies of
the original filesystem namespace there when testing persisted absolute/canonical
roots; retain original mapping evidence separately. Do not rewrite roots merely
to make a startup test pass or launch the normal server against a copied DB whose
absolute roots still point to live directories. Root relocation needs its own
reviewed procedure. Never point the tests below at Amos; they accept no data path.

On the isolated v28 copy, record baseline per-table counts/catalog/sequences,
credential/claim/root equality and file hashes. SQLite checks (offline copy only):

```sql
SELECT max(version) FROM schema_version; -- 28
PRAGMA integrity_check;                 -- exactly ok
PRAGMA foreign_key_check;               -- zero rows
SELECT * FROM tenant_roots;             -- protected operator record, not public log
```

The future migration owner must reserve one connection, disable FK enforcement
before BEGIN, verify it is off, and keep all components in that outer transaction.
Before commit check every classified table, zero null/empty owners, preserved IDs
and sequences, composite FK correctness, relocated permanent claim equality,
tenant FTS counts/content/ranking, CAS references/hashes/layout and control routing.
After commit or rollback restore FK ON on that connection and verify `PRAGMA
foreign_keys` returns 1; check integrity and foreign_key_check again after reopen.
Do not release the traffic fence until restart and complete local compatibility
and equal-ID tenant denial fixtures pass with the exact candidate binary.

## Interruption, restart, rollback boundary

- Before outer commit: terminate only the rehearsal process, retain its complete
  DB/sidecar set, reopen with SQLite recovery, and verify the entire old catalog,
  rows, version, roots and claim. Retry the whole outer migration from validated
  v28; do not manually promote `p4_new_*` tables or stamp v29. A failure to validate
  keeps the copy sealed. Save forensic artifacts before discarding it.
- After future atomic commit: restart only a binary implementing the complete
  cutover. Test interruptions at **every** downstream stage and the commit boundary;
  this phase tests only after relational staging, before commit, including a
  simulated downstream SQL failure and full outer rollback. Unexpected mixed
  version/catalog state is a blocker, not permission to repair by version editing.
- Returning to an old binary after commit requires a coordinated pre-cutover
  DB/files/blob/config recovery set in quarantine, not just deleting the version
  row or restoring the main DB. Once new writes occur, rollback loses them; agree
  the boundary/RPO before the window. Replay current security revocations/deletion
  tombstones, suppress stale jobs and reconcile paid effects before exposure.
  Missing trustworthy journal means stay sealed and controlled credential recovery;
  never reactivate snapshot credentials. The full D11 restore tooling is not here.
- **Old binaries cannot retroactively refuse a newer schema.** Only binaries with
  the phase 1 checks refuse future/malformed versions at storage entry points.
  Pin/disable old services and executables operationally. This guard is not a
  concurrent migration lock. `buildHTTPHandler` calls `config.MigrateDataDir` and
  mkdir before storage.New: earlier server directory migration is explicitly
  **outside phase 1's no-mutation scope**. Never probe recovery with normal server
  startup and assume it is read-only.

## Reproducible component evidence

Commands from this worktree (no services or external datasets):

```sh
CI=1 go test ./internal/storage -run '^TestTenantRelationalInterruptedRestart$' -count=1 -v
CI=1 go test ./internal/storage -run '^TestTenantRelationalInterruptedRestart$' -count=3 -v
CI=1 go test ./internal/storage -run '^TestTenantRelational(ProjectKeys|ForeignKeyInventory|EveryOwnershipAndIndex|DownstreamFailureRollsBackOuter)$' -count=1 -v
BRAIN_P4_SYNTHETIC_SCALE=1 CI=1 go test ./internal/storage -run '^$' -bench '^BenchmarkTenantRelational78952$' -benchtime=1x -count=1 -timeout=10m
CI=1 go test ./... -count=1
go vet ./...
go build ./...
just lint
```

Measured on Darwin/arm64, Apple M4 Max, Go 1.25.0, integrated HEAD
`b04952c51bc13c53689a73469066b4d7f8da882b` plus phase 1/2/3 worktree edits:

| Rehearsal | Observation |
|---|---|
| File-backed subprocess | Readiness after 50.692375 ms, then parent SIGKILL; tiny cache forces uncommitted WAL spill. DB 544,768 B; WAL 733,392 B; SHM 32,768 B. Test passed in 0.15 s. |
| Reopen/retry | Every baseline ordinary-table row, catalog and sequence matched; component staging twice and rollback succeeded; version remained 28, FK restored. Post-retry DB/WAL/SHM sizes unchanged. No committed component or server route tested. |
| Repetition | Three further SIGKILL/restart runs passed (0.16/0.14/0.16 s); readiness 51.008375/50.594750/71.585083 ms, identical boundary file sizes. |
| Opt-in 78,952-note synthetic fixture | 100 projects, ~1.3 KB repetitive bodies, JSON metadata, 78,952 tags, 7,895 links and 1,536-byte embedding rows plus metadata, 789 attachment/reference/derived rows, permanent claim. No actual markdown/blob files or real vectors/providers. |
| Baseline sizes | DB 221,925,376 B; WAL 0 B; SHM 0 B after seed connection closes. |
| Staging + internal validation | 2.195033167 s (benchmark 2,195,053,959 ns/op); DB 221,925,376 B; WAL 204,096,592 B; SHM 425,984 B at staged boundary and after rollback. Total observed files 426,447,952 B. |

Sizes are logical file lengths at named boundaries, **not continuously sampled
peak allocation**, temp-space peaks, or a committed/checkpointed final schema.
Benchmark excludes fixture construction, commit, FTS rebuild, CAS, receiver routing
and restart. It is not the 1.2 GB Amos database and provides no extrapolated Amos
timing/capacity guarantee or 100/500-tenant ranking/load evidence. Full Go suite:
36 packages passed, one package had no tests; vet/build exited 0;
`just lint` reported `0 issues.` The historical
ratchet requires its separately supplied trusted base; no base was supplied here.

**Final post-P4.11 gate: authorized Amos-copy and coordinated recovery evidence.**
That gate is not performed or required to be executed in P4.2 Phase 2/2. No live
Amos access was attempted and no authorized coordinated snapshot was used here.
The new downstream-failure test stages the component, writes a disposable probe
table and mutates a note (including legacy FTS maintenance), forces a CHECK failure,
then rolls back the outer transaction. It compares every workload/control table,
catalog, sequences and FTS shadows, verifies version 28, restores FK enforcement
and checks integrity/FKs. This is simulated downstream evidence, not execution of
the future FTS/CAS/runtime/root/claim cutover or a post-commit restore rehearsal.
Full V01/V03/V10/V15 remain pending: relational
constraints and one precommit crash point are component evidence only, not local
workflow parity, complete tenant isolation, every-stage cutover recovery, or
revocation-safe restore/RPO/RTO proof. Public activation remains separately gated.

## P4.3 Phase 2 — dormant complete transaction and handoff

The private owner reserves **one `sql.Conn`**, disables and verifies foreign keys
before beginning its transaction, then obtains SQLite's writer reservation before
reading source version/catalog/rows. No pool-backed service or root-repository
method is called while the transaction holds the only connection. The sequence is:

1. Audit the exact reviewed v28 workload and control catalog, refusing partial
   tenant schema under v28. Control audit explicitly accepts the existing v4
   no-FK OAuth rebuild definitions as well as their bootstrap definitions, not
   arbitrary historical DDL. Unknown tables/indexes/triggers/views fail closed.
2. Read durable roots through a transaction-bound, read-only `tenantfs.Repository`;
   use `Resolver.Lookup` and `BlobPath` for drift, exclusion and layout policy.
   Require existing directory roots and verify every referenced CAS object's
   regular-file status, length and SHA-256. Never provision/create directories,
   relocate roots, repair metadata or move local bytes. Source root configurations
   that cannot map to the resulting tenant registry fail closed rather than
   inventing tenants or silently dropping registrations.
   Regularity is checked **before opening** each resolved CAS path, then checked
   again on the opened descriptor with `os.SameFile` identity comparison. This
   rejects existing FIFOs/devices without a potentially blocking open. It is not
   atomic: the external writer fence must cover CAS files and every ancestor
   throughout validation. A concurrent regular-file-to-FIFO swap between stat
   and open can still block; Go context cancellation cannot interrupt that open.
   Neither descriptor checks nor SQLite's writer reservation replace the
   filesystem fence. Hash/length checks still validate the bytes actually read.
3. Capture transaction-local TEMP multiset snapshots of every workload table's
   original columns, sequences, all credential/control rows, root mappings and
   every column of the permanent installation claim. TEMP snapshots may spill;
   include their disk needs in rehearsal capacity planning.
4. Run `stageTenantRelationalSchema(tx)`, then `stageTenantFTS(tx)` in this same
   transaction. Retain all IDs and high-water marks. For an originally absent
   sequence only, remove rebuild-created zero rows for still-empty reviewed
   tables; do not synthesize/reset an allocated sequence.
5. Revalidate roots/CAS, the complete final relational/search catalog, FKs,
   SQLite/FTS integrity and exact FTS rowids/content. Compare original values as
   multisets, including duplicates/NULLs/bytes; require every legacy workload row
   to remain explicitly `local`. The installation claim is compared against
   `operator_install_claim`, not recreated as a boolean. All other control data
   and exact lexical/absolute/canonical roots remain unchanged.
6. Remove TEMP snapshots; **only then** insert private pending version **29** and
   commit. Failure at publication rolls back the entire transaction. Version 29
   never masquerades as 28. `CurrentSchemaVersion` and `InitSchema` are unchanged;
   current bootstrap must reject the future schema.
7. After commit/rollback, restore and verify FK ON on that same connection with
   an independent cleanup context, even after caller cancellation. If restoration
   fails, discard the connection and return the error; keep traffic fenced. A
   cleanup error after commit does not imply rollback—inspect the durable version
   in quarantine. Repetition at 29 validates complete catalog/content/root/CAS
   state and rolls back its validation transaction; it does not rebuild or repair.

**Wiring prerequisite, not implemented here:** P4.4 onward must finish all tenant
receivers, tenant-only lexical/semantic/hybrid queries and error propagation,
attachment metadata/reference/GC writers, control install-claim readers/writers,
and bootstrap/backfill/new-database behavior. The candidate binary must select the
matching future receiver routing from this final version, not a separately
committed flag. Only after that complete cutover and acceptance may trusted shared
bootstrap invoke this owner and advance its supported version. Do not call legacy
`InitSchema` against a committed 29 DB, wire only this migration, enable a global
search fallback, or start any old query/background writer after commit. This phase
does not implement the receiver switch or authorize deployment.

### INSERT rowid ownership guard

SQLite documents `NEW.rowid` as undefined for implicit allocation in a BEFORE
INSERT trigger ([trigger cautions](https://sqlite.org/lang_createtrigger.html#cautions_on_the_use_of_before_triggers)).
The observed value `-1` is also a valid legacy ID and must not be exempted or
reassigned. The finalized search catalog therefore includes the private,
single-slot `tenant_fts_insert_guard` scratch table and paired triggers:

- BEFORE INSERT clears scratch and captures any existing foreign ID matching
  the provisional `NEW.id`, **without rejecting it yet**.
- AFTER INSERT compares that candidate with the actual inserted ID and raises
  **ABORT** on equality, rolling back the entire statement, REPLACE deletes,
  FK cascades and FTS changes. Implicit allocation cannot reuse an existing ID;
  explicit cross-owner REPLACE (including `-1`) does match and is refused.
- Every attempted row resets scratch. DO NOTHING and UPSERT-update may skip
  AFTER INSERT and retain one candidate; it is harmless, not durable ownership,
  and must not be interpreted as a pending operation on restart. UPDATE retains
  its direct BEFORE guard because its new ID is already defined.

This relies on SQLite's serialized writers and per-row execution, **not** on
AFTER-trigger ordering or `recursive_triggers`. The exact audited catalog has no
nested INSERT into `notes`; adding one requires redesign/review of scratch
lifetime. Scratch and schema are server-owned, not public SQL write surfaces.
As with the mapping, these guards are not protection against arbitrary raw SQL.
No extra rowid allocation occurs, and legacy IDs/sequences remain unchanged.

### Fresh synthetic evidence (not Amos / not the load gate)

`schema_tenant_migration_test.go` uses real temporary roots, real hashed CAS bytes,
and the populated relational fixture's IDs, relationships, credentials and claim.
It covers composition/idempotent validation, updated-title search and permanent
claim guards, exact-value/owner/sequence checks, malformed source/final state,
cancellation cleanup, and injected rollback at `reserved`, `snapshot`,
`relational`, `fts`, `files`, `validated`, and `published`. An independent WAL
connection tests writer reservation and old-state visibility through publication.
Subprocess SIGKILL rehearsals at relational/FTS/files/final-version-publication
boundaries reopen spilled WAL, compare every baseline row/catalog/sequence/FTS
shadow and retry the whole migration. No normal server is launched.

```sh
CI=1 go test ./internal/storage -run '^TestTenant(FTS|Relational|Migration)' -count=1
CI=1 go test -race ./internal/storage -run '^TestTenantMigration' -count=1 -timeout=5m
CI=1 go test ./... -count=1
go vet ./...
go build ./...
just lint
BRAIN_P4_SYNTHETIC_SCALE=1 CI=1 go test ./internal/storage -run '^$' -bench '^BenchmarkTenantMigrationComplete78952$' -benchtime=1x -count=1 -timeout=10m
```

Opt-in benchmark observed on **Darwin/arm64, Apple M4 Max, 36 GiB RAM,
Go 1.25.0**, this Phase 2 working tree: **16.409604416 seconds** for one complete
migration including snapshots, relational/FTS composition, root/CAS validation,
final commit and FK restoration (fixture construction excluded). The fixture has
78,952 repetitive ~1.3 KB notes across 100 **projects in one local tenant**, 78,952
tags, 7,895 links and synthetic 1,536-byte embedding rows plus metadata, and one
real synthetic CAS object with a reference/derivation and permanent claim. It has
no production markdown corpus, real provider vectors, or receiver traffic.
Postcommit boundary lengths before close/checkpoint: DB **544,768 B**, WAL
**621,543,232 B**, SHM **1,212,416 B**. WAL includes fixture construction; these are
not migration-only growth or continuously sampled peak allocation/temp-space
measurements. No Amos capacity/timing extrapolation or p95 claim is made. The
initial benchmark correctly failed closed on newly created zero sequence rows;
`TestTenantMigrationPreservesAbsentSequence` reproduces and guards the correction.

Final Phase 2 verification: targeted relational/FTS/migration tests passed in
9.962 s; full Go suite passed all **36 tested packages** (one additional package
has no test files), storage 14.567 s; migration race tests passed in 86.445 s with
the explicit 5-minute test budget. Vet/build and `git diff --check` exited 0;
`just lint` reported **0 issues**. The first race invocation exceeded the tool's
120-second execution budget; the explicit longer-budget rerun passed, and the
writer-reservation test now disables observer busy-waiting so it checks lock
ownership without paying seven configured wait intervals. Historical ratchet
comparison was not requested with a trusted base; ordinary current-tree ownership
and shrinking-ratchet checks ran in the full suite. No Brain status changes,
commits, receiver cutover, production access or deployment were performed.

### Remaining mandatory final gates

- **`jr1xs3a3` coordinated Amos-copy rehearsal remains unavailable:** requested
  authorized backup location has not been supplied. Still require the real
  78,952-note/~1.2 GB copy, current titles, firing triggers, exact IDs/relationships/
  credentials/claim/roots/local bytes, integrity, idempotence, measured timing,
  capacity and coordinated restore evidence. Synthetic data is not a substitute.
- **Full V10/V11 remains pending:** complete candidate receiver/routing crash
  boundaries, no partial public routing/global fallback, same-name tenant
  reference/reindex behavior, and fixed A scores/order/counts/snippets invariant
  under radically changed B frequencies/lengths in **both lexical and hybrid**
  search. Component lexical tests and dormant migration crashes do not complete it.
- Benchmark **100 and 500 tenants**: catalog and per-tenant index costs, rebuilds,
  deletes, hot-tenant interference, shared WAL/disk/temp peaks and writer-lock
  waits. Required **p95 search <500 ms at 100 aggregate requests/second**, with
  documented hardware and fixture sizes. Measure Phase 1 catalog validation and
  REPLACE cleanup scans too. The single-tenant migration timer is not this gate.
- Finalized P0/complete P3, receiver/acceptance cutover, local compatibility and
  remaining security/recovery gates retain their dependencies. Production,
  credential cutover/rotation and public activation need separate approvals;
  hosted execution remains blocked pending separately approved VM isolation.
