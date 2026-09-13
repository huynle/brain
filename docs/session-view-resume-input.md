# ADR: Session-view always-on input with auto-routing (live=steer, finished=resume-with-context)

- **Status:** Accepted (design)
- **Date:** 2026-09-08
- **Feature:** `session-view-resume-input`
- **Brain ADR:** `projects/brain-api/decision/7ihrqpi4.md`
- **Task:** `projects/brain-api/task/dr595zpw.md`
- **Scope:** dashboard PWA (`web/`) only. No new backend endpoint required.

## Context / Motivation

Live incident 2026-09-08: in the dashboard Session view a FINISHED session shows the
transcript but the input prompt is inert — replaced by a static note *"This is a recorded
transcript — the process is gone."* A user typed a `SUPERVISOR CONTEXT` message into a finished
session (it rendered as the trailing USER turn) but nothing acted on it because the process was
gone.

Desired behavior: **every** Session view has a working input prompt that steers or continues the
work, regardless of session status; the box auto-decides its route from session liveness:

- **LIVE session** → existing live steer/inject (`prompt_async`) into the running session.
- **FINISHED/dead session** → the supervisor-session-resume backend (`resume-with-context`),
  which relaunches the task (rehydrate or same-session) with the typed text as `injected_context`.

## Decision (summary)

1. Make the Session-view input **always render**. Remove the "render Composer only when
   `canSteer && live`" gate (`web/src/components/Session/SessionPane.tsx:193-204`); render an
   input in all steerable-or-resumable states.
2. **Auto-route on submit** from the liveness signal already computed by the view:
   - live → `controlPrompt` (unchanged `prompt_async` path).
   - finished/dead → new FE `resumeTaskWithContext(project, taskId, {injected_context, prefer_same_session:true})`.
3. **Session→task resolution is entirely client-side** — no new endpoint. History refs already
   carry `{task_id, project_id}`; live refs resolve `{task_id, project_id}` off the matching
   `allInstances` row by `instance_id`.
4. **Reuse the existing `resume-with-context` backend as-is.** The only net-new backend-facing
   work is a thin FE api-client function + TS types (mirroring the Go types and MCP tool).

This is a **reuse-heavy** change: both routes' backends exist and are verified. The build is
confined to the FE (routing logic in the Session body + one api-client function + types + UX).

## Decision 1 — Liveness signal the UI keys off

**Signal:** the `delivery` field returned by `useSessionTranscript(sref)`
(`web/src/hooks/useSessionTranscript.ts:136-142`), combined with the `SessionRef` mode. Same
signal the current `sessionSteerState()` already consumes — no new liveness probe.

| Condition | Meaning | Route |
|---|---|---|
| `sref.mode==="live"` && `session_id` present && `delivery !== "ended"` | live now (`sessionSteerState().canSteer===true`) | **STEER** (`controlPrompt`) |
| `sref.mode==="history"` | recorded transcript, process gone | **RESUME** (`resume-with-context`) |
| `sref.mode==="live"` && `delivery==="ended"` | streamed then the instance exited under us | **RESUME** |
| `sref.mode==="live"` && no `session_id` yet (`starting`) | session id not yet known | **input disabled** (transient) |

`delivery` already distinguishes "streaming now" from "ended"/"none", and
`onStreamClosed → setEnded(true)` (`useSessionTranscript.ts:131`) is exactly how a session dies
underneath us mid-view — so a session that ends while the user is looking flips STEER→RESUME
with no extra polling. We deliberately do NOT depend on a separate `/sessions/status` call for
routing (fewer round-trips, single source of truth). The instance registry
(`useSessions().allInstances`) is used only for task resolution (Decision 3), not for liveness.

## Decision 2 — Routing logic on submit

Extend the pure decision function (today `sessionSteerState` in
`web/src/lib/sessionRef.ts:147-173`) into a route classifier — kept pure so it stays
unit-testable:

```ts
type SubmitRoute =
  | { kind: "steer"; target: { runner_id; instance_id; session_id } }   // live
  | { kind: "resume"; project_id: string; task_id: string }             // finished, task known
  | { kind: "disabled"; reason: string };                               // starting / no owning task

function sessionSubmitRoute(sref, delivery, owner /* {project_id?,task_id?} */): SubmitRoute
```

- **live** → `{kind:"steer", target}` → `controlPrompt(runner,instance,session,{text})`.
- **finished** (history ref, or live ref with `delivery==="ended"`) with an owning task →
  `{kind:"resume", project_id, task_id}` →
  `resumeTaskWithContext(project, taskId, {injected_context: text, prefer_same_session: true})`.
- **finished but NO owning task** (adhoc/continuation session — Decision 3 GAP) →
  `{kind:"disabled", reason:"This session isn't attached to a task, so it can't be resumed."}`.
- **starting** (live, no session_id) → `{kind:"disabled", reason:"Waiting for the session id…"}`.

`SessionPane.tsx:193-204` calls the classifier and renders one always-present input; the input
is `disabled` (not hidden) for the two disabled cases, with the reason as helper text.

## Decision 3 — Session→task resolution (client-side, no new endpoint)

`resume-with-context` is addressed by `{project, taskId}` and **internally re-discovers the
session** — the FE never passes a session id to it. So the only requirement is to turn the
current `SessionRef` into `{project_id, task_id}`:

- **History ref** → already carries `task_id` + `project_id` (`web/src/lib/types.ts:626-627`,
  built in `historySessionRefs()`/`instanceTranscriptRef()`). Use directly.
- **Live ref (ended)** → look up the matching row in `useSessions().allInstances` by
  `instance_id`; `OpencodeInstance` carries `task_id` + `project_id`
  (`internal/types/types.go:1933-1958`, TS `web/src/lib/types.ts:425-449`). `SessionPane`
  already fetches this row (`SessionPane.tsx:98-102`).

