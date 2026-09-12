# P4.9 — tenant graph resource ownership

Scope: phases 1–3 on `ce103b16`, not multi-mode activation. Runtime still uses
schema 28 and the existing single-mode startup guard. Phase 2 adds an internal
cache/request-selection seam; phase 3 exercises it on test-only staged 29.
No production migration, SDK, deployment or public signup is introduced here.
The approved [D01–D13 contracts](multi-tenant-security-contracts.md) and
[ownership inventory](multi-tenant-ownership-inventory.md) remain authoritative.

## Composition seam

`apiserver.newTenantGraph(ctx, TenantStore, tenantfs.Resolver, Config,
graphIdentity)` builds the service and API handler graph. It derives the ID from
the store and looks up the persisted mapping; neither a request selector nor a
config path is filesystem authority. BrainDir and blob root are replaced by the
mapping's absolute anchors. Indexer, service writers and CAS retain the bound
tenantfs policy, including later drift checks. Missing roots fail; construction
never provisions them. The existing CAS constructor creates/validates `.staging`;
this is the only construction-time filesystem allocation, not a content scan.

All slices and bool pointers in the current Config are copied. No configuration
setter or mutable global "current tenant" exists. Extend `copyGraphConfig` and
its aliasing test when Config gains reference-bearing fields. Graph fields are
private to server composition; immutability means the tenant/config/dependency
binding, not that service coordination maps are read-only.

The seam deliberately stays internal to `apiserver`, where phase 2 can implement
the graph manager without exposing raw services to external callers. Existing
`api.Handler` is the routing seam. `graphIdentity` contains narrow interfaces for
legacy login/token compatibility; only single-mode composition supplies it today.
Do not pass the single-mode token adapter or installation password capability to
future multi-mode tenant handlers. Identity/operator routes require their own
structural composition, not a cache lookup followed by legacy auth options.

## Single boot and shutdown order

Single boot still performs storage/claim/root initialization first. Graph assembly
then precedes an asynchronous one-shot `IndexChanged`, synchronous built-in AI and
simple checkout installation, and explicit worker startup. HTTP startup does not
wait for the scan. The watcher is still optional/off by default and starts only
after the scan, including when the scan reports an error. Existing worker
intervals, immediate scheduler pass, automation replay and reminder sweep remain.
CAS initialization now fails before launching the scan/installations; no worker
cleanup is needed on constructor failure.

The returned boot cleanup is idempotent and has this order:

1. Cancel/join boot workers (`startSingleGraphWorkers`).
2. Fence watcher startup, join the boot scan, stop/join the watcher.
3. `tenantGraph.Close`: fence, cancel and join detached embeddings and webhook
   deliveries through service-owned `asyncWork` groups.
4. Only the composition owner closes the shared database.

The graph **never calls TenantStore.Close or shared pool Close**. New completion
channels from claim cleanup, runner lifecycle, scheduler and cascade let the boot
owner join their goroutines. Other loops are blocking Start methods launched in
a boot-owned wait group. Services can still be used independently; owners that
schedule detached work must call BrainService/WebhookService Close themselves.

Synchronous HTTP requests and streams must be drained before graph Close. It is
not an HTTP admission barrier. `RunServer` retains its existing graceful shutdown
behavior; forced/time-limited shutdown and hijacked bridge connections are not
made into a full stream-revocation solution by this extraction. A provider that
ignores context can delay a join; never close the DB on a timeout while work can
still use it. IndexChanged currently has no cancellable API, so cleanup waits for
it to finish rather than abandoning it. No bounded eviction latency is claimed.

## Resource audit

