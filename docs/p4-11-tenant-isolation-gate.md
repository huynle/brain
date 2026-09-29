# P4.11 — tenant isolation gate

## P4 repair phase 4 — complete dormant successor and sync

Task `j9amjg42`, 2026-09-12, uncommitted implementation over `4f629a28`.
**Runtime stays28; no public activation.** This section supersedes phase3's
temporary partial-staging admission description, not its historical measurements
or the unchanged private29 tests/manifests below.

The private entry point is
`migrateSuccessorSchema(ctx context.Context, db *sql.DB, checkpoint func(string) error) error`
in `internal/storage/schema_successor_migration.go`. Exact sources are `main28`,
`main29`, `main30-pre-sync`, `main30-initial-sync`, `main30-devices` and validated
`private29`. The immutable successor record is `schema_provenance` singleton1:
source profile/family/version → **`tenant`/31**, with preserved version history.
Main profiles retain byte-exact catalog pins; private29 uses its existing strict
catalog validators. See [the phase4 migration contract](p4-migration-recovery.md#phase-4-dormant-atomic-successor31)
for pinned revisions, transformations and final-only publication order. No source
is numerically relabeled, and no main29/private29 ambiguity is accepted.

`executionScope`'s unpublished partial-staging branch is **removed**. Its A/B
tests now run against complete migrated successor fixtures with real roots/CAS,
tenant FTS, provenance and version31; the partial fixture is explicitly denied.
`contentScope`, tenant search and private bootstrap routing also validate31.
Public constructors/InitSchema still refuse successor31 and main29/30. Existing
exact main-ledger preflights remain local-only; their private receiver tests are
not public startup admission. Old private29 source tests remain unchanged.

### Added storage surface and exact coverage

There are **182 workload methods/helpers** in the executable receiver manifest:
174 through phase3 plus these eight: `syncScope`, `ReadEntryChanges`,
`ReadSelectedEntries`, `ReserveSyncOperation`, `CompleteSyncOperation`,
`SyncDevices`, `SaveSyncDevice`, `SyncNote`. Raw receiver, control, exported
package-function and call-site allowances have not grown.

The four added sync tables are `entry_sync_devices`, `entry_sync_identity`,
`entry_sync_changes`, `entry_sync_operations`. Alongside the seven phase3 ledgers,
`TestSuccessorElevenTableInventory` checks the exact eleven-table set, nonempty A/B
rows, ownership FKs, and the complete catalog-discovered tenant-column table set.
Historical 26-table collision tests keep their original private29 fixture;
`TestSuccessorPrivateAllOwnedRows` adds full A/B preservation through successor
migration, including independently supplied foreign CAS bytes.

| Exact test name | Evidence exercised |
|---|---|
| `TestSuccessorMigrationActualProfiles` | Six archived revisions/five main catalog profiles; populated 26 historical workloads plus available ledgers/sync; original bytes, owners, epochs, tombstones, receipts, JSON and sequence high-water preservation |
| `TestSuccessorPrivateAllOwnedRows` | All historical A/B workloads, FTS snapshots, claim and roots survive private29→31 and repeat |
| `TestSuccessorRollbackAndPublication` | Injected failures at all seven named precommit checkpoints for the five main source profiles; complete original snapshots restored |
| `TestSuccessorPostcommitExitReopen` | Main-devices WAL child exits after Commit without Close; persisted sync/history/sequences and repeated validation remain unchanged |
| `TestSuccessorUnknownSourceNoPragmaMutation` | Unknown catalog refused without changing initially disabled FK enforcement or data |
| `TestSuccessorNonlocalSourceLedgerRefusal` | Foreign main-source ledger owners are refused, not reassigned |
| `TestSuccessorRejectsTargetMutations` | Extra/missing/changed objects, ownership, epoch and sequence corruption refused without repair |
| `TestSuccessorRejectsInvertedFTSCorruption` | Missing inverted segment rejected even when FTS content rows remain intact |
| `TestSuccessorPartialLedgerRefused` | Old unpublished partial ledger fixture denied by every execution-ledger API method |
| `TestSuccessorReceiverRouting` | Content, execution-ledger and permanent-claim routing on complete31 |
| `TestSuccessorSearchRouting` | Tenant-local FTS query through successor catalog validation |
| `TestTenantSyncIsolation` | Own payload/epoch reads, cursor reset, status0 no-replay reservation, response/hash behavior, device JSON CAS and independent foreign snapshots |
| `TestTenantSyncTriggerReplacement` | Explicit-row-ID REPLACE under both recursive-trigger settings, rename/delete tombstones, foreign replacement refusal and unchanged foreign FTS/sync state |
| `TestTenantSyncGuards` | Every sync method's invalid/missing/mismatched context/handle denial, page bounds and damaged-catalog checks |
| `TestTenantSyncMainProfiles` | Exact main profile sync/devices availability and nonlocal refusal |
| `TestTenantSyncConcurrentCAS` | Eight concurrent stale-version candidates produce one CAS winner; foreign state unchanged |
| `TestSuccessorElevenTableInventory` | Eleven-table population/FKs and complete owned-table discovery |

The existing `TestExecutionLedgers*` suite now uses complete successor fixtures
for A/B behavior, while retaining actual-main profile and public refusal checks.
The full storage suite also runs old private29 provenance hashes, manifests,
relational/FTS tests and raw/control/type-boundary guards; none is waived.

### Parent verification commands and recorded results

```sh
# All phase4 tests plus the migrated phase3 ledger tests, uncached:
CI=1 go test ./internal/storage -run '^(TestSuccessor|TestTenantSync|TestExecutionLedgers)' -count=1

# Exact final race slice (6 top-level tests, 2 recursive-trigger subtests):
CI=1 go test -race ./internal/storage \
  -run '^(TestTenantSyncIsolation|TestTenantSyncTriggerReplacement|TestTenantSyncConcurrentCAS|TestSuccessorReceiverRouting|TestSuccessorPostcommitExitReopen|TestSuccessorPrivateAllOwnedRows)$' \
  -count=1 -v -timeout=5m

CI=1 go test ./... -count=1
go build ./...
go vet ./...
golangci-lint run ./internal/storage/... ./internal/types/...
git diff --check
```

Final implementation run: full Go suite **37 packages passed**, storage
**145.531s**; exact race slice **passed, 40.909s**, no reported races/failures.
Build/vet/diff checks exited0; scoped lint reported **0 issues**. An earlier
broad focused run passed in **55.363s**, before the final inverted-FTS/private
all-row additions; the final full suite includes those additions. The final
focused migration/private-row/inverted-corruption selection also passed in
**4.928s**. These are recorded implementation results, not tests rerun for this
documentation-only update. Aggregate `just check`, hosted CI, full-repository
race and independent review were not performed by this phase.

### Limitations and phase5 integration concerns

- Complete validation includes FTS5's INSERT-syntax `integrity-check`: it is
  validation, not repair, and is incompatible with `PRAGMA query_only=ON`.
  Repeated migration validation rolls back and preserves all snapshot rows.
- Receiver preflight performs full relational/FTS and roots/CAS validation; it
  may be expensive and contend for the SQLite writer. No production throughput,
  100/500-tenant cost or coordinated-copy recovery claim follows from these tests.
- Sync sequence allocation is physically global, queries are tenant-qualified.
  Before-insert collision observation can add an extra notification for an ignored
  insert/implicit `NEW.id=-1` collision; hydration returns the still-live payload,
  not a fabricated tombstone. Main's byte-JSON CAS behavior remains unchanged.
- Phase5 must reconcile public main29/30 startup and newer API/service/worker
  callers with matching tenant context/binding and schema availability. Preserve
  newer single-mode features, Assistant/push sidecars and uncertain-effect receipts;
  do not enable unsupported tenant routes, broad forwarding or worker effects.
  No receiver may seed/repair missing history, roots or FTS during admission.
- Authentication, lifecycle/output fencing, physical filesystem race protection,
  actual-copy/sidecar restore, power-loss and post-new-write recovery, load gates
  and separate operator activation remain outstanding. No commit, deployment,
  public migration wiring or Brain status change was made.

## P4 repair phase 3 addendum — seven execution ledgers

**Historical phase3 state:** its temporary staging branch described below was
removed in phase4 above. Its then-current counts/results remain historical.

Repair task `j9amjg42`, phase 3, integrates the pinned main `cd22b4bd` storage
behavior for `bulk_jobs`, `bulk_job_items`, `execution_budgets`,
`budget_reservations`, `supervisor_checkpoints`,
`supervisor_checkpoint_versions` and `supervisor_operations`.
The executable receiver manifest now contains **174 workload methods/helpers**:
the historical 149 below plus 25 execution-ledger methods/helpers. Raw receiver,
control, package-function and call-site allowances are unchanged.

`execution_ledgers_test.go` adds a separate exact seven-table inventory and real
shared-SQLite A/B collision fixtures, independent full-row snapshots, contention,
rollback, reopen, uncertain-outcome/no-replay checks and scope/schema refusal.
The existing private29 26-table fixture remains unchanged. Its historical staging
evidence, and the new deliberately incomplete ledger staging fixture, are **not
successor readiness** or authorization to publish/admit tenant/31.

`executionScope` replaces main's `bulkScope`/`listQuery` dependency with explicit
context-to-immutable-receiver validation. Known main profiles are local-only and
must contain the requested ledger group. Its exact, unpublished partial-staging
branch is **temporary**: phase 4 must replace it with the **complete successor
validator**, composed with sync, content/FTS, roots and provenance. Do not promote
the partial catalog, broaden allowances or infer complete migration support from
these receiver tests. Runtime28 and private29 remain unchanged; no routes or
workers are enabled, and tenant/31 remains unpublished and unadmitted.

The counts and phase evidence below describe their historical implementations;
use `tenant_coverage_manifest_test.go` for the current exact receiver inventory.

This gate consolidates the shared collision fixture, runtime tests, executable
coverage manifest and canonical snapshot oracle, and runs them through
`just check` and `.github/workflows/go.yml`. The sampled predicate-removal
experiments below provide observed runtime failures and restoration evidence,
not proof that every possible predicate removal fails. Production activation
remains subject to the separate P5–P10 gates below.

## Execution and trust boundary

`just tenant-isolation-gate` runs, uncached and fail-fast:

1. **All** `internal/storage` tests, not only names containing `Tenant`. This
   includes every tenant test, `TestPhase6WebhookIsolation`,
   `TestPhase6ProjectPurgeIsolation`, `TestPhase6ScopeGuards`, both executable
   inventories and the complete guard set below.
2. All `internal/tenant` tests, including `TestProductionTenantCallsitePolicy`.
3. Apiserver tests matching `Tenant|Tenancy|GraphManager|RejectsMulti|TestRunServerRejectsOperationalMultiBeforeStorage`: private
   tenant HTTP, graph manager/cache lifetime, single-mode and operational rejection.
4. Indexer tests matching `Tenant|NonlocalLegacy`: scoped indexing, watcher policy,
   registry failure propagation and embedding indexer compatibility rejection.
5. Focused `-race` across storage/apiserver/indexer: collision concurrent access,
   claim contention, atomic attachment attach/delete, all GraphManager tests,
   concurrent HTTP/reconstruction, held HTTP/suspension, scan/watcher policy.

Go tests are non-interactive (`CI=1`), `-count=1`, package parallelism `-p 1`;
`GOMAXPROCS` defaults to 2 but can be explicitly supplied. Storage and race runs
have 15-minute test timeouts; apiserver and indexer runs have 10-minute timeouts.
Selected runtime/race runs use `-v` so CI exposes the names actually executed.
The race slice is not a full repository race run. A successful regex invocation
alone does not prove selection: review test names/output when extending it.

Go CI installs Just and invokes this same recipe, in addition to the existing
vet/build/full tests/coverage/lint. `just check` depends on it before the ordinary
checks. The existing PR historical step remains and now also runs the independent
package-function historical ratchet. Both that step and the ordinary CI test step
reject an empty PR base. The dedicated recipe rejects a missing/empty
`PR_BASE_SHA` on `GITHUB_EVENT_NAME=pull_request`, and overrides any caller-supplied
`BRAIN_STORAGE_RATCHET_BASE` with that event base.

CI supplies **`${{ github.event.pull_request.base.sha }}`**, not HEAD, the merge
commit, a branch-name guess or this worktree's local baseline. Checkout retains
full history. Unresolvable supplied bases fail in the existing checker. For local
review use `BRAIN_STORAGE_RATCHET_BASE=<reviewed-commit> just tenant-isolation-gate`.
Without a local base only the two historical tests skip; current exact checks
still run. Push events do not claim PR historical comparison.

“Trusted base” means the historical bytes are read from the selected base commit
with the existing Git-object reader, not regenerated from head. It does **not**
make PR-controlled workflows, test code, manifests or environment assignments
tamperproof. Repository review/branch protection must protect these controls.
An adversarial change to the workflow/checker itself can disable this gate.

### Exact guard matrix (all run in the full storage suite)

| Boundary | Executed tests / evidence |
|---|---|
| Raw receiver definitions, including private/value receivers | `TestFinalStorageDefinitionAllowlists`, `TestProductionUnscopedStorage`: exact four `Close`, `Control`, `ForTenant`, `SingleModeTokens` |
| Exported package functions | `TestFinalStorageDefinitionAllowlists`, `TestStoragePackageFunctionMutations`: independent seven-function golden and complete pure-helper declaration pins |
| Raw selectors/call sites | `TestProductionUnscopedStorage`, `TestUnscopedChecker`, `TestUnscopedASTMutations`, `TestUnscopedInventoryConservativeDB`: independent site golden, retaining migrated spellings |
| Historical method **and** site growth | `TestProductionUnscopedStorageBaseline`, `TestUnscopedGitBaseline`: compare actual base/head source and goldens; absent base golden bootstraps only from base source |
| Historical package-function growth | `TestProductionStoragePackageFunctionsBaseline`: separate source/golden ratchet, same supplied base |
| Actual compiled type ownership | `TestTenantProductionTypeSurface`: real production package via go/types, value/pointer/promotion/interface checks, positive scoped controls |
| Owner construction/lifetime and privileged adapters | `TestProductionStorageOwnership`, `TestStorageOwnershipPolicyChecker`: three inferred production owner seams, no production storagetest import or raw holder/alias escape |
| Workload method drift | `TestTenantWorkloadMethodInventory`: AST-defined workload set exactly matches 149 manifest methods, including private helpers |
| Table drift and colliding fixture | `TestTenantCollisionFixtureInventory`: 26 relational tables plus two durable-key tables populated for both tenants; catalog-discovered tenant columns must be classified |

The exact seven exported functions remain `New`, `NewWithDB`, `InitSchema`,
`GetSchemaVersion`, `SetSchemaVersion`, `ProjectPathPrefix`,
`DefaultProjectPlacement`. No goldens or checker logic are rewritten here.
These guards are not full data-flow analysis, SQL-predicate proofs, or an
in-process security sandbox. A newly introduced unowned table also requires the
migration catalog inventory and explicit review, not just tenant-column discovery.

## Complete 149-workload surface-to-test matrix

The executable source is `internal/storage/tenant_coverage_manifest_test.go`:
`TestTenantRuntimeCoverageManifest` actually invokes each referenced detailed test;
references fail compilation if a test is removed/renamed. The matrix is group-level
review coverage, **not** a claim of exhaustive branch coverage or a separate
assertion for every method. Helpers are covered through public entry points.
There are 60 content + 89 other workload methods. `TenantID` and `contentScope`
are the two additional binding/routing methods, excluded from the 149 workload
count and covered by execution guards/type checks.

Test names below are representative executed anchors per complete method group;
the executable manifest contains the additional detailed variants and the full
storage run includes tests outside that manifest too.

| Group (count) | Complete methods | Runtime test anchors |
|---|---|---|
| Notes (8) | `InsertNote`, `GetNoteByPath`, `GetNoteByShortID`, `GetNoteByTitle`, `GetNoteByTitleScoped`, `MergeMetadata`, `UpdateNote`, `DeleteNote` | `TestTenantNotesCRUDIsolation`, `TestTenantNotesTitleRanking`, `TestTenantNotesForeignOnlyLookupsNotFound`, `TestTenantNotesExecutionSchemaGuard`, `TestTenantNotesCorruptChildOwnership`, `TestTenantNotesInsertRepair` |
| List (3) | `ListNotes`, `listQuery`, `listNotes` | `TestTenantNotesListFilters` |
| Search (9) | `SearchNotes`, `searchFTS`, `ftsMatchWords`, `ftsMatchWordsResult`, `ftsMatch`, `searchExact`, `searchLike`, `searchAttachmentDerivedText`, `searchTenant` | `TestTenantSearchIsolation`, `TestTenantSearchFiltersAndPagination`, `TestTenantSearchAttachmentOwnership`, `TestTenantSearchReceiverForeignCorpusStability`, `TestTenantSearchReceiverConflictMaintenance`, `TestTenantSearchSchemaBeforeEmpty` |
| Graph (5) | `GetBacklinks`, `GetOutlinks`, `GetRelated`, `GetOrphans`, `graphScope` | `TestTenantGraphExactForeignPaths`, `TestTenantGraphIdenticalCorpusAndForeignMutation`, `TestTenantGraphExecutionGuards`, `TestTenantGraphCorruptChildEndpoints` |
| Links (5) | `SetLinks`, `GetLinks`, `ResolveLinksTo`, `ResolveLinksToNote`, `resolveLinksTo` | `TestTenantLinksResolutionPrecedence`, `TestTenantLinksDelayedGlobalRepairKeepsResolved`, `TestTenantLinksTagsReplacePreservesForeign`, `TestTenantRepairRejectsForgedTarget` |
| Tags (2) | `SetTags`, `GetTags` | `TestTenantLinksAndTagsRoundTrip`, `TestTenantCollisionRuntimeWrites` |
| Attachments (13) | `CreateAttachment`, `GetAttachment`, `GetAttachmentByDigest`, `ListAttachments`, `LinkAttachmentToEntry`, `UnlinkAttachmentFromEntry`, `ListAttachmentsForEntry`, `ListEntryReferencesForAttachment`, `CountAttachmentReferences`, `UpsertAttachmentDerived`, `GetAttachmentDerived`, `ListAttachmentDerived`, `DeleteAttachmentIfUnreferenced` | `TestTenantAttachmentsIsolation`, `TestTenantAttachmentsGuards`, `TestTenantAttachmentsAttachDeleteAtomic` |
| Embeddings (6) | `UpsertNoteEmbeddings`, `GetNoteEmbedding`, `EmbeddingStatus`, `SyncNoteEmbeddingMetadata`, `DeleteNoteEmbeddings`, `SearchByEmbedding` | `TestTenantEmbeddingCandidatesBeforeLimit`, `TestTenantEmbeddingMutationOwnership`, `TestTenantEmbeddingGuardsBeforeFastPaths`, `TestTenantEmbeddingMigratedSchemaNeverFallsBack` |
| Embedding index (3) | `ListEmbeddingNotes`, `EmbeddingSource`, `EmbeddingHealthCounts` | `TestTenantEmbeddingIndexStorage` |
| Events (4) | `InsertEvent`, `MarkProcessed`, `GetEventsByType`, `GetUnprocessed` | `TestTenantEventsIsolation`, `TestTenantEventsGuards`, `TestTenantEventsV29NoFallback` |
| Index maintenance (2) | `ListIndexedNoteStates`, `DeleteAllNotes` | `TestTenantCollisionRuntimeReads`, `TestTenantCollisionRuntimeWrites` |
| Metadata (4) | `RecordAccess`, `GetAccessStats`, `SetVerified`, `GetStaleEntries` | `TestTenantPhaseOneIsolation`, `TestTenantPhaseOneSchema28`, `TestTenantCollisionConcurrentWrites` |
| Triggers (3) | `ListTriggeredTasks`, `CountInProgressByTrigger`, `ActivateTask` | `TestTenantPhaseOneIsolation`, `TestTenantPhaseOneSchema28` |
| Project pause (10) | `SetProjectTaskPaused`, `SetProjectAutomationsPaused`, `setProjectPauseColumn`, `SetAllProjectTasksPaused`, `SetAllProjectAutomationsPaused`, `IsProjectTaskPaused`, `IsProjectAutomationsPaused`, `isProjectPauseColumn`, `ListProjectPauseStates`, `listKnownProjectIDs` | `TestTenantProjectPolicyIsolation`, `TestTenantProjectPolicyScopeGuards` |
| Feature pause (3) | `SetFeaturePaused`, `ListPausedFeatures`, `IsFeaturePaused` | `TestTenantProjectPolicyIsolation`, `TestTenantProjectPolicyScopeGuards` |
| Cascade roots (3) | `UpsertFeatureCascadeRoot`, `DeleteFeatureCascadeRoot`, `ListFeatureCascadeRoots` | `TestTenantProjectPolicyIsolation`, `TestTenantProjectPolicyScopeGuards` |
| Placement (2) | `GetProjectPlacement`, `UpsertProjectPlacement` | `TestTenantProjectPolicyIsolation`, `TestTenantProjectPolicyScopeGuards` |
| Claims (7) | `ClaimTask`, `ReleaseClaim`, `GetClaim`, `GetClaimsByRunner`, `ExpireStaleClaims`, `ReleaseAllByRunner`, `RenewClaim` | `TestTenantClaimsLifecycle`, `TestTenantClaimAssignmentContention`, `TestTenantClaimAssignmentForeignReferences`, `TestTenantClaimAssignmentScopeGuards` |
| Assignments (7) | `AssignFeatureIfEmpty`, `ForceAssignFeature`, `GetFeatureAssignment`, `ClearFeatureAssignment`, `ClearFeatureAssignmentsByRunner`, `ListFeatureAssignmentsByRunner`, `ListFeatureAssignmentsByProject` | `TestTenantFeatureAssignmentsLifecycle`, `TestTenantClaimAssignmentForeignReferences` |
| Dispatch/diagnostics (15) | `CreateDispatchLease`, `GetDispatchLeaseRow`, `AckDispatchLease`, `RejectDispatchLease`, `ReleaseDispatchLease`, `ClearDispatchLease`, `ExpireDispatchLeases`, `RecordPlacementReason`, `ListPlacementReasonRows`, `ListPlacementReasonRowsLimit`, `PrunePlacementReasonsForTask`, `GetDispatchLease`, `ListPlacementReasons`, `ListPlacementReasonsLimit`, `ListExpiredDispatchLeases` | `TestTenantDispatchLifecycle`, `TestTenantDispatchReasons`, `TestTenantDispatchReferences`, `TestTenantDispatchScopeGuards` |
| Runners (13) | `UpsertRunner`, `GetRunner`, `ListRunners`, `ListRunnersByStatus`, `DeleteRunner`, `UpdateHeartbeat`, `UpdateRunnerDispatchMetadata`, `UpdateRunnerCapabilities`, `UpdateAffinity`, `SetRunnerStatus`, `UpdateRunnerMaxParallel`, `ExpireStaleRunners`, `SetRunnerPaused` | `TestTenantRegistryCollisions`, `TestTenantRegistrySweepPauseDurability`, `TestTenantRegistryScopeGuards` |
| Instances (7) | `UpsertInstance`, `DeleteInstance`, `DeleteInstancesByRunner`, `GetInstance`, `ListInstancesByRunner`, `ListAllInstances`, `ReplaceInstancesForRunner` | `TestTenantRegistryForeignReferencesAndReplacementRollback`, `TestTenantCollisionReplacementRollback`, `TestTenantCollisionForeignRelationships` |
| Clients (4) | `UpsertBrainClient`, `GetBrainClient`, `UpsertBrainClientWorkspace`, `ListBrainClientWorkspaces` | `TestTenantRegistryCollisions`, `TestTenantRegistryKeyAtomicity` |
| Webhooks (7) | `CreateWebhook`, `GetWebhook`, `ListWebhooks`, `UpdateWebhook`, `DeleteWebhook`, `CreateDelivery`, `ListDeliveries` | `TestPhase6WebhookIsolation`, `TestPhase6ScopeGuards` |
| Stats (1) | `GetStats` | `TestTenantPhaseOneStats` |
| Project purge (3) | `ListProjectNotePaths`, `PurgeProjectState`, `DeleteProjectNotes` | `TestPhase6ProjectPurgeIsolation`, `TestTenantCollisionPurgeRollback` |

## Complete 26-table matrix

Each table below is SQL-seeded in **both** tenants, with colliding logical keys
and remapped globally unique physical integer IDs. The independent snapshot reads
all columns/rows and canonicalizes complete rows (retaining duplicates), rather
than using the methods under test as the noninterference oracle.

| Relational workload tables (26 total, each listed once) | Surface / test anchors |
|---|---|
| `notes`, `links`, `tags` (3) | Notes/list/search/graph/links/tags above; `TestTenantCollisionRuntimeReads`, `TestTenantCollisionRuntimeWrites` |
| `entry_meta` (1) | Metadata/stats; `TestTenantPhaseOneIsolation`, `TestTenantCollisionConcurrentWrites` |
| `generated_tasks` (1) | Legacy migration-preserved state, **no current workload receiver**; `TestTenantRelationalBackfill`, `TestTenantCollisionFixtureInventory`, retention assertions in `TestTenantCollisionRuntimeWrites` / `TestTenantCollisionPurgeRollback` |
| `event_log` (1) | Events; `TestTenantEventsIsolation`, `TestTenantCollisionPurgeRollback` retention |
| `task_claims`, `feature_assignments` (2) | Claims/assignments; lifecycle, contention and foreign-reference tests above |
| `task_dispatch_leases`, `task_placement_reasons` (2) | Dispatch/diagnostics; `TestTenantDispatchLifecycle`, `TestTenantDispatchReasons`, `TestTenantDispatchReferences` |
| `runners`, `runner_pause_state`, `opencode_instances` (3) | Registry, durable pause and atomic replacement; `TestTenantRegistrySweepPauseDurability`, `TestTenantCollisionReplacementRollback` |
| `project_pause_state`, `feature_pause_state`, `feature_cascade_roots`, `project_placement` (4) | Project policies; `TestTenantProjectPolicyIsolation`, `TestTenantProjectPolicyScopeGuards` |
| `brain_clients`, `brain_client_workspaces` (2) | Client registry/foreign parent; `TestTenantRegistryKeyAtomicity`, `TestTenantCollisionForeignRelationships` |
| `webhooks`, `webhook_deliveries` (2) | `TestPhase6WebhookIsolation`, `TestTenantCollisionForeignRelationships`, webhook cascade in `TestTenantCollisionRuntimeWrites` |
| `note_embeddings`, `note_embeddings_meta` (2) | Embeddings/index; candidate-before-limit, mutation ownership and fast-path guards above |
| `attachments`, `entry_attachments`, `attachment_derived` (3) | Attachment/derived-text ownership; `TestTenantAttachmentsIsolation`, `TestTenantCollisionForeignRelationships` |

`TestTenantRelationalEveryOwnershipAndIndex` and
`TestTenantRelationalForeignKeyInventory` check schema contracts;
`TestTenantRelationalConstraints` attempts cross-owner relational writes.
The runtime collision fixture adds `tenant_runner_keys` and `tenant_client_keys`
to those 26 (28 populated tables), and snapshots `tenant_fts` plus every mapped
FTS virtual table and data/idx/content/docsize/config shadow. This is distinct from
the number **28** used as the runtime schema version.

| Additional durable relationship keys (not part of the 26) | Runtime test anchors / contract |
|---|---|
| `tenant_runner_keys` | `TestTenantRegistrySweepPauseDurability`, `TestTenantClaimAssignmentForeignReferences`, `TestTenantDispatchReferences`, `TestTenantCollisionForeignRelationships`: owned durable runner anchors, including references retained beyond live runner deletion |
| `tenant_client_keys` | `TestTenantRegistryKeyAtomicity`, `TestTenantCollisionForeignRelationships`: owned durable client anchors and workspace-parent rejection/atomicity |

Excluded/control classifications remain those in [P4.10](p4-10-storage-surface.md):
`api_tokens`, four `oauth_*` tables, `tenant_roots`, `schema_version`, and staged
`tenants`, `operator_install_claim`, FTS mapping/trigger scratch and engine
metadata. Durable registry keys are relationship anchors, **not** enrollment or
credential authority. Legacy `notes_fts` belongs to schema 28; staged 29 uses
per-tenant mapped FTS. Physical CAS ownership is a filesystem policy, not a SQL
digest assertion.

## Cross-cutting runtime and cache matrix

| Requirement | Evidence and limits |
|---|---|
| Reads cannot cross colliding IDs/paths/projects/titles/digests/vectors | `TestTenantCollisionRuntimeReads` plus detailed group suites; both snapshots unchanged after reads |
| Writes affect own state, not foreign state | `TestTenantCollisionRuntimeWrites` asserts a non-vacuous own change and full foreign snapshot stability; representative writes, not every possible branch |
| Foreign parent relationships | `TestTenantCollisionForeignRelationships`, claim/dispatch/registry reference tests, `TestTenantRelationalConstraints`; own and foreign snapshots stable on rejected writes |
| Bulk replacement / transaction rollback | `TestTenantCollisionReplacementRollback` injects a failure after replacement begins; `TestTenantRegistryForeignReferencesAndReplacementRollback` validates foreign references |
| Project purge / rollback | `TestTenantCollisionPurgeRollback` injects a late delete failure and checks both tenants; success deletes owned selected state/notes. **Not tenant erasure**: events, attachments/blob bytes, metadata, legacy generation and other P4.10 exclusions remain |
| Rebuild/delete-all | `TestTenantCollisionRuntimeWrites` checks scoped `DeleteAllNotes` and FTS; `TestTenantAcceptanceLegacyParentAndReusedPaths` runs `IndexChanged` and `RebuildAll`, rejects nested foreign direct reads/indexing, recreates a path and verifies foreign files/CAS remain independent |
| Concurrent access / contention | `TestTenantCollisionConcurrentWrites` uses a start barrier and 16 increments with foreign snapshot stability; claim contention and attachment atomicity have separate tests; focused race includes these |
| Search/cache reconstruction | `TestTenantSearchReceiverForeignCorpusStability`; `TestTenantAcceptanceConcurrentHTTP` runs eight workers for three rounds and invalidates both graphs, asserting six builds, not just cache hits |
| Graph cache eviction, capacity, stale publication, lifetime | All `TestGraphManager*`, including `TestGraphManagerLRU`, `TestGraphManagerStaleBuildPublication`, `TestGraphManagerConcurrentShutdownAndAcquire`, `TestGraphManagerSingleConstructionAndIdempotentLeases` |
| Held HTTP and suspension | `TestTenantAcceptanceHeldHTTPAndSuspension` proves lease drain, capacity retention and rejection of later suspended admission. It blocks socket output **after** the handler executed: not P6 output fencing or retraction of authorized effects |
| Indexer background ownership | `TestTenantPolicyScanAndWatcher`, `TestTenantScanPropagatesRegistryFailure`, `TestEmbeddingIndexerTenantIsolation`, `TestEmbeddingIndexerRejectsNonlocalLegacy`; not all future worker loops |
| Operational rejection | `TestRunServerRejectsOperationalMultiBeforeStorage`, `TestStorageCompositionRejectsMultiBeforeOpeningDatabase`, `TestTenantHTTPRealFactoryAndUnsupportedSurfaces`, concurrent HTTP unsupported-route assertions |
| New-main Assistant routes remain sealed | `TestTenantAcceptanceConcurrentHTTP`: `GET /api/v1/assistant/jobs`, `POST /api/v1/assistant/speech`, `POST /api/v1/assistant/transcribe`, `POST /api/v1/assistant/voice-diagnostics` return 501 for both fixture tenants. These supplement historical POST jobs/voice placeholders; they do not exercise main's handlers, providers, sidecars or P6 authentication. |

## Runtime boundary and remaining P5–P10 work

Runtime `CurrentSchemaVersion` stays **28**, **single-only/local-only**. Public
constructors refuse staged 29. Privately migrated **29** exercises tenant SQL and
an existing private **read-only HTTP allowlist** using fixture authority. Search
POST is read-only; telemetry is covered separately by
`TestTenantAcceptancePhaseOneTelemetry`. Arbitrary writes/bulk, operational task
execution, config/token admin, extraction, MCP and newer surfaces are not admitted
by that fixture. Naming a `TenantStore` is not authenticating or authorizing one.

Actual gaps / required future extensions, not promises this gate already proves:

- **P5–P10 identity/authentication/authorization:** trusted principal-to-tenant,
  membership, runner/client enrollment, credential ownership/revocation and
  cross-transport authority must be tested at real admission boundaries. The
  listener-specific tenant context in acceptance tests is fixture authority.
- **SSE/MCP/suspension/output:** authenticated stream routing, reconnect/replay,
  MCP session scope, mid-response suspension and last-mile output fencing remain
  future gates. Private realtime separation and held graph lifetime are not full
  SSE or MCP multi-tenant support.
- **Logs and background work:** cross-tenant log/diagnostic redaction and access,
  scheduling, cron, goals, webhooks, extraction/embedding provider effects and
  detached workers need explicit tenant/lifetime/authority tests before enabling
  them. Storage CRUD tests do not validate outbound delivery or provider isolation.
- **Legacy-parent/new-tenant nested filesystem:** the existing acceptance case
  proves the supplied nested fixture's scan/rebuild/direct-path/CAS separation and
  path reuse. It does not prove all provisioning, root-registry changes, concurrent
  symlink replacement, new nested tenant creation during active scans/watchers, or
  production migration/rollback layouts. Extend those lifecycle cases before
  activation; the storage fixture alone does not authorize CAS bytes.
- **New-main integration:** SDK, Assistant chat/jobs, voice, push and bulk routes
  are not merged/activated by this phase. Current private HTTP rejection tests do
  not prove isolation of future implementations. Add new ownership inventories,
  runtime/transport/worker tests and matrix rows when integrating them.
- **Coverage limits:** the 149-method inventory is exact but its test mapping is
  group-level; no per-branch mutation score is claimed. `generated_tasks` has
  migration/retention coverage, not a runtime CRUD API. Purge is not tenant
  deletion. Focused race does not establish exhaustive interleaving safety.

Whenever adding a **table, query, route, or background loop**, CLAUDE.md requires
extending this matrix and applicable executable collision/runtime inventories,
including rollback, bulk, cache and concurrency cases. Do not widen an allowance
instead of adding ownership. Unimplemented boundaries must stay explicitly
unavailable. P5–P10 release/activation obligations remain separate from P4.11.

### Required extension checklist

| Change | Required evidence before admission |
|---|---|
| New table or ownership relationship | Classify it in the schema/catalog inventory and this table matrix; extend migration/backfill, unique-key and foreign-key tests. Seed colliding keys for both tenants and include all rows/columns in independent snapshots. Explicitly classify control/identity/engine exclusions rather than silently omitting them. |
| New query or changed query branch | Update the method manifest when its surface changes and add behavioral assertions even when the method name does not. Exercise own results and foreign exclusion, empty/invalid scope, joins, ordering/ranking and limits; for writes prove own change, foreign snapshot stability, foreign-parent rejection and applicable bulk/late-failure rollback. Run schema-28 compatibility and staged-29 fail-closed cases without changing allowances. |
| New route or transport | Extend the real-router admission/rejection matrix with colliding resource IDs and trusted authority binding, not just direct storage calls. Cover response/error isolation, graph/cache lifetime and applicable stream/replay/suspension paths. Until implemented and authorized, assert rejection; do not widen the private read-only router as a test convenience. |
| New background loop or detached effect | Test tenant-scoped enumeration and work context, registry/admission failure, cancellation/join before resource close, cache reconstruction and concurrent tenants under `-race`. Cover outbound provider/webhook effects and diagnostic scope where applicable; storage CRUD alone is not worker isolation. |

Keep exact method, package-function, call-site, compiled-type and owner guards
alongside these runtime extensions. Update the CI selection for new packages or
test names and inspect verbose output; an empty regex match is not coverage.

## Phase 2 verification (2026-09-12, uncommitted worktree)

Final recipe validation, using a **local test base**, not an actual GitHub event:

```bash
env CI=1 GITHUB_EVENT_NAME=pull_request PR_BASE_SHA=16ba6a3e \
  BRAIN_STORAGE_RATCHET_BASE=deliberately-invalid-override just tenant-isolation-gate
```

Exit 0: full storage **179.995s**, tenant **1.416s**, selected apiserver
**9.272s**, selected indexer **0.572s**; focused race storage **24.020s**,
apiserver **17.140s**, indexer **2.338s**. All seven package runs passed, no
reported failures/races. Verbose output confirmed the explicitly added
`TestRunServerRejectsOperationalMultiBeforeStorage` (the shorter `RejectsMulti`
pattern did not select it), runtime anchors and race cases. The invalid caller
override was replaced by `PR_BASE_SHA`; both historical checks ran in full storage.

Additional commands and actual outcomes:

```bash
just --dry-run tenant-isolation-gate 2>&1 | bash -n
just --show check
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7 -shellcheck= .github/workflows/go.yml
go build ./...
git diff --check
```

All exited 0. `check` lists the isolation recipe before vet/test/lint/web checks.
Actionlint reported no diagnostics; ShellCheck was unavailable and explicitly
disabled. This validates workflow syntax/expressions, not hosted execution.

```bash
env -u PR_BASE_SHA GITHUB_EVENT_NAME=pull_request \
  BRAIN_STORAGE_RATCHET_BASE=HEAD just tenant-isolation-gate
```

Expected exit 1 before tests: `PR_BASE_SHA: actual pull_request.base.sha is required`.

```bash
env -u BRAIN_STORAGE_RATCHET_BASE -u GITHUB_EVENT_NAME CI=1 GOMAXPROCS=2 \
  go test -p 1 ./internal/storage \
  -run '^(TestFinalStorageDefinitionAllowlists|TestProductionUnscopedStorage|TestProductionUnscopedStorageBaseline|TestProductionStoragePackageFunctionsBaseline|TestTenantProductionTypeSurface|TestProductionStorageOwnership)$' -count=1 -v
CI=1 GITHUB_EVENT_NAME=pull_request BRAIN_STORAGE_RATCHET_BASE=16ba6a3e GOMAXPROCS=2 \
  go test -p 1 ./internal/storage \
  -run '^TestProduction(UnscopedStorage(Baseline)?|StoragePackageFunctionsBaseline)$' -count=1 -v
```

No-base run: exit 0, **0.583s**, four top-level guards passed (plus two golden
subtests), only the two historical tests skipped. Supplied-base historical CI
selection: exit 0, **1.595s**, all three top-level tests passed (plus two golden
subtests). No checker/golden/production files were edited during Phase 2.

At the end of Phase 2, full repository `just check`, full-repository race, hosted
GitHub CI and Phase 3 mutation experiments had **not** run. Later evidence appears
below. Existing checker mutation unit tests are included in full storage; they
are separate from the runtime mutation campaign. Phase 2 left runtime 28,
the private 29 router and prior Phase 1 tests unchanged.

## Phase 3 — sampled runtime mutation evidence (2026-09-12)

### Isolation and reproducibility

Scratch repository (independent `git clone --no-hardlinks`, not a linked worktree):

```text
/var/folders/x0/k6yjvm7d53l79z35btbzpg100000gn/T/opencode/p411-phase3-mutations
```

The preapproved parent was listed before creation. Clone source HEAD was
`00ef9d61`; tracked uncommitted changes were overlaid with `git diff --binary HEAD`
and the four untracked Phase 1/2 files were copied into the scratch baseline:
`tenant_collision_fixture_test.go`, `tenant_collision_runtime_test.go`,
`tenant_coverage_manifest_test.go` (under `internal/storage/`) and this document.
Thus the baseline includes the current uncommitted tests, canonical SQL snapshot
sorting, CI/Just wiring and CLAUDE.md extension policy, not merely committed HEAD.
No tests, guards, allowances or schema/HTTP authority were weakened for mutants.
All mutation and restoration edits used `apply_patch` **in scratch only**.

Baseline commit: `7b4b69979edfe504a47813ecd40e39dfdd2b2d0b`.
One mutant at a time: each mutation commit has a baseline-equivalent parent tree;
each restoration was checked with `git diff 7b4b6997 --exit-code` and the complete
targeted selection below before starting the next mutant.

All commands in this section ran in scratch, non-interactively, uncached:

```bash
# B: initial baseline and after EACH restoration (not the aggregate gate)
CI=1 GOMAXPROCS=2 go test -p 1 ./internal/storage \
  -run '^(TestTenantCollisionRuntimeReads|TestTenantCollisionRuntimeWrites|TestTenantRegistryCollisions)$' \
  -count=1 -v
```

B passed **3/3 top-level tests and all 14 write subtests**, zero failures/skips,
on the initial baseline (**2.651s**) and after M1 (**2.443s**), M2 (**2.479s**),
M3 (**2.560s**) and M4 (**2.451s**). Every selected test name was visible in output.

### Exact mutants and results

Each mutant changes a real executed storage query after the unchanged
`contentScope(ctx)` validation. Predicate bind arguments are removed along with
their placeholders, so these are valid SQL executions, not bind-count/compiler
failures. M1/M2/M4 override only that query's `scope.where` result; they do not
change the shared helper or any other query.

| Mutant | Exact production query change (scratch only) | Expected runtime detector | Actual result |
|---|---|---|---|
| M1, content read | `internal/storage/index_maintenance.go`, `ListIndexedNoteStates`: after `predicate, args := scope.where("1=1")`, insert `predicate, args = "1=1", nil`. Staged SQL changes from `SELECT path, checksum FROM notes WHERE 1=1 AND tenant_id = ?` to `SELECT path, checksum FROM notes WHERE 1=1`. | `TestTenantCollisionRuntimeReads`, two own index states | **Killed**: four rows returned; assertion at line 26. Exit 1, **0.434s**. |
| M2, content bulk write | Same file, `DeleteAllNotes`: identical override before `ExecContext`. Staged SQL changes from `DELETE FROM notes WHERE 1=1 AND tenant_id = ?` to `DELETE FROM notes WHERE 1=1`. | `TestTenantCollisionRuntimeWrites/delete-all-notes`, two own deletes | **Killed**: `deleted 4`, assertion at line 249. Exit 1, **0.421s**. |
| M3, noncontent read | `internal/storage/runners.go`, `GetRunner`: remove `pred += " AND r.tenant_id = ?"` and `args = append(args, scope.owner)`. Retain `query += " AND p.tenant_id = r.tenant_id"`; WHERE becomes only `r.runner_id = ?`. | Shared read probe plus `TestTenantRegistryCollisions`, tenant-specific hostname | Shared read probe **survived** (exit 0, **0.445s**). Detailed registry test **killed** it: foreign `Hostname:local` for acme, assertion at line 40, nil error. Exit 1, **0.382s**. |
| M4, noncontent write | Same file, `UpdateRunnerMaxParallel`: after `pred, args := scope.where("runner_id = ?", maxParallel, runnerID)`, insert `pred, args = "runner_id = ?", []interface{}{maxParallel, runnerID}`. SQL changes from `UPDATE runners SET max_parallel = ? WHERE runner_id = ? AND tenant_id = ?` to the same UPDATE without the tenant suffix/bind. | `TestTenantCollisionRuntimeWrites/runner`, independent foreign snapshot | **Killed**: `acme/runners` changed from max_parallel 1 to 17, assertion at line 263. Exit 1, **0.439s**. |

Exact mutant commands (M3 deliberately ran both probes against the same mutant):

```bash
# M1
CI=1 GOMAXPROCS=2 go test -p 1 ./internal/storage -run '^TestTenantCollisionRuntimeReads$' -count=1 -v
# M2
CI=1 GOMAXPROCS=2 go test -p 1 ./internal/storage -run '^TestTenantCollisionRuntimeWrites$/^delete-all-notes$' -count=1 -v
# M3, first shared probe, then independent detailed suite
CI=1 GOMAXPROCS=2 go test -p 1 ./internal/storage -run '^TestTenantCollisionRuntimeReads$' -count=1 -v
CI=1 GOMAXPROCS=2 go test -p 1 ./internal/storage -run '^TestTenantRegistryCollisions$' -count=1 -v
# M4
CI=1 GOMAXPROCS=2 go test -p 1 ./internal/storage -run '^TestTenantCollisionRuntimeWrites$/^runner$' -count=1 -v
```

Observed diagnostic output (verbatim assertion lines; package prefix omitted only
from the summary table above):

```text
M1:
    tenant_collision_runtime_test.go:26: index states: [{Path:projects/p/task/same.md Checksum:<nil>} {Path:target Checksum:<nil>} {Path:projects/p/task/same.md Checksum:<nil>} {Path:target Checksum:<nil>}]
--- FAIL: TestTenantCollisionRuntimeReads (0.14s)

M2:
    tenant_collision_runtime_test.go:249: deleted 4
--- FAIL: TestTenantCollisionRuntimeWrites (0.14s)
    --- FAIL: TestTenantCollisionRuntimeWrites/delete-all-notes (0.14s)

M3 shared probe:
--- PASS: TestTenantCollisionRuntimeReads (0.15s)
PASS
ok  	github.com/huynle/brain-api/internal/storage	0.445s
M3 detailed probe:
    registry_tenant_test.go:40: runner: &{RunnerID:same MachineID: Hostname:local Labels:map[] Executors:[] Capabilities:[] DispatchPush:false WorkspaceRoots:[] Projects:[] Resources:map[] Capacity:map[] Draining:false Paused:false MaxParallel:0 FeatureIDs: RegisteredAt:0 LastHeartbeat:1 Status:online} <nil>
--- FAIL: TestTenantRegistryCollisions (0.14s)

M4:
    tenant_collision_runtime_test.go:263: acme/runners changed:
        want [[acme runner  host {} [] [] 0 [] [] {} {} 0 1  1 2 online]]
        got  [[acme runner  host {} [] [] 0 [] [] {} {} 0 17  1 2 online]]
--- FAIL: TestTenantCollisionRuntimeWrites (0.14s)
    --- FAIL: TestTenantCollisionRuntimeWrites/runner (0.14s)
```

M2 stops at its count assertion, before its later foreign snapshot check; do not
claim that particular snapshot assertion fired. M4 does reach and fail the
independent SQL snapshot oracle after its own-write/readback check succeeds.
None of these failing invocations selects static checker tests.

### Scratch commit ledger and restoration

| Mutant commit | Restoration commit (tree equals baseline) |
|---|---|
| M1 `cfbc6481abf4d36e33fbf12b5769c3c00a9ffc36` | `310efc3b8155fb5a87582ed5124d0d7defdd80d3` |
| M2 `6b7261ecb1d2c9419ae5206cdcb089366adc402c` | `b97b6d00d2da400ed26bbf6c43109c8e7bd8ff6c` |
| M3 `0d5efcbb6a1662213ac3358a0f1d59267fd9567c` | `53fed488d1bed2b2585b172925bbc7c789a02f02` |
| M4 `c1ef7975e1795691df7537aead2824788e6428f8` | `8a3760d99ceb95e9856198415d45212cefc90076` |

Final B rerun after removing M4: `TestTenantRegistryCollisions` **0.14s**,
`TestTenantCollisionRuntimeReads` **0.14s**, `TestTenantCollisionRuntimeWrites`
**1.92s** (all 14 subtests PASS); package **2.451s**, exit 0. Final
`git diff 7b4b6997 HEAD --exit-code` exited 0 with no output and
`git status --short` was empty after the restoration commit. Scratch is left at
that restored HEAD, not a live mutant. Mutant history is retained for review.

### Survivor investigation and limits

**Actual shared-test blind spot:** the fixture deliberately clones all logical
values (including `runners.hostname='host'`) into acme. The shared read test checks
only non-nil runner and `Hostname == "host"`; `RunnerRow` does not expose an owner
field. Removing caller ownership can therefore return either identical row while
both snapshots remain unchanged. Read-only snapshot stability cannot prove which
tenant supplied an indistinguishable result. This explains the observed M3
survivor; it is not a compiler error or a claim that the removed predicate is safe.

The existing `TestTenantRegistryCollisions` seeds the same runner key with
`Hostname = s.TenantID().String()` (lines 21–24), then asserts the bound hostname
for both tenants (lines 36–40). It actually killed M3 in this experiment. Therefore
the sampled runner read has a detector in the existing gate; **no campaign-wide
survivor was observed across these four mutants**. The shared fixture alone is
not a substitute for that detailed test. If extending it, keep the exact collision
inventory intact and add a separate asymmetric/foreign-only read probe rather
than weakening collision equality. No new tests or production fixes were made in
this experiment-only phase.

This is **four sampled query mutations**, spanning content/noncontent and
read/write, not an exhaustive mutation score over 149 methods or every SQL/join
predicate/branch. Other indistinguishable single-row projections merit the same
review; they were not experimentally certified here. Runtime remains 28,
single/local-only, and private staged 29/read-only HTTP restrictions remain intact.
No main-worktree production mutations, main commit, push, deployment or Brain
completion occurred. Full aggregate tests, full race/build/lint and hosted CI
were deliberately deferred to the orchestrator; targeted runtime evidence above
does not replace them. Only this evidence document is changed by Phase 3 in the
main worktree; all preserved Phase 1/2 partial work remains uncommitted.

## Final resumed-session verification (2026-09-12)

This was a fresh resumed parent session after the previous Phase 2 worker died;
the API's same-session indication was advisory. Existing partial work was
preserved, and new sequential workers completed Phase 2 and the scratch campaign.

An independent verifier inspected the aggregate diff, fixtures, matrix and exact
guards, reproduced all four runtime mutant failures through Go overlays, confirmed
the M3 shared-probe survivor, and verified scratch restoration. It passed the full
Go suite, focused race, vet, build, clean-cache lint (zero issues), both TypeScript
projects and all 1,101 web tests. Its aggregate invocation was stopped by its own
dependency-install prohibition, so the parent then ran the ordinary recipe with
no command wrappers or substitutions:

```bash
CI=1 GOMAXPROCS=2 BRAIN_STORAGE_RATCHET_BASE=16ba6a3e just check
go build ./...
git diff --check
```

**All exited 0.** The unmodified `just check` included the isolation gate, full
uncached Go tests, vet, lint (**0 issues**), `npm run typecheck`, the normal
`npm install --no-audit --no-fund` prerequisite and web tests (**1,101 passed,
0 failed**, 783.857458ms). Isolation package times: storage **47.188s**, tenant
**0.776s**, apiserver **3.761s**, indexer **0.389s**; focused race storage
**10.341s**, apiserver **7.409s**, indexer **1.618s**. Full Go storage passed in
**47.839s**. The dependency step produced no additional tracked changes.

This closes the verifier's aggregate-command limitation. The comparison base was
a local reviewed commit, not a hosted PR run. Hosted GitHub CI and full-repository
race were not executed; the four-mutant campaign and P5–P10 limits above still
apply. No production Go, ownership checker or allowance changes are included.
