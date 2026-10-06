# Script execution building blocks (disabled)

This package is not an execution service or a sandbox. No caller or route is
wired to it. SDK script exposure remains false. The approved implementation plan
is Brain `qfcda7ct`, amendment `SDK-TENANCY-20261005`.

The prototype codec uses a four-byte big-endian length followed by one JSON
envelope: `version`, `kind`, `sequence`, `payload`. It models `call`, `result` and
terminal `error`. Envelopes are capped at 1 MiB before payload allocation;
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

## Linux-first production launcher (disabled by default) — G

`launcher.go`/`launcher_linux.go` are the LINUX-FIRST-20261006 launch policy
for the sealed QuickJS worker. The zero `launcherConfig` refuses
(`errLauncherDisabled`); any non-Linux OS, including native macOS, returns
`errLauncherUnsupported` even when explicitly enabled. There is no degraded
fallback, privileged helper, entitlement, route, config key or caller.
`launcherAvailability` gives a content-free reason (`disabled`, `misconfigured`,
`unsupported_platform`, `configured`); `configured` is not execution availability.

Each `run` does the following:

1. **Pinned binary:** open the configured absolute path with `O_NOFOLLOW`.
   Require a regular executable owned by root or the service euid, with no
   setuid/setgid and no group/world write. Hash that descriptor against the pinned
   SHA-256, then exec `/proc/self/fd/<n>` so a path swap cannot substitute
   another binary.
2. **Launch:** empty environment, cwd `/`, only fds 0–2, parent-death SIGKILL on a
   locked thread (`runWorkerProcessWithStart`), stderr counted and discarded at
   64KiB, stdout wire budget 1MiB, source ≤32KiB, wall deadline ≤30s.
3. **Parent-side attestation before any source byte:** `/proc/<pid>/status`
   must show `NoNewPrivs:1`, `Seccomp:2` and `Seccomp_filters` strictly greater
   than the launcher's own. Docker/systemd filters are inherited by every child,
   so `Seccomp:2` alone proves nothing; the observed sealed worker in Docker
   reports 2 filters. Then `/proc/<pid>/limits` must show CPU ≤1s and address
   space ≤64MiB (soft ≤ hard, never unlimited), `/proc/<pid>/fd` exactly {0,1,2},
   and `/proc/<pid>/exe` the pinned inode. The worker seals last and the seal
   denies `setrlimit`/`prctl`/further filters, so this is its final state.
   Failure or a 1s attestation timeout → kill, Wait, no source written.
4. **Lifecycle:** caller cancellation, wall deadline, protocol/wire/diagnostic
   violation or attestation failure all kill and Wait before `run` returns.

Confinement comes from the worker's own deny-default seccomp seal plus kernel
rlimits, not from containers or namespaces (unprivileged user namespaces are
commonly unavailable under Docker/AppArmor and are not required). Filesystem
(`open`/file mmap), network (`socket`), process creation/replacement (`fork`,
`clone`, `clone3`, `execve`, `execveat`), descriptor reuse, executable memory and
limit/parent-death changes are all denied in the sealed child. `RLIMIT_AS` is
kernel-enforced on Linux (prior native probe: 128MiB mapping → ENOMEM), unlike
the macOS counterexample above.

