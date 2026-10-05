# SDK/script V1: Phase 0 integration ledger

Task `vggevclc`, plan `qfcda7ct`, authoritative revision
`SDK-TENANCY-20261005`, canonical tenancy plan `9fguh2pr`.
This is an implementation handoff, **not V1 completion or security evidence**.
Phase 0 inventory/proposals are underway; shared allocations are outstanding.

## Source pins and execution ownership

- Feature branch: `sdk-script-execution-v1`, created from clean main
  `7bea47d13bb99502035f51e32dd0c10e523581db`.
- Workspace: `.worktrees/sdk-script-execution-v1`. Main is not edited.
- Manual owner: OpenCode `ses_ef3ce56d4ffe5yZrAY1sYfRE8O`. Task is held
  `blocked` with an explicit manual reservation to exclude automatic runner
  dispatch. This is not a runner claim or a completed implementation.
- P5 inspected pin: `421719ef36fa1771506b7489a03201e549b4d9f5`.
- P6 inspected pin: `c751d0e604e0b58fa95156f3caa9481d009051bc`
  (includes `6077f9e7` credential33 foundation).
- Org/project policy inspected pin: `8d313875a134e172935e80ff93bf1a2082fc8271`.
- Bounded integration inspected pin: `3de1edc33ea27861aa5429453552ef4669734c07`;
  its documented P6 input is
  `6077f9e7`, **not** the newer private-operation checkpoint `c751d0e6`.
- Foreign worktrees were not used as source. Inspection uses immutable Git
  objects. None of these pins was merged or cherry-picked into this branch.

`prompt_only` remains binding: no merge, push, deployment, installation,
production migration, credential rotation or public activation.

## Availability is not inferred from foundations

| Profile | Current boundary | Required evidence before script availability |
|---|---|---|
| Public SDK over existing single REST | Independent client/contract work may proceed | Real authenticated handler parity, immutable binding/cancellation, DTO and error contracts |
| Opt-in single scripts | Not implemented; no enabling configuration added | Explicit local/authenticated authority, per-operation checks, allocated persistence, platform confinement and real worker/API/MCP tests |
| Private tenant integration | Not composed | Accepted live identity, resource ACL, commit/output fences, exact reviewed schema successor, colliding-org/revocation fixtures |
| Public hosted tenant scripts | Unavailable; outside V1 activation authority | Separately reviewed D06 VM isolation plus P5/P6/ACL/P8/P10 and existing release gates |

Public startup remains runtime30/single. Exact runtime30/private29/tenant31/
identity32/credential33 catalogs and the sealed tenant route allowlist are not
changed by client work. Unknown profiles do not fall back to local execution.
Single-mode script submitters are not presumed trusted merely because the server
has one tenant. Missing OS confinement means unavailable, not degraded sandboxing.

## Coordination requests (not allocations)

| Owner | Evidence at inspected pin | Required handoff |
|---|---|---|
| P6 capability/schema owner | `docs/p6-s03-credential-proposal.md` fixes credential33 to eleven capability literals; `script:execute` is absent | Reviewed successor/composition and role ceiling for explicit script submission; never change historical credential33 in place or inherit OAuth `mcp` wildcard |
| Shared schema coordinator | Exact runtime/profile admission is required; no execution tables are allocated | Exact target profile/version, migration provenance, execution/operation/idempotency/quota relations, scoped receiver ownership, retention/recovery, and affected manifest/test deltas |
| P6 S09 (`i8aurh42`) | `internal/auth/contracts.go` explicitly limits `AuthorityFence` to admission; mutation/output/effect signatures remain deferred | Accepted same-writer mutation and cross-process output/revocation protocol plus durable journal composition; no duplicate fence implementation |
| P6 S10 (`ap90gj4e`) | Trusted hosted MCP adapter is a future specification in `docs/p6-identity-implementation.md` | Accepted operation-specific in-process adapter retaining sealed authority; no loopback headers or public trusted-principal constructors |
| Org-policy/P6/P9 owners | `8d313875:internal/orgpolicy` is pure snapshot policy, explicitly unused by runtime; its document predates the later individual-entry SHARE-COLLAB requirement | Current authenticated grant acquisition/persistence, entry grants, source provenance/publication decisions and commit/output integration |
| P5/P8 lifecycle/effects owners | Physical/source-generation and delegated-effect boundaries remain separate contracts | Graph leases, worker join-before-store-close, side-effect-free validators, provider reservation/delegation and honest unknown-outcome handling |

The SDK owner requests ownership of new `api/`, `sdk/`,
`internal/scriptexec/`, and `cmd/brain-script-worker/` surfaces, subject to exact
reviewed contract and tests. Existing auth/storage/router/apiserver/MCP files
remain coordinated overlaps, **not allocated by this document**. Request exact
file/method ownership before edits there. No new schema number is guessed.

Nested independent verification is unavailable in this session: delegation was
rejected by the tool's subagent depth limit (1). The parent must arrange a verify
agent or enable deeper delegation; local tests cannot be relabeled independent.

Parent-arranged reviewer `ses_ef3af4924ffeKkZcdp3tx3JBwh` subsequently gave PASS
for the accuracy of `2396b7a2`/`vdkt6qvp` documentation and blocker evidence only.
It did not run a new baseline or verify an implementation. Substantive candidates
still require parent-arranged independent review. See
[the concrete allocation request](sdk-script-allocation-proposal.md) for exact
owner/task IDs, proposed tables/receivers/migration/retention and file ownership.
No approval is inferred from submitting that proposal.

## Initial operation/effect inventory

