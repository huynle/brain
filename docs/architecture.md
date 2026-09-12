# Brain Architecture

## Model Roles

Brain uses separate model roles for embedding and attachment extraction. These roles serve different parts of the content pipeline and should remain independently configurable.

### Embedding Models

The embedding role converts text into vectors for semantic retrieval. It operates on text that Brain already understands, such as note bodies, task content, summaries, and derived text from attachments.

Embedding models are optimized for similarity search rather than content interpretation. Their output is stored as vector data in the embedding index and used by semantic and hybrid search to find entries by meaning.

### Attachment Extraction Models

The attachment extraction role converts media into text. It handles files such as images, PDFs, audio, and other attachment formats that are not directly searchable as plain Markdown.

Extraction models are optimized for understanding source media and producing readable derived text. They do not write vectors directly. Their output becomes normal text content that Brain can inspect, index, and pass to the embedding pipeline.

### Derived Text Bridge

Derived attachment text is the bridge between media storage and Brain search. The attachment blob remains stored as the source artifact, while extracted text is saved as derived metadata associated with that attachment.

Search then works in two layers:

- Full-text search can match the derived text directly.
- Embedding backfill can convert the derived text into vectors for semantic and hybrid search.

This keeps media interpretation separate from retrieval. Attachment extraction answers "what text can we derive from this file?" Embedding answers "how should this text be represented for semantic search?"

### Production-Safe Defaults

Attachment extraction is disabled by default. This prevents new deployments from unexpectedly sending uploaded files to a model provider, incurring model costs, increasing latency, or processing sensitive media without an explicit operator decision.

Embeddings are also optional and fall back safely to full-text search when unavailable. Together, these defaults keep Brain useful in local and production environments without requiring external model services.

Operators should enable attachment extraction only after choosing an appropriate provider, model, timeout, and data-handling policy for their deployment. Once enabled, extracted text flows through the same search and embedding paths as other Brain text.

## Durable tenant filesystem roots (P3.4)

This is a filesystem-policy foundation, **not multi-tenant runtime readiness**.
Multi-mode startup remains disabled. It does not introduce signup, provisioning
HTTP endpoints, a backup subsystem, or authorization from caller-supplied paths.

### Immutable operator-owned mappings

`internal/tenantfs` resolves an explicit tenant ID through `tenant_roots` in the
existing shared SQLite database. This table is authoritative configuration, not
a markdown-derived index; reindexing does not remove it. Registration takes a
SQLite writer reservation before validating all existing registrations and
inserting. Repeating an identical registration is idempotent; replacing roots is
refused. Future root changes require a separately designed fenced migration.

Single-mode startup persists `local` before starting file consumers. Its exact
effective BrainDir and independently configured attachment root strings are kept
alongside absolute I/O anchors and canonical symlink identities. Reopening the
same database, or operator provisioning of additional tenants, does not relocate
local files or reinterpret relative configuration against a new working directory.
The database location is not tenant-rewritten; operators must still open that
same database (the resolver does not discover a moved database).

Trusted operator composition may register nonlocal IDs under
`<base>/tenants/<id>` or supply explicit brain/CAS overrides. Omitted overrides
on repeat retain the registered values. Invalid IDs, changed explicit roots,
overlapping foreign brain/blob roots, and symlink aliases are rejected. The only
cross-tenant nesting exception is beneath a legacy local root's reserved
`tenants/` directory. Canonical comparisons resolve both sides, including macOS
`/var` aliases. Every access re-reads the registry and checks root drift; cached
root handles see later registrations.

### File consumers and exclusions

Server composition binds the local policy to the indexer, its watcher, service
writers/readers, recursive project deletion, and the attachment filesystem store.
Actual I/O uses persisted absolute anchors. Scans prune explicit exclusions;
registry failures abort synchronous scans rather than produce an incomplete
successful scan and delete index rows as supposedly missing files. Task lookup
helpers distinguish not-found from policy failure. Async watcher admission and
flush also deny failed checks, but currently drop failed events without retries.

Watcher discovery is restricted to `projects/` and `global/`. On Darwin,
fsnotify v1.9.0's kqueue child enumeration can stop at an unrelated dangling
sibling without reporting an error. A separate root-only vnode notification
therefore triggers a scoped content-root rescan; it does not open siblings,
poll, or patch fsnotify. Registration follows root admission but precedes the
initial directory snapshot, so changes during startup remain buffered. Recovery
retains tenant admission, ignore rules and no directory-symlink recursion;
recovery failures stop the watcher rather than silently report success.