**Real Linux evidence (kernel 6.8 arm64, Colima, uid 65534, non-race
cross-built parent):** `TestQuickJSLauncherLinux` → `TestNativeLauncher` 10/10.
The launch pin is taken from an INDEPENDENT relocated rebuild with the same recipe
(`401a66247dfad878dd68be039851274eeae05921838f9d872b1fc3068e64abca`), and the
shipped artifact must equal it byte-for-byte.
Pinned exchange returns 42 with attestation `{NoNewPrivs, Seccomp 2, filters 2,
cpu 1/1, as 64MiB/64MiB}`. Pin mismatch, a world-writable copy and a symlink are
refused before start. An unsealed, correctly pinned `/bin/cat` is refused at
attestation (~1.0s), killed and Waited with zero frames accepted; in a mutation
run that ignored attestation, the source reached cat and was echoed back. A
worker blocked on IPC is killed at the 500ms wall and Waited. Caller cancellation
kills and Waits. A `for(;;){}` loop is killed by the attested RLIMIT_CPU before
its 5s wall. A 1MiB-ArrayBuffer flood ends as a `script_failed` frame (exit136)
in ~13ms through QuickJS's 16MiB heap limit, which fires before the OS ceiling.
Kernel enforcement of the attested 64MiB `RLIMIT_AS` under the same `seal()` is
freshly re-observed by `TestQuickJSNativeAddressSpaceProbe`: 4KiB map succeeds,
128MiB map gets ENOMEM. `TestQuickJSNativeChildExecDenied`: the same probe without its seal
really execs `/bin/echo` (the container-only gap reproduced); sealed, `execve`,
`execveat`, `clone`, `clone3`, `PR_SET_PDEATHSIG` and `RLIMIT_AS` raises all
return EPERM.

**Supported topology (single Linux server):** brain-api runs under systemd
(`KillMode=control-group`, plus an outer `MemoryMax`/`TasksMax` for
API+workers) or as a container with an init/subreaper PID 1 (`--init`/tini),
because a dead parent cannot reap its own killed child. Linux ≥5.9
(`Seccomp_filters`), arm64 or x86_64 (only arm64 observed). The worker is
installed read-only and root-owned, and its digest is pinned in configuration.
Aggregate admission is `localWorkerPool` (N × 64MiB AS, N CPU); it is not a
multi-server quota. Hosted multi-tenant execution still requires D06 VM
isolation.

**Release packaging, concurrency and reaping (2026-10-07):**

- **Release source:** the worker source is now release source in
  `runtime/script-worker/` (`worker.c`, shared `seal.h`, `build.sh`,
  `release.json`, install docs). The confinement probe compiles the same
  `seal.h`.
- **Recorded pin:** `release.json` pins the source archive, the compiler image
  and GCC version, and the expected output `linux/arm64 =
  f81221bb56c904862207676316342a7a307457be538987954b2589aeb7fd1972`.
  `TestQuickJSLauncherLinux` fails unless an independent relocated rebuild
  reproduces that committed digest (observed in three separate builds plus
  `TestQuickJSExperimentalBuildReproducible`).
- **Artifact reuse:** tests may reuse a prebuilt artifact through
  `BRAIN_SCRIPT_WORKER_ARTIFACT`, accepted only when its SHA-256 is recorded in
  `release.json`. The independent pin rebuild still compiles from source.
- **Concurrency:** `TestNativeLauncherPool` (inside the Linux wrapper) runs two
  simultaneous sealed workers held mid-call through `localWorkerPool`
  (global 2, principal 1). A third is queued and doesn't start; cancelling the
  first kills and Waits it, and the queued run completes 42 in the recovered
  slot. The second then completes 42, a fourth run reusing the cancelled
  principal completes 42, `close` joins, and no child process remains.
- **Init reaping:** `TestQuickJSInitReaping` SIGKILLs only the server process
  (a Go test stand-in holding a sealed worker mid-call).
  - With PID 1 = `/bin/sleep` (no init), the worker ends as zombie `Z`:
    PDEATHSIG killed it, but nothing reaps it.
  - With `docker --init` (PID 1 = `docker-init`), it is `gone`.
  - Local Colima VM with systemd 255 as PID 1, transient unit with
    `KillMode=control-group`: the sealed worker (filters 1, NoNewPrivs 1) is
    `gone` after the main PID's SIGKILL, the unit ends `failed/result=signal`,
    and no unit or process is left.
- **Process name:** the worker's `comm` shows the descriptor number (e.g. `6`)
  because it is executed through `/proc/self/fd/<n>`. Identify workers by
  parent PID or exe inode, not by name.
- **Caller contract:** `run` returning nil means only that the worker exited 0
  with intact framing. Callers must still require a ProtocolSession terminal
  frame. `launchReport.sourceWritten` is set only after the source write
  succeeds.

