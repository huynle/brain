# Multi-tenant security contracts — phase 1 design

Status: **APPROVED FOR IMPLEMENTATION — not implemented or runtime-verified by P0.**
Assignment: P0 `7iyskorq`, Route C phase 1 contracts; documentation-only Route S.
Scope: approved security boundaries for Brain API, PWA, CLI/MCP, storage,
background work, and runners. This document does not enable signup or execution.

Related: [Multi-tenant ownership inventory](multi-tenant-ownership-inventory.md)
records the source baseline and approved ownership contract. Its runtime verification and
refresh after integration are release prerequisites.
See also [Brain Architecture](architecture.md) for the existing separation of
embedding and attachment extraction, and `../AGENTS.md` / `../CLAUDE.md` for
current service, runner, indexing, and transport conventions.

## 1. Authority and settled constraints

The following constraints were **SETTLED by the operator's assignment**:

- One shared SQLite database with `tenant_id` ownership, targeting hundreds of tenants.
- Untrusted signup is the threat model; signup is **not enabled**.
- One runner per tenant.
- Default single mode remains provisioning-free: explicit internal `local` scope,
  existing authentication behavior and unchanged data/database/blob paths.

D01-D13 below, including roles, time limits, budgets, storage mechanics and
release criteria, are **approved for implementation** by the revision-bound
record in section 2. “Must”, “forbidden”, and “reject” describe the approved
future contract, not existing enforcement. The register retains the approved
choices and rejected alternatives without policy amendments.
Design approval is not production deployment, single-mode query-credential
cutover, credential rotation, or public activation approval.

Existing project-global IDs, shared runner pools, local path conveniences, or
current pause/force semantics are not evidence of a tenant authorization boundary.
Security suspension must override manual run, force, resume, and automation paths.

## 2. Approved decision register

Every row is **APPROVED FOR IMPLEMENTATION**. Verification IDs
refer to the concrete, not-yet-executed matrix in section 10.

| ID | Approved decision | Rejected alternatives and reason | Required verification |
|---|---|---|---|
| D01 | Stable authenticated principal with multiple independently authorized tenant memberships; explicit selected tenant on every tenant operation | Email/domain/project name as identity; first/last/default membership fallback: ambiguous or forgeable authority | V01–V03 |
| D02 | Owner/admin/member roles; separate audited operator break-glass capabilities | Tenant admin as platform admin; standing operator data token; automatic support access: excessive authority | V04 |
| D03 | Atomic, accepted owner transfer and controlled recovery | Last-owner removal; email-only recovery; partial transfer: takeover or orphaned tenant | V05 |
| D04 | Suspension and deletion revoke authority; finite retention and tombstones | Pause-only suspension; indefinite soft-delete; restore-based reactivation: stale authority survives | V06, V15 |
| D05 | Opaque online-checked access tokens preferred; online epoch-checked signed tokens acceptable; rotating refresh families and bounded execution leases | Offline JWT expiry as revocation; unbounded caches/streams; partition grace: no bounded fail-close | V06–V08 |
| D06 | Initial release uses tenant-provided runners ONLY; platform-hosted execution blocked pending reviewed VM isolation | Shared OS user/home, containers alone, or “tenant-owned means trusted”: arbitrary code can cross trust boundaries | V08–V09 |
| D07 | Tenant-qualified storage and tenant-specific FTS5 virtual tables in the SAME shared SQLite | Separate database per tenant conflicts with settled topology; global FTS with row filtering leaks BM25 corpus statistics | V10–V11 |
| D08 | Tenant-qualified CAS and `UNIQUE(tenant_id,digest)`; tenant-owned derivations and extraction caches | Global physical dedup/cache or digest-as-authorization: existence leaks and cross-tenant lifecycle coupling | V12 |
| D09 | P2 forbids standing admin token passthrough; narrow credential/proxy or block; P7 removes legacy mechanisms | Temporary broad passthrough, query credentials, compatibility bypass: leaves escalation path active | V07, V13 |
| D10 | Explicit atomic quotas and tenant-attributed paid-provider reservations/settlement | Advisory limits, postpaid-only checks, unknown-cost dispatch: concurrent overspend and noisy neighbors | V14 |
| D11 | Finite backup retention and approved RPO/RTO targets; restore excludes revoked identities and deleted data | Snapshot auth resurrection, indefinite backup exemptions, unfiltered shared restore: access/data resurrection | V15 |
| D12 | Tenant-global entries only; no cross-tenant references; observations never confer ownership; tenant-owned runner placement | Platform-global content namespace, trust client paths/labels, cross-tenant scheduling fallback: confused deputy | V02–V03, V09–V10 |
| D13 | Public release requires implementation evidence AND separate explicit operator activation | Passing tests or merging docs automatically enables signup: evidence is not authorization | V16 |

Approval record for **D01-D13, no amendments**:

