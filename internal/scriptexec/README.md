# Script execution building blocks (disabled)

This package is not an execution service or a sandbox. No caller or route is
wired to it. SDK script exposure remains false. The approved implementation plan
is Brain `qfcda7ct`, amendment `SDK-TENANCY-20261005`.

The prototype codec uses a four-byte big-endian length followed by one JSON
envelope: `version`, `kind`, `sequence`, `payload`. It currently models only
`call` and `result`. Envelopes are capped at 1 MiB before payload allocation;
zero, partial, malformed, duplicate-field, unknown-field and trailing JSON
envelopes are refused. Sequence numbers fit JavaScript's exact integer range.
An IPC error must retire the stream, never replay its partially written frame.
No authority is represented by the envelope; payload is untrusted opaque JSON.

`ProtocolSession` now validates parent-side sequence/direction, pending replies,
terminal state, call count, payload bytes and cumulative payload bytes. Call
payloads have exactly `operation` and object `arguments`; UTF-8, duplicate decoded
keys at every level and nesting beyond 64 are rejected. Every violation retires
the session, including concurrent duplicate admission; output buffers are copied.
The actual framed QuickJS fixture uses this state machine. It has **no dispatch
callbacks or authority**: a syntactically accepted operation name is not a registry
entry, permission, dry-run validation or output-release decision. The owner must
retire the session on external I/O failure and must never retry partial frames.

Still required before use: per-operation schema decoding and explicit allowlisting,
authorized preflight, operation/log/result production policy, cancellation
and pipe deadlines, aggregate admission, authority/output/publication fences,
durable audit, worker process lifecycle, selected runtime and Linux/macOS resource
and confinement proofs. Nested arguments remain semantically opaque: the broker
must decode and validate each approved operation, never dispatch raw JSON.

A passing codec/fuzz test proves none of OS isolation, worker reaping, provider
safety, authorization or hosted VM readiness. There is no execution fallback.

### Inactive process lifecycle primitive

`runWorkerProcess` owns one trusted command's Start/Wait, kills on context
cancellation, preserves cancellation cause, and joins its observer before return.
It independently counts/discards stderr with a 64KiB aggregate ceiling; overflow
cancels the child and returns a fixed non-content error. It never retains or
forwards diagnostics to a logger. Real local subprocess tests prove kill/Wait,
pre-cancel no-start, start failure, normal exit and diagnostic flooding; these are
not tests of a confined JS runtime. There is no production caller.

The trusted launch owner must still ensure no descendants can be created, close
inherited descriptors, sanitize the environment, establish OS resource/confinement
bounds, supply cancellation-safe bounded stdin/stdout, and arrange external reaper
ownership. An arbitrary blocking Go reader/writer is not made interruptible by this
helper. No shell/command input is exposed through HTTP, MCP or a script facade.

`frameSink` incrementally assembles stdout frames under a total **wire-byte**
budget (including envelope padding), before forwarding to a trusted parsing/
quarantine callback. Real malicious-child flooding tests compose it with
`ProtocolSession` and `runWorkerProcess`: one call is admitted, the unsolicited
duplicate retires the protocol, cancels and reaps the child without the outer
deadline. Hostile length headers, packet boundaries, budget exhaustion and
truncated EOF are covered, including fuzzing. The single writer must be joined
before `finish`; terminal-result presence and current output authority are
separate required checks. No callback is an operation grant or permission to
release result bytes, and no untrusted callback is made interruptible here.

`TestQuickJSManagedParentIntegration` cross-builds the Go tests for Linux arm64
and runs them inside the existing opt-in compiler container. The Go parent
**directly** starts the sealed native QuickJS binary with empty environment,
exchanges two framed fixture calls through `frameSink`/`ProtocolSession`, retains
result42 until successful Wait, and separately cancels after one call and Waits
the killed child. This is not a Docker CLI PID being mistaken for the worker.
The inner cross-build is non-race (`CGO_ENABLED=0`); host-side parent primitives
have separate race coverage. No Brain services, credentials or live output exist.

