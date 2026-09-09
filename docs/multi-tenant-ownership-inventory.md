# Multi-tenant ownership inventory

P0 `7iyskorq`; source baseline `a1440e7`, inspected 2026-09-06. This is a
manual source inventory, not implemented tenancy or passing isolation evidence.
See [security contracts](multi-tenant-security-contracts.md) for approved decisions
D01-D13 and required verification V01-V16, still NOT RUN by P0.

## Approval record

The user in the existing supervising Codex task approved D01-D13 without
amendments at `2026-09-06T21:02:01.445953+00:00`, responding
"Approve the proposed contracts for implementation" to the security contract
at revision `4d10904afeedfb8791a600f5e925bc82b64bf97b`. The approval is recorded
in Brain task `7iyskorq` and canonical plan `9fguh2pr`. This inventory is the
ownership handoff for those approved implementation contracts, not a new source
scan or runtime evidence. Its source baseline remains `a1440e7`.

Design approval does not authorize production deployment, single-mode
query-credential cutover, credential rotation or public activation. Hosted
execution remains blocked pending a separately reviewed and approved VM design.
P0 completion requires committed documents, plan/task reconciliation and
verification; P4 still requires finalized P0 AND complete P3.

## Classification rules

- **T**: tenant data. Mandatory tenant scope on reads, writes, joins, caches and work.
- **I**: identity registry. Narrow ControlStore operations with principal/session
  checks; global identity does not grant access to any tenant.
- **O**: deployment operator. Separate capability, not tenant `admin:*`.
- **T/O**: split the operation: tenant-owned view/control versus separately guarded
  deployment aggregate or host operation. Never expose the aggregate as a tenant view.

All object IDs are lookup hints, not grants. Tenant owner/admin/member permissions
are narrowed by token capability. Every indirect lookup verifies ownership, even
when an identifier is globally unique. Tenant-global entries remain T. Built-in
templates are operator-owned code/configuration copied into each tenant, not an
unrestricted shared entry namespace. Cross-tenant entry, attachment, dependency,
goal, feature, webhook, runner, session and placement relationships are forbidden.

## SQLite tables

Authoritative DDL and upgrades: `internal/storage/schema.go`; connection setup:
`internal/storage/storage.go`. Baseline: 32 ordinary application tables and one
FTS5 virtual table, schema version 27, WAL and one pooled connection. No tenant
column exists in this baseline. This list includes tables created through old
migrations, not just the current initialization list.

| Table | Class | Required ownership/key contract |
|---|---|---|
| notes | T | Non-null tenant; unique (tenant,path); project/global/short ID lookups scoped |
| links | T | Tenant plus both note endpoints; unresolved target path cannot resolve across tenants |
| tags | T | Inherit note tenant; composite parent reference |
| entry_meta | T | (tenant,path); explicitly purge/rekey, currently no note FK |
| generated_tasks | T | (tenant,key); same-tenant task_path/feature; idempotency never global |
| schema_version | O | One shared atomic migration version; not a tenant setting |
| api_tokens | I | Principal, tenant grant, capabilities, revocation; tenant token administration cannot mint operator grants |
| event_log | T/O | Explicit envelope owner and (tenant,dedup_key); operator events separate and not tenant-subscribable |
| oauth_clients | I | Protocol client registration is not membership; bounded public registration, explicit grant/audience |
| oauth_auth_codes | I | Bind client, principal, selected tenant, redirect/PKCE and one-use grant |
| oauth_access_tokens | I | Session/principal/tenant grant and epochs; online authorization |
| oauth_refresh_tokens | I | Same ownership; atomic rotation/replay-family revocation |
| task_claims | T | (tenant,project,task); same-tenant runner and attempt fence |
| task_dispatch_leases | T | Same task key; runner/machine ownership, immutable lease generation |
| task_placement_reasons | T | Tenant task diagnostics; no foreign candidates or host secrets |
| feature_assignments | T | (tenant,project,feature); assigned runner belongs to tenant |
| runners | T/O | Immutable tenant enrollment; operator capacity/lifecycle distinct from tenant control |
| opencode_instances | T | Tenant runner and session/task ownership, including ad-hoc instances |
| project_pause_state | T | (tenant,project); ordinary pause never overrides security suspension |
| feature_pause_state | T | (tenant,project,feature); explicit erasure despite surviving feature removal |
| runner_pause_state | T/O | Tenant runner key; deployment emergency stop separately authorized |
| feature_cascade_roots | T | Tenant project/feature roots and all dependency traversal |
| project_placement | T | Tenant preferences restricted to authorized resources |
| brain_clients | T/I | Principal-bound tenant enrollment; host/home/user fields sensitive, never authentication |
| brain_client_workspaces | T | (tenant,client,path); project and client same tenant; observations never grants |
| webhooks | T/O | Tenant subscriptions/secrets; operator-only internal-network policy separate |
| webhook_deliveries | T | Inherit webhook tenant; redact secrets, bounded retention |
| note_embeddings | T | Tenant + note/chunk; composite note reference |
| note_embeddings_meta | T | Same ownership as vector and note; tenant prefilter before candidate loading |
| attachments | T | UNIQUE(tenant,digest); project visibility within tenant explicitly modeled |
| entry_attachments | T | Both note and attachment parents same tenant; scoped refcount/delete serialization |
| attachment_derived | T | Inherit attachment tenant; extraction/OCR/status/errors and caches scoped |
| notes_fts (baseline) | T | Replace with mapped tenant-specific FTS tables in shared SQLite, D07 approved; never filtered global BM25 |

