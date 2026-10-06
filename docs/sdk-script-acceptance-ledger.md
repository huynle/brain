# V1 acceptance ledger — SDK-TENANCY-20261005

Starting checkpoint: `2228f1f1ca97ff8108573016d869b89ac239c207`.
This ledger supersedes stale pending counts in earlier handoffs, not the plan.
Manual exclusive ownership, blocked dispatch reservation and `prompt_only` remain.
No enablement, merge, push, deployment or task completion is authorized.

## Classification

**R** = runnable in owned files now; **E** = exact external interface/allocation;
**P** = platform proof/decision; **V** = existing bounded evidence, not full acceptance.
A row can have both an independently implementable part and a blocked composition.

| V1 criterion | Class and current evidence | Remaining work / completion oracle |
|---|---|---|
| OpenAPI inventory and both SDK namespaces | V/R: 104 operations, router inventory, external Go module and installed Node fixtures | Audit every operation's effect/resource/precondition/provider metadata; remove stale matrix wording; guard completeness. Task/automation creation uses entries, not new routes. |
| SDK transport, immutable identity, pagination, errors | V/R: cancellation/rebind, no ambiguous write replay, bounded responses/SSE, typed errors and error-only conflicts, deterministic generated DTOs | Audit API compatibility/version failure, examples and release docs; no retry is safer than inventing safe retry. Existing legacy route ambiguity must remain disclosed. |
| Capability/version discovery | E/R: no endpoint; clients cannot negotiate yet | Client decoder/old-server refusal tests runnable. Router allocation requires coordinator A below. Must distinguish compiled/configured/profile/caller, reveal no resources and advertise scripts unavailable. |
| Stdio MCP SDK convergence and backward compatibility | E: no approved file ownership; S10 still draft | Coordinator B: new stdio constructor plus minimal composition change, retain local-file support and real child stdio parity. Hosted constructor remains untouched. |
| Hosted MCP sealed authorized adapter | E: S10 `ap90gj4e` draft | Accepted immutable operation-specific adapter and verified caller/session binding, never loopback reconstruction. Real per-call/session/cross-principal tests. |
| Worker async/final-expression/return semantics | V/R: `2228f1f1` compile-only grammar fallback and promise/thenable settling, no runtime reevaluation | Full JS facade/unsupported-method behavior and JSON-only boundaries; native corpus and examples. No actual service dispatch implied. |
| Bounded console, errors and protected results | R/E: diagnostics currently discarded; result byte bound exists | Bounded quarantine and actual console fixture; source union from trusted parent only, no ambient logger or default formatting leak. Real release must use S09/S17 source-wide fence, not a new authorization callback masquerading as it. |
| Pinned reproducible runtime/toolchain | V/R/P: experimental official source digest, immutable image, hardened ELF and relocated byte equality | Preserve generation/build recipe, expand native denial corpus; reviewed production runtime/launcher selection still required. No production command selected. |
| Per-worker compile/wall/CPU/heap/native memory/IPC limits | V/R/P: Linux experimental limits, native denials, protocol budgets and hostile corpus | Further source/serialization/native abuse and prefork races runnable. Linux local container is not hosted VM acceptance. |
| Cancellation/crash/death/reaping and shutdown | V/R/E: inactive Start/Wait, Linux creating-thread PDEATHSIG, native subreaper tests | Bounded admission-to-start and shutdown ownership tests; real graph lease integration only through approved composition. Dead-parent orphan reaping still needs reviewed external init. |
| Aggregate admission, fairness, memory/CPU and lifetime | R/E: not implemented | Inactive bounded local scheduler with principal/tenant/global reservations and fair waiting, cancellation/shutdown join, no credit before Wait. This is not authoritative multi-server quota; durable reservation/topology allocation remains E. |
| macOS confinement | P: RLIMIT_AS resident-memory counterexample; memorystatus EPERM | Approved enforceable unprivileged profile or reviewed VM/worker-service decision. No root helper/entitlement/degraded fallback. Keep disabled. |
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
