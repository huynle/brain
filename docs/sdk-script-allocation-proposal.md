# Script C–F delta for the active DB.1 design

Disposition packet **SDK-DB1-CF-20261006**, SDK task `vggevclc`, plan `qfcda7ct`.
**PROPOSED ONLY. No DDL, profile number, capability grant or activation authority.**
**Exception — §5 SDK-U1–U3 are user-APPROVED product policy (SCRIPT-DECISIONS-20261006;
replay codes and deadline-anchored retention in SCRIPT-DECISIONS-20261006b).**
Their approval allocates no DB1 schema, capability, fence or activation; T1–T6 stay proposed.
This replaces the earlier filesystem-era/broad table request in this file.

## 1. Evidence, ownership and settled decisions

- DB.1 `hxcyvu0i` is **in_progress**, sole writer on
  `codex/mt-db-authoritative-storage`.
  - **Pin (refreshed 2026-10-07):** DB.1 design **rev 6** at
    **767dce41** (`docs/db-authoritative-storage-design.md`, 2,508 lines, read
    with `git show`; that worktree was not touched). Branch head at read time
    was `cd775325` (test-only after 767dce41).
  - **Status:** rev 6 is PROPOSED; its dormant Phase B slice is being
    implemented (e.g. `a3ddbb26`, `1a4f2cf3`) and its literal DDL (Appendix A)
    may be amended in place until DB.5 cutover (D31). It allocates no
    script-specific relation, namespace or ref kind.
  - **History:** this packet was first written against rev 1 `d365a4aa` and
    reviewed PASS there (`kkpe2zs9`). Section and method references below now
    cite rev 6.
- Read current Brain plans `9fguh2pr` and `qfcda7ct`, and owner records
  `hxcyvu0i`, `i8aurh42`, `yp7llda1`. DB1's live Started/Phase A notes supersede
  canonical-plan text saying its worktree is not created. No dirty DB tree used.
- S09 `i8aurh42` is draft and depends on DB1. P6 `yp7llda1` remains blocked;
  repair **21d2d1e7 is unaccepted**. Neither supplies accepted commit/output fences.
  P6 design at the DB1 pin, §§2.2/4/S09, specifies semantics, not usable signatures.
- **DB.1 binding user decisions (rev 6 §0) are settled; not reopened here or
  applied automatically to script artifacts:**
  - D1 full text for the last 20 revisions OR newer than 90 days, then
    actor/time/length without digest;
  - D2 erasure leaves only tenant, entry_uid, short_id, state, erased_at;
  - D3 deleted content follows D1, and the tombstone identity is kept forever;
  - D5 attachments 25 MiB default, configurable to 100 MiB;
  - DB.1 U1 sync tombstone until devices sync past, ≤30 days; U2 legacy files
    30 days after import, database backups under the 90-day rule; U3 identity
    tables rebuilt on tenant-content; U4 1-hour cutover budget;
  - D20 `event_log` retained 1 year;
  - D25 DB.1 retention wins over the security contract; D26 hourly backups,
    90 days; D27 goal auto-check-in append removed; D28 move into global;
    D32 tiered backups (7 days hourly, then daily to 90 days); D33 pause
    p95 ≤ 30 s; D34 revocation/deletion replay journal 120 days.
  - D29–D31 are rev 6 §6 technical defaults (event-ref redaction, sync floor,
    Phase B DDL approach), also settled for this packet's purposes.
- **Two retention conflicts are OPEN pending a user decision** (the parent is
  asking; not resolved here):
  - **Backups vs SDK-U1:** D26/D32 keep database backups 90 days, but SDK-U1
    limits protected script results/logs/plans/digests to 24 hours. Backups
    would retain those payloads up to 90 days.
  - **Event log vs SDK-U1 audit:** D20 keeps `event_log` 1 year, but SDK-U1
    allows only a 90-day content-free audit. Script events written there would
    outlive that window.
- SDK branch `sdk-script-execution-v1` stays exclusively owned here. A248f2fab and
  B42802cfb are author-verified only. No DB-writer edits, competing catalog,
  dependency/status changes, runner dispatch or independent-worker-review retry.

## 2. Reuse first: what DB1 already covers, and the exact gap