FTS shadow tables `notes_fts_data`, `notes_fts_idx`, `notes_fts_docsize` and
`notes_fts_config` inherit the index owner; external content uses `notes`, not a
`notes_fts_content` table. Approved per-tenant shadow tables inherit their mapped
tenant. `sqlite_sequence`, SQLite schema catalogs and optional engine statistics
are O engine metadata, never exported as a tenant database. Migration-v4
`oauth_access_tokens_new` and `oauth_refresh_tokens_new` are temporary I tables.

Migration coverage: v1 baseline; v2 token revoke; v3 OAuth; v4 OAuth replacement;
v5 events; v6 claims; v7 runners; v8 scopes; v9 webhooks; v10 assignments;
v11 capabilities; v12 embeddings; v13 attachments; v14 derived text;
v15 clients/workspaces; v16 instances; v17 placement; v18 runner resources;
v19 dispatch; v20 placement reasons; v21 lease IDs; v22 project pause;
v23 runner pause; v24 cascades; v25/v26 link checksum invalidations;
v27 feature pause. Instance columns are also ensured outside version guards;
clients/workspaces are created through v15. Initialization creates tables/FTS/
triggers before upgrades and has no encompassing migration transaction today.
Reserve the next available version during P4, do not assume v28 is still free.

Current FKs cover note children, attachment children, webhook deliveries and
OAuth codes. Most project/task/runner/client/path relationships have no FKs.
Embedding metadata and vectors independently reference notes, not each other.
P4 must rebuild tenant-qualified parent uniqueness and composite FKs or provide
transactional equivalent tests; foreign keys enabled alone do not establish tenancy.

Current project purge (`internal/storage/project_purge.go`) retains events/shared
blob bytes and omits feature pause, workspace observations, generation dedup and
path metadata. Its claim that note deletion cascades entry_meta is not supported
by the DDL. Tenant erasure must inventory these explicitly, not reuse purge as-is.

## Routes

Source: `internal/api/router.go`. All rows below are prefixed `/api/v1`.
Brace alternatives enumerate paths; they do not grant wildcard authority.
These are approved ownership classifications, not assertions about current guards.
Fallback/501 registrations duplicate the same surface and must use the same guard.

