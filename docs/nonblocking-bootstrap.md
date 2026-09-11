# Progressive entry loading

The full-library background download has been replaced by selective caching.
See [Offline entry sync](offline-sync.md) for retention and protocol details.

From web/, `node scripts/verify-cold-startup.mjs` tests an isolated localhost:3338
server (BRAIN_READER_TEST_URL override), seeded with at least 5,000 entries and
projects/bootstrap/scratch/n4999.md, titled “Bootstrap note 4999”. It verifies
50-row paging, zero global-feed requests, only opened documents cached, zero
unchanged bodies, and offline reload/reconnect. `test:offline` verifies pending
edits, replay, conflict reconciliation and cross-tab behavior for opened entries.
`test:cached-startup` verifies cached entries and task snapshots render before
network responses and that selected changes still reach the visible reader.
