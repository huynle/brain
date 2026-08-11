# Task & Feature Actions — gap analysis + implementation plan

Status: **implemented** (all phases). See §10 for what shipped and the
two places the implementation deviates from this plan.
Scope: `web/` (panes-v2 PWA), with a small amount of `internal/api` work.

The PWA can *look at* tasks and features but barely *act* on them. This
document inventories the gap between what the API offers, what the typed
client already wraps, and what a user can actually reach — then lays out
the work to close it.

---

## 1. The headline finding

**The client layer is largely already built. The UI never wired it up.**

`web/src/lib/api.ts` exports typed, working wrappers for most of the
missing verbs. They have **zero call sites** anywhere in the component
tree:

| Wrapper | Endpoint | Call sites in UI |
|---|---|---|
| `setTaskStatus` | `PATCH /entries/{path}` | **0** |
| `deleteEntry` | `DELETE /entries/{path}?confirm=true` | **0** |
| `getEntryRaw` / `updateEntryRaw` | `GET/PATCH` w/ `text/x-brain-full` | **0** |
| `createEntry` | `POST /entries` | **0** |
| `moveEntry` | `POST /entries/{path}/move` | **0** |
| `releaseDispatchLease` | `DELETE .../dispatch-lease` | **0** |
| `getDispatchLease` | `GET .../dispatch-lease` | **0** |

`ALL_STATUSES` is likewise defined in `lib/types.ts` and never imported.
`@uiw/react-codemirror` + the `@codemirror/*` and `@replit/codemirror-vim`
packages are in `package.json` and imported **nowhere** in `src/`.

So a meaningful slice of this work is wiring, not building.

---

## 2. What already works in our favour

Worth stating up front, because it removes plumbing from the plan:

- **Live refresh is already correct for every mutation path.**
  `internal/realtime/bridge.go` subscribes to `entry.*` and republishes a
  fresh `tasks_snapshot` for the project. `PATCH`, `DELETE`, and
  `bulk-update` all emit `entry.*`, so any mutation propagates to every
  connected client with no cache-invalidation code. The web SSE reducer
  only handles `tasks_snapshot` — that's sufficient.
- **Status changes already emit the right domain event.**
  `HandleUpdateEntry` emits `task.status_changed` (not just
  `entry.updated`) when a task's status actually changes, so automations
  and the feature cascade react correctly to UI-driven edits.
- **`ContextMenu` already supports what destructive menus need** —
  `danger`, `disabled`, and `separator` are implemented in
  `components/common/ContextMenu.tsx` and used by nobody.
- **`useIsMobile` exists** (`hooks/useIsMobile.ts`), currently used only
  by `Dashboard.tsx`.
- **`POST /entries/bulk-update` accepts `filter: {feature_id, project}`**
  plus `dry_run` — exactly the primitive a feature-wide status change
  needs. No new endpoint required for that verb.

---

## 3. Gap inventory

### 3.1 Missing verbs

| # | Gap | Today | Notes |
|---|---|---|---|
| G1 | **Change a task's status** | Nothing, anywhere | `setTaskStatus` + `ALL_STATUSES` both exist unused |
| G2 | **Cancel a task** | Nothing | TUI has `X`. Is a status transition, not a delete |
| G3 | **Delete a task** | Nothing | TUI has `⌫`. `deleteEntry` exists unused |
| G4 | **Change a feature's status** | Nothing | Needs fan-out; `bulk-update` filter supports it |
| G5 | **Cancel / delete a feature** | Nothing | No bulk-delete endpoint exists (see B1) |
| G6 | **Create a task** | Nothing | `createEntry` exists unused |
| G7 | **Edit task metadata** | Nothing | TUI `s` opens a 4-tab modal; web has no equivalent |
| G8 | **Edit task content** | Nothing | TUI `e`; `getEntryRaw`/`updateEntryRaw` exist unused |
| G9 | **Move a task to another project** | Nothing | `moveEntry` exists unused |
| G10 | **Release a stuck dispatch lease** | Nothing | Surfaces in toasts as `already_leased` with no recovery |

### 3.2 Reachability gaps

These matter as much as the missing verbs — several actions that *do*
exist are effectively invisible.

