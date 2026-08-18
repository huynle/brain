# Session Monitoring, Steering & Continuation — Web UI Integration Plan

Status: **implemented** 2026-08-18 (same day) on branch `claude/task-session-monitoring-af8b63` · all phases landed with unit tests (Go: `archived_settled_test`, `archive_finalize_test`, `session_history_sqlite_test`; web: 464 passing incl. transcript reducer, sessionRef, instanceStream, lane, and full verb gate matrices) · verified live end-to-end in an isolated demo stack: watched a real task session stream, injected a check-in (INJECTED chip), read the completed task's transcript through the new SQLite tier with every process dead, continued the session on a fresh instance (including the workdir-fallback path), and got the follow-up answered. Remaining Phase-5 polish tracked below.
Scope: PWA (`web/`) + small backend enablers (`internal/runner`, `internal/api`)
Addendum 2026-08-18: archive completed features/tasks + Overview lane overflow fix (Track A, below) — also implemented + live-verified.
Post-implementation notes: (1) the runner's `control.allowed_workdir_roots` policy applies to continuation spawns — recorded workdirs outside the allowed roots are refused, and the flow falls back to the task's configured workdir with a warning toast; (2) sidebar/MobileNav live rows address sessions by instance id, so a session opened from there degrades to "not found" at completion — reopen via the task's Sessions section (ref-addressed surfaces fall back to history automatically); (3) Phase-5 leftovers: mobile full-screen polish, revive-or-delete `CardSession.tsx`, smoke-checklist update.

## Goals

1. **Watch** a running task's agent session live in the web UI.
2. **Steer** an active session by injecting a message.
3. **Review** a completed task's session transcript.
4. **Continue** a completed session with additional input.

## Why this is smaller than it looks

Nearly all plumbing already exists and is verified working:

- **Backend control plane is complete.** `/api/v1/control/runners/{r}/instances/{i}/…` covers session list/status/messages, prompt injection (with files, agent/model overrides), abort, permission responses, ad-hoc spawn/kill, and a per-instance SSE event stream (`internal/api/control.go`, routes at `internal/api/router.go:669-698`). Transport is the runner-dials-out WebSocket bridge with a hard endpoint allowlist on both sides (`internal/bridge/protocol.go:92-103`).
- **Live message streaming already flows end-to-end.** `HandleControlEvents` (control.go:438) attaches the browser to the OpenCode `/event` firehose: `permission.*`/`session.*` always flow; the full message-delta stream flows while a browser stream is open (`internal/runner/bridge_client.go:393-466`, `internal/bridge/hub.go:470-490` — everything arrives under the single SSE event name `instance_event`; switch on the inner OpenCode `type`). Pending permissions are replayed on connect; `stream_closed {reason:"instance_exited"}` signals teardown; 15s heartbeats.
- **The web client for all of it already exists, unused.** Typed wrappers `controlListSessions/Messages/Prompt/Abort/RespondPermission/PendingPermissions/SessionHistory/SpawnInstance/KillInstance/EventsUrl` in `web/src/lib/api.ts:1133-1240`, plus full `OcMessage/OcPart/OcEvent/OcPermission` types (`web/src/lib/types.ts:370-455`). Sole consumer today: `controlAbortTask` (`useTaskActionContext.ts:133`).
- **The UI seams are prepared.** `DockLeaf.kind:"session"` exists; sidebar **Live sessions** rows and MobileNav session pills route to `SessionFull`; drag-to-dock (`session-row` drag source) works. But `SessionFull` renders runner logs as a proxy with a dead mock composer, and `SessionLeaf` is an explicit placeholder — `docs/panes-v2-followups.md:118-121` tracks "full interactive PTY / prompt composer / permissions surfacing" as its own follow-up. **This plan is that follow-up.**
- **Task ↔ session linkage survives completion.** The runner stamps `metadata.sessions.{sid} = {runner_id, machine_id, hostname, workdir, timestamp}` on every task (`internal/runner/runner.go:1889-1905`) precisely "so remote control can locate and re-open it". Surfaced to the UI as `ResolvedTask.sessions`. Note: entries **accumulate** — an abandoned-then-resumed task has ≥2 sessions, possibly on different runners (metadata deep-merges one level, `storage/notes.go:159`).
- **Steering precedent exists.** Goal steering already injects prompts into live task sessions through the identical plumbing (`internal/apiserver/goal_steerer.go:103-122` for task→instance matching, `:73-76` for last-session pick), and the runner already protects injected turns from teardown (steered-turn completion hold, `process_manager.go:472-499`, `executor.go:475-494`, 10-min cap) — user-injected prompts get the same protection ("goal steering, control-plane send" are named together in the comment).
- **Auth is a non-issue.** Control routes need `control:*`; the PWA's PKCE flow already requests `scope: "mcp control"` (`web/src/lib/config.ts:12`). (Tokens minted before that scope string was added need one re-login.)