`Stop` signals shutdown, rejects further queued work, and waits for the event
loop, root notification source, and owned debounce callbacks to drain before
returning. `Start` is serialized behind cleanup, including fatal loop exits, so
old work cannot reach a new generation or a store closed after `Stop`. The loop
owns cleanup directly on fatal exit instead of joining itself. An already
in-flight index operation may finish before `Stop` returns; this is draining,
not transactional rollback. Other platforms retain their fsnotify event source.

Local excludes reserved `tenants/` trees even before registration, all foreign
brain/CAS roots, and aliases into them. Check each traversal entry, not just the
starting directory. Direct `IndexFile`, service reads and mutations must use the
same policy. The markdown parser independently checks root containment; it is
not itself a tenant resolver and its callers must supply tenant exclusions.
Recursive deletion preflights the full plan before mutation and rejects an
ancestor containing excluded roots. Future export/restore/purge implementations
must use per-entry traversal admission and destructive preflight as applicable;
copying the shared SQLite database is not a tenant-scoped export.

Local CAS retains `<root>/<digest[0:2]>/<digest[2:4]>/<digest>`. New registrations
default to `<brain-root>/blobs/<id>/sha256/<digest[0:2]>/<digest>`; a blob override
names the CAS root itself. Equal digests occupy independent tenant roots. Bound
uploads stage under the mapped CAS `.staging` directory. Blob paths and staging
are checked again per operation, including after registration and root drift.

### Explicit remaining boundaries

- This is check-then-use policy, **not an atomic openat-style capability**. The
  host filesystem and provisioning must be trusted/fenced between validation and
  I/O. Concurrent symlink swaps, hostile host users, and hard-link aliases are not
  an isolated execution environment.
- Existing unbound constructors remain for legacy/internal callers and tests.
  They must not be used as a tenant-isolation boundary. Server composition uses
  the bound constructors.
- P4.8 now scopes index reconciliation/rebuild through TenantStore. P4.10 removes
  owner promotion across the workload surface; runtime remains local-only schema
  28, with staged-29 fixtures only. Background authorization and activation remain
  separate work. See [P4.10 scope and inventory](p4-10-storage-surface.md).
- No backup/restore service, quota accounting, crash-safe blob intent protocol,
  tenant deletion lifecycle, or public activation is added by this foundation.

Regression coverage is in `internal/tenantfs/resolver_test.go`,
`internal/storage/tenant_roots_test.go`, the indexer/service/blobstore
`tenant_policy_test.go` files, `internal/apiserver/tenant_filesystem_test.go`, and
`pkg/markdown/parser_test.go`. Tests cover durable restart/promotion mappings,
independent CAS layouts, aliases and root drift, registry errors, direct indexing,
watcher exclusions, service mutations, and recursive deletion preflight.

## Shared storage handles (P3.5, phases 1 and 2)

`storage.New` / `NewWithDB` (through `newFromDB`) still own the **one shared
SQLite pool**, its PRAGMAs, `InitSchema`, migrations and shutdown. Production
workload holders now take `*storage.TenantStore`, never `*storage.StorageLayer`.
This is an ownership migration, **not SQL isolation**.

- `(*StorageLayer).ForTenant(tenant.ID) (*TenantStore, error)` validates with
  `ID.Valid()` before SQL. Zero IDs fail; trusted `tenant.Local` is valid. A
  private `tenantID` field is exposed read-only by `TenantID() tenant.ID`.
- `(*StorageLayer).Control() (*ControlStore, error)` wraps the same storage with
  a **private named backing**, not an embedding. Both factories are cheap memory
  wrappers: no SQL, pool configuration, schema initialization, root registration,
  tenant-existence checks, new database, per-tenant DB registry or database stamp.
- `ControlStore.ValidateToken` and `GetAccessToken` implement the existing API
  and OAuth validator interfaces. They authenticate possession of a token, not
   deployment-operator authority. P4.10 makes the best-effort `last_used` update
   synchronous with the validation request context, leaving no detached telemetry
   after validation returns. Update failure does not deny authentication; write
   contention can add latency. Authorization and revocation semantics are unchanged.