- Actor: user in the existing supervising Codex task.
- Timestamp: `2026-09-06T21:02:01.445953+00:00`.
- Reviewed revision: `4d10904afeedfb8791a600f5e925bc82b64bf97b`.
- Reviewed artifact: `docs/multi-tenant-security-contracts.md`.
- Exact response: "Approve the proposed contracts for implementation".
- Provenance: approval recorded in Brain task `7iyskorq` and plan `9fguh2pr`.
- Scope: implementation design only; no production deployment, single-mode
  query-credential cutover, credential rotation or public activation authorized.

This supersedes earlier approval-pending notes, not implementation evidence.
**P0 completion gate:** commit this approval record and ownership handoff,
reconcile plan `9fguh2pr` and affected tasks, and verify consistency/dependencies
before completing `7iyskorq`. Independent P1/P2/P3 remain authorized. P4 requires
both finalized P0 and complete P3; design approval alone does not waive either.

## 3. Principal, tenant selection, and lifecycle

### 3.1 Authentication is not tenant authorization (D01, D12)

- A human principal has an immutable server-issued ID. Federated identity is
  keyed by verified issuer plus subject, never mutable email alone. Linked
  identities require reauthentication of both identities; matching email does
  not automatically merge accounts. Service/runner principals are distinct
  kinds with explicit scopes, not human impersonations.
- Membership is an explicit `(tenant_id, principal_id, role, status, epoch)`
  relation. A human can belong to multiple tenants; removal from A does not
  grant or remove B membership. A runner/service credential binds one tenant.
- Require an `X-Brain-Tenant` selector on tenant HTTP operations,
  including bearer-authenticated SSE and HTTP MCP. Tokens are tenant-bound
  after selection. Session issuance validates the requested membership; an
  authenticated tenant-picker can list only that principal's memberships.
  Any tenant IDs in routes, payloads, tokens, and MCP session bindings must
  agree. Missing selector is `400`, invalid/revoked authentication is `401`,
  and nonmembership or suspension is `403` without foreign tenant metadata.
- No implicit ambiguous fallback, including “first membership”, last used
  tenant, project inference, or server launch directory. Even a single
  membership is not an omitted selector. Clients may persist an explicit user
  choice, send it each time, and validate it anew at the server.
- The selector requirement above applies to multi mode. Explicitly configured
  single mode injects the internal `local` scope without requiring new headers,
  login or provisioning. This is a mode-specific trusted composition seam, never
  a missing-identity fallback in multi mode. Existing authenticated single-mode
  deployments continue to validate their configured credentials.
- Construct a request-scoped principal and tenant authorization context before
  service calls. Repository APIs, transactions, events, caches, audit records,
  pagination cursors, idempotency keys, and background envelopes preserve that
  context. Missing context is an error, never a wildcard or operator context.
  Object lookup happens within the selected tenant; foreign/not-found object
  requests have the same `404` shape without titles, counts, or foreign IDs.
- All references (dependencies, links, attachments, goals, features, automations,
  runner sessions, webhook deliveries) must resolve within the same tenant.
  Composite foreign keys or transactional equivalent checks prevent accidental
  cross-tenant joins. Reject a mixed-tenant bulk operation before any mutation.
  Moves between projects are same-tenant only; cross-tenant transfer is not an
  implicit copy/move feature. Tenant-global entries omit project scope, not
  tenant scope; there is no shared public/global entry namespace at launch.
- Client/workspace observations, hostname, git remote, local path, labels,
  machine IDs, and claimed origin are hints, not authority. Registration binds
  ownership using an authorized enrollment, and resolution only searches the
  selected tenant. A path match or shared repository never grants membership.

### 3.2 Roles, operator access, owner transfer, recovery (D02, D03)

| Capability | Member | Admin | Owner | Operator |
|---|---|---|---|---|
| Read/write tenant content; submit work to an authorized runner | Yes, within credential scopes | Yes | Yes | No implicit access |
| Invite/remove members; configure tenant automation and runner enrollment | No | Yes, members only | Yes | No implicit access |
| Grant/revoke admin; approve provider billing settings | No | No | Yes | No implicit tenant role |
| Transfer ownership; request tenant deletion/export of whole tenant | No | No | Yes, reauthenticate | Only specifically granted recovery/break-glass |
| Suspend for abuse; inspect platform health | No | No | No platform capability | Separate platform capability |

Require exactly one active owner. Admins cannot promote themselves, remove
the owner, change owner recovery factors, or gain operator scope. Member task
execution is arbitrary code on the tenant runner, so membership implies that
tenant-local execution trust; it is not a sandbox between members. Secrets are
write-only configuration where possible, never returned by general content APIs.
Scopes can narrow a role but cannot broaden it; role changes advance epochs.

