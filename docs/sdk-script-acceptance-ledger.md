# V1 acceptance ledger — SDK-TENANCY-20261005

Starting checkpoint: `2228f1f1ca97ff8108573016d869b89ac239c207`.
This ledger supersedes stale pending counts in earlier handoffs, not the plan.
Manual exclusive ownership, blocked dispatch reservation and `prompt_only` remain.
No enablement, merge, push, deployment or task completion is authorized.

## Current controlling disposition — 2026-10-06

Parent/user explicitly allocated **A discovery** and **B stdio SDK convergence**
in `vggevclc`, `ap90gj4e`, and `zsief5fj`. Earlier unanswered-allocation notes below
are historical, not current blockers. Sole continuation writer is
`ses_eee8ce506ffeycyBFJAGoE0lPz`, starting clean `b6f0c1ff` with no dispatch lease.
A/B now have bounded author implementation/verification evidence below; allocation
and author checks are not independent acceptance or whole-feature delivery.

**LINUX-FIRST-20261006:** server execution targets Linux; macOS clients connect
remotely. Native macOS execution remains unsupported/deferred, not a Linux-first
completion blocker. Linux production launcher and full integration acceptance,
plus the separate D06 hosted gates, remain mandatory. Scripts stay unavailable.

**DB-AUTH-20261006:** the shared database is the target authority for entry content
and attachment bytes. SDKs remain storage-agnostic; legacy filesystem tests are
compatibility evidence. Storage migration belongs to `mt-db-authoritative-storage`
(DB1 `hxcyvu0i` through DB6 `ch7t990k`), not this SDK writer. C–F composition must
consume that reviewed allocation, never resume obsolete filesystem isolation.

Main has advanced from the original recorded target SHA. Parent owns eventual
whole-feature integration into `main` and fresh postmerge verification. No merge
or completion is authorized for these partial A/B slices.

**Current next gate — SDK-DB1-CF-20261006:** DB1 `hxcyvu0i` is active, sole
writer on `codex/mt-db-authoritative-storage`; immutable Phase A design `d365a4aa`
is proposed and Phase B DDL awaits independent design review/profile allocation.
The SDK-owned [C–F disposition packet](sdk-script-allocation-proposal.md) replaces
the older broad table/filesystem proposal with precise DB1 reuse, transaction,
retention and typed-method requests. **User decisions U1–U3 are APPROVED policy
(SCRIPT-DECISIONS-20261006, see below)**; technical T1–T6 remain **unapproved
proposals**. DB1 D1/D2/D3/D5 decisions are already settled; not asked
again. No further generic prototype work or new schema has been substituted for
this actual owner handoff.

**SCRIPT-DECISIONS-20261006 (user-approved policy, not allocation):**
U1 no raw source persisted; protected result/log/plan/digest envelope 24h after
terminal (interrupted: 24h after admission deadline, no sliding extension);
content-free audit 90 days; result ≤64KiB, logs ≤16KiB/32 records, envelope ≤256KiB.
U2 detached consumed-key MAC tombstones until irreversible namespace retirement;
reuse returns content-free 409 `idempotency_key_retired`, never reruns.
SCRIPT-DECISIONS-20261006b (approved): live-fingerprint mismatch 409
`idempotency_key_conflict`, active claim 409 `idempotency_key_in_progress`;
late-finish retention stays deadline-anchored (expiry ≤ admission deadline+24h). U3 only
explicitly opted-in verified owner/admin human credentials; auth-off ordinary REST
unchanged; credential-free submission deferred. These do NOT allocate DB1 schema/
profile, grant `script:execute`, supply S09 fences, select a launcher or activate
anything. Pure SDK-owned enforcement of these rules: `internal/scriptexec/policy.go`.

**G — Linux-first launcher (author evidence, disabled):** see final section
"G Linux production launcher". Author-implemented and real-Linux tested; NOT
independently reviewed, not wired to any route and not activation.

## Classification

**R** = runnable in owned files now; **E** = exact external interface/allocation;
**P** = platform proof/decision; **V** = existing bounded evidence, not full acceptance.
A row can have both an independently implementable part and a blocked composition.

