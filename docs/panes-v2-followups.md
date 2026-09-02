# panes-v2 Wire-In: Follow-up Work

Deferred features from the panes-v2 wire-in rewrite (see plan
`projects/brain/plan/lbni8jpz.md`). Track these as separate work — do
not sneak into the panes-v2 feature branch.

## Frontend deferrals
- Brain notes view (was `views/BrainView.tsx`)
- Assistant chat drawer / sidebar / panel
- Full Automations detail editor
- Command bar (⌘K)
- Rich mobile UX (bottom sheets, swipe gestures)
- Attachments upload
- CodeMirror editor integration
- i18n
- a11y sweep
- PWA install prompt polish
- Keyboard shortcut registry (currently only Esc-closes-modal in wireframe)

## Backend gaps (tracked as separate Brain tasks in feature `panes-v2-wire-in`)
- `POST /api/v1/runners/{id}/features` — assign feature to runner
  (Brain task `ivwx9a8t`)
- Ad-hoc shell instance API for runner terminals
  (Brain task `4mymbjen`)
- `Task.mr_url` field in task response — verify existence during Phase 5
  (Brain task `jea9vg8n`)

## Resolution
As each item lands, cross it off with a link to the merged PR or Brain
entry that resolved it.

## Phase 2 notes (styles + primitives)

Deviations from the original plan text worth remembering when Phase 9
consolidates:

- **Additive CSS, not a replace.** The old `web/src/styles/global.css`
  (2931 lines of active dashboard styles) was renamed to
  `legacy-global.css` and still loads. The new `global.css` only holds
  panes-v2 base rules scoped under `.p2-app`. Every new class uses
  `--p2-*` / `.p2-*` prefixes so the two UIs coexist during the rewrite.
  Phase 9 deletes `legacy-global.css` and drops the `.p2-app` scope.
- **Component name collisions.** Three primitives shipped with a `V2`
  suffix because the old component tree still owns the bare name:
  `ModalV2`, `LoadingV2`, `ErrorStateV2`. Phase 9 removes the legacy
  files and renames these to `Modal`, `Loading`, `ErrorState`.
- **npm test needs node ≥ 23.** The existing `web/package.json` test
  script relies on `node --test` glob support, which is Node 22+. On
  Node 20 (the current default via Homebrew), invoke tests as
  `/opt/homebrew/opt/node@23/bin/node --import tsx --test 'src/**/*.test.ts' 'src/**/*.test.tsx'`
  or upgrade the default Node. Not fixed in Phase 2 — out of scope.
- **No DOM test library.** `common.test.tsx` uses `renderToStaticMarkup`
  for markup assertions and exports pure helpers (`classForDot`,
  `handleModalKeyDown`, `clampContextMenuPosition`,
  `isDismissContextMenuKey`) for interactive-logic tests. Real DOM
  behavior (focus trap, portal mount, scrim click, right-click open)
  is *not* covered. Adding `@testing-library/react + happy-dom` is a
  reasonable Phase 9 follow-up.

## Phase 3 notes (shell + sidebar + overview grid at `/v2`)

Deltas from the Phase 3 task text worth remembering:

- **`/v2` route lives inside the Gate.** `App.tsx` now runs a
  nested `<Routes>` inside the authenticated branch of Gate — the
  legacy Dashboard is the catch-all so any deep link that predates
  panes-v2 keeps working. Unauthenticated users still hit `<Login/>`.
- **`workspace` store persists only 4 slices.** `partialize` writes
  `view`, `focusSessionId`, `sidebarSection`, and `featureAssignments`
  to localStorage under `panes-v2:workspace:v1`. Ephemeral flags
  (`mobile`, `streaming`) and the Phase-7 `dockTree` placeholder are
  recomputed each session on purpose.
- **`safeStorage()` guards Node/SSR.** `zustand/persist` is wired
  through `createJSONStorage(() => safeStorage() ?? noopStorage)` so
  `node --test` doesn't crash when it constructs the store during
  imports for the reducer tests.
- **`.dragover` reused for card-focus flash.** Clicking a project row
  in the sidebar scrolls the matching `[data-project-card]` into view
  and briefly adds `.dragover` (which was defined for drag-drop in
  Phase 2). Same tokenized accent — no new CSS needed. Phase 4/5 can
  swap this for a dedicated `.pulse` class if the accent conflicts.
- **Card tab strip.** Resolved: the strip is Tasks / Goals /
  Automations. `CardAutomations` was promoted out of scaffolding, and
  Session / Logs never became card tabs — they are dock leaves. The
  **Features** tab was removed once every feature became a foldable,
  dependency-nested group header inside Tasks; `CardFeatures.tsx` is
  deleted and its three unique affordances (the feature forest, the
  ⛓ chain chips, and the merged fold, now a per-feature fold that
  defaults closed for finished work) live in `CardTasks.tsx`.
