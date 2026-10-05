# Script persistence and trusted-single composition: allocation request

For task `vggevclc` / plan `qfcda7ct`, SDK-TENANCY-20261005.
**PROPOSAL ONLY. No profile number, migration, capability or route is allocated.**
Independent OpenAPI/SDK development does not depend on accepting this proposal.

## Exact owner handoffs

- Schema coordinator: session `ses_ef3f81f1effe5QIXiGEt2qIGkD`, report
  `zsief5fj`; credential owner `yp7llda1`.
- Mutation/output fences: S09 `i8aurh42`, P5.5 `krmkfe26`, P5.9 `5tmpau6l`;
  independently durable security-journal lifecycle remains P9-owned.
- Hosted operation adapter: S10 `ap90gj4e`, coordinated with P8/SDK.
- Resource ACL: S15 `rihm769f` (sole writer
  `ses_ef3f6d064ffeQzLEQZQM16mLIT`), S16 `wuge6wiv`, S17 `86ij8ynq`,
  S18 `cfo4q3z1`.
- Effects: P8 `8gxc3qi1`, notices `4v1bv798`, parent `kimljzt0`.

Accepted credential foundation is `6077f9e7`. `c751d0e6` is a private
credential-policy/storage candidate; `l3c70bk9` aggregate acceptance is pending.
`3de1edc3` combines P5 `421719ef` and P6 `6077f9e7`, not `c751d0e6`.
Coordinator-reported dormant P5 M1 `97e62eb4` is not complete lifecycle/fences.
Re-resolve accepted immutable inputs before implementation.

## Proposed persistence delta

Request a coordinator-named, exact successor for opt-in single scripts, and a
separately allocated future tenant composition. Numeric versions and final profile
names are **unassigned**. Never amend historical 30/31/32/33 manifests. A successor
must admit only its reviewed exact predecessor; a published target reopens
validate-only. No ad hoc optional tables or independent audit database.

Proposed tables, all in the existing shared SQLite owner:

1. `script_executions`: `(tenant_id, execution_id)` primary key; `principal_id`,
   credential/auth-generation reference from the accepted server binding;
   `endpoint`, nullable `idempotency_key`, `fingerprint` (32-byte SHA-256),
   script digest, contract/policy version, dry-run, canonical requested/effective
   limits, source transport, admitted/started/finished timestamps, terminal status,
   bounded error code and operation count, expiry/purge timestamp. Unique
   `(tenant_id, principal_id, endpoint, idempotency_key)` for non-null keys.
   No raw source, token, arguments, logs or result body.
2. `script_operations`: `(tenant_id, execution_id, operation_index)` primary key;
   same-tenant execution FK, stable operation ID, request digest, outcome
   (`planned`, `committed`, `failed`, `outcome_unknown`), optional atomic receipt
   reference, timestamps and bounded non-content error code. Composite FK never
   accepts an execution from another tenant. No normalized content arguments.
3. `script_execution_sources`: `(tenant_id, execution_id, resource_kind,
   resource_id)` primary key; composite execution FK, source revision and
   authorization-generation references. This stores provenance identifiers, not
   content. Resource IDs cannot be accepted as authority or expose hidden existence.
4. Replay content, if enabled later: a separately reviewed bounded protected
   payload relation keyed by the same composite execution identity, with encoded
   byte count, digest, explicit retention and source-provenance requirement.
   **Default off**; initial implementation can retain only minimal receipts and
   return a stable `result_not_retained` on an otherwise-authorized replay. This
   exception to the plan's retained-result acceptance needs explicit disposition;
   absent approval, implement the protected relation before claiming replay done.

Execution admission/idempotency claims and quota reservations commit in a short
writer transaction. Never hold the writer while JavaScript runs. Terminalizing
an interrupted execution does not rerun it. A lost journal/finalization after a
domain commit is `outcome_unknown` unless an atomic domain receipt proves outcome.