- `ControlStore.TenantRegistry(auth.DeploymentOperator)` returns
  `(tenantfs.Repository, error)`: exactly `ListTenantRoots` and
  `RegisterTenantRoots`, preserving the resolver's serialized validation policy.
- `ControlStore.TokenAdmin(auth.DeploymentOperator)` returns
  `(*storage.TokenAdmin, error)`, implementing `api.TokenService`: `GenerateToken`,
  `CreateToken`, `ListTokens` (including the variadic revoked flag),
  `GetTokenByName`, `RevokeToken`, `CountActiveTokens`, plus explicit
  `DeleteTokenPermanent` for offline administration. P1 adds `BootstrapToken` to
  the API interface; that delegate also checks operator capability. These retain
  existing signatures and semantics; empty token scope defaults to `admin:*`.
- Identity flow delegates are explicit: `CreateOAuthClient`, `GetOAuthClient`,
  `CreateAuthCode`, `ConsumeAuthCode`, `CreateAccessToken`, `SaveAccessToken`,
  `CreateRefreshToken`, `ConsumeRefreshToken`. They support existing password
  login and OAuth consent/PKCE/refresh flows; authorization stays with those
  callers. No identity listing, client-wide revocation, or global cleanup is
  exposed. `MarkInstallClaimed(ctx, operator)` is capability-gated initialization.

### Operator authority and delegation

`(*auth.Verifier).AuthenticateOperator(username, password)` returns
`(auth.DeploymentOperator, error)` using the **existing configured operator
username and bcrypt password verification**, not a new credential store. Nil or
unconfigured verifiers, wrong credentials and zero capabilities deny access with
`auth.ErrOperatorRequired`. No public constructor accepts a boolean, an arbitrary
verifier, an API token scope, an auth context, or a tenant ID. `Valid()` is a
read-only query on the opaque capability. `admin:*` and `local` are not authority.

Unattended local startup and offline token commands instead use the narrow
`auth.AuthenticateLocalDatabaseOwner` host-ownership seam. The OS authenticates
the process; successful read/write opening of the **existing regular database**
is the authorization evidence. It does not create files, chmod them, accept a
caller boolean/verifier/token, or require a newly configured password. It logs
the authorization source and database path. This is OS filesystem authorization,
not proof of a particular UID, and not a sandbox against hostile code running
with the same host permissions. The database, its parent paths, process and
deployment configuration remain trusted. Never give it request-supplied paths.
The capability is used immediately for guarded control adapters; it is not
installed in ordinary handlers. Exact production call sites are checked by CI.

Registry enumeration/provisioning and general system token administration require
this capability at adapter construction; adapter methods also reject zero/nil adapters
before SQL. The control and adapter exported method sets have **exact allowlist
tests** in `internal/storage/control_test.go`; adding a method requires explicit
review. This is not the later P3.7 data-plane method ratchet.

Capabilities and their adapters are in-process delegated authority, not session
tokens: there is no expiry, revocation, serialization or request authentication
machinery here. Keep them inside trusted composition for the authenticated
operation. Do **not** install an operator-bound token adapter into an ordinary
admin-scope HTTP handler: doing so would confer system-wide operator authority on
all its callers. The filesystem resolver retains registry read/write capability;
expose only bound roots to data-plane consumers, not its provisioning methods.

### Shared schema ownership

`InitSchema` continues to initialize both domains together, once per shared store
open, not per handle. Identity/control owns `api_tokens`, `oauth_clients`,
`oauth_auth_codes`, `oauth_access_tokens`, `oauth_refresh_tokens`, and the durable
`tenant_roots` configuration. Workload owns notes/FTS, links, tags, metadata,
attachments/embeddings, events, task/dispatch state, runners/instances, placement,
pause state, webhooks and deliveries. Runner registration is workload data, not
control tenant registration. `schema_version`, schema changes, raw DB maintenance
and pool close remain with the audited shared bootstrap/migration owner. Neither
control adapters nor tenant handle creation runs migrations. Reindexing must
preserve authoritative tenant roots; a shared database copy is not a tenant export.

### Migrated holders and audited ownership