Source is `7bea47d1:internal/api/router.go`. This is a family-level discovery
inventory, **not the completed per-operation support matrix or script allowlist**.
Every script method remains unimplemented/unexposed. Each eventual operation ID
must name its DTO, profile, current resource/action authorization, CAS contract,
preflight validator, effects/providers, and positive/negative test evidence.

| Family / existing HTTP surface | Effects and preflight obligations |
|---|---|
| `GET /api/v1/health` | Public health, not authenticated capability discovery; readiness must not imply scripts are enabled |
| Entries `/entries`, wildcard ID/path, metadata/move/verify, bulk update/delete | SQL + Markdown + index/sync + events; generic task/automation writes require work/configuration permission in addition to content edit; current revision and source/destination checks |
| `POST /search`, `/inject`; entry sections | Read-shaped semantic/hybrid operations can call embedding providers; dry-run cannot invoke providers or mutate content counters |
| Entry backlinks/outlinks/related; `/orphans`, `/stale`, `/stats` | Filter every contributing source before counts/snippets/edges/output; restricted-resource visibility is not merely a tenant predicate |
| `/tasks/{projectId}` and feature/dependency/assignment/resume actions | Claims, dispatch, queue, sessions and events; safe preflight cannot invoke live run/resume/checkout then roll back |
| `/goals`, `/automations/run`, `/automation-runs` | Runnable work/provider effects and durable audits; automation CRUD also uses entry routes |
| `/reminders` CRUD/ack/snooze/fire | Scheduler/task/event effects; `action=task` is work submission, not just content |
| `/attention` CRUD/state/counts | Recipient-qualified sidecar persistence and revision transitions; no assumption this is a tenant-shared store |
| `/attachments`, entry attach/detach/content/text/extract | Blob/derived files, SQL references, async extraction/providers; scripts accept bytes, never caller-local file paths; authorized references and final output fences |
| Projects (`GET /tasks`, placement, project deletion) | Placement/work and destructive domain operations are distinct; existence/list/count visibility requires resource policy |
| `/webhooks` and test/delivery routes | Outbound delivery/provider effects; do not infer safe dry-run or scope from HTTP method or group placement |
| `/events`, timeline and observability reads | Session/cursor/content visibility, request telemetry and potentially long-lived output; event ingestion is a write |
| `/control`, `/runners`, `/tokens`, config/deployment administration | Excluded from the V1 script facade; no arbitrary HTTP fallback |

Legacy route scopes are compatibility facts, not the future resource evaluator.
In particular some routes rely on handler checks rather than an explicit
`RequireScope` at their router registration. Record and test actual composed
behavior before advertising SDK guarantees; do not silently rewrite legacy auth.

## Dry-run and protected-output contract

The complete per-operation matrix must enumerate minimal execution/operation
audit, idempotency receipts, security/quota accounting and bounded isolated scratch
as the **only** control exceptions. Enumerate inherited auth/request telemetry
exactly. Snapshot domain SQL, Markdown, CAS/derived content, embeddings, events,
queues and provider spies. A transaction rollback is not a safe preflight.

Reads observe committed state. Planned mutation responses and provisional IDs
must be distinguishable; unsupported provisional/provider dependencies return
`dry_run_dependency_unsupported`. No promise of a simulated snapshot or later
success under concurrency.

Track conservative execution-wide source resources. Current access to all of them
fences plans, logs, results, journals and replays. Writes cannot publish into a
broader audience without a separately authorized publication contract. Default
durable audit contains hashes/IDs/bounded non-content outcomes, not source,
arguments, logs or result bodies. Principal ownership alone is insufficient after
source revocation. Earlier real commits remain committed; uncertain outcomes are
not silently retried.

## Completion ledger

No OpenAPI contract, SDK, broker, worker, real script flow, migration, platform
confinement, restricted-resource integration or hosted release proof is delivered
by this document. Do not mark `vggevclc` complete from this checkpoint.
Independent SDK phases may proceed without waiting for public hosted activation;
the shared integration handoffs above are required before composing execution.

## Executed baseline verification — 2026-10-05

These results verify the **unchanged code baseline**, not SDK/script behavior:

- `CI=1 just vet`: exit 0.
- `CI=1 just build`: exit 0.
- Initial `CI=1 go test ./...`: failed at the default ten-minute storage-package
  timeout (600.552s), during fixture construction inside
  `TestTenantRuntimeCoverageManifest/notes/TestTenantNotesCorruptChildOwnership`.
  Forty other packages passed; this run is not reported as success.
- `CI=1 GOMAXPROCS=2 GOFLAGS='-timeout=20m' just test`: exit 0;
  **41/41 packages passed**, **10,860 passing test/subtest records**, 11 skips,
  zero failure records. Storage took 528.589s. Manifest wrappers rerun tests;
  records are not unique independent behaviors. Skips were three built-PWA
  checks (assets not built), two containment cases, four child/fixture entry
  points and two historical-base checks (base environment unset).
- Initial `CI=1 just lint` had stale diagnostics referencing the removed
  `continuous-timeline-zoom` worktree. The current source has the relevant lint
  suppression, but the cache's missing source prevented applying it. Repeating
  with a fresh task-local `GOLANGCI_LINT_CACHE` gave **0 issues**, exit 0,
  without editing or suppressing any source.
- `git diff --check`: exit 0. No runtime/schema/auth/SDK source was changed.
- Independent `verify` invocation was attempted and rejected by the depth limit.
  This checkpoint has **no independent verification verdict**.

Local evidence: approved temporary directory `.../T/opencode/`, files
`sdk-baseline-tests.log` and `sdk-just-test.log`. The latter contains the full
uncached recipe output. Platform confinement, targeted script races, real
REST/MCP worker flows, OpenAPI and Go/TS SDK checks do not yet exist and were
**not run**. No frontend change was made; no frontend readiness claim is made.