| V1 criterion | Class and current evidence | Remaining work / completion oracle |
|---|---|---|
| OpenAPI inventory and both SDK namespaces | V/R: 104 operations, router inventory, external Go module and installed Node fixtures | Audit every operation's effect/resource/precondition/provider metadata; remove stale matrix wording; guard completeness. Task/automation creation uses entries, not new routes. |
| SDK transport, immutable identity, pagination, errors | V/R: cancellation/rebind, no ambiguous write replay, bounded responses/SSE, typed errors and error-only conflicts, deterministic generated DTOs | Audit API compatibility/version failure, examples and release docs; no retry is safer than inventing safe retry. Existing legacy route ambiguity must remain disclosed. |
| Capability/version discovery | V: A implemented; author evidence in final section | Authenticated single-mode discovery plus public Go/TS negotiation, wired IDs, independent script flags, old-server/auth refusal and no hidden metadata. Parent independent acceptance remains separate. |
| Stdio MCP SDK convergence and backward compatibility | V: B shared public SDK transport implemented; author evidence below | Real authenticated child stdio preserves legacy DTO/error/local-file/discovery behavior. Hosted constructors unchanged. Four-method delegation expansion withdrawn; parent independent acceptance remains separate. |
| Hosted MCP sealed authorized adapter | E: S10 `ap90gj4e` draft | Accepted immutable operation-specific adapter and verified caller/session binding, never loopback reconstruction. Real per-call/session/cross-principal tests. |
| Worker async/final-expression/return semantics | V/R: `2228f1f1` compile-only grammar fallback and promise/thenable settling, no runtime reevaluation | Full JS facade/unsupported-method behavior and JSON-only boundaries; native corpus and examples. No actual service dispatch implied. |
| Bounded console, errors and protected results | R/E: diagnostics currently discarded; result byte bound exists | Bounded quarantine and actual console fixture; source union from trusted parent only, no ambient logger or default formatting leak. Real release must use S09/S17 source-wide fence, not a new authorization callback masquerading as it. |
| Pinned reproducible runtime/toolchain | V/R/P: experimental official source digest, immutable image, hardened ELF and relocated byte equality | Preserve generation/build recipe, expand native denial corpus; reviewed production runtime/launcher selection still required. No production command selected. |
| Per-worker compile/wall/CPU/heap/native memory/IPC limits | V/R/P: Linux experimental limits, native denials, protocol budgets and hostile corpus | Further source/serialization/native abuse and prefork races runnable. Linux local container is not hosted VM acceptance. |
| Cancellation/crash/death/reaping and shutdown | V/R/E: inactive Start/Wait, Linux creating-thread PDEATHSIG, native subreaper tests | Bounded admission-to-start and shutdown ownership tests; real graph lease integration only through approved composition. Dead-parent orphan reaping still needs reviewed external init. |
| Aggregate admission, fairness, memory/CPU and lifetime | R/E: not implemented | Inactive bounded local scheduler with principal/tenant/global reservations and fair waiting, cancellation/shutdown join, no credit before Wait. This is not authoritative multi-server quota; durable reservation/topology allocation remains E. |
| macOS confinement | Deferred/unsupported by LINUX-FIRST-20261006 | Remote clients supported; native execution disabled. No root helper/entitlement/degraded fallback. Not a Linux-first V1 blocker. |
| `script:execute` and trusted-single admission | E: absent from fixed credential33 | P6 chooses explicit server-verified permission and principal/auth-generation binding. OAuth `mcp` and admin wildcard cannot silently grant it. No nil-auth or display-name shortcut. |
| Per-operation registry/authorization/CAS | R/E: protocol syntax is not authorization; script exposure false everywhere | Descriptive registry/shape checks runnable; live adapter needs S09/S16/S17 and effect APIs. Every supported write requires safe reusable preflight, current auth, ordinary rights and expected revision. |
| Dry-run ordered plan and provisional dependencies | E/R: request fingerprint only, no broker | Pure plan shape/unsupported dependency validation runnable; service validation ownership required before real composition. Never writer-then-rollback. Prove SQL/files/CAS/events/queues/providers unchanged except exact control allowlist. |
| Continuous revocation and source restrictions | E: S09/S16/S17 draft; S15 private evaluator accepted only | Same-writer check+commit and fenced output ordered with revoke; all-read-source lineage, unknown/outage deny, broader publication denied absent separately authorized contract. |
| Real writes, partial success and uncertain outcomes | E/R | Pure journal/state validation runnable; actual atomic receipts/commit fences and service semantics required. No false atomicity across Markdown/SQL/provider. Stop on first error; never blind rerun. |
| Durable idempotency, audit APIs, quotas and retention | E: no schema allocation | Exact coordinator successor/predecessor/catalog, scoped receivers, claim fingerprint, one-owner concurrency, protected replay and purge/restore policy. Current source IDs are not release authority. |
| REST/SDK/MCP execute and audit methods | E/R | DTO/client contracts may be drafted only without advertising server support; actual routes need approved persistence/authority/launcher. One real MCP multi-read/write script remains unproven. |
| Colliding-org/restricted-resource/role/revoke acceptance | E | Real accepted authenticated integration base, not synthetic contexts: selectors, OAuth escalation, hidden counts/graph/attachments/cursors/logs/audit/replay, generic work/spend smuggling. |
| Migration/foreign snapshots/COMMIT cleanup | E | Use allocated exact migration and actual integration base, preserve roots/sync/tombstones/manifests. No ad hoc tables or historical profile edits. |
| Default-off/configuration/disable/observability | R/E | Inactive stop/admission tests and operator documentation runnable; production config/composition requires ownership, all unproven profiles remain unavailable. No log content in ordinary telemetry. |
| Documentation/examples/CI/final checks | R | Public Go/TS quickstarts exist; finish script reference/security/limits/incident/compatibility examples with explicit inactive status. Run frontend checks and historical tenant gate against reviewed base, full tests/build/vet/lint, SDK and native races. Parent independent verification separate. |
| Public hosted availability | E/P, outside V1 activation authority | D06 reviewed VM isolation plus P5/P6/ACL/P8/P10 and release gates. Never certify from local subprocess/container evidence. |

## Fresh owner evidence and exact requested decisions

Recalled all owners after `2228f1f1`. The coordinator has newer integration
acceptances (`b125dbd7`, `6d2db8e2`, P9/P8 candidates), but **none allocates SDK
script interfaces**. Newer unrelated acceptance is not approval of this request.

1. **A — discovery:** coordinator `zsief5fj`: approve/refuse new
   `internal/api/sdk_capabilities.go` and adjacent tests plus one authenticated
   `/api/v1/capabilities` registration in `internal/api/router.go`. Single-mode
   only; existing tenant allowlist unchanged. Request is already recorded there;
   fresh recall contains request but no reply. No script permission is granted.
2. **B — stdio:** S10 `ap90gj4e` and coordinator: approve/refuse new
   `internal/mcp/sdk_stdio_client.go`, necessary private client field, one
   `internal/mcpserver/server.go` call site and parity tests. Existing hosted
   constructors unchanged. Both broad and narrowed requests remain unanswered.
3. **C — capability/schema:** P6 `yp7llda1` is blocked at repair candidate
   `21d2d1e7`; credential33 vocabulary immutable, no script successor. Coordinator
   must name exact predecessor/profile/catalog and author-owned files for the
   execution/operation/source/replay/reservation proposal in
   `sdk-script-allocation-proposal.md`, plus explicit trusted-single mapping.
4. **D — fences/journal:** S09 `i8aurh42` draft, P5.5 `krmkfe26` draft,
   P5.9 `5tmpau6l` draft. Need fixed same-writer mutation records and protected
   output release/cancel/join protocol; P9 owns the SAME independently durable
   journal lifecycle. No admission-only check/send shortcut. P5's newer durable
   attachment proposal `1e425e2f` is itself awaiting owner decisions, not a seam.
5. **E — ACL/publication:** S15 `rihm769f` accepted `23664fe4` is private policy
   only; S16 `wuge6wiv`, S17 `86ij8ynq`, S18 `cfo4q3z1` remain draft/full
   composition incomplete. Require live source set acquisition and current
   resource/output/publication decision under D, not caller resource assertions.
6. **F — effects:** P8 `8gxc3qi1`, notices `4v1bv798` draft; need actual
   source-limited delegation/reservation/outbox/result APIs. Reminder-specific
   source allocation to the P8 writer is not a general SDK allocation.
7. **G — platform/topology:** owner must approve production Linux launcher/init
   and supported topology; macOS needs enforceable memory profile or reviewed
   VM/service alternative. Multi-server reservation authority and D06 remain
   separate. No privileged helper or production enablement requested.

## Runnable execution order

1. Bounded console/result quarantine and native console transport (Route T).
2. Full inactive JS facade/JSON-only/unsupported operations and native corpus
   (Route T), preserving all script-off metadata and no service dispatcher.
3. Inactive local aggregate scheduler/start/stop/join/fairness and resource
   reservation tests (Route T); document multi-server boundary precisely.
4. Operation metadata and compatibility decoder, schema/examples/CI/docs audit
   (T for behavior; S for declarations/docs), then final verification.
5. Consume any actual acknowledged narrow allocations and test real composition;
   otherwise report exact remaining decisions above, not generic integration delay.

Runnable scope is **not exhausted** at this ledger's creation. Independent review
of immutable `2228f1f1` is running with the parent and does not block this work.

## Subsequent execution and precise remaining queue

Independent `va815e0d` **FAILED** `2228f1f1`: serialization callbacks advanced the
call sequence after terminal sequence capture. That verdict supersedes any
suggestion that the initial completion tests proved serialization generally.

- `654752e9`: observed native RED then GREEN with actual `ProtocolSession`;
  serialization occurs once before C envelope construction, queued serializer
  jobs drain before terminal, unsupported top-level JSON values refuse. Added
  bounded native console and parent-only source-union quarantine. It deliberately
  has no protected release method; S09/S17 remains necessary.
