# P4 migration and recovery runbook — dormant component

Assignment `jr1xs3a3`, phase 3, 2026-09-07. **Pending v29 NOT ENABLED.**
`CurrentSchemaVersion = 28`. No CLI migration command, runtime call, version bump,
public activation, or production operation is introduced. This is a preparation
and isolated rehearsal procedure, not authorization to migrate a deployment.
See [approved contracts](multi-tenant-security-contracts.md) D07/D08/D11 and the
[refreshed inventory](multi-tenant-ownership-inventory.md).

## Stop/go prerequisites

**STOP for deployment today.** Finalized P0 and complete P3 are required; so are
the downstream tenant FTS map/rank isolation, tenant CAS metadata/root handling,
all scoped receivers (including raw-handle removal), and operator install-claim
reader/writer routing. One future outer owner must validate and atomically commit
the relational changes, FTS mapping, CAS metadata, receiver-routing selection,
claim cutover and schema version. There must be no externally visible intermediate
state. Filesystem publication is not a SQLite transaction: retain legacy bytes in
place and reconcile any future durable blob intents before serving. No global FTS
fallback. This component alone must **never be committed in a deployment**.

`stageTenantRelationalSchema(*sql.Tx)` requires v28 and FK OFF before the outer
transaction. It owns only a savepoint; no pool, commit, flag or version stamp.
It rebuilds 26 workload tables, adds four registry/reference/claim tables,
validates counts, ownership, definitions, integrity and FKs, and leaves old FTS
for replacement in that SAME transaction. Repetition validates staged structure;
it is not a supported restart of a partially committed migration.

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
  this phase tests only after relational staging, before commit. Unexpected mixed
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

**Acceptance blocker: no authorized coordinated Amos snapshot is available.**
No live Amos access was attempted. Full V01/V03/V10/V15 remain pending: relational
constraints and one precommit crash point are component evidence only, not local
workflow parity, complete tenant isolation, every-stage cutover recovery, or
revocation-safe restore/RPO/RTO proof. Public activation remains separately gated.
