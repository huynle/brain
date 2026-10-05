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