- `66e49fd7`: additional observed RED/GREEN for queued/async Promise rejection;
  engine rejection/handled count prevents false successful terminal output while
  allowing caught rejection. Four affected native suites: 55 subcases pass,
  62.244s. Full 45 tested Go packages pass (many cached), vet/build/lint0issues.
- `3b166b75`: inactive local fixed-policy capacity and bounded queue, tenant then
  principal selection, cancellation and shutdown join. Five admission tests
  repeated20 under race pass; one actually kills/Waits a helper process before
  Close returns. Full45 tested packages pass (many cached; storage142.513s),
  vet/build/lint0issues. This is **not** a measured host aggregate cgroup/CPU
  budget, cross-server quota or production graph/store shutdown proof.

### Runnable next steps (not external blockers)

1. **Facade:** current native `brain` still implements only `entries.get`.
   Implement a closed, explicit JS binding inventory mirroring actual TS public
   names (note `brain.health()`, `brain.search()`, `brain.inject()` differ from
   operation IDs), argument/default/JSON behavior and stable unsupported errors.
   Do not equate all104SDK methods with script support; provider/stream/binary or
   writes without preflight must remain explicitly unsupported. No raw HTTP or
   exposed generic RPC. Test both method/argument parity and real native bounded
   IPC; parent performs independent registry validation. The exact supported
   service subset awaits E, but descriptor/binding/denial mechanics are runnable.
2. **Structured errors:** console quarantine is implemented, but safe fixed-code
   worker error outcomes plus bounded source-location metadata are not. Never
   stringify arbitrary thrown objects into an ambient error/log; callbacks during
   exception inspection can reenter. Add real native regressions and parent
   sequencing/terminal/duplicate-outcome tests; retain raw-content suppression.
3. **Aggregate/lifecycle evidence:** add actual simultaneous sealed-child fixed
   memory/CPU pressure, cancellation during launch/prefork and independent-owner
   progress tests around the local pool. Existing callback/barrier tests do not
   prove OS aggregate budgets. No production graph leasing/reaper claims without D/G.
4. **Quarantine boundary corpus:** expand exact source-count/source-byte/result
   bounds, deep/invalid UTF8 JSON, cancellation during formatting/retirement and
   malformed native log frames. Live release/revocation still requires D/E.
5. **Metadata:** `api/operation-policy.yaml` now makes the104-row conservative
   inventory machine-readable with contract/matrix coverage, legacy-scope parity,
   precondition/effect/provider fields and global script-off/unimplemented-preflight
   guards. It is not a broker allowlist or proof of per-handler current resource
   enforcement. Capability client parser/old-server refusal can be tested without
   adding an unallocated endpoint; do not invent server availability.
6. **Docs/examples/checks:** add tested script reference/examples, precise operator
   disabled/incident/limit guidance and compatibility notes. Run frontend checks
   (this checkout currently has no web/node_modules), package checks and historical
   tenant gate against a reviewed actual base. Existing full Go suites are not
   fresh frontend or newly integrated tenant-profile evidence. Public publishing
   and licensing disposition are not authorized by a package build.
7. **Pure plans/journals:** ordered-plan/provisional-reference and journal-state
   validation may be built without services, but cannot satisfy actual dry-run,
   partial commit, durable idempotency or replay requirements. Prefer existing
   operation contracts; do not design a rival transaction/authority interface.

### Remaining external decisions (unchanged after fresh recall)

A/B: actual discovery/stdio file allocation and exact composition sites. C: exact
schema/profile, permission/trusted-single binding, quota and retention allocation.
D: same-writer mutation/security-journal and source-wide protected output protocol.
E: live current resource/source/publication adapter. F: provider/work delegation,
reservation/outbox and reusable side-effect-free service validation ownership.
G: supported production launch/reaper/topology, macOS enforceable memory decision,
separate hosted D06 VM gate. Concrete owner IDs/files/proposals are above; no reply
is inferred from newer unrelated integration acceptance.

This is a context-capacity handoff, **not exhausted runnable scope**, not a stop
waiting for independent review, and not V1 completion. Continue the same exclusive
worktree, preserve these commits and take the next failing test from the queue.

### Final author handoff evidence

Latest implementation is `aeef43a3`, following metadata inventory `57847362`.
The local admission audit caught a further late-success race: GOMAXPROCS1,
Close from the running callback, three RED repetitions returned nil before the
asynchronous cancellation bridge ran. The fixed pool synchronously observes its
lifetime after the callback as well as before. All six admission tests repeated20
under race pass (1.416s). Final full45 tested Go packages pass (many cached,
scriptexec0.257s/storage132.863s), fresh vet/build pass and lint0issues.

Full native evidence at `654752e9`:43 passing top-level test/fuzz roots,8 explicit
platform/helper skips,243.282s, log `sdk-serialization-fix-native.log` in the approved
opencode temporary directory. At `66e49fd7` the affected native completion,
console, hostile compiler and serialization suites55subcases passed62.244s;
unchanged lifecycle/hardening checks were not unnecessarily repeated afterward.
Metadata contract races2packages pass4.143s/1.893s. These are author observations;
no new independent PASS is claimed after `va815e0d`.

All commands completed foreground; no pending checks. Docker's final
`ps -a --filter name=brain-quickjs-probe-` returned no containers. No shared API,
auth, storage, config, apiserver, tenant, MCP, command or module files changed
since `2228f1f1`; all source changes remain owned/inactive. TDD gate exited.

Resume with the **Facade** item above, or regression-first parent review feedback.
Do not reinterpret the quarantine as authorized release, local slot accounting as
measured host or durable multi-server quotas, or descriptive policy metadata as
service preflight. Exact platform and C–G contracts still gate real execution.
Full facade, structured safe errors, actual aggregate pressure/startup corpus,
quarantine edge corpus, compatibility decoder, operator/script examples and
frontend/historical gate checks remain useful runnable work; they are not waived
by this handoff and are not all externally blocked.

## Fresh takeover: facade boundary (author evidence only)

Sole writer `ses_ef17f4d15ffeiMziW4dpsKY7jO` took over clean
`3786c13fe2a876c037c9c305194994ac83304bf6`; fresh dispatch lease absent. Blocked
manual reservation and prompt_only unchanged. Native tests now compare the full
105-name surface to the actual TypeScript client, test104 explicit fixed-code
denials with zero operation IPC, frozen/null-prototype namespaces, optional
undefined get argument, no identifier coercion, and denial without evaluating
hostile argument getters/toJSON. `entries.get` remains the sole fixture operation.
This does **not** implement service bindings, Promise-return or full argument/default
parity for the unsupported operations, nor permit any write/provider/stream/binary
operation. Those still require approved service subset and preflight composition.

Observed RED: missing inventory terminated136; get's optional undefined and invalid
arguments terminated130, empty id emitted a call then132. GREEN: native facade
inventory plus8 argument cases, existing17 completion and22 serialization cases
pass under host-race (70.254s). Full45 tested Go packages pass (many cached,
storage129.733s); native source remains testdata, no activation.

