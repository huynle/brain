# Public SDK checkpoint

**Partial implementation, not V1 completion. Scripts are unavailable.**

The reviewed public protocol lives in `api/openapi.yaml` (OpenAPI 3.1).
Current typed operations: health get; entries list/create/get/update/delete/move/
bulk-update/bulk-delete; search; tasks list/get; sections list/get; graph
backlinks/outlinks/related; attachments upload/list/get/delete/download/text/
extract/for-entry/attach/detach; goals list/create/update/delete/progress/audit/run
(34 operations), plus eight reminder, nine attention and seven webhook operations
(58 total), plus automation run/history/get, task waiting/blocked/ready/next,
project list, graph orphans and observability stats/stale (69 total).
The remaining inventory is in
`docs/sdk-operation-matrix.md`. Task creation and dependency updates use typed
entry requests. No capability route, hosted adapter or script route is added.

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
server-cursor guarantee is claimed. Broader namespaces remain work.

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
