# SDK operation inventory (work in progress)

Source: `internal/api/router.go` at base `7bea47d1`. This is an explicit
per-operation inventory, not a claim of complete SDK delivery or script availability.
Paths below are relative to `/api/v1`. IDs are proposed contract IDs until their
contract is checked in and validated. Unless listed below, rows have SDK implementation
**pending** and SDK parity evidence **pending**. All have profile **single**,
script exposure **false**, and dry-run validator **unimplemented**. No table row grants
authority. Tenant routes remain sealed by the existing server allowlist.

Current Go/TypeScript coverage: `health.get`, `entries.list`, `entries.create`,
`entries.get`, `entries.update`, `entries.delete`, `search.query`, `tasks.list`,
`tasks.get`, `entries.move`, `entries.bulkUpdate`, `entries.bulkDelete`,
`sections.list`, `sections.get`, `graph.backlinks`, `graph.outlinks`,
`graph.related`, plus `attachments.list/get/delete/upload/download/text/extract/forEntry/attach/detach`.
Goals `list/create/update/delete/progress/audit/run` are also implemented (34 total).
Reminders `list/get/create/update/delete/ack/snooze/fire` and attention
`list/counts/get/create/read/unread/snooze/resolve/dismiss` bring coverage to 51.
Both languages have route tests for these 17 methods. The authenticated real-service
Go fixture additionally exercises all eight reminder methods and all nine
attention methods, including token-name recipient binding. These notification
methods now also run through the installed external Node consumer, with unread
count and state assertions. Reminder fire is notify-only with durable fire-count
and no-generated-task assertions; this does not prove external notice delivery.
Webhooks `list/get/create/update/delete/deliveries/test` bring delivered coverage
to 58. All seven have Go/TS route tests and real Go service integration, including
one actual HTTP delivery to an isolated local test receiver (not a public target).
The checked-in installed Node fixture also verifies disabled/enabled filtering,
update/read, one actual local delivery and history, then delete/not-found.
Automations `run/runs/getRun` bring delivered coverage to 61; both SDK route
tests cover all three, and real Go fixtures check filtered run history/get.
Automation run submission now also generates a real task in the Go service fixture,
with its generated-by provenance checked; no executor/runtime/provider is launched.
The installed Node fixture now also submits a task, checks provenance, lists its
actual run audit and fetches each audit entry. These assertions are checked in,
not only held in an independent verifier overlay.
`TestDeliveredContractMatchesRouterAndInventory` checks every delivered operation
against the real Chi route inventory and its table row below (69 currently).
Projects `list`, graph `orphans`, and observability `stats/stale` have route tests
in both SDKs plus external Go/Node real-service checks. Stats preserves legacy
host paths and is not script-safe. Stale means never verified or verified before
the cutoff, not simply old creation time; newly created unverified tasks qualify.
Task `waiting/blocked` have both SDK route tests and installed Node real-service
checks: a pending dependency produces waiting, then cancelling that dependency
produces hard-blocked and removes the task from waiting. Empty selections can be
null, matching the existing wire behavior. Ready/next now have both SDK route
tests and real external Go/Node selection fixtures, including repeated feature
filters, executor filtering, and no-ready results. `next` returns HTTP200 null
when no task qualifies; neither method claims a task. Runner/provenance filter
encoding is route-tested, not real runner-admission evidence.
It normalizes parameter names/trailing slashes and explicitly maps legacy entry
dispatchers to their wildcard routes. It does not certify dispatcher suffix
semantics, authorization, or completion of pending inventory operations.
The Go external consumer exercises six goal operations. Node additionally runs a
goal after its linked task completes, asserting the completion decision and audit.
This does not prove goal-driven work generation or live session steering.
Contract: `api/openapi.yaml`; transport tests: `sdk/brain/client_test.go`
and `sdk/typescript/test/client.test.mjs`; real stored-token/SQLite/service/router
and external-module/package evidence:
`TestExternalClientsAgainstAuthenticatedRealHandler`. That real fixture exercises
the first 17 operations plus all ten attachment operations (real local blob storage;
extraction uses the actual HTTP extractor against a deterministic local provider,
not a real remote model). Both external consumers assert persisted derived text
and repeat text reads with exactly one provider call each. Explicit extraction
is effectful on every call; it is not a cached read. Legacy bulk calls use dry-run mode and search uses FTS,
plus unauthenticated refusal. It does not prove hosted ACL composition,
search-provider behavior, real bulk partial outcomes, or script dry-run guarantees.

