# Brain PWA

The Brain PWA is the primary web interface for the Brain memory + task
system. It's a React + TypeScript + Vite single-page app that embeds
into the Brain binary via Go's `embed` package.

## Dev

    npm install
    npm run dev              # http://localhost:5179
    npm test                 # node --test suite
    npm run build            # emits to ../internal/webui/dist/
    npx tsc --noEmit         # type check

## Requirements
- Node 20+ (some test globs need 23+; documented in
  `docs/panes-v2-followups.md`)
- Running brain-api backend at :3333 (or set VITE_API_URL)

## Architecture
- React 18 + Vite 6 + Zustand (persisted UI state) + React Query (server
  cache) + native SSE for live updates
- No CSS framework — dark-theme design tokens under `src/styles/tokens.css`
- All API calls go through `src/lib/api.ts` (typed wrappers); live
  updates through `src/lib/sse.ts` (`useLive()` store)
- Auth is OAuth PKCE via `src/lib/auth.ts` + `src/lib/pkce.ts`
- PWA install + service worker updates via `vite-plugin-pwa`

## Adding a new UI piece
- New modal: create `src/components/Modal/YourModal.tsx`, dispatch from
  `Modal/ModalHost.tsx`, open via `useModal.open("your-kind", ...)`
- New pane leaf: create `src/components/Workspace/leaves/YourLeaf.tsx`,
  extend the `DockLeaf.kind` union in `src/lib/dock.ts`, wire the
  renderer in `Workspace/PaneNode.tsx`
- New sidebar section: create `src/components/Sidebar/YourSection.tsx`,
  register in `Sidebar/Sidebar.tsx`

## Follow-ups
See `docs/panes-v2-followups.md` for known deferred work.

## Plan (this rewrite)
See Brain entry `projects/brain/plan/lbni8jpz.md` for the panes-v2
wire-in plan that produced this dashboard.
