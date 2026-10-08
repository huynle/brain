# Public SDK checkpoint

**Partial implementation, not V1 completion. Scripts are unavailable.**

The reviewed public protocol lives in `api/openapi.yaml` (OpenAPI 3.1).
All 104 original inventoried operations, 21 runner/dispatch/control/scheduler
operations (`runners.*`, `dispatch.*`, `control.*`, `tasks.dispatchLease`,
`tasks.placementReasons`, `scheduler.status`), 20 operator/supervision
operations (`monitors.*`, runner candidates, `clientContext.resolve`, `sync.*`,
`control.sessionTail`/`sessionDescendants`, `supervision.*`) and capability
discovery have typed Go/TypeScript adapters, including
task/feature actions, metadata, project placement/deletion, delivery state,
finite events, timeline and SSE. Exact evidence and remaining integration gates are in
`docs/sdk-operation-matrix.md`. Task creation and dependency updates use typed
entry requests. Discovery is single-mode only; no hosted adapter or script route is added.

Negotiate explicitly with `client.Capabilities(ctx)` (Go) or
`await client.capabilities({signal})` (TypeScript). The required OpenAPI contract
version is1.0.0, distinct from the SDK package version0.1.0. Discovery reports
sorted wired-operation IDs, not permissions or resource existence. All five script
flags are false. Old servers (404/501) fail `unsupported_server`;401/403 fail
`capability_auth_required` without anonymous fallback. The manifest is bounded to
64KiB and never cached. See `docs/sdk-capability-proposal.md` for exact semantics.

## Generation and validation

```sh
npm ci --ignore-scripts --prefix sdk/typescript
go generate ./sdk/brain
npm --prefix sdk/typescript run generate
npm --prefix sdk/typescript run validate
go test ./internal/sdkcontract/... ./sdk/brain
git diff --exit-code -- sdk/brain/schema.gen.go sdk/typescript/src/schema.gen.ts
```

Pinned generators: oapi-codegen **v2.8.0** and openapi-typescript **7.13.0**.
oapi-codegen v2.5.1 cannot represent the nullable OpenAPI 3.1 schemas and is not
used. `internal/sdkcontract/bootstrap` only prints candidate schemas for human
review when the server wire shape changes; it does not overwrite OpenAPI.
The parity test detects DTO drift. Neither public SDK imports server internals.
The Go wire-shape checker is deliberately limited; Redocly validates OpenAPI
structure. Neither is a replacement for handler/service input validation.

## Go usage

```go
client, err := brain.New(brain.Config{BaseURL: origin, Token: token})
if err != nil { return err }
defer client.Close()
entry, err := client.Entries().Get(ctx, entryID)
```

Import `github.com/huynle/brain-api/sdk/brain`. See `sdk/examples/go` for an
executable task create/read/revision-update example. It reads `BRAIN_API_URL`
and `BRAIN_API_TOKEN`, creates a temporary task in `sdk-example`, and attempts
cleanup. Run only against an installation where those writes are intended.
The integration test runs it against isolated real services/SQLite and the real
authenticated router with a real stored token, not mocked service responses.

Clients bind origin/credential/optional tenant/auth-generation for their lifetime.
Tenant is a selector only; omit it in single mode. `Rebind` retires the previous
client and cancels its requests. The current client stores no response cache.
`Close` also retires a binding. Custom transports are trusted caller code.
HTTPS is recommended; HTTP remains supported for existing local installations.

Responses are bounded (8 MiB default, configurable up to 64 MiB), requests have
a 30-second default timeout, and redirects are refused. Default error formatting
does not include server code/content or credentials. `Error.Code`, `Error.Message`
and `Error.Details` (TypeScript: `code`, `serverMessage`, `details`) are separately
available to applications that deliberately inspect server errors. The legacy
`error`/`message`/`details` wire envelope is adapted without REST changes. A
well-formed optional machine code is retained; otherwise HTTP status determines
the stable SDK code. X-Request-ID takes precedence over optional body request_id.
If the legacy response omits a nonempty `message`, a string `error` supplies the
explicitly inspected message (including dispatch conflicts); it is still excluded
from default formatting. Non-string error values are not coerced into content.