- **Runner-row context-menu "Clear assignment"** — wired in Phase 8.
  The Phase 6 stub is now a real call to
  `POST /api/v1/tasks/{projectId}/features/{featureId}/assignment/clear`
  (see `internal/api/tasks.go` `HandleClearFeatureAssignment`). The
  runner backend for assignments landed in Brain task `ivwx9a8t` and
  the client wrappers live in `web/src/lib/api.ts`
  (`assignFeatureToRunner`, `clearFeatureAssignment`).
- **Focus / Session views are placeholder screens.** Phase 7 fills
  them in. The view switcher in the topbar toggles between them
  correctly (verified by store unit tests); the workspace just shows
  a "Phase 7 arrives here" panel.

## Phase 7 follow-ups

- **`TaskDetailLeaf` duplicates the KV builder from `TaskModal`.** The
  KV rows (status/priority/feature/agent/etc.) are constructed by an
  identical `buildKvPairs` in both files. Phase 7 intentionally
  duplicated rather than extract a shared `TaskDetailView` to keep
  the diff scoped to the docking system. Follow-up: pull the row
  builder into `components/v2/common/TaskDetail.tsx` and consume from
  both modal + leaf. Trivial refactor once the docking system is
  stable.
- **Sidebar-row → leaf-edge drop is best-effort.** Dropping a sidebar
  row (task/session/runner) onto a leaf's edge zone currently calls
  `openInFocus` (which adds at the last-focus leaf center) and then
  attempts a second `moveLeaf` to the requested edge. For a fresh
  drag onto a specific edge this usually lands correctly, but the
  two-step nature can look flaky if the tree is complex. Cleaner
  fix: add a store-level `addLeafAtEdge(kind, target, title, targetId, edge)`
  action that does it in one atomic call. Deferred to keep Phase 7
  focused on the tree ops.
- **`SessionLeaf` is a placeholder.** It shows the instance id,
  status, workdir, and a link to open the runner modal. Full
  interactive PTY / prompt composer / permissions surfacing is a
  larger effort tracked as its own follow-up.