The following workload holders and all their constructors/fixtures take a tenant
view: indexer `Indexer`, events `StorageScheduleSource`, services
`BrainServiceImpl`, `TaskServiceImpl`, `RunnerServiceImpl`,
`RunnerRegistryServiceImpl` (runner registration is workload),
`ClientContextServiceImpl`, `AttachmentServiceImpl`, `WebhookServiceImpl` (both
constructors), `GoalService`, `ReminderService`, `ProjectPlacementService`, and
`TriggerTaskStoreAdapter`.

`apiserver/server.go` passes that view to data services, scheduler leases, the
feature-assignment cleaner and scheduler visibility. Password login, dual-auth,
OAuth persistence/access issuance and MCP validators receive `ControlStore`.
Only the token HTTP interface receives the single-mode compatibility adapter
described below. Bound filesystem policies, not registry repositories, go to
file consumers.

Raw ownership outside the storage implementation is confined to these exact
functions (no retained/returned raw pointer, alias, or raw workload call):

- `internal/apiserver/storage.go:openSingleModeStorage`: rejects operational
  multi/unknown mode before opening; opens one pool; creates local/control/token
  views; authenticates OS ownership; persists configured-password claim through
  guarded Control; obtains `Control.TenantRegistry(operator)` for filesystem
  composition; returns a close closure. No operator capability is returned.
- `internal/tokens/direct.go:openDatabase`: validates configured single mode,
  opens the offline pool, authenticates OS ownership, delegates only
  `Control.TokenAdmin(operator)`, and returns the pool's close closure. It never
  gives the CLI a raw owner or silently chooses a tenant in multi mode.
- `internal/doctor/checks.go:loadAttachmentDigestChecksFromDatabase`: validates
  single mode, owns open/close, and lists attachment metadata via the local view.
  Its historical database-path choice is unchanged by P3.5.
- `internal/storage/storagetest/store.go`: fixture-only local view factories;
  production imports are forbidden. Storage implementation tests may keep raw
  owners. `New(t,path)` registers owner cleanup; `NewWithDB` borrows the caller's
  explicitly owned DB. There is no TenantStore.Close. Register worker cleanup
  after owner cleanup so LIFO drains workers before closing the database.

`internal/storage/ownership_test.go` is the production ownership policy, **not
P3.7's method ratchet**. It scans Go source including build-tagged files, resolves
storage import aliases, rejects dot imports/raw holder declarations/type aliases,
new pool factories outside exact owner functions, raw-owner argument/alias/query
escapes, test-helper production imports, and unreviewed references to host-owner
authentication or privileged adapter constructors. Offline token administration
imports are restricted to `cmd/brain/commands/token.go`, preventing ordinary
handlers from routing around those restrictions through CLI helpers. This is an auditable CI
convention, not an in-process security boundary. Exact reflected control, admin,
and single-mode adapter method allowlists also run under `go test ./...`.

### Temporary single-mode HTTP token compatibility (replace in P6/P7)

`StorageLayer.SingleModeTokens(tenant.ModeSingle)` constructs an unembedded,
private-backed `SingleModeTokenStore`. Unset/unknown/multi modes and zero/nil
adapters deny; every method checks the private single-mode binding before SQL.
Only audited server composition may install it. It exposes exactly
`GenerateToken`, `CreateToken`, `ListTokens`, `GetTokenByName`, `RevokeToken`, and
`BootstrapToken`; no raw DB, registry, permanent deletion, control handle, or
operator capability. It is not a `TokenAdmin` and does not mint operator power.

This explicitly preserves the existing single-install HTTP `admin:*` token
management exception (and legacy auth-disabled local behavior). API auth/scope
checks remain in place. It is a **temporary API-auth limitation**, not the future
multi-tenant authorization design. P6/P7 must replace this exception with proper
tenant/operator authorization **before multi enablement**. Passing a caller flag
or a bearer token must never authorize deployment-wide operations.

The narrowly ported P1 safeguards from `5611563` remain mandatory: first-install
bootstrap checks actual loopback `RemoteAddr` (ignoring forwarded headers), with
only exact server environment `BRAIN_ALLOW_REMOTE_BOOTSTRAP=true` bypassing the
peer restriction. A configured password, active API credential, or unexpired
OAuth/password access token closes bootstrap permanently. An atomic writer-first
transaction permits only one bootstrap winner. Credential issuance and startup
backfill persist `brain:system/install_claimed` in existing `entry_meta` on v28
(`operator_install_claim` on privately staged v29); password
configuration is stamped before background workers start. Revocation, deletion,
expiry, config removal, and restart cannot reopen it. The global install claim
is excluded from tracked-entry stats and is **not a per-tenant registry/stamp**.
No unrelated P1 filesystem/CORS/bind-auth changes were imported by this work.