No SDK write retry is performed, including after uncertain transport outcomes.
The Go transport's implicit buffered-POST replay is disabled. An idempotency
header does **not** give legacy REST writes server-side deduplication. Do not
retry an uncertain mutation merely because it carried a key. No automatic
cross-binding retry, cursor transfer, credential refresh or identity migration
is implemented. Entry iterators (`Entries().Iterate` / `entries.iterate`) snapshot
their filters and stay bound to their original client. They do not interpret the
legacy page-local `total` as a collection count. They stop at an empty page, fail
on `truncated` or inconsistent offsets/limits, and cap a walk at 10,000 pages.
Concurrent server changes can move entries between offset pages; no snapshot or
server-cursor guarantee is claimed.

Event streaming uses `Events().Stream(ctx, filters, lastEventID, callback, options)`
or `events.stream(filters, callback, {lastEventId, signal})`. It is bounded per
frame by the response-byte limit and by the client timeout/lifetime. Callback
errors stop delivery and close the stream; there is no automatic reconnect.
Replay is best-effort from a volatile server buffer, not a durable or gap-free
cursor. The finite `events.wait` cursor instead binds exact filters and caller;
handle its `cursor_expired`, `truncated`, `shutdown` and `timed_out` flags explicitly.

Task/feature actions can return HTTP 200 with a no-op or partial result; inspect
`resumed`, `triggered`, `dispatched`, reasons and per-task results. Feature cancel
only stops future dependent-chain dispatch, not already-running work. Checkout
creates an indexed task; pass `merge_policy: "prompt_only"` and `delivery_mode:
"none"` when Git delivery must not be requested. Project deletion requires the
exact project name as confirmation and deletes entries of every type. Delivery
verification is separate from implementation status and can return a provider
error inside a successful response; configuration uses `expected_revision`.

Dispatch dials (`Dispatch()` / `dispatch`) write server-wide state and notify
runners; resuming releases queued work. `RemoteControl()` / `control` is code execution
on runner hosts (control:* scope): prompts and granted permissions drive a remote
agent, spawn/kill start and stop processes. Proxied session calls return the
instance's own JSON (`json.RawMessage`; TS `null` for an empty 204 body). None of
these are script-exposed. Snooze timestamps (`remind_at`, `snoozed_until`) are
plain strings in both SDKs: the server validates them and owns the error text.

Monitors (`Monitors()` / `monitors`) create tasks that run agents when they fire.
`Supervision()` / `supervision` reads bounded supervisor views and submits
idempotent prompts, contextual resumes and triggers; its checkpoint and budget
ledgers gate those operations. `Sync()` / `sync` reads browser-reported state and
queues reconciliation commands (202; the browser writes after it reconnects).
`ClientContext().Resolve` writes the client registry. Session tail/descendants
are control:* reads of runner hosts. In Go the supervisor, checkpoint and budget
command documents are `json.RawMessage`, and `Tasks().SendDeliveryCommand` sends a
delivery command document as given: the server decodes them strictly and is the
only validator (unknown fields and wrong types are refused with its message).
None of these are script-exposed.

Attachment upload takes bytes, never a filesystem path. Filenames cannot contain
path separators or CR/LF/NUL. Uploads (including multipart overhead) and downloads
use the configured response-byte limit; these are bounded-buffer methods, not
unbounded streaming APIs. Link and unlink require a non-empty role. Extraction
invokes the existing provider path; transport tests do not prove provider success.
The real local integration covers eight attachment operations with actual blob
storage, excluding successful extraction and stored-derived-text retrieval.

The TypeScript package contains matching generated types and an ergonomic ESM
client. `BRAIN_SDK_NODE_INTEGRATION=1 go test ./internal/sdkcontract -run
TestExternalClients -v -count=1` tests an offline-installed npm tarball and an
isolated Go consumer module against the authenticated real handler. Run npm
build first. Package licensing/publishing requires owner disposition: no repository
license was found, so no MIT grant is invented.