| Resource | Owner / sharing rule / remaining obligation |
| --- | --- |
| SQLite pool, ControlStore, tenant root repository | Deployment composition owns lifetime; graph borrows only TenantStore plus authoritative root binding. P4.10 must still remove promoted owner/control methods. Identity token last-used work is not graph-owned and needs separate owner audit. |
| Brain/task/indexer/attachment services | One tenant binding; same indexer/policy for writes. No workers or scans from graph construction. Synchronous extraction/backfill must retain a request/worker lease until completion. |
| Embedding HTTP client | Config/credential/model snapshot per graph. `AiFactoryEmbeddingClient` stores immutable fields, creates request bodies/headers locally and uses concurrency-safe http.Client. Its nil Transport uses the shared default transport: connection pooling can be shared, but not mutable auth, cookie jars, content/vector caches, budgets or attribution. Do not mutate transport/TLS settings concurrently (legacy loopback TLS setup remains outside the graph). Provider calls still need P8/P10 grant and paid-call fences. |
| Detached embeddings | Graph-owned per-path locks and async work group. Request disconnect does not cancel a persisted write's refresh; graph Close does. Close fences new submissions before Wait, including metadata syncs. No durable job/revocation guarantee is implied. |
| Attachment extraction and Assistant providers | Graph-local service/config; synchronous request work on this branch. No shared mutable tenant credentials or derived caches. Cancellation does not undo already authorized provider effects. |
| Realtime Hub, EventHub, bridge Hub, event dedup, trigger state, log buffer | Created per graph, never process-global. Existing project/runner topic names are safe only inside that private hub. Replay cursors and stream authorization still require P6/P8. Phase 2 must retain graph references for SSE/bridge/control work and avoid two simultaneous graphs for one tenant splitting live state. |
| Scheduler, claim expiry, runner lifecycle, cascades, automations, goals, reminders, triggers | Graph-bound state but **boot/runtime-owned work**, not cache-owned scheduling. Future authoritative active-tenant enumeration must service quiet tenants even when no HTTP graph is resident. Do not start one perpetual scheduler per LRU entry or stop durable scheduling on eviction. Keep service grants, dedup and cascade continuity across cache churn. |
| Webhook dispatcher / delivery | Boot owns subscriber loop; graph owns detached delivery/retry tasks. Cancel/join subscriber first, then service Close. HTTP transport is borrowed, never globally closed by a tenant. External deliveries already accepted cannot be recalled; durable publication, SSRF policy, tenant grant and commit fencing remain later work. |
| Watcher/boot index | Single boot owns them, outside graph constructor. FileWatcher.Stop joins its debounce/event loop; scan is separately joined. Future multi-tenant indexing needs independent fair maintenance ownership, not incidental LRU residency. |
| Runner executor registry | Remains **tenant runner-owned** in runner process composition, not server graph/cache. No executor created by this change. Hosts, homes, credentials, caches, instances and process supervisors must not be shared across tenant runners. |
| Rate limit, OAuth, MCP, deployment config | Outside tenant graph lifecycle. Existing single-mode auth/router behavior retained. Do not confuse shared IP protection or identity state with tenant quotas. Hosted MCP must eventually propagate trusted verified identity in-process, not reconstruct authority via loopback headers. |

## Newer-main integration handoff — not implemented in this phase

Read against main's `docs/sdk-multitenant-integration.md` and current composition
on 2026-09-12. These features are absent from this P4 branch and must not be lost
when reconciling main. Their presence on main is not shared-SQLite tenancy proof.

- **Assistant conversation jobs:** `api/assistant_jobs.go` currently opens
  `assistant-jobs/jobs.db`, recovers jobs, installs mutable `s.jobs`, launches a
  coordinator plus child/lease-renewal workers, and returns cancel/join/close.
  Split graph wiring from recovery and runner registration. Keep the single-mode
  path until coordinated migration; multi mode needs tenant-qualified rows in
  shared SQLite, principal/grant envelopes, fenced task mirrors/leases/results,
  invalidation, and independent scheduling. Never open a sidecar per cached tenant
  or run recovery on cache misses. Its fixed conversation-runner ID needs tenant
  ownership, not deployment-global identity.
- **Voice/transcription/speech:** main adds Assistant provider roles and voice
  diagnostics, plus browser capture/playback state. Bind calls and diagnostics to
  the authorized tenant/principal and provider budget; cancel/discard late audio
  and results on switches or revocation. Extend config copying for new fields.
- **Push:** `storage.OpenPhonePush` currently opens `push/notifications.db`;
  `Handler.StartPush` immediately collects/delivers then repeats every 10 seconds.
  Keep this worker outside construction/cache residency; join before sidecar/owner
  close. Migrate subscriptions, pending notifications and dedup to tenant/principal
  ownership in shared SQLite. Remove installation-wide empty-owner broadcast from
  any multi-mode path; require tenant opt-in and send-time authority checks.
  Already handed-off push payloads cannot be retracted; minimize sensitive data.
- **Offline/browser caches:** partition persisted entries, drafts, conversations,
  sync cursors and queued operations by origin/principal/tenant/grant generation.
  Server graph isolation alone does not isolate service workers or IndexedDB.
  Stop old requests/streams and reject late results on identity switches. No
  automatic replay of ambiguous writes under replacement credentials.