### P4.10 final receiver boundary

TenantStore is now unembedded, with only a private SQL pool and tenant binding.
`DB`, `ValidateToken`, `Close`, `Control`, `ForTenant`, `SingleModeTokens` and the
exported embedded owner are absent, including promotion. Callers must name a
tenant to obtain workload data through the production storage surface; this is
not principal authorization or proof of predicates inside correctly typed methods.
**Multi mode remains disabled.** Runtime stays local-only schema 28; public
constructors still refuse privately staged 29.

See [P4.10 complete table/method inventory](p4-10-storage-surface.md) for all 89
moved workload methods, existing content groups, private identity ownership, the
exact four raw methods, separate seven package functions and verification limits.
Predicate/raw-SQL/new-table audits remain P4.11/anti-rot; constructor/migration
body review and the three audited lifetime seams are independent requirements.

## Notes/list/search receiver transition (P4.4)

Notes, list and search are now defined on `TenantStore`, with execution-time
schema routing. **Runtime `CurrentSchemaVersion` and public constructors remain
v28**; only private migration tests publish v29. For v28, an explicit `local`
binding retains the legacy private-install query behavior. Nonlocal bindings,
invalid handles/contexts and unsupported versions fail before blank-query or
other early returns. This transitional route is not a tenant/global fallback.

Privately migrated v29 searches reserve one transaction for FTS mapping/catalog
validation, full `NoteRow` hydration, filters, ranking, counts, word retries and
attachment merging. No transaction helper calls the pool. Mapped per-tenant FTS
tables provide BM25; filters precede limits and nested tags, attachment references,
attachment parents and derivations all require matching ownership. Missing or
damaged catalogs return `ErrTenantSearchUnavailable`, never global FTS, LIKE or
attachment-only successful results. Only recognized FTS expression syntax errors
may retry as literal words. The old no-error `ftsMatchWords` signature remains for
compatibility; production fallbacks use an error-preserving path instead.

The v29 catalog audit also rejects unreviewed nested note-insertion triggers:
the migration's row-ID scratch guard depends on no nested insertion into `notes`.
Existing migration tests cover explicit/automatic/negative IDs, UPSERT, REPLACE
and `recursive_triggers` variants; receiver tests exercise the finalized catalog.
Short-ID/path/title lookup remains fuzzy within the bound tenant, and title
project preference remains a rank, not a project filter.

**Final activation must remove the local-v28 query route.** P4.10 has removed the
legacy raw bridge and embedding, and moved identity/bootstrap/install-claim
routing without public v29 publication. Do not enable v29 in constructors to
accommodate this plane or preserve v28 routing as recovery for a failed v29 search.
This is not public multi-tenant readiness.

## Unscoped storage debt ratchets (P3.7)

`TestProductionUnscopedStorage` in `internal/storage/unscoped_ratchet_test.go`
runs under ordinary `go test` (therefore `just test` / `just check`). It makes no
runtime changes. Two manually reviewed, canonical, exact inventories live in
`internal/storage/testdata/`:

- **`storage_unscoped_methods.golden`**: the 173 initial method **names defined**
  on `StorageLayer` in production `internal/storage` Go files. `go/ast` includes
  pointer and value receivers, private methods, and every build-tag/GOOS variant,
  independently of the host build. Same-named definitions in mutually exclusive
  variants are one allowance; moving a method between files is not new debt.
  Definitions are not calls. Method signatures and bodies are not compared.
- **`storage_unscoped_sites.golden`**: 315 initial syntactic occurrences outside
  the storage implementation. Each line groups `selector#occurrence` tokens by
  repository-relative `file:receiver.function` (or `file:package`). It inventories
  selectors named like raw methods, not just calls: method values/expressions,
  defer/go calls, and same-spelled fields/types/unrelated APIs are included.
  Closures count in their enclosing function. Occurrence counts catch another
  reference in the same function without depending on line numbers or formatting.
  `DB` is always included, plus `db-return` markers for explicit result types
  referencing `database/sql.DB` (including import aliases/dot imports). In
  particular, `Indexer.DB` is a forwarding escape, not a tenant-scoped operation;
  its signature and forwarding call, the indexer's direct DB calls, and any new
  external `.DB` references are counted. The runner's unrelated SQL DB parameter
  is deliberately conservative noise, not a claimed storage escape.

