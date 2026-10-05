# SDK/script takeover evidence — incomplete V1

Task `vggevclc`, plan `qfcda7ct` / `SDK-TENANCY-20261005`.
Sole continuation writer: `ses_ef225e26fffeCnN8Nv0CwBdc3e`.
Start: clean `9061f351f13ff25688f105d54323ac62ae8de53a`, no dispatch lease.
The parent explicitly transferred ownership from the completed prior writer.
No old writer restart, runner dispatch, shared authority/schema/MCP edit,
main edit, merge, push, deployment, installation or script activation occurred.

## Immutable implementation commits

| SHA | Scope |
|---|---|
| `ea28dd9fa307192da62dbf6f4807e97430c9851a` | Fix independent Go buffered SSE Close/Rebind finding; actual Go/Node regressions |
| `8196463bcde2e322de4f06289eb45a2a2413e275` | Inactive single-process cancellation/Wait and discard-only 64KiB stderr bound |
| `12d13088e10292e5ab95207bb2ac31edf9737ee6` | Linux test supervisor child death signal; external subreaper proves orphan kill/Wait |
| `8cfd38550da04b0c8c92b7acd47466bfcccd1f8e` | Inactive incremental frame sink, total wire budget, malicious flood cancellation/reaping, truncated EOF refusal |
| `bed8b3aa2b5c10ba46be34cf6805e4c8fc45d25e` | Actual hostile compiler/serializer/promise corpus and fresh native child state |
| `973a335995e0b26dc7eb17a0c05d2040235c9916` | SIGTERM to test supervisor kills/reaps worker before supervisor exit |
| `4b3dbb5bbe499e3ce8bc551cbc879ab2d61cdc3b` | Direct Go parent/native QuickJS exchange and cancellation; macOS memorystatus refusal probe |

All worker code remains inactive. C runtime/supervisor are testdata; no production
runtime dependency or worker command is selected. Parent owns independent verify.
Neither prior 104-operation coverage nor these primitives certify full V1.

## Observed failures before fixes

- SSE: independent report `xswoha7x` read in full. Real HTTP three-frame write,
  GOMAXPROCS1, `-race -count=3`: Close/Rebind delivered **3 callbacks, nil**.
  Single-frame EOF and incomplete-tail variants returned nil after retirement.
  Streams only checked a request context whose lifetime cancellation was bridged
  by asynchronous `AfterFunc`. Fix checks the immutable client lifetime directly
  before/after reads/callback and at EOF; AfterFunc still interrupts blocked I/O.
- Parent cancellation: actual helper exited normally, `err=nil state=exit0`,
  despite cancellation from its readiness output. Fixed primitive kills and Waits.
- Diagnostic flood: actual child emitted >64KiB, returned nil; parent now counts/
  discards and cancels with a fixed error, retaining no diagnostic content.
- Supervisor death: observer killed only supervisor while worker blocked on an
  unanswered framed call; orphan survived to outer5s deadline. Child now installs
  PDEATHSIG(SIGKILL), checks the pre-fork parent PID, then seals. External native
  subreaper actually reaps signal9 and observes ECHILD. An initial observer macro
  compilation error was corrected in the test harness before recording RED.
- Frame flood: 10,000 unsolicited frames originally exited normally with zero
  admission; composition now admits one, refuses its unsolicited duplicate,
  cancels and Waits the child independently of the outer deadline. Truncated
  header/payload EOF separately failed with nil before `finish` retirement.
- Supervisor SIGTERM: native observer reported exit80 because supervisor died
  by signal, rather than waiting and exiting143. It now records cancellation,
  kills/Waits the child, and exits143; external observer sees no adopted child.

## Verification (local author evidence, not independent acceptance)

- SSE fix: all `TestEventStream*` with `-race -count=3` pass (1.517s).
  Go real buffered tests cover Close/Rebind/caller cancellation crossed with
  buffered/EOF/incomplete-tail cases, exact old token/cursor. Native-fetch Node
  parity adds nine cases, custom caller reason and one-request/no-reconnect proof.
  Node **36/36**, no failures/skips; unchanged TS implementation passes parity.
- `BRAIN_SDK_NODE_INTEGRATION=1 go test -race ./sdk/brain ./internal/sdkcontract/...
  -count=1`: **3/3 packages**, 1.314s/6.413s/1.609s; actual authenticated external
  Go module and installed Node package included. Earlier replay/redaction/path
  regressions remain in these passing suites.
- Full opt-in Linux/native worker `go test -race ./internal/scriptexec -count=1 -v`:
  **33 passing top-level test/fuzz roots, 5 explicit unrelated-platform skips**,
  71.112s. This preceded the final direct-parent/macOS alternative tests below.
  Seven original refusal cases and eight new compile/freshness cases included.
  Linux supervisor death race test was additionally repeated three times (31.629s).
- `FuzzFrameSink`: **455,026 executions**, 10.357s, PASS. Packetization tests,
  length/header/envelope corruption, total wire budget and retirement pass.
