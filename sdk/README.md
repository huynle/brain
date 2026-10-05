# Public SDK checkpoint

**Partial implementation, not V1 completion. Scripts are unavailable.**

The reviewed public protocol lives in `api/openapi.yaml` (OpenAPI 3.1).
Current typed operations: health get; entries list/create/get/update/delete;
search; tasks list/get. The remaining inventory is in
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
does not include response content or credentials. `Error.Message` is separately
available to applications that deliberately display server errors.

No SDK write retry is performed, including after uncertain transport outcomes.
The Go transport's implicit buffered-POST replay is disabled. An idempotency
header does **not** give legacy REST writes server-side deduplication. Do not
retry an uncertain mutation merely because it carried a key. No automatic
cross-binding retry, cursor transfer, credential refresh or identity migration
is implemented. Pagination iterators, attachments and broader namespaces remain
required work; this checkpoint does not claim them.

The TypeScript package currently contains generated types and pinned tooling;
its ergonomic client is the next increment. Package licensing/publishing requires
owner disposition: no repository license was found, so no MIT grant is invented.