**Independent verification pending:** parent reports the independent3786c13f
review session ended with a tool/provider cybersecurity-policy flag before returning
a review. This is not a code finding or PASS; no retry/rephrasing to bypass that
safeguard was attempted. Serialization/control changes still lack independent
acceptance. Parent owns resolution; no full task completion is claimed.

### Follow-on Promise parity and frontend evidence

The fixture now creates intrinsic Promises for get results and fixed-code failures;
native RED for then/catch and synchronous-throw detection preceded the change.
Existing serialization regressions still perform the same calls during getters/
toJSON and retain exact parent terminal-sequence checks; their synchronous `.value`
reads were replaced by fixed values after initiating the call because actual SDK
responses are Promises. The awaited control continues checking the actual reply.
Native4 suites pass56.875s (11argument,17completion,22serialization subcases plus
full inventory). Full45 Go packages pass, many cached (storage154.109s), fresh
vet/build/lint0issues. No concurrent RPC or additional operation dispatch added.

Frontend lockfile installed with `npm ci --ignore-scripts`; typecheck succeeds,
1269/1269 tests pass with0failures/0skips and existing Zustand unavailable-storage
warnings. Vite/PWA build succeeds (2741modules,33precache entries), output remains
ignored/uncommitted. Installation reports **12 existing dependency vulnerabilities
(1low/3moderate/8high)** plus source-map/glob deprecations; no audit fix or dependency
change was made. This is not a zero-vulnerability or browser-E2E claim.

### Fixed terminal-error protocol (inactive)

Added a distinct terminal `error` frame with exactly the fixed code vocabulary
`compile_failed`, `script_failed`, `result_invalid`. Optional source line/column
must each be integers1..32768; no text/filename/stack or extra field is accepted.
These are untrusted bounded hints, not verified source provenance. Native worker
deliberately emits no location rather than inspect potentially hostile exceptions.
Thrown proxies, Error stack accessors and rejection objects are never queried or
formatted; six real native regressions first failed for missing terminal errors,
then passed with one fixed terminal, zero unexpected calls and zero stderr.
Terminal/error sequence, byte budgets, retirement and duplicate outcome rejection
are parent-validated. Hard kills/limit/IPC failures can still have no terminal;
Wait/join remains mandatory. This is neither an operation commit receipt nor
permission to release prior results/logs, and no journal/fence is invented.

Final full opt-in native host-race suite:54passing top-level test/fuzz roots,
8explicit platform/helper skips,341.180s. Linux inner Go-parent test remains
non-race cross-built. Existing console, serializer, hostile compiler, parent death,
Wait/reaping, hardening and relocation-reproducibility tests pass. Relocated binary
SHA256 `3dcc9b72fd3bdcea30c689b55344d7e0831e24446e19f609c3052d9d47c660d6`
matches both directories; no production runtime selection implied. Full45 tested
Go packages pass (many cached; storage166.706s), fresh vet/build/lint0issues.
Logs: `sdk-terminal-errors-{suite,full-native}.log` in the approved opencode temp.
Independent review remains pending under the previously recorded tooling blocker.

Fresh S10 and coordinator recalls still show no A/B allocation acknowledgement.
Coordinator now has unrelated24f1f0cc reminder integration candidate; it is not
script schema/permission/output/adapter allocation and was not imported.

## Approved-base local gate and iterator correction

Parent explicitly approved `7bea47d13bb99502035f51e32dd0c10e523581db` for this
local historical gate: freshly verified main and origin/main equal that SHA,
feature HEAD was `f467b1a5fea1b46aac5898ece2d810ca8b1382c5`, and merge-base
equals that same unchanged target. This is not a candidate baseline. If target
integration changes or a PR opens, resolve the actual reviewed integration/PR
base again; do not automatically reuse this pin.

`CI=1 GOMAXPROCS=2 BRAIN_STORAGE_RATCHET_BASE=7bea47d13bb99502035f51e32dd0c10e523581db just tenant-isolation-gate`
exited **0**. Complete output read: full storage110.793s, tenant0.923s,
selected apiserver17.466s/indexer0.359s; focused race storage12.663s,
apiserver179.498s/indexer1.668s. No goldens, guards or production tenant files
changed. Log: `sdk-historical-tenant-gate.log` in approved opencode temp.
This is author-owned local historical evidence, NOT independent acceptance.

Facade comparison to the actual TypeScript client found `entries.iterate`
incorrectly returning a rejected Promise. Three real native RED cases established
the mismatch. It now returns a lazy async generator that rejects on first next,
then completes; early return closes without rejection; for-await reports the fixed
unsupported code without inspecting hostile query/options. The operation remains
unsupported and issues no IPC. Native facade inventory plus14cases pass27.237s
under host race; full45 Go tested packages pass (many cached, storage153.044s).
This corrects a return-shape gap, not full argument/default/service parity.

## Current acceptance accounting (supersedes stale queue wording above)

All rows remain incomplete unless their narrow evidence boundary says otherwise.
The original plan and acceptance criteria are not reduced to the runnable subset.

