# Capability discovery contract — single-mode SDK support

Allocation **A** was explicitly approved by the parent/user on 2026-10-06.
`GET /api/v1/capabilities` now exposes the OpenAPI `CapabilityManifest` through the
ordinary authenticated read-scope group in single mode. Existing auth-disabled
local operation is retained. Multi/unknown modes do not register the route; the
tenant read-only allowlist is unchanged. This is discovery, not script activation.

Bounded JSON (maximum64KiB; exact fields, no resource/principal metadata):

```json
{
  "contract_version": "1.0.0",
  "operations": ["capabilities.get", "health.get"],
  "scripts": {
    "compiled": false,
    "configured": false,
    "deployment_available": false,
    "caller_authorized": false,
    "available": false
  }
}
```

`contract_version` matches OpenAPI info.version (1.0.0), not the package version
(0.1.0) or server build. `operations` is sorted, unique and contains only the public
contract IDs whose handler dependencies are wired; a minimal server reports only
discovery and health. A fully composed server reports105 IDs. This is NOT caller
authorization, resource existence, or readiness of providers/remote runners.
No service method is invoked to build discovery. Unknown future IDs may be
described without becoming client/script methods. The four script dimensions
remain independent and `available` equals their conjunction. All are currently
false: no production runtime, script configuration, supported deployment composition
or dedicated caller permission exists. Neither admin nor legacy OAuth mcp grants
script execution. No historical capability/schema vocabulary changes.

Both public SDK decoders reject missing/null/wrong-typed fields, duplicate/escaped-alias keys,
duplicate operation IDs, invalid UTF-8, oversized bodies, trailing JSON and
inconsistent flags.404/501 yields `unsupported_server`;401/403 remains
`capability_auth_required`, never anonymous or old-server fallback; other non200
statuses yield `capability_discovery_unavailable`. Valid mismatched contracts yield
`incompatible_contract_version`; malformed manifests yield
`invalid_capability_manifest`. Errors contain fixed codes only, not server bodies.

Use `client.Capabilities(ctx)` in Go or `await client.capabilities({signal})` in
TypeScript. Negotiation uses the existing immutable authenticated transport,
cancellation and redirect refusal; no cache, anonymous retry or old-server
fallback. Server responses use `Cache-Control: no-store`. Each actual operation
still requires current server-side authorization. All32 flag combinations and
refusal cases are tested in both public implementations; all-true fixtures merely
test decoding claims, never enable execution. Earlier unexported prototype decoders
remain historical test fixtures, not the active SDK contract source.
