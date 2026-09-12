# j9amjg42 phase 1: archived source fixtures

The executable fixture is `schema_provenance_test.go`, not the earlier synthetic
main-additions fixture and not target `InitSchema` with appended DDL. It reads
immutable Git objects with `git show`, retains the complete archived `schema.go`,
and extracts ancillary constants and `initEntrySync` by Go AST source offsets.
Only the package/import harness changes. The archived initializer executes against
a new temporary SQLite file using this repository's pinned SQLite driver. No
server, production database, checkout switch, network download or main write is
used. Missing Git history fails the test; there is no mutable-source fallback.

| Profile | Archived revision | Catalog objects | Exact catalog SHA-256 |
|---|---|---:|---|
| main28 | c0b28634355a91b24ab9e9415978f12866d3b5cd (44f963bb parent) | 129 | cb702fa33fbe1231a6c1347384aaeee874ebdf35aa308c068dcf5923e942d2c3 |
| main29 | 44f963bbb0c5102390a06f6548088eda6d0f9832 | 136 | 269268c4e9ed0cc16a05a5ce9c9e2b23cd86d63372d1aec48e6c7af8c9888a23 |
| main30-pre-sync | 3d2baaf450b1b93f981f6e03eb3ab07766237c21 | 146 | d43f48233533d3476d92ffeec25b1f874cb91dc55e01ee7d09e81a7880a17776 |
| main30-initial-sync | d21ba2d948738bed6456c66f4453fc96a593d1b4 | 154 | 72fc3207492e15819c48b58df76f4faf79eefd7f1b035f007140eee95227941d |
| main30-devices | 7132bf0ce97cb961b3d233c915c1a99ef63708c2 | 156 | 2104f3eac47abaa1b5cc8e35e7265e7ca7b7856cd0e1335df03d33f01bb65a66 |
| main30-devices (final source check) | cd22b4bdc3b5229621169fe5b214d7ffd12a6015 | 156 | same as above |

Hash input is JSON `[][4]string`: `(type,name,tbl_name,coalesce(sql,''))` for
**every** `sqlite_schema` row ordered by type/name, including implicit indexes,
FTS shadows and triggers. SQL bodies are byte-exact. Root pages, application rows
and sequence values are excluded from the fingerprint, not from no-mutation
snapshots. A profile describes a catalog lineage, not cryptographic proof of which
binary created a database. Source version history must be nonempty positive
integers <=30 with the profile's maximum version; historical rows need not be
contiguous because archived bootstrap records only its current version.

Private29 uses the unchanged exact relational/search/mapping and control
validators, not main29's stamp. The existing populated private fixture is pinned
independently to the unchanged 014d3d1d catalog: SHA-256
`fb03efc669e87027ba2e3607d640ce3fe763d08336a7434d0da35acdaca8848e`, over newline-joined
tab-separated catalog columns with its one random internal FTS ID replaced by
`PINNED` (including trigger names and mapping literals). This pin prevents the
fixture and mutable private target DDL silently drifting together. It does not
replace the existing dynamic mapping validator.

## Evidence surfaces

- `TestSchemaProvenanceActualMain`: six archived fresh bootstraps, including the
  final pinned main, with exact profile, integrity/FK and full catalog/row snapshots.
- `TestSchemaProvenanceHistoricalUpgrades`: actual sequential archived startup
  from 28 through 29 and all three 30 generations, including same-version startup.
  Seeds all eleven new tables, uncertain bulk attempts/fingerprints, committed and
  parent-linked reservations, checkpoint history, outcome-unknown supervisor
  receipts, a conflict receipt, device draft, deletion tombstone absent from notes,
  fixed epoch and a sequence watermark above the live maximum.
- Main sync startup attempts `INSERT OR IGNORE ... SELECT path FROM notes` even
  for already-known live paths. In this one-live-note fixture that consumes one
  AUTOINCREMENT value per startup: 900 -> 901 -> 902. The classifier consumes none.
  Future migration must preserve the actual input high-water mark, not reconstruct
  it from live notes or `max(seq)`.
- `TestSchemaProvenanceRefusesUnknown`, `VersionCollision`, `Private29`: reject
  table/index/trigger/view/column mutations, partial hybrids and numeric relabeling.
- `TestSchemaProvenanceInvalidContext`: nil/cancelled inputs and TEMP shadow denial.
- `TestSchemaProvenanceDoesNotAdmitRuntime`: actual main29/30 still refused by
  `New`, `NewWithDB`, `InitSchema` and the old dormant private migration.
- Positive/refusal classification runs with SQLite `query_only=ON`; full snapshots
  include every table, FTS shadows, catalog/root pages and sequence rows. The caller
  can still query and roll back its transaction afterward.

## Deliberately limited recognition and phase-2 contract recommendation

No runtime call site is added. Runtime28, private29, receiver routing, operational
multi-mode refusal and all ownership/method/package manifests remain unchanged.
This adds no table, DDL allowance, raw accessor or public package function.

Main recognition covers the exact fresh catalogs and the tested upgrade chain
originating at that genuine main28 baseline. Older ALTER-derived catalogs, optional
ANALYZE statistics, other SQLite engines/catalog renderings and operator extensions
are **not** silently admitted. Private29 retains its existing reviewed validators
and their existing optional engine-statistics policy. Neither path certifies row
ownership, data integrity, root/CAS bytes, credentials or migration readiness.

Phase 2 should select a distinct reviewed successor (not reused 29 or main30),
with profile-aware dormant routing only. Under one fenced connection/transaction,
classify before staging/TEMP snapshots; dispatch explicitly by profile, never
restamp a source as 28 to reuse the old migration. Keep seven already-keyed ledgers
and sync history intact. Classification is not permission to backfill a nonlocal
ledger row as local. Require explicit ownership validation and complete preservation
of payloads, revision history, roots, permanent claim, tombstones, epoch and sequence
high-water marks. Stage relational/FTS/sync/claim changes together and publish the
successor last in that same transaction only after phases 3/4 implement the full
contract. Unknown catalogs stay untouched; committed successor repeat validates,
never repairs. Ordinary startup/public activation remains unavailable until its
separate reviewed integration and acceptance gates.

No successor migration, scoped new receivers, main feature integration, real-copy
recovery, load acceptance or independent external review is claimed by phase 1.