| Acceptance area | Author evidence now | Remaining work / owner |
|---|---|---|
| Public Go/TS inventory, typed DTOs and contract | 104 existing operations, generated contracts, installed/external client fixtures; policy inventory tests | Repeat SDK/package/contract checks for final candidate; no script-service support inferred |
| SDK binding, cancellation, errors, pagination | Existing SDK tests and real-handler fixtures | Compatibility decoder/old-server refusal is still runnable; discovery wire allocation is A |
| Capability discovery | A implemented: authenticated single-mode manifest; public Go/TS exact-version negotiation; real external consumers |105 wired-operation contract, independent all-false script flags; no resource grants or script activation. Author checks recorded below; independent acceptance remains parent-owned |
| Stdio MCP convergence | B implemented: same public SDK HTTPTransport used by typed SDK and stdio; authenticated real child parity passes | Legacy DTO/error/local-file adapters retained, hosted constructor unchanged. Transport convergence, not replacement of every MCP DTO with typed SDK calls; parent independent acceptance pending |
| Hosted MCP | Unavailable | S10 sealed adapter, current principal/session binding and live cross-principal tests |
| Full JS facade | 105-name closed surface, intrinsic Promise returns, lazy iterator refusal, get fixture, argument/error cases | **Not complete.** Per-method argument/default normalization and broader fixture mapping are runnable; actual supported service subset/preflight is C–F. Unsupported/provider/binary/stream/write operations must not gain authority from test mappings |
| Unsupported facade corpus | 104 methods × 10 argument forms tested in real native child; zero IPC and zero hostile inspection | This proves denial only, not DTO validation or service parity |
| Completion/errors/console/quarantine | Native completion/serialization/error/console tests; exact source/result/depth/retirement boundaries and terminal races | Current source-wide authorized release/publication requires D/E; malformed native console-frame corpus can still expand |
| Runtime/build provenance | Pinned source/image; hardening and relocated byte equality previously observed | Repeat reproducibility after C change; independent runtime/native-compromise review and selected production launcher G |
| Per-worker limits and denial | Native CPU/AS/framing/serialization/compile/fresh-state/cancellation corpus | More pressure/launch-race cases runnable; not a universal DoS proof |
| Aggregate admission and lifecycle | Local fair fixed-slot pool; cancellation/join; direct Linux parent-death/reaping tests | Simultaneous sealed-child pressure and prefork/launch races still runnable. Authoritative topology/quota/init and graph lease composition C/D/G |
| macOS / hosted platform | Linux-first approved; macOS remote clients, native worker unsupported/deferred | G: Linux production launcher/full integration evidence; D06 hosted VM gate separately |
| Dedicated execute permission and single binding | No capability granted | C: successor vocabulary/trusted-single mapping; do not alter credential33 |
| Per-operation auth, CAS and preflight | Descriptive policy metadata, no dispatch authority | C–F: approved current rights/resource validation, same-writer fences, safe reusable preflight |
| Dry-run/provisional plan | Request fingerprint only | Pure ordered-plan/dependency validation runnable; real SQL/files/CAS/queues/provider proof requires C–F; no writer-rollback substitute |
| Revocation, source restrictions and publication | Bounded trusted-parent source union without release | D/E: current source-wide acquisition/check/commit/output ordering and live outages/revocation tests |
| Real writes, partial/uncertain outcomes | Fixed worker errors do not claim commit state | Pure journal-state validation runnable; durable atomic receipts/fences/service semantics D/F |
| Durable idempotency, audit, quota, retention | No new tables or replay endpoint | C/D: exact successor/catalog, scoped ownership/receipts/security journal and purge/restore contract |
| REST/SDK/MCP execution and audit flow | No execute interface wired | C–G plus A/B; real multi-read/write MCP call still required |
| Colliding org/ACL/role/revoke acceptance | Existing historical tenant gate passed at actual approved target | Real accepted authenticated integration base and C–F composition, not synthetic identities or tenant predicates alone |
| Migrations, foreign snapshots, COMMIT cleanup | Unmodified historical tenant gate passes | Exact allocated script migration and integrated foreign-data/COMMIT fixtures remain C/D |
| Default-off, disable and observability | No runtime selection, route, config or tool; local stop/admission tests | Production configuration/composition ownership; authoritative joined disable/telemetry tests C/D/G |
| Documentation/examples | New inactive operator/reference guide and three native-tested example files | Keep docs synchronized with actual future service bindings; no install/enablement claim |
| Build/test/frontend/SDK final checks | Go/vet/build/lint, frontend1269 tests/typecheck/build, approved-base tenant gate; exact prior logs above | Fresh final aggregate/package checks as candidate evolves; frontend12 existing dependency findings unresolved |
| Independent acceptance | No new independent PASS | Parent/provider tooling blocker remains; never retry/rephrase to bypass; author checks are not independent review |
| Public hosted availability | Disabled, outside activation authority | D06 plus P5/P6/ACL/P8/P10 and release approval; no enablement/merge/push/deploy authorized |

The three examples are fixture-only (`read-pair`, unsupported write, unsupported
iterator), not a promised API deployment. `TestQuickJSScriptExamples` and
`TestQuickJSFacadeUnsupportedArguments` passed under host race in52.626s.
See [operator/reference status](script-execution-status.md) for precise limitations.
This accounting is not exhausted runnable scope: full facade argument/default
mapping, aggregate/launch corpus, compatibility decoder and pure plan/journal
validation remain author work. Do not report them as completed or exclusively
externally blocked.

### Continuation checkpoint verification

After the iterator correction and example/corpus additions, full opt-in native
`go test -race ./internal/scriptexec -count=1` passes386.267s (same opt-in platform
skips remain; this terse run does not report a fresh per-root count). This includes
the existing relocation/hardening, parent-death, console and serialization tests.
Full45 Go tested packages pass (many cached; storage240.577s), fresh vet/build pass
and lint0issues. Public Go SDK/contract/bootstrap races pass1.604s/8.126s/2.153s.
TypeScript SDK typecheck/build and36/36 tests pass0fail0skip. OpenAPI validation
passes with the existing **one ambiguous-path warning** between task delivery and
feature-get paths; it was not suppressed or resolved by changing shared routes.
Logs: `sdk-iterator-examples-native.log`, `sdk-examples-suite.log` in approved temp.

No production/service/schema/auth/MCP/tenant files changed in this continuation.
All new evidence is author-owned. Independent review of3786c13f and later remains
pending under the provider flag. No retry, rephrasing, activation, merge, push,
deployment or task completion occurred. Continue the runnable items above; this is
an explicit context handoff, **not a claim that full JS facade or V1 is finished**.

## Continuation: pure argument/default mappings

Exclusive writer `ses_ef137ec1fffear7Og33cArX1ES` confirmed clean starting
`68b7dc3d29c6fa3aa311c86461b47759a96b4e09`, absent dispatch lease and persisted
ownership transfer (PATCH timed out; recall confirmed it, no duplicate PATCH).

The new **inactive pure fixture** maps all101 JSON-shaped public methods' positional
arguments and defaults to operation IDs and detached argument objects. The other
four public methods (binary upload/download, callback stream, iterator) explicitly
refuse. Two Node tests derive all105 signatures/defaults from the actual TypeScript
AST, rather than another expected inventory. Required string/object/boolean/limit
types, non-JSON values, accessors, transport options and excess args are checked.
Initial RED was `health defaults=false: undefined` and missing expected refusal.
Final Node38/38 pass; native8mapping cases +6refusals pass under host race28.910s,
with zero operation IPC and continued denial through the real fixture `brain`.

This does **not** change the native facade's accepted operations, install the mapper
as a capability, implement DTO/service preflight, authorize writes, or define final
broker wire arguments. It is argument/default preparation only, not full live
facade acceptance. No new dependency or shared file was needed. Fresh S10/P6 and
coordinator recalls still contain no A/B/C acknowledgement; newer reminder-only
integration acceptance does not allocate SDK seams. Independent-review restriction
is unchanged and no review retry/redelegation was attempted.

Full Go45/45 tested packages pass (many cached, storage265.907s), fresh vet/build
and isolated-cache lint0issues. SDK/contract/bootstrap races pass1.848s/15.907s/
2.337s, including external Go and installed Node fixtures. Log
`sdk-normalization-suite.log` in approved temp. Existing one OpenAPI path warning
remains disclosed. None of these author checks supplies independent acceptance.

### Simultaneous native pressure and launch-boundary evidence

The existing direct-parent integration now includes two sealed children held after
touching2MiB each, measured simultaneously at VM11880KiB/RSS8704KiB combined.
A third independent-tenant child returns42 while the pressure tenant is saturated.
Releasing both barriers yields heap exhaustion `script_failed`/exit136 (maxrss17792KiB)
and CPU SIGKILL after997.748ms user CPU (maxrss4480KiB). Both Wait and pool Close join.
This is measured local fixed-slot behavior, **not** host-cgroup/parent-overhead or
durable multi-server quotas.20 pre-Start refusals and20 post-Start/pre-source
cancellations also pass; no kernel-internal prefork hook or universal race proof.
Inner Linux parent remains non-race cross-built; host wrapper runs under race.

