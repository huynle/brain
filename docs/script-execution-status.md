# Script execution: reference and operator status

This is the **inactive V1 prototype**, not an install or enablement guide.
Approved plan: Brain `qfcda7ct`, revision `SDK-TENANCY-20261005`.
The complete acceptance and ownership ledger is [here](sdk-script-acceptance-ledger.md).

## Availability and compatibility

- No REST script-execute endpoint, MCP execution tool, production worker command,
  script audit endpoint, or script configuration switch is wired up.
- SDK coverage of 104 existing REST operations does not grant script access.
  The SDKs are usable against their documented existing endpoints independently.
- No capability-discovery endpoint has been allocated. An existing-server 404 is
  not proof of a disabled but installable script feature. Clients must not attempt
  a local execution fallback or assume a health response grants execution support.
- Do not substitute a tenant runner, direct database connection, arbitrary HTTP,
  or loopback request for the missing authorized broker.
- Linux native tests are local prototype evidence. macOS native-memory confinement
  remains unproven; hosted execution separately requires D06 VM acceptance. Neither
  profile may be enabled based on a passing SDK build or local worker test.

## JavaScript shape currently tested

The fresh test child receives a frozen, null-prototype `brain` root and namespaces.
There is no `request`, `fetch`, identity constructor, rebind, or generic RPC method.
All 105 public TypeScript method names exist (104 operations plus an iterator).
This is **not full argument/default/service parity**.

| Method | Current fixture behavior |
|---|---|
| `brain.entries.get(id[, undefined])` | Nonempty primitive string ID; returns a Promise of a canned parent reply. Extra arguments or transport options reject with `invalid_arguments`; no coercion. |
| `brain.entries.iterate(...)` | Lazy async generator; first `next()` rejects with `unsupported_operation`, subsequent `next()` completes. Closing before the first `next()` performs no operation. |
| All other methods | Return rejected Promises with `unsupported_operation`, before inspecting any arguments. |

No method reaches a real service. Unsupported methods ignore even malformed or
hostile arguments: default processing and schema validation for future supported
methods remain to be implemented. Rejection-before-inspection is intentional, not
evidence that a request would validate when a binding is eventually approved.
SDK transport options (credentials, tenant selectors, signals, headers and
idempotency controls) are not script authority inputs.

Top-level await and explicit return are tested. Otherwise the JavaScript completion
value is returned; a final Promise/thenable is awaited. Compilation fallback never
reruns a script after a runtime error. Results pass bounded JSON serialization:
unsupported top-level values, cycles, excessive nesting or size are refused.
Getters/toJSON may execute script code during serialization; the parent sequence
checks and all limits still apply. This is not arbitrary-object serialization.

Runnable examples live in `internal/scriptexec/testdata/examples/`:

- `read-pair.js`: two awaited fixture reads produce `{sum: 42}`.
- `unsupported-write.js`: catches a fixed refusal; it does **not** create a note.
- `unsupported-iterator.js`: demonstrates refusal through `for await`.

`TestQuickJSScriptExamples` runs those exact files in the sealed native child with
canned parent replies. It does not contact a Brain server. See the package README
for the opt-in, checksum-pinned local test setup; no archive/image is automatically
downloaded and no test flag activates scripts in Brain.

## Limits: experiment, not a deployment promise

| Boundary | Current prototype limit/evidence |
|---|---|
| Source | 32 KiB before evaluation |
| Worker frame / result | 64 KiB; parent generic codec also has an absolute 1 MiB envelope ceiling |
| Calls | 100, including console calls; parent has independently supplied budgets |
| Console | 32 records, 8192 encoded argument bytes/record, 16384 total |
| Parent JSON nesting | 64 |
| Quarantine sources | 1000 IDs, 4096 bytes/ID, 65536 total source bytes |
| Quarantine result | 65536 bytes, no release API |
| Linux worker address space / CPU | Hard 64 MiB / one CPU second in native prototype |
| Experimental supervisor wall time | Two seconds, then kill and wait |
| Local admission | Fixed-policy slots; configured local limits capped at 64 active and 1024 waiting |

Local slot accounting is not a measured host aggregate cgroup budget, distributed
quota, rate limit, or authority resolver. Parent/launcher/VM overhead is not covered
by multiplying worker address-space limits. Actual simultaneous pressure and
launch-race evidence remains on the ledger. Child kill does not imply reaping;
Wait/join and an external orphan reaper remain necessary.

## Errors, effects and output

Fixed worker terminal codes are `compile_failed`, `script_failed`, `result_invalid`.
They contain no submitted exception text or stack. The native worker does not
fabricate source locations; the parent protocol accepts only bounded untrusted
line/column hints. Hard kills, limit exhaustion and broken IPC may produce no
terminal frame. A terminal error is **not a service commit receipt**.

Console records and results are protected content, not ordinary telemetry.
The parent quarantine has no release method: current source-wide authorization
and publication fences have not been composed. Redacted formatting and overwriting
owned buffers do not promise erasure of all heap/kernel copies.

Future real writes must commit individually with durable, authorized outcome
records. Earlier commits are not rolled back by a later failure. Never rerun an
interrupted or ambiguous write solely because the worker returned an error.
There is currently no durable script idempotency/replay service.

Dry-run is **not available**. `PrepareRequest` fingerprints dry-run intent only;
it does not validate mutations or prove zero domain/provider effects. No writer-
then-rollback emulation is permitted. Unsupported writes stay unsupported until
reusable service preflight and effect/authorization contracts are approved.

## Disable and incident handling

Production scripts are already unavailable. Do not invent an enabling config key
or register a tool/route to investigate an incident. No production execution IDs
or script audit records can be queried through this prototype.

For a local opt-in test, interrupt its owning test and verify its uniquely named
container/child cleanup; never stop unrelated containers or use a global prune.
Retain exit status, test name, immutable source/image hashes and non-content
resource measurements. Do not attach source, result bytes, arguments, raw exception
objects, tokens or console content to an ordinary operational log.

If a production integration is proposed, require explicit admission disable,
cancellation, joined worker/lease shutdown, protected-output suppression, durable
uncertain-outcome handling, and reviewed retention/purge behavior first. The
current local helper tests are not an operational recovery procedure for that
future system. Independent review remains pending and must not be replaced with
author-generated PASS claims.
