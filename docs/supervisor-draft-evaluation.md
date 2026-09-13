# Supervisor draft evaluation and local implementation

Scope: the 17 September 9 draft tasks in 11 supervisor features. Multi-tenant drafts are excluded. Work is performed locally without runner dispatch. The six DP/OR/CU/HC/EB/SS drafts request evaluation; this change also implements their bounded foundations. Their larger proposals and remaining limits are recorded below, rather than treating a prototype as the entire proposed architecture.

## Inventory and evidence

| Task | Implemented or evaluated behavior | Verification |
|---|---|---|
| jtkerfg0 SI1 | Bounded visible session tails, opaque scoped cursors, streaming upserts, explicit truncation and expiry | supervision session tests; control-scope denial tests |
| 1n6gxznu SI2 | Bounded persisted descendant traversal, depth/cycle protection, historical linkage | supervision children tests; REST/MCP fixtures |
| j85yf4hg SI3 | Stdio and HTTP session reader integration | TestSupervisorMCPTransportsToREST; controlled executor bridge |
| rddz5vui RH1 | Existing memory-guard sample becomes process-tree RSS/count/command-name/activity telemetry | runner resource-health and existing memory-guard tests |
| ua1vxiwr RH2 | Timestamped health snapshots and 80% warning/70% clear events | synthetic threshold and unavailable-sample tests |
| cbn60n22 EW1 | Subscribe-before-replay event waits; bridge control hints join existing event ring | event-wait registration race, reconnect, expiry, cancellation tests; control projection test |
| koiltisx EW2 | Bounded MCP event wait, project/task/feature filters | MCP transport integration and REST ordering tests |
| rxf9u15z VC1 | Separate revisioned delivery evidence, exact GitHub head and actual merge artifact | provider fixture, CAS and reindex preservation tests |
| id266ifs VC2 | Opt-in task/feature dependency gates, dedicated evidence write/verify tools | mixed-policy dependency, failed check, changed head and integration tests |
| ob93fqup MR1 | Shared MCP registration and capability/version report | discovery over both wire transports |
| w6b62puv MR2 | Supervisor workflow composed from tested readers, waits, receipts, health and delivery gates | local fixture suite; operator workflow below |
| qsjhrr07 DP1 | Evaluated dispatch preview; reuse scheduler readiness and placement predicates | existing scheduler tests; remaining provenance limits below |
| azciwwby OR1 | Evaluated and implemented durable idempotent operation admission | concurrent ownership, lost acknowledgment, database reopen, payload conflict and budget denial tests |
| glpatqxy CU1 | Evaluated transaction boundary; added revision preconditions and serialized append | concurrent writers, stale body/status/metadata, invalid references and cycle rejection |
| u9ejc2s7 HC1 | Evaluated and implemented versioned artifact-specific checkpoints and explicit resume handoffs | immutable history, duplicate request, superseded artifact and stale-answer tests |
| vnjbdym1 EB1 | Evaluated and implemented daily unit reservations shared by parents/children and operation admission | five-unit limit with twenty competing children, retries, settlement and timezone boundaries |
| kwydxafp SS1 | Evaluated and implemented compact task/checkpoint snapshot with event cursor and timestamps | shared read endpoints and bounded projection tests |

## Evaluation gap matrix

Effort estimates are engineering estimates, not delivery promises.