These are **proposed design methods**, not claims of implementation or approval.

| Need | Reuse at DB.1 rev 6 (`767dce41`) | Minimal additional contract for reviewer disposition |
|---|---|---|
| Entry identity/CAS/history | §§2.1/2.2/2.4: entry_uid nonreuse, monotonic revision, actor and operation_id; §3.1 ReadEntry/CreateEntry/UpdateEntry/PatchEntryMetadata/MoveEntry/DeleteEntry, plus TransitionTask (task status/completed_at/schedule + runtime set/clear + claim release), AppendEntryBody (note/append), CompareEntryRuntime (per-key runtime CAS) and RestoreEntryRevision as candidate script mutations, each only after its own preflight/authority contract (T6) | Resolve legacy locator once to tenant+entry_uid; require expected revision on script edits/move/delete/attach/detach. Reuse revision/view tokens, never create script revision/history tables. Runtime-only updates remain nonrevisioned. |
| Domain mutation receipt | §2.3 optional Receipt{Namespace, ID, Hash}, reservation+mutation+completion in one transaction; receipts' `namespace` CHECK is the closed set (`sync`, `reminder`, `project_drift`); §3.1 methods return a `Commit` value | Request an **extension of that closed namespace set** with one script-operation namespace in a later reviewed successor; bind receipt to execution+operation+verified owner. Extend fixed writer participation below, not reserve→HTTP call→complete. |
| Generated work | §3.1 UpsertGeneratedEntry and §2.3 receipt | Reuse generated-key uniqueness; it does NOT supply work permission, delegation, outbox or dry-run validation. Deny runnable-type/field writes without those contracts. |
| Attachment writes | §3.1 AttachToEntry/DetachFromEntry, §2.5 refs and §2.4 CAS | Same execution receipt/authority participation, atomic entry bytes+association+refs. No filesystem intent/quarantine scheme. |
| BLOB bytes/reservations | §2.5; §3.1 ReserveBlobUpload/AppendBlobChunk/SealAttachmentUpload/AbortBlobUpload; reservation expiry runs inside MaintenanceStep | Reuse chunked bytes for any protected replay payload via an extension of §2.5's closed `blob_refs.ref_kind` set (`attachment`, `reservation`; rev 4's `revision` kind was dropped), allocated by DB.1. Do NOT create a public attachments row or reuse its visibility to store replay. |
| BLOB quota/reclamation | §2.5 blob_tenant_usage (renamed from rev 4's tenant_blob_usage)/blob_refs; §3.1 CondemnUnreferencedBlobsPage/ReclaimCondemnedBlobsPage | Account replay staging/live bytes under an explicit script-purpose quota; add reviewed ref-kind/lifecycle integration, never fake an attachment/revision ref. Worker CPU/slots are not BLOB quota. |
| Output after commit | §3.1 `Commit` value (`EntryCommit` for entry methods); `Commit.Events` consumed by the §5 P8 outbox | A `Commit` proves DB commit, NOT protected delivery or durable external effect. S09/P8 must supply fixed in-transaction participation and final release/dispatch ordering. |
| Erasure/restore | §2.2 history/restore, §2.6 paged erasure (BeginErase/EraseStep), §4, §7 DB.6 ch7t990k | Add script lineage/payload purge and no-rerun admission-epoch handling; reuse DB6 lifecycle, never an SDK sweeper or security-journal clone. |

## 3. Logical relationships requested (not a catalog)

DB1 alone chooses physical relations, exact keys/indexes/constraints/triggers,
successor predecessor/pin and file ownership after review. Historical30/31/32/33
remain immutable. Recommend content successor first, then one coordinated script/
  authority extension as appropriate to DB1's §5 ordering, not modifying
DB1's pending content catalog just to unblock SDK. No version number is proposed.

### C1 — execution claim and operation linkage

- Execution key: **(tenant_id, execution_id)**, server-issued immutable execution
  ID; owner is P6's stable principal/local association, credential or grant ID and
  current authorization generations. Display names, raw bearer and frontmatter
  are never keys/authority. Store request mode, contract/policy versions, normalized
  requested/effective limits, owner-instance/admission generation, lifecycle state,
  timestamps and bounded non-content outcomes. Fingerprints are protected (SDK-U1).
- Dedup uniqueness: **(tenant, stable principal, endpoint, admission epoch,
  idempotency-key MAC)**. Namespace/epoch comes from trusted server composition;
  raw user keys may contain private text and are not persisted. Key rotation and
  MAC key custody belong to P6/P9, not a new SDK secret store. SDK-U2 controls lifetime.
  Endpoint is `/api/v1/scripts/execute`; request fingerprint covers source bytes,
  contract version, mode and normalized requested limits. Record effective limits
  and worker policy separately. Every record is still protected/owner-scoped.
- Operation key: **(tenant, execution, operation_index)**; contiguous bounded
  sequence, closed operation ID, dry-run/real mode and outcome. Composite parent
  relationship forbids cross-tenant linking. The execution owner cannot change.
  Outcome states distinguish planned, committed, failed, outcome_unknown; a
  control-only pending marker is not evidence of a domain commit.
- Atomic domain receipt key: **(tenant, namespace='script-operation/v1', receipt_id)**
  (literal proposed as an extension of rev 6 §2.3's closed set `sync`, `reminder`,
  `project_drift`; not allocated now);
  receipt_id is a server-derived unambiguous encoding of execution+operation index.
  Bind receipt Hash to operation ID, canonical typed input, expected revision,
  execution fingerprint and versions. One operation has at most one such receipt;
  receipt replay cannot mutate again. Both directions validate the same tenant,
  execution, owner and operation binding. No unauthenticated receipt lookup API.
- Reuse DB1 `Receipt` storage and `entry_revisions.operation_id`; do not duplicate
  domain receipts in a second script table. A script operation can reference zero
  or multiple resulting revisions (e.g. future fixed bulk operation), each with
  tenant+entry_uid+revision identity. Pure reads/plans have no mutation receipt.
  If a bulk call is independently committed per target, represent child steps
  explicitly and expose partial results; never label the batch one atomic receipt.
  Future P8 outbox/delegation records must reference that same composite operation
  and receipt identity plus constrained grant/reservation; they cannot fabricate a
  second success independent of the content transaction.

### C2 — source/protected-payload linkage

- Source evidence joins **(tenant, execution, operation_index)** to stable
  **(resource kind, resource identity, observed revision/view version)** plus the
  relevant grant/epoch evidence. Entries use entry_uid, not paths; attachments use
  tenant-scoped attachment/blob identity and complete owning-resource restrictions.
  Polymorphic resource kinds need closed kind-specific ownership validation or
  typed join relations, not an unchecked string-FK convention.
- The authoritative service resolves sources in the same snapshot that produces
  bytes. Persist bounded lineage before passing read bytes to the worker. Worker
  assertions never add/replace authority. Lists/search/graphs include every
  contributing readable resource and query visibility scope needed for counts/
  absence; S17 must define complete provenance, otherwise operation unsupported.
  Proposed initial lineage bound: 1,000 distinct evidence records / 64 KiB encoded
  total, overflow denies and retires output rather than dropping sources. Reviewer
  must account for complete epoch/query evidence within that budget.
- Before each write, use the conservative union of **all prior reads** for
  source→destination publication checks. Final logs/result/plan/journal/replay use
  **all reads in the execution**, including later reads. No JS information-flow
  claim, and no retroactive undo of earlier authorized commits/disclosures.
- Protected replay envelope references execution plus complete source evidence
  and a private sealed BLOB ref. Include result/logs/ordered plan/authorized
  operation detail only within approved bounds (SDK-U1). Opaque ref is not a download
  capability. Never serve it through the ordinary attachment catalog. The private
  BLOB ref is an extension of rev 6 §2.5 `blob_refs.ref_kind` (closed set
  `attachment`, `reservation`), allocated by DB.1.
- Delete/revoke first denies access. Source erasure purges dependent payloads,
  content-derived fingerprints and source/target relationships, not just the live
  note; detached no-rerun residue is classified separately in SDK-U2. No CASCADE may
  silently remove replay prevention and turn an old key into fresh execution.
- DB6's purge inventory includes **DB1 receipt completion bodies/Hashes**, revision
  operation_id backreferences, operation request digests/targets and private BLOB
  digests, not just a new replay relation. D1 pruning must not retain forbidden
  history metadata through script links; D2 erasure must leave only its settled
  tuple associated with that entry. Retire affected replay before removing lineage;
  missing lineage thereafter means deny, never "no restricted sources". DB1 must
  specify a retired receipt representation compatible with this purge, while the
  detached SDK-U2 tombstone continues refusing reuse. Other legitimately retained BLOB
  references follow DB1/DB6 ownership rules; script refs cannot prolong them.

### C3 — quota and worker-owner linkage

- One reservation per **(tenant, execution, reservation generation)**, linked to
  claim+stable principal+owner instance+fixed worker policy. Account global,
  tenant and principal slots, fixed memory/CPU ceilings, queue capacity, payload
  staging/live bytes and, separately, delegated paid/effect budgets.
  At most one unreleased reservation per execution across ALL generations; a new
  owner may reconcile/reap but may not start the same execution again.
- Claim/dedup and slot/byte reservation must be one short writer-owned decision;
  all competing coordinators see the same quota state. Reuse P10's authoritative
  reservation model if allocated; otherwise DB1 must allocate a narrow fixed
  execution-reservation participant, NOT a second generic quota framework.
- Release memory/slot credit only after trusted kill+Wait/join evidence; deadline
  expiry or lease loss is not worker death. Uncertain owners quarantine reservations
  until the launcher/reaper proves termination. Settle measured CPU once; use the
  reserved maximum on missing measurements, not a free refund. No rerun on takeover.
- P10/launcher reviewer recommendation: first supported topology is one Linux
  execution coordinator per installation, enforced by a generation-bound exclusive
  ownership mechanism; global2/tenant2/principal1 active, global queue8, at most
  one queued request/principal. These are **unapproved technical defaults**, not
  measured production capacity; existing local pool is not durable quota evidence.
  Multi-coordinator support requires equivalent cross-instance accounting/fencing.
  Existing paid-budget default zero stays zero; D5 is not a script-payload budget.

## 4. Fixed transaction ownership and typed method requirements

All names added here are **signature proposals**, not existing exported APIs.
Authority/operation handles are sealed server-owned types, not public DTO structs
the SDK or worker can construct. No generic SQL/Tx/callback or HTTP dispatch seam.

| Boundary / owner | Exact proposed typed requirement |
|---|---|
| P6/S04 admission | `BindScriptExecution(ctx, VerifiedRequest, ScriptAdmission) -> ScriptBinding`: verify actual mode, stable identity, explicit execute grant and current state; never nil/local/loopback fallback. |
| DB1/P10 claim | `ClaimScriptExecution(ctx, ScriptBinding, ClaimInput) -> ClaimResult{new,replay,in_progress,conflict,retired}`: atomically dedup+reserve; replay returns a protected reference, never starts a worker. |
| DB1 operation start | `BeginScriptOperation(ctx, ExecutionHandle, OperationDescriptor) -> OperationHandle`: owner-generation/sequence/mode check and bounded control marker; descriptor is not authority or a validated plan. |
| DB2/3/S17 reads | `ReadScriptEntry(ctx, ExecutionHandle, EntryRead) -> ProtectedEntry`: wraps DB1 ReadEntry snapshot and typed source capture, not a naked get followed by invented provenance. Each other read needs its own typed contract. |
| DB3 preflight | `PreflightEntryCreate(ctx, ExecutionHandle, EntryCreate) -> PlannedEntry`; corresponding Update/MetadataPatch/Move/Delete/Attach/Detach methods. Return normalized typed arguments, resolved identities, revision/preconditions, effect classification, source/target evidence and clearly provisional result; no effect or reusable authorization token. |
| DB1/2 mutations | Reuse §3 CreateEntry/UpdateEntry/PatchEntryMetadata/MoveEntry/DeleteEntry/AttachToEntry/DetachFromEntry input types, with a reviewed optional **sealed ScriptMutationContext** carrying operation binding, Receipt and source lineage. Missing/zero context is refused on the script path. Each fixed method owns the entire transaction below. |
| DB1 recovery/finalize | `ReadScriptOperationReceipt(ctx, ExecutionHandle, index) -> ProtectedOutcome`; `FinishScriptExecution(ctx, ExecutionHandle, TerminalInput) -> TerminalRecord`; `Get/ListScriptExecutions` use bounded owner+resource-authorized cursors. No append method can independently assert a committed domain outcome. |
| S09/S17 output | `ReleaseScriptRead(ctx, ExecutionHandle, ProtectedEntry, WorkerReplySink)` and `ReleaseScriptEnvelope(ctx, ExecutionHandle, ProtectedEnvelope, ResponseSink) -> ReleaseOutcome`: S09 owns actual bounded handoff/cancellation ordered with revoke, not `Authorize()->bytes` or a bool then send. Sinks are fixed transport adapters, no arbitrary authority/transaction callback. |
| P8 effects | Operation-specific `PreflightTaskSubmission`, `CommitTaskSubmission` and `DispatchAuthorizedEffect`/`CommitEffectResult` contracts: typed constrained delegation, finite reservation, transactional outbox+domain receipt and late-result fencing. Names/bindings await P8; no generic enqueue or detached request credentials. |
| DB6/P9 lifecycle | `PurgeScriptArtifactsPage(ctx, LifecycleHandle, cursor, limit)` and `ReconcileScriptRecovery(ctx, RecoveryHandle)`: fixed bounded purge/restore operations under existing lifecycle ownership, not goroutines started by graph construction. |

**One operation's linearization:** acquire DB1 reserved writer → re-admit exact
profile/content_authority → check live execution/owner generation/cancellation,
current P6 credential/principal/member/tenant/grants, complete source/destination
ACL/publication and CAS → repeat service validation against current transaction
state → mutate domain+projections+revision/sync+refs, complete DB1 Receipt and
script operation outcome, settle applicable reservation and append any allocated
P8 outbox record → commit → expose only through S09's output/effect boundary.

DB1's public fixed methods already open transactions: a wrapper must **not** open
another transaction and call them, nor call P6's pool reader while holding the
one-connection writer. DB1/P6 must implement fixed transaction-local helpers under
that owner, with exact reviewed signatures. No caller-supplied `func(tx)` hook.
No SQL writer is held while JavaScript, socket writes or a provider is running.
Protected read holders do not expose raw bytes to arbitrary broker callers;
S09 release covers read-reply frames to the worker as well as final user output.

Rollback leaves no committed operation/receipt/domain delta. A known no-effect
failure may be recorded in a later fixed control transaction. COMMIT ambiguity
requires querying the authoritative receipt before reporting; without proof report
outcome_unknown and do not execute again. If finalization/output fails after a
proven commit, retain that commit outcome but withhold protected bytes; do not
claim rollback. P8 delivery failure remains separate from content DB success.

**Preflight is not commit authority.** Reuse DB7's pure parser/rendering and DB3
service validation in both paths, then recheck state/authority in the actual writer.
Do not implement dry-run by calling a writer and rolling it back. Reads after
planned writes see committed data; unsupported provisional references return
`dry_run_dependency_unsupported`. First composition should prove typed entry
get/create/update plus required permissions/CAS/dry-run; adding any other method
requires its own validator/effect contract, not automatic105-operation exposure.
This sequence is an integration increment, not a reduction of V1 acceptance.

Dry-run's exact persistent allowlist: execution claim/dedup, operation planned or
failed markers, source evidence, quota/security accounting, and (per approved SDK-U1)
private replay staging/payload refs/chunks/settlement. These private-purpose bytes
are execution audit, NOT domain attachment BLOBs. Ordinary notes/revisions/runtime,
domain refs/bytes/FTS/embeddings/access counters/events/work/outbox/provider calls
must stay unchanged. Any inherited auth telemetry must be enumerated separately;
`unreviewed` in api/operation-policy.yaml cannot count as approval.

## 5. USER dispositions — APPROVED policy (SCRIPT-DECISIONS-20261006)

The user explicitly approved all three recommended answers below from packet
`ef83b9fcc9cb9caca5e9ead4e7dddfded3bdb18c` (recorded and read back in Brain plan
`qfcda7ct` and task `vggevclc`). They are now **binding product policy**, not
proposals. Approval does **not** allocate DB1 tables/profile, grant `script:execute`,
supply S09 fences, select a production launcher or activate anything: T1–T6 below
and independent acceptance remain required before any of it is enforced in storage.
SDK-owned pure enforcement of the approved bounds/expiry/replay/eligibility rules
lives in `internal/scriptexec/policy.go` (inactive, no persistence or caller).

These are **new script-specific** decisions; none reopens DB1 D1/D2/D3/D5.

| ID | Question and APPROVED answer | Consequence / dependency |
|---|---|---|
| SDK-U1 | What script material may persist, and for how long? **Never persist raw submitted source. Retain protected final result/log/plan envelope and content-derived fingerprints for24h after terminal state; interrupted artifacts expire no later than24h after the admission deadline. Result≤64KiB, logs≤16KiB/32records, full envelope including plan/journal≤256KiB. Keep only content-free actor/time/limits/closed outcome/count audit up to90days.** No sliding extension on replay. | Approved: classify script hashes/request/result digests as short-lived protected material rather than permanent minimal audit. Plan's retained-result promise is satisfied only during the declared window; after it use SDK-U2. These sizes are approved policy bounds (result/log match the inactive quarantine), not attachment D5 or a production quota grant. DB1/DB6/S09/P9 must prove enforcement. |
| SDK-U2 | What prevents old keys rerunning after payload purge/erasure? **After SDK-U1 expiry or source erasure, keep only a detached consumed-key tombstone (tenant, stable principal, endpoint, admission epoch, key MAC, consumed state), without execution/source/target IDs, content hashes or payload. Retain until that admission namespace is irreversibly retired; same key always returns a content-free409 `idempotency_key_retired`, never runs.** Before expiry compare fingerprints and return authorized stored result or mismatch409. | User approved potentially long-lived pseudonymous replay-prevention residue and the post-expiry conflict behavior, not eternal script/source history. DB6 erasure still leaves only D2 tuple associated with the erased entry; purge its lineage and linked content-derived metadata. Technical T4 must provide durable namespace retirement/restore safety before any purge of tombstones. If such residue is unacceptable, require a separately reviewed bounded server-issued expiring-key protocol; do not silently allow key reuse. |
| SDK-U3 | Who may receive the new submit permission in the first Linux-single release? **Only explicitly opted-in, verified owner/admin human credentials; no role receives it automatically. Auth-disabled ordinary REST stays unchanged, but script submission requires a verified owner-bound credential. Defer no-credential trusted-single script mapping and service/runner/member/OAuth script issuance until separately reviewed.** | This narrow initial eligibility/auth-off choice is now user-approved (separately from LINUX-FIRST). Existing plan permitted explicit trusted-local mapping; this approval defers it. If no-login scripts are required now, P6 must specify a real server-owned local policy/association and revoke source before implementation. Neither `admin:*` nor OAuth `mcp` nor resource editor/manager alone grants execute. |

The stable permission spelling is **`script:execute`**, already required by the
SDK plan. P6 must allocate its exact successor vocabulary/issuance/role ceilings;
old eleven-capability credential33 is unchanged. No new grant is inferred for
existing credentials. Effective execution authority = explicit execute permission
intersected with current role/credential/client/grant ceilings AND every ordinary
operation/resource permission. Workers never receive that credential or binding.

Revocation/deletion suppresses protected access immediately, even within SDK-U1's
window. Live erasure uses DB6's approved lifecycle bound (no later than24h), backups
follow the existing30day policy; restoration must replay current deletion/security
state before serving any bytes. Ordinary entry tombstoning is not D2 erasure:
entry revisions still follow settled D1/D3, without granting script replay access.
Script source evidence never pins entry revision payloads beyond D1/D3.

## 6. Finite TECHNICAL dispositions — owners answer accept/change/reject

Each answer must identify an immutable contract pin and responsible writer/files;
agreement to semantics is not permission for SDK to edit another lane.

| ID / owner | Exact proposed answer requested | Dependencies / evidence before consumption |
|---|---|---|
| T1 DB1/reviewer | **Keep content successor first; reuse §2.3 Receipt/§3 methods and §2.2 revisions. Allocate C1–C3 and private payload ref-purpose only in a subsequent reviewed extension, naming exact predecessor/profile/catalog/file owner without guessing a number.** | DB1 independent design review and D4/D6–D15 technical dispositions; no dependency on completed SDK feature. Publish narrow DB1 primitives before full DB14 acceptance; operational cutover remains separate. |
| T2 DB1+S09+DB2/3 | **Fixed DB1 writers own current P6/ACL/CAS checks, domain receipt, script outcome and outbox participant in one transaction; accept §4 types or return exact replacement signatures.** | DB2/DB7 methods, accepted current-authority P6 inputs (21d2d1e7 unresolved), S09 protocol, S16/17. Real independent-pool/subprocess revoke/commit/failed-COMMIT tests; no pool recursion or generic callback. |
| T3 S09+S17+P8 | **Execution-wide trusted source union; no broadening publication without a separate authorized contract; typed ReleaseScriptEnvelope owns actual bounded handoff and current-source authorization.** | Actual S09 cross-process release/revoke/journal protocol and S17 source acquisition, P8 effect/outbox composition. State exact linearization/cancellation/slow-sink behavior and unavailable-journal denial; no check-then-send substitute. |
| T4 DB1+DB6+P9+S09 | **Classify SDK-U1 protected bytes/digests separately from non-content audit and SDK-U2 tombstones; use DB1 private BLOB purpose, existing DB6 purge and S09/P9 current-security recovery.** | User SDK-U1/U2. Reviewer specifies admission-epoch-qualified keys and durable namespace retirement after restore: an old key can never be reinterpreted under a fresh epoch. If content backup may lose consumed keys, old epoch stays sealed/retired via the SAME acknowledged security journal, not a new script ledger. Interrupted records never resume. No restored grants, source payloads or work revived. |
| T5 DB1+P10+Linux launcher | **Reuse authoritative reservations or allocate only C3 participant; initially enforce one Linux coordinator, propose2/2/1active and8global queued, release only after death/join.** | Technical capacity/topology review, measured parent+worker budgets and crash/lease/reaper evidence. Paid budget zero unless separately configured. Durable byte accounting includes replay staging even for dry-run; BLOB D6 unlimited-single recommendation cannot make execution unlimited. |
| T6 P6/S04+DB3+P8 | **Implement explicit script:execute only in reviewed successor and fixed verified binding; reuse DB7 parser/DB3 typed preflight and allocated P8 delegation/outbox.** | User SDK-U3; credential repair/admission acceptance independent of this request; safe read/source/telemetry contract plus CAS/preflight positive/negative tests. Do not issue through old credential33 or generic entry fields. Unsupported effectful/provider methods stay denied. |

**Linux-single required:** approved DB-authoritative runtime composition/import
contract (DB1 alone is dormant), current credential binding+S09/ACL/source rules,
DB2/3 atomic operation receipts and real dry-run, SDK-U1/U2 lifecycle, measured launcher/
quota/reaping, real REST+stdio MCP multi-read/write and revocation/partial/uncertain
tests. Single mode does not waive any of those. Private colliding-tenant/hidden-
resource negatives remain acceptance fixtures; they do not unseal tenant routes.

**Deferred hosted:** S10 verified per-call/session in-process adapter and D06 VM/
public release gates. A/B transport discovery is not their acceptance. Native
macOS execution stays unsupported; macOS clients use remote Linux. No parent
independent-worker-review retry or rephrasing is authorized by this packet.

## 7. Handoff and next gate

Search/read existing `hxcyvu0i`, `i8aurh42`, `yp7llda1` coordination notes before
appending this packet ID, immutable SDK doc commit and only the relevant T/U rows.
No new task/container/catalog is needed. Parent routes P8/DB6/P10 dispositions
through existing lanes. SDK-U1–U3 are answered (approved); DB1 D1/D2/D3/D5 are settled.

Next implementable gate is **accepted T1 primitive allocation**, not a demand that
DB1 wait for all SDK/S09/P8 or final DB14 completion. T2–T6 can be specified against
those primitives while their actual implementation dependencies remain mandatory.
No runtime/schema/auth/source code is delivered by this document. A/B remain
author-verified; this packet is design coordination, not C–F acceptance.