- Final added direct-parent test: host wrapper `-race` PASS7.425s. The actual Go
  test parent is cross-built Linux arm64 with CGO off, **not race-instrumented**.
  It directly starts sealed QuickJS with empty environment, performs two calls/
  result42, then a separate one-call cancellation, and checks Wait/ProcessDone.
  It does not mistake Docker CLI termination for the worker lifecycle.
- New macOS probe `TestDarwinMemlimitRequiresPrivilege -race`: PASS1.846s,
  actual `memorystatus_control(7,self,...)` returns -1/EPERM. This is a negative
  availability result, NOT a confinement pass. No privilege requested. See README
  for pinned Apple XNU source showing root/private-entitlement requirement.
- Final `CI=1 GOMAXPROCS=2 go test -p 1 ./... -timeout=20m`: **45/45 tested
  packages**, no failures; many cached, scriptexec0.215s/storage147.397s.
  No frontend change or fresh frontend acceptance claim.
- Final **`just vet`, `just build`, task-local-cache `just lint`** pass;
  lint0issues, `git diff --check` clean. No contract/generator changes in takeover;
  unchanged generation evidence is the independent prior report, not rerun here.

Evidence directory:
`/private/var/folders/x0/k6yjvm7d53l79z35btbzpg100000gn/T/opencode/`.
Full outputs read: `sdk-lifecycle-suite.log`, `sdk-diagnostic-suite.log`,
`sdk-supervisor-death-suite.log`, `sdk-frame-flood-suite.log`,
`sdk-frame-eof-suite.log`, `sdk-compile-corpus-suite.log`,
`sdk-takeover-worker-race.log`, `sdk-native-cancel-suite.log`,
`sdk-managed-final-suite.log`. SSE/targeted final additions also have tool output.
All commands ran foreground; no pending/background checks. Final Docker listing
for `brain-quickjs-probe-*` was empty. Test cleanup removed only owned containers.

## Current external blockers (not all remaining work)

Latest successful S10 `ap90gj4e` recall is still draft, with both SDK proposals
and no acknowledgement. Next recall of `yp7llda1` failed **Invalid authentication
token**. No replacement credentials/identity or subsequent Brain write was
attempted. The SSE fix evidence had already been appended successfully to
`ihlslifb`; this later local report should be relayed by the parent once Brain
access is restored. Do not call an unperformed Brain write successful.

Exact shared allocations still absent: capability/profile/schema/quota/retention,
S09 commit/output/revocation/security-journal interfaces, S10 stdio file ownership
and hosted adapter, current resource ACL/publication and P8 effects composition.
No admit-only fence, OAuth wildcard, guessed schema successor, loopback identity,
real-writer-then-rollback dry-run, or duplicate authorization system was introduced.
macOS hard native memory isolation remains unproven (RLIMIT_AS counterexample
stands; memorystatus alternative refused). D06 hosted VM is a separate gate.

## Exact next runnable scope — do not relabel as exhausted

This is a context-boundary continuation record, **not completion** and not a
claim all author-owned work is blocked. Continue the existing exclusive worktree;
do not dispatch a runner or reactivate a previous writer.

1. Extend direct-parent lifecycle: the experimental C **supervised** path has
   parent-death protection, but direct `worker_main` launches do not. Production
   launch must establish parent-death and external reaper ownership even when the
   Go server dies, rather than infer it from normal context cancellation. Add an
   actual orphan observer test first. Exercise pre-fork cancellation/startup races.
2. Selected runtime/build policy remains unapproved: pinned source provenance,
   reproducible hardened binary/toolchain, native syscall/descriptor compromise
   corpus and independent policy review. Do not turn testdata into an enabled
   production command merely because these specific probes pass.
3. Bounded source-aware logs/console, final-expression behavior, complete JS facade,
   aggregate memory/CPU admission/fairness and graph/store shutdown remain absent.
   The stderr sink **discards**, not returns logs. Frame callbacks parse/quarantine;
   they are not authorized broker dispatch or protected result release. `finish`
   checks framing only; caller must separately require terminal execution outcome.
4. Keep investigating a reviewable supported macOS memory/isolation profile (or
   reviewed VM/worker service); no root helper, entitlement or degraded fallback is
   authorized by the negative test. Linux local evidence is not D06 acceptance.
5. Complete remaining per-operation metadata/error/discovery compatibility audits;
   consume owner-approved router/stdio allocation before editing shared files.
   Service-backed preflight, durable receipts/audit/quota, revocation/publication,
   zero-domain-effect dry-run and real API/MCP execution require actual handoffs.
6. Parent must independently verify **ea28dd9f** for SSE and **4b3dbb5b** for the
   new inactive lifecycle/protocol/native tests. Do not wait for that review to
   do independent work, and do not mark task complete from a bounded PASS.

Manual blocked dispatch reservation and prompt_only remain unchanged. No execution
was enabled; the full original/revised V1 acceptance ledger remains binding.
