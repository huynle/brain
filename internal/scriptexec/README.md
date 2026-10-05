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

Still required before use: message direction/state validation, per-operation
schema decoding and allowlisting, operation count/log/result budgets, cancellation
and pipe deadlines, aggregate admission, authority/output/publication fences,
durable audit, worker process lifecycle, selected runtime and Linux/macOS resource
and confinement proofs. Nested payload keys are deliberately not interpreted by
the envelope codec. The broker must validate them, not dispatch raw JSON.

A passing codec/fuzz test proves none of OS isolation, worker reaping, provider
safety, authorization or hosted VM readiness. There is no execution fallback.

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