On Linux `runWorkerProcess` now sets `Pdeathsig=SIGKILL` before exec using Go's
parent-PID race check. Because Linux binds it to the creating **thread**, that
thread stays locked until Wait and cancellation-observer join finish. Other OSes
acquire no parent-death guarantee. A launch must prohibit set-ID execution and
the sealed native worker must deny changing this signal. External init/subreaper
ownership remains mandatory: a dead parent cannot reap its own orphan.
`TestQuickJSManagedParentDeath` actually SIGKILLs the Go parent after its direct
native worker's first call; an external C subreaper keeps stdin open, reaps signal9
and verifies ECHILD. Without the change the orphan survives the outer five-second
deadline. The Linux Go parent remains a non-race cross-build; this is test-fixture
evidence, not production init/deployment approval or a macOS lifecycle guarantee.

### macOS address-space negative control

`BRAIN_SCRIPT_DARWIN_MEMORY_PROBE=1 go test -race ./internal/scriptexec -run
TestDarwinAddressSpaceDoesNotBoundResidentMemory -count=1 -v` compiles/runs a
bounded native counterexample (128MiB maximum requested allocations, hard CPU
five seconds). PASS means an **isolation gap** was reproduced, not confinement.
With baseline+64MiB `RLIMIT_AS`, one million ordinary 128-byte allocations grew
RSS by 143,163,392 bytes while virtual size grew only 524,288 bytes locally.
Pre-existing allocator virtual reservations permit this: refusing a new 128MiB
mapping does not constrain new resident memory. Do not promote the earlier
baseline-relative mmap observation to macOS memory readiness. A separately
reviewed enforceable memory/isolation mechanism is still required.

