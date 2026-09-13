# Durable bulk entry jobs

The dashboard submits one server job for selection archive/delete, feature and
task-group status changes/deletion, and deleting all archived tasks. The worker
also supports moving a set of entries to another project. Existing synchronous
`entries/bulk-update` and `entries/bulk-delete` remain compatible for API callers.

Jobs run in the API process, independently of HTTP request lifetime and browser
navigation. SQLite schema 29 stores the request, idempotency key, immutable target
paths, content fingerprints, and per-entry outcomes. Each entry is claimed before
mutation and its outcome is recorded before moving on. There is one serial worker
per API process; deploy one API process per data/index database. This is not a
multi-server worker lease system.

## HTTP contract

All routes require `admin:*` and the existing authorized tenant context. The local
TenantStore checks both its capability and the context tenant on every access.

- `POST /api/v1/bulk-jobs` returns 202 and the accepted job.
- `GET /api/v1/bulk-jobs` returns up to 100 recent jobs, active jobs first.
- `GET /api/v1/bulk-jobs/{id}` returns counts and state.
- `GET /api/v1/bulk-jobs/{id}/items?offset=0&limit=50` returns per-entry results,
  with uncertain and failed entries first. Maximum page size: 200.
- `POST /api/v1/bulk-jobs/{id}/control` accepts `{"action":"pause"}`,
  `{"action":"resume"}`, or `{"action":"retry"}`.

Example request:

```json
{
  "request_id": "a-client-generated-unique-uuid",
  "label": "Delete archived tasks · example",
  "operation": "delete",
  "filters": [{"project": "example", "type": "task", "status": "archived"}]
}
```

Operations: `archive`, `delete`, `set_status` (requires `status`), `move`
(requires `target_project`). Explicit `paths` and constrained `filters` form a
union, deduplicated before insertion. A request accepts up to 10,000 targets,
512 filters, and 2 MiB of JSON. Oversized selections are rejected, not truncated.
Filters are resolved once; later matching entries never join an existing job.

Retry an uncertain submission with the **same request_id and request body**.
The server returns the existing job even after a restart. Reusing a key for a
different request returns 409. The dashboard saves submission intent before POST,
reconnects by reading server jobs on reload, and offers an explicit same-key retry
if acknowledgement was lost. It never automatically resubmits on page load.

## Recovery and safety

- Pausing finishes the current entry, then stops before the next. Resuming keeps
  the original snapshot.
- Live claims are checked before each task write. Without an explicit `force`
  request, an online runner's claim prevents mutation. The dashboard does not
  silently force these operations; stop the task/runner and retry failed entries.
- Entries whose content changed since submission fail before mutation. Review
  those entries and submit a new selection if the changes are intentional.
- Known preflight failures are `failed` and eligible for explicit retry. Completed
  and unchanged entries are never included in that retry.
- On restart, a claimed entry lacking a confirmed outcome becomes `uncertain`.
  Pending entries continue. Unknown mutation errors receive the same treatment.
  Uncertain entries require inspection and are never automatically replayed.
- Filesystem writes and SQLite are separate commits. This is conservative
  interruption recovery, **not an atomic transaction across the entire batch or
  a guarantee of exactly-once writes**. Fingerprints detect prior edits but do not
  lock out external filesystem writers or all other API mutations.
- The worker starts after boot indexing finishes. Progress notifications and per-feature completion checks are
  coalesced to roughly once per second plus completion, avoiding a full task
  snapshot and repeated feature scan for every entry. A pending-item index avoids rescanning completed items.

A batch still performs each entry's existing filesystem and index updates.
Status changes can therefore take longer than deletion/archiving. The durable
queue removes browser dependence and unstable pagination; it does not replace
Brain's markdown/index storage architecture.

## Verification

Run `python3 scripts/test-bulk-lifecycle.py` to create a disposable loopback
instance with 2,512 synthetic entries, no production credentials, and no runner.
It exercises the actual dashboard submission client and API, then kills the server
during a 1,000-entry deletion and restarts it against the same data/database.

Observed locally (September 9, 2026):

| Case | Result | Time |
| --- | --- | --- |
| Archive mixed legacy/explicit/feature selection | 954 succeeded | 10.0 s |
| Delete all archived | 1,096 succeeded; sibling projects preserved | 8.5 s |
| Change 205 statuses both directions | 410 succeeded | 38.3 s |
| Move between projects | 125 succeeded | 1.5 s |
| Explicit deletion | 205 succeeded | 2.0 s |
| Kill API during deletion, then restart | 1,000 attempted once; 0 pending; interrupted outcomes quarantined when present | verified |

Also verified: lost POST acknowledgement deduplicates; pause/resume; reconnect
from empty browser state; same-project move preserves the source; destination
collision preserves both files; content changes and live claims fail safely;
failed-only retry; tenant isolation; strict JSON and route authorization;
v28 migration. Browser UI verification deleted 42 disposable archived tasks and
confirmed the result after reload, including paginated interrupted-entry details.
The final timing run occurred under substantial machine load; earlier runs were
faster. These are local synthetic results, not production performance guarantees.

Validation also passed: the full Go suite from a clean checkout, targeted final
coalescing/startup regressions, backend race checks, Go vet, lint, all 1,151 web
tests, and the production PWA build. Running the suite in the primary checkout
encountered existing repository scanners traversing an unrelated nested checkout;
the clean checkout removed that interference. A loaded-machine run also exceeded
an existing runner-hook test's one-second timeout; the isolated rerun passed.
