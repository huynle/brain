# Bulk lifecycle regression verification — 2026-09-09

These checks used disposable synthetic data and loopback-only servers. Production
was not mutated. The starting revision was `c0b28634`.

This records the initial bug-fix verification. The subsequent durable backend
implementation and current test workflow are documented in [Bulk jobs](bulk-jobs.md).

## Reproduced failures

1. **Legacy archives refused:** a task with an existing SSH `git_remote` failed an
   archive-only PATCH because runnable Git admission was checked on every update.
   The bulk response was HTTP 200 with per-entry errors; the tray hid the useful
   error behind aggregate failure counts.
2. **Visible archived tasks did not match bulk deletion:** `GET /tasks/{project}`
   used the task directory, while bulk filters used optional frontmatter
   `projectId`. With 142 legacy tasks missing that field, the dashboard showed
   `Delete all archived (142)` but returned `Nothing matched — no tasks deleted`.
3. **Same-project move deleted its source:** the service wrote the destination
   and then removed the source, even when they were the same path. It reported
   success with neither a file nor an index entry remaining.
4. **Destination collisions overwrote existing data:** a cross-project move used
   an overwriting file write instead of refusing an existing destination.

The new service regression tests were run before the fixes and failed on each
case. They now pass.

## Corrections

- Status-only retirement to archived/cancelled/superseded may preserve an existing
  unsupported remote. Reopening, changing the remote, or changing execution fields
  still requires normal admission. Existing live-claim protection remains in force.
- Project-scoped task filters use the same task directory as task listings, so
  absent or stale `projectId` fields do not hide tasks from bulk operations.
  SQL prefix matching escapes wildcard characters and includes the directory
  boundary, protecting similarly named sibling projects.
- Same-project moves are no-ops. New destinations are created exclusively so a
  collision cannot overwrite a file.
- Archive failures retain representative server error messages in the tray.

## Real server + real dashboard API client

Run `python3 scripts/test-bulk-lifecycle.py` from the repository root. It builds
Brain, seeds 1,512 synthetic files, starts a new loopback server with its own
configuration/database and no runner or embeddings, waits for indexing, and runs
`web/scripts/bulk-lifecycle.ts`. The TypeScript driver uses the actual archive
job, API wrappers, and pagination helper. The launcher stops its server in a
finally block and retains its fixture, server log, and results log for inspection.

Observed local results (timings are not production benchmarks):

| Operation | Verified outcome | Time |
| --- | --- | --- |
| Archive 954 selected tasks | All archived; includes 278 legacy SSH tasks, feature pagination, explicit paths, missing/stale project metadata | 5.74 s |
| Reopen unsupported legacy remote | Refused; no task reopened | 0.07 s |
| Purge 1,096 archived tasks | 11 pages; all removed; sibling and active tasks preserved | 3.97 s |
| Change 205 task statuses twice | Completed → pending → completed; both directions drained fully | 12.47 s |
| Move 125 entries across projects | All moved; source lookups return 404; destination content preserved | 1.93 s |
| Same-project move and destination collision | No-op preserves source; collision refuses and preserves both files | 0.01 s |
| Explicit deletion with one missing entry | 205 deleted, exactly one failure reported | 0.39 s |

Final filesystem counts agreed with API results: the source project contained
only the untouched collision fixture, and the destination contained 125 moved
entries plus its pre-existing collision fixture.

## Browser interaction

A separate synthetic dashboard at `http://127.0.0.1:43339` was exercised through
its real controls:

- Before the fix: `Delete all archived (142)` reproduced the user's exact
  `Nothing matched — no tasks deleted` message.
- After the fix: the same action completed with `142 tasks deleted`.
- Selected two features totaling 824 tasks, including the 278 legacy SSH tasks.
  The confirmation closed immediately and the tray reported live progress.
  Navigation to Entries remained available; the retained result was
  `824 archived … 0 failed attempts. 2/2 groups checked.`

## Automated checks

- Service/storage/API regression suites pass, including existing remote-admission
  and move safety tests.
- Web tests: 1,151 passed.
- Production PWA build: passed.
- `go vet ./...`: passed.
- `golangci-lint run`: passed, zero issues (with a warning about an unrelated removed nested worktree).
- `go test ./...`: passed in a clean checkout containing the patch and regression
  tests. In the primary checkout, two existing repository scanners also scanned
  an unrelated `.claude/worktrees/` copy and failed on duplicate policy callsites;
  the clean-checkout run avoided that unrelated filesystem artifact.

No production deployment is part of this verification.
