# Automation Event-Trigger Audit

**Scope:** the complete path from event emission through `AutomationService` to a
runner picking up a generated task. Produced by 9 mappers, 2 empirical harnesses
(in-process wiring of `EventService.Ingest → realtime.EventHub →
AutomationService.Start`), a coverage-matrix builder, 8 dimension auditors, and a
3-verdict adversarial verification pass.

**Commit audited:** `7b9286b` (main).
**Date:** 2026-08-07.

---

## 0. Amendments since the audited commit

This document is a point-in-time audit. The findings below were true at
`7b9286b`. Several have since been fixed, so the affected claims are marked
inline with **AMENDED** and the original wording is preserved next to the
correction — a reader needs to know both what was broken and that it no longer
is.

| Finding at `7b9286b` | Status now | Where |
|---|---|---|
| `webhook.received` absent from `AllEventTypes`, so `trigger.type: webhook` is structurally unreachable | **Fixed** — registered. Still no *unauthenticated inbound receiver*: an integration POSTs the event to `/api/v1/events` like any other. | §1, §3, §5.4, M-1 |
| `action.type: update` degrades to an empty prompt task | **Fixed** — implemented in process as `applyUpdateAction` (`automation_service.go:936`) | §1 |
| `entry.created` never sets `TaskID`, so `once_per: task_id` collapses to one constant key | **Fixed** — `evt.TaskID = resp.ID` (`entries.go:213`) | §3 |
| `MatchFilterValue` has exactly three forms, none able to test membership of a multi-valued actual | **Extended** — a fourth form, `has:`, added | §6.2 |
| No attachment events exist | **Fixed** — `attachment.created` and `entry.attachment_added` added | §3 |

Events added since the audit and absent from the §3 inventory:
`attachment.created`, `entry.attachment_added`, `reminder.fired`,
`control.exec_started`, plus `webhook.received` now being registered.

**Not fixed, still true:** `action.type: http` has no dispatch branch; the
`retry` block, `action.timeout` and `requires_capability` have no live readers;
`ignore_automation_events` has no live reader in the production matcher;
`feature.all_completed` does not exist on the live bus and the shipped
`feature-review.md` asset that subscribes to it remains inert; events are not
persisted; non-matches are logged nowhere.

---

## 1. Verdict

Event-driven automation **works**. The core dispatch path was verified end to
end: an ingested `task.completed` event produced exactly one generated task with
a template-rendered prompt, and a project-scoped cron automation produced exactly
one task per minute bucket with the correct dedup key. The machinery is not
broken. What is broken is almost everything *around* it — the configuration
surface advertises roughly twice the capability it implements. Of four trigger
types, one (`event`) is fully wired, one (`cron`) works but is not event-driven,
one (`session`) only fires under a single undocumented scope shape, and one
(`webhook`) is structurally unreachable — the event type it matches on is not in
`types.AllEventTypes`, so `Ingest` rejects it, and no inbound receiver route
exists. Of four action types, two (`prompt`, `script`) work and two (`update`,
`http`) are declared, accepted, persisted, displayed — and silently degrade to a
prompt task with an empty prompt.

> **AMENDED.** Two of those claims no longer hold. `webhook.received` is now in
> `types.AllEventTypes`, so a webhook automation *can* fire — though still only
> when something POSTs the event to `/api/v1/events`; there is still no
> unauthenticated inbound receiver route. And `action.type: update` is now
> implemented in process (`applyUpdateAction`), so only `http` still degrades to
> an empty prompt task. See §0.

The `retry` block, `action.timeout`, and
`requires_capability` are round-tripped through every layer and read by nothing.
`ignore_automation_events` is documented in the README as a default-on loop
guard and has zero live readers, so a `task.status_changed` automation re-fires
on its own generated tasks with no bound.

The two most damaging defects are that the **shipped built-in feature-checkout
automations cannot fire at all** (they are registered global with no project
wildcard, and `globalAutomationMatchesProjectEvent` rejects a global automation
for any event carrying a `ProjectID` — probe: 0 matches, 0 tasks, both modes),
and that **`checkout_mode` never survives storage** (two missing field copies), so
the documented `simple` checkout fold is unreachable and always resolves to
`ai`. The third is `once_per` with a time bucket (`day`, `5m`, `hour`) producing
a constant key — the pattern the shipped `brain-automation` SKILL.md teaches —
which makes such an automation fire exactly once, ever.

Underneath, there are two real concurrency defects in the realtime layer
(`realtime.Hub.publish` ranges a map outside the lock; `EventHub.Publish` can
send on a channel closed by a concurrent unsubscribe), both reproduced. Neither
is likely per-operation, but the first is an unrecoverable `fatal error` that
kills the process.

If you fix nothing else, fix: the built-in checkout scope, the `checkout_mode`
read path, `once_per` time buckets, and delete-or-implement the four inert
config blocks. That converts a subsystem that *looks* half-broken into one that
is honest about what it does.

---

## 2. How it works today

### 2.1 The dispatch path, end to end

```
                         ┌─────────────────────────────────────────┐
   RUNNER SIDE           │              API SIDE                   │
                         └─────────────────────────────────────────┘

  RunnerEvent{Type,...}
  internal/runner/*.go
        │
        │ emitEvent() → OnEvent handlers
        ├──────────────────────────► local hooks / TUI (never leaves process)
        │
        ▼
  EventForwarder.Handle
  event_forwarder.go:109
   (batch 10 / flush 2s)
        │
        │ RunnerEvent.ToEvent()   event_conversion.go:44
        │   maps RunnerEventType → types.Event.Type via runnerEventTypeMap
        │   fallback: "runner." + string(type)   (:115)
        ▼
  POST /api/v1/events ───────────► api.HandleIngestEvents (router.go:246)
                                              │
   ┌── API-native emitters ───────────────────┤
    │   entries.go:209/495/511/606/622/677/798 │
    │   tasks.go:335/395/836/895/916/977/992   │
    │   control.go:275/301/374/428/656         │
    │   attachments.go (attachment.* — added   │
    │     after this audit)                    │
    │   event_service.go:309/316 (direct pub)  │
   └──────────────────────────────────────────┤
                                              ▼
                              EventServiceImpl.Ingest
                              internal/service/event_service.go:77
                                │
                                ├─ IsValidEventType(evt.Type)?  ── no ──► 400,
                                │     WHOLE BATCH rejected mid-loop (:87)
                                ├─ source ∈ {runner, api}?      ── no ──► 400
                                ├─ dedup on evt.ID (seenIDs, unbounded map :47)
                                ├─ assign ID + timestamp
                                ▼
                        realtime.EventHub.Publish
                        internal/realtime/event_hub.go:64
                                │
                                ├─ append to 1000-slot ring buffer (:68)
                                │    ← the ONLY event storage. Not persisted.
                                │
                                └─ fan out to subscribers, 64-slot chans,
                                   DROP SILENTLY on full (:84-88)
                                        │
        ┌───────────────┬───────────────┼──────────────┬─────────────────┐
        ▼               ▼               ▼              ▼                 ▼
  AutomationService  GoalService  TriggerDispatcher  WebhookDispatcher  FeatureCascade
   .Start (:75)      .Start        (task-level        (outbound)        (type-filtered)
        │                           triggers)
        │  select { ticker.C (1 min) | ch }   ← SAME goroutine
        │      ├── ticker → CheckScheduled(now)      [cron path]
        │      └── event  → HandleEvent(evt)         [event path]
        ▼
  AutomationService.HandleEvent   automation_service.go:173
        │
        ├─ brain.List{Type:"automation", Status:"active", Limit:1000}   (:178)
        │     ← NOT filtered by project; NOT filtered by generated_by
        │
        └─ for each automation:
              automationMatchesEvent(automation, evt)      (:241)
                 │
                 ├─ Trigger==nil || Action==nil            → false
                 ├─ switch Trigger.Type
                 │    "event"   → automationMatchesNamedEvent   (:257)
                 │    "webhook" → automationMatchesWebhook      (:272) [dead]
                 │    "session" → automationMatchesSession      (:295)
                 │    default   → false, silently              (:252)
                 │
                 └─ automationMatchesNamedEvent:
                      1. globalAutomationMatchesProjectEvent    (:310)
                         global automation + project event ⇒ requires
                         Filter["project"]=="*" or ["project_id"]=="*"
                      2. Trigger.MatchesEvent(evt.Type)
                         unions Trigger.Event + Trigger.Events
                         patterns: exact | "ns.*" | "*"
                      3. automation.ProjectID != "" && != evt.ProjectID
                         ⇒ requires the same "*" escape hatch
                      4. matchAutomationFilters(Trigger.Filter, evt)  (:320)
                             per key: getEventField(evt, key)
                             vs types.MatchFilterValue(actual, expr)
                 │
                 ├─ no match → continue.  NO audit. NO log. NO counter.
                 │
                 ▼  match
              isAutomationPaused(project)?  (:217) → run audit "skipped", continue
                 │
                 ▼
              createTask(automation, evt, "")   (:342)
                 │
                 ├─ project := automation.ProjectID, else evt.ProjectID  (:343)
                 ├─ shouldSkipTaskGeneration → max_concurrent / cooldown (:347)
                 │     both List(type=task, Limit:1000, modified DESC)
                 ├─ automationGeneratedKey(automation, evt)              (:364)
                 │     "" when Trigger.OncePer unset ⇒ dedup SKIPPED entirely
                 │     else "automation:<id>:" + getEventField(evt, OncePer)
                 ├─ generatedTaskExists(project, key)                    (:369)
                 │     List(type=task, Limit:1000) + linear scan
                 ├─ renderAutomationTemplate(DirectPrompt/Command)       (:467)
                 ├─ if Action.Type == "script": Executor="script",
                 │     ExecutionMode default "current_branch", workdir "/tmp"
                 └─ brain.Save(CreateEntryRequest{Type:"task", Status:"pending",
                       GeneratedBy:"automation:<id>", GeneratedKey:key, ...})
                 │
                 └─ createRunAudit(status:"queued")                      (:435)
                       (only ever "queued" or "skipped" — never completed/failed)
                 │
                 │  ⚠ FIRST ERROR ⇒ `return err` (:209), skipping every
                 │    LATER automation for this event; Start discards it (:94)
                 ▼
        task entry saved to projects/<p>/task/<id>.md, status=pending
                 │
                 ▼
        SchedulerService dispatch / runner poll → claim → spawn executor
                 │
                 ▼
        Runner status PATCH → entries.go:622 emits task.status_changed
                 │
                 └──────────► BACK INTO THE HUB (no loop guard)
```

### 2.2 Things a new contributor must know

- **Events are not persisted.** The only store is the 1000-slot in-memory ring
  in `realtime.EventHub` (`event_hub.go:10`). `EventServiceImpl` has no storage
  handle. A restart loses everything; `hub.Replay("")` at
  `automation_service.go:97` is always empty on a cold boot. The single
  exception is `goal.reconcile`, written to the SQLite `event_log` table and
  deliberately excluded from `AllEventTypes` so it cannot loop back.
- **`internal/events` is dead code.** The `DedupBus`, `AutomationMatcher`,
  `event_log` replay machinery, and template resolver in that package are fully
  implemented and never constructed in production —
  `internal/apiserver/server.go:300` passes a `nil` bus to `NewBrainService`,
  and `NewAutomationMatcher` has only test callers. Do not read
  `internal/events/matcher.go` to understand live matching; read
  `internal/service/automation_service.go`.
- **Two dispatchers share the shape but not the semantics.** `AutomationService`
  handles `type=automation` entries. `TriggerService` handles `type=task`
  entries carrying a `trigger` block; it applies no project scoping, keeps
  cooldowns in memory only, and its SQL pre-filter requires a non-empty
  `$.trigger.event`, so a task trigger using only `events: []` is never loaded.
- **There is no automation CRUD API.** `internal/api/automations.go` is 46 lines
  and exposes only `POST /automations/run`. Creating and editing automations
  goes through the generic `/entries` endpoints. Only MCP `save`/`update` and
  the `brain automation create` wizard can create an arbitrary event automation,
  and the wizard cannot set `events`, `timezone`, `cooldown`, `max_concurrent`,
  or `filter` (it builds a `types.AutomationTrigger`, which lacks those fields).
- **`POST /api/v1/events` has no scope requirement.** The `/events` routes sit
  inside the Auth group but outside any `RequireScope` group
  (`internal/api/router.go:244-254`), unlike `/entries` writes which require
  `admin:*`. Any authenticated token, including a read-only one, can inject
  arbitrary domain events and thereby drive automation task creation — including
  `script`-executor tasks that shell out.

---

## 3. Event coverage matrix

Legend: **Hub** = reaches `realtime.EventHub` in production. **Auto** = can
trigger an automation (`Y` = yes; `C` = conditional, see caveat; `N` = no).
Rows marked ✅ were empirically probed.

