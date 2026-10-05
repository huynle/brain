# @huynle/brain-sdk (checkpoint)

Node 22+ ESM package with generated OpenAPI 3.1 DTOs and a bound HTTP client.
This checkpoint covers health, entry CRUD/list/move/bulk, search, task get/list,
sections list/get, graph backlinks/outlinks/related, and ten attachment operations
and seven goal operations, eight reminder operations and nine attention operations
and seven webhook operations plus automation run/history/get (61 total).
Upload takes Uint8Array, never a path, and bounds encoded multipart
size using maxResponseBytes. Download returns bounded Uint8Array. Link/unlink
require a non-empty role. Extraction success depends on server provider configuration.
It does not expose script execution or hosted authority.

```js
import { BrainClient } from "@huynle/brain-sdk";
const brain = new BrainClient({baseUrl: "http://localhost:3333", token});
try {
  const entry = await brain.entries.get(id, {signal});
} finally { brain.close(); }
```

Use `rebind(config)` to retire a client and cancel its old requests. Configuration
is copied, not shared with the caller. `tenant` selects an org but grants no
membership; omit it in single mode. Requests have a 30-second default timeout and
an 8 MiB response bound (up to 64 MiB configurable). Redirects are refused and
writes are never automatically retried. Legacy endpoints may ignore idempotency
headers; a key is not a deduplication guarantee. Default error text omits server
content; `BrainError.serverMessage` exposes it only for deliberate use.

```sh
npm ci --ignore-scripts
npm test
npm run typecheck
npm pack --ignore-scripts
```

The package has no runtime dependencies. Custom `fetch` is trusted application
code and must respect cancellation and manual redirects. `../examples/node`
contains an external installed-package example that writes a temporary task and
attempts cleanup. Publishing is not performed; licensing requires owner review.