| # | Gap | Detail |
|---|---|---|
| R1 | **No touch access to any action** | Every row action is behind `onContextMenu`. There is no `onTouchStart` / long-press anywhere in `components/Workspace/`. On a phone, right-click doesn't exist — so Run task, Run feature, Checkout, Resume and Plan drawer are all unreachable. Directly contradicts the mobile-parity goal. |
| R2 | **No keyboard access to any row action** | No `tabIndex`, `onKeyDown`, `role`, or roving focus on `.trow` / `.feat-head`. `useGlobalKeyboard` handles only ⌘K / ⌘/ / ⌘B / ⌘1 / ⌘2. Nothing k9s-like. |
| R3 | **Command palette has no verbs** | `CommandPalette.tsx` emits navigation only ("Go to project", "Feature: X", "Task: Y"). No "Run", "Cancel", "Delete". For a keyboard-first user this is the natural home for verbs. |
| R4 | **Orphan task rows have no menu at all** | In `CardTasks.tsx`, tasks in the "No feature" group render with `onClick` only — no `onContextMenu`. Grouped rows get a 4–5 item menu; ungrouped rows get nothing. |
| R5 | **Resume is buried** | `TaskModal` exposes it as a generic `Actions…` button that opens a second modal. Two clicks and a vague label for the one action an abandoned task needs. |
| R6 | **Run is inconsistent** | Three entry points with three labels: "Run now" (TaskModal), "Run task now" (context menu), "Run next ready feature" (Overview). Feature-level Run exists only in the context menu and `FeatureActionsModal`. |

### 3.3 Safety gaps

| # | Gap | Detail |
|---|---|---|
| S1 | **No confirmation primitive** | `FeatureActionsModal` hand-rolls a `confirmForce` view. There's no reusable confirm dialog, so every new destructive action would reinvent one. |
| S2 | **No guard on in-flight work** | Neither the UI *nor the server* refuses to delete or re-status a task that a runner is actively executing. Delete only requires `?confirm=true`. |
| S3 | **No undo** | Nothing records a prior value, so a mis-click on a bulk status change is unrecoverable by hand. |

### 3.4 Backend gaps

| # | Gap | Detail |
|---|---|---|
| B1 | **No bulk delete** | `bulk-update` can only *update*. Deleting a feature means N× `DELETE /entries/...` from the client — non-atomic, partially-failing, and N round-trips. |
| B2 | **`bulk-update` silently truncates at 100** | `Limit` defaults to 100 and is capped at 100. A feature with more tasks would be partially updated with no error. |
| B3 | **Bulk mutations cause an SSE storm** | The bridge fires one `GetTasks` + broadcast **per `entry.updated` event**. A 100-task bulk update triggers 100 full task-list queries, each fanned out to every subscriber. |

---

## 4. Decisions taken

1. **Feature delete is cancel-first.** The primary destructive verb sets
   every task in the feature to `cancelled` (reversible, preserves
   history). Hard delete is a separate action behind a type-the-name
   confirmation.
2. **Scope is verbs + metadata editor.** Run / status / cancel / delete /
   resume everywhere, plus a field-level metadata editor at TUI `s`
   parity. The raw frontmatter/body editor (TUI `e`) is explicitly
   deferred.
3. **Keyboard and touch land alongside the desktop menu**, from one
   shared action registry, rather than being retrofitted later.

---

## 5. Architecture: one action registry, four renderers

The core move. Every verb is described once as data, and each surface
renders the same list:

```
lib/actions/taskActions.ts     ─┐
lib/actions/featureActions.ts  ─┤
                                ├─→  ActionDescriptor[]
                                │
                                ├─→ ContextMenu        (desktop right-click)
                                ├─→ ActionSheet        (touch long-press)
                                ├─→ Modal footer       (TaskModal / FeatureModal)
                                └─→ CommandPalette     (keyboard verbs)
```

```ts
export interface ActionDescriptor {
  id: string;
  label: string;                    // imperative: "Cancel task"
  group: "run" | "state" | "edit" | "danger" | "navigate";
  /** Single-key accelerator, k9s style. */
  key?: string;
  /** Present ⇒ rendered disabled, with this as the tooltip. */
  disabledReason?: string;
  danger?: boolean;
  /** Present ⇒ route through ConfirmDialog before running. */
  confirm?: { title: string; body: string; typeToConfirm?: string };
  run: () => Promise<void>;
}
```

Why this shape:

- **Disabled-with-reason, never hidden.** A user who can't cancel a
  completed task should see *why*, not wonder where the item went. The
  existing `computeTaskResumeState` already produces exactly this kind of
  reason string — it becomes one contributor to the registry.
- **One place to add a verb.** New action ⇒ one descriptor ⇒ appears in
  all four surfaces automatically.
- **Testable without React.** Same discipline as `lib/features.ts` and
  `lib/depTree.ts`: pure functions, `node --test`.

---

## 6. Phases

### Phase 0 — foundations