| Event | Emitted from | Hub | Auto | Filterable fields | Caveats |
|---|---|---|---|---|---|
| ✅ `task.status_changed` | API `entries.go:511`, `:622`; `tasks.go:916`, `:992`; runner `runner.go:1579/1603/1637/2438` | yes | Y | `project_id`, `task_id`, `from_status`, `to_status`, `type`, `source`, `project`; `feature_id`/`runner_id` on some emitters | Highest-value trigger. **Fires TWICE** per runner completion (runner pre-update + API PATCH) with different IDs, so no-`once_per` automations double-fire. `TaskTitle`/`TaskPath` are set but **not filterable**. Main self-trigger loop vector — no loop guard. |
| ✅ `task.completed` | Runner only: `runner.go:2488`, `idle_detection.go:243/299`, `schedule.go:403` | yes | Y | `task_id`, `source`, `runner_id`, `type`; `project_id`/`feature_id` **only** from `runner.go:2488` | Field sparsity differs by emitter — idle-detection and schedule-gate completions carry no `project_id`, so a project-scoped automation silently misses them. Also consumed by `FeatureCascadeService`. |
| ✅ `feature.completed` | API `event_service.go:304` (direct publish, bypasses Ingest); runner `feature_tracker.go:209` | yes | **C** | `project_id`, `feature_id`, `task_id`, `source`, `type`; metadata `completed`, `total`, `checkout_mode` (API path only) | **Dead for the built-ins** (both registered global with no project wildcard — probe: 0 matches, 0 tasks, both modes). Runner path carries no `checkout_mode`, so it defaults to `"ai"`. User-created project-scoped automations DO work. |
| `feature.progress` | API `event_service.go:311`; runner `feature_tracker.go:216` | yes | Y | `project_id`, `feature_id`, `task_id`, `source`, `type`; metadata varies by emitter | Runner metadata is misleading: `running_count` actually carries the completed count. No `ToStatus`, so `to_status` filters never match. |
| `feature.started` | Runner `feature_tracker.go:157` | yes | Y | `project_id`, `feature_id`, `source`, `runner_id`, `type`; metadata `ready_count` | No `task_id`. Runner-only, so a pure-API workflow never sees it. |
| `feature.blocked` | Runner `feature_tracker.go:238` | yes | Y | `project_id`, `feature_id`, `source`, `runner_id`, `type` | Runner-derived only; no API equivalent; no metadata. |
| ~~`feature.enabled`~~ | **REMOVED** | — | — | — | Removed 2026-08-26 along with the inert per-runner feature-toggle surface (`POST /runners/{id}/features/{fid}/toggle`). No emitter remains and the type is no longer in `AllEventTypes`. Use `POST /tasks/{project}/features/{fid}/run` to run one feature while a project is paused. |
| ~~`feature.disabled`~~ | **REMOVED** | — | — | — | Same removal. |
| `task.claimed` | API `tasks.go:335`; runner `runner.go:1539` | yes | Y | API: `project_id`, `task_id`, `source`, `type` + metadata `runner_id`. Runner: adds `feature_id`, `runner_id`, drops metadata | Fires TWICE per claim with different sources and different field sets. Hub dedupes by ID only. |
| ✅ `task.released` | API `tasks.go:395`; runner `runner.go:1612/1646/1696/2545`, `bridge_client.go:687` | yes | Y | `project_id`, `task_id`, `source`, `type`; `feature_id` on runner sites; `reason` **only via the Metadata fallback** | The top-level `evt.Reason` field has no `getEventField` case. `reason` works only because both emitters mirror it into `Metadata` — a convention, not enforced. API path sets no `feature_id`. |
| `task.claim_rejected` | Runner `runner.go:1523` | yes | Y | `project_id`, `task_id`, `feature_id`, `source`, `runner_id`, `type`; metadata `claimed_by` | Runner-only origin. |
| `task.started` | Runner `runner.go:1792`, `execute.go:125` | yes | Y | `project_id`, `task_id`, `feature_id`, `source`, `runner_id`, `type` | `TaskTitle`/`TaskPath` populated but not filterable. Also used as a hook-only synthetic type at `runner.go:1595` / `execute.go:42` — those never reach the hub. |
| `task.failed` | Runner `runner.go:2488`, `idle_detection.go:243/336` | yes | Y | `task_id`, `source`, `runner_id`, `type`; `project_id`/`feature_id` only on `runner.go:2488` | **`CompletionBlocked` maps here, NOT to `task.blocked`** (`runner.go:2431-2433`). `idle_detection.go:336` sets only `TaskID`. |
| `task.cancelled` | Runner `runner.go:2488`, `schedule.go:309` | yes | Y | `task_id`, `source`, `runner_id`, `type`; `project_id`/`feature_id` on runner path; metadata `reason` on schedule path | Also consumed by `FeatureCascadeService`. |
| `task.triggered` | API `tasks.go:836`, `:1060` | yes | Y | `project_id`, `task_id`, `source`, `type` | No `feature_id`, no statuses, no metadata. |
| `task.resume_requested` | API `tasks.go:895`, `:977` | yes | Y | `project_id`, `task_id`, `source`, `type`; metadata `abandon_reason`, `prior_status` when non-empty | **`FeatureID` is NEVER set** — not even in the feature fan-out where the feature id is in the URL. A `feature_id` filter can never match; `once_per: feature_id` degenerates to a constant key. |
| ✅ `task.blocked` | **NOWHERE.** Declared `events.go:50`, in `AllEventTypes` | conditional | **C** | n/a in practice | **DEAD CONSTANT.** Zero emitters. `Ingest` accepts it, so a hand-crafted POST reaches the hub. Working idiom is `task.status_changed` + `filter: {to_status: blocked}`. |
| ✅ `task.idle_detected` | **NOWHERE.** Declared `events.go:55` | conditional | **C** | n/a | **DEAD CONSTANT.** `idle_detection.go` emits `task.completed` or `task.failed` instead. |
| ✅ `project.started` | **NOWHERE.** Declared `events.go:40` | conditional | **C** | n/a | **DEAD CONSTANT.** No `runnerEventTypeMap` entry either. |
| `project.paused` | Runner `runner.go:2807`, `:2791` | yes | Y | `project_id`, `source`, `runner_id`, `type` | Server-initiated pause emits no domain event directly; the runner emits it after applying the SSE command. |
| `project.resumed` | Runner `runner.go:2819`, `:2793` | yes | Y | same | Same as above. |
| ✅ `runner.started` | Runner `runner.go:449` | yes | **C** | `source`, `runner_id`, `type`; metadata `projects`, `mode` | `ProjectID`/`TaskID`/`FeatureID` all EMPTY ⇒ global-no-filter automations only. A second emission at `runner.go:461` posts to `POST /api/v1/events/emit`, **a route that does not exist**; the error is discarded. |
| `runner.stopped` | Runner `runner.go:1190`, `:1137` | yes | **C** | `source`, `runner_id`, `type`; metadata `reason` | No `project_id` — global only. |
| `runner.poll_complete` | Runner `runner.go:3102` | yes | **C** | `source`, `runner_id`, `type`; metadata `running_count` when >0 | **Highest-volume event on the hub** — every poll interval per runner. Main driver of hub backpressure, since `AutomationService` does a `List(limit:1000)` per event on a 64-slot drop-on-full channel. Feeds three unbounded dedup maps. |
| ✅ `runner.state_saved` | **NOWHERE in production.** Mapped at `event_conversion.go:27` but no `RunnerEvent` literal exists | conditional | **C** | n/a | **DEAD.** `saveState()` emits nothing. |
| `runner.all_paused` | Runner `runner.go:2831`, `:2787` | yes | **C** | `source`, `runner_id`, `type` | Global only. |
| `runner.all_resumed` | Runner `runner.go:2842`, `:2789` | yes | **C** | `source`, `runner_id`, `type` | Global only. |
| ✅ `runner.session_discovered` | Runner `runner.go:1921` | yes | **C** | `source`, `runner_id`, `type`, `session` (→`metadata.session_id`), `session_id` | The **only** event a `trigger.type: session` automation can match. Probe truth table: global+no filter = MATCH; global+`project:"*"` = NO MATCH; project-scoped = NO MATCH under any filter. |
| `entry.created` | API only `entries.go:209` *(was `:196`)* | yes | Y | `project_id`, `source`, `type`, **`task_id`**; `feature_id` when set; metadata `entry_type`, `title`, **`entry_id`**, **`tags`**, **`has_attachment`**, **`attachment_media_types`** | **AMENDED:** `TaskID` is now set to the created entry's id, so `once_per: task_id` produces distinct keys and `{{.TaskID}}` renders. *(Originally: "`TaskID` is never set" — while that held, every created entry produced the same dedup key and such an automation fired exactly once, ever.)* `tags` are the **persisted** tags — sanitized, deduplicated, entry type appended — read from `CreateEntryResponse.Tags`, not the request. Filter them with `has:` (§6.2); an exact match would require listing every tag in order. **Still true:** entries created through internal service paths — `AutomationService.createTask`, `GoalService.generateGoalTask`, the scheduler, monitors — emit **nothing**, so automation-generated entries remain invisible to `entry.*` triggers. |
| `attachment.created` | API `attachments.go` `HandleCreateAttachment` | yes | Y | `project_id`, `source`, `type`; metadata `attachment_id`, `media_type`, `filename`, `size_bytes` | Added after the audit. Fires when blob content is stored, **before** any entry references it. Carries no entry id — pair it with `entry.attachment_added` if you need one. |
| `entry.attachment_added` | API `attachments.go` `HandleAttachEntryAttachment` | yes | Y | `project_id`, `source`, `type`, `task_id` (the ENTRY id), `task_path`; metadata `attachment_id`, `media_type`, `role` | Added after the audit. This is the event an image-delivery integration should trigger on: unlike `entry.created`, the attachment is linked by the time it fires. No `entry.attachment_removed` counterpart — `Detach` returns 200 even when nothing was unlinked, so a naive emitter would fire spurious removals. |
| `reminder.fired` | `internal/service/reminder_service.go` sweeper | yes | Y | per reminder | Added after the audit; in `AllEventTypes`. Not analysed by this audit. |
| `control.exec_started` | API `control.go:656` | yes | **C** | `source`, `runner_id`, `type` | Added after the audit; in `AllEventTypes`. Same project-blindness as the other `control.*` events. |
| `entry.updated` | API only, 4 sites: `entries.go:495`, `:606`, `:798`, `:986` | yes | Y | `project_id`, `source`, `type` always; `task_id`+`feature_id` on `:495`/`:606` only; metadata varies per site | Bulk and move paths set neither `task_id` nor `feature_id`. Same internal-path blind spot. |
| `entry.deleted` | API only `entries.go:677` | yes | Y | `project_id`, `source`, `type`; `task_id`+`feature_id`+metadata only when the pre-delete Recall succeeded | — |
| `control.prompt_sent` | API `control.go:275` via `emitControlAudit` | yes | **C** | `source`, `runner_id`, `type`; metadata `instance_id`, `actor`, `auth_type`, `session_id` | `ProjectID`/`TaskID`/`FeatureID` all EMPTY ⇒ unroutable by project-scoped automations. |
| `control.permission_responded` | API `control.go:301` | yes | **C** | same + `permission_id` | Same. |
| `control.instance_spawned` | API `control.go:374` | yes | **C** | same + `workdir` | Same. |
| `control.instance_killed` | API `control.go:428` | yes | **C** | `source`, `runner_id`, `type`; metadata `instance_id`, `actor`, `auth_type` | Same. |
| ✅ `webhook.received` | No *producer* in this tree; an external integration POSTs it to `/api/v1/events` | **yes** *(was no)* | Y | `project_id`, `source`, `type`; metadata `webhook_path` | **AMENDED:** now declared in `internal/types/events.go` and present in `AllEventTypes`, so `Ingest` accepts it and `trigger.type: webhook` is reachable. *(Originally: "NOT in `AllEventTypes`… structurally unreachable" — `Ingest` returned `invalid event type "webhook.received" at index 0`.)* There is still **no unauthenticated inbound receiver route**; `router.go` webhook routes remain outbound-only. The `normalizeWebhookPath` fail-open below is therefore now live, not hypothetical. |
| ✅ `goal.reconcile` | `goal_service.go:219` via `store.InsertEvent` | **no** | **N** | n/a | The **ONLY persisted event type** — SQLite `event_log`. Deliberately excluded from `AllEventTypes` so it cannot loop back. No retention/pruning. Read only by `GoalAuditHistory`, hard-capped at 1000 rows then filtered in memory. |
| ✅ `manual` | Constructed as a literal in `automation_service.go:56-61` by `RunAutomationNow` | **no** | **C** | `project_id`, `source`, `type` | Pseudo-event. `POST /automations/run` works and creates a task, but it **bypasses `automationMatchesEvent` entirely** — the automation's own trigger is not consulted and the pause gate is deliberately skipped. `trigger: {event: manual}` on another automation can never match. |
| `webhook.test` | `webhooks.go:191` | **no** | **N** | n/a | Pseudo-event for the outbound test endpoint; delivered directly, never published. |
| ✅ `feature.all_completed` | Only the dead `internal/events` bus, published by `brain.go:90` — a **no-op** (nil bus at `apiserver/server.go:300`) | **no** | **N** | n/a | Probe: `IsValidEventType == false`, `Ingest` rejects. **The shipped `cmd/brain/assets/automations/feature-review.md:11` subscribes to this**, so that automation is inert as shipped. `README.md` and `internal/mcp/task_tools.go:1033` still advertise it. Live constant is `feature.completed`. |
| `runner.first_task_today` | `runner.go:1561` via `POST /api/v1/events/emit` | **no** | **N** | n/a | That route is **not registered** (`router.go:244-253`). Always 404s; error discarded with `_ =` inside a goroutine. Not in `AllEventTypes` either. |
| `runner.<unmapped>` (latent) | `event_conversion.go:115-121` fallback `"runner."+type` | **no** | **N** | n/a | All 22 current `RunnerEventType` constants are mapped, so latent. If one is added without a map entry, `Ingest` rejects the **whole batch** mid-loop and `EventForwarder` permanently drops every event at and after the bad index after 3 retries. |

---

## 4. Trigger types

### `event` — fully wired ✅

The only fully functional trigger type. Dispatch:
`automationMatchesEvent` → `automationMatchesNamedEvent`
(`automation_service.go:241`, `:257`). Supports multi-event OR via
`Trigger.Event` + `Trigger.Events` (unioned by `TriggerConfig.EventPatterns()`,
`events.go:226`) with patterns `exact` / `ns.*` / `*` (`MatchEventPattern`,
`events.go:405`).

**Verified end to end.** `EventService.Ingest → realtime.EventHub →
AutomationService.Start` wired in-process, `task.completed` POSTed, exactly one
generated task produced with the template-rendered prompt.

Caveats:

- **Project scoping is a three-stage gate** with counter-intuitive semantics.
  Verified truth table:

  | Automation scope | Filter | Project-less event | Project-carrying event |
  |---|---|---|---|
  | global (`ProjectID:""`) | none | **MATCH** | no match |
  | global | `project: "*"` | no match | **MATCH** |
  | project-scoped `P` | none | no match | MATCH only if `evt.ProjectID == P` |
  | project-scoped `Q` | `project: "*"` | no match | **MATCH** — and the task is created under `Q`, not the event's project |

  `MatchFilterValue("", "*")` is `false` (`*` means "non-empty", not "any"), so
  there is **no way to express "all projects including project-less events"**.