Transfer contract: current owner reauthenticates with MFA within 5 minutes,
nominates an active same-tenant member, and receives an offer expiring in 24 hours.
The recipient reauthenticates and explicitly accepts. A single transaction
compares the expected owner/version, promotes the recipient, demotes the old
owner to admin, increments affected membership epochs, invalidates outstanding
offers, and records an audit/outbox event. No intermediate zero/two-owner state;
concurrent acceptance has one winner (`409` for stale version). Refuse last-owner
removal and deletion of an owning principal until transfer or tenant deletion.

Recovery contract: hashed one-use recovery codes plus verified second factor;
successful recovery revokes all old login/refresh sessions before issuing a new
one. Lost-all-factors recovery is not an email-only shortcut: two distinct
authorized operators review identity evidence, notify existing channels, and
wait 48 hours before an audited single-use recovery grant. If this staffing or
evidence is unavailable, block assisted recovery rather than weaken it. Local
offline recovery uses an explicit local-console procedure with a backup and
audit trail; it is not a remotely callable unauthenticated reset.

Operator break-glass is separate: MFA reauthentication, two-person approval,
incident reason, named tenant, named capabilities, and a maximum 15-minute
grant. No standing content-read/write role. Suspension capability need not
include content access. Recovery, export, and restore each require explicit
capabilities; grants do not silently bypass deletion or allow cross-tenant joins.
Record issuer, approver, actor, tenant, capability, result, and expiry in an
append-only audit sink; redact content and all credential material.

### 3.3 Suspension, deletion, and retention (D04)

Use `active → suspended → active` and
`active|suspended → deleting → deleted`. Suspension records a reason and bumps
the tenant epoch atomically. Reject reads, writes, paid calls, dispatch, claims,
refresh, stream events, and control actions using the bounds in section 4.
Allow only separately authorized appeal/recovery/erasure operations. Unsuspension
requires an authorized operator (for abuse suspension), fresh credentials, and
explicit reauthorization of held work; it does not replay old dispatches.

Deletion requires owner reauthentication and explicit tenant-name confirmation.
At acknowledgement, deny access and create a durable deletion tombstone before
asynchronous erasure. There is no login-enabled grace period or delete-undo at
launch. Cancel queued work, revoke sessions and runner enrollment, and suppress
late callbacks; destruction is idempotent, retryable, and audited. Tenant IDs
are never reused. Pending paid-call reconciliation uses minimal ledger data,
not restored tenant content. Finite erasure and backup deadlines are in section 9.

## 4. Approved revocation and partition contract (D05)

All numeric limits here are **approved implementation requirements**. Let `t0` be the durable commit
of logout/revocation, membership removal, role reduction, principal disable,
runner revocation, tenant suspension, or deletion, as applicable. API success
must not precede that commit. Logout revokes a session family; account recovery
revokes all principal sessions; membership and tenant epoch changes have their
respective scopes. Check all relevant epochs, including session and runner state.

| Surface | Approved bound and enforcement |
|---|---|
| New HTTP/MCP requests | Zero stale-authorization cache allowance: authoritative online check at admission. Requests whose check starts after `t0` are denied. Opaque token lookup is preferred; signed access tokens require the same online session/principal/membership/tenant epoch checks. A 5-minute access-token expiry is defense in depth, NOT the revocation bound. |
| In-flight reads/writes | Revalidate before emitting protected output and in the write transaction. Serialize the final check with the mutation/revocation commit. Output already handed to the socket before `t0` cannot be recalled; abort remaining buffered work. Never claim retroactive confidentiality. |
| Refresh sessions | One-use rotating refresh token, hashed at rest, 7-day idle and 30-day absolute expiry. Rotation is atomic; replay revokes the whole family, with no reuse grace. Concurrent refresh has one winner; clients serialize refresh. Every refresh checks current authority; replayed/lost tokens require login. |
| SSE and HTTP MCP | Bearer authentication plus explicit tenant at connect/reconnect and every MCP call. Authorize before each protected event/frame; purge buffered events on invalidation. Idle connections recheck at most every 5 seconds and close by `t0 + 5 seconds`. Subscription IDs and resume cursors are tenant-bound; no replay across sessions/tenants. |
| Queues, automations, goals, extraction, embeddings, webhook jobs | Persist tenant, initiating/service principal, epoch, resource, and idempotency key; recheck at enqueue, claim, execution, paid-call authorization, and result commit. No new authorized claim or side effect after `t0`; inert queue records may take up to 5 seconds to mark cancelled. Automation uses an explicit tenant service grant, not ambient operator authority; removing its grant cancels future work. |
| Running cooperative agents | Tenant/task-scoped execution authorization lease valid for 30 seconds, renewed every 10 seconds. On revocation notification or lease expiry stop new tool calls, terminate the process tree, then hard-kill within 5 seconds. Worst-case cooperative local stop: `t0 + 35 seconds`. API and paid proxy permissions are revoked independently at `t0`; a cached runner lease does not authorize an API request. |
| Network partition | No online authority means no new admission, claim, renewal, protected stream event, provider reservation, or result write. Fail immediately on dependency failure, not cached success. Runner watchdog uses monotonic lease deadlines and stops within 35 seconds of last successful renewal; a restart requires online authorization. There is no offline/force override for a shared-service runner. |