| File | Work |
|---|---|
| `lib/actions/types.ts` | `ActionDescriptor` + shared helpers |
| `lib/actions/taskActions.ts` | `buildTaskActions(task, ctx)` — pure |
| `lib/actions/featureActions.ts` | `buildFeatureActions(feature, tasks, ctx)` — pure |
| `lib/api.ts` | Add `bulkUpdateFeature()`, `deleteFeatureTasks()`; keep `setTaskStatus`/`deleteEntry` |
| `components/common/ConfirmDialog.tsx` | Reusable confirm, optional type-to-confirm, busy state |
| `components/common/ActionList.tsx` | Renders `ActionDescriptor[]` into menu / sheet / footer |

Tests: `taskActions.test.ts`, `featureActions.test.ts` — one case per
disabled-reason branch.

### Phase 1 — task verbs

- Status submenu built from `ALL_STATUSES`, current status marked and
  disabled.
- `Cancel task` — status → `cancelled`, confirm if `in_progress`.
- `Delete task` — `deleteEntry`, type-to-confirm, blocked with a reason
  while a live claim exists (mirrors the server's live-claim safety in
  `ResumeTask`).
- `Run now` / `Resume` — move existing calls behind the registry so all
  three call sites agree on label and behaviour. Fixes **R5**, **R6**.
- Wire the registry into `CardTasks` grouped rows **and orphan rows**
  (fixes **R4**) and into `TaskModal`'s footer.

### Phase 2 — feature verbs

Feature-level semantics need stating plainly in the UI, because a feature
is not a stored entity:

- `Run feature` — existing `runFeature`.
- `Set status for all tasks…` — `bulk-update` with
  `filter.feature_id`. **Runs `dry_run: true` first** and shows
  "will update N of M tasks" before committing. Also surfaces the
  100-task cap (**B2**) instead of silently truncating.
- `Cancel feature` — the above, pinned to `cancelled`.
- `Delete feature` — fan-out delete, behind type-the-feature-name
  confirmation, with per-task progress and an explicit partial-failure
  report ("7 of 9 deleted; 2 failed: …"). Non-atomic by construction
  until **B1** is addressed.
- Route `Checkout`, `Assign runner`, `Resume abandoned`, `Blocked
  inspector` through the registry so `FeatureActionsModal`, the context
  menu, and the drawer stop diverging.

### Phase 3 — reach

- **Touch:** long-press (~500 ms, movement-cancelled) on `.trow` and
  `.feat-head` opens an `ActionSheet` — a bottom sheet rendering the same
  descriptors. Gated on `useIsMobile`. Fixes **R1**.
- **Keyboard:** roving `tabIndex` across rows within a card; `↑/↓` move,
  `Enter` opens detail, `x` run, `d` delete, `s` status, `Space`
  select. Rows get `role="row"` / `aria-selected`. A `?` overlay lists
  the keys, mirroring the TUI help bar. Fixes **R2**.
- **Palette:** extend `CommandPalette` to emit verbs for the focused /
  matching task or feature, not just navigation. Fixes **R3**.

### Phase 4 — metadata editor

Mirrors `internal/tui/metadata_modal.go`, which is the reference
implementation — including its tab split:

| Tab | Fields |
|---|---|
| Task | `status`, `priority`, `feature_id`, `move_to_project` |
| Execution | `agent`, `model`, `executor`, `target_workdir`, `execution_mode`, `complete_on_idle` |
| Git & Merge | `git_branch`, `merge_target_branch`, `merge_policy`, `merge_strategy`, `remote_branch_policy`, `open_pr_before_merge` |
| Feature *(feature mode only)* | `feature_priority`, `feature_depends_on`, `feature_schedule`, … |

Enum fields render as selects driven by the same constants the server
validates against. `feature_depends_on` is a multi-select over sibling
feature ids — which also makes the dependency trees from the previous
change **editable**, not just viewable.

Saves go through `updateEntry` (single) or `bulk-update` (feature mode).

### Phase 5 — backend follow-ups

Not blocking, but each removes a sharp edge:

- **B1** `POST /entries/bulk-delete` mirroring `bulk-update`'s
  filter + `dry_run` + result-list shape. Makes feature delete atomic and
  one round-trip.
- **B3** Debounce the `entry.*` → snapshot bridge (~100 ms coalescing
  window per project). Turns a 100-entry bulk update from 100 broadcasts
  into one.
- **S2** Refuse `DELETE` on a task holding a live claim from an online
  runner, reusing the check `ResumeTask` already performs.
- **B2** Return an explicit error when a bulk filter matches more than
  `limit`, instead of truncating.

---

## 7. Risks