**GAP — task-less sessions:** adhoc/continuation instances (`kind==="adhoc"`, empty `task_id`)
and history refs without `task_id` have no owning task; resume-with-context cannot address them.
**Decision:** render the input **disabled** with the reason above — we do NOT invent a
session-scoped resume path in this feature.

**Decision: no new `sessionId→{project,taskId}` endpoint.** It would duplicate data the FE
already holds and add a round-trip. Revisit only if a surface appears with a bare session id and
neither a history ref nor a loaded instance row.

## Decision 4 — `prefer_same_session` default and advanced toggle

**Verified source fact:** `HandleResumeWithContext` decodes the JSON body straight into
`ResumeWithContextOptions` (`internal/api/tasks.go:1058-1062`) with **no default applied**;
`PreferSameSession bool json:"...,omitempty"` decodes an **absent field to `false`**. The
docstring's "defaults to true when absent" is aspirational and NOT enforced at decode — the MCP
tool `resume_task_with_context` compensates by explicitly sending `true`
(`internal/mcp/task_tools.go:1911`).

**Decision:** the FE **always sends `prefer_same_session: true`** explicitly.

**Advanced toggle:** NOT exposed in v1. `prefer_same_session` is advisory — the runner makes the
authoritative same_session-vs-rehydrate call via `CanResumeSession` (OpenCode true-resumes
durable on-disk history; Pi always rehydrates). `executor_override` and `force` are likewise not
exposed (supervisor/MCP concerns, not a per-message steer).

## Decision 5 — UX affordances

- **Always visible.** One input box at the bottom of every Session view, replacing the current
  static-note branch. When not actionable it is **disabled with helper text**, never removed.
- **Adaptive label/placeholder** driven by the route:
  - steer → placeholder "Steer this session…", button "Send".
  - resume → placeholder "Resume with this context…", button "Resume".
  - disabled(starting) → "Waiting for the session id…".
  - disabled(no task) → "This session isn't attached to a task, so it can't be resumed."
- **Confirm before relaunch:** YES for the resume route (relaunching a dead task is heavier than
  a live nudge). Inline confirm ("Resume this task with your context? It will relaunch and
  continue.") before firing. The steer route sends immediately (parity with today's Composer).
- **Feedback = the returned `resume_mode`:**
  - `live_injected` → "Injected into the running session — it acts next turn." (the task was
    actually still live; no relaunch happened — the endpoint, not a history assumption, is the
    source of truth).
  - `same_session` → "Resuming the previous session with your context…"
  - `rehydrate` → "Relaunching with a fresh session seeded with your context…"
  - live steer route keeps "Delivered — the agent acts on it next turn."
- **Loading/error/empty states:** disable input + button spinner while in-flight; surface
  `ApiError.message` on failure (400 empty context, 404 task-not-found, live-claim-safety
  refusal) inline, leaving the typed text intact for retry. Empty/whitespace text → button
  disabled.
- **Injected-turn precedent:** the resumed context re-appears in the transcript as an injected
  USER turn — `Transcript.tsx:304-318` already renders an "injected" pill, so no new transcript
  rendering is needed.

## Reuse-vs-build matrix

| Piece | Reuse | Build |
|---|---|---|
| Live steer (`prompt_async`) backend + `controlPrompt` FE caller | reuse (unchanged) | — |
| `resume-with-context` backend (handler + service + live-inject) | reuse (unchanged) | — |
| Liveness signal (`delivery` from `useSessionTranscript`) | reuse (unchanged) | — |
| Instance registry for task resolution (`allInstances`) | reuse (already loaded) | — |
| Session→task mapping | reuse (derivable client-side) | — (no endpoint) |
| Transcript "injected" pill | reuse (exists) | — |
| FE api-client `resumeTaskWithContext` + TS types | — | build (thin, mirror `resumeTask`/Go types) |
| Route classifier (extend `sessionSteerState`) | — | build (pure fn, unit-tested) |
| Always-render input + adaptive UX in `SessionPane` | — | build (replace `:193-204` gate) |
| Confirm-before-resume + resume_mode feedback toasts | — | build (small) |

## Consequences

- **Positive:** the finished-session dead-end that caused the incident is closed; one mental
  model (type → it continues the work) across live and finished sessions; almost no backend
  surface added; the routing decision stays a pure, testable function.
- **Negative / limitations:**
  - Task-less (adhoc/continuation) sessions remain non-continuable by design — input disabled.
  - `prefer_same_session`/`executor_override`/`force` are not user-tunable in v1.
  - The relaunch route is asynchronous (status→pending, runner re-spawns on next poll); the
    continued transcript appears when the new session starts, not instantly — the toast must set
    that expectation.
  - `resume_mode` can surprise the user (typing into a "finished" view may report
    `live_injected` because the claim lapsed while the process kept running) — the toast wording
    handles this honestly.

## Follow-on tasks (implementation/tests/verify depend on this ADR)

1. FE api-client `resumeTaskWithContext` + `ResumeWithContextOptions`/`ResumeWithContextResult`
   TS types (mirror Go `internal/types/types.go:1836-1854` and MCP `task_tools.go:1877`).
2. Route classifier `sessionSubmitRoute` (extend `web/src/lib/sessionRef.ts`) + unit tests.
3. Session→task resolver (history ref direct; live-ended via `allInstances`) + the
   task-less-session disabled case.
4. Always-render input + adaptive label/placeholder/confirm/feedback in `SessionPane.tsx`
   (replace the `:193-204` gate; generalize or wrap `Composer`).
5. Verify end-to-end: live steer unchanged; finished→resume returns a mode and the task
   relaunches; task-less session shows disabled input.