Check-and-authorize at external-call dispatch defines its linearization point.
A provider request authorized before `t0` can complete afterward. Cancellation
is best effort; payment, a sent message, git push, deployment, downloaded secret,
or other irreversible effect cannot be undone by token revocation. Fence result
commits and prevent subsequent calls; retain an auditable “outcome unknown” state
and reconcile charges rather than pretending rollback occurred. Do not retry an
uncertain external effect unless the provider offers a usable idempotency key.

The 35-second stop guarantee applies only to a verified cooperative supervisor,
not a malicious tenant host. A tenant can patch its runner or keep copied code,
data, and its own credentials running indefinitely. The service can bound access
to its API/proxies, not reclaim disclosed data or revoke tenant-owned third-party
credentials. This limitation must be disclosed and tested separately from server
fail-close. If bounded external effects are required, use a fenced online proxy
with no direct egress/credentials or block that workflow.

## 5. Execution trust and local operation (D06, D12)

**Require tenant-provided runners ONLY at initial release.** One enrolled
runner belongs immutably to one tenant; replacement revokes/fences the old
enrollment before the new one becomes active. No platform-hosted arbitrary-code
execution until a separate VM-isolation design, threat review, and escape tests
are approved. Containers or worktrees alone are not acceptable substitutes.

Tenant-owned does not prove that a host is trusted. Never place distinct tenants
in a shared OS user, home directory, credential store, agent session database,
git credential helper, provider account access, or unrestricted shared network.
Require separate dedicated hosts or independently reviewed VMs with isolated
users/homes, volumes, credentials, network policy, and metadata-service denial.
The API host is not a runner. Do not mount its data directory, socket, service
credentials, or operator home into an executor. Enrollment records ownership,
not an attestation that a tenant machine implements isolation.

Validate tenant ownership on push, pull, direct claim, acknowledgment, renewal,
placement/assignment, manual run, resume/force, instance listing, prompt delivery,
permissions, abort, and kill. A matching machine ID/capability cannot override
tenant mismatch. No fallback to another tenant's idle runner. Scheduling keys,
claim leases, topics, sessions, logs, and placement configuration include tenant
ownership. Commands carry a fencing generation to reject replaced runners and
stale task attempts; agent output and callbacks are untrusted inputs.

Local first-class settled contract: default single mode injects the explicit
internal `local` tenant without new login, provisioning, config or file movement.
Retain existing configured authentication and legacy BrainDir, shared database
path and independently configured blob root through migration and every restart.
If legacy local owns the base directory, exclude foreign `tenants/` subtrees and
symlink aliases from local scanning, resolution, export, restore and purge.
Localhost, cwd, or an absent token never enables single-mode behavior on a
multi-mode server. An offline client of a shared service cannot substitute local
state for shared-service authorization. Keep stdio-only file
access explicitly gated; HTTP MCP accepts wire bytes, never arbitrary API-host
`file_path`/`output_path`. Runner workdirs are separately tenant-authorized paths.
Do not use launch-directory context or observation resolution to select an
unrequested tenant. Positive local fixtures are mandatory, not just remote denial.

## 6. Storage, ranking, and blobs (D07, D08)

### 6.1 Shared SQLite, tenant-local ranking

Keep the settled **same shared SQLite** topology. Every owned row has a non-null
`tenant_id`; uniqueness, joins, foreign keys, indexes, leases, and lookup keys
include it wherever IDs are tenant-local. Filesystem markdown paths are also
logically tenant-qualified; legacy local physical paths remain unchanged under
the durable root mapping in section 5. SQLite remains a derived content index, but authorization,
revocation, quota ledgers, and deletion state must not be reconstructed from
caller-editable frontmatter or lost during content reindexing.

Use **one tenant-specific FTS5 virtual table per tenant in this same
database**. Its content and derived-text rows belong exclusively to that tenant.
Filtering results of a global FTS table by `tenant_id` is insufficient: BM25
document count, average length, and term frequencies would still depend on
foreign documents. Use only the selected tenant table for BM25, snippets,
suggestions, counts, and hybrid lexical scoring. Semantic candidates and hybrid
normalization are tenant-scoped too. **No global BM25 fallback**, even during
migration, error recovery, backfill, or local-mode operation. If a table is
unavailable, return an explicit search-unavailable error rather than widen scope.