| Risk | Mitigation |
|---|---|
| Bulk status change hits the wrong tasks | `dry_run` preview with exact counts before every fan-out; filter is always `{project, feature_id}`, never status-only |
| Feature delete partially fails | Per-task result list surfaced in the UI; retry offered for the failures; **B1** removes this entirely |
| SSE storm during bulk ops (**B3**) | Ship the debounce with Phase 2, not Phase 5, if a feature exceeds ~25 tasks in practice |
| Destructive action reachable by one keystroke | `d` opens the confirm dialog; it never deletes directly |
| Registry becomes a dumping ground | `group` is a closed union; surfaces render groups in fixed order |

---

## 8. Test plan

- **Pure:** `taskActions.test.ts` / `featureActions.test.ts` — every
  enabled/disabled branch, every `disabledReason`, every `confirm` gate.
  Follows the existing `features.test.ts` / `depTree.test.ts` pattern.
- **Integration:** against the isolated preview brain
  (`~/.brain-rc-preview`, API :3410, web :5190) — status round-trip,
  cancel, delete, feature bulk status with dry-run, feature delete with
  an induced partial failure.
- **Reach:** keyboard-only walkthrough of every verb; touch-viewport
  long-press walkthrough; both verified in the browser, not by hand.
- **Regression:** `npm test` (169 today) + `npx tsc -b --noEmit`.

---

## 9. Sequencing

Phases 0 → 1 → 2 deliver the interactions asked for. Phase 3 makes them
reachable on the surfaces that matter. Phase 4 is the editing pass.
Phase 5 hardens the server and can land in parallel with any of them.

---

## 10. What shipped

### New modules

| File | Role |
|---|---|
| `web/src/lib/actions/types.ts` | `ActionDescriptor` + ordering / grouping / key dispatch |
| `web/src/lib/actions/taskActions.ts` | Task verb matrix (pure) |
| `web/src/lib/actions/featureActions.ts` | Feature verb matrix (pure) |
| `web/src/lib/actions/metadataFields.ts` | Field schema + diffing (pure) |
| `web/src/lib/longPress.ts` | Touch gesture (pure factory, injectable timers) |
| `web/src/hooks/useActionRunner.tsx` | The only place an action is invoked |
| `web/src/hooks/useRowActions.tsx` | Binds a row to menu + sheet + keyboard |
| `web/src/hooks/useTaskActionContext.ts` | Task effects (+ per-project factory) |
| `web/src/hooks/useFeatureActionContext.ts` | Feature effects (+ factory) |
| `web/src/components/common/ConfirmDialog.tsx` | Shared confirmation, type-to-confirm |
| `web/src/components/common/ActionSheet.tsx` | Touch rendering |
| `web/src/components/common/ActionBar.tsx` | Modal-footer rendering |
| `web/src/components/Modal/StatusPickerModal.tsx` | Status picker, task + feature modes |
| `web/src/components/Modal/MetadataModal.tsx` | Field editor, task + feature modes |
| `internal/service` `BulkDelete` | Filter/paths, dry-run, per-entry results |
| `internal/api` `HandleBulkDelete` | `POST /entries/bulk-delete` |
| `internal/service` `GetLiveClaim` | "Is this task actually running?" |

### Deviations from the plan

1. **B2 signals rather than errors.** The plan said the server should
   reject a bulk filter matching more than `limit`. Doing that would
   break existing automation callers that rely on the current truncating
   behaviour. Instead the response carries `truncated` + `matched_total`,
   and the *client* refuses to proceed when a dry run reports truncation
   (`TruncatedOperationError`). Same safety outcome, no breaking change.

2. **B3 (bridge debounce) shipped with Phase 5, not deferred.** Feature
   delete emits one `entry.deleted` per task, so without coalescing the
   very feature the work enables would have been its worst trigger. The
   window is 120 ms, per project; `project_dirty` still fires per event.

### Test coverage added

- Web: 274 tests pass (was 169). New: `types.test.ts` (11),
  `taskActions.test.ts` (32), `featureActions.test.ts` (27),
  `metadataFields.test.ts` (23), `longPress.test.ts` (12).
- Go: `bulkdelete_test.go` (11), `liveclaim_test.go` (6),
  `bulkdelete_handler_test.go` (14), `bridge_test.go` (6). Full suite
  green.

### Verified against the live preview

Status change round-trip · feature bulk delete via type-to-confirm ·
metadata diff-only save · live-claim guard (409, and `force=true`
override) · touch long-press sheet at 375 px · keyboard accelerators
(`d`, `e`) · command-palette verbs with disabled reasons.

### Still open

- The raw frontmatter/body editor (TUI `e`) remains deferred, as decided.
- Multi-select + bulk ops across arbitrary task sets (TUI `Space`) was
  not in scope; the feature-scoped fan-out covers the common case.
- No undo. Cancel-before-delete and type-to-confirm are the mitigations.
