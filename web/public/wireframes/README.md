# Dockable Panes — PWA Wireframes

Two standalone HTML/CSS/JS wireframes for the Brain PWA multi-project dashboard.

- **`panes-v2.html`** — **v2 (current, recommended)**: multi-project dashboard inspired by Claude Code Desktop 2026 redesign. No tabs — every project you care about is visible side-by-side.
- **`panes.html`** — v1: original tab-per-project dockable panes design (kept for reference).

## Run

**Via dev server** (recommended):

```bash
cd web && npm run dev
# then open:
#   http://localhost:5173/wireframes/panes-v2.html   ← v2 dashboard
#   http://localhost:5173/wireframes/panes.html      ← v1 dockable panes
```

**Directly**: open the HTML files in a browser.

---

## v2 — Multi-Project Dashboard

Inspired by:
- Anthropic's **Claude Code Desktop 2026 redesign** (April 2026) — parallel sessions in a sidebar, drag-and-drop panes, integrated terminal, live streaming
- Anthropic's **Claude Projects** — curated knowledge per project

### Layout

```
┌────────────────────────────────────────────────────────────────────────────────────┐
│ brain workspace │ Overview | Focus │ ⌕ search  ⌘K │ ＋session  🔔  Assistant ▸    │
├────────────┬───────────────────────────────────────────────────────────────────────┤
│ Filters:   │                                                                       │
│ [All] [Active][Ready][Blocked][Done]                                                │
│ [prod][staging][dev]                                                                │
│                                                                                     │
│ PROJECTS   │  ┌── orion-ai [prod] ────────┐  ┌── brain [dev] ─────────┐            │
│ ● orion-ai │  │ 3 active 2 ready 1 blk    │  │ 2 active 2 ready       │            │
│ ● brain    │  │ Tabs: Tasks Features Sess │  │ Tabs: Tasks Features   │            │
│ ● dispatch │  │                           │  │                        │            │
│ ● pathfind │  │ ▸ OAuth (F-auth) 65%      │  │ ▸ Dockable panes 50%   │            │
│ ● orion-web│  │   T-1001 Wire callback    │  │   T-2001 Wireframe v2  │            │
│            │  │   T-1002 Rate limiter ▸   │  │   T-2002 Splitter      │            │
│ LIVE SESS. │  │ ✕ Migrate SSE (blk) 20%   │  │ ✓ Runner queue 100%    │            │
│ ▸ rate lim │  └───────────────────────────┘  └────────────────────────┘            │
│   orion-ai │  ┌── dispatch-runner ────────┐  ┌── pathfinding ─────────┐            │
│ ▸ gateway  │  │ Session: capacity race    │  │ Session: heuristic bnc │            │
│ ▸ wireframe│  │ ● live 12:04 session ...  │  │                        │            │
│ ▸ capacity │  │ ● live 12:04 spawn ...    │  │   Tasks · Features     │            │
│            │  └───────────────────────────┘  └────────────────────────┘            │
├────────────┴───────────────────────────────────────────────────────────────────────┤
│ ● streaming · 4 live · 5 projects        context ▓▓▓░░ 34%  session ▓░░░ 12%  v2   │
└─────────────────────────────────────────────────────────────────────────────────────┘
```

### Key features

**Overview mode (default)** — every open project is a **card in a grid**, all live at the same time. Each card has its own tabs: **Tasks / Features / Session / Logs**. You can:
- Watch multiple live sessions ticker in parallel
- See feature dependency progress bars on each card
- See task counts (active/ready/blocked/completed) at a glance in the header
- Drag any project card to reorder
- Drag any **task row** or **session row** out onto the workspace to open in a focus pane

**Focus mode** — the dockable-pane workspace from v1. Any pane can host content from any project. Each pane header carries a color-coded **project badge**.

**Session mode (full view)** — click any live session in the sidebar or in a card → dedicated full-page session view with:
- Streaming assistant/user/tool messages (with a typing cursor when streaming)
- Right rail: session metadata, related task, live logs
- Composer at the bottom
- "Open in Focus split" button to promote to a pane in Focus mode

**Left sidebar**:
- **Status chips** (Active / Ready / Blocked / Done) with counts across all projects
- **Env chips** (prod / staging / dev)
- **Projects list** — click to jump to card, right-click for actions, hide/show
- **Live sessions list** — drag onto focus workspace to promote to a pane

**Live streaming**:
- Every ~900ms, running sessions add a new log line and occasionally advance the session turn index
- New log lines flash with a fade
- Toggle **Pause stream** in the banner to stop the ticker

**Drag semantics**:
- Drag a **task row** onto a Focus pane's edge → creates a task-detail pane at that edge
- Drag a **session row** onto a Focus pane's center → docks as a sibling tab
- Drag a **project card header** onto another card → reorders (mock)
- Drop zones highlight during drag with orange dashed outlines

**Mobile**:
- Sidebar collapses to a horizontal pill nav at the top
- Overview grid becomes a single column of cards
- Long-press any draggable item → BottomSheet with actions

### What to try

1. Open v2 → you see **5 project cards** in a grid, all live. Watch the session tickers tick.
2. Click the **Session** tab on any card → live mini-transcript with assistant/tool bubbles.
3. Click the **Logs** tab on any card → tail of the mock log stream, new lines fade in.
4. Click a **session in the sidebar** → full session view with streaming turns.
5. Right-click any **task** → "Open detail in focus pane".
6. Switch to **Focus** mode → drag task tabs between panes, split edges, etc.
7. Click **Mobile** in the banner → cards stack, sidebar collapses.
8. Click **Pause stream** → tickers freeze.
9. **Reload page** → filters, hidden projects, active card tabs, and Focus layout all persist. Live buffers reset.

---

## v1 — Original Tab-Per-Project

Kept for comparison. Documented behavior:

- Project tabs at the top (like browser tabs)
- Dockable-pane workspace per project
- Reuse-by-kind Task Detail, right-click "Open in new pane" for pinned siblings
- Drag panes onto edges/centers or onto other project tabs
- Mobile bottom sheet for pane moves

Superseded by v2's overview grid + sidebar sessions.

---

## Files

- `panes-v2.html/.css/.js` — v2 multi-project dashboard (current)
- `panes.html/.css/.js`   — v1 tabs-per-project (reference)
- `README.md` — this file