Table identifiers come solely from server-generated internal IDs: use
32 lowercase hexadecimal characters validated against `^[0-9a-f]{32}$`, with
a fixed `fts_t_` prefix and a server-owned mapping. Human names, slugs, headers,
and frontmatter must never be interpolated as SQL identifiers. Reject invalid
internal IDs rather than lossy sanitization; quote validated identifiers and
bind all values. Provision/drop only via controlled tenant lifecycle operations.

Atomic migration contract: with public exposure disabled, quiesce writers and
background indexers, snapshot, explicitly map legacy data to its authorized
tenant, and reject unmapped/ambiguous ownership. Build/validate shadow FTS tables
and counts; atomically commit schema version plus routing switch in one SQLite
transaction. On crash, readers see either the old private deployment or the
complete tenant-isolated mapping, never mixed routing. Old global indexes are
not accessible to tenant requests. Remove them after successful cutover; rollback
keeps public access disabled, never reinstates global ranking for public traffic.

Hundreds of virtual tables also create shadow tables, catalog entries, statement
cache churn, WAL growth, and shared writer-lock contention. Budget index bytes
and rebuild work per tenant, serialize expensive maintenance (one FTS maintenance
job globally), and prioritize request work. Tenant optimize/rebuild/drop must
target that tenant only; shared database vacuum/backup requires a separately
scheduled operator maintenance window. Benchmark 100 and 500 tenants, one hot
tenant, concurrent search, and deletes/rebuilds. Approved gate: p95 search below
500 ms at 100 aggregate requests/second on documented hardware and fixture size;
report writer waits, disk/WAL peaks, and per-tenant index costs. If this fails,
revisit limits/design with the operator, not global ranking fallback.

### 6.2 Tenant-qualified CAS and derived ownership

Use `blobs/<internal-tenant-id>/sha256/<digest-prefix>/<digest>` with
`UNIQUE(tenant_id,digest)` metadata. Digest is a lowercase validated SHA-256 of
server-verified bytes, not a path or authorization credential. Attachment IDs
and references are also tenant-qualified. No cross-tenant **physical** dedup,
reference counts, extraction cache, embedding cache, or “already exists” lookup.
Identical bytes in A and B occupy independent physical objects and quota.

Legacy `local` is an explicit physical-layout exception, not an ownership
exception: persist its configured CAS root and existing
`<root>/<digest[0:2]>/<digest[2:4]>/<digest>` layout without moving bytes. Stamp
existing metadata local. New tenants use exclusive registered roots/layouts;
foreign tenants never reuse local objects. Resolve the layout from the durable
tenant mapping on every restart; GC, export and erasure use that same mapping
and reject overlapping roots/symlink aliases. A root string alone is insufficient.

Stage uploads in a tenant-private temporary path, stream-check length/hash,
reserve quota, then atomically publish metadata and a durable blob intent.
Because SQLite and filesystem rename are not one transaction, recovery must
reconcile that intent before serving it. Idempotent retry is tenant-bound;
same digest with mismatching bytes is corruption/collision and must be rejected
and quarantined, never overwrite an existing object. Defend against traversal,
symlinks, and caller-supplied digest/path substitutions.

Raw downloads, ranges, extracted text, previews, OCR, embeddings, and source URLs
all require the same ownership check. Derived records inherit the source tenant,
and cache keys include tenant, digest, provider/model/version, and extraction
configuration. Attaching a foreign blob is forbidden. Within a tenant, only
unreferenced blobs may be deleted; unlink and reference checks are serialized
against concurrent attach/delete. Deleting A never removes or invalidates B's
equal-digest blob. Extraction rechecks authority and source generation before
commit; deletion/suspension while extraction runs suppresses late resurrection.
Garbage collection and temporary-upload cleanup use tenant-qualified manifests,
not a global digest scan that can delete another tenant's object.

## 7. Credential migration and transport (D09)

| Phase | Approved contract |
|---|---|
| P2 interim | **No standing admin token passthrough** to agents, child environments, task prompts, MCP configuration, scripts, or runner control. Supply a restricted tenant/task-scoped credential with only necessary actions, or an authenticated online proxy that independently checks task/tenant authority. If neither exists, block the workflow with an actionable error. “Temporary” does not permit broad authority. |
| P7 removal | Hard-remove legacy standing-token inheritance, fallback configuration, compatibility handlers, query-token parsing, and broad bypasses; rotate/revoke previously exposed credentials and inspect deployment configs/logs. Removing documentation alone is not removal evidence. |

Credentials are forbidden in URL query strings, paths, fragments, redirects,
logs, traces, analytics, error messages, and SSE resume IDs. Reject query-token
authentication even when a valid header is also present; redact sensitive query
keys at the earliest ingress/proxy logging boundary so rejected URLs do not
leak tokens. Use `Authorization: Bearer` over TLS for remote access.
Browser SSE uses bearer-authenticated streaming `fetch`, not an `EventSource`
URL token workaround. HTTP MCP uses bearer headers on every transport request;
stdio MCP uses protected local credential configuration, not URL credentials.
MCP session IDs are not credentials. Do not forward Authorization on redirects
to a different origin. Service workers, browser caches, diagnostics, and reverse
proxies must not persist protected responses or raw credential headers.