**x86_64 is blocked (exact):**

- **Reproducible build: done.** It is not recorded as a pinned output because
  its compiler image differs from the arm64 one. Two relocated builds are
  byte-identical, `9564f7a70dcff1c9f692b8ed59bcd0d8973f4e25bffbc175894bab8969fdd552`
  (x86-64 PIE). They were built under user-mode emulation, which is fine for
  compilation, with image
  `sha256:dad5ba2223cbb389a20e8fd47e5efb8841894852359e8ce03fc19c6d7f729519`
  and the same GCC 12.2.0-14+deb12u1.
- **Execution on a real x86_64 kernel: not achieved.**
  - Colima/Lima 2.2.0 full-system QEMU TCG (`brain-x86` profile) boots a real
    Ubuntu 24.04 x86_64 kernel, but cloud-init starts only at ~234s of
    guest uptime. Lima's usernet gives up resolving the guest IP after 2 min
    and kills the VM; that timeout isn't configurable.
  - Direct `qemu-system-x86_64` TCG booting an offline-provisioned clone
    (host key and root key written with debugfs, journal replayed first,
    `e2fsck -fn` clean) reaches `ubuntu login:` with `ssh.socket` listening,
    but no SSH session was established within 30 minutes.
  - Next step: run `TestQuickJSLauncherLinux` (with
    `BRAIN_SCRIPT_LINUX_GOARCH=amd64`) plus the probe/child-exec tests on a
    native x86_64 Linux host or KVM-capable runner, then record
    `linux/amd64` from that host's pinned compiler image.

Still not covered here: independent review of the seal/launcher and runtime
selection, the x86_64 execution above, and C–F integration before any route
can use this.

## Approved script policy enforcement (inactive) — SCRIPT-DECISIONS-20261006

`policy.go` is pure code enforcement of the three user-approved product rules.
It has no caller, storage, route, schema or capability effect; approval allocates
no DB1 table/profile, `script:execute` grant, S09 fence or launcher.

- **U1:** `checkProtectedEnvelope` enforces result ≤64KiB, logs ≤16KiB and ≤32
  records, and the whole envelope (result+logs+plan+hex digests) ≤256KiB, all valid
  JSON. Persistable types (`protectedEnvelope`, `contentFreeAudit`,
  `consumedKeyTombstone`) have reflection-guarded exact field allowlists with no
  source/script field. `protectedExpiry` is terminal+24h, interrupted = admission
  deadline+24h; a terminal recorded after the deadline is also capped at
  deadline+24h (deadline-anchored, approved in SCRIPT-DECISIONS-20261006b). No access-time input, so no sliding.
  `auditExpiry` is admission+90d.
- **U2:** `consumedKeyMAC` is HMAC-SHA256 (≥32-byte server key) over a
  domain-separated, length-prefixed tenant/principal/endpoint/epoch/key tuple.
  `decideReplay` never reruns a consumed key: expired, erased, tombstoned or
  unknown state → content-free 409 `idempotency_key_retired`. Before expiry, a
  matching fingerprint returns the stored result (output release still needs
  separate authorization); a mismatch → 409. `idempotency_key_conflict` and `idempotency_key_in_progress` (both 409)
  are approved names (SCRIPT-DECISIONS-20261006b). `tombstonePurgeable` is true only for an exact irreversibly retired
  namespace.
- **U3:** `checkSubmitEligibility` admits only auth-enabled, verified, human,
  exact `owner`/`admin` role with explicit script opt-in. Auth-off/credential-free
  submission is refused (deferred); ordinary auth-off REST is untouched.

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

`validatePlanShape` is separate pure preparation: contiguous indices, object JSON
arguments under existing protocol count/byte/duplicate-key/depth rules, bounded
revision strings, and backward-only unique provisional references. Even a shaped
reference returns `dry_run_dependency_unsupported`: no service-owned provisional
dependency path has been allocated. It does not check operation registry membership,
DTOs, target existence, actual revisions, permissions or service preflight. A test
explicitly demonstrates that a syntactically valid unknown operation is not thereby
supported. There is no mutation or snapshot to roll back.