- Only the literal `"*"` satisfies the wildcard escape hatch. `filter:
  {project: "in:P,Q"}` — which reads as explicit multi-project intent — is
  rejected by the scope guard before the filter loop runs, because both guards
  test literal equality to `"*"` (`automation_service.go:265/280/303/317`).
- **No self-trigger guard.** `Trigger.IgnoreAutomationEvents` is round-tripped
  everywhere; its only reader is the dead `internal/events/matcher.go:171`.

### `cron` — fully wired, but NOT event-driven ⚠️

Handled exclusively by `AutomationService.CheckScheduled` on a 1-minute ticker
(`automation_service.go:82 → :117`), plus one synchronous call at `Start` (`:84`).
`automationMatchesEvent` returns `false` for `Trigger.Type=="cron"` via the
default branch (`:252-254`), so **a cron automation can never fire from an
event**.

**Verified.** A project-scoped cron automation with schedule `* * * * *` created
exactly one task with `generated_key = automation:cron:<id>:202601020304` and
prompt `"tick P"`.

Caveats:

- **`Trigger.OncePer` is IGNORED** — the dedup key is force-overridden to a
  minute bucket at `:163`.
- **A Global cron automation loses its project.** `CheckScheduled` passes
  `types.Event{ProjectID: automation.ProjectID}` == `""`, so the task is saved
  under `projects/default` (`brain.go:146-148`) and `{{.Project}}` renders
  EMPTY. Verified: the global cron produced prompt `"global tick []"` at
  `projects/default/task/*.md`. **This is exactly the shape of the shipped
  `dream-consolidation.md` asset**, whose prompt references `{{.Project}}` 15+
  times.
- `Trigger.Timezone` is honored (`cron.LoadTimezone`, `:158`) but the CLI wizard
  cannot set it (it builds a `types.AutomationTrigger`, which has no `Timezone`
  field), and an invalid zone silently falls back to UTC.
- `pkg/cron.Parse` accepts `0 9 * * 1-5` but **rejects** `0 9 * * MON` and
  `@daily`. A parse error is swallowed with a bare `continue` (`:152-155`) — no
  log, no audit, and `internal/service/automation_service.go` imports no logging
  package at all.

### `session` — partially wired ⚠️

Reachable but severely constrained. `automationMatchesSession`
(`automation_service.go:295-308`) **hard-requires** `evt.Type ==
types.EventRunnerSessionDiscovered` and **ignores `Trigger.Event`/`Events`
entirely** — a session automation declaring `event: task.completed` still only
sees `runner.session_discovered`. The producer (`runner.go:1921`) sets neither
`ProjectID` nor `TaskID`.

Verified truth table:

| Automation scope | Filter | Result |
|---|---|---|
| global | none | **MATCH** |
| global | `project: "*"` | no match |
| project-scoped | none | no match |
| project-scoped | `project: "*"` | no match |

So a session automation **only works if declared global with no project
filter**. The working filter key is `session` (→ `Metadata["session_id"]`);
`session_id` also resolves via the metadata fallback. `createRunAudit`
(`:536-539`) reports `Trigger.Event` as the `trigger_event`, so the audit trail
claims a match rule the matcher never consulted.

### `webhook` — ~~broken / structurally unreachable ❌~~ → reachable, with one live fail-open ⚠️ **AMENDED**

> **Original finding, no longer true:** `types.IsValidEventType("webhook.received")`
> returned `false` and `EventService.Ingest` rejected the event with ``invalid
> event type "webhook.received" at index 0``, so **a webhook automation could
> never fire under any input**. The only definition of the string was the dead
> `internal/events/types.go:38`.

`webhook.received` is now declared in `internal/types/events.go` and listed in
`AllEventTypes`, so `Ingest` accepts it. `automationMatchesWebhook` requires
`evt.Type == "webhook.received"` and compares
`normalizeWebhookPath(evt.Metadata["webhook_path"])` against
`normalizeWebhookPath(trigger.webhook)`.

**What is still missing:** there is no inbound webhook receiver route. The
`router.go` webhook routes remain outbound CRUD/test/deliveries only. A webhook
automation fires only when something POSTs

```json
{"type":"webhook.received","source":"api","project_id":"…",
 "metadata":{"webhook_path":"…"}}
```

to `/api/v1/events` **with a valid token**. Note that `/events` carries no
`RequireScope`, so any authenticated token — including a read-only one — can
inject this and drive task creation.

The trigger type remains offered as option 3 by the CLI wizard
(`cmd/brain/commands/automation.go:121`), documented at `README.md:844`, listed
in `cmd/brain/help.go`, taught in `brain-automation/SKILL.md:233`, and exposed in
the MCP `save` trigger schema (`internal/mcp/brain_tools.go:96`, `:1101`) — all
of which are now accurate rather than advertising a dead feature.

⚠️ **The fail-open is now live, not latent.** `normalizeWebhookPath` is
`strings.Trim(path, "/")` only, so an **empty** `Trigger.Webhook` matches any
`webhook.received` lacking `webhook_path` metadata. While the event type was
unreachable this could not be reached; it can be now. *(Contrast the `has:`
filter added in §6.2, which deliberately fails closed on an empty operand.)*

### Any other value (`""`, `"Event"`, `"EVENT"`, typos) — silently inert ❌

`automationMatchesEvent`'s default branch returns `false`
(`automation_service.go:252-254`). **Verified:** `type: "Event"`, `"EVENT"`, and
`""` all matched nothing against `task.completed`,
`runner.session_discovered`, or `webhook.received`.

There is **no server-side validation of `trigger.type`** at create or update
time. The automation is stored, listed, shown in the TUI/PWA, and never fires.
Note the inconsistency: `TriggerConfig.EventPatterns()` calls `strings.TrimSpace`
on each event pattern (so `" task.completed"` DOES match), while `Type` is
compared as an exact, case-sensitive, untrimmed literal.

---

## 5. Action types

Dispatch lives entirely in `AutomationService.createTask`
(`internal/service/automation_service.go:342-453`). There is exactly **one**
runtime branch — `script` — and everything else falls through to the prompt
path. All four rows below were empirically produced and inspected.

| `action.type` | Implemented | What actually runs |
|---|---|---|
| `prompt` | ✅ yes | Default fall-through (`:388-412`) |
| `script` | ✅ yes | The only real branch (`:414-425`) |
| `shell` | ✅ yes (alias) | Normalized to `script` before dispatch |
| `update` | ❌ **NO** | Silently becomes a prompt task |
| `http` | ❌ **NO** | Silently becomes a prompt task |
| anything else / `""` | ❌ **NO** | Silently becomes a prompt task |

### `prompt` ✅

Builds a `type=task` entry with `Content`/`DirectPrompt =
renderAutomationTemplate(Action.DirectPrompt)`, `Status=pending`, and
`firstNonEmpty` precedence *entry-level > action-level* for `Agent`, `Model`,
`Executor`, `ExecutionMode`, `TargetWorkdir`. `CompleteOnIdle` defaults to
**true** when unset (`:455-461`).

Verified output: `executor=""`, `execution_mode=""`,
`direct_prompt="PROMPT-prompt"`.

Caveats: `SessionMode` is taken **only** from `Action.SessionMode` (`:409`) —
no entry-level fallback, asymmetric with the other five fields — and it is inert
downstream anyway (finding H-9). `renderAutomationTemplate` (`:467-505`)
silently returns the **raw un-rendered input** on either a parse or an execute
error, with no log.

### `script` ✅

Sets `Executor="script"`, overwrites `Content` and `DirectPrompt` with the
rendered `Action.Command`, and defaults `ExecutionMode` to `"current_branch"`
and `TargetWorkdir` to `"/tmp"` when still empty.

Verified output: `executor="script"`, `execmode="current_branch"`,
`workdir="/tmp"`, `direct_prompt="echo CMD-script"`.

Runner-side caveats, all sharp:

- The `script` executor is registered **only** when `cfg.Script.Enabled`
  (`internal/runner/executor_factory.go:49-51`, default `false` via
  `RUNNER_SCRIPT_ENABLED`), and `filterByExecutors`
  (`internal/service/task_filter.go:30-49`) drops tasks whose executor is not in
  the runner's advertised list. **With scripts disabled the task is never even
  fetched** and sits `pending` forever with no error surfaced anywhere.
- Timeout comes from the runner's global `Script.MaxTimeout`, **not**
  `AutomationAction.Timeout` (which is inert — finding M-8).
- **Exit code is ignored for task status** when `CompleteOnIdle` is true (the
  automation default), so a failing script yields a `completed` task (finding
  H-8).

### `shell` ✅ (undocumented alias)

Normalized to `script` by `types.NormalizeAutomationActionType`
(`internal/types/automation.go:27-35`) before the dispatch check. Verified: an
`action.type` of `"shell"` produced a byte-identical task shape to `"script"`.
Not documented outside the source comment; not offered by the CLI wizard.

### `update` ❌ — declared, accepted, does nothing

`types.AutomationActionUpdate` (`internal/types/automation.go:13`) has **zero
production readers**. `createTask` has no branch for it. Verified: an automation
with `action.type="update"` produced a **normal AI prompt task** with
`executor=""`, `execution_mode=""`, `target_workdir=""`, and
`direct_prompt="PROMPT-update"` — i.e. `Action.DirectPrompt`, which for a real
update action would be empty.

It is also **unimplementable with the current schema**: `AutomationAction` has
no `Target` or `Fields` member. The only `case "update"` in the repo is
`internal/events/matcher.go:295-302`, whose `AutomationMatcher` is never
constructed in production — and even that `dispatchUpdate` builds an empty
`types.UpdateEntryRequest{}`. No validation rejects it; no log or audit records
the degradation.

### `http` ❌ — declared, accepted, does nothing

`types.AutomationActionHTTP` (`internal/types/automation.go:14`) has **zero
production readers**. Verified: `action.type="http"` produced a normal prompt
task with `executor=""`, `direct_prompt="PROMPT-http"`.

Also unimplementable: `AutomationAction` has no `URL`, `Method`, `Headers`, or
`Body` field, and neither does the frontmatter mirror
(`pkg/frontmatter/frontmatter.go:113-126`).

The CLI wizard offers only `prompt` and `script`
(`cmd/brain/commands/automation.go:178-181`) — which matches what actually
works, not what the type comment at `internal/types/automation.go:54`
advertises.

---

## 6. Filter grammar reference

**This is the section to hand to anyone authoring an automation.**

### 6.1 Event pattern matching (`trigger.event` / `trigger.events`)

`TriggerConfig.MatchesEvent` unions `Trigger.Event` and `Trigger.Events` via
`EventPatterns()` (`internal/types/events.go:224-246`, each entry
`strings.TrimSpace`d) and ORs them through `MatchEventPattern`
(`internal/types/events.go:405-423`):

| Pattern | Semantics |
|---|---|
| `"task.started"` | Exact match. |
| `"task.*"` | Namespace wildcard — matches any type with the prefix `task.` |
| `"*"` | Matches any event type. |
| `""` | **Never** matches. An empty pattern and an empty event type both fail. |

### 6.2 Filter value expressions (`trigger.filter`)

Every value in the `filter` map is evaluated by `types.MatchFilterValue`
(`internal/types/events.go`). There are **four** forms *(was three — `has:` was
added after this audit)*:

| Expression | Semantics | Gotcha |
|---|---|---|
| `"*"` | `actual != ""` — i.e. **"field is present"**, not "any value" | `MatchFilterValue("", "*")` is **false**. This is why a global automation with `project: "*"` stops matching project-less events. |
| `"in:a,b,c"` | `actual` equals any member. Whitespace around members is trimmed; empty members ignored. | `"in:P,Q"` in a `project`/`project_id` filter does **not** satisfy the project scope guard, which tests literal equality to `"*"` before the filter loop runs. |
| `"has:x"` **(new)** | Splits the **actual** on commas and matches if any element equals `x` exactly. | Element-exact, **never substring**: `has:note` does *not* match an actual containing `supernote`. An empty operand (`"has:"`) fails **closed** — deliberately unlike `normalizeWebhookPath` (§5.4). |
| `"<value>"` | Exact string equality. | An unresolvable key yields `""`, and `"" == "<value>"` is false ⇒ the automation **silently never fires**. |

`in:` and `has:` are duals and neither can express the other: `in:` ORs over the
**filter's** values against a single actual, while `has:` tests membership within
a multi-valued **actual**. Before `has:` existed, comma-joined metadata such as
`entry.created`'s `tags` was effectively unfilterable — an exact match required
listing every tag in the same order.

There is still **no** support for negation, prefix/suffix globs inside a value,
numeric comparison, regex, or boolean composition. Filters are ANDed: every key
must match.

### 6.3 The COMPLETE set of resolvable filter keys

`getEventField` (`internal/service/trigger_service.go:224-253`) resolves exactly
**nine** keys, plus a metadata fallback. Anything else falls through to
`evt.Metadata[key]`.

| Key | Resolves to | Notes |
|---|---|---|
| `project_id` | `evt.ProjectID` | |
| `feature_id` | `evt.FeatureID` | Empty on many emitters — see the matrix. |
| `task_id` | `evt.TaskID` | |
| `source` | `evt.Source` | Only ever `"runner"` or `"api"`; `Ingest` rejects anything else. |
| `runner_id` | `evt.RunnerID` | |
| `session` | `evt.Metadata["session_id"]` | Note: reads **metadata**, not a struct field. |
| `from_status` | `evt.FromStatus` | |
| `to_status` | `evt.ToStatus` | |
| `type` | `evt.Type` | |
| *anything else* | `evt.Metadata[key]` | Returns `""` when `Metadata` is nil. |

**Two automation-only overrides** are applied in `matchAutomationFilters`
(`automation_service.go:320-340`) and exist **nowhere else** (notably not in
`TriggerService.matchTriggerFilters`, so the identical filter behaves differently
for task-level triggers):

- `project` → forced to `evt.ProjectID` (so `project` and `project_id` are
  synonyms for automations).
- `checkout_mode` → an empty resolved value is rewritten to `"ai"`. This is
  **not gated on event type or source**, so `filter: {checkout_mode: "ai"}`
  matches *any* event that lacks the key (finding M-3).

### 6.4 Keys that look like they should work but DO NOT

Verified against a fully-populated event — every one of these resolves to `""`:

