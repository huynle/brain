# Nonblocking offline bootstrap

New devices download the change feed into SQLite in the background. Until that
download completes, lists and summaries use the online API. Opening an entry
checks the cache (including pending drafts), then fetches a missing entry
directly. Search uses the server until the cache is complete. Pending edits are
overlaid on online list results. Only the last change page marks the cache ready.

The sync control distinguishes “Downloading offline copy” from subsequent syncs.
Keep the page open to finish preparing offline access. This removes the download
from the UI loading path; it does not eliminate the initial download. Ready
caches still render locally and resume from their saved cursor.

## Verification

From web/, run `node scripts/verify-cold-startup.mjs`. It uses an isolated server
on localhost:3338 (override BRAIN_READER_TEST_URL) seeded with at least 5,000
entries including projects/bootstrap/scratch/n4999.md, titled “Bootstrap note
4999”. It holds the initial sync request indefinitely and verifies that the
uncached entry opens in the dashboard, including after reload. The previous
release fails this test because project discovery waits on the blocked sync.

The cached-startup test allows up to two minutes to prepare its initial cache;
its warm-render requirement remains five seconds. The offline test covers
queued edits, reconnect, conflicts, cross-tab persistence, and replay recovery.