## Spike results (run 2026-08-18, opencode 1.18.18)

**Spike A — continuation works with zero backend changes.** Started `opencode serve` in a workdir, created a session, sent a prompt, killed the process. A **fresh** `opencode serve` in the same directory listed the old session, returned its full backlog, and accepted `POST /session/{id}/prompt_async` (204) — the new turn appended and ran. OpenCode sessions are machine-global (shared storage), and the runner's bridge proxy does not bind sessions to the instance that created them (`bridge_client.go:251-296`). So **"continue a completed session" = `controlSpawnInstance({workdir})` on the recorded runner + `controlPrompt(newInstance, oldSessionId, text)`** — both wrappers exist today. Caveat: agent/model on the spawn spec are accepted but ignored by `spawnAdhoc` (only workdir/title are read); per-turn overrides belong on `controlPrompt`, where they are honored (`control.go:261-268`).

**Spike B — the completed-task transcript fallback is broken on current OpenCode.** OpenCode ≥1.x stores sessions/messages/parts in **SQLite** (`~/.local/share/opencode/opencode.db`, tables `session`/`message`/`part` with JSON `data` columns) — verified by inspection. Brain's on-disk fallback (`internal/runner/session_history.go`) reads the **legacy** `storage/message/<sid>/*.json` layout, which 1.x sessions never write (verified: spike session absent from file storage, present in SQLite). Today the history endpoint works for a completed task only if some live OpenCode process still hosts the session. **Fix required (Phase 0)** — and it's small: brain already opens SQLite with the same driver (`github.com/glebarez/go-sqlite`, see `internal/storage/storage.go:19`).

**Also observed:** `GET /session/status` surfaces provider-retry states (`{type:"retry", attempt, message:"Provider is overloaded", next}`) — worth rendering as a status chip instead of a silent spinner.

## Product design

One shared component family, addressed by a new session-ref scheme, surfaced through the action registry.

### Addressing: `SessionRef` (the one real schema change)

Today `SessionFull` is keyed by `instanceId` resolved against the live-instance poll — a dead session renders "Session not found or already exited" (`SessionFull.tsx:22-58`), and the dock session-leaf target is `{instance_id, runner_id, project_id}`. History and continuation need addressing **without** an instance. Introduce a discriminated union in `lib/types.ts`:

```ts
type SessionRef =
  | { mode: "live";    runner_id: string; instance_id: string; session_id?: string }
  | { mode: "history"; runner_id: string; session_id: string;
      task_id?: string; project_id?: string; workdir?: string };
```

- `workspace.focusSessionId: string` becomes `focusSessionRef: SessionRef | null` (persisted; rehydrate coercion maps old strings to live-refs — `coerceDockTree` already tolerates unknown target fields, so no storage-key bump).
- Session dock leaves carry a `SessionRef` in `target`; a rehydrated live leaf whose instance is gone **falls back to history mode** via `session_id` instead of rotting ("Session not found").
- Resolution helpers in a new pure `lib/sessionRef.ts` (+ `node --test`): live path matches `listInstances()` on `task_id` and takes the last `session_ids` entry (mirrors `goal_steerer.go`); completed path enumerates `task.sessions`. Reuse `knownRunnerId(task)` (`taskActions.ts:87-97`), which already derives the session-recorded runner.

### The `Session` component family (`web/src/components/Session/`)

