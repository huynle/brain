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