`summarizeMutationOutcomes` checks only a descriptive sequence of mutation claims:
dry-run `planned` versus real `committed`, and final-only `failed`/`outcome_unknown`.
It rejects mixed modes, continuation after a stop, unknown states and budget overflow,
returning no partial summary on invalid input. Reads are not represented by this
helper. It is not a durable journal, state-transition owner, atomic receipt, replay
decision or proof any write committed. No schema/API or S09/P9 journal interface is
defined or replaced by either helper; both remain uncalled by production.

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

`runtime/script-worker/build.sh` (moved from testdata; see its README and
`release.json`) fixes the trusted compiler command, locale,
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

`TestQuickJSWorkerFramedAsyncCalls` builds `runtime/script-worker/worker.c` (shared `seal.h`) against the
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
to real clients. Ordinary methods return intrinsic Promises, including rejected
Promises for unsupported/invalid arguments; await/then/catch work. The iterator
returns an async generator: first `next()` rejects with `unsupported_operation`,
subsequent `next()` completes; `return()` before iteration closes without rejection.
It does not inspect query/options or issue calls. IPC is still
serial and blocking inside the test worker, not concurrent RPC. This is
name/denial/Promise parity, **not full service/argument/default parity**, approved
script support or a broker registry.
There is no generic request/HTTP/identity/rebind function in the facade. Further
service bindings require approved per-operation preflight and authority contracts.
`testdata/facade-normalization.js` separately exercises pure positional mapping:
101 JSON-shaped methods, root-name/operation-ID aliases, optional query omission,
request/boolean/numeric defaults and an optional **undefined-only** transport slot.
The pinned TypeScript compiler reads the actual public method signatures in the
parity test; binary upload/download, streaming callbacks and the iterator remain
explicitly unsupported. Input data is copied without JSON coercion or evaluating
accessors/toJSON. Invalid types, cycles and non-JSON values are refused with fixed
errors. This mapper is **not installed in `brain`**, never dispatches, and is not
DTO validation, service preflight, authorization or an enabled-operation registry.
The native test evaluates it only as ordinary submitted fixture source and verifies
zero operation IPC and continued denial of `brain.tasks.resume`. Full supported
facade/service composition remains blocked on the approved subset and C–F seams.
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
Terminal error payloads admit only fixed `compile_failed`, `script_failed` or
`result_invalid` codes. Optional line/column hints are integers1..32768 with no
filename/path/text; parent treats them as untrusted hints, not verified locations.
The native worker emits **no location** rather than inspect a submitted exception:
thrown proxies, Error stack getters and rejection objects are never formatted or
queried. Error frames use the current sequence after prior calls, consume parent
budgets, permanently retire the session and remain distinct from successful results
(`WorkerMessage.Failure` versus `Result`). Partial output is never retried. Hard
kills/protocol/limit failures may emit no terminal, so the parent must still join
and classify process failure. No protected-output release or durable operation
journal exists; a fixed worker error proves nothing about earlier service commits.

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

`TestNativeManagedAggregate` now composes this pool with two actual sealed native
children held at a fixture-call barrier after touching 2MiB each. `/proc` measurements
verify simultaneous per-process virtual/resident memory, an independent tenant's
third child returns42 while the pressure tenant is saturated, then both pressure
children are released: heap exhaustion reports `script_failed`; the CPU loop is
SIGKILLed around one CPU second. Both are Waited and maxrss remains below the
experimental64MiB AS ceiling; pool Close joins. This measures these local workers,
not parent/container overhead, a host aggregate cgroup, or multi-server quota.
`TestNativeManagedStartup` adds20 pre-Start refusals and20 cancellations at Go's
post-Start/pre-source stdin-copy boundary; every started child is Waited. It does
not insert hooks inside the kernel fork/exec sequence or prove every prefork race.
Both tests run in the existing non-race Linux cross-built parent fixture; the host
wrapper and local primitives have separate race coverage. No launcher policy changed.

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