| Piece | Responsibility |
|---|---|
| `Transcript` | Ordered message list from `OcMessage[]`: user/assistant turns, tool-call parts (collapsible, status chip from `part.state.status`), reasoning parts (collapsed by default), step boundaries, errors. Autoscroll pinned-to-bottom with scroll-away detach. **Injected-prompt labeling:** user messages matching the goal-steering header (`## Goal check-in`) get an "injected · goal steering" chip; later, correlate `control.prompt_sent` audit events to label remote steers generically. |
| `Composer` | Textarea + Send (⌘↵) → `controlPrompt`. Abort button → `controlAbort`. Note under the field: *"Queues a message for the agent's next turn — it does not interrupt the current turn."* Optional agent/model override popover (data from `controlAgents`/`controlProviders`). **Check-in preset** (Decision 3): one tap inserts the goal-steering-style template built from the task's title + original request. |
| `PermissionBanner` | Pending permission requests (replayed on SSE connect + live `permission.*` events) with respond actions → `controlRespondPermission`. |
| `SessionHeader` | Status (busy/idle/**retrying**), executor badge, runner + `bridge_connected`, workdir, linked task chip → task modal. Phase 1 shows "updating every 5s"; the streaming live-dot appears only once real SSE lands (Phase 2), so polling never masquerades as streaming. |

Styling: extend the existing `session-view` block in `styles/global.css` (:936) and use `--p2-*` tokens throughout — also replacing the hardcoded hex currently in `SessionFull`/`CardSession`.

### Where it appears (corrected entry inventory)

Every **pre-routed** entry point today is live-only: sidebar Live-sessions rows, MobileNav pills, Statusbar count all filter `kind === "task" && status ∈ {starting, busy}` (`useSessions.ts`). `CardSession.tsx` is currently **dead code** (not imported by `ProjectCard`, whose tabs are Tasks/Features/Goals/Automations). So:

1. **`SessionFull`** (`Workspace/SessionFull.tsx`) — the primary surface. Replace the log-proxy body with the family; take a `SessionRef`. Live entries: sidebar rows, MobileNav pills (unchanged).
2. **`SessionLeaf`** (Focus dock pane) — same family, compact header; target is a `SessionRef`.
3. **Task surfaces — the primary entry for history mode and the task-centric path for live.**
   - `TaskModal` gains a **Sessions** section listing *every* `task.sessions` entry (timestamp + hostname + runner) with per-row **View** / **Continue** actions — after an abandonment+resume, the pre-abandonment transcript is exactly the one you want, so "newest wins" is only the default for the top-level verbs, never the only path. Each row gates on **its own** runner's connectivity ("runner amos offline — reconnect to view").
   - `TaskDetailLeaf` is read-only KV with no action wiring today (deferred-refactor item `followups.md:100-107`) — it gets the Sessions section only when that extraction happens; not sized into this plan.
   - Continuation instances (ad-hoc) must become visible: extend the sessions predicate to include live `adhoc` instances (labeled "continue: {task}"), and **include idle ones** — a continuation waiting for your next message is idle and would otherwise vanish between turns.

### Action-registry verbs (`lib/actions/taskActions.ts`)

Four new descriptors, following the house rules exactly (required `group`, closed set; disabled-with-reason, never hidden; sentence-case reasons that teach the next step):

| id | Label | Group | Key | Phase |
|---|---|---|---|---|
| `watch` | Watch session | `navigate` | `w` | 1 (read-only) |
| `transcript` | View transcript | `navigate` | `t` | 1 |
| `steer` | Steer session… | `run` | — | 3 |
| `continue` | Continue session… | `run` | — | 4 |

- **Gating data** (`bridge_connected`, instance `session_ids`, per-runner online state) is not on `Task`; per the pure-builder rule it enters via new query callbacks on `TaskActionContext` (precedent: `isSelected`), not a builder-signature change. New effect callbacks (`openSession(ref)`, `openTranscript(ref)`, `openContinue(taskId)`) live in `useTaskActionContextFactory` and follow the close-modal-then-navigate pattern of `openDetails`/`openLogs`.
- **disabledReason copy** (house style): "Executor is pi — session control requires an OpenCode task" · "Runner is offline — session unavailable until it reconnects" · "Session starting — the runner is still discovering it" · "No session recorded — discovery may have failed; check runner logs".
- **CommandPalette is an allowlist, not automatic** (`PALETTE_VERBS`, CommandPalette.tsx:41-51): add `watch` + `transcript` only (navigate verbs, cheap); steer/continue stay off the palette.
- **Mobile reality:** long-press on task rows runs Select (selection mode), not the sheet — on mobile these verbs are reached via the modal ActionBar, whose `primary` defaults to `["run","resume"]`; add `watch` to `primary` when the task is running so it isn't buried behind "More…".
- **Out of scope for selection/bulk:** session verbs are single-target; they deliberately do not appear in SelectionBar.
- **Tests:** the gate matrix (executor × bridge × session_ids × task.sessions) gets exhaustive `taskActions.test.ts` coverage like every other verb.
- Continue's confirm: plain-string `ActionConfirm` interpolating workdir + runner (precedent: abort). No new modal kind needed — the multi-session case is handled by the per-row actions in the Sessions section, not a picker.

### Data layer

- `lib/transcript.ts` — **pure** reducer: `applyEvent(messages, ocEvent)` upserting on `message.updated` / `message.part.updated` / `message.removed`, filtered by `sessionID` (events are per-instance). `node --test` coverage per house rule.
- `hooks/useSessionTranscript.ts` — react-query backlog under the `["v2", …]` key convention (`["v2","session-messages",ref…]`), mode-aware: live → `controlListMessages` + SSE; history → `controlSessionHistory` one-shot.
- **Streaming client is net-new code.** Only `parseSSEFrame` is reusable from `lib/sse.ts`; `MultiStream` is hardcoded to the task stream. Build a small `InstanceStream` class (fetch-based, `Authorization` header, abort + backoff), and **on stream 401 explicitly call `useAuth.onUnauthorized()`** — the existing once-refresh lives in `api()` and does not fire for hand-rolled streams. Retire `controlEventsUrl` (`?token=` EventSource helper) so one auth convention remains.
- One SSE connection per open session surface (the control stream is per-instance by design). Fine at realistic pane counts; the HTTP/1.1 6-connection cap only bites in dev with many panes open — document, don't engineer around it yet.
- Permission responses: the TS wrapper sends `{response:"once"|"always"|"reject"}`; a stale doc-comment on the handler says `allow|deny`; the handler is a pass-through — **pin the real OpenCode vocabulary in Phase 3** (wrapper believed correct).

## Backend changes

| # | Change | Where | Size |
|---|---|---|---|
| B1 (required) | SQLite transcript tier: in `fetchSessionHistory`'s fallback chain (`bridge_client.go:721-745` — live port → **new: `opencode.db` read-only query** → legacy files → lsof probe), with the reader itself beside the legacy one in `session_history.go`: query `message`/`part` by `session_id` (indexed — never table-scan; db can be 9 GB), assemble `{info,parts}[]` exactly like `GET /session/:id/message`. Fail soft to the remaining tiers; log schema version from `__drizzle_migrations`. | `internal/runner/session_history.go` + `bridge_client.go` | ~150 LOC + tests |
| B2 (optional, later) | Task-addressed `GET /control/tasks/{taskId}/session-ref` (extract `findTaskInstance` from `goal_steerer.go` into a shared service) so PWA/TUI/steerer share resolution | `internal/api`, `internal/apiserver` | small |
| B3 (optional, later) | Continuation lifecycle polish: idle auto-kill for continuation instances — deferred unless orphans accumulate (Decision 1); spawn rate limit 6/min already exists; `HandleControlKill` already permits ad-hoc kills | `internal/runner/bridge_client.go` | small |

Everything else is frontend.

## Phases

Each phase ships independently and is verifiable in the live UI.

- **Phase 0 — backend enabler.** B1 SQLite history tier. (Continuation mechanics already proven — Spike A.) *Exit: `controlSessionHistory` returns a transcript for a completed task after killing its serve process; verify with no other opencode process alive on the machine so the live tier can't mask B1 (the db is machine-global — an unrelated `opencode serve` from other work can answer instead).*
- **Phase 1 — read-only transcripts + task entry points.** `SessionRef` addressing; `lib/transcript.ts` + `Transcript`; history mode in SessionFull/SessionLeaf; **Sessions section + `watch`/`transcript` verbs** (read-only watch, 5s poll, "updating every 5s" header). *Exit: from a running task's modal, two clicks to its message feed; read any completed task's transcript from its Sessions section.*
- **Phase 2 — live streaming.** `InstanceStream` + delta reducer; busy/idle/retry header states; autoscroll pinning; `stream_closed` → history-mode transition; reconnect + explicit 401 refresh. *Exit: watch a task stream token-by-token; kill the runner and see a clean offline state; steering injections from goals appear with the "injected" chip.*
- **Phase 3 — steering.** Composer + `controlPrompt`, abort, `PermissionBanner`, queued-turn notice, `steer` verb + full gate matrix, permission-vocabulary pin, `control.prompt_sent` toast ("Delivered — the agent acts on it next turn"). *Exit: steer a deliberately long-running demo task (sleep-loop prompt) from the PWA and watch the agent act on the injection.*
- **Phase 4 — continue completed sessions.** `continue` verb → confirm (workdir + runner) → **workdir existence check with fallback**: worktree-mode workdirs are force-removed at feature checkout (`builtin_feature_checkout_simple.go:238`), and `validateSpawnWorkdir` requires the dir to exist — on failure offer the task's repo root with an explicit "original worktree was removed — file references in the old session may not resolve" warning. Then `controlSpawnInstance` + `controlPrompt(oldSessionId)`; surface flips to live; sessions predicate extended so the ad-hoc continuation instance (busy **or idle**) is visible everywhere, labeled "continue: {task}"; kill affordance in its header. *Exit: complete a demo task, kill its processes, continue its session with a follow-up question and get an answer.*
- **Phase 5 — polish.** Mobile pass (full-screen SessionFull, sticky composer), keyboard accelerators, revive-or-delete decision on dead `CardSession.tsx`, smoke-test checklist update, docs. Optional: multi-session tab groups in Focus.

## Risks & gotchas

- **Runner must be online for everything** — live stream, history (it reads the runner's disk), prompt, spawn all 502 when the bridge is down. Gate per the **recorded** runner of each session entry (`task.sessions[].runner_id`), not just "a" runner; show hostname so a half-openable multi-runner task is explicable.
- **Injection queues; it cannot preempt.** Abort-then-prompt is the "interrupt" story. Driver-exit race: a prompt sent after the final turn ends but before teardown may be lost — the steer-hold only protects turns that *started*. UI: warn when the task is completing; never claim more than "delivered" (delivery ≠ effect).
- **Deleted worktrees break naive continuation** (see Phase 4) — this is the common case for merged feature tasks, not an edge case.
- **Session discovery can fail silently** (~5×2s attempts, log-only failure): a task can complete with a transcript in `opencode.db` but no `task.sessions` entry. Copy stays honest ("discovery may have failed"); a workdir-based db lookup is a possible B1 extension.
- **Pi tasks have no session surface at all** (no port, no session id, JSONL protocol). Disabled-with-reason now. Future: `PiRPCProcess` already parses a `PiEvent` stream (`Events()` channel, currently test-only) — pumping it through the bridge as a read-only transcript is plausible later work.
- **Schema drift**: OpenCode's SQLite schema is internal to OpenCode; B1 fails soft and logs the migration version. The bridge allowlist pins the HTTP surface we depend on.
- **Stale tokens**: logins predating the `control` scope addition silently lack access — map 403 on control routes to a "re-login needed" toast.
- **Dev-proxy buffering**: verify the Vite proxy doesn't buffer the control SSE path (the task stream already works through it).

## Addendum: Archive & finished-work overflow (added 2026-08-18)

Second scope, same plan: completed features crowd the Overview, and the FINISHED lane can't show more than 4 rows. Verified against code and reproduced live in the demo UI.

### What's actually happening

- **The lane cap is a JS slice, not a scroll problem.** Every Overview lane renders `items.slice(0, 4)` (`OverviewGrid.tsx:324`); "+N more" is a bare non-clickable `<span class="lane-more">` (`OverviewGrid.tsx:342-346`) — rows 5..N are never in the DOM, so no amount of scrolling can reveal them.
- **`archived` already exists.** It's a valid entry status (`internal/types/types.go:48-59`), accepted by every update endpoint including bulk-update, already a terminal status for resume/checkout logic, and the TUI already renders archived tasks in a collapsed Inactive section. Goals archive exactly this way (`goal_archive` = PATCH `status:"archived"`; default listings hide it, `?status=archived` reveals it).
- **But naive completed→archived breaks five things** (verified, `internal/…`): ① `CheckFeatureCompletion` counts only completed|validated — archived tasks fall out and feature-progress counts shrink (`event_service.go:276`); ② `ComputeFeatureStatus` puts archived in no bucket — a fully-archived feature computes **"pending"** and reappears as active work (`features.go:52-93`); ③ an active goal over that scope sees zero complete tasks → `need_work` → **regenerates a task** (`goal_service.go:94-127`); ④ `finalizeAutomationRun` rewrites a completed automation run's audit to `cancelled` (`entries.go:544-560`); ⑤ `completionStamp` clears `completed_at` (`brain.go:1544-1565`). The client-side mirror of ② exists too: `deriveFeatures` counts archived in `total` but no bucket, deflating progress and demoting a merged feature back to in-progress — the opposite of decluttering.

### Design: `archived` status + "settled" semantics

Keep the existing status (no new transport, TUI parity for free), and give archived a consistent meaning: **settled — counts as done for logic, excluded from view for UI.**

**Backend semantics (A1):**
- `CheckFeatureCompletion` done-set and goal `defaultCompleteStatuses` gain `archived` (goals already support per-goal `complete_statuses` overrides).
- `completionStamp` preserves `completed_at` on completed/validated→archived (archive is not un-completing).
- `finalizeAutomationRun` stops rewriting already-finalized completed runs on archive.
- `ComputeFeatureStatus`/`FeatureTaskStats` exclude archived from totals; all-tasks-archived → feature status `archived`.
- Accepted (desirable) side effect: archived recurring/trigger tasks stop re-firing on cron/events — archive doubles as retirement.

**UI derivation (A2):** `lib/features.ts` excludes archived tasks from totals/progress; a feature whose tasks are all archived derives no feature at all — it leaves the lanes and cards entirely. Reachable afterward via the Archived filter chip and `/` query (`status:arch` already matches).

**Verbs (A2)** — follow the `goalActions.ts` archive precedent exactly (`id:"archive"`, `group:"state"`, reversible-tier confirm, body ends "restore it later from the Archived filter"):
- **Task `archive`**: enabled for terminal statuses (completed/validated/cancelled/superseded); reason otherwise: "Task is still active — archive is for settled work." Explicit `unarchive` verb on archived rows (status picker already allows it; the verb makes it discoverable).
- **Feature `archive`**: enabled for settled lifecycles (`merged`/`finished`, the existing `SETTLED_LIFECYCLES` set); runs `setStatusForAll(feature, "archived")` through the existing per-source-status bulk baton + dry-run → confirm → 409/force ladder — zero new fan-out machinery.
- **Bulk archive** in SelectionBar beside Delete. Note: `bulk-update` is filter-only today — either add `paths` support server-side (mirroring bulk-delete) or loop `updateEntry` per path chunked; prefer the server addition (small, symmetric).
- **Where**: task/feature context menus, modal ActionBars, FeatureDrawer; palette gets feature-level `archive` only.

**Visibility (A2):**
- Sidebar gains an **Archived** chip (`StatusFilter` union + `statusFilter.ts` case). Default: all views exclude archived tasks at the task level, with an "N archived" expander row modeled on the existing merged-features fold (`CardFeatures.tsx:75-85` + persisted `mergedExpanded` pattern).
- Counts: the Done chip keeps counting completed+validated; archived is its own count.

**Lane overflow fix (A0 — independent quick win):**
- "+N more" becomes a button; clicking expands the lane in place with `.lane-items` capped (`max-height: ~40vh`, `overflow-y: auto`) so the board stays balanced and long lanes finally scroll. Collapse toggles back. Same behavior in the mobile stacked layout. Lane headers already show the true count.

### Phases (Track A, parallel to the session track)

- **A0 — lane overflow fix.** Frontend-only, ~small. *Exit: with 80 finished features, click "+76 more" and scroll the full list; every row keeps its context menu.*
- **A1 — archived-as-settled backend semantics.** The five fixes above + tests. *Exit: archive a completed task of a checked-out feature → no goal regeneration, no automation-run rewrite, `completed_at` intact, feature stats don't regress.*
- **A2 — archive verbs + visibility.** Derivation exclusion, task/feature/bulk verbs, Archived chip, per-card archived fold, unarchive. *Exit: archive a whole merged feature in two clicks → it vanishes from lanes/cards; find and restore it via the Archived chip.*

### Risks

- **Active goals over archived scopes**: A1's `complete_statuses` addition covers default goals; goals with explicit custom `complete_statuses` need the archive confirm to warn when an active goal targets the feature ("An active goal watches this feature — it may regenerate work").
- **Ordering matters**: ship A1 before (or with) A2 — verbs without the semantics fixes produce the pending-feature regression the recon found.
- **Old clients/TUI**: TUI already handles archived rendering; no coordination needed beyond normal deploy order (API first per amos deploy convention).

## Decisions (2026-08-18)

1. **Continuation lifecycle: keep-alive.** Continuation instances stay up until manually closed — visible in Live sessions (labeled "continue: {task}"), one-click kill in the session header. No idle auto-kill in Phase 4; B3 adds it later only if orphans accumulate in practice.
2. **ActionBar `primary` promotes `watch` for running tasks.** On mobile, a running task's modal shows Watch session beside Run/Resume instead of burying it behind "More…"; non-running tasks keep the default `primary` set.
3. **Composer ships a check-in preset.** A one-tap preset inserts the goal-steering-style template ("## Check-in" + task title + original request + "self-assess progress and correct course; only complete if truly done"), mirroring `buildGoalSteeringPrompt`'s shape so steered agents see a familiar format whether the nudge came from a goal or a human. Lands with the composer in Phase 3.