| Proposal | Existing mechanism reused | Smallest implementation in this change | Remaining gap / risk | Next scope and effort |
|---|---|---|---|---|
| Dispatch preview | GetReady, pause dials, placement predicates, runner registry | Read-only readiness/placement result; configured fields and explicit unknowns | API cannot observe runner-local defaults or filesystem access. Preview never reserves capacity. | Runner capability/config provenance exchange; 2–4 days; depends on runner versioning |
| Operation receipts | Existing bridge prompt, contextual resume and task trigger | Durable principal-scoped ID/digest before effects; no replay after uncertainty | Executor acknowledgment cannot prove completion. Receipt IDs retained indefinitely. | Retention/archive policy and executor-native command IDs; 2–3 days |
| Conflict-safe updates | Existing entry Update and metadata writes | Optional expected_revision; process-local writer serialization; graph prevalidation for conditional entry updates | Markdown and SQLite are separate commit domains. **Atomic multi-entry bulk_update is not implemented.** Out-of-band disk/index writers are outside the API mutex. | Durable file journal plus SQLite transaction/recovery, graph-wide preview/apply preconditions and fault injection; 5–8 days. Do not advertise sequential rollback as atomic. |
| Handoffs/checkpoints | Verified evidence and contextual resume | Versioned exact-artifact answers, separate verification, explicit checkpoint revision in operation handoff | Authenticated submitter identity is provenance, not proof a human actually performed the check. No implied extra permissions. | Optional task/feature gate policy and richer decision/remaining-check schema; 2–3 days |
| Budgets | Existing memory/capacity policies, durable operation admission | Atomic daily application-unit reservations and charge-before-delivery | **Only work using the reservation protocol is hard limited.** Opaque executor tokens, spend and unreported work remain unknown. No inferred inactivity kill. | Executor usage receipts and actual-usage reconciliation, elapsed/concurrency admission; 4–7 days; depends on trustworthy executor metrics |
| Structured snapshot | Existing resolved tasks, checkpoints and event ring | Compact bounded task page with start/end timestamps and pre-read event cursor | Snapshot is non-atomic; resource health has its own timestamp. Pagination can change as tasks change. | Projection selection and scoped opaque snapshot pagination; 1–2 days |

## Operator workflow

1. Call `supervisor_capabilities`, then MCP `tools/list`. Compare server commit/version with the intended deployment. Source presence is not evidence that a client is connected.
2. Read `supervisor_snapshot` for a project. Preserve its event cursor; concurrent changes after the snapshot starts remain replayable through `events_wait`.
3. Use `session_children` to obtain persisted session IDs and `session_tail` for each. Upsert records by record ID. A growing final part is returned again when its content changes. On cursor expiry, replace/reconcile from a fresh snapshot.
4. Call `events_wait` with the same project filters and cursor. Maximum wait is 25 seconds; timeout is normal. Permission/activity events are hints from existing bridge control frames. The bounded bridge queue and event ring are lossy; state reads remain authoritative, especially after disconnect, overload or expiry.
5. Read `resource_health`. Missing/null measurements mean unavailable. Warning begins at 80% of the existing task memory limit and clears below 70%; no limit, capacity or kill policy changes. Quiet output alone does not prove a stall.
6. To send guidance once, submit `supervisor_operation` with a unique ID and explicit target. Reuse that ID after a timeout and inspect `supervisor_operation_get`. `accepted` means recorded for dispatch; `delivered` means executor acknowledgment; neither means completion. `outcome_unknown` requires reconciliation before using a new ID.
7. Optionally attach `checkpoint_id` and its exact `checkpoint_revision` to a contextual resume. An artifact change clears current answers/verification while retaining history. Reading an unrelated synthetic artifact cannot verify the requested finished artifact.
8. Configure an `execution_budget`, reserve/commit units through that tool or supply `budget_id` and `budget_units` on an operation. Commit occurs before delivery, so crashes/unknown outcomes remain charged. A committed charge cannot be cancelled. Child reservations share the same account; parent IDs are linkage, not duplicated aggregate charges. IDs remain idempotent across calendar rollover. Timezone and unit cannot be changed in place to reset consumption.
9. Configure `delivery_record` with an explicit policy, repository/PR/head/target and required checks. `delivery_verify` performs read-only GitHub verification. Record integration against the actual verified merge commit. Open PRs, stale checks, missing provider evidence and failed integration do not release opted-in prerequisites.