The D09 transport design is approved; deployment/cutover still needs a separate
explicit operational approval:
multi mode refuses query credentials from its first release. For existing single
mode, removal is a deliberate security compatibility break, not an assertion of
unchanged authentication. First ship header-capable SSE/MCP/CLI clients, inventory
URL-token callers without recording token values, then obtain a maintenance-window
approval to reject query auth and rotate exposed tokens. Do not deploy that break
silently or preserve it through a multi-mode compatibility flag. V01 preserves
configured header authentication and provisioning-free operation; V07/V13 query
denials apply to multi mode and explicitly approved single-mode cutovers. Until
that cutover exists, do not claim the query-credential removal milestone passed.

No broad “trusted client”, localhost-on-shared-service, old-version, or migration
compatibility bypass. The proxy is not a general network tunnel: allowlist
operations/resources, bind tenant/task/attempt, enforce revocation and budget,
and withhold operator/provider credentials from the agent. Tenant-supplied
provider credentials never imply permission to access a platform account.

## 8. Approved quotas and paid-provider accounting (D10)

All values are **approved implementation limits**, not product tiers
or current configuration. Units: MiB = 2^20 bytes; GiB = 2^30 bytes; time windows
are rolling unless a UTC calendar period is specified. Limits apply across all
of a tenant's users, transports, background jobs, and retries.

| Resource | Approved initial hard limit |
|---|---|
| API requests | 60 requests/minute/principal and 600 requests/minute/tenant; burst 20 and 100 respectively |
| Projects / entries | 100 projects and 100,000 entries/tenant, including tenant-global entries |
| Entry / request body | 1 MiB UTF-8 entry body; 2 MiB JSON request (attachment byte transport separately capped) |
| Blob upload / stored bytes | 25 MiB/upload; 5 GiB raw blobs/tenant including reserved in-flight uploads |
| Derived text + search indexes | 2 GiB/tenant combined, including temporary rebuild reservation |
| Streams / MCP sessions | 10 open SSE streams and 10 HTTP MCP sessions/tenant |
| Runner / concurrent execution | 1 active runner and 2 running tasks/tenant |
| Queue / extraction | 1,000 queued jobs/tenant; 2 active extraction/embedding calls/tenant |
| Extraction input | 100 PDF pages or 10 minutes audio per job; reject oversized work before paid dispatch |
| Paid services | USD 1.00/job, USD 5.00/UTC day, USD 50.00/UTC month per tenant; effective budget zero until explicitly configured |
| Signup abuse admission | Signup remains off; if separately activated, 5 attempts/IP/hour, 1 new tenant/verified principal/day, and 500 active tenants/deployment |

Enforce byte/count limits before expensive parsing; reserve unknown-length uploads
to their full cap, or reject. Use atomic shared-store reservations, not per-worker
counters. Release on known failure; crash recovery must not allow double spend.
Exhaustion returns `429` with a bounded retry hint for rate limits, `413` for body
size, or a stable quota/budget-exhausted error; retries do not bypass accounting.
Fair scheduling and query deadlines limit noisy neighbors; quotas do not promise
complete timing isolation in shared SQLite. Capacity gates must include adversarial
costs from signup, FTS table creation, and background work, not only API traffic.

Every paid extraction, embedding, agent/proxy provider call records tenant,
principal/service grant, job and attempt, provider account, model, price version,
currency, and idempotency key. A tenant-owned account is still attributed; raw
direct tenant-provider calls outside the proxy are explicitly outside the
platform's enforceable budget and cannot use platform credentials.

Before dispatch, atomically reserve the conservative maximum charge across
job/day/month budgets, including input/output token caps or media units, fees,
and retries. Available = limit − settled − outstanding reservations. If a
finite maximum cannot be enforced at the provider/proxy, **do not dispatch**.
Bind reservations to the UTC buckets at admission; carry unsettled reservations
across rollover instead of resetting them. Provider usage settles idempotently
exactly once against a tenant-qualified operation key, returning only unused
reservation. Duplicate callbacks cannot double-charge or double-release. Timeout
with unknown outcome retains the reservation and requires reconciliation; never
blindly expire it into free credit. Provider idempotency and fenced dispatch
prevent duplicate execution where supported; otherwise an uncertain retry is
blocked. Operator adjustments are separate audited ledger entries, not edits
to history. Provider-price changes require updated conservative limits before
new work is admitted. Extraction remains off by default as in architecture.md.

## 9. Approved durability, erasure, and restore (D04, D11)

These finite numbers are **approved implementation targets**, not an SLA or a claim that backup
jobs exist. “Deletion” distinguishes immediate denial from physical erasure.