Full opt-in native race passes659.514s; full Go45tested packages pass (many cached,
storage252.280s), fresh vet/build/lint0issues. Logs `sdk-aggregate-native.log` and
`sdk-aggregate-suite.log`; no owned probe containers remain. No C/runtime policy
or production caller changed. Actual router-match regression also proves static
feature precedence for the disclosed OpenAPI overlap. No shared REST compatibility
change is appropriate in this scope; warning remains visible, not suppressed.

Remaining runnable preparation: compatibility decoder/old-server refusal and pure
ordered-plan/journal validation. The normalization fixture is not installed in the
native facade and does not replace service mappings; those still require C–F.
All A–G allocations and independent-review restrictions remain as stated above.

### Pure plan and mutation-outcome validation

`validatePlanShape` now checks contiguous indices, existing bounded strict JSON
arguments, bounded revision text and unique backward-only provisional references.
All otherwise-shaped provisional dependencies still return the exact
`dry_run_dependency_unsupported` error: no service integration is invented.
`summarizeMutationOutcomes` rejects mode mixing, continuation after failure/unknown,
unknown statuses and excessive mutation records. This is an unverified descriptive
sequence, not a durable journal, read-operation journal, atomic receipt or replay
permission. No persistence, service calls, output or SQL interfaces were introduced.

Observed RED:14 malformed/dependent plan cases wrongly returned nil, budget overflow
returned nil and two claimed commits summarized as zero. GREEN:4top-level tests
(14plan subcases plus outcome/budget matrices), repeated20 under race1.569s; fuzz
244469executions11.394s. Full Go45tested packages pass (many cached; storage316.956s),
fresh vet/build/lint0issues. Log `sdk-plan-shapes-suite.log` in approved temp.
No requirement for real dry-run/preflight, partial commit, protected replay or
S09/P9's actual journal lifecycle is satisfied by these pure shape checks.

### Inactive compatibility decoders

Go and JavaScript now decode the **proposed**, unpublished manifest in
`docs/sdk-capability-proposal.md`.32flag combinations check the independent
compiled/configured/deployment/caller dimensions and their availability conjunction;
old404/501, auth401/403, other non200, version mismatch, missing/null/wrong-typed or
duplicate/escaped-alias fields, duplicate operations, invalid UTF8 and byte overflow
all fail with fixed non-content errors. This does not grant permission from any
flag, issue HTTP requests, advertise server support or add public SDK methods.
Route/file/wire approval A and permission/profile C are still required.

Observed RED in both languages: valid manifest decoded empty/undefined and404
returned success. GREEN: Go2top-level suites repeated20race2.072s, fuzz65214executions
12.053s; Node40/40tests/typecheck/build pass, fresh vet/build/lint0issues. Full45Go
tested packages pass (many cached; storage306.530s). Log
`sdk-capability-draft-suite.log`. No shared server/router/auth/schema/MCP files changed.

## Current continuation accounting

Immutable implementation checkpoints:

- `7ba660f5edb62a1aac90d80cf03a6e414bff3397`: positional/default preparation,
  all105public signatures (101JSON-shaped mappings +4explicit refusals).
- `fedaf9e94e81031651a49e8001f3e5c68daf5f62`: simultaneous native children,
  startup cancellation, and actual legacy router precedence tests.
- `cdcdb1142cdd82416efcbc13ea3e197c474e7a7c`: pure plan/dependency and descriptive
  mutation-outcome validation, never durable receipts/journal authority.
- `e75d43bfaac47bf0b4c6072a5ac68ee46954077a`: unpublished Go/JS compatibility
  decoder proposal and refusal corpus, no negotiation route or SDK export.

The four explicitly queued preparation units now have bounded code/test evidence.
They **do not** finish the corresponding live V1 criteria. Remaining composition
must not be simulated by claiming the pure mapper is a service facade, a parsed
manifest grants permission, a shaped plan is preflighted, or a claimed outcome is
a durable commit. Existing normal facade continues denying all but its get fixture.

Fresh final non-opt-in race run: public Go SDK1.904s, SDKcontract13.271s (real
external Go/installed Node included), bootstrap3.254s, scriptexec3.832s. Pinned
Go/TS generation has zero diff. OpenAPI validates with the same one investigated
legacy warning. Full opt-in native evidence above remains applicable: no C/runtime
source changed afterward. Frontend1269/typecheck/build and historical tenant gate
remain **prior evidence**, not rerun here; main=origin/main=merge-base was freshly
confirmed as the unchanged reviewed `7bea47d13bb99502035f51e32dd0c10e523581db`.
No historical goldens or admission gates were changed. Frontend12previously
reported dependency vulnerabilities remain unresolved, not silently cleared.

Fresh final owner reads: S09`i8aurh42`, P5.5`krmkfe26`, P5.9`5tmpau6l`,
S16`wuge6wiv`, S17`86ij8ynq`, S18`cfo4q3z1`, P8`8gxc3qi1` remain draft with no
SDK script allocation; S15`rihm769f` is completed only for private policy at
`23664fe411de41f8b3ff1200232bed1df86bbd27`. S18 has bounded same-instance CAS
work and P5.5 has an unacknowledged corrected durable-intent proposal, neither an
accepted live script seam. Earlier fresh S10/P6/coordinator reads in this continuation
likewise provide no A/B/C approval. A–G concrete decisions above remain required.
No other writer's tree was edited or imported; no runner was dispatched.

Checklist at this stop:

- [x] Each queued preparation unit implemented and verified within owned scope.
- [x] Full Go suite, affected races/fuzz, SDK build/typecheck/tests, generation,
  OpenAPI, native suite, vet/build/lint evidence recorded; exact limits disclosed.
- [x] OpenAPI warning root cause established without REST compatibility changes.
- [x] Documentation checked against plan; drafts and unavailable surfaces explicit.
- [x] A discovery route and public SDK negotiation implemented; author evidence below.
- [x] B stdio public transport convergence, bounded author evidence below.
- [ ] Separate S10 hosted adapter and independent acceptance.
- [ ] Live facade/preflight/CAS/dry-run/publication and current fences — C–F.
- [ ] Allocated persistence/idempotency/audit/quota/retention/recovery — C/D.
- [ ] Supported Linux production launcher/full integration and hosted D06 — G; native macOS deferred.
- [ ] Real authenticated REST/MCP multi-read/write and negative acceptance matrix.
- [ ] Required independent acceptance; provider flag remains parent-owned.
- [ ] Whole V1 completion. Task stays blocked/manual-reserved, `prompt_only`.

No activation, merge, push, deployment, review-bypass attempt or task completion.
Remaining C–F production composition requires the DB-AUTH-aligned owner handoffs,
not self-allocation or mock-based completion claims. A/B have explicit allocation.

## A discovery author evidence — 2026-10-06

`internal/api/sdk_capabilities.go` registers only in empty/single mode under the
existing auth, tenant scope and read-scope middleware. Minimal/unwired servers
advertise only health/discovery; fully wired fixtures advertise105 contract IDs.
No resource reads, identities, grants or script permission are returned. The five
script booleans remain independently false. No tenant seal/schema/auth changes.