Implementation status stays separate from delivery: existing `feature.completed` automations can perform delivery, avoiding a circular dependency where delivery waits on its own evidence. Downstream dependency readiness uses the delivery gate. Legacy tasks without an opt-in policy retain existing behavior. GitHub merge commit evidence handles squash/manual merges without pretending the feature head is an ancestor of a squash commit. GitLab verification is not implemented; unsupported providers fail closed. Private GitHub access requires server-side `GITHUB_TOKEN` configuration; this change does not install credentials.

## Contracts and REST fallback

All paths below are under `/api/v1` and retain existing authorization middleware.

| MCP tool | REST |
|---|---|
| session_tail | GET /control/runners/{runner}/sessions/{session}/tail |
| session_children | GET /control/runners/{runner}/sessions/{session}/descendants |
| events_wait | GET /events/wait?project_id=…&after=… |
| resource_health | GET /events/resource-health?project_id=… |
| delivery_gate / delivery_verify / delivery_record | GET / POST /tasks/{project}/{task}/delivery |
| supervisor_capabilities / supervisor_snapshot | GET /supervision/capabilities or /supervision/snapshot?project_id=… |
| task_dispatch_preview | GET /supervision/dispatch-preview?project_id=…&task_id=… |
| supervisor_operation / supervisor_operation_get | POST /supervision/operations; GET /supervision/operations/{id} |
| supervisor_checkpoint | GET / POST /supervision/checkpoints |
| execution_budget | GET / POST /supervision/budgets |

Checkpoint and budget tools accept an optional `command` object mirroring the REST write body; without it they read by project/id. Mutation routes require admin scope. Session readers require control scope. Read-only grants cannot inject prompts or attest evidence. Receipt reads are restricted to the submitting principal. Credentials and raw operation payloads are not persisted in receipts.

## Client and executor limits

The canonical stdio server and Streamable HTTP server share registration. The legacy bundled Pi `brain-tools.ts` extension is a separate five-tool REST adapter, not a generated MCP inventory; it does not automatically acquire these tools. Use a supported MCP connection or the documented REST fallback. Do not mistake its older tool list for a server discovery failure. This change does not install plugins or rewrite user client configuration.

For a stale client inventory, check the API URL and advertised commit, inspect grants, reconnect/refresh MCP discovery, and restart an old stdio child with the new `brain-mcp` binary. A missing installation/connection is distinguishable from a 403 grant denial, a disconnected runner, and unsupported executor history. The capability report explicitly leaves client installation unknown because the server cannot observe it.

Local integration uses real MCP framing and REST handlers with a controlled executor bridge. It is not a real OpenCode/Pi LLM run. Persisted OpenCode child linkage does not prove current child state/activity; those fields are explicitly unknown. Pi child-session discovery is unsupported. History projection is bounded, but upstream history retrieval still loads the existing full history response. Session output redacts known credential patterns and omits reasoning/tool inputs; it is not a general secret-classification engine.

New resource samples require an updated runner with its existing memory guard enabled. Deploying only the API does not update remote runner binaries. Unavailable metrics remain explicit until those runners are upgraded.

## Persistence and release

Schema 30 adds scoped operation receipts, immutable checkpoint versions and budget reservation tables. Delivery verification is preserved independently of file reindexing. Back up the live SQLite database consistently before upgrade. Keep the prior container image for rollback; older code must not be used as a mechanism to edit new ledgers.

Validation logs are produced during local work; final release validation must use a clean source tree because nested `.claude/worktrees` in this checkout are incorrectly included by two existing source-policy tests. Those unrelated policy tests and multi-tenant code are not changed by this feature work.

## Verified local release checks (2026-09-09)

A clean staging copy passed `just check` (Go vet/tests, golangci-lint with zero issues, frontend typecheck, and all 1,159 web tests) and `just build`. Targeted race tests passed for operation receipts, checkpoints, reservations, event waits, conditional edits and delivery gating. The fixture suite includes actual stdio/HTTP MCP framing through REST handlers; external assistant installation and real LLM executor compatibility remain explicitly unclaimed.
