# P3.8 integration verification

Task: `6aysuevx`. Integration of pinned main
`4fa7210e1ba14113e61671db55cc33a84ceb9346` into P3 tip
`7a20876366bf040c4cbad78e1ed48c121bbc4104` in the existing feature worktree.
Main was not changed. No push, installation, deployment, restart, or multi
activation was performed. Reconciliation is ready for supervisor review, **not
an all-green acceptance or authorization to open the P3/P4 gate**.

## Scope and review

All ten merge conflicts were resolved without discarding either security line.
P1 content-only discovery, containment, bootstrap and bind safeguards coexist
with P3 tenant roots and TenantStore/control composition. P2 Git admission and
claim checks retain their pre-mutation ordering and credential/lifecycle fixes.
The incoming 737 test/benchmark functions across 50 changed test files were
checked for preservation, including add/add collisions.

Independent review found and drove regression-tested corrections for:

- Child or registry ENOENT being mistaken for an absent task directory, including
  checkout source discovery and later gate dependency injection.
- Durable metadata reindex failure falling through to DB/event success. The
  error now propagates; an already-written file is not rolled back.
- Ignored dangling siblings breaking bound content discovery and macOS watcher
  startup or late-root discovery. Darwin now has an event-driven root vnode
  observer with projects/global-only recovery; no polling or dependency fork.
- A notification-registration startup gap and undrained rescan/debounce work
  crossing Stop/restart generations. Deterministic blocked-repository tests
  exercise both interleavings; shutdown drains owned work before restart.

The final independent source review reported no remaining actionable findings.
These tests are stable-filesystem/single-mode evidence, not SQL tenant isolation
or an adversarial symlink-race sandbox. Operational multi remains refused before
storage opens, and v28 tenant_roots remains the P4 migration handoff.

## Passing checks

Go commands used `CI=1 GOMAXPROCS=2`, `-p 2` and fresh `-count=1` test runs.
Independent verification additionally used `GOPROXY=off`.

| Check | Result and freshness |
|---|---|
| `go test -p 2 ./...` | All 36 tested packages pass after the final lifecycle fix. |
| `go build -p 2 ./...` | Pass after final fix. |
| `go vet -p 2 ./...` | Pass after final fix. |
| `golangci-lint run --timeout 15m ./...` | Pass, zero issues after final fix; version 2.12.2. |
| Full `go test -race -p 2 ./internal/indexer` | Pass after final fix, 15.169s. |
| Startup/drain/fatal-stop/shutdown race regressions, `-count=10` | Pass after final fix, 12.648s. |
| Full runner `-race` | Independent pass, 1,196 test/subtest records; runner unchanged afterward. |
| Service injection, tenant/Git admission, metadata, claims and containment `-race` | Independent pass, 188 records, one intentionally inapplicable containment skip; service unchanged afterward. |
| Broad tenant/tenantfs/brainpath/blobstore/indexer/apiserver/auth/tokens/oauth/gitremote/storage/api `-race` | 12 packages pass, 2,242 records before review fixes; affected indexer/service checks rerun afterward. |
| Current inventory, ownership and historical-checker regression scenarios | Pass after final fix, 3.809s; 26 real-Git historical regression leaf scenarios. |
| Web `npm run typecheck`, `npm test`, `npm run build` | Independent pass; all 1,101 tests, no skips. Current unchanged web source copied to temporary directory using existing dependencies to avoid generated worktree artifacts. |
| Linux amd64, CGO-disabled indexer cross-build | Pass before final platform-independent lifecycle fix; not Linux runtime verification. |
| Formatting, staged and unstaged `git diff --check` | Clean. |

Together these cover the just-check check set plus builds and focused/broad
race verification. The ordinary full suite skips the historical comparison
without its environment variable; explicit comparisons below were run instead
of counting that skip as a pass.

## Historical gate: failures retained

See [per-identity provenance and initial-seeding semantics](p3-8-phase2-integration-evidence.md).
The reviewed exact-current baseline is **174 methods / 319 sites**. Relative to
P3.7, P2 contributes one method and seven site identities, with three stale/moved
sites removed. Every addition is attributed to its source commit in that report.

The actual unchanged historical checker was run explicitly with
`BRAIN_STORAGE_RATCHET_BASE` and
`go test -p 2 ./internal/storage -run '^TestProductionUnscopedStorageBaseline$' -count=1 -v`:

- `b8ec49c`: FAIL, imported P2 method and seven site additions.
- `7a20876366bf040c4cbad78e1ed48c121bbc4104`: FAIL, same additions.
- `4fa7210e1ba14113e61671db55cc33a84ceb9346`: FAIL, six pre-existing P3
  methods and eight P3 site identities absent from that main source.

No historical guard, trusted-base selection, or future shrinking policy was
weakened. No self-selected integrated HEAD comparison is claimed as historical
proof. Missing base goldens seed from actual base source, not current goldens.
Supervisor review must explicitly accept this one-time integration baseline;
subsequent P4 work must shrink from the trusted integrated baseline. A PR against
the pinned pre-integration main still fails the strict historical gate.

## Full service races: baseline reproduction

`go test -race -p 2 ./internal/service -v -count=1 -timeout 30m` failed in four
tests (eight race reports). Each reproduced with `-race -count=3` both on the
integration and an isolated archive of pre-integration `7a20876`:

| Test | Observed synchronization defect |
|---|---|
| `TestUpdate_FeatureAllCompleted_DedupKeyPreventsDoubleEmission` | Test closes channel while asynchronous callback sends. |
| `TestSchedulerLifecycleTickExpiresLeasesSchedulesProjectsAndUpdatesStatus` | Fake scheduler store write races polling read. |
| `TestDeliver_SuccessfulDelivery` | HTTP handler capture writes race assertion reads. |
| `TestDeliver_NoSignatureWithoutSecret` | HTTP handler capture writes race assertion reads. |

These are reproduced pre-existing test synchronization defects, not waived
passes or evidence of new tenant/Git production races. They were not changed as
part of this security reconciliation. Full service race acceptance remains red.

## Raw evidence

Independent logs are retained locally under
`/var/folders/x0/k6yjvm7d53l79z35btbzpg100000gn/T/opencode/p38-independent/`.
Relevant files include `vnode-go-full.log`, `vnode-indexer-race.log`,
`vnode-service-race.log`, `vnode-recovery-repeat.log`, `vnode-lint.log`,
`final-runner-race.log`, `final-web-{typecheck,tests,build}.log`, `core-race.log`,
`final-historical.log`, `final-historical-<full-revision>.log`,
`service-race.log`, `baseline-race.log` and `repro-race.log`.
The final lifecycle fix was followed by fresh full-suite/build/vet/lint and
indexer race checks reported above; the vnode-prefixed independent logs precede
that last fix. Temporary logs are supporting evidence, not repository fixtures.