Legacy scope abbreviations: R = admin/runner/read; A = admin; W = admin/runner;
Auth = router authentication only (handler checks still apply); Public = no auth.
Auth-disabled single mode follows existing middleware, not new SDK grants.
Future resource rights in the last column are required composition, not a claim
that legacy scopes implement those rights. Revisions are only stated where the
wire API carries them; do not invent CAS guarantees for legacy mutations.

Effects: read = domain read, **not** a proof of zero inherited telemetry; content
= files/index/domain events and possible scheduled work; work = queue/dispatch;
provider = external or paid work; state = persisted domain/control state. Effects
are conservative and need per-handler proof before any script allowlisting.

| Operation ID | Method/path | Scope | Preconditions / effects / resource rights |
|---|---|---|---|
| health.get | GET /health | Public | read; no resource identity |
| entries.list | GET /entries | R | filters/limit/offset; visible entry set/count |
| entries.get | GET /entries/{id} | R | immutable ID or legacy path; entry read |
| entries.create | POST /entries | A | content; destination edit, separate work/automation permission for runnable types |
| entries.update | PATCH /entries/{id} | A | expected_revision; content; source/destination edit, runnable-field checks |
| entries.updateMetadata | PATCH /entries/{id}/metadata | A | metadata validation; content/work-sensitive keys; verify revision behavior |
| entries.move | POST /entries/{id}/move | A | source/destination authorization; content; verify revision behavior |
| entries.delete | DELETE /entries/{id} | A | confirm=true required, force overrides live-claim guard; 204 empty; no revision input; content deletion |
| entries.bulkUpdate | POST /entries/bulk-update | A | nonempty bounded filter; per-entry edit/work checks; partial outcomes |
| entries.bulkDelete | POST /entries/bulk-delete | A | nonempty bounded filter; per-entry delete; partial outcomes |
| search.query | POST /search | R | provider for semantic/hybrid; visible results/count/snippets |
| search.inject | POST /inject | R | provider/telemetry review required; visible content |
| sections.list | GET /entries/{id}/sections | R | read entry/sections |
| sections.get | GET /entries/{id}/sections/{title} | R | read entry/section |
| graph.backlinks | GET /entries/{id}/backlinks | R | read both ends; suppress hidden edges/counts |
| graph.outlinks | GET /entries/{id}/outlinks | R | read both ends; suppress hidden edges/counts |
| graph.related | GET /entries/{id}/related | R | read source and candidates |
| graph.orphans | GET /orphans | R | visible entries/count; hidden links must not leak |
| projects.list | GET /tasks | R | visible project set; read |
| projects.delete | DELETE /tasks/{projectId} | A | destructive all-entry-type wipe; project manage/delete |
| projects.getPlacement | GET /projects/{projectId}/placement | R | project placement read |
| projects.setPlacement | PUT /projects/{projectId}/placement | A | state/work placement; project manage |
| tasks.list | GET /tasks/{projectId} | R | visible task/dependency set; read |
| tasks.get | GET /tasks/{projectId}/{taskId} | R | task read; context/source checks |
| tasks.ready | GET /tasks/{projectId}/ready | R | task/dependency read |
| tasks.waiting | GET /tasks/{projectId}/waiting | R | task/dependency read |
| tasks.blocked | GET /tasks/{projectId}/blocked | R | task/dependency read |
| tasks.next | GET /tasks/{projectId}/next | R | task/dependency read; not a claim |
| tasks.status | POST /tasks/{projectId}/status | R | bounded ID set; task read |
| tasks.metadata | GET /tasks/{projectId}/{taskId}/metadata | R | task metadata read |
| tasks.claimStatus | GET /tasks/{projectId}/{taskId}/claim-status | R | task claim metadata read |
| tasks.resume | POST /tasks/{projectId}/{taskId}/resume | A | abandoned/live-claim guard; work; task submit |
| tasks.resumeWithContext | POST /tasks/{projectId}/{taskId}/resume-with-context | A | live injection or relaunch; work; context read + task submit |
| tasks.assign | PUT /tasks/{projectId}/{taskId}/assignment | A | placement validation; state/work; task manage |
| tasks.clearAssignment | POST /tasks/{projectId}/{taskId}/assignment/clear | A | state/work; task manage |
| tasks.trigger | POST /tasks/{projectId}/{taskId}/trigger | A | work; task submit |
| tasks.run | POST /tasks/{projectId}/{taskId}/run | A | work; task submit |
| tasks.dispatch | POST /tasks/{projectId}/{taskId}/dispatch | A | placement/claim guard; work; task submit |
| tasks.logs | GET /tasks/{projectId}/{taskId}/logs | R | protected task/source output read |
| tasks.delivery | GET /tasks/{projectId}/{taskId}/delivery | R | task delivery evidence read |
| tasks.verifyDelivery | POST /tasks/{projectId}/{taskId}/delivery | A | state; task manage |
| features.list | GET /tasks/{projectId}/features | R | visible feature/task set |
| features.ready | GET /tasks/{projectId}/features/ready | R | visible feature/dependency set |
| features.get | GET /tasks/{projectId}/features/{featureId} | R | visible feature/tasks/context |
| features.chains | GET /tasks/{projectId}/chains | R | visible chain/dependency set |
| features.checkout | POST /tasks/{projectId}/features/{featureId}/checkout | A | content/work; per-task read + submit |
| features.assign | PUT /tasks/{projectId}/features/{featureId}/assignment | A | placement; state/work; per-task manage |
| features.clearAssignment | POST /tasks/{projectId}/features/{featureId}/assignment/clear | A | state/work; per-task manage |
| features.run | POST /tasks/{projectId}/features/{featureId}/run | A | work; per-task submit |
| features.cancel | DELETE /tasks/{projectId}/features/{featureId}/run | A | work cancellation; chain manage |
| features.resume | POST /tasks/{projectId}/features/{featureId}/resume | A | per-task claim guards; work; partial results |
| features.resumeWithContext | POST /tasks/{projectId}/features/{featureId}/resume-with-context | A | context/output authority + per-task claim guards; work |
| projects.run | POST /tasks/{projectId}/run | A | ready-feature fanout; work; per-task submit |
| goals.list | GET /goals | R | filtered visible goal set |
| goals.progress | GET /goals/{goalId}/progress | R | goal + linked task reads |
| goals.audit | GET /goals/{goalId}/audit | R | goal/source output reads |
| goals.create | POST /goals | A | content/work; destination edit + automation submit |
| goals.update | PATCH /goals/{goalId} | A | content/work; goal edit + automation submit |
| goals.delete | DELETE /goals/{goalId} | A | content; goal delete |
| goals.run | POST /goals/{goalId}/run | A | work/provider; goal submit + downstream source/effect permissions |
| automations.run | POST /automations/run | W | work/provider; automation submit + effect delegation |
| automations.runs | GET /automation-runs | R | visible execution/source output |
| automations.getRun | GET /automation-runs/{runId} | R | execution/source output |
| reminders.list | GET /reminders | R | visible reminders |
| reminders.get | GET /reminders/{reminderId} | R | reminder read |
| reminders.create | POST /reminders | A | content/work/notices; reminder edit + action permissions |
| reminders.update | PATCH /reminders/{reminderId} | A | content/work/notices; reminder edit + action permissions |
| reminders.delete | DELETE /reminders/{reminderId} | A | content; reminder delete |
| reminders.ack | POST /reminders/{reminderId}/ack | A | state; reminder act |
| reminders.snooze | POST /reminders/{reminderId}/snooze | A | state/scheduling; reminder act |
| reminders.fire | POST /reminders/{reminderId}/fire | A | work/provider/notices; reminder act + action permission |
| attention.list | GET /attention | R | authenticated recipient + source read |
| attention.counts | GET /attention/counts | R | authenticated recipient; hidden-source count protection |
| attention.get | GET /attention/{id} | R | recipient + source read |
| attention.create | POST /attention | W | state/notices; authorized publication audience |
| attention.read | POST /attention/{id}/read | R | state; own recipient item |
| attention.unread | POST /attention/{id}/unread | R | state; own recipient item |
| attention.snooze | POST /attention/{id}/snooze | R | state; own recipient item |
| attention.resolve | POST /attention/{id}/resolve | R | state; own recipient item |
| attention.dismiss | POST /attention/{id}/dismiss | R | state; own recipient item |
| attachments.list | GET /attachments | R | visible attachment catalog/count; resource grants required |
| attachments.get | GET /attachments/{attachmentID} | R | attachment/source read |
| attachments.download | GET /attachments/{attachmentID}/content | R | bounded binary output; attachment/source read |
| attachments.text | GET /attachments/{attachmentID}/text | R | derived output/source read |
| attachments.upload | POST /attachments | A | CAS/content; destination edit; bytes, never server-local caller paths |
| attachments.extract | POST /attachments/{attachmentID}/extract | A | provider/derived content; source read + extraction/spend |
| attachments.delete | DELETE /attachments/{attachmentID} | A | CAS/content deletion; attachment delete |
| attachments.forEntry | GET /entries/{id}/attachments | R | entry + attachment/source read |
| attachments.attach | POST /entries/{id}/attachments | A | content relationship; entry edit + source read/publication |
| attachments.detach | DELETE /entries/{id}/attachments/{attachmentID} | A | content relationship; entry edit |
| webhooks.list | GET /webhooks | Auth | handler policy review; subscription/source read |
| webhooks.get | GET /webhooks/{id} | Auth | handler policy review; subscription read |
| webhooks.create | POST /webhooks | Auth | state/future egress; subscription manage + source audience |
| webhooks.update | PATCH /webhooks/{id} | Auth | state/future egress; subscription manage + source audience |
| webhooks.delete | DELETE /webhooks/{id} | Auth | state; subscription delete |
| webhooks.deliveries | GET /webhooks/{id}/deliveries | Auth | protected delivery/source output |
| webhooks.test | POST /webhooks/{id}/test | Auth | outbound provider/effect; subscription manage + effect permission |
| events.recent | GET /events/recent | Auth | visible event/source output; handler policy review |
| events.stream | GET /events/stream | Auth | current source authority per output; handler policy review |
| events.wait | GET /events/wait | R | bounded wait + current source authority |
| events.resourceHealth | GET /events/resource-health | R | visible resource diagnostics |
| observability.stats | GET /stats | R | visible aggregates, not hidden totals |
| observability.timeline | GET /timeline | R | visible source events |
| observability.stale | GET /stale | R | visible entries/counts |

Task creation/update/dependency changes and automation CRUD are typed aliases of
entry operations with type-specific validation, not invented REST routes.
Pagination, exact DTOs, errors, request IDs and revision semantics remain to be
encoded and verified per operation; this inventory does not satisfy that gate.

Explicitly outside the script facade: `/control/**`, `/tokens/**`, `/auth/**`,
`/runners/**`, `/instances`, `/config/**`, `/push/**`, `/assistant/**`,
`/embeddings/backfill`, `/attachments/backfill/extraction`, `/sync/**`,
`/tasks/runner/**`, runner claim/release/renew/log ingestion, dispatch protocol,
`/supervision/**`, and event ingestion. Operator surfaces, bulk-job execution,
monitors, link-generation and client-context resolution require separate SDK
support decisions; no generic fallback exposes them. Capability discovery and
script execution routes are not registered at this checkpoint.