| Methods and paths | Class / required boundary |
|---|---|
| GET /health | O public minimal liveness only, no tenant/config secrets |
| POST /tokens/bootstrap | I/O install-claim gate; never public tenant provisioning |
| POST /auth/{login,refresh,logout} | I protocol credentials, session ownership and revocation |
| GET /server/requests/recent | O aggregate; tenant diagnostic view requires separately scoped data |
| GET /config, /config/schema, /config/task-defaults; PUT /config | O deployment config; any future tenant settings need separate projection |
| GET /{stats,orphans,stale} | T includes global entries only within tenant |
| POST /{search,inject,link,embeddings/backfill} | T scope before ranking/limits; backfill budget and cancellation |
| GET, POST /attachments/ | T upload/list scoped and quota reserved |
| GET, DELETE /attachments/{attachmentID}; GET /attachments/{attachmentID}/{content,text} | T row-ID lookup and bytes/derived ownership |
| POST /attachments/backfill/extraction; POST /attachments/{attachmentID}/extract | T paid-call grant and budget |
| GET, POST /entries/; POST /entries/{bulk-update,bulk-delete} | T atomic mixed-scope rejection |
| GET /entries/{id}/{attachments,sections,backlinks,outlinks,related}; GET /entries/{id}/sections/{title} | T every graph endpoint and attachment parent |
| POST /entries/{id}/attachments; DELETE /entries/{id}/attachments/{attachmentID} | T both parents |
| GET, POST, PATCH, DELETE /entries/* | T wildcard subdispatch includes POST move/verify and PATCH metadata; validate stored and supplied paths |
| POST /events/; GET /events/{stream,recent} | T envelope ownership derived from auth; operator events not caller-asserted |
| GET /automation-runs, /automation-runs/{runId}; POST /automations/run | T wildcard projects stay in tenant; action grant required |
| GET /assistant/status; POST /assistant/{chat,chat/stream,goal-draft} | T tools inherit principal and tenant; status excludes platform secrets |
| GET, POST /goals/; PATCH, DELETE /goals/{goalId}; GET /goals/{goalId}/{progress,audit}; POST /goals/{goalId}/run | T goal and linked tasks |
| GET, POST /reminders/; GET, PATCH, DELETE /reminders/{reminderId}; POST /reminders/{reminderId}/{ack,snooze,fire} | T dates do not authorize execution |
| GET /scheduler/status | T/O tenant projection versus operator fleet aggregate |
| GET, PUT /projects/{projectId}/placement/ | T authorized runner resources only |
| GET /tasks/, /tasks/stream | T project discovery and replay, never deployment wildcard |
| POST /tasks/runners/{runnerId}/dispatch/{ack,reject,release} | T runner credential subject, tenant, task and lease fence |
| GET /tasks/runner/status | T/O tenant pause projection versus operator aggregate |
| POST /tasks/runner/{pause,resume}; POST /tasks/runner/{pause,resume}/{projectId} | T all selected-tenant projects or one project; deployment-wide variant O |
| POST /tasks/runner/features/{pause,resume}/{projectId}/{featureId} | T feature ownership |
| POST /tasks/runner/automations/{pause,resume}; POST /tasks/runner/automations/{pause,resume}/{projectId} | T all tenant or project automations; deployment variant O |
| GET /tasks/{projectId}/; GET /tasks/{projectId}/{ready,waiting,blocked,next,features,chains,stream} | T query/dependency/subscription scope |
| GET /tasks/{projectId}/features/ready; GET /tasks/{projectId}/features/{featureId} | T feature membership |
| POST /tasks/{projectId}/status | T every requested task |
| GET /tasks/{projectId}/{taskId}; GET /tasks/{projectId}/{taskId}/{claim-status,metadata,dispatch-lease,placement-reasons,logs} | T no foreign diagnostic metadata |
| POST /tasks/{projectId}/{taskId}/{claim,release,renew,logs} | T runner subject and attempt bound |
| POST /tasks/{projectId}/features/{featureId}/{checkout,assignment/clear,run,resume}; PUT /tasks/{projectId}/features/{featureId}/assignment; DELETE /tasks/{projectId}/features/{featureId}/run | T authorized execution; force/manual cannot bypass suspension |
| POST /tasks/{projectId}/run; DELETE /tasks/{projectId}/ | T project action grant; deletion not tenant deletion |
| POST /tasks/{projectId}/{taskId}/{trigger,dispatch,run,resume} | T action/attempt ownership |
| GET, POST /tokens/; DELETE /tokens/{name} | I tenant grant administration versus O operator credential administration; no scope escalation |
| POST /context/resolve | T/I records observations; read credential alone must not enroll foreign clients |
| GET, POST /webhooks/; GET, PATCH, DELETE /webhooks/{id}; GET /webhooks/{id}/deliveries; POST /webhooks/{id}/test | T subscription owner plus outbound destination policy |
| GET /runners/, /runners/{runnerId}, /runners/{runnerId}/{stream,instances}; GET /instances | T tenant registry projection; O separate fleet view |
| POST /runners/register; POST /runners/{runnerId}/{heartbeat,deregister} | T/I enrollment and subject-bound heartbeat |
| PUT /runners/{runnerId}/{affinity,pause,resume,shutdown}; PATCH /runners/{runnerId}/config | T/O approved tenant-owned runner control; platform-host configuration O |
| PUT, DELETE /runners/{runnerId}/instances/{instanceId}; GET /runners/{runnerId}/bridge | T tenant/runner enrollment plus instance owner |
| GET /monitors/, /monitors/templates; POST /monitors/; DELETE /monitors/by-scope; PATCH /monitors/{taskId}/toggle; DELETE /monitors/{taskId} | T instantiated monitors; O templates copied/read-only, never global content access |

### Control subtree

Prefix `/api/v1/control/runners/{runnerId}`. Every operation requires explicit
control capability AND tenant/runner/resource ownership. Raw host exec/signal is
O and remains unavailable to tenant credentials at initial release. Other
tenant-runner control needs an explicit owner-delegated grant; ordinary content
membership must not silently become host control. Platform hosting remains blocked.

| Methods and suffixes | Class |
|---|---|
| GET /sessions/{sessionId}/history | T tenant runner/session including external OpenCode DB |
| POST /tasks/{taskId}/abort | T tenant runner/task |
| POST /exec; POST /exec/{execId}/signal | O raw host execution |
| POST /instances/; DELETE /instances/{instanceId}/ | T explicit delegated spawn/kill, restricted workdir/environment |
| GET, POST /instances/{instanceId}/sessions | T instance and session owner |
| GET /instances/{instanceId}/sessions/status | T no foreign session aggregation |
| GET /instances/{instanceId}/sessions/{sessionId}/messages | T session owner |
| POST /instances/{instanceId}/sessions/{sessionId}/{prompt,abort} | T execution grant and session owner |
| POST /instances/{instanceId}/sessions/{sessionId}/permissions/{permissionId} | T owner-bound outstanding request, no approval escalation |
| GET /instances/{instanceId}/{permissions,events,agents,providers} | T restricted projection; no platform provider secrets |

Other registration surfaces:

- `internal/oauth/routes.go`: GET `/.well-known/oauth-authorization-server`,
  `/.well-known/oauth-protected-resource`, `/.well-known/oauth-protected-resource/mcp`;
  POST `/register`; GET/POST `/authorize`; POST `/token`. I protocol checks and
  bounded discovery/registration, not anonymous tenant data access.
- `internal/apiserver/server.go`: authenticated POST/GET/DELETE `/mcp/`,
  POST/DELETE `/`. T/I transport sessions bind tenant/principal and each tool's
  capability. GET MCP currently returns 405. No ambient unauthenticated client.
- `internal/webui/webui.go`: non-API GET/HEAD embedded assets or SPA fallback;
  API/OAuth/MCP paths delegate to router. Public static shell must contain no
  tenant content. `web/src/App.tsx`: `/auth/callback` and auth-gated `*` UI.

Current guards do not implement these classifications: event/webhook routes
lack a route scope requirement; missing auth context passes scope middleware;
legacy OAuth `mcp`/empty scope and JWT admin mapping grant broad access. See
`internal/api/middleware.go`. A future route manifest must record method,
pattern, owner lookup, capability, operator delegation and negative fixture.

## Caches and coordination state

All T state uses tenant-qualified keys and retains principal/grant epochs where
authorization is cached. Cache partitioning does not replace invalidation on
logout, suspension, membership change, deletion or tenant switch.

| Source | State / classification |
|---|---|
| internal/realtime/hub.go | T project/runner subscriber channels; separate O channels |
| internal/realtime/event_hub.go | T/O replay ring, sequence/cursors and live subscriptions |
| internal/service/event_service.go | T seenIDs dedup |
| internal/service/automation_service.go | T replay seen set, wildcard enumeration |
| internal/service/trigger_service.go | T cooldown and concurrency keys |
| internal/service/runner.go | T project/feature pauses; O deployment pause separately |
| internal/service/scheduler.go | T per-project results; O shared capacity/aggregate |
| internal/service/feature_cascade.go | T active project/feature cascade set |
| internal/service/goal_service.go | T goal mutex map |
| internal/service/reminder_service.go | T reminder mutex map |
| internal/service/task.go | T feature-resume in-flight map and runner-status memo |
| internal/service/brain.go | T embedding per-path locks and pending-operation wait group |
| internal/indexer/watcher.go | T pending path actions and debounce timers |
| internal/bridge/hub.go | T runner connections/jobs, pending requests, stream refs, permission/live status buffers |
| internal/logbuffer/logbuffer.go | T task log buffers |
| internal/api/requestlog.go | O 2,000-request aggregate ring; redaction before insertion |
| internal/api/ratelimit.go | O IP defense plus T quotas/SSE counters; neither substitutes for other |
| internal/api/auth_login.go | I login failure/lockout maps |
| internal/api/control.go | T/I action limiters; tenant and principal, not auth name alone |
| internal/mcp/http_transport.go | T/I session registry currently sessionID->bool; must bind authority |
| internal/mcp/context.go | Local process ambient context; forbidden as multi-mode HTTP authority |
| internal/mcp/planning_tools.go | T/I PlanningState (objectives, intent, document paths, audit); registration-scoped, fresh per HTTP request today, not a durable tenant session |
| internal/events/{bus,dedup,cron,matcher}.go | Optional/not wired by current server: T subscriptions, dedup/cron keys, cached automations |
| internal/realtime/bridge.go | Optional T per-project snapshot coalescing |
| internal/oauth/routes.go | Optional I in-memory clients/codes/refresh store; production uses persistent store |
| internal/runner/runner.go | T monitored projects, pause caches, dispatch channels and log streamers |
| internal/runner/{process_manager,state_manager,feature_tracker}.go | T process/task/feature maps and persisted finalization |
| internal/runner/executor.go | T serve/task/project maps and persisted process records |
| internal/runner/bridge_client.go | T ad-hoc instances, pumps/subscriptions/jobs; host discovery must not claim foreign processes |
| internal/runner/executor_common.go | T repo caches/workdirs; never shared cross-tenant credential helper/home |
| internal/runner/client.go | T credential-specific API health cache (10 seconds), not authorization cache |
| internal/runner/memory_guard.go | O host admission cache (3 seconds), warnings; no exposed foreign usage |

Browser T/I caches: `web/src/store/{workspace,entries,assistantChat}.ts` persisted
state; `web/src/lib/auth.ts` token/PKCE storage; `store/{selection,modal,ui}.ts`
transient state. Query families in `web/src/hooks/`: `useProjects`, `useEntries`,
`useGoals`, `useReminders`, `useMergeRequests`, `useAutomations`, `useAutomationRuns`,
`useDependentChains`, `useRunners`, `useRunnerStatus`, `usePauseState`,
`useSchedulerStatus`, `useSessions`, `useSessionTranscript`, `useAttachmentBlob`.
Additional query sites: `components/Session/PermissionBanner.tsx`,
`components/Modal/SettingsModal.tsx`, `components/RunnerProcesses.tsx`,
`components/Workspace/leaves/LogsLeaf.tsx`. Cancel requests/streams, discard stale
results, clear stores, revoke object URLs, reset transcripts and scope query keys
on identity switch. `web/vite.config.ts` currently uses network-only API caching;
service-worker upgrades must preserve that and purge obsolete protected caches.
`web/src/lib/cronSchedule.ts` expression/timezone cache is content-free and need
not be tenant-partitioned. No other cache receives that exemption by convention.

HTTP MCP also registers planning tools directly, not just REST-backed wrappers.
`RegisterPlanningTools` creates PlanningState per registration;
`plan_discover_docs` uses ambient `GetCachedContext`, API-host home/workdir and
caller `additional_dirs` to glob local markdown in `internal/mcp/planning_tools.go`.
This needs a separate transport guard: disallow server-filesystem discovery in
HTTP MCP, or replace it with authorized tenant document APIs. Stdio discovery
must stay explicitly local and contained in authorized roots. Test HTTP absolute
and traversal-bearing `additional_dirs`, not only attachment `file_path` and
`output_path`. Any future persisted planning state must bind principal, tenant
and session and be revoked/cleared with that session; no ambient singleton scope.

## Background loops and detached work

T work must capture immutable tenant/grant/generation at creation and recheck at
claim, effect authorization and commit. Trusted O enumeration constructs a T
service graph per active tenant, bounded/paginated with fair scheduling. Missing
identity, suspended tenant or unavailable authority fails closed, including retries.

| Work / source | Ownership and lifecycle |
|---|---|
| Boot IndexChanged, apiserver/server.go | T one boot scan per root; exclude nested foreign roots |
| Optional fsnotify/debounce, indexer/watcher.go | T starts after boot scan; cancel/drain before store close |
| Claim cleanup, service/task.go | O enumeration/T expiry every 60 seconds; scoped updates/events |
| Runner lifecycle, service/runner_registry.go | O enumeration/T stale/offline cleanup every 60 seconds |
| Push scheduler, service/scheduler.go | O fair enumeration/T dispatch immediately and every 5 seconds |
| Cascades, service/feature_cascade.go | T completion events and 30-second fallback |
| Automations, service/automation_service.go | T startup replay/events/minute cron; wildcard within tenant |
| Goals, service/goal_service.go | T events and 5-minute reconcile/steer |
| Reminders, service/reminder_service.go | T startup and minute sweep |
| Webhooks, realtime/webhook_dispatcher.go and service/webhook_service.go | T event subscription and detached deliveries; SSRF policy including DNS, redirects, IPv4/IPv6/private/metadata destinations |
| Task triggers, realtime/trigger_dispatcher.go and service/trigger_service.go | T all-event subscription, cooldown/concurrency |
| Embedding refresh, service/brain.go | T detached per-write goroutines/metadata sync, 2-minute contexts |
| Bridge, internal/bridge/hub.go | T per-connection loops, 30-second heartbeat and pending control |
| SSE, api/{sse,events,runners,control}.go | T request streams, replay, heartbeat, exec watchdog; revocation before delivery |
| Rate cleanup, api/ratelimit.go | O/IP and T counters without foreign diagnostic exposure |
| Token last-used, storage/tokens.go | I detached write, no revoked-token resurrection |
| Backfill/extraction/assistant/manual actions, service and api handlers | T long request work and retry delays inherit cancellation/budget |
| Optional events/{dedup,cron}.go and realtime/bridge.go | T cleanup/cron/coalescing if wired later; not exempt because inactive |

OAuth expiry cleanup functions have no observed periodic production caller.
Placement-reason pruning is write-triggered. REST event replay currently uses the
in-memory EventHub, not necessarily durable `event_log`.

Runner T loops in `internal/runner/runner.go`: startup/poll (default 30 seconds),
completion/idle detection, scheduling, pause sync/orphan reap, claim renewal
(5 minutes), heartbeat (default 30 seconds), project refresh, command consumer
and dispatch pool. Additional implementations: `dispatch_pool.go`,
`sse_listener.go` reconnects, `bridge_client.go` WebSocket reconnect/event pumps/
shell IO, `event_forwarder.go`, `log_streamer.go`, `process_manager.go` process
watch/termination, `executor.go` session discovery/orphan process recovery,
`pi_rpc.go` read/wait, `pi_executor.go`, `hooks.go`. All state/credential roots
must be tenant-exclusive; current cadences do not satisfy approved revocation
bounds merely by existing. `session_history_sqlite.go` reads external OpenCode
`message`/`part` tables: outside Brain schema, inside the runner confidentiality
boundary. Authorize session owner before reading that database.

O service lifecycle/signal/health-wait work is in `internal/lifecycle/{daemon,signals}.go`
and `cmd/brain/commands/lifecycle.go`. Browser T query polling and SSE reconnect
are in the hooks above and `web/src/lib/{sse,api}.ts`; static update polling in
`web/src/main.tsx` is content-free but must not revive an old authenticated cache.

## Inventory ratchet and verification handoff

P4/P8/P10 must turn this manual baseline into a reviewed machine-readable manifest.
Diff actual `sqlite_schema` after fresh initialization AND every supported upgrade
against classified ordinary/virtual/shadow tables. Enumerate registered method/path
pairs (including fallback, wildcard subdispatch, OAuth, MCP and outer handlers)
and fail CI for missing classifications/capability/owner fixtures. AST/source checks
must flag new storage receivers/raw DB access, persistent stores, maps/channels,
goroutines, tickers, subscription sites and browser query/persistence sites for
explicit review, not assume every map is tenant-sensitive. Record reviewed
content-free exceptions with rationale. Manifest removal also requires review.

Static checks enforce inventory coverage, not correctness. V01-V16 require
two-tenant equal-ID runtime tests for SQL/roots/blobs, rank invariance, cache
switches, stream replay, task/runner fences, suspended jobs, quota races and restore.
Exercise writes and side effects, not response filtering alone. Manual grouped
patterns and source names here can drift and cannot prove exhaustive runtime
coverage of dynamic tool dispatch or all future code. Re-inventory after merging
independent P1/P2/P3 work before schema implementation. No such CI automation or
runtime verification is claimed by this documentation commit.