| Item | Approved target / retention |
|---|---|
| Content RPO | At most 15 minutes of acknowledged content changes lost |
| Shared service RTO | At most 4 hours from declared outage to validated service restoration |
| Scoped tenant restore RTO | At most 8 hours from approved restore request, within tested capacity |
| Backups | Consistent encrypted snapshot/WAL + markdown/blob manifest every 15 minutes; retain 30 days |
| Deleted live content | Purge markdown, blobs, indexes, derived caches and temporary copies within 24 hours of deletion acknowledgement |
| Deleted backup content | Age out all backup copies within 30 days of deletion acknowledgement; no indefinite offline copy exemption |
| Unfinished upload staging | Purge within 24 hours, including crash leftovers |
| Operational logs / agent transcripts | Retain 7 days / 30 days respectively; tenant deletion still purges content within 24 hours |
| Minimal security/deletion audit and provider ledger | Retain 90 days, then purge; only pseudonymous IDs, timestamps, reasons, and financial units, not content or credentials |
| Revocation/deletion replay journal | Retain 90 days (longer than backup horizon); never reuse IDs |

Back up content, filesystem artifacts, manifests, and authoritative metadata at
a consistent boundary, with checksums and tested encryption-key recovery. A
shared SQLite snapshot can contain many tenants: only privileged restore tooling
may read it, never a tenant download endpoint. A scoped export is a newly built
tenant-only artifact, not a copy of the shared database/WAL.

The 15-minute content RPO **does not allow resurrection of revoked authority**.
Replicate an append-only revocation/deletion journal independently of rollbackable
content snapshots before acknowledging those security transitions (approved
security-state RPO: zero acknowledged transitions). If that acknowledgement
path is unavailable, fail closed. If the latest journal cannot be recovered or
its completeness verified, restored service stays sealed: invalidate all old
credentials and require controlled identity recovery; do not guess membership
or revive deleted tenants to meet RTO.

Restore in quarantine with outbound jobs, signup, runners, and streams disabled.
Replay the current revocation journal and deletion tombstones BEFORE serving
any restored bytes. Exclude deleted tenants, removed memberships, disabled
principals, refresh families, API tokens, runner credentials, pending dispatches,
and previously revoked grants. Backed-up credentials are never made live again;
fresh login/enrollment is required. Restore current authorization from verified
security state, not historical content/frontmatter. Reconcile paid reservations
against the current ledger before enabling any paid work.

Scoped restore stages only the authorized tenant's rows/files/derived objects,
validates all reference ownership, rebuilds only that tenant's FTS, and swaps
the tenant data under a maintenance fence. It must not rewind another tenant's
records, memberships, quotas, or jobs. Do not replay historical automations or
external effects. Explicitly inventory backup replicas and encryption-key copies;
per-tenant crypto-erasure may supplement, not replace, the 30-day copy expiry.
Any required legal retention beyond these figures needs a separately approved,
disclosed policy before release; this document invents no such exception.

## 10. Verification matrix — required evidence, not test results

**All rows are NOT RUN / evidence pending.** Future implementation tests should
use Go's standard testing framework, table-driven cases, and isolated temporary
SQLite/filesystem fixtures consistent with repository conventions. Never use
production tenant data or real paid calls. Use a deterministic clock and a fake
provider with recorded admissions, charge limits, and idempotency behavior.

Shared fixture: tenants A and B deliberately reuse project, entry, task, feature,
attachment, client, machine, session, and logical runner IDs wherever the schema
allows tenant-local IDs; both upload identical bytes. Include a principal in
both tenants, one in A only, separate owners/admins/members, revoked credentials,
and a local-only installation. Check response bodies AND storage changes,
files, logs, emitted events, outbound calls, and timing of authorization points.

