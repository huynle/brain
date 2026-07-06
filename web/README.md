# Brain PWA

A mobile-installable Progressive Web App that mirrors the Brain TUI: tasks,
real-time logs, automations/goals, the knowledge base, and runners — all over
the same REST + SSE API the TUI uses.

The app is built with **Vite + React + TypeScript** and compiled into
`../internal/webui/dist`, where it is embedded into the `brain` binary via
`go:embed`. In production the Go server serves the app at `/` from the same
origin as the API — so there is **no CORS, no second deploy**, and the PWA is
installable straight from your Brain domain.

## Architecture

```
web/  (this dir)                          internal/webui/
├── src/
│   ├── lib/        api, sse, auth (OAuth+PKCE), types, config, format
│   ├── store/      zustand UI state
│   ├── hooks/      projects, aggregated live tasks
│   ├── components/ layout (Header, BottomNav) + common (Modal, Badge, …)
│   ├── views/      Tasks, Logs, Brain, Automations, Runners, Settings
│   └── pages/      Login, AuthCallback, Dashboard
└── vite.config.ts  → build.outDir = ../internal/webui/dist
                                            └── webui.go  (go:embed dist + SPA handler)
```

- **Real-time:** one `EventSource` per active project (`/api/v1/tasks/{id}/stream`)
  feeds a zustand "live" store (task snapshots, runner updates, runner logs).
  `EventSource` can't send headers, so the token is passed as `?token=`.
- **Auth:** OAuth 2.1 authorization-code + PKCE against the server's `/authorize`
  (PIN consent), `/token`, `/register` endpoints. Falls back to a pasted API
  token. OAuth tokens bypass REST scope checks server-side, so a PIN login grants
  full access. See `src/lib/auth.ts`.
- **Routing:** `internal/webui/webui.go` serves static assets + an SPA fallback
  to `index.html`, while delegating `/api`, `/mcp`, `/authorize`, `/token`,
  `/register`, and `/.well-known/*` to the API router.

## Develop

```bash
# From the repo root, start the API server (any port; default 3333):
just dev                       # or: ./bin/brain api start

# In another terminal, run the PWA dev server with HMR:
just web-dev                   # → http://localhost:5179
#   proxies /api, /token, /authorize, … to $BRAIN_API_URL (default :3333)
```

Point the dev proxy at a different backend with `BRAIN_API_URL`:

```bash
BRAIN_API_URL=http://localhost:4444 just web-dev
```

## Build

```bash
just web-build        # compile the PWA into internal/webui/dist
just build            # build the Go binary (embeds whatever is in dist)
just build-all        # web-build + build in one step
```

`just release` and the Docker image build the PWA automatically.

## Notes / parity

- The TUI's `$EDITOR`-spawn becomes an in-app **CodeMirror** editor (lazy-loaded).
- The TUI's tmux/fullscreen **session reattach** has no browser equivalent and is
  intentionally omitted; logs are streamed instead.
- Host CPU/mem metrics aren't readable from a browser; the Runners view shows
  runner-reported workload instead.