Quota rows/reservations should reuse the coordinator-approved authoritative
quota subsystem. If unavailable, request a bounded `script_reservations` relation
with tenant/principal/execution identity, lease generation and deadline, unique
execution reservation, and same-writer aggregate admission; no process-local
counter is a multi-server quota proof. Expiry alone must not imply a worker is
gone or permit duplicated uncertain work.

## Exact requested storage ownership

New proposed files (implementation only after allocation):
`internal/storage/schema_script.go`, `schema_script_validation.go`,
`schema_script_migration.go`, `script_executions.go`, `script_operations.go`,
`script_sources.go`, `script_retention.go`, with adjacent tests.
Existing `schema.go` only for reviewed private-artifact refusal/admission;
`tenant_surface_test.go`, `tenant_coverage_manifest_test.go`, schema provenance
and catalog testdata only for individually justified new surfaces. Do not grow
raw DB/control/package-function allowances or blanket-regenerate goldens.

Proposed scoped receiver operations: `ClaimScriptExecution`,
`GetScriptExecution`, `ListScriptExecutions` (bounded principal-scoped cursor),
`AppendScriptOperation`, `RecordScriptSources`, `FinishScriptExecution`,
`PurgeScriptExecutionPayloads`. Each takes context plus the accepted sealed
operation binding through the existing authorized adapter. DTOs are descriptive;
no public caller can construct authority. Exact signatures await fence allocation;
no DB/Tx callback or general SQL accessor is requested.

Migration acceptance: reject unknown/hybrid/lowered-stamp/TEMP artifacts before
mutation; preserve permanent install claims, roots, all foreign rows/FTS/CAS,
sync epochs/receipts/tombstones/high-water marks; atomic crash/reopen; failed COMMIT
cleanup and next transaction success. Use colliding A/B execution and resource
IDs plus principal in both orgs. Restore never restores access or reruns receipts.

## Trusted-single `script:execute` mapping request

Request a separate explicit opt-in submission permission at the existing verified
single-mode composition. `admin:*` or OAuth `mcp` wildcard alone must not grant it.
An auth-disabled single server may use only an explicitly configured trusted-local
mapping, composed after actual mode/local policy checks, never a client selector,
loopback address inference, forged name or nil auth shortcut.

The credential owner must choose the stable server-verified binding and live
revocation/generation source for current runtime credentials. Do not invent a
second identity registry, treat display names as immutable human principals, or
derive a hosted identity from loopback headers. Submitted workers receive no
credential. Request exact ownership of the new script authorization helper and
minimal single composition sites before changing `internal/api/middleware.go`,
`router.go`, `internal/apiserver/server.go` or MCP composition. Keep historical
credential33's eleven-capability vocabulary untouched.

## Broker requirements for the fence/ACL owners

Every operation needs current admission, then service preflight; mutations need
the same-writer current grant/credential/revision validation and domain commit.
Protected output/replay needs a release decision ordered with revocation, including
the entire conservative source-resource set. Require fixtures pausing between
admission/operation/commit/output and revoking credential, org membership, team,
project or entry grant. Dependency outages deny, not use cached authority.

Publication must prove destination audience is no broader than every source or
have a separately authorized declassification decision. Until that decision exists,
the broker can reject such writes but cannot claim arbitrary cross-resource
publication support. Generic content methods creating runnable work must request
work/automation capability and the P8 effect/delegation reservation path.

Dry-run must enumerate only execution audit/idempotency/quota/security accounting
and bounded scratch exceptions. Validators never invoke a real writer and roll
back. Provider-shaped reads are denied unless an explicitly side-effect-free path
exists. Snapshot domain SQL/Markdown/CAS/derived/events/queues and provider spies;
assert exact control deltas rather than claiming byte-identical whole-host state.

## Retention disposition request

Reuse D11 minimal security audit retention (90 days) only if the lifecycle owner
classifies these minimal records accordingly. Content-bearing replay is not minimal
audit and must follow content deletion, source revocation and approved shorter
retention; no source/result retention is inferred from the audit period. Payload
purge must leave non-content idempotency tombstones sufficient to prevent rerun.
Specify legal purge/restore ordering with the existing owner rather than launching
an unowned sweeper from graph construction.
