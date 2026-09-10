# Offline entry sync

The existing React PWA now reads entries from SQLite WASM in a dedicated worker.
OPFS persists the database across reloads; the service worker precaches the
application, database worker and WASM assets. The Go API remains authoritative.
The embedded server serves `.wasm` as `application/wasm`.

## User behavior

Open **Offline sync** at the bottom right, or **Edit definition** in an entry's
reader. Knowledge entries, tasks, and automation definitions can be read,
searched, created, and edited offline. The full-file editor includes YAML and
Markdown, including automation actions. Normal single-entry update/create calls
also use the outbox. New tasks and automations in the offline creation form start
as drafts; activating their definitions can allow server execution after sync.

The initial download caches all indexed entries available on this single-tenant
server, including global entries. Subsequent requests download only changed
paths. The editor's selector displays at most 250 matches; filter by title, type,
or project to find another entry. Existing entry lists and FTS searches read the
local database. Semantic/hybrid search still uses the server while connected;
when disconnected, searches fall back to local full text, not local embeddings.

“Saved locally” means durable on this device, **not yet accepted by the server**.
The panel distinguishes queued edits, unconfirmed uploads, and edits needing
review. Validation failures preserve the draft for correction; conflicts display
the local and current server versions before explicitly applying a draft to the
current revision. Export a draft before discarding it if it needs to be retained.

Runner controls, execution, sessions, bulk operations, moves, deletion, graph
queries, and attachment downloads still require connectivity. Cached task
views do not claim that a task is runnable. Sync runs while the app is open,
on reconnect and every ten seconds; it is not guaranteed while closed or while
the browser suspends the page.

## Protocol

- `GET /api/v1/sync/identity` provides a namespace derived from the authenticated
  identity, rather than an account name supplied by the browser. Successful
  logins bind the cache to this namespace; token refresh preserves it. Browser
  origin also isolates servers. Logging out removes the active namespace, not
  the underlying OPFS files. Signing back in under the same identity restores
  pending edits. OAuth clients have their own identities. As with any offline
  cache, revocation cannot erase copies on disconnected devices.
- `GET /api/v1/sync/entries?epoch=...&cursor=...&limit=200` returns `{epoch,
  cursor, more, changes}`. Each change is a path plus an entry and raw indexed
  file, or a deletion tombstone. A nonzero cursor requires an epoch. Limits are
  1–500. Empty changes means no data transfer beyond the envelope.
- `POST /api/v1/sync/entries` accepts `{id, method, path, revision, body?, raw?}`.
  Supported methods are `POST` (JSON create, empty path) and `PATCH` (JSON update
  or full-file `raw`, with a mandatory base revision). Existing entry handlers
  perform validation and persistence. Responses retain their normal status/body.
- Receipts bind operation IDs to authenticated identity and exact payload. A
  repeated ID returns the recorded response; changing its payload is refused.
  A reservation is persisted before side effects. If a process dies before
  recording its result, replay fails closed with an uncertain-outcome conflict.
  This is deliberately not a claim of an atomic transaction spanning SQLite,
  filesystem writes, and scheduler events.

SQLite triggers maintain one latest monotonic sequence per path, including
retained tombstones. They cover API, indexer, watcher and runtime metadata writes,
with change markers committed in the same database transaction as indexed data.
A move produces a source tombstone and a destination entry. Sync reads markers
and indexed payloads in a single read transaction. Newer writes move a path
forward, so pagination does not skip a concurrent update. The current feed has
no time-based retention/pruning; storage grows with distinct paths, including
previously deleted paths, rather than every version of a frequently edited row.
Receipts are likewise retained; retention requires an explicit protocol change.

The first sync drains this same paginated feed from cursor zero. It is a
convergent bootstrap, not a server-side long-lived snapshot. The client records
each page and cursor in one local transaction, and marks the cache ready after
catching up. A different database epoch or impossible cursor returns 410 and
causes re-bootstrap. Drafts survive reset. Unconfirmed sent operations become
uncertain on epoch reset because the new database may have lost their receipts.

Unsent edits coalesce; once sent, payloads cannot change until the outcome is
known. A local draft version guards against two editors overwriting each other.
The full-file editor retains its original server revision even if background
sync fetches a newer version. Conflict resolution allocates a new operation ID
and revision. Deleted server entries leave local drafts available for export.

## Persistence and boundaries

The official `@sqlite.org/sqlite-wasm` SAH-pool VFS avoids requiring COOP/COEP
headers. Each database operation uses a browser-wide Web Lock, opens the SQLite
database, then closes it and pauses its VFS to release file handles. This lets
multiple tabs share OPFS without independently holding exclusive handles. A
separate Web Lock serializes uploads; BroadcastChannel refreshes other tabs.
The app never reports a successful local save before the SQLite transaction
commits. Storage failures surface as errors.

Use a browser with OPFS, Web Workers and Web Locks on HTTPS or localhost.
Browsers missing these APIs retain the online API path. Request persistent
storage with **Keep data on device**. Browser quota, private-mode restrictions,
user-cleared site data, and device loss can still remove unsynced drafts.
Attachments are references in the entry cache, not offline blob replicas.

This follows the repository's current single-tenant boundary; the storage
methods reject non-local tenants. Multi-tenant SQL isolation remains a separate
project. Out-of-band Markdown changes reach clients only after server indexing:
enable the existing file watcher or restart to index changes. There is no new
filesystem scanner in this feature.

## Reproduce verification

```sh
just build-all
cd web
npx playwright install chromium
npm run test:offline
```

`web/scripts/verify-offline.mjs` starts the production Go binary on an unused
loopback port, with temporary Brain/config/state directories and feature checkout
disabled. It seeds 208 real entries and drives the built PWA in Chromium,
including service-worker offline reloads and real SQLite OPFS. It prints the
temporary evidence directory containing results, server logs, and screenshots.
It does not use a user's Brain database or start a runner.

Additional checks:

```sh
go test ./...
go vet ./...
cd web
npm test
npm run typecheck
```

Browser coverage is Chromium on macOS; Safari, Firefox, mobile devices, and very
large production datasets still need release qualification. Unit/integration
coverage includes transactional feed rollback, pagination, watcher/indexer
writes, tombstones, moves, revision parity, validation, receipt persistence after
reopening the server database, local draft conflicts, and cache reset behavior.

## Local verification result (2026-09-10)

The production build passed the seeded Chromium scenario with 208 entries and
13 explicit checks: paginated bootstrap and an empty unchanged feed; WASM MIME
and cached automation actions; offline PWA/OPFS reload; local FTS in the existing
Entries view; durable knowledge/task/automation edits; update/delete/move deltas;
reconnect with exactly one offline create; server conflict review and resolution;
stale second-tab rejection; shared OPFS across tabs; a simulated lost response
*after the real server committed* followed by reload/retry without a duplicate;
retaining an open editor's original revision after background sync; and no
uncaught browser errors with the editor fitting a 390px viewport.

`just build-all`, `go test ./...`, `go vet ./...`, `just lint` (zero issues),
`npm test` (1,166 tests), and TypeScript checking passed locally. The browser
script leaves a machine-readable result plus screenshots in its printed temp
directory. These results do not claim Safari/Firefox or physical-mobile testing.