| Key you might write | Result | Use instead |
|---|---|---|
| `status` | `""` | `to_status` |
| `task_title` | `""` (even though `evt.TaskTitle` is populated) | — no substitute |
| `task_path` | `""` (even though `evt.TaskPath` is populated) | — no substitute |
| `event` | `""` | `type` |
| `id` | `""` | — |
| `timestamp` | `""` | — |
| `reason` | `""` from the struct field | Works **by accident** only because both the API and runner emitters mirror it into `Metadata["reason"]`. A convention, not enforced. |

Because `MatchFilterValue("", "<literal>")` is `false`, **any of these with a
literal value makes the automation never fire, silently**.

### 6.5 `once_per` key resolution

`automationGeneratedKey` (`automation_service.go:725-730`) builds
`"automation:<id>:" + getEventField(evt, Trigger.OncePer)`. It goes through the
**same** `getEventField`, so:

- **Working values:** `feature_id`, `task_id`, `project_id`, `session`,
  `to_status`, `from_status`, `runner_id`, `source`, `type`, or any key actually
  present in `evt.Metadata`.
- **Broken values:** `day`, `week`, `hour`, `5m` and any other time bucket —
  there is no case for them, so the key is the **constant** `"automation:<id>:"`
  and the automation fires exactly **once, ever** (finding H-3).
- **Broken value:** `project` — the automation-only `project` override in
  `matchAutomationFilters` is **not** applied in `automationGeneratedKey`, so it
  is also constant-empty.

### 6.6 Template variables (prompt and script bodies)

`renderAutomationTemplate` (`automation_service.go:467-505`) binds a Go
`text/template` against an anonymous struct with exactly these fields:

`{{.Project}}`, `{{.ProjectID}}`, `{{.EventProjectID}}`, `{{.FeatureID}}`,
`{{.TaskID}}`, `{{.TaskPath}}`, `{{.TaskTitle}}`, `{{.FromStatus}}`,
`{{.ToStatus}}`.

Anything else — `{{.Status}}`, `{{.Reason}}`, `{{.Title}}`, `{{feature_id}}`,
`{{date}}` — is an error, and on error the function returns the **raw,
uninterpolated input string** with no log line and no audit note. One bad
placeholder makes **every** placeholder in that string literal (finding M-6).

---

## 7. Confirmed findings

### 7.A — Survived 3-verdict adversarial verification

Severities below reflect the **post-verification** rating; several original
claims were downgraded by the verifiers and are annotated accordingly.

---

#### C-1 · CRITICAL · `realtime.Hub.publish` ranges the subscriber map outside the lock

**`internal/realtime/hub.go:56-68`**