- **Iframe URL viewer relies on remote consent.** `BrowserLeaf`
  renders an iframe with a strict `sandbox`; most origins send
  `X-Frame-Options: DENY` or `frame-ancestors` and will render blank.
  This matches the task spec ("mock URL viewer only — no server
  data") — no fix needed, but users should not expect embedding
  arbitrary sites to just work.
- **Persisted `dockTree` shape.** We persist under the existing
  `panes-v2:workspace:v1` key. A defensive `coerceDockTree` in the
  `persist.merge` hook discards trees that don't parse as a valid
  DockNode (logs a console warning). Bumping the storage key was
  intentionally avoided so unrelated slices (view / sidebar / feature
  assignments) don't get wiped when we add new pane kinds.

## Phase 8 follow-ups

- **Feature→runner assignment endpoints are live.** Backend investigation
  from Brain task `ivwx9a8t` confirmed the endpoints exist at:
  - `PUT  /api/v1/tasks/{projectId}/features/{featureId}/assignment`
    body: `{ runner_id, intent: "assign"|"reassign", force? }`
  - `POST /api/v1/tasks/{projectId}/features/{featureId}/assignment/clear`
    body: `{ intent: "clear" }`

  Handlers live in `internal/api/tasks.go`
  (`HandleAssignFeatureToRunner`, `HandleClearFeatureAssignment`).
  Server tests in `internal/api/runners_test.go` exercise the 200 /
  409-on-reassign-without-intent / clear paths. Client wrappers in
  `web/src/lib/api.ts` (`assignFeatureToRunner`,
  `clearFeatureAssignment`) mirror that shape with wire-format
  coverage in `web/src/lib/apiAssignment.test.ts`.
- **SSE reconciliation on assignment.** Assignment mutations emit
  `runners_update` events on the runners lifecycle topic (also
  landed in `ivwx9a8t`); the existing `useLive` handler in
  `lib/sse.ts` picks these up automatically, so the UI reflects
  server truth without a manual refetch. If a browser session runs
  against an older backend build that doesn't emit these events, the
  optimistic `workspace.featureAssignments` overlay keeps the row
  in sync until the next `getRunners()` poll (react-query default
  cadence is 12 s in `useRunnersV2`).
- **ContextMenu doesn't support nested submenus.** The feature-row
  right-click "Assign to runner ▸" is a *flat* list — a
  `{ section: true }` header labeled "Assign to runner" followed by
  one item per online runner, with a `✓` glyph on the currently
  assigned one and a separator + "Clear assignment" below. When
  more than ~8 runners come online this list gets long; a follow-up
  is to extend `ContextMenu` to support real submenus (probably a
  `children?: ContextMenuItem[]` variant that opens on hover).
- **`Clear all` on a runner row is best-effort.** It uses
  `Promise.allSettled` over `combineRunnerAssignments`, optimistically
  drops each locally, and rolls back individual failures. If the
  runner's `feature_assignments` has an entry without a `project_id`
  (only ever happens with legacy/malformed data) we skip it and
  toast an "info" message — the clear endpoint requires a project
  segment. In practice every real assignment carries the project id.
- **Optimistic override keyed by featureId only.** `workspace.featureAssignments`
  is `Record<string, string>` (featureId → runnerId), no project
  scoping. This works today because feature IDs collide only
  cross-project and the UI resolves them per-project anyway
  (`resolveAssignedRunner` scopes to the current project's card).
  A future refactor could key by `${projectId}/${featureId}` if a
  collision is ever reported; not urgent.
- **No new DnD library.** Reuses Phase 7's `useDragDrop` hook with a
  new `"feature-header"` source; drop handlers filter on the source
  string so runner rows only accept assignment drops and pane-leaf
  drop zones still accept dock moves.

## Phase 9 follow-ups (cutover)

- **`.p2-app` CSS scope cleanup.** The Phase 2 plan called for Phase 9
  to drop the `.p2-app` scoping now that the legacy UI is deleted.
  Phase 9 kept the scope in place — it wasn't strictly necessary to
  remove and doing so cleanly required touching every `.p2-*` rule.
  Cleanup: drop the `.p2-app` wrapper `div` in `src/main.tsx` (or
  wherever it lives) and remove the `.p2-app` prefix from
  `src/styles/*.css`. Purely mechanical. No behaviour change.
- **`V2` component suffixes.** `ModalV2`, `LoadingV2`, `ErrorStateV2`
  still carry the disambiguation suffix from Phase 2 (when the legacy
  components owned the bare name). Legacy is gone; rename to
  `Modal`/`Loading`/`ErrorState` and update all import sites.
  Follow-up.
- **`npm test` glob quirk on Node 20.** `web/package.json` still uses
  the `node --test 'src/**/*.test.ts'` glob form which needs Node
  ≥ 22 (globstar support). On Node 20 (the current Homebrew default),
  invoke tests via
  `node --import tsx --test $(find src -name '*.test.ts' -o -name '*.test.tsx')`
  or upgrade. Documented in `web/README.md`. Fix options: bump
  package.json engines to `>=22`, or switch to Vitest.

## Phase 10 follow-ups (smoke + PR)

- **Full interactive click-through smoke was not executed via
  browser-automation** during Phase 10 because the ChromeUse MCP tool
  wedged mid-session and could not recover. Substituted:
  - direct HTML/JS DOM inspection of the running app (screenshot
    captured before the tool hung; confirmed Overview grid renders,
    projects/sessions/runners sidebar renders, `.p2-*` classes
    present, zero console errors, `panes-v2:workspace:v1` localStorage
    key set),
  - direct backend HTTP verification against `./bin/brain api --port
    3336` running the freshly-embedded PWA (bundle hash confirmed
    identical to `internal/webui/dist/`),
  - source presence + shape checks for every modal / lib file the
    14-step smoke would exercise,
  - live round-trip of the feature→runner assignment endpoint
    (`PUT` then `POST /clear`) against the real backend.

  Explicit gaps: nothing was interactively clicked-through in a
  browser during this session for RunnerModal (Overview/Shell/Logs
  tabs), TaskModal, FeatureModal (with a real MR-having feature),
  AutomationModal + Run now, Settings toggle persistence across
  reload, focus-mode drag → split → resize → reload persistence,
  drag feature → drop on runner assignment, mobile viewport
  rendering, or sign-out. The **code paths** and **backend
  endpoints** for each are verified; the **integration** at the
  browser layer is verified only for the initial render.

  Recommended follow-up: rerun the 14-step smoke as an
  interactive session (or via a working browser automation tool)
  before shipping any bugfix that touches these surfaces.
- **Real `SessionFull` view.** Phase 7 `SessionLeaf` shipped a
  placeholder. Full interactive PTY / prompt composer / permissions
  surfacing is still open.
- **Real task-Trigger button in TaskModal.** Phase 6 deferred the
  "Trigger now" action button; the modal currently only shows task
  detail.
- **Mobile UX polish.** Bottom sheets, swipe gestures, mobile-first
  drawer, better touch targets. Layout collapses correctly at
  `max-width: 720px` (single media query in `layout.css`) but the
  UX is desktop-first.
- **Sidebar keyboard navigation.** No arrow-key focus flow through
  the projects / sessions / runners lists. Only Esc-closes-modal is
  wired.
- **`tsc --noEmit` clean.** Zero errors, zero warnings as of Phase 10.
- **Browser tab spinner during live streaming.** Chrome reports the
  tab as "loading" while any fetch/EventSource is in-flight. The
  live-tasks/runners/logs streams are in-flight for their entire
  lifetime by design, so the spinner never stops. The only reliable
  fix is migrating SSE to WebSocket (post-upgrade WS connections are
  exempt from the loading indicator). Documented in `web/src/lib/sse.ts`
  and accepted as expected behavior. Follow-up: consider WebSocket
  transport if the spinner becomes a UX blocker; would require a
  backend upgrade handler + message framing.