Both comparisons reject additions by **set identity**, including same-count
replacements, with actual/golden counts and added/stale keys in the diagnostic.
Removal requires deleting stale golden entries, so dead allowances cannot linger.
No update-golden switch exists. The initial fixtures were transcribed from the
checkout scan; do not grow them to silence failures. Selector vocabulary includes
both raw definitions and spellings retained by the site golden, so a receiver move
does not silently drop still-live selector debt.

### Limits and companion guards

This is a review ratchet, **not type checking or tenant isolation**. Receiver
expressions and data flow are not resolved. Same-spelled selectors can be false
positives; changing a receiver/argument or replacing a use within the same
file/function/selector count is invisible. Calls through existing function-value
aliases, reflection, unqualified functions, and wrappers with other names or
aliased/interface/indirect DB result types are not a complete escape analysis.
The explicit DB-return marker does not prove the returned DB belongs to Brain.
Renames/moves of inventoried functions/files require review, not automatic rebaselining.

`Close` selectors are deliberately omitted to avoid a large unrelated resource
close inventory; `Close` **definitions remain in the method golden**. The existing
`ownership_test.go` continues auditing raw owners' lifecycle calls, holders,
constructors, explicit embedded `StorageLayer` access and privileged seams. It is
unchanged for production seams. P4.10 separately proves TenantStore.Close absent
using real-package go/types; its registry fixture exception is test-only.
Scans exclude `_test.go`, testdata/fixture directories (`storagetest`), vendored
dependencies, node_modules and git/worktree metadata; all other Go files are parsed
even when build constraints disable them. Malformed production Go fails the scan.

There are **no SQL-predicate, raw-SQL, new-table, authorization or query-body
guarantees**. Existing local-only query guards and disabled multi-mode startup must
remain. P4's scoped SQL work needs its own tests; **P4.10 un-embedding supplies the
type-surface barrier** that these syntax inventories cannot provide. A separate
package-function allowlist prevents exporting removed workload methods as free
functions; it is not folded into the independent call-site vocabulary. Both the
package-function and original inventories retain trusted-base anti-growth checks.

### Historical anti-growth gate

`TestProductionUnscopedStorageBaseline` additionally rejects source **and golden**
set additions for both inventories against `BRAIN_STORAGE_RATCHET_BASE`. Coordinated
source/golden edits and same-count replacements cannot approve new debt. Shrinkage
is allowed; the separate exact-current test still requires stale allowances removed.

Go CI fetches full history and passes the actual `pull_request.base.sha`, not a
merge parent or PR-controlled golden, to an explicit targeted test. The full test
step uses that same SHA for PRs; pushes retain the exact-current checks. A requested
empty, invalid or unavailable commit fails closed; no fetch or guessed fallback
occurs inside the test. A local run without the variable skips only the historical
test; PR runs missing the variable fail. To check locally:

```sh
BRAIN_STORAGE_RATCHET_BASE=<trusted-base-sha> GOMAXPROCS=2 go test -p 1 ./internal/storage -run 'TestProductionUnscopedStorage' -count=1 -v
```

Base data comes from `git ls-tree` and `git cat-file --batch` into an in-memory
filesystem: no checkout, archive attributes, filters, or symlink traversal. Relevant
non-regular files are rejected. Each absent base golden bootstraps independently
from **actual base source**, never the head golden. Both source scans use a shared
union of base/head definitions and spellings retained in either inventory, so
removed/migrated methods do not erase live debt or manufacture site additions.

Real temporary Git repository regressions in `unscoped_baseline_regression_test.go`
cover coordinated growth, independent source/golden gates, shrinkage, replacements,
missing/invalid refs, no-golden bootstrap, receiver moves and archive exclusions.
Synthetic AST coverage remains in `unscoped_checker_test.go`. These are review/CI
guards; modifying the checker or workflow itself still requires trusted review.