- **Bulk jobs:** main's `BulkJobService` uses TenantStore, an explicit index-ready
  channel, recovery and a serial worker with Stop. Preserve that readiness barrier,
  uncertain-item semantics and event/cascade publication. Move constructor wiring
  into the graph only after its tables/receivers and actor/idempotency keys are
  tenant-qualified. Worker lifetime and recovery remain outside the LRU, with
  cancellation/join and grant checks at admission/execution/commit.
- P4/P6/P8/P9 must inventory all of these for migration, revocation, scoped
  export/restore and deletion. Keep affected multi-mode capabilities blocked until
  verified. SDK interfaces cannot manufacture trusted authority or replace these
  server obligations.

## Phase 2/3 implementation and evidence

Phase 2 implements bounded lazy LRU, in-flight references including long-lived work,
single-flight construction, invalidation/suspension, structural request selection
and independent worker/realtime ownership. No cached object or selector is an
authorization grant. Avoid duplicate coordination state for one tenant.

Phase 3 adds real HTTP equal-ID isolation and legacy compatibility against explicit
**staged schema-29 fixtures**, shared-DB concurrency and isolation benchmarks.
Do not remove the schema-28 startup gate to make those tests run. Phase-1 local
fixtures and private-hub tests are not evidence for phase-3 multi-tenant HTTP
isolation, revocation, scalability or the final atomic migration/recovery gate.

See [phase 3 acceptance and measurements](p4-tenant-graph-evidence.md) for exact
commands, fixture limitations, measured construction costs and cache sizing.
The workload allowlist is read/search only. Generic entry writes can reach
unmigrated task/background metadata; enabling them would bypass the intended
boundary. Tasks, control, runner/identity/operator APIs, hosted MCP, SSE, Assistant,
bulk jobs, paid extraction and push remain unavailable through this seam (501).
Single mode retains its full router. The manager is **not** mounted as an
operational multi-mode server.

### Explicit downstream ownership (including newer-main work)

| Phase | Required handoff; not implemented by graph isolation |
| --- | --- |
| P4.10 / final P4 | Remove TenantStore embedding/promoted control and Close methods; migrate remaining task/background receivers and install claim together. Reconcile newer-main Assistant jobs, phone push and bulk tables/sidecars into the shared-SQLite inventory before admitting those routes. Keep atomic publication and old-binary refusal; staged tests do not authorize migration. |
| P6 | Supply online principal/membership/tenant/grant authority to the selector and lifecycle permits. Durable epochs plus invalidation must cover absent caches. Fence protected output and write commit, not only cancellation. Assistant jobs, voice calls and push subscriptions need principal/tenant ownership and revocation. Reusing a logical entry ID is tested; deleted tenant IDs must never be reused. |
| P7 | Runner/executor registries, homes and external session databases stay tenant runner-owned. Assistant conversation-runner identities and job/task mirrors need tenant-qualified enrollment, narrow credentials and attempt leases. No standing-token restoration or shared host fallback. |
| P8 | Independent fair maintenance/scheduling owns scans, watchers, jobs, bulk recovery, push delivery, automations and reminders; cache residency must neither schedule nor unschedule durable work. Retain graph leases until joined work ends. Add event/frame authorization, replay isolation and verified-context hosted MCP; no loopback/header authority reconstruction. Preserve newer-main bulk index-ready barrier and crash/uncertain-publication semantics. |
| P9 | Partition browser/offline drafts, conversations, audio, push opt-ins and queued operations by origin/principal/tenant/grant generation; discard late callbacks on switches. Provision roots explicitly and inventory sidecars in scoped export/restore/erasure; never export the shared DB/WAL or revive revoked identities. Preserve legacy paths in coordinated sidecar migration. |
| P10 | Measure shared-root resolution/FTS/SQLite contention, admission fairness and sustained memory under growing hubs/queues; graph idle-footprint numbers are not quotas. Voice, Assistant jobs, extraction, embedding and retries require atomic paid reservations/settlement. Push and other external effects need send-time authority; accepted external payloads cannot be recalled. |

The main-checkout untracked `docs/sdk-multitenant-integration.md` and supervisor
`/Users/huy/.codex/brain-api-supervisor/20260912-continuation.md` were read for this
handoff. No SDK file or Brain task state was changed. Newer-main features have not
been merged into this worktree and are not covered by this branch's runtime tests.