Public Go/TS negotiation consumes generated OpenAPI types, uses immutable bound
HTTP and cancellation, bounds discovery to64KiB, rejects duplicate/hidden/malformed
fields and inconsistent flags, requires exact1.0.0, refuses404/501 and401/403 without
fallback and rejects non200 success statuses. All32 availability combinations are
covered in both public implementations. Real stored-token REST plus isolated Go
module and offline-installed Node consumers exercise discovery before their existing
flows. Contract/policy/matrix now contain105 operations; generated files reproduce
exactly. Existing one legacy ambiguous-task-path OpenAPI warning remains disclosed.

RED evidence: router404 instead of200/401/403; Go client made zero requests and
accepted retired binding; JS method absent; non200 success incorrectly accepted.
GREEN: Node43/43 full tests, four affected Go race packages, real external consumers,
vet/build and lint0issues. Full-suite and final milestone SHA are reported in Brain
`ihlslifb`; `sdk-discovery-{focused,race,suite,final-suite}.log` under the approved
opencode temp directory retain command output. No independent PASS is claimed.

## B stdio shared-transport author evidence — 2026-10-06

The actual Go SDK send policy now lives in public `brain.HTTPTransport`, consumed
by both typed SDK methods and `NewStdioSDKClient`. It binds origin/API prefix,
rejects structural traversal/redirects, suppresses implicit mutation replay and
calls the trusted underlying RoundTripper once. It is not a generic script API,
an authority adapter or a response decoder. Typed SDK response bounds, retirement,
errors and streaming remain in the typed client. Stdio retains legacy response
decoding/error text and local-file adapters, including their existing limits;
it does not silently acquire all typed SDK semantics.

Only the stdio composition in `internal/mcpserver/server.go` changes. Its new
constructor snapshots `BRAIN_API_TOKEN`, rejects invalid header characters and
uses the shared transport. `APIClient`, `NewAPIClient`, `WithAuthToken`, hosted
constructors and identity paths are untouched. No four-method delegation expansion
was consumed (withdrawal recorded in `ap90gj4e`). No storage authority is selected
by clients; current local-store fixtures remain compatibility evidence only.

RED established out-of-binding requests reaching transport, replayable mutations,
typed/stdio clients not consuming the public transport, and missing stdio bearer
causing child save401. GREEN real child MCP over stdin/stdout now proves stored-token
save/recall, SDK-visible content, matching missing-entry errors, local upload,
byte-exact inline/file download, local PRD discovery, read-only write refusal and
unauthenticated read refusal. Additional tests cover in-flight cancellation,
auth-disabled requests, immutable stdio token snapshots, explicit copied tokens,
hosted ignoring ambient credentials, single-send failure and redirect refusal.

The local discovery test initially failed because macOS canonical cwd used
`/private/var` while fixture HOME used `/var`; diagnostic output showed the scan
path duplicating both. Canonicalizing fixture HOME fixed it. Production discovery
is unchanged; this is not a claim that its pre-existing path-spelling behavior is
fixed for arbitrary environments.

Verification: full45/45 tested Go packages pass (many cached, storage104.559s).
Fresh affected race packages pass: SDK1.524s, MCP4.090s, MCPserver1.621s,
SDKcontract6.768s including isolated external Go/offline-installed Node consumers.
Node43/43 tests pass, typecheck/build pass, pinned Go/TS generation has zero diff.
OpenAPI retains its one existing ambiguous-task-path warning. Fresh vet/build pass;
shared-cache lint initially returned stale deleted-worktree diagnostics, then an
isolated lint cache reports0issues. Logs `sdk-stdio-{suite,race}.log` are under the
approved opencode temporary directory.

Nested B-only verification was unavailable (subagent depth limit1). No independent
PASS is claimed; the earlier worker-review restriction was not retried or bypassed.
C–F DB-AUTH-aligned permission/schema/persistence/fences/ACL/preflight/effects,
Linux production launcher/full integration and separate hosted S10/D06 gates remain.
Native macOS execution stays deferred/unsupported; macOS remote-client evidence is
not server confinement evidence. No activation, merge, push, deployment or task
completion; manual reservation and `prompt_only` remain.

## DB1 coordination delta — design-only, 2026-10-06

Read DB1 design `d365a4aac3c9b251a0c6095d3c17f0ec2371173a`, all620lines,
current canonical plans `9fguh2pr`/`qfcda7ct`, current SDK proposal and existing
DB1/S09/P6 request histories. Brain searches for script receipts, DB1+SDK and
script:execute found existing broad requests; this packet narrows them, creates
no competing task/catalog and does not edit the DB writer's worktree.

DB1 already proposes §2.3 atomic Receipt, §2.2 revision history, §3 fixed entry/
BLOB methods and §2.5 reservations/refs/quota. SDK requests reuse plus exact
execution→operation→receipt→revision/source relationships and fixed transaction
participation. `EntryCommit` is not protected output or durable provider success.
S09 remains draft; P6 repair21d2d1e7 remains unaccepted, not a supplied fence.

Finite disposition register (U rows approved 2026-10-06; T rows still proposed):

| ID | Required disposition, not implementation acceptance |
|---|---|
| U1 user — APPROVED | Script-specific24h protected envelope/digests, no raw source persistence,64KiB result/16KiB logs/256KiB total envelope,90day content-free audit. Not DB1 D1/D5 inheritance. |
| U2 user — APPROVED | Detached consumed-key MAC tombstones until namespace retirement; after payload purge/erasure old key returns409 retired, never reruns. No source linkage/digest in residue. |
| U3 user — APPROVED | Initially explicit owner/admin human credential opt-in only; ordinary auth-off REST unchanged, no-credential scripts deferred. Narrows the plan's optional trusted-local mapping (approved). |
| T1 DB1/reviewer | Content successor first; reuse methods/receipts/history, exact later extension ownership/profile allocated by DB1, no guessed version. |
| T2 DB1/S09/DB2/3 | Same fixed transaction owns current auth/ACL/CAS, content+receipt+operation outcome+allocated outbox; no pool recursion/callback. |
| T3 S09/S17/P8 | Trusted complete source capture and bounded read-frame/final-output release ordered with revoke; no broader publication without separate authority. |
| T4 DB1/DB6/P9/S09 | Classify all receipt/hash/source/replay data, reuse private BLOB refs and existing purge; irreversible admission-epoch retirement prevents restored/purged keys from becoming fresh. |
| T5 DB1/P10/launcher | Authoritative reservations; proposed initial single Linux coordinator and2/2/1active/8queued limits require technical capacity approval; credit only after death/join. |
| T6 P6/S04/DB3/P8 | Explicit script:execute successor/binding and typed side-effect-free preflight/effect contracts; immutable credential33 unchanged. |

Next primitive gate is T1, not all-SDK or DB14 completion before DB1 can proceed.
U1/U2 (approved) fix retention/replay policy, U3 (approved) first submit eligibility; T2–T6
need their specified accepted owner contracts and actual integration tests.
Linux-single still requires authoritative content runtime, live auth/fences,
preflight/CAS/receipts, source restrictions, lifecycle and full real REST/stdio
execution evidence. Hosted S10/D06 and public activation remain separate; native
macOS server execution deferred, remote clients retained. No DDL/auth grant/broker
activation, merge/push/deployment/status/dependency change or worker-review retry.

