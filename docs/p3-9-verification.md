# P3.9 service fixture race verification

Task: `ao72u3bq`. Starting source and supervisor-approved integrated ratchet
reference: `196cec6c27093fba87af4f977b1e4039bc9faa11`.

Only three service test files and this evidence document changed. No production
code, inventory goldens, checker, CI policy, main branch, runner installation,
deployment, restart, push, or multi activation changed. **P3/P4 remains closed
until supervisor review and local main integration.**

## Repairs

- Feature completion: a fixture-specific bus wrapper counts publications before
  real MemoryBus dispatch. A wait group joins every callback, including cleanup
  after fatal exits; a mutex protects concurrent collection. All collected
  events still require a key, now checked against the exact expected key. The
  wrapper assumes the fixture's one matching subscriber and synchronous
  publishers. MemoryBus itself does not deduplicate; the old misleading comment
  was removed, not converted into a claim of bus-level deduplication coverage.
- Scheduler lifecycle: shared fake observations are mutex-protected. The test
  waits for the synchronized completed-tick status, not the beginning of the
  second GetReady call. Started/running assertions precede cancellation; cleanup
  cancels and waits for stopped status before releasing fixture resources.
- HTTP captures: handlers publish completion explicitly, and server Close joins
  HTTP handlers before assertions read captures. Cleanup registered before
  Deliver waits for terminal persistence on either success or exhausted retries
  before store teardown. A one-second client timeout bounds individual attempts.
  Deliver exposes no worker join API: terminal persistence is the worker's last
  store-access boundary, not a claim of joining the detached production worker.

No behavioral assertions were deleted, tests skipped, or arbitrary sleeps added.
Existing condition polling is used only for synchronized observable completion.

## Reproduction and acceptance

Executed on macOS with Go 1.25.0, `CI=1 GOMAXPROCS=2`, and `-p 2` for Go
commands. All test commands used explicit fresh counts.

The exact four-test selection used below is:

```text
^(TestUpdate_FeatureAllCompleted_DedupKeyPreventsDoubleEmission|TestSchedulerLifecycleTickExpiresLeasesSchedulesProjectsAndUpdatesStatus|TestDeliver_SuccessfulDelivery|TestDeliver_NoSignatureWithoutSecret)$
```

| Check | Result |
|---|---|
| Before edits: `go test -race -p 2 ./internal/service -run '<selection>' -count=3 -timeout=3m` | FAIL, all four assigned tests report data races; 4.224s. |
| After final code edits: same selection, `-count=10 -timeout=3m` | PASS, 6.730s. |
| Each selected test in a separate `go test -race -p 2 ./internal/service -run '^<name>$' -count=10 -timeout=3m` invocation | PASS, respectively 3.630s, 1.589s, 3.233s, 2.960s. |
| `go test -race -p 2 ./internal/service -count=1 -timeout=10m` | PASS, 166.610s; full service package. |
| `go test -p 2 ./... -count=1 -timeout=10m` | PASS, all 36 tested packages. |
| `go build -p 2 ./...` | PASS. |
| `go vet -p 2 ./...` | PASS. |
| `golangci-lint run --timeout 15m ./...` | PASS, zero issues. |
| `git diff --check` | PASS. |

The explicit ratchet/ownership command was:

```bash
CI=1 GOMAXPROCS=2 \
BRAIN_STORAGE_RATCHET_BASE=196cec6c27093fba87af4f977b1e4039bc9faa11 \
go test -p 2 ./internal/storage \
  -run '^(TestProductionUnscopedStorageBaseline|TestUnscopedGitBaseline|TestStorageOwnershipPolicyChecker|TestProductionStorageOwnership)$' \
  -count=1 -v
```

PASS, 3.259s: current exact inventory, historical comparison against the approved
integrated reference, ownership policy, and all 26 real-Git historical regression
leaf scenarios. Inventory remains **174 methods / 319 sites**, with no new
production debt. The ordinary suite's unset-reference historical skip is not
counted as historical proof. The pre-integration historical comparisons reported
in P3.8 remain failures; this task does not relabel those comparisons as passing.

## Independent review

The initial fixture repair passed repeated race execution but review identified
incomplete fatal-path drains and first-event-only key validation. Both were
corrected before the final acceptance runs above. Final source review found no
remaining actionable findings.

Independent verification with `GOPROXY=off` repeated the four-test combined race
run at count 10 (PASS, 6.638s), affected-package build/vet/lint, and diff hygiene.
Temporary Go-overlay probes exercised zero and 32 delayed callbacks, and an
HTTP-500 webhook exhausting all three attempts entirely during cleanup (PASS,
7.675s including race overhead). A second probe injected a fatal exit immediately
after Deliver: the test intentionally failed, while cleanup still observed the
terminal failed delivery before store close, without a race report. This expected
injected failure is not an acceptance-suite residual. Probes were removed and
introduced no repository files or production hooks.

No residual failures were observed in the requested final acceptance checks.
This is fixture synchronization evidence, not new operational multi-tenant
authorization or SQL-isolation proof.