`publish` copies the inner subscriber map *reference* under `RLock`, releases
the lock, then ranges it. A concurrent `Subscribe` (`hub.go:35`) or unsub
`delete` (`hub.go:42`) triggers Go's `fatal error: concurrent map iteration and
map write` — a runtime **throw**, not a panic, so `recover()` cannot catch it
and `api.Recovery` is irrelevant. The whole `brain-api` process exits, taking
every runner dispatch, SSE stream and automation subscriber with it.

*Scenario:* any TUI/PWA client connecting or disconnecting on the global
`runners` topic (`internal/api/sse.go:56`, `:230`) while the background
lifecycle sweep publishes `runner_offline`
(`internal/service/runner_registry.go:424/:450`). A **single** subscriber
disconnecting suffices — `delete` on an empty map is still a map write.
Reproduced verbatim by two verifiers, with `-race` flagging `hub.go:35` vs
`hub.go:61`.

*Second failure in the same window:* unsub does `close(ch)` at `hub.go:47`
**after** the range has already yielded `ch` ⇒ `panic: send on closed channel`.

*Fix:* snapshot channels into a slice while still holding the `RLock`, then
iterate the slice. Separately, stop closing subscriber channels in unsub — use a
per-subscriber `done chan struct{}` that `publish` selects on. A slice snapshot
alone does **not** close the send-on-closed window.

---

#### C-2 · CRITICAL · The shipped built-in feature-checkout automations cannot fire

**`internal/service/builtin_feature_checkout.go:77`,
`builtin_feature_checkout_simple.go:79`**

Both built-ins are saved `Global: true` (path `global/automation/*.md`,
`ProjectID == ""`) with only `filter: {checkout_mode: ai|simple}` and **no
project wildcard**. `globalAutomationMatchesProjectEvent`
(`automation_service.go:310-318`) rejects a global automation for any event
carrying a `ProjectID`. Every `feature.completed` from `CheckFeatureCompletion`
carries one, and so does the runner's `FeatureTracker` copy.

*Probe:* for a `feature.completed` with `ProjectID="P"`, matches = **0** in both
modes and `AutomationService.HandleEvent` created **0** tasks. Independently
re-confirmed during adversarial verification of finding M-3 ("as-shipped global
AI automation vs project event → false").

The passing integration test only works because it explicitly copies the globals
into project scope first
(`checkout_mode_integration_test.go:41-73`, `materializeBuiltInAutomationsInProject`)
— **nothing in server startup does that**. A user who has never toggled the
automation into a project in the TUI gets no feature checkout at all.

*Fix:* register the built-ins with `filter: {project_id: "*"}` (matching the
verified global-wildcard semantics), or materialize a project-scoped copy on
first `feature.completed` for a project, or change
`globalAutomationMatchesProjectEvent` so a global automation with no project
filter matches all projects. Add a startup assertion that the built-ins can
match a synthetic project-scoped `feature.completed`.

---

#### H-1 · HIGH · `checkout_mode` is write-only — the `simple` checkout path is unreachable

**`internal/service/task.go:1842` and `internal/service/taskdeps.go:184`**
*(downgraded from CRITICAL by all three verifiers — it degrades safely to the
pre-existing AI path)*

The write path is complete (frontmatter → metadata JSON), but there are **two
independent read-path gaps**, and both must be fixed:

1. `parseMetadataIntoEntry` (`task.go:1842`) has no `checkout_mode` case, so
   `BrainEntry.CheckoutMode` is never populated.
2. `brainEntryToResolvedTask` (`taskdeps.go:184-243`) copies ~50 fields and
   omits `CheckoutMode`.

*Probe (real storage, clean worktree of HEAD):* DB metadata
`{"checkout_mode":"simple", "merge_policy":"auto_pr"}` →
`BrainEntry.CheckoutMode=""` (control `MergePolicy="auto_pr"` survives, proving
metadata parsing itself works) → `ResolvedTask.CheckoutMode=""` →
`foldCheckoutMode()=="ai"`. The `simple` filter never matches; the `ai` filter
always does.

`foldCheckoutMode` itself is **not** buggy — the 9 `TestFoldCheckoutMode_*`
tests pass because they construct `ResolvedTask` literals and never touch
storage. `GET /tasks` (`internal/api/tasks.go:279`) and MCP `task_get`
(`internal/mcp/task_tools.go:703`) also always report `checkout_mode` empty, so
the user cannot observe that their setting was dropped.

**Third drop the original claim missed:** `TaskServiceImpl.CheckoutFeature`
(`internal/service/task.go:948`) copies `MergePolicy`/`MergeStrategy`/
`ExecutionMode` into the generated checkout task but never
`normalizedOpts.CheckoutMode`, so the MCP `feature_checkout` tool's argument is
discarded too.

*Fix:* add a `checkout_mode` case to `parseMetadataIntoEntry`, add
`CheckoutMode: task.CheckoutMode` to `brainEntryToResolvedTask`, propagate it in
`CheckoutFeature`, and add a **storage round-trip** test (not a `ResolvedTask`
literal) to the fold suite.

---

#### H-2 · HIGH · `EventHub.Publish` can send on a channel closed by unsubscribe

**`internal/realtime/event_hub.go:85` / `:107-114`**
*(downgraded from CRITICAL — `api.Recovery` contains the panic and the ring
buffer still holds the event)*

`Publish` snapshots subscribers under `h.mu`, unlocks, then does `case sub.ch <-
evt:`. `unsub` deletes under the lock but calls `close(sub.ch)` **outside** it.
A `select` `default:` arm does not help — a send on a closed channel is always
"ready" and panics. Reproduced by all three verifiers in ~1.4s.

The damaging part is the ordering in `Ingest`
(`internal/service/event_service.go:115-124`): `seenIDs[evt.ID]` is written
**before** `hub.Publish`, so when the panic aborts the request, the runner's
`EventForwarder` retry re-posts the identical batch and that event is **deduped
away**. Live subscribers — automations, goals, task triggers, webhooks, feature
cascade — never see it. (Events later in the batch were never marked seen and
*are* republished; and the event does remain in the ring buffer, so
`/events/recent` and `Last-Event-ID` replay still show it.)

*Fix:* give each `eventSubscriber` a `done chan struct{}` closed by unsub, and
`select { case sub.ch <- evt: case <-sub.done: default: }`. Move the `seenIDs`
write to **after** a successful publish.

---

#### H-3 · HIGH · `HandleEvent` aborts all remaining automations on the first error, and `Start` discards it silently

**`internal/service/automation_service.go:203-211`, `:94`**

`HandleEvent` `return err`s from inside `for _, automation := range
automations.Entries`, so every automation later in the list is never evaluated
for that event. `Start` then does `_ = s.HandleEvent(ctx, evt)` — and
`automation_service.go` **imports no logging package at all**, so there is zero
output. `CheckScheduled` has the same abort at `:145`/`:164-166`, where the loss
is permanent (the minute-bucket dedup key means that minute is never revisited).

Aggravating: `automationRunAudit.errorText` is declared (`:514`) and rendered
(`:565-567`) but **never set by any call site**, and audits are only ever
`"queued"` or `"skipped"` — no failure is recorded on any surface.

*Verified:* with three matching automations and the newest one failing to
`Save`, tasks created = **0** instead of 2.

*Fix:* collect a `firstErr` and continue, mirroring `GoalService.HandleEvent`
(`goal_service.go:403-425`) which already does exactly this. Log per-automation
failures with automation id + event id. Log the error in `Start` instead of
discarding it. Write a `"failed"` run audit using the existing `errorText` field.

---

#### H-4 · HIGH · No automation loop guard: `ignore_automation_events` has no live reader

**`internal/service/automation_service.go:241`**

`Trigger.IgnoreAutomationEvents` is accepted by the API, persisted to
frontmatter, round-tripped by CLI/MCP/PWA, and documented at `README.md:851` as
defaulting to loop-safe. Its **only** reader is
`internal/events/matcher.go:171`, inside an `AutomationMatcher` that is never
constructed in production. `automationMatchesEvent` never inspects `evt.Source`.

*Verified end-to-end with the flag explicitly set to `true`:* 1 task after the
seed event, 2 after replaying the generated task's `pending→in_progress`, 3
after `in_progress→completed`. Growth is 2+ new tasks per generation, bounded
only by `once_per` / `cooldown` / `max_concurrent` if the author set them.

**The knob is inert twice over:** even if wired, the dead guard's predicate
`isAutomationSource` (`matcher.go:395`) matches only source
`"automation"`/`"automation_matcher"`, while `Ingest`
(`event_service.go:91-94`) rejects any source other than `"runner"`/`"api"` and
`types` defines no automation source constant.

*Fix:* a correct guard must consult the triggering task's provenance —
`GeneratedBy` prefixed `"automation:"` (set at `automation_service.go:402`) — or
stamp `generated_by` into the metadata of task lifecycle events. Honor a
`nil`/`true` `IgnoreAutomationEvents` as the documented default.

---

#### H-5 · HIGH · `List(Limit:1000)` truncation silently voids `once_per` dedup

**`internal/service/automation_service.go:709-713` and `:645-649`**
*(scale-gated: needs >1000 tasks modified more recently than the one being
sought)*

Both guard lookups fetch only the 1000 most-recently-modified tasks in a project
(`ORDER BY modified DESC LIMIT 1000`, `internal/storage/list.go:83-100`) with no
offset loop and no truncation detection —
`BrainServiceImpl.List` sets `total := len(entries)` (`brain.go:1987`), so
truncation is invisible.

*Probe:* after padding with 1001 newer tasks, `generatedTaskExists` returned
`false` (want `true`), `runnable` returned 0 (want 1), and `createTask` created
a **duplicate** despite `once_per: feature_id` + `max_concurrent: 1` +
`cooldown: 1h` all being set.

The reliably-exposed rail is **`once_per`**: a completed generated task (e.g. a
week-old feature-checkout task) is never written to again and ages out
permanently. `max_concurrent` and `cooldown` degrade only under heavy write
bursts, since running/just-created tasks sort near the top.

Note the repo already knows 1000 is too small — the sibling
`TaskServiceImpl.getAllTasks` (`internal/service/task.go:361`) uses
`Limit: 10000, // effectively unlimited` for the same entity in the same scope.

*Fix:* add an indexed storage lookup for `generated_key`
(`SELECT 1 FROM notes WHERE project_id=? AND
json_extract(metadata,'$.generated_key')=?`), a `COUNT` query for runnable tasks
by `generated_by`, and a `MAX(created)` for cooldown. Failing that, paginate
with an offset loop and make `List` return a real `COUNT(*)` as `Total`.

---

#### H-6 · HIGH · Goal automations are dispatched twice

**`internal/service/automation_service.go:178`**

Goal entries are stored as `type=automation`, `status=active`,
`generated_by=brain-goal`, with `Trigger.Type="event"` and a non-nil `Action`
(`goal_automation.go:81-91`). `AutomationService.HandleEvent` lists **all**
active automations with no `generated_by` exclusion, so each goal-relevant event
both reconciles the goal (correct, via `GoalService`) **and** generates a
spurious task.

*Reproduced twice, independently:* one goal entry + one `task.status_changed` →
1 unwanted task, `title="Automation: <id>"`, `generated_by="automation:<id>"`,
`prompt="reconcile prompt"`. `buildGoalTrigger` sets no `OncePer`, `Cooldown`, or
`MaxConcurrent`, so `automationGeneratedKey` returns `""`, the dedup check is
skipped entirely, and a **fresh** task is queued on **every** matching event —
unbounded, not one-shot.

The spurious copy carries the goal's reconcile `DirectPrompt` with
`CompleteOnIdle` defaulting true, so the runner actually executes it, bypassing
`Reconcile`'s need-work decision, `goalGeneratedTaskOpen` dedup, and audit
trail. The two copies also fall on opposite sides of the pause gate.

*Fix:* skip entries with `automation.GeneratedBy == types.GoalGeneratedBy` (or
carrying `types.GoalTag`) in both `HandleEvent` and `CheckScheduled`, mirroring
`GoalService.listActiveGoals` (`goal_service.go:443-445`) which already does it.

---

#### H-7 · HIGH · `complete_on_idle` forces script failures to be recorded as completed

**`internal/service/automation_service.go:410` +
`internal/runner/process_manager.go:522-528`**

`createTask` defaults `CompleteOnIdle` to `true` for every automation-generated
task, and `CheckCompletion` returns `CompletionCompleted` for a `CompleteOnIdle`
task **regardless of exit code**. The exit-code branch
(`process_manager.go:475-480`) is gated on `!checkTaskFile`, and the only
production caller (`checkRunningTasks`, `runner.go:2368`) always passes
`checkTaskFile=true` — so the tests asserting `CompletionCrashed` on non-zero
exit exercise a mode production never uses.

`spawnScript` uses a real `OsProcess`, so a truthful exit code **is** available.
`apiStatus` becomes `"completed"` and is PUSHed at `runner.go:2450` **before**
`finalizeScriptTask` (`:2457`) records `exit_code` into metadata, and nothing
anywhere reads `exit_code` back to correct status. The same swallow applies to
script **timeouts** (SIGKILL from `Script.MaxTimeout`, `executor.go:358-364`).

Setting `complete_on_idle: false` is not a workaround: with it false, a script
exiting **0** falls through to `CompletionCrashed` → `apiStatus "pending"` →
endless re-run. There is no correct setting.

*Fix:* in `CheckCompletion`, special-case `task.ExecutorType == "script"` and
honor the real exit code (0 → `CompletionCompleted`, non-zero →
`CompletionFailed`) **before** the `CompleteOnIdle` shortcut. `CompleteOnIdle`
is an idle-detection heuristic for agent processes and should not apply to a
process with a meaningful exit status.

---

#### H-8 · HIGH · PWA assistant's `create_automation` always writes status `"pending"`

**`internal/api/assistant.go:340`**
*(downgraded from CRITICAL — one-click recoverable in the UI)*

`createEntryRequestFromAction` hard-defaults `Status` to `"pending"`, which is
correct for `create_task` and wrong for `create_automation`.
`BrainServiceImpl.Save` (`brain.go:137-141`) rewrites only an **empty** status to
`"active"`, so `"pending"` persists. Both `HandleEvent` (`:178-182`) and
`CheckScheduled` (`:122-126`) list `Status: "active"`, and
`internal/storage/list.go:39-42` turns that into exact SQL `status = ?`. The
automation can never fire on any event or cron tick.

The `status` property exists in the tool schema
(`assistant_tools_write.go:50`) but as a bare `{"type":"string"}` with no
description, enum, or default, and `assistantSystemPrompt()` never mentions it —
so the model has no signal to pass `"active"`.

Every other automation-creation site in the repo explicitly sets `"active"`
(`goal_automation.go:80`, `builtin_feature_checkout.go:73`,
`builtin_feature_checkout_simple.go:73`). The assistant path is the lone
outlier; `git log -L` shows the line predates `create_automation` being layered
onto the shared helper.

Mitigations: `RunAutomationNow` ignores status so the PWA Run button still
works, and `CardAutomations.tsx:63-66` flips it to `"active"` in one click —
though the tooltip misleadingly says "Paused — click to re-enable" when it was
never enabled.

*Fix:* make the default type-aware in `createEntryRequestFromAction` — default
`automation` (and `goal`) to `"active"`, or drop the literal and let
`BrainServiceImpl.Save` apply its own per-type default.

---

#### H-9 · HIGH · The shipped `feature-review` automation triggers on a nonexistent event

**`cmd/brain/assets/automations/feature-review.md:11`**

`brain init` (`cmd/brain/commands/setup.go:116`) and `brain migrate automations`
(`cmd/brain/commands/migrate.go:112`) install this asset with `status: active`
and `trigger.event: feature.all_completed`. That string is **not** in
`types.AllEventTypes` (`IsValidEventType == false`, verified), has no live
publisher, and `MatchEventPattern` is exact-string — so it can never match.

The constant exists only on the dead `internal/events` bus, published by
`BrainServiceImpl.publish` (`brain.go:55-59`), which is a nil-guarded no-op
because `internal/apiserver/server.go:300` passes `nil` for the bus. It is
**deader than that**: `realtime.BridgeBusToHub` has zero non-test callers and
subscribes only to `entry.*`, so even a non-nil bus would never reach the
automation pipeline.

The codebase already knows — `internal/service/goal_automation.go:54` carries
the comment "feature.all_completed is dead and never returned", and
`goal_automation_test.go:102-105` asserts it must never appear in a goal
trigger. The shipped asset, `README.md:843`/`:862`, and
`internal/mcp/task_tools.go:1033` were never updated.

**Worse than stated in the original claim:** `migrate.go:84` maps the monitor
template `feature-review` → `feature-review.md` and then disables the existing
monitor task schedules — so `brain migrate automations` actively turns **off** a
working dependency-gated feature-review monitor and substitutes a never-firing
automation.

*Fix:* change the asset to `event: feature.completed`; fix `README.md:843`,
`:862`, and `internal/mcp/task_tools.go:1033`. Fail `brain init`/`migrate` loudly
if an installed asset's `trigger.event` is not in `types.AllEventTypes`.

---

#### M-1 · MEDIUM · `trigger.type: webhook` is structurally unreachable — ✅ **RESOLVED (partially)**

**`internal/service/automation_service.go:276`**
*(both verifiers rated this a missing-feature / doc mismatch rather than a
runtime bug)*

> **Original finding:** `webhook.received` is not a valid event type, has no
> producer, and there is no inbound receiver route — yet it is offered by the CLI
> wizard, the README, CLI help, `brain-automation/SKILL.md`, and the MCP schema.
> The automation saves, lists as active, and can never be triggered by any
> webhook. (`POST /api/v1/automations/run` still fires the action, since
> `RunAutomationNow` never inspects `Trigger.Type`.)
>
> *Proposed fix:* either implement it (add `"webhook.received"` to
> `types.AllEventTypes` and register an inbound receiver route that `Ingest`s
> it), or reject `trigger.type=="webhook"` at entry-create time and remove it
> from all six advertising surfaces.

**Resolution:** the first half of the proposed fix was taken.
`"webhook.received"` is now declared and present in `types.AllEventTypes`, so
`Ingest` accepts it and a webhook automation is reachable. The six advertising
surfaces are therefore no longer lying.

**Residual, still open:** no inbound receiver route was added, so the trigger is
only usable by an integration that POSTs the event to `/api/v1/events` with a
valid token — and that endpoint has no `RequireScope`, so a read-only token
suffices to drive task creation. The `normalizeWebhookPath` fail-open described
in §5.4 is now reachable as a result. Downgrade to LOW rather than closing
outright.

---

#### M-2 · MEDIUM · No validation of `trigger.type` / `trigger.event` / `trigger.cooldown` at write time

**`internal/api/entries.go:173-179`, `:445-462`** · *contested (1 verifier
argued for LOW observability-only)*

`types.IsValidEventType` has exactly one caller — `EventServiceImpl.Ingest`
(`event_service.go:86`). The create/update validators cover `timezone`,
RFC3339 fields, and the top-level `schedule`, but a grep for `req.Trigger` in
`internal/api/entries.go` returns exactly one hit — line 1381, a
frontmatter→domain copy. Nothing inspects `Trigger.Type`, `Trigger.Event`,
`Trigger.Cooldown`, or `Trigger.Schedule`.

So `type: "Event"`, `event: "task_claimed"`, `cooldown: "1d"`, and `schedule:
"@daily"` all store as active and never fire, with no error, no log, and no run
audit (audits are only written *after* a match).

The counter-argument is worth recording: a naive `IsValidEventType` gate would
wrongly reject the legitimate `task.*` / `*` patterns and the `Events []string`
form. Validation must check pattern **shape** (and prefix-match wildcards
against known namespaces), not set membership. Note also that `task.blocked`
*is* in `AllEventTypes` and would pass such a gate while still never firing.

*Fix:* validate `Trigger.Type ∈ {event, cron, webhook, session}`; validate each
`EventPatterns()` entry as exact-in-`AllEventTypes`, a known-namespace `ns.*`,
or `*`; `time.ParseDuration` the cooldown; `cron.Parse` the trigger schedule.
Return a 400 `ValidationDetail` naming the offending value. At minimum add
warn-level logs at `automation_service.go:152-155` (cron parse swallow) and
`:252-253` (unknown trigger type).

---

#### M-3 · MEDIUM · The `checkout_mode` empty→`"ai"` default applies to every event type

**`internal/service/automation_service.go:332`** · *contested (1 verifier called
this latent-only, since C-2 currently masks it)*

`if key == "checkout_mode" && actual == "" { actual = "ai" }` sits inside the
generic filter loop with **no event-type or source guard**. Verified:
`matchAutomationFilters({checkout_mode:"ai"}, Event{Type:"task.completed"})` ==
`true`. The identical filter yields `false` for task-level triggers, because
`matchTriggerFilters` (`trigger_service.go:212-220`) has no such rewrite.

The dangerous consequence is contingent on C-2 being fixed: the runner's
`FeatureTracker` publishes an independent `feature.completed` carrying only
`ready_count` and **no** `checkout_mode` (`feature_tracker.go:207-213`;
`event_conversion.go:70-105` never maps it), so it always reads as `"ai"`. Once
the automations are project-scoped, a feature explicitly configured for the
simple path would trigger the AI checkout automation **as well** — and their
`once_per` keys differ (`automation:<simpleID>:F` vs `automation:<aiID>:F`), so
neither dedups the other. Two concurrent merges of the same branch.

*Fix:* gate the default on `evt.Type == types.EventFeatureCompleted` (and
ideally `evt.Source == "api"`), or drop the special case and have
`FeatureTracker` stamp `checkout_mode` on the runner-side event so both emitters
agree.

---

#### M-4 · MEDIUM · Whole families of events are emitted without `ProjectID`

**`internal/runner/idle_detection.go:242`, `:298`, `:335`**

The three emissions set only `{Type, [Result,] TaskID}` — no `ProjectID`, no
`FeatureID`, no `TaskPath` — even though the in-scope `RunningTask` carries all
three. `RunnerEvent.ToEvent` (`event_conversion.go:50-72`) backfills only
`TaskID` from `Result`, and nothing downstream enriches (`Ingest` assigns only
ID/timestamp). Contrast `runner.go:2486-2493`, which sets all of them.

For an OpenCode task, the idle path is the **normal** completion route (the
agent goes idle rather than the process exiting), so project-less
`task.completed` is systematic, not a coin flip. `complete_on_idle` defaults
true for every automation-generated task, so chained automations are hit
hardest. Also starves `FeatureCascadeService`, which ignores events with no
`feature_id`.

Reproduced: a `RunningTask` with `ProjectID:"proj-a", FeatureID:"feat-x"`
produced `project_id="" task_id="task1" task_path="" feature_id=""`.

**Scope correction:** the same idle path also calls `UpdateTaskStatus` → `PATCH
/entries/*/metadata`, and `internal/api/entries.go:620-631` emits a
**correctly-scoped** `task.status_changed`. So automations on
`task.status_changed`, on `task.*`, or on `feature.completed` still fire; only
automations keyed specifically on `task.completed` / `task.failed` break.
`pi` and `script` executors are immune (`checkIdleStatus` routes them to
no-ops).

The same class affects `runner.session_discovered` (`:1920`),
`runner.poll_complete` (`:3099`), and all four `control.*` events
(`internal/api/control.go:84-90`). It also affected
`feature.enabled`/`feature.disabled`, which have since been removed outright
(see the table above).

*Fix:* add `ProjectID: task.ProjectID, TaskPath: task.Path, FeatureID:
task.FeatureID` to the three `idle_detection.go` emissions. Add a regression
test asserting field parity across every `task.completed`/`task.failed` emitter.
Consider a defensive backfill in `ToEvent` (derive `ProjectID` from `TaskPath`
when empty).

---

#### M-5 · MEDIUM · `task.resume_requested` never carries `FeatureID`

**`internal/api/tasks.go:895-922`, `:977-998`**

Both the single-task and feature fan-out resume handlers build
`EventTaskResumeRequested` **and** the paired `EventTaskStatusChanged` with only
`ProjectID`/`TaskID`/`TaskPath` — diverging from `internal/api/entries.go:515`
and `:627`, which both set `FeatureID`. `HandleResumeFeature` has `featureId` in
scope at `tasks.go:936` and still does not stamp it.

Consequences: a `filter: {feature_id: X}` never matches; `filter: {feature_id:
"*"}` never matches (`MatchFilterValue("", "*")` is false); `once_per:
feature_id` degenerates to the constant `"automation:<id>:"` so the automation
dedups forever after the first fire; `createTask` only propagates the feature
via `if evt.FeatureID != ""` (`:427`), orphaning generated tasks. `GET
/api/v1/events?feature_id=` also silently omits resume events.

No shipped automation is broken by this today (the built-in checkouts key on
`feature.completed`; goal automations also filter `to_status` which resume sets
to `pending`), so this is a consistency/observability gap rather than a live
regression.

*Fix:* `HandleResumeFeature` is a one-liner (`featureId` is in scope).
`HandleResumeTask` needs `types.ResumeTaskResult` (`internal/types/types.go:1331`)
to carry `FeatureID` back from the already-loaded task. Add `TaskTitle` while
you are there.

---

#### M-6 · MEDIUM · `renderAutomationTemplate` silently emits the raw un-rendered template

**`internal/service/automation_service.go:471-474`, `:500-504`** · *contested (1
verifier argued the passthrough is defensible for scripts containing literal
`{{...}}`)*

Both the parse-error and execute-error paths `return input` with **no log and no
audit note**. The failure is all-or-nothing: `Execute`'s partial buffer is
discarded, so `"{{.ProjectID}} and {{.Foo}}"` returns fully literal — the valid
placeholder is lost too.

Every Go template shipped in-repo uses valid fields, so no built-in is currently
broken. What **is** live is the shipped skill documentation:
`cmd/brain/assets/plugins/opencode/skill/brain-automation/SKILL.md:161`/`:191`/
`:216` and `using-brain/SKILL.md:311` teach `{{feature_id}}`, `{{task_id}}`,
`{{date}}` — all hard parse errors (`function "feature_id" not defined`). An
author copying the shipped examples ships an unrendered prompt with zero
diagnostics.

Mitigations: `buildTaskAssignmentHeader`
(`internal/runner/executor_common.go:383-408`) prepends the real Task ID,
Feature ID, Project and Brain path above every `DirectPrompt`, so an agent is
degraded rather than blind; and a user script with a literal placeholder fails
loudly under `set -euo pipefail`.

*Fix:* (1) fix the four SKILL.md examples to the supported dot-syntax names;
(2) return `(string, error)`, `slog.Warn` with the automation ID and the
template error, and set the already-declared-but-never-written
`automationRunAudit.errorText`. Note `internal/events/template.go` already has a
correct error-returning resolver with a richer context — it is dead code.

---

#### M-7 · MEDIUM · `cooldown` fails open on any unparsable duration and is never validated

**`internal/service/automation_service.go:675-680`**

`time.ParseDuration` error → `return false` (no cooldown). Verified:
`"1d"`, `"7d"`, `"30 minutes"`, `"5 m"`, `"1h30"` all disable the guard;
`"24h"`, `"1h"` work. `"1d"` is the canonical Go duration mistake, and this repo
itself ships `internal/lifecycle/logs.go:131` `ParseDuration` which **does**
accept `"2d"` — so the assumption is reasonable.

The CLI wizard prompts `"Cooldown (optional, e.g. 5m, press Enter to skip): "`
and stores the raw string unchecked
(`cmd/brain/commands/automation.go:234-237`) while validating `max_concurrent`
three lines later. The failure is silent in both directions: no write-time
rejection, and at runtime the skip audit simply never records reason
`"cooldown"`.

The sibling `trigger_service.go:265-269` carries an explicit comment
"Invalid cooldown format: treat as no cooldown", so fail-open may be a chosen
policy — but nothing validates the value, so the user's intent is discarded
either way.

*Fix:* `time.ParseDuration` the value at create/update time and reject with 400.
As defence in depth, fail **closed** and emit a run audit with
`skip_reason="invalid_cooldown"`.

---

#### M-8 · MEDIUM · `retry.*` and `action.timeout` are round-tripped and read by nothing

**`internal/types/automation.go:64`, `:69-73`**
*(narrowed — `max_runs` and `expires_at` ARE honored; drop them from the claim)*

A repo-wide non-test grep for `MaxAttempts|\.Backoff|Retry` returns **only**
struct definitions and pure field-copy sites (`api/entries.go:1443-1450`,
`automation_convert.go:51-58`/`:115-122`, `brain.go:207`/`:887-888`,
`task.go:2041`, `taskdeps.go:240`, `tui.go:6041`, `migrate.go:365-372`,
`frontmatter.go:129-131`/`:843-844`). Zero read sites. There is no attempt
counter anywhere in the task lifecycle. `AutomationAction.Timeout` is likewise
copy-only.

`internal/runner/runner.go:2431-2433` resets `failed`/`crashed`/`timeout` to
`"pending"` — comment: "back to pending for retry" — with nothing capping it. So
`retry: {max_attempts: 1}` on a `complete_on_idle: false` script automation
still loops forever, consuming a runner slot.

`retry.timeout` (used in the shipped `SKILL.md:82` example and advertised at
`internal/mcp/brain_tools.go:98`/`:1103` as "Common fields: max_attempts,
backoff, timeout") is not even a field of `AutomationRetry` — it is silently
dropped at decode.

`requires_capability` is a third inert field, and additionally
type-incompatible with its intended consumer (`string` on the action vs
`[]string` on `ResolvedTask`/`CreateEntryRequest`).

**Corrections:** `max_runs` and `expires_at` are enforced —
`internal/runner/schedule.go:224-241` (`disableSchedule` on window expiry and
run-count exhaustion) and `internal/service/task.go:1357-1363`. The unbounded
loop is also not the default path: `automationCompleteOnIdle` defaults
`complete_on_idle` to true and `process_manager.go:526-531` then returns
`CompletionCompleted` on exit.

*Fix:* implement an attempt counter (persist `attempts` on the task, compare
against `Retry.MaxAttempts` before the `failed→pending` requeue, apply
`Backoff`/`Delay`), **or** delete `AutomationRetry`, `Action.Timeout`, and
`RequiresCapability` along with their SKILL.md / CLI / MCP surface so users are
not given safety controls that do nothing.

---

#### M-9 · MEDIUM · `action.session_mode` is inert — `ResolvedTask` has no such field

**`internal/service/automation_service.go:409` and
`internal/service/goal_service.go:252`**

Compiler-verified: `types.ResolvedTask` has `ExecutionMode` and `CheckoutMode`
but **no** `SessionMode` (`r.SessionMode undefined`). `brainEntryToResolvedTask`
therefore cannot copy it, and `grep -rn "SessionMode|session_mode"
internal/runner/` returns **zero** hits. `git log -S` shows it was never wired,
not wired-then-removed.

The value round-trips fully — `brain.go:190` persists it, `task.go:1925-1926`
reads it back into `BrainEntry` — and is offered by `--session-mode`
(`cmd/brain/flags.go:1325`), the MCP goal schema
(`internal/mcp/goal_tools.go:46`), and a TUI dropdown that **defaults to
`"continue"`** (`internal/tui/goal_config_modal.go:130`). Worse,
`internal/service/goal_automation.go:28` documents "SessionMode is honored." It
is not.

**Even if plumbed, `"continue"` has nothing to reach:** `executor.go:585`
baselines `listSessionIDs` only to *discover* the new session id, never to
resume one, and `pi_executor` uses `--no-session`.

*Fix:* either implement session continuation and add `SessionMode` to
`ResolvedTask` + `brainEntryToResolvedTask` + the executor spawn path, or remove
the field from `AutomationAction`, the CLI flag, the MCP schemas, the TUI modal,
and the false doc comment.

---

#### M-10 · MEDIUM · The dry-run debugging tools do not reproduce the real matcher

**`internal/mcp/brain_tools.go:2405-2412` and
`cmd/brain/commands/automation.go:397`**

Both reimplement matching client-side using **only** `Trigger.Event`, ignoring
`Trigger.Events`, all filters, project scoping, and the `session`/`cron` trigger
types. They disagree with `service.automationMatchesEvent` in **both**
directions — verified side by side against the real matcher:

| Case | Real | Dry-run |
|---|---|---|
| Both built-in checkout automations vs `feature.completed{ProjectID:"brain-api"}` | `false`, `false` | **`true`, `true`** |
| Default goal automation (`Events=[task.status_changed, feature.completed]`, `Event` empty) vs `task.status_changed` | **`true`** | `false` |
| Automation with `filter {to_status: blocked}` vs a `to_status=completed` event | `false` | **`true`** |

So the tool tells a user debugging "why did checkout not run" that **both**
checkout automations matched (impossible — their filters are mutually
exclusive), and reports every default-configured goal as inert while it is
firing. `automation_test`'s own schema example offers the dead
`feature.all_completed`.

This compounds M-2: the one surface that could diagnose a typo'd filter actively
reports `**MATCH:**`.

*Fix:* add a server-side dry-run endpoint (`POST /automations/test`) that
constructs a `types.Event` and calls the exact `automationMatchesEvent` /
`matchAutomationFilters` used by `HandleEvent`, returning per-automation
`matched bool` plus the first failing predicate. Point both the MCP tool and the
CLI at it and delete `matchesAutomationEvent`/`matchesEvent`.

---

#### M-11 · MEDIUM · No dedup record at all without `once_per`, while Brain double-emits

**`internal/service/automation_service.go:364-388`**

When `Trigger.OncePer` is unset, `automationGeneratedKey` returns `""`,
`createTask` skips the entire existence check, and it writes an **empty**
`GeneratedKey` — so there is no dedup record for anything to check later. Since
`shouldSkipTaskGeneration` also short-circuits when `MaxConcurrent<=0 &&
Cooldown==""`, a bare `trigger: {type: event, event: task.status_changed,
filter: {to_status: completed}}` has **zero** guards.

Meanwhile one runner task completion produces **two** hub events with distinct
IDs (runner `runner.go:2437-2444` via `EventForwarder`, then the API's own
`PATCH /entries/*/metadata` at `entries.go:622-631`), and `Ingest`'s `seenIDs`
dedup is ID-keyed so it never collapses them. Result: two identical agent tasks
per completion. Same double-emission shape for `feature.completed` and
`task.claimed`.

**Correction to the original claim:** this *is* fixable by configuration —
`getEventField` supports `source`, so `filter: {to_status: completed, source:
api}` deduplicates the pair exactly, and `cooldown`/`max_concurrent` also
suppress it (they key off `GeneratedBy`, not `GeneratedKey`). The defect is that
the zero-config default double-fires, not that it is unfixable. Note the two
events can arrive up to ~2s apart (`EventForwarder` batches 10 / flushes 2s).

*Fix:* always derive a dedup key even when `once_per` is unset — e.g. fall back
to `automation:<id>:evt:<evt.ID>` — and/or collapse the runner-side and
API-side `task.status_changed` emissions for a runner completion into one.

---

#### M-12 · MEDIUM · `createTask` is a lock-free TOCTOU on `max_concurrent` / `cooldown`

**`internal/service/automation_service.go:347-431`**

`shouldSkipTaskGeneration` → `generatedTaskExists` → `brain.Save` with no mutex
(`AutomationService` is `{brain, pauseChecker}`) and no unique constraint on
`generated_key` (schema has UNIQUE on `path`/`token`/`dedup_key`/`attachments`
only). Concurrent entry points: every `POST /api/v1/automations/run` HTTP
goroutine, plus the single `Start` goroutine.

*Probe:* 6 concurrent `RunAutomationNow` with `max_concurrent=1` → **3** tasks
created.

**Scope narrowing:** the `once_per` half of the probe is not reachable in
production — `RunAutomationNow` deliberately substitutes a nanosecond-unique key
(`:61`), and the only callers that pass a real `once_per` key (`HandleEvent`,
`CheckScheduled`) both run on the single `Start` goroutine and are already
serialized. Only `max_concurrent` and `cooldown` can genuinely race.

**Verifier disagreement worth resolving:** one verifier found the runner
re-enforces the cap in `canStartAutomationTask` (`runner.go:2877`), so a
duplicate task queues and runs serially; another found that guard is **dead in
production**, because its only call sites (`runner.go:1307`, `:1372`) sit in the
poll-fetch branch that `dispatchPushEnabled()` skips at `runner.go:1236`, and
`config.go:226-227` hard-rejects `dispatch_push: false`. Worth a 10-minute
check — it determines whether this is MEDIUM or HIGH.

*Fix:* serialize per-automation with a keyed mutex around the
`shouldSkipTaskGeneration → generatedTaskExists → Save` window, and/or add a
UNIQUE index on `(project_id, generated_key)` so `Save` is the arbiter and a
conflict maps to the existing "dedup" skip path. Also disable the PWA Run button
while a request is in flight (`web/src/components/Workspace/CardAutomations.tsx:39-52`
has no in-flight guard).

---

#### M-13 · MEDIUM · Four event constants pass validation but have zero emitters

**`internal/types/events.go:34`, `:40`, `:50`, `:55`** · *contested (1 verifier
called this low-severity taxonomy hygiene, since these names appear in no doc,
UI, or MCP description)*

`task.blocked`, `task.idle_detected`, `project.started`, `runner.state_saved`
are declared, registered in `AllEventTypes`, and accepted by `Ingest` — but a
non-test grep finds no emitter. `CompletionBlocked` maps to `task.failed`
(`runner.go:2426`); idle detection emits `task.completed`/`task.failed`;
`saveState()` emits nothing.

Notably the repo's own `justfile:249-250` seeds a trigger task with
`"trigger":{"event":"task.blocked"}` and the comment "Should activate when a
task.blocked event fires", and the only thing that makes it fire is the
synthetic hand-POST at `justfile:351`. `hooks.go:229` maps `blocked → block`, so
the `pre-task-block`/`post-task-block` hook filenames are dead for the same
reason.

The working idiom is `event: task.status_changed` + `filter: {to_status:
blocked}`, which is exactly what the shipped
`cmd/brain/assets/automations/blocked-inspector.md:11-14` uses.

*Fix:* emit them (`task.blocked` alongside the `CompletionBlocked` branch;
`task.idle_detected` before the complete/fail decision), or remove them from
`AllEventTypes` and the const block. At minimum document the working substitute
next to the constant.

---

#### M-14 · MEDIUM · Assistant trigger/action decoders silently drop half the schema

**`internal/api/assistant.go:406-425`, `:427-442`**

`triggerFromPayload` reads only `type`/`event`/`schedule`/`filter`, dropping
`once_per`, `cooldown`, `max_concurrent`, `webhook`, `events`, `timezone`,
`ignore_automation_events`. `actionFromPayload` sets 8 of `AutomationAction`'s 12
fields, dropping `SessionMode`, `CompleteOnIdle`, `Timeout`,
`RequiresCapability`. The same decoders back `update_automation`, so the
assistant cannot repair an automation it created.

The direct HTTP path (`HandleCreateEntry`) JSON-decodes the whole request and
preserves everything, so this loss is unique to the assistant's hand-rolled
decoder. Guaranteed casualties are `trigger.webhook` (making the advertised
webhook type unusable via `create_automation`), `timezone`, `events`, and
`action.session_mode`/`complete_on_idle`. Whether the model emits
`once_per`/`cooldown` at all is uncertain — the tool schema declares
`trigger`/`action` as bare untyped objects and the system prompt never documents
those keys, which is the same contract gap.

*Fix:* replace both hand-rolled decoders with `json.Marshal` of the raw
sub-object followed by `json.Unmarshal` into `types.TriggerConfig` /
`types.AutomationAction`, so the decoder can never drift from the struct. Also
give the tool schema real sub-properties.

---

#### M-15 · MEDIUM · PWA toggle on a global/built-in automation edits the global entry

**`web/src/components/Workspace/CardAutomations.tsx:63-82`, `:87-103`**

`listAutomationData` (`web/src/lib/api.ts:1118-1134`) merges global automations
into the per-project card, and the glyph is derived purely from `a.status`. So
the built-in checkout automations render as **Enabled** (green ✓, tooltip
"Enabled — click to pause") in every project card, for automations that provably
cannot fire there (C-2). Clicking calls `updateEntry(a.path, {status})` where
`a.path` is `global/automation/<id>.md`, writing shared global state with no
project handling and no materialization.

The TUI does the opposite: `tui.go:6010-6058` detects a global/built-in row and
creates or updates a **project-scoped copy**, and `tui.go:5942-5954` renders
un-materialized globals as `archived`.

The functional blast radius is smaller than it looks — archiving the global stops
nothing (it was never firing), doesn't touch materialized project copies, and
the `Ensure*` reconcilers skip on `GeneratedBy` regardless of status (so the
flipped status is permanent). The real harm is a misleading enabled state plus a
cross-project state write, and no PWA path to actually enable checkout.

*Fix:* port the TUI's materialize-into-project behaviour into the PWA — better,
move it behind a server endpoint both clients call — and render un-materialized
globals with a distinct "not enabled for this project" state.

---

#### M-16 · MEDIUM · `update_automation` tells the model to disable via `schedule_enabled=false`, which dispatch never reads

**`internal/api/assistant_tools_write.go:208`, `:251-253`**

The tool description says "…or disable via `schedule_enabled=false`". The field
is mapped, persisted (`brain.go:755-756`), and echoed back by
`handleListAutomations` (`assistant_tools_read.go:580`) — but
`AutomationService.CheckScheduled` and `HandleEvent` gate only on `Status ==
"active"` and the runner pause state. `grep ScheduleEnabled
internal/service/automation_service.go` returns nothing. The cron automation
keeps firing and the assistant reports success; a verification read shows
`false`.

`schedule_enabled` is honored, but only for `type: task` scheduled entries
(`internal/runner/schedule.go:209-211`, `internal/service/task.go:1340-1343`) —
which also appear in the Automations tab, which is presumably how the confusion
arose.

*Fix:* change the description to instruct `status: "archived"` for automation
entries, and/or make `CheckScheduled`/`HandleEvent` honor
`ScheduleEnabled == false` as a hard skip for `type=automation`. Stop surfacing
`schedule_enabled` on automation-typed rows in `handleListAutomations`.

---

#### M-17 · MEDIUM · No "evaluated but did not match" record and no logging anywhere in `AutomationService`

**`internal/service/automation_service.go:187-212`** · *contested (1 verifier
argued `GET /events/recent` + `automation_test` partially close this)*

A non-match produces no audit, no event, no counter, no log. `createRunAudit` is
reached only at `:136`/`:196` (paused), `:350` (max_concurrent/cooldown), `:374`
(dedup), `:435` (queued) — all downstream of a successful match. The file
imports no logging package. There is no way to distinguish "my filter is wrong"
from "the event had no project_id" from "an earlier automation errored and
aborted the batch" (H-3).

This is an asymmetry within the repo, not house style:
`realtime/trigger_dispatcher.go:56-62` and `goal_service.go:347-412` both log.

Partial mitigation: `GET /api/v1/events/recent` (`internal/api/events.go:118`,
`router.go:248`) and the `events_recent` MCP tool do let a user confirm whether
the event fired. The remaining three cases stay undiagnosable — and M-10 means
the dedicated dry-run tool gives a **wrong** answer for the filter case.

*Fix:* `slog.Debug` per non-match recording automation id + the first failing
predicate (project scope / event pattern / which filter key), and log
`HandleEvent` errors in `Start`. Avoid writing a brain entry per non-match —
`HandleEvent` lists up to 1000 automations per event, so an audit-per-non-match
would be an amplification bomb.

---

### 7.B — Empirically probed by the harness, **not** put through the adversarial pass

These carry probe evidence but only one pass of scrutiny. Treat as
high-confidence-but-unverified.

---

#### P-1 · HIGH (probable) · `once_per` with a time bucket is broken

**`internal/service/automation_service.go:725-730`**

`automationGeneratedKey` resolves the bucket through `getEventField`, which has
no case for `day`, `week`, `hour`, or `5m`. Probe: `once_per: "day"` and
`once_per: "5m"` both produce the **constant** key `"automation:<id>:"` for two
different events, so the automation fires exactly **once, ever**, and is deduped
forever. `once_per: "project"` is equally constant-empty, because the
automation-only `project` override in `matchAutomationFilters` is not applied in
`automationGeneratedKey`.

**The shipped `brain-automation` SKILL.md teaches exactly this pattern**
(`once_per: "5m"`, `once_per: "day"`).

*Fix:* add explicit time-bucket handling in `automationGeneratedKey` (format
`evt.Timestamp` per bucket), apply the `project` override, and reject unknown
`once_per` values at write time.

---

#### P-2 · MEDIUM · `action.type` `update` and `http` degrade silently — see §5

Verified outputs recorded in §5. Not adversarially contested because the
mechanism is unambiguous (`createTask` has one branch).

*Fix:* reject unknown/unimplemented action types at create time with a 400, or
implement them — which requires adding `Target`/`Fields` and
`URL`/`Method`/`Headers`/`Body` to `AutomationAction` and its frontmatter
mirror.

---

#### P-3 · MEDIUM · The run audit cannot distinguish success from failure

**`internal/service/automation_service.go:139/:200/:354/:379/:439`, `:559-567`**

`createRunAudit` only ever writes `"queued"` or `"skipped"`; nothing transitions
a run to completed/failed. `completed_at` is written equal to `started_at`,
`duration_ms` is hardcoded `0`, and `errorText` is rendered but never set by any
call site. The body is unstructured markdown that readers scrape line-by-line
(`internal/api/automation_runs.go:68-77`), and `GET /automation-runs` applies
the `automation_id` filter **after** the server-side limit, so
`?automation_id=X&limit=N` can return nothing while matching runs exist.

*Fix:* store the run as structured metadata rather than scraped markdown; move
the `automation_id` filter into the query; write a terminal status when the
generated task reaches a terminal state.

---

## 8. Considered and dismissed

Adversarial verification struck down or materially narrowed the following.
Recording them so they are not re-litigated.

**Fully refuted**

- **"`renderAutomationTemplate` will make the built-in simple checkout script
  run `git merge --squash "{{.FeatureID}}"`."** Counterfactual. A `grep -o
  "{{[^}]*}}"` over `internal/service/` returns only `.Project`, `.ProjectID`,
  `.FeatureID`, `.TaskID`, `.EventProjectID` — every shipped Go template uses
  valid binding fields. The scenario required a hypothetical future maintainer
  edit. What survives is the SKILL.md doc issue (M-6).
- **"Global automations resolving `project==""` cause cross-project dedup
  contamination."** Backwards: an empty `ProjectID` means **no** project filter,
  so the scanned set is a strict *superset* of the correct scope — the guards
  can over-count, never under-count from scoping. The only real failure is the
  `LIMIT 1000` truncation, which is already H-5 and applies equally to
  correctly-scoped projects. What survives: the cosmetic asymmetry that search
  treats `""` as "everywhere" while `Save` treats it as `"default"`, and the
  empty `{{.Project}}` rendering (recorded in §4 under `cron`).
- **"The cron tick blocking the shared select loop is what overflows the 64-slot
  subscriber channel."** `CheckScheduled` costs about the same as processing one
  ordinary event, and it runs 1/minute versus the loop's per-event rate. It
  cannot plausibly be the amplifier. The "measurement" offered was a
  reproduction of the existing `event_hub_test.go:320-340`, which publishes 200
  events to a **never-draining** subscriber and asserts non-blocking fan-out — a
  documented design property, not a bug. The realistic overflow path is
  `HandleBulkUpdate` (`internal/api/entries.go:793-804`) emitting one
  `entry.updated` per row in a tight loop against a per-event SQLite drain.
- **"A >60s stall loses a cron minute."** Off by ~2×. Go buffers the *earliest*
  pending fire and delivers it late, so the claim's own 02:59→03:01 scenario
  results in `CheckScheduled(03:00)` running late and firing correctly with the
  right dedup key. Losing a due minute requires blocking across **two**
  consecutive tick boundaries (~2+ min) in a single uninterrupted call, which
  needs `cfg.Embedding.Enabled` plus a sustained burst. Downgraded to a
  low-severity "best-effort in-process cron with no persisted watermark" — the
  same limitation already applies to any restart spanning 03:00.
- **"`max_runs` and `expires_at` are inert."** False. Both are enforced at
  `internal/runner/schedule.go:224-241` and `internal/service/task.go:1357-1363`.
  Only `retry.*` and `action.timeout` are dead (M-8).
- **"`feature.all_completed` is not a valid event type" is a *validation*
  bug.** It is a real constant on the legacy `internal/events` bus; the
  validation-gate fix would have *rejected* the project's own shipped asset. The
  right fix is changing the asset, not adding a set-membership gate (see M-2's
  shape-validation caveat).
- **"`task.blocked` is an invalid event name."** It **is** in `AllEventTypes`
  and would pass any `IsValidEventType` gate. It is a valid name with no
  emitter — a distinct problem (M-13).
- **"`update_automation`/`create_automation` `status:pending` means the
  automation is permanently inert."** One click in `CardAutomations.tsx:63-66`
  flips it to `active`, and `RunAutomationNow` ignores status entirely so manual
  runs still work. Downgraded from CRITICAL to HIGH (H-8).

**Materially narrowed (kept, but smaller than filed)**

- `checkout_mode` read-path gap: not CRITICAL — it degrades **safely** to the
  pre-existing AI checkout path with no crash or data loss. But it is a *double*
  break (two files), plus a third drop in `CheckoutFeature`. → H-1.
- `EventHub` send-on-closed-channel: `api.Recovery` contains the panic (every
  `Publish` caller happens to be on an HTTP goroutine — luck, not design), the
  ring buffer still holds the event, and the batch's later events **are**
  republished. Only the panicking event is lost, and only to live subscribers.
  → H-2.
- `createTask` TOCTOU: `RunAutomationNow` already uniquifies the key, and
  `HandleEvent`/`CheckScheduled` are single-goroutine, so `once_per` cannot race
  in production. Only `max_concurrent`/`cooldown`. → M-12.
- "Whole families of events carry no `ProjectID`": the same idle path *also*
  triggers a correctly-scoped `task.status_changed` via the metadata PATCH, so
  only automations keyed literally on `task.completed`/`task.failed` break, and
  only for the OpenCode executor. → M-4.
- `checkout_mode` empty→`"ai"` default: the concrete double-merge is currently
  **masked** by C-2 (neither built-in matches at all). It becomes live the
  moment C-2 is fixed. → M-3.
- `task.resume_requested` missing `FeatureID`: no shipped automation is broken
  today; also, sibling handlers (`HandleTriggerTask`, `HandleRunTask`) omit it
  too, so it is the `tasks.go` convention rather than a one-off. → M-5.
- "No dedup without `once_per`" is unfixable by config: false —
  `filter: {source: api}` deduplicates the double-emission exactly. → M-11.
- Trigger validation: a naive `IsValidEventType` gate would break `task.*`, `*`,
  and `Events[]`, and would still let `task.blocked` through. → M-2.

---

## 9. Raised but not adversarially verified

⚠️ **Lower confidence than §7.** These came from the mappers and dimension
auditors and were deliberately not put through the 3-verdict pass — mostly
because they are low-severity, or because the mechanism is unambiguous enough
that verification would not change the answer. Terse by design.

- **Three unbounded memory leaks.** `EventServiceImpl.seenIDs`
  (`event_service.go:47`), `AutomationService.Start`'s `seen`
  (`automation_service.go:86`), and `GoalService.Start`'s `seen`
  (`goal_service.go:349`) each accumulate one entry per distinct event ID for the
  process lifetime, with no cap or eviction. `runner.poll_complete` alone feeds
  them one entry per poll interval per runner.
- **Unbounded audit growth.** `automation_run` entries accumulate forever — one
  per match **and** one per skip, including every `paused` and `dedup` — with no
  pruning code anywhere. `event_log` (`goal.reconcile`) has no retention policy
  either.
- **`POST /api/v1/events` has no scope requirement** (`router.go:244-254`),
  unlike `/entries` writes which require `admin:*`. Any authenticated token —
  including read-only — can inject arbitrary domain events and thereby drive
  automation task creation, including `script`-executor tasks that shell out.
  Arguably the highest-value item in this section.
- **`runner.first_task_today` and the second `runner.started` emission** POST to
  `POST /api/v1/events/emit`, a route that is not registered. Both always 404;
  errors are discarded with `_ =` inside a goroutine, so failure is completely
  silent.
- **Latent batch-drop hazard.** If a new `RunnerEventType` is added without a
  `runnerEventTypeMap` entry, `mappedType()` falls back to `"runner."+type`,
  `Ingest` rejects the **whole batch** mid-loop (`event_service.go:87`), and
  `EventForwarder` retries 3× then permanently drops every event at and after
  the bad index (`event_forwarder.go:215-243`). All 22 current constants are
  mapped, so this is latent.
- **Task-level triggers are a separate, partially-broken mechanism.**
  `TriggerService` applies **no project scoping at all**
  (`storage.ListTriggeredTasks` selects across all projects), its cooldowns are
  in-memory only (lost on restart), `CountInProgressByTrigger` ignores its
  `triggerEvent` argument, its SQL pre-filter requires a non-empty
  `$.trigger.event` so a task trigger using only `events: []` is silently never
  loaded, and it has no pause gate. Users reading the shared `TriggerConfig`
  docs will reasonably expect automation semantics and get something different.
- **Goal automations cannot react to feature completion.** `buildGoalTrigger`
  (`goal_automation.go:111-139`) unconditionally attaches `to_status:
  "in:completed,validated,blacked"`-style filter, but `CheckFeatureCompletion`
  never sets `evt.ToStatus`. Probe: `automationMatchesEvent(goal,
  feature.completed) == false` while `(goal, task.status_changed→completed) ==
  true`. So `trigger_source: "feature"`, and the feature half of the default
  `"both"`, are dead.
- **CLI wizard cannot express most of the trigger schema.** It builds a
  `types.AutomationTrigger`, which lacks `Events`, `Timezone`, `Cooldown`, and
  `MaxConcurrent`, and it copies a never-assigned `Filter`. The TUI's `n`
  creates a **goal** only, and `e` on an automation edits the entry **body**,
  never the frontmatter where trigger/action live. The PWA has no create/edit UI
  at all outside the assistant.
- **`AutomationAction.RequiresCapability` is type-incompatible** with its
  intended consumer (`string` on the action vs `[]string` on
  `ResolvedTask`/`CreateEntryRequest`), and `createTask` never propagates it — so
  an automation declaring a required runner capability produces a task any
  runner can claim.
- **`normalizeWebhookPath` fail-open** (`automation_service.go:291-293`): an
  empty `Trigger.Webhook` would match any `webhook.received` lacking
  `webhook_path` metadata. Doubly unreachable today (M-1), and the CLI wizard
  rejects an empty path.
- **`feature.progress` runner metadata is mislabeled** —
  `feature_tracker.go:216-221` puts the completed count in `running_count`.
- **Stray non-compiling probe files** were left in the tree by parallel
  verification agents and currently break `go test ./internal/service/`:
  `internal/service/zzz_refute_projless_auto_test.go` and
  `internal/service/zz_probe_resume_featureid_test.go`. Also
  `internal/runner/zz_refute_idle_projectid_test.go` and
  `internal/service/zz_probe_refute_test.go` exist as intentional probes. Delete
  or fix before the next `just check`.

---

## 10. Recommended next steps

Ordered by (impact × confidence) ÷ effort.

### Tier 1 — do first (each is small and unblocks a shipped feature)

1. **Fix the built-in checkout scope (C-2).** Give both built-ins `filter:
   {project_id: "*"}` at registration, or materialize a project-scoped copy on
   first `feature.completed`. Add a startup self-test that asserts a synthetic
   project-scoped `feature.completed` matches. *Without this, feature checkout
   does not exist for any user who never touched the TUI toggle.*
2. **Fix the `checkout_mode` read path (H-1).** Two one-line additions
   (`parseMetadataIntoEntry`, `brainEntryToResolvedTask`) plus one in
   `CheckoutFeature`. Add a **storage round-trip** test to the `foldCheckoutMode`
   suite — the existing 9 tests pass over the break.
3. **Fix `once_per` time buckets (P-1)** or reject them at write time. The
   shipped SKILL.md teaches `once_per: "5m"` / `"day"`, which currently means
   "fire once, ever".
4. **Change `feature-review.md` to `feature.completed` (H-9)**, fix
   `README.md:843`/`:862` and `internal/mcp/task_tools.go:1033`, and stop
   `brain migrate automations` from disabling the working monitor.
5. **Stop `HandleEvent` aborting on the first error (H-3).** Collect `firstErr`
   and continue, copying `GoalService.HandleEvent`. Add `slog` to
   `automation_service.go` and log in `Start`. ~15 lines, removes an entire class
   of invisible failure.
6. **Fix the PWA assistant's `"pending"` default (H-8).** One type-aware branch.

### Tier 2 — correctness and safety

7. **Fix `realtime.Hub.publish` (C-1).** Snapshot channels under `RLock`,
   iterate the slice, and replace `close(ch)` with a `done` channel. Same
   treatment for `EventHub` (H-2), plus move the `seenIDs` write after a
   successful publish.
8. **Exclude goal entries from `AutomationService` (H-6).** One condition in
   `HandleEvent` and `CheckScheduled`.
9. **Honor script exit codes (H-7).** Special-case `ExecutorType == "script"` in
   `CheckCompletion` before the `CompleteOnIdle` shortcut.
10. **Implement the loop guard (H-4)** using `GeneratedBy` provenance, matching
    what `README.md:851` already promises.
11. **Resolve the `canStartAutomationTask` disagreement (M-12)** — 10 minutes of
    reading determines whether the automation concurrency cap has a second line
    of defence at all.

### Tier 3 — honesty pass (delete or implement; do not leave half-declared)

12. **Pick a fate for `update` / `http` actions (§5, P-2)**, `retry.*`,
    `action.timeout`, `requires_capability` (M-8), and `session_mode` (M-9). Six
    inert knobs across four UIs is the single biggest source of the "does this
    even work?" feeling. Deleting them is a legitimate, cheap fix.
13. **Pick a fate for `trigger.type: webhook` (M-1)** — implement the receiver
    or remove it from the six surfaces that advertise it.
14. **Emit or delete `task.blocked` / `task.idle_detected` / `project.started` /
    `runner.state_saved` (M-13).**

### Tier 4 — validation, observability, tooling

15. **Add write-time validation (M-2, M-7):** `trigger.type` enum,
    event-pattern *shape* (exact-in-`AllEventTypes` | known-namespace `ns.*` |
    `*`), `time.ParseDuration` on cooldown, `cron.Parse` on trigger schedule.
16. **Add a server-side `POST /automations/test` (M-10)** that calls the real
    matcher and returns the first failing predicate; repoint MCP
    `automation_test` and `brain automation test` at it and delete the two
    client-side reimplementations.
17. **Add `slog.Debug` on non-match with the failing predicate (M-17)** and a
    drop counter + rate-limited warn on `EventHub` fan-out drops.
18. **Fix the indexed lookups (H-5):** `generated_key` lookup, `COUNT` for
    concurrency, `MAX(created)` for cooldown — plus a UNIQUE index on
    `(project_id, generated_key)` which also closes M-12.
19. **Backfill `ProjectID`/`FeatureID`/`TaskPath` on the three
    `idle_detection.go` emissions and the resume handlers (M-4, M-5),** with a
    field-parity regression test across all emitters of each type.
20. **Add `RequireScope` to `POST /api/v1/events` (§9).**
21. **Cap the three `seen`/`seenIDs` maps and add retention for
    `automation_run` and `event_log` (§9).**
22. **Delete the stray probe test files** listed at the end of §9 so
    `just check` passes.

### Worth considering separately

- `internal/events` is a fully-implemented, better-designed parallel
  implementation (dedup bus, `event_log` replay, error-returning template
  resolver with a richer context, a real loop guard) that is 100% dead. Either
  wire it up or delete it — its presence actively misleads anyone reading the
  code to understand matching.
- Events are not persisted at all. There is no durable at-least-once delivery,
  no replay-after-crash, and no way to answer "which events did my automation
  miss". If automations are meant to be load-bearing, this is the structural gap
  underneath H-2, H-3, and M-17.

---

## 11. Empirical test log

### What was actually run

**Harness A — in-process end-to-end wiring.** `EventService.Ingest →
realtime.EventHub → AutomationService.Start` wired together in a test binary
against a real temp brain.

| Test | Observation |
|---|---|
| `task.completed` POSTed, project-scoped `event` automation | ✅ exactly 1 task, prompt template-rendered |
| Project-scoped `cron` automation, schedule `* * * * *` | ✅ 1 task, `generated_key=automation:cron:<id>:202601020304`, prompt `"tick P"` |
| Global `cron` automation | Task written to `projects/default/task/*.md`, prompt rendered `"global tick []"` (empty `{{.Project}}`) |
| `feature.completed{ProjectID:"P"}` vs both built-in checkout automations | ❌ **0 matches, 0 tasks** in both `ai` and `simple` modes |
| Project-scope truth table (5 combinations × global/scoped × filter) | Full table in §4; `MatchFilterValue("", "*") == false` confirmed |
| Session trigger truth table (4 combinations) | Only global + no filter matches |
| `trigger.type` = `"Event"` / `"EVENT"` / `""` | ❌ matched nothing against any of 3 event types |
| `action.type` = `prompt`/`script`/`shell`/`update`/`http`/`bogus`/`""` | Task shapes recorded in §5; `update`/`http`/`bogus`/`""` all produced ordinary prompt tasks |
| Goal automation + `task.status_changed` | ⚠️ 1 spurious task `title="Automation: <id>"` alongside the correct reconcile |
| Loop amplification with `ignore_automation_events: true` | 1 → 2 → 3 tasks across replayed status flips |
| 6 concurrent `RunAutomationNow`, `max_concurrent: 1` | 3 tasks created |
| 8 concurrent `createTask`, `once_per`+`max_concurrent`+`cooldown` | 6 tasks, all `generated_key="automation:a1:f1"` |
| 1001-task padding, then dedup lookup | `generatedTaskExists=false` (want true), duplicate created |
| 3 matching automations, first one fails to `Save` | 0 tasks created (want 2) |

**Harness B — storage round-trip.** Real SQLite + markdown store.

- `brain.Save(CreateEntryRequest{CheckoutMode:"simple"})` → file frontmatter
  `checkout_mode: simple`, DB metadata `{"checkout_mode":"simple",...}` →
  `NoteRowToBrainEntry(...).CheckoutMode == ""` (control `MergePolicy` survives)
  → `brainEntryToResolvedTask(...).CheckoutMode == ""` → `foldCheckoutMode ==
  "ai"`.

**Direct function probes** (`go test`-driven, in-package):

- `types.IsValidEventType`: `false` for `webhook.received`,
  `feature.all_completed`, `goal.reconcile`, `manual`; `true` for
  `task.blocked`, `task.idle_detected`, `project.started`,
  `runner.state_saved`.
- `EventServiceImpl.Ingest("webhook.received")` → ``invalid event type
  "webhook.received" at index 0``.
- `getEventField` against a fully-populated event: `task_path`, `task_title`,
  `status`, `event`, `id`, `timestamp`, `reason` all → `""`.
- `matchAutomationFilters({checkout_mode:"ai"}, Event{Type:"task.completed"})` →
  `true`; the same filter via `matchTriggerFilters` → `false`.
- `renderAutomationTemplate`: `"{{.ProjectID}} and {{.Foo}}"` → returned
  verbatim; `"{{feature_id}}"`, `"{{date}}"`, `"{{time}}"`, `"unclosed {{
  .FeatureID"` → returned verbatim; `"Review {{.FeatureID}} in {{.Project}}"` →
  `"Review F in P"`.
- `cooldownActive`: `1d`/`30 minutes`/`5 m`/`1h30` → `false` (guard off);
  `24h`/`1h` → `true`.
- `pkg/cron.Parse`: `"0 9 * * MON"` → error, `"@daily"` → error,
  `"0 9 * * 1-5"` → ok.
- `automationGeneratedKey` with `once_per: "day"` / `"5m"` → constant
  `"automation:<id>:"` for two different events.
- Compiler probe: `ResolvedTask.SessionMode` → `undefined (type ResolvedTask has
  no field or method SessionMode)`.

**Race reproductions** (throwaway modules/tests, since deleted):

- `realtime.Hub`: 1 publisher on `runners` + 1 Subscribe/unsub goroutine →
  `-race`: `WARNING: DATA RACE` read `hub.go:61` vs write `hub.go:35`/`:42`;
  without `-race`: `fatal error: concurrent map iteration and map write ...
  (*Hub).publish ... hub.go:61`. Reproduced independently by 3 verifiers.
- `realtime.EventHub`: 50 subscribers, 1 publisher, 1 unsubscriber → `panic:
  send on closed channel ... (*EventHub).Publish ... event_hub.go:85` in ~1.3s,
  without needing `-race`. Reproduced independently by 3 verifiers.
- Go ticker semantics: 100ms ticker with a 550ms-blocked receiver → 1 of 5 ticks
  delivered, carrying the **earliest** missed fire time (this is what refuted the
  cron catch-up claim).
- `EventHub` fan-out: 200 events to a non-draining subscriber → 64 delivered,
  136 silently dropped, ring buffer retained all 200.

**Static analysis** — repo-wide greps excluding `_test.go` and
`.claude/worktrees/`, used to establish zero-reader / zero-emitter claims for:
`IgnoreAutomationEvents`, `MaxAttempts`/`Backoff`, `SessionMode` in
`internal/runner/`, `webhook.received`, `EventTaskBlocked`,
`EventTaskIdleDetected`, `EventProjectStarted`, `EventStateSaved`,
`AllEventTypes`/`IsValidEventType` callers, `NewAutomationMatcher` callers,
`req.Trigger` in `internal/api/entries.go`, and `ScheduleEnabled` in
`internal/service/automation_service.go`.

### What could NOT be tested, and why

- **Live runner pickup of a generated task.** Everything up to and including
  `brain.Save` was exercised in-process; actual claim → spawn → executor
  behaviour was read, not run. The claims about `CheckCompletion` /
  `finalizeScriptTask` ordering (H-7) and `filterByExecutors` dropping
  `script` tasks are code-reading, corroborated by test-file inspection, not
  live runs.
- **`canStartAutomationTask` reachability (M-12).** Two verifiers reached
  opposite conclusions about whether the poll-fetch branch that calls it is dead
  under mandatory `dispatch_push`. Deciding this requires running a real runner
  against a real API; it was not attempted.
- **Production-scale hub backpressure.** The 64-slot drop was demonstrated with
  a non-draining subscriber, which is not a real subscriber's behaviour. The
  actual overflow threshold under a realistic
  `HandleBulkUpdate`-vs-per-event-SQLite race was **not** measured, so the
  practical severity of the drop is unquantified.
- **`brain init` / `brain migrate automations` end-to-end.** The asset contents,
  the install paths, and the `templateToAutomationFile` mapping were read; the
  commands were not executed against a real brain, so the claim that migrate
  disables the working feature-review monitor (H-9) is code-reading only.
- **PWA behaviour.** All PWA findings (H-8, M-14, M-15, M-16) are TypeScript
  source reading plus Go-side probes of the handlers. No browser session was
  run.
- **Multi-runner / multi-instance concurrency.** All race probes were
  single-process. The `createTask` TOCTOU across two `brain-api` instances
  sharing a store was not tested.
- **`go test ./internal/service/`** could not be run cleanly at several points
  because parallel verification agents left non-compiling probe files in the
  tree (listed in §9). Individual probes were run via focused `-run` filters or
  isolated packages instead.