Verification for this packet is documentation-only: diff/whitespace, pinned DB1
method/section references, balanced structure and finite U/T disposition coverage.
No45-package rerun or runtime PASS is claimed. A/B author evidence above remains
at its own commits; C–F is not delivered by writing this proposal.

## G Linux production launcher — author evidence, 2026-10-07

Sole writer `ses_eee03b0f7ffeQnbOPdx1BA4D9T`, from `ef83b9fc` (plus the inherited
uncommitted U1–U3 doc edits, completed in `31c9d998` with `policy.go`). Details and
topology are in `internal/scriptexec/README.md` ("Linux-first production launcher").

- `launcher.go`, `launcher_linux.go`, `launcher_other.go`, and the
  `runWorkerProcessWithStart` hook. Disabled by default; non-Linux/macOS returns
  `unsupported`. Pinned O_NOFOLLOW digest-verified exec via `/proc/self/fd`;
  empty env, cwd `/`, fds 0–2, PDEATHSIG. Parent `/proc` attestation (own seccomp
  filter above the inherited count, NoNewPrivs, CPU ≤1s, AS ≤64MiB, fds {0,1,2},
  pinned exe inode) BEFORE any source; kill+Wait on every failure path.
- Studied the archived Node failure (`sdk-seccomp-node-rejected.tar.gz`): Node
  needs ambient post-seal syscalls (stdio reset, `newfstatat`, `fcntl`), so a
  deny-default seal there means widening the allowlist; that was correctly
  refused. The QuickJS core-only worker initializes before sealing and needs only
  fd0 read / fd1–2 write / anonymous non-exec mmap afterwards, so no widening was
  needed. The earlier container-only gap (child `/bin/echo` allowed despite
  no-new-privileges/cap-drop/pids-limit) is closed by the worker's own seal and
  is now verified from the parent, not assumed.
- Real Linux (Colima kernel 6.8 arm64, uid 65534), historical as of `91c7b3e9`:
  `TestNativeLauncher` 10/10 PASS, launch pin from an independent relocated
  rebuild (`401a6624…abca`, pre-`seal.h`). Current from `3ca51d12`: 11
  subtests, pin `f81221bb…1972` from `release.json`.
  Memory: heap flood ends within bounds; fresh `TestQuickJSNativeAddressSpaceProbe`
  shows kernel ENOMEM at the attested 64MiB AS. Refactor regression:
  `TestQuickJSManagedParentIntegration` + `TestQuickJSManagedParentDeath` PASS. `TestQuickJSNativeChildExecDenied` PASS: the unsealed control really
  execs; sealed `execve`/`execveat`/`clone`/`clone3`/pdeathsig/RLIMIT_AS changes
  all return EPERM. Attestation-ignored mutation → the unsealed-binary case FAILS
  (source echoed back by `/bin/cat`). Host `-race` package suite plus pure
  attestation/file-mode/config tests pass on darwin.

G remaining: independent review of seal/launcher/runtime selection
(provider-flagged review not retried); release packaging/installation of the
worker (source still in `testdata/`); x86_64 and live systemd/init deployment
observations; composition with C–F. Hosted D06 is separate. No route, config,
capability or activation changed.

## G closure — packaging, concurrency, reaping, review gaps (2026-10-07)

Independent review of `91c7b3e9` returned PASS (Brain report `wdyetyqh`). Its
test-gap and minor findings are addressed below. SCRIPT-DECISIONS-20261006b
approved `idempotency_key_conflict` and `idempotency_key_in_progress` (both 409)
and deadline-anchored late-finish retention.

- **Release packaging:** the worker moved to `runtime/script-worker/` with the
  shared `seal.h` and `release.json`. Pinned `linux/arm64` =
  `f81221bb56c904862207676316342a7a307457be538987954b2589aeb7fd1972`; the
  committed digest is enforced by an independent rebuild in the launcher
  wrapper. Install path: `/usr/libexec/brain/brain-script-worker`, root 0555,
  documented only (no install or activation).
- **Concurrency:** `TestNativeLauncherPool` PASS on real Linux. Two simultaneous
  sealed workers; a third is queued; cancellation → kill+Wait → slot recovered
  (42). Same-principal reuse works, `close` joins, no children left.
- **Reaping:** `TestQuickJSInitReaping` PASS: no init → `Z`; `--init` →
  `gone`. A systemd 255 transient unit (Colima VM) → `gone`, `result=signal`,
  no leftovers.
- **x86_64: BLOCKED.** The build is reproducible (`9564f7a7…d552`), but
  execution on a real x86_64 kernel didn't happen: Lima's usernet timeout
  (2 min) races a slow TCG boot (cloud-init at ~234s), and the direct-QEMU
  provisioned boot reached login without an SSH session in 30 min. Needs a
  native x86_64 host or KVM runner.
- **Review gaps closed,** each confirmed by a mutation that removes the
  protection (M1–M6 all FAIL):
  - expired in-progress keys are retired and never re-run;
  - a real length-prefix collision pair plus a golden MAC pin the domain label
    and length prefixes;
  - invalid-JSON log and plan cases are refused;
  - a default (non-opt-in) 32KiB source-limit test runs on every OS.
- **Launcher fixes:** `sourceWritten` is set only after a successful write; the
  `run` terminal-frame contract is documented.
- **Build timeouts:** trusted-build harness timeouts are 600s, because a
  shared, loaded VM took up to ~5 min per compile, and prebuilt pinned
  artifacts are reused by default.
- **Remaining G:** x86_64 execution evidence, independent re-review of these
  commits, and C–F composition. Hosted D06 is separate. No activation, route,
  config or deployment.

## G re-review follow-ups (2026-10-07)

Independent re-review of `91c7b3e9..b30ce8cd`: PASS (Brain `85280tcu`; an
independent rebuild reproduced `f81221bb…1972`). Its three test-strength
findings are fixed, each confirmed by a mutation on real Linux:

- **PDEATHSIG (intended design: set by the launcher's Go parent):** the
  stand-in hands the worker's stdin to a holder that outlives the server, so the
  worker cannot exit on EOF. Without init the worker must be a zombie with
  termination signal 9; with `--init` it must be gone. Mutation removing
  `Pdeathsig` → worker stays `S` → FAIL.
- **sourceWritten:** `testdata/stdin_closer.c` (shipped `seal.h`, passes
  attestation, no stdin reader) makes the source write fail with EPIPE, and the
  report must say not written. The old wiring as a mutation → `sourceWritten:
  true` → FAIL. A first mutation attempt was a compile error and was discarded
  and redone, not counted.
- **systemd:** now a committed opt-in test (`TestLinuxSystemdReaping`). PASS on
  the local Colima VM (systemd 255, PID 1): worker gone, `result=signal`, no
  leftover units or PIDs.

x86_64 is unchanged: waiting on a user decision, and the `brain-x86` profile
was left untouched.
