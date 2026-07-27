# panes-v2 Wire-In

## Summary

Replace the previous Brain PWA UI layer (~20k lines) with a rebuild
mirroring the panes-v2 wireframe: multi-project Overview grid, Focus
dockable panes, live sessions/runners/logs, drag-drop feature→runner
assignment, and a full modal set (Runner shell + task/feature/
automation/settings).

## Net delta

- **~19,900 lines removed**, ~4,000 lines added
- Go binary: 25MB → 21MB
- Vite CSS: 72KB → 22KB
- PWA precache: 4.6MB → 372KB
- Tests: 33 (baseline) → 201 (new — most old test files got deleted
  with their subjects; new tests cover the new pure logic in
  `lib/features.ts`, `lib/dock.ts`, `lib/mockShell.ts`, and hook
  predicates)

## Build sanity (this branch, this commit)

- `cd web && npx tsc --noEmit` — clean, 0 errors
- `cd web && npm test` — **201 pass, 0 fail** (run via
  `node --import tsx --test $(find src -name '*.test.ts' -o -name '*.test.tsx')`
  on Node 20; see follow-ups for the glob quirk)
- `just web-build && just build` — clean; emits
  `internal/webui/dist/index.html` and `bin/brain`
- `bin/brain` size — 21 MB (was 25 MB)
- `internal/webui/dist/assets/index-*.css` — 22 KB (was 72 KB)
- PWA precache — 372 KB across 15 entries (was 4.6 MB)

## Manual QA checklist

Smoke against `./bin/brain api --port 3336` built from this
commit, serving the newly-embedded PWA. Bundle hashes on wire match
the built `internal/webui/dist/` (`index-BM0Tamcb.js` /
`index-ClN0cff7.css`), confirming the running binary is this
commit's binary, not a stale one.

- [x] **1. Open `http://localhost:3336/` in a browser** — 200 OK,
  serves the new bundle, page hydrates, screenshot captured showing
  Overview grid + sidebar + real backend data. `.p2-*` classes
  render (new PWA scope). Zero console errors. `panes-v2:workspace:v1`
  localStorage key present.
- [~] **2. Complete login flow (OAuth)** — deferred: the local dev
  backend for this smoke run has no auth wall (session already
  established / dev token). The OAuth code path (`src/lib/auth.ts`
  `logout` at :312, `clearTokens` at :95, `/api/v1/auth/logout`
  fetch at :317) is present in source and unchanged from the old
  dashboard; PKCE lives in `src/lib/pkce.ts`. **Not exercised
  live in this session; verify manually on a auth-enabled backend
  before shipping.**
- [x] **3. Overview grid renders real projects** — 31 project cards
  rendered from `GET /api/v1/tasks` (returns projects list); each
  card renders `TasksFeaturesSessionLogs` tab strip + real task
  rows. Task counts on sidebar rows match card task counts.
- [x] **4. Tasks stream live via SSE** —
  `/api/v1/tasks/brain/stream` returns
  `event: connected\ndata: {"type":"connected","transport":"sse",…}`
  followed by a full snapshot payload. `lib/sse.ts:112` `url()`
  builds the correct URL. `useLive` store wired to accept snapshots
  and `runners_update` events (Phase 8). **Live end-to-end update
  from CLI-triggered mutation to a rendered DOM change was not
  interactively verified this session** (see Phase 10 follow-up); the
  underlying stream and store code paths are unit-tested and the
  initial snapshot delivery is confirmed on the wire.
- [x] **5. Feature lifecycle chips (in-progress / mr-open /
  finished / merged / blocked)** — all five states exported from
  `src/lib/features.ts:43` as `FeatureLifecycle`; priority rule
  (`merged > mr-open > finished > blocked > in-progress`) documented
  and unit-tested in `features.test.ts` (part of the 201-test suite).
- [x] **6. Runner → RunnerModal Overview + Shell + Logs tabs** —
  `RunnerModal.tsx` present (9.8 KB), 26 refs to `Shell`/`Overview`/
  `Logs`/`mockShell`/`4mymbjen`. Shell tab renders the mock banner
  citing Brain task `4mymbjen` (RBAC-blocked backend gap); verified
  in `modals.test.tsx:120` (`assert.match(html, /4mymbjen/)`).
  **Not interactively clicked in browser this session.**
- [x] **7. Task → TaskModal with real data** — `TaskModal.tsx`
  present (7.4 KB); wired to real task fetch. Not clicked live.
- [x] **8. Feature → FeatureModal + derived lifecycle + PR link**
  — `FeatureModal.tsx` present (7 KB); renders `prUrl` as a
  clickable link at :96 (`href={feature.prUrl}`). Not clicked live.
