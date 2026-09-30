# P4.10 — final storage receiver and owner boundary

**Current integration addendum:** the original phase-8 record below is historical.
[j9amjg42 phase5](p4-phase5-integration.md) integrates pinned newer-main, supports
single-mode runtime30, and keeps private29/tenant31 publicly refused. Its narrow
Assistant/push sidecar packages do not expand the four raw receiver or seven
storage package-function inventories. The workload method manifest is now 183
(excluding TenantID/contentScope), including delivery verification and sync.

Implementation phase 8, on `16ba6a3e` plus the preserved, uncommitted phases 1–7.
This is **not P4.11, P6, deployment, migration publication or activation**.
`CurrentSchemaVersion` remains **28**. Public constructors refuse staged 29;
the private, reviewed stage migration and read-only HTTP route allowlist remain
unchanged. The newer-main SDK/Assistant/voice/push/bulk ownership handoff in
[P4.9 resources](p4-tenant-graph-resources.md#newer-main-integration-handoff--not-implemented-in-this-phase)
still applies; those features have not been merged here.

## What the boundary guarantees — and what it does not

`TenantStore` contains only private `db *sql.DB` and `tenantID tenant.ID` fields.
It does not embed or retain a `StorageLayer`, and has no hidden owner accessor,
raw forwarding layer, DB accessor, identity validator, rebinding factory or pool
shutdown method. Callers must **name a tenant to obtain workload data through the
production storage surface**. `ForTenant` validates the ID without querying or
provisioning; naming a tenant does **not** authorize it.

Every migrated workload method routes at execution time: schema **28 accepts only
local** and retains legacy private-install SQL; privately staged **29 uses tenant
predicates**. Invalid bindings/contexts, nonlocal 28 and unknown schemas fail
closed. This compatibility path must not become fallback for damaged 29 or survive
final activation. Physical CAS authorization remains the bound filesystem policy.

The type boundary does **not** prove correctly typed methods include every tenant
predicate, raw SQL inside allowed code is safe, new tables are tenant-scoped, or
callers possess principal/membership authority. Those remain P4.11/anti-rot and
later authorization work. A method-definition ratchet misses new uses of allowed
methods; the independent call-site inventory and SQL/runtime audit remain required.
No new broad route, worker admission or multi-mode startup permission follows from
this change. Already-authorized external effects are not retractable by shutdown.

## Owner, identity and lifetime inventory

The exact raw receiver allowlist is **4**, including private receiver additions:
`Close`, `ForTenant`, `Control`, `SingleModeTokens`. No raw workload methods remain.

The separate exported **package-function** allowlist is **7**:

| Category | Functions | Boundary |
|---|---|---|
| Shared constructors | `New`, `NewWithDB` | Open/configure the shared owner; do not return workload data |
| Schema/migration | `InitSchema`, `GetSchemaVersion`, `SetSchemaVersion` | Trusted explicit DB operations, not tenant APIs; retain version-29 refusal |
| Pure value helpers | `ProjectPathPrefix`, `DefaultProjectPlacement` | No DB, SQL or delegation; full declarations pinned against laundering |

These are not seven additional raw receiver allowances. New exported workload
functions fail the package-function inventory even if their method definitions
disappear. Constructor/migration SQL still needs independent body review: name
allowlists cannot establish semantic SQL safety.

There remain exactly **3 production inferred-owner construction/lifetime seams**,
with no external production declaration of `*storage.StorageLayer`:

1. `internal/apiserver/storage.go:openSingleModeStorage`: one pool, local view,
   guarded control/root composition and single-mode token compatibility; returns
   close closure, not owner. Server cleanup cancels/joins boot workers, fences and
   joins scan/watcher, closes graph async work, **then** closes the shared DB.
2. `internal/tokens/direct.go:openDatabase`: single-mode offline token admin,
   authenticated OS ownership, guarded token adapter and owner-close closure.
3. `internal/doctor/checks.go:loadAttachmentDigestChecksFromDatabase`: single-mode
   pool lifetime plus explicitly local attachment query; closes in that function.

`storagetest` is fixture-only; production imports are forbidden. `New(t,path)`
registers owner cleanup before later worker cleanup, so LIFO drains workers first.
`NewWithDB` borrows a caller-owned DB; the test retains and closes that DB, not the
tenant view. Registry fixtures retain an explicit owner/DB and construct the real
capability-guarded adapter; no test extracts ownership from TenantStore. The exact
fixture `Registry` function has a test-only registry exemption, not production
constructor or token-admin authority. Close/corruption/restart tests now use the
explicit DB/owner. All production shutdown ordering is unchanged.

### Identity implementation inventory (30 relocated operations)

Identity is private behind the existing reviewed `ControlStore`, `TokenAdmin` and
single-mode compatibility adapters. No new public identity surface was introduced.
There are **29 private `identityStore` methods plus `generateToken`**, a private
random-token helper (formerly a raw method):

| File / count | Complete operation inventory |
|---|---|
| `tokens.go` / 9 | `generateToken` (package helper), `createToken`, `validateToken`, `listTokens`, `getTokenByName`, `revokeToken`, `deleteTokenPermanent`, `countActiveTokens`, `updateTokenLastUsed` |
| `oauth.go` / 16 | `createOAuthClient`, `getOAuthClient`, `listOAuthClients`, `createAuthCode`, `consumeAuthCode`, `cleanupExpiredCodes`, `createAccessToken`, `saveAccessToken`, `getAccessToken`, `revokeAccessToken`, `revokeAccessTokensByClient`, `cleanupExpiredAccessTokens`, `createRefreshToken`, `consumeRefreshToken`, `revokeRefreshTokensByClient`, `cleanupExpiredRefreshTokens` |
| `bootstrap.go` / 3 | `bootstrapToken`, `markInstallClaimed`, `backfillInstallClaim` |
| `tenant_roots.go` / 2 | `listTenantRoots`, `registerTenantRoots` |

Token possession is not operator authority. Global enumeration/administration and
registry provisioning retain capability gates; single-mode HTTP token behavior
retains its documented compatibility limitation pending P6/P7. Identity last-used
updates now execute synchronously with the validation request context, before
validation returns. They remain best-effort: an update failure does not deny
authentication. No detached telemetry remains after validation returns; write
contention can therefore add validation latency. This lifetime change does not
change authorization or revocation semantics. Bootstrap routes
retain the permanent claim: `entry_meta` in 28, `operator_install_claim` in staged
29, never tenant-scoped credential authority or a workload-editable marker.

## Complete P4.10 workload receiver inventory — 89 methods

All methods below are defined on `TenantStore`; counts include the three private
project-pause helpers. They are scoped on staged 29/local-only on 28. Multiple
groups touch notes or registry keys; table membership is not a claim that every
operation touches every listed table.

| Group / count | Tables | Complete methods |
|---|---|---|
| Metadata / 4 | `entry_meta`, `notes` | `RecordAccess`, `GetAccessStats`, `SetVerified`, `GetStaleEntries` |
| Triggers / 3 | `notes` | `ListTriggeredTasks`, `CountInProgressByTrigger`, `ActivateTask` |
| Project pause / 10 | `project_pause_state`, `notes` | `SetProjectTaskPaused`, `SetProjectAutomationsPaused`, `setProjectPauseColumn`, `SetAllProjectTasksPaused`, `SetAllProjectAutomationsPaused`, `IsProjectTaskPaused`, `IsProjectAutomationsPaused`, `isProjectPauseColumn`, `ListProjectPauseStates`, `listKnownProjectIDs` |
| Feature pause / 3 | `feature_pause_state` | `SetFeaturePaused`, `ListPausedFeatures`, `IsFeaturePaused` |
| Cascade roots / 3 | `feature_cascade_roots` | `UpsertFeatureCascadeRoot`, `DeleteFeatureCascadeRoot`, `ListFeatureCascadeRoots` |
| Placement policy / 2 | `project_placement` | `GetProjectPlacement`, `UpsertProjectPlacement` |
| Claims / 7 | `task_claims`, `tenant_runner_keys` | `ClaimTask`, `ReleaseClaim`, `GetClaim`, `GetClaimsByRunner`, `ExpireStaleClaims`, `ReleaseAllByRunner`, `RenewClaim` |
| Assignments / 7 | `feature_assignments`, `tenant_runner_keys` | `AssignFeatureIfEmpty`, `ForceAssignFeature`, `GetFeatureAssignment`, `ClearFeatureAssignment`, `ClearFeatureAssignmentsByRunner`, `ListFeatureAssignmentsByRunner`, `ListFeatureAssignmentsByProject` |
| Dispatch/diagnostics / 15 | `task_dispatch_leases`, `task_placement_reasons`, `tenant_runner_keys` | `CreateDispatchLease`, `GetDispatchLeaseRow`, `AckDispatchLease`, `RejectDispatchLease`, `ReleaseDispatchLease`, `ClearDispatchLease`, `ExpireDispatchLeases`, `RecordPlacementReason`, `ListPlacementReasonRows`, `ListPlacementReasonRowsLimit`, `PrunePlacementReasonsForTask`, `GetDispatchLease`, `ListPlacementReasons`, `ListPlacementReasonsLimit`, `ListExpiredDispatchLeases` |
| Runners / 13 | `runners`, `runner_pause_state`, `tenant_runner_keys` | `UpsertRunner`, `GetRunner`, `ListRunners`, `ListRunnersByStatus`, `DeleteRunner`, `UpdateHeartbeat`, `UpdateRunnerDispatchMetadata`, `UpdateRunnerCapabilities`, `UpdateAffinity`, `SetRunnerStatus`, `UpdateRunnerMaxParallel`, `ExpireStaleRunners`, `SetRunnerPaused` |
| Instances / 7 | `opencode_instances`, `tenant_runner_keys` | `UpsertInstance`, `DeleteInstance`, `DeleteInstancesByRunner`, `GetInstance`, `ListInstancesByRunner`, `ListAllInstances`, `ReplaceInstancesForRunner` |
| Clients / 4 | `brain_clients`, `brain_client_workspaces`, `tenant_client_keys` | `UpsertBrainClient`, `GetBrainClient`, `UpsertBrainClientWorkspace`, `ListBrainClientWorkspaces` |
| Webhooks / 7 | `webhooks`, `webhook_deliveries` | `CreateWebhook`, `GetWebhook`, `ListWebhooks`, `UpdateWebhook`, `DeleteWebhook`, `CreateDelivery`, `ListDeliveries` |
| Stats / 1 | `notes`, `links`, `tags`, `entry_meta` | `GetStats` |
| Project purge / 3 | `notes`, `task_claims`, `task_dispatch_leases`, `task_placement_reasons`, `feature_assignments`, `feature_cascade_roots`, `project_pause_state`, `project_placement`, `opencode_instances` | `ListProjectNotePaths`, `PurgeProjectState`, `DeleteProjectNotes` |

### Existing content groups — 60 methods, plus 2 binding/routing methods

| Group / count | Tables | Complete methods |
|---|---|---|
| Notes / 8 | `notes` | `InsertNote`, `GetNoteByPath`, `GetNoteByShortID`, `GetNoteByTitle`, `GetNoteByTitleScoped`, `MergeMetadata`, `UpdateNote`, `DeleteNote` |
| List / 3 | `notes`, `tags` | `ListNotes`, `listQuery`, `listNotes` |
| Search / 9 | `notes`, `tags`, `entry_attachments`, `attachments`, `attachment_derived`, `tenant_fts`, mapped FTS | `SearchNotes`, `searchFTS`, `ftsMatchWords`, `ftsMatchWordsResult`, `ftsMatch`, `searchExact`, `searchLike`, `searchAttachmentDerivedText`, `searchTenant` |
| Graph / 5 | `notes`, `links` | `GetBacklinks`, `GetOutlinks`, `GetRelated`, `GetOrphans`, `graphScope` |
| Links / 5 | `notes`, `links` | `SetLinks`, `GetLinks`, `ResolveLinksTo`, `ResolveLinksToNote`, `resolveLinksTo` |
| Tags / 2 | `notes`, `tags` | `SetTags`, `GetTags` |
| Attachments / 13 | `notes`, `attachments`, `entry_attachments`, `attachment_derived` | `CreateAttachment`, `GetAttachment`, `GetAttachmentByDigest`, `ListAttachments`, `LinkAttachmentToEntry`, `UnlinkAttachmentFromEntry`, `ListAttachmentsForEntry`, `ListEntryReferencesForAttachment`, `CountAttachmentReferences`, `UpsertAttachmentDerived`, `GetAttachmentDerived`, `ListAttachmentDerived`, `DeleteAttachmentIfUnreferenced` |
| Embeddings / 6 | `notes`, `note_embeddings`, `note_embeddings_meta`, `tags` | `UpsertNoteEmbeddings`, `GetNoteEmbedding`, `EmbeddingStatus`, `SyncNoteEmbeddingMetadata`, `DeleteNoteEmbeddings`, `SearchByEmbedding` |
| Embedding index / 3 | `notes`, `note_embeddings`, `note_embeddings_meta`, `entry_attachments`, `attachment_derived` | `ListEmbeddingNotes`, `EmbeddingSource`, `EmbeddingHealthCounts` |
| Events / 4 | `event_log` | `InsertEvent`, `MarkProcessed`, `GetEventsByType`, `GetUnprocessed` |
| Index maintenance / 2 | `notes` and cascading children/FTS | `ListIndexedNoteStates`, `DeleteAllNotes` |
| Binding/routing / 2 (not workload) | `schema_version` | `TenantID`, `contentScope` |

Total defined TenantStore methods: **151 = 89 + 60 + 2** (135 exported including
TenantID, 16 private). No lifecycle methods are included or promoted.

## Complete workload table coverage and exclusions

The **26 relational workload tables** are: `notes`, `links`, `tags`, `entry_meta`,
`generated_tasks`, `event_log`, `task_claims`, `task_dispatch_leases`,
`task_placement_reasons`, `feature_assignments`, `runners`, `opencode_instances`,
`project_pause_state`, `feature_pause_state`, `runner_pause_state`,
`feature_cascade_roots`, `project_placement`, `brain_clients`,
`brain_client_workspaces`, `webhooks`, `webhook_deliveries`, `note_embeddings`,
`note_embeddings_meta`, `attachments`, `entry_attachments`, `attachment_derived`.
`generated_tasks` is migration-preserved legacy state with no current workload
receiver; generation dedup is currently represented in note metadata. It is not
an unscoped raw-method exception.

Control/identity owns `api_tokens`, the four `oauth_*` tables and `tenant_roots`;
schema ownership owns `schema_version`. Staged 29 additionally has `tenants`,
`tenant_runner_keys`, `tenant_client_keys`, `operator_install_claim`, `tenant_fts`
and `tenant_fts_insert_guard`. Durable runner/client keys preserve historical
references and do not authenticate live enrollment. The insert guard is trusted
FTS trigger scratch state, not public workload data. Legacy `notes_fts` and its
four shadows belong to 28; staged 29 replaces global ranking with mapped
`fts_t_<internal_id>` virtual tables and their data/idx/content/docsize/config
shadows. SQLite catalogs, sequence and optional statistics are engine metadata;
intermediate migration tables are not API surfaces.

Project purge is **not tenant erasure**: it retains events/blob bytes and still
omits feature pause, workspace observations, generation dedup and path metadata.
No new erasure semantics are claimed by moving its receiver. See the full
[ownership inventory](multi-tenant-ownership-inventory.md) for FK contracts and
future lifecycle obligations.

## Verification guards and limits

- `TestTenantProductionTypeSurface` uses `go list -deps -export` plus `go/types`
  to load the **real production package**, checks value/pointer lookups including
  promotion, denies external selectors/interfaces (`DB`, `ValidateToken`, owner,
  control, rebinding, close), and includes positive scoped/identity controls.
- `storage_unscoped_methods.golden`: exact 4 raw definitions, private additions
  included. `TestFinalStorageDefinitionAllowlists` also fixes the final four names.
- `storage_package_functions.golden`: independent exact 7 exported package
  functions, with pure-helper declaration pins and laundering mutations.
- `storage_unscoped_sites.golden` remains unchanged. Its independent vocabulary
  retains migrated spellings; no blanket regeneration or dropped selector debt.
- Trusted-base monotonic checks compare actual source and goldens, with base-source
  bootstrapping for absent historical goldens. Local runs without a supplied base
  skip only historical checks; PRs require it. Syntax scans retain build-tagged
  files; actual go/types compilation covers the host's production build.
- Ownership policy still restricts the three inferred production owners, raw
  holders/aliases and privileged adapters. These are review guards, not a hostile
  in-process sandbox, full data-flow analysis, SQL-predicate proof or authorization.

Run `go build ./...` and `CI=1 just check` (vet, uncached Go tests, golangci-lint,
web typecheck, web tests). Final command outcomes and any environment limitations
are reported separately; this document is not activation evidence.

### Phase-8 verification evidence (2026-09-12, uncommitted worktree)

- `go build ./...`: exit 0; `git diff --check`: exit 0.
- `CI=1 just check`: exit 0. Repeated with isolated Go/lint caches and
  `BRAIN_STORAGE_RATCHET_BASE=16ba6a3e`: exit 0, lint **0 issues**, web typecheck
  exit 0, web tests **1,101 passed, 0 failed, 0 skipped**. The first lint run's
  stale unrelated-worktree cache warning was absent in the isolated-cache run;
  an additional final isolated-cache lint run also reported 0 issues.
- Uncached full `go test ./... -json -count=1` with that same local base:
  **8,591 passing test/subtest results, 7 skips, 0 failures; 37 packages passed**.
  Three skipped cases require a built embedded PWA; three are subprocess/staged
  fixture entry points; one explicitly excludes task-file reading from
  ListProjects. No failing case was skipped or weakened to restore compilation.
- Targeted real-package type proof, exact raw/package inventories, laundering
  mutations, ownership policy, original call-site/method historical checks and
  new package-function historical check all passed. The initial type proof failed
  on promoted owner/lifecycle selectors and Close interface satisfaction before
  unembedding, then passed after removal.
- `16ba6a3e` is this worktree's local comparison baseline, not a substitute for the
  actual trusted PR-base SHA that CI must supply. The call-site golden and
  production owner construction functions were not changed.

No Brain state, other task, commit, push, deployment, schema activation or newer-main
feature was changed. Remaining release obligations are the explicitly bounded
P4.11/authorization/activation work above, not unresolved fixture compile errors.