| ID | Positive fixture | Negative / concurrency / failure assertion |
|---|---|---|
| V01 | Existing single-mode install upgrades/restarts without new login, provisioning, tenant headers or moved DB/files/blobs; CLI/PWA/stdio MCP and runner workflows pass normalized compatibility fixtures; query-auth cutover tested separately under D09 approval | Multi-mode missing identity/selector, expired configured credentials, nested foreign roots, remote filesystem arguments and shared-service offline fallback fail; HTTP MCP planning discovery with absolute/traversal additional_dirs cannot read API-host files |
| V02 | Multi-member principal selects A then B explicitly and gets only the selected tenant | A-only principal selects B; selector/token/payload mismatch; missing/ambiguous context; forged workspace/hostname/remote all fail without metadata leakage |
| V03 | Same-tenant global entries, links, moves, bulk operations, goal/feature dependencies and cursors work | Equal-ID B references, mixed bulk, foreign cursor, cross-tenant move/attach, cache reuse and guessed paths cause no reads/writes/events in B |
| V04 | Each role executes only its matrix capabilities; approved scoped break-glass works within expiry | Member/admin escalation, tenant admin platform action, unapproved operator content access, expired/reused grant and credential logging fail |
| V05 | MFA transfer acceptance commits one new owner; one-use recovery revokes old sessions | Concurrent transfers have one winner; crash injection at transaction boundaries never yields zero/two owners; last-owner removal, email takeover and stale recovery replay fail |
| V06 | Unsuspension requires fresh credentials and explicit work reauthorization | Revoke/suspend/delete during requests, streams, queued work and a network partition; assert no post-commit admissions/output authorization, idle streams close within 5 seconds, no force bypass |
| V07 | Opaque online lookup and signed-token online epochs meet identical bounds; refresh rotates; bearer fetch SSE/MCP reconnect succeed | Stale epoch, revoked membership, simultaneous refresh/replay, foreign stream replay, session-ID-only auth, query tokens and cross-origin header forwarding fail |
| V08 | Cooperative runner renews 30-second leases every 10 seconds; permitted effect settles once | Partition immediately after renewal; process tree stops within 35 seconds; fenced late result rejected. Fake pre-revocation provider call may finish but no new call is admitted. Malicious runner cannot access API/proxy despite not stopping locally |
| V09 | Tenant-provided runner enrolls and receives same-tenant push/pull/direct claims and control | Foreign assignment, matching machine labels, old replacement generation, cross-tenant session/permission/kill rejected; assert no shared home/credentials/network/provider access in supported deployment fixture; platform-hosted execution unavailable |
| V10 | Same-SQLite tenant ownership survives reindex and tenant FTS provisioning | Inject malformed/internal ID and SQL metacharacters; schema mapping rejects them. Crash each migration stage; no partial public routing, unowned rows, or global FTS fallback |
| V11 | Fixed A corpus/query yields a baseline ordered result, snippets and exact BM25 scores | Add/delete radically different B corpus (lengths and term frequencies); A scores/order/counts unchanged. Test lexical and hybrid paths; run 100/500-tenant cost benchmark including rebuild/lock/WAL measurements |
| V12 | A/B identical uploads create separate physical tenant paths and extraction cache entries; same-tenant retry is idempotent | Forced digest mismatch/collision, partial upload crash, quota failure, concurrent attach/delete, cross-tenant download/range/delete, extraction finishing after deletion: no overwrite, leak, undercount or resurrection; B remains intact |
| V13 | P2 restricted credential/proxy completes an allowed workflow; unsupported workflow is blocked | Capture child env/prompts/configs, redirects, ingress logs and traces: no standing admin/provider secrets or URL tokens. At P7, scan and exercise legacy branches/configs to prove hard removal, not just nonuse |
| V14 | Within-quota requests/uploads/jobs settle with tenant attribution | Race N workers for last byte/slot/cent in one shared store; exactly eligible reservations succeed. Duplicate callbacks, midnight rollover, crash/timeout, unknown provider maximum and cross-tenant idempotency keys cannot overspend or release credit twice |
| V15 | Restore A from an older shared backup into quarantine; check checksums and measured RPO/RTO, B unchanged | Revoke/delete after snapshot then restore: identities/content stay excluded. Lost journal keeps service sealed. Verify 24-hour live purge, 30-day backup expiry, 90-day journal expiry, replica/key handling, stale jobs and paid-ledger reconciliation |
| V16 | Release rehearsal collects revision-bound implementation/test evidence and a separate activation record | Passing matrix, merging docs, or approving design alone cannot enable signup; absent approval, failed isolation evidence, or unavailable stop/fail-close checks keep public gate closed |

## 11. Public release gate and handoff (D13)

Require two separate gates, neither satisfied by this design-only task:

1. **Evidence gate:** reconcile the ownership inventory with every persisted
   entity, handler, MCP tool, event, background job, filesystem writer, and runner
   control path. Implement approved decisions; attach fresh test commands,
   outputs, revision, fixtures, revocation timelines, load/cost measurements,
   credential-removal evidence, and restore/erasure drill reports for V01–V16.
   Record unresolved risks and independent security review. A missing row is
   missing evidence, not a waived requirement. Verify local positive paths as
   well as equal-ID cross-tenant denials. No platform-hosted execution at this gate.
2. **Operator activation gate:** after reviewing evidence, an operator explicitly
   approves the exact deployment/revision, signup exposure, quotas, retention,
   provider policy, and rollback/incident procedure. Record actor/time/scope.
   Only then may a separately controlled activation enable untrusted signup.
   Design approval and activation are different records. Failed gates keep
   signup off; rollback never turns off tenant checks to regain availability.

P0 handoff is this approved implementation contract plus the ownership inventory.
This document records revision-bound design approval, changes no runtime behavior,
and demonstrates no runtime test result. Implementation phases must supply the
required evidence; operational actions still require their separate approvals.