An additional unprivileged alternative was investigated against Apple XNU
[`f6217f891ac0bb64f3d375211650a4c1ff8ca1ea`](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/bsd/kern/kern_memorystatus.c#L9192):
`memorystatus_control` command7 (SET_MEMLIMIT_PROPERTIES) is behind root or
`com.apple.private.memorystatus`; the documented exceptions in that source do
not include this command. `TestDarwinMemlimitRequiresPrivilege` invokes it only
for the fresh probe's own PID, empty environment, no privilege escalation and
no allocation stress: locally it returns -1/EPERM. PASS means that alternative
is **unavailable**, not secure memory enforcement. No entitlement/helper is
installed; this does not prove every future macOS/VM design impossible.

## Opt-in macOS process-confinement experiment

`BRAIN_SCRIPT_CONFINEMENT_PROTOTYPE=1 go test -race ./internal/scriptexec -run TestDarwin -count=3 -v`
runs installed Node under `/usr/bin/sandbox-exec`, with an empty environment and
fresh working directory. It evaluates async JavaScript and asserts actual OS
denial of a host-secret read, host-file write, child-process spawn and connection
to a listening local TCP endpoint. A second probe starts an infinite JavaScript
loop, cancels it after a readiness message and calls Wait to reap the killed
process. No API credentials or Brain handles are passed. No runtime dependency
has been selected or added to the production build.

Observed locally with Node22.23.3: async42, environment0, read/write/spawn/network
EPERM; three repeated race runs of both tests pass. Initial deny-default echo
launches aborted before main: the sandbox log identified dyld reads of `/` plus
sysctl needs. The Node probe subsequently exposed regex escaping and OpenSSL's
ambient configuration read. The profile now allows loader files and explicitly
uses `--openssl-config=/dev/null`, not access to host OpenSSL configuration.

**Not a usable worker sandbox:** sandbox-exec is deprecated (local Apple manpage).
The experimental profile grants system-library reads, Homebrew dylib reads,
metadata and executable mapping; those are not a reviewed native-compromise
boundary. `--max-old-space-size` is only a JS heap setting, NOT address-space,
external-buffer, aggregate-memory or CPU enforcement. Inherited-descriptor denial,
source/compile/log/result bounds, broker IPC, fresh capability state, Linux
confinement and hosted VM evidence remain unproven. Normal test runs explicitly
skip these host-dependent experiments unless opted in. Production execution
remains unavailable; no profile or capability discovery was changed.

## Opt-in local Linux container experiment

Set `BRAIN_SCRIPT_LINUX_PROTOTYPE_HOST` to a local Unix Docker socket and
`BRAIN_SCRIPT_LINUX_PROTOTYPE_IMAGE` to an **already installed immutable image
digest**, then run `go test -race ./internal/scriptexec -run TestLinuxContainerPrototype -count=1 -v`.
The test never pulls images or mounts host files. It uses a nonroot user, empty
environment, read-only root, no network, dropped capabilities, no-new-privileges,
32 PIDs, 128 MiB cgroup memory, and 0.5 CPU. Only its own uniquely named container
is forcibly removed on cleanup, including a failed/timed-out probe.

Local evidence: Colima Docker28.4.0, linux/amd64 image
`sha256:dad5ba2223cbb389a20e8fd47e5efb8841894852359e8ce03fc19c6d7f729519`
with Node24.16.0, running under local architecture emulation. Reads of cgroup-v2
files confirm memory.max134217728, cpu.max50000/100000 and pids.max32.
An external-Buffer allocation loop triggers **OOMKilled=true, exit137, exited**;
this is stronger than merely observing a nonzero process exit. Cleanup verification
found no remaining `brain-script-probe-*` container.

The negative control deliberately demonstrates that `/bin/echo` child spawning
**still succeeds** with these settings. Its PASS means the gap was reproduced,
not that process confinement passed. No reviewed syscall/exec/descriptor boundary,
JS capability facade or parent broker exists. This image is an experiment input,
not a selected/published worker runtime. This does not meet D06 hosted VM isolation
or license/reproducibility/release gates. No container is deployed as a service.

## Pure request preparation (inactive)

`PrepareRequest` checks UTF-8 source byte size and server-supplied timeout and
operation ceilings, normalizes omitted limits, and hashes the exact source plus
contract version, normalized limits and dry-run flag. Its returned value contains
no source text. It performs no compilation, authorization, quota reservation,
operation validation, provider call or execution. In particular, `DryRun` here is
only fingerprinted intent, **not an implemented dry-run broker**. Fingerprints are
not receipts or authority; tenant/principal/endpoint scoping and replay/output
authorization remain integration-owner responsibilities. No HTTP/MCP caller or
public capability is added by this helper.

## Minimal embedded-runtime investigation (not a worker)

The rejected native Node filter experiment is superseded **only as an experiment**
by `TestQuickJSNativeConfinementProbe`. QuickJS source is not vendored, linked to
Brain, automatically downloaded, installed, or selected as a production dependency.
The opt-in test checks the official `quickjs-2026-06-04.tar.xz` SHA256
`b376e839b322978313d929fd20663b11ba58b75df5a46c126dd19ea2fa70ad2a` and requires an
already-present immutable compiler image and local Unix Docker socket:

```
BRAIN_QUICKJS_PROBE_ARCHIVE=/absolute/path/quickjs-2026-06-04.tar.xz \
BRAIN_SCRIPT_LINUX_PROTOTYPE_HOST=unix:///absolute/path/docker.sock \
BRAIN_QUICKJS_PROBE_IMAGE=sha256:<installed-image> \
CI=1 go test -race ./internal/scriptexec -run TestQuickJSNativeConfinementProbe -count=1 -v
```

Trusted compilation uses an isolated container with no host mounts, no network,
read-only root, bounded tmpfs and dropped capabilities. Input copies use stdin
into tmpfs: Docker `cp` refuses read-only rootfs even for this tmpfs destination;
tar uses `--no-same-owner` rather than granting CHOWN. The compiled probe runs as
uid65534 with an empty environment. Only its uniquely named container is removed.

The C harness embeds the core engine **without quickjs-libc**, initializes it before
sealing, then parses/evaluates async JS after a deny-default architecture-checked
seccomp TSYNC filter is installed (any nonzero installation result fails closed).
Reads allow fd0; writes allow fd1/fd2; mmap allows only private anonymous,
non-executable memory. Arbitrary filesystem opens/writes, socket creation, fork,
read/pread/readv/dup of a deliberately retained test descriptor, file-backed mmap
and executable mmap are native probes, not merely missing-JS-API checks.
The original unsealed harness returned async42 and errno0 for all ten probes;
the sealed harness returns async42 and EPERM1 for each, on local Linux arm64.
`TestQuickJSNativeAddressSpaceProbe` additionally injects native allocation attempts
immediately after the same seal: a 4KiB anonymous mapping succeeds, while a 128MiB
mapping fails with ENOMEM under an irreversible 64MiB `RLIMIT_AS`. Without that
limit the large mapping succeeds. This tests native address-space enforcement,
not just the engine's heap accounting, on the observed Linux platform only.

This is **not confinement certification or execution availability**. The retained
test descriptor is intentional adversarial input, not an approved worker launch
policy. Real descriptor closure, broker/frame IPC, script API, source/result/log
budgets, compile and hard CPU limits, broader memory-pressure cases, termination/
reaping, aggregate admission and native-compromise review still need evidence.
QuickJS heap/stack limits alone do not prove these. The compiler container's 512MiB
limit is not a claimed per-worker policy. No macOS native-memory proof or D06 VM
acceptance follows from this Linux test; all script routes remain unavailable.

### Experimental build provenance (not production runtime approval)

`testdata/build-quickjs-probe.sh` fixes the trusted compiler command, locale,
source-date epoch and flags for both probe and worker (including the native
supervisor fixture). It enables PIE, full RELRO/BIND_NOW, non-executable stack,
strong stack protector and FORTIFY_SOURCE=3. `TestQuickJSExperimentalBuildHardening`
checks the **actual ELF** headers/dynamic flags and referenced stack/fortify symbols,
not just flag text. The previous build lacked BIND_NOW and both checked symbols.
`TestQuickJSExperimentalBuildReproducible` builds in two distinct source/output
directories, compares complete binary bytes, and records the compiler/image/hash.
With installed image `sha256:363e1587494626837fa7f9a23bdb453d13b0ff3c67c705c2805cfc69c2d2fad7`
and Debian GCC `12.2.0-14+deb12u1`, the build-policy checkpoint artifacts had SHA256
`6aaa62426a2e49a2a12bfdcd7e7e65e7a49b619f9a48ce9a177d92bdb6560800`.
Source archive verification remains mandatory before compilation. This proves
local relocation reproducibility only, not a second independent builder, complete
supply-chain audit, a reviewed runtime version or native-compromise resistance.

## Inactive framed embedded worker experiment

`TestQuickJSWorkerFramedAsyncCalls` builds `testdata/quickjs_worker.c` against the
same checksum-pinned source in the same opt-in isolated compiler container. This
is an actual fresh JS child receiving submitted source over framed stdin, making
two `brain.entries.get` calls over framed stdout/stdin, then returning async JSON
result 42. The parent supplies fixture objects only: no Brain service, HTTP,
credentials, authorization adapter, audit or publication path is connected.
The source may use explicit `return` or a JavaScript completion expression.
The inactive facade now declares all105 public TypeScript method names (104 wire
operations plus the entries iterator). Namespace objects are frozen with null
prototypes. Only `entries.get(id[, undefined])` retains its fixture-only framed
exchange; other methods fail with a fixed `unsupported_operation` before inspecting
arguments. Nonempty string IDs are required without coercion; transport options
and extra arguments fail with `invalid_arguments`. These fixed exceptions have
null prototypes and contain no submitted content. All methods remain unavailable
to real clients. This is name/denial parity, **not full service/argument/default
parity**, async Promise-return parity, approved script support or a broker registry.
There is no generic request/HTTP/identity/rebind function in the facade. Further
service bindings require approved per-operation preflight and authority contracts.
QuickJS remains an experiment input,
not a selected production dependency or installed worker command.

`TestQuickJSWorkerCompletionSemantics` exercises actual sealed children with
top-level await, object/template/comment boundaries, nested return, block completion,
explicit return/ASI, and final promises/thenables. Compilation first uses QuickJS's
async global completion grammar; only a **compile-only** rejection tries function
body grammar for top-level return. Neither parse executes a Brain call. A runtime
failure after one fixture call never reevaluates the source, and a syntax error
after a syntactic call executes zero calls. Final values are awaited through a
retained intrinsic async adapter before JSON serialization; rejected/unresolved
promises do not silently become `{}`. Undefined completion is refused as before.
The same existing CPU/heap/wall bounds cover compilation, jobs and serialization.
This is still fixture-only evaluation, not protected result release or dry-run.

### Serialization reentrancy and inactive console quarantine

Independent report `va815e0d` rejected `2228f1f1`: terminal sequence was captured
before JSON serialization, whose getters/toJSON may perform valid brokered calls.
`TestQuickJSSerializationThroughParent` reproduces those failures through the real
parent `ProtocolSession`, rather than checking only result bytes. The worker now
serializes once, drains jobs queued during terminal serialization, then constructs
the envelope in C with the current sequence. No JS callback runs while assigning
the envelope sequence or writing it. Getter/toJSON calls remain supported; JSON's
own synchronous treatment of an async toJSON return (a Promise serializes as `{}`)
is preserved. Symbol/function/undefined top-level output is refused, not a success
frame missing `payload`. Tests cover nested calls/logs, exact100/overflow101 calls,
exception-after-call, deferred async effects and no second terminal outcome.
An outstanding unhandled Promise rejection also refuses terminal success (without
retaining its reason); a rejection handled during the job turn is allowed. This
uses the pinned engine's rejection/handled notifications, not only the pending-job
return code, which can report success while a callback's Promise was rejected.

Experimental `console.debug/info/warn/error/log` sends structured JSON arguments
through acknowledged `console.log` IPC calls, never stderr/ambient host logging.
Limits are 32 records, 8192 encoded argument bytes per record, 16384 total; console
also consumes the conservative100-call protocol budget. Serialization callbacks
run once. Reentrant logs must satisfy the bounds again after callback completion.
Native tests cover count/byte floods, cycles and getter CPU loops.

The parent `outputQuarantine` independently validates limits/shape/duplicate keys,
clones bytes, tracks a bounded trusted-parent-only execution-wide source union,
redacts ordinary formatting/JSON and overwrites owned byte buffers on retirement.
It has **no release method**, persistence, logger or authorization callback. Real
S09/S17 source-wide release remains unavailable; the fixture only inspects its
private state. Clearing owned buffers is not a promise to erase Go heap copies,
source-ID strings, kernel pipes or bytes already legitimately released elsewhere.
Structured source-aware exception delivery is still pending; raw exceptions remain
discarded and worker exit statuses are not a durable operation outcome journal.

### Inactive local aggregate admission

`localWorkerPool` bounds active workers globally (maximum64), per tenant and per
tenant/principal binding, plus a bounded waiting queue (maximum1024). Each slot
reserves the **same fixed launch policy** until its owning callback has completed
Wait; cancellation/timeout does not refund capacity while that callback is still
joining. With the current experimental policy N slots account for at most N ×
64MiB worker address-space ceilings and N simultaneously running CPU consumers;
this excludes parent/launcher/container overhead and is not a host cgroup budget.
No per-request variable weights or unproven worker limit is admitted by this claim.

Selection rotates eligible tenants, then principals, preserving FIFO among equal
choices; a saturated binding does not block another eligible binding. Fairness is
for requests admitted to this bounded local queue, not an anti-abuse admission
guarantee if one caller fills it. Key strings are descriptive, not authority.
Construction starts no goroutine/process/scan. Close refuses waiting/new work,
cancels active work and joins it; a timed-out Close retains occupancy and a later
Close can finish joining. Tests include a real subprocess killed and Waited before
Close returns, canceled queued work, exact capacity, independent tenants, rotation
and repeated race runs. A GOMAXPROCS1 regression closes the pool from a running
callback before AfterFunc can run: retirement is checked synchronously after the
callback too, so this cannot return late success. The callback must own
process/scratch/lease cleanup.

No production caller, graph lease adapter, cross-server reservation, rate limiter
or principal resolver is connected. Those require the ledger's C/D/G allocations;
local counters must never be presented as authoritative multi-server quota.

Before receiving/compiling source it closes descriptors 3+ with `close_range`,
sets hard/soft CPU to one second, and applies the experimental 64MiB address-space
and syscall seal. Worker framing/source/result ceilings are 64KiB/32KiB/64KiB;
its defensive call ceiling is 100 (the parent must independently enforce all
budgets and authority). Tests observe normal EOF/Wait and refusal of oversized
source/result, cyclic result, syntax errors, heap exhaustion and dynamic import.
An infinite JS loop exits 137 around one second without requiring the outer
five-second test deadline. This is local Linux evidence, not native-compromise
certification. Negative control: removing only the worker's CPU `setrlimit` made
that case fail at the outer five-second deadline; restoring it returned exit137
in 1.04s. The restored four QuickJS experiment tests pass under Go's race detector.
This is not
macOS proof, D06 VM acceptance or an authorized dry-run broker.
Compile is inside the OS CPU/AS bounds, but a dedicated adversarial compilation
corpus, production parent-controlled cancellation/reaping,
source-aware output fences, aggregate limits and all production launch/descriptor
review still remain. A compromised worker can forge its own frames; only parent
validation and authorization may determine operations or release protected output.

`TestQuickJSWorkerWallDeadlineKillsAndReapsBlockedIPC` exercises the additional
test-only `--supervise` launcher. It forks one fresh child with no service authority,
sets a two-second wall alarm outside that child, kills it if it blocks waiting for
a fixture reply, and reports the actual `waitpid` outcome. RED hit the outer
five-second Go deadline. GREEN reported `timed_out=true,reaped=true,signal=9`
without that deadline firing. This establishes this local Linux blocked-IPC wall
case only, not production coordinator cancellation, supervisor death, graph/store
shutdown, stderr/log budgets, fair admission or a macOS launch contract.

The experimental supervisor now installs Linux `PR_SET_PDEATHSIG(SIGKILL)` in
its child before any source input, checks `getppid()` against the pre-fork parent
PID to close the installation race, and then seals the child (which cannot clear
the signal). `TestQuickJSWorkerSupervisorDeathReapedByObserver` uses an external
native subreaper: it waits for an actual worker call, SIGKILLs only the supervisor,
keeps worker stdin open, and actually waitpid's the adopted worker. RED hit the
outer deadline with an orphan still blocked; GREEN repeatedly observed signal9
and ECHILD after reaping. This proves that Linux fixture, not production init/
subreaper deployment or macOS parent-death cleanup. Direct unsupervised fixture
launches do not acquire this guarantee.

SIGTERM to the experimental supervisor requests cancellation, kills the worker,
and waits before exiting143. An external subreaper test confirms no worker was
orphaned to it; supervisor diagnostics report `cancelled=true,reaped=true,signal=9`
without wall timeout. The handler records cancellation even before the forked
PID is published. This is still test-only Linux behavior, not graph shutdown.

The compilation/fresh-state corpus additionally exercises 12,000-level parser
nesting, runtime `Function` compilation, recursive accessor serialization,
infinite `toJSON`, and endless promise jobs. Each is refused within the worker's
existing bounds with no result bytes; no outer five-second deadline is needed.
A 32,000-byte comment plus `return 42` succeeds, and two actual fresh children
cannot see each other's global marker. These are concrete adversarial cases,
not a complete parser/runtime vulnerability audit or a production binary review.

References: [QuickJS C API](https://bellard.org/quickjs/quickjs.html#QuickJS-C-API),
[official release](https://bellard.org/quickjs/),
[seccomp architecture/TSYNC/allowlist semantics](https://man7.org/linux/man-pages/man2/seccomp.2.html).
