# Cached dashboard startup

The dashboard reopens its existing per-origin, per-account SQLite OPFS cache;
network synchronization resumes from the saved change cursor. A first visit,
a cleared browser cache, a different origin/device/account, or a server sync
epoch reset still needs a download. This does not promise instantaneous first
load or remove server authority over live task execution state.

Startup now probes `/api/v1/sync/identity` instead of fetching tasks to discover
auth requirements. Project names and type counts are computed from metadata in
the database worker, without transferring every entry body to the UI. A sync
poll invalidates cached queries only if epoch/cursor/readiness/pending mutations
changed, rather than reloading every list every ten seconds. Live task streams
track whether a task snapshot has arrived separately from connection status;
cached task definitions remain visible until that authoritative snapshot arrives.
Cached definitions retain unknown runtime classification.

Verification: `just web-build && just build`, then `npm test` and
`npm run test:offline` in `web/`. `npm run test:cached-startup` seeds a loopback
server at `http://localhost:3335` (override `BRAIN_READER_TEST_URL`) with 60 entries
per run and retains them. Use an isolated development data root. It verifies:

- The entry and cached tasks render while change sync and task snapshots are held.
- Startup makes no task-list auth probe and resumes a nonzero sync cursor.
- An unchanged sync poll does not reread lists or sidebar metadata.
- A subsequent server edit still reaches the visible entry.

A local Chromium run measured 221 ms from reload to visible cached entry. This
is a development fixture measurement, not a production/device latency guarantee.
