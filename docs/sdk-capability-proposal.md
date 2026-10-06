# Discovery decoder experiment — proposal, not an allocated endpoint

The revised SDK plan requires capability/version negotiation. Allocation **A** in
the acceptance ledger remains unanswered: there is no discovery route, public SDK
discovery method, or production caller. These pure Go/JavaScript decoders exercise
client refusal semantics ahead of that decision. This is **not a published wire
contract**; the route/auth owner must approve or replace it before integration.

Proposed bounded JSON (maximum64KiB; exact fields, no resource/principal metadata):

```json
{
  "contract_version": "0.1.0",
  "operations": ["health.get", "entries.get"],
  "scripts": {
    "compiled": false,
    "configured": false,
    "deployment_available": false,
    "caller_authorized": false,
    "available": false
  }
}
```

`contract_version` is compared exactly to the client-required contract, not a server
build version. Operation names are distinct syntactic IDs (maximum10000), not proof
of operation authorization. Unknown operations can be described without becoming
client or script methods. The four availability dimensions remain independent;
the draft requires `available` to equal their conjunction. No source IDs, resource
counts or credentials belong in this manifest. These are proposed semantics, not
permission to edit historical capability vocabularies or deployment admission.

Both decoders reject missing/null/wrong-typed fields, duplicate/escaped-alias keys,
duplicate operation IDs, invalid UTF-8, oversized bodies, trailing JSON and
inconsistent flags.404/501 yields `unsupported_server`;401/403 remains
`capability_auth_required`, never anonymous or old-server fallback; other non200
statuses yield `capability_discovery_unavailable`. Valid mismatched contracts yield
`incompatible_contract_version`; malformed manifests yield
`invalid_capability_manifest`. Errors contain fixed codes only, not server bodies.

All32 flag combinations and refusal cases are tested in both implementations.
`internal/sdkcontract/capabilities.go` is unexported; the JavaScript expression is
testdata and not packaged in the public SDK. No HTTP request, retry, identity
selection, output release, script permission or runtime configuration is performed.
An all-true fixture merely tests decoding claims: it does **not** enable execution.
Real negotiation must use the immutable SDK binding, ordinary authenticated
transport and the allocated route, and every actual operation still requires
current server-side authorization. No capability result may substitute for that.