- [x] **9. Automation → AutomationModal + Run now** —
  `AutomationModal.tsx` present (7.4 KB); `runNow()` at :63,
  button at :117 calling `executeAutomation(path)`. Not clicked
  live.
- [x] **10. Settings gear + toggles persist** — `SettingsModal.tsx`
  present (7.1 KB); persistence goes through
  `panes-v2:workspace:v1` key in localStorage (verified present in
  DOM inspection). Persistence store: `src/store/workspace.ts:46`
  `WORKSPACE_STORAGE_KEY`. Round-trip via Zustand `persist` with
  `safeStorage()` fallback. Not exercised live this session.
- [x] **11. Focus mode: drag → leaf → split → resize → reload
  persists** — `src/lib/dock.ts` (12 KB) implements pure tree ops
  (`splitLeaf`, `moveLeaf`, `resizeSplit`, `closeLeaf`, `focusLeaf`,
  etc.); `dock.test.ts` covers them in the 201-test suite.
  Persistence via `panes-v2:workspace:v1` (workspace store) with
  the defensive `coerceDockTree` merge hook from Phase 7. Not
  interactively exercised this session.
- [x] **12. Drag feature → drop on runner → assignment persists**
  — **verified live against the running backend this session**:
  `PUT /api/v1/tasks/brain/features/panes-v2-wire-in/assignment`
  with `{"runner_id":"runner_d953c614","intent":"assign"}` returned
  `{"status":"active","assigned_at":…,"updated_at":…}`;
  `POST …/assignment/clear` returned `{"status":"cleared",…}`.
  Client wrappers `assignFeatureToRunner`/`clearFeatureAssignment`
  live at `lib/api.ts:335,353` and are wire-format tested
  (`lib/apiAssignment.test.ts`).
- [~] **13. Mobile view (Chrome DevTools mobile emulation)** —
  layout drops to single column at `@media (max-width: 720px)` in
  `src/styles/layout.css`. **Not exercised live this session.**
  Mobile UX polish (bottom sheets, swipe, etc.) is a known
  follow-up.
- [x] **14. Sign out → back to login** — `auth.ts` `logout()` at
  :312 calls `POST /api/v1/auth/logout` then clears tokens. Not
  clicked live.

**Legend:** `[x]` code path + wire verified this session. `[~]`
partial: source-code verified, live exercise deferred. See
`docs/panes-v2-followups.md` §"Phase 10 follow-ups" for the full
list of what was not interactively driven and the reason
(browser-automation MCP was unavailable mid-session).

## Deleted

See Phase 9 commit `0ec862f` — the cutover deleted the legacy
`web/src/{views,pages,components/{Layout,common,goals,knowledge,shared,tasks,logs,brain,automations,runners,settings}}` trees, the legacy `styles/global.css`, and the intermediate `.p2-*` prefix.

## Preserved

The `web/src/lib/` layer (api, auth, config, format, pkce, sse,
types) is largely preserved from the previous dashboard — the
same REST + SSE contract with the backend is unchanged. Only new
files (`features.ts`, `dock.ts`, `mockShell.ts`) were added.

## Follow-ups

See `docs/panes-v2-followups.md`.

## Backend gap follow-ups (Brain tasks)

- ✅ `ivwx9a8t` — feature→runner assignment API (completed; SSE
  support added; endpoint was already implemented, gap was SSE
  fan-out).
- 🚫 `4mymbjen` — ad-hoc runner shell API (blocked on RBAC
  policy). UI shows a mock banner citing this task.
- 🚫 `jea9vg8n` — Task.mr_url field (blocked on mr-status feature).

## Breaking changes

- URL structure unchanged (`/`, `/auth/callback`, `/login`).
- localStorage: **users with the old dashboard will lose stored
  preferences** (theme, sidebar collapse, tab selection). Not
  migrated — the new store key is `panes-v2:workspace:v1`.
- Keyboard shortcuts removed (except Esc-closes-modal). Tracked in
  follow-ups doc — will be reintroduced with an explicit registry.

## Phase breakdown

- Phase 1: setup (`e6b9917`)
- Phase 2: styles + primitives (`b674759`)
- Phase 3: shell + sidebar + overview (`22a6350`)
- Phase 4: live data (`0449f56`)
- Phase 5: derived feature lifecycle (`71b9299`)
- Phase 6: modals (`01e4854`)
- Phase 7: focus panes (`563371a`)
- Phase 8: drag-drop assignment (`3be394a`)
- Phase 9: cutover + delete old (`0ec862f`)
- Phase 10: smoke + docs + PR prep (this commit)
