# Runtime Calendar Sources Design

## Goal

Let an admin add, replace and remove ICS calendar sources (for example a
personal Google Calendar) while the server runs, through the API, SDK, MCP and
PWA, without editing server config or restarting. Automations already refer to
calendars by name; they keep doing so.

## Background

Calendar sources are read from `server.calendars` once at startup
(`internal/apiserver/tenant_graph.go` builds the registry,
`internal/apiserver/server.go` builds the poller). Adding one means putting the
secret address on the server, editing config and restarting, which on
production also means up to 20 minutes of slow task reads during the boot index
scan. Each Google calendar has its own secret iCal address; there is no
combined feed.

Decision `paecvrn8` kept addresses out of Brain entries and out of reach of
automation authors. This design keeps both properties: addresses are accepted
only by admin endpoints, stored only in the server's private data directory,
and never returned.

## Decisions

1. Runtime sources are added through admin-only endpoints (`admin:*`). Sources
   in server config keep working unchanged and cannot be changed or removed
   through the API.
2. Addresses are stored server-side only, one file per source under
   `<dataDir>/calendars/sources/<name>.json` (file 0600, directory 0700,
   atomic write). They never appear in entries, responses, logs, snapshots or
   notices.
3. A runtime source may only reach public addresses. Every connection of a
   runtime source (including redirects) resolves the host and refuses
   loopback, private, link-local, CGNAT (100.64.0.0/10, which includes
   Tailscale), unique-local, unspecified and multicast addresses, and dials the
   checked address so DNS rebinding cannot bypass the check. Config sources are
   operator-defined and keep their current behaviour.
4. A new address is validated by one synchronous fetch and parse before it is
   saved. A failure returns 422 with a redacted reason and saves nothing; a
   success stores that first snapshot, so the source has events immediately.
5. Removing a source that automations still reference is refused (409, with the
   references) unless `force=true`. After a forced removal, existing behaviour
   applies: `skip_if_event` fails open, `only_if_event` fails closed, calendar
   triggers fire nothing, and saving an automation with the name is rejected.
6. At most 32 runtime sources. Names use the config rule
   `^[a-z0-9][a-z0-9_-]{0,63}$` and must not collide with any existing source.
7. Single local tenant only, like the poller. In tenant mode the write
   endpoints return 501.
8. Writes are serialized by one manager that owns the registry, poller and
   source store. Each write is logged with the source name and the caller's
   name, never the address.

## HTTP contract

All bodies are JSON. Error bodies use the existing `WriteError` shape.

### `GET /api/v1/calendars` (read, existing)

Each entry keeps its current fields (`name`, `kind`, `last_fetch`,
`last_success`, `event_count`, `last_error`, `stale`) and gains:

- `origin`: `"config"` or `"runtime"`
- `poll`: poll interval as a Go duration string (ICS sources only)

### `POST /api/v1/calendars` (`admin:*`)

Request: `{"name": "personal", "url": "https://calendar.google.com/...", "poll": "5m"}`;
`poll` is optional (default 5m, minimum 1m).

- 201: the calendar view above (`origin: "runtime"`), never the URL.
- 400: invalid JSON, name or poll.
- 409: the name exists (config or runtime), or the 32-source limit is reached.
- 422: the address is rejected: not https, has user info, longer than 2048
  characters, resolves to a non-public address, or the validation fetch or
  parse failed. The message is redacted.
- 501: no calendar manager (tenant mode).

### `PUT /api/v1/calendars/{name}` (`admin:*`)

Request: `{"url": "...", "poll": "10m"}`; at least one field. Used to replace
the address after resetting it in Google, or to change the poll interval.

- 200: the calendar view.
- 400, 422, 501: as for POST.
- 404: unknown name. 409: the source is defined in server config.

### `DELETE /api/v1/calendars/{name}?force=true` (`admin:*`)

- 200: `{"name": "personal", "removed": true, "references": [...]}`.
- 404: unknown name. 409: defined in server config, or referenced and `force`
  is not set; the response lists the references.
- A reference is `{"id", "path", "title", "field"}`, where `field` is one of
  `trigger.calendar`, `trigger.skip_if_event`, `trigger.only_if_event`. All
  automation statuses are scanned, bindings included.

Removal stops polling and deletes the source file and its cached snapshot.

## Components

- `internal/calendar`: a source store for runtime sources; a mutable registry
  (`AddICS`, `Remove`, origin per source); a poller that can add and remove
  sources while running; the public-address dialer; a `Manager` that
  validates, persists and applies changes.
- `internal/api`: the three handlers in the `admin:*` group; `origin` and
  `poll` on the list view.
- `internal/apiserver`: build the manager in single mode, load runtime sources
  at startup after config sources (a colliding or unreadable runtime file is
  skipped with a warning, never fatal).
- SDK (Go and TypeScript): `calendars.create`, `calendars.update`,
  `calendars.delete`, marked write and not for scripts; contract 148 → 151
  operations, explicit deltas only.
- MCP: `calendars_list`, `calendar_add`, `calendar_update`, `calendar_remove`.
  Descriptions state that the address is a secret, stored on the server and
  never shown again.
- PWA: a Calendars section in Settings to list, add, replace the address,
  change the poll interval and remove, with references shown before a forced
  removal.

## Testing

- Store: 0600/0700 modes, atomic writes, reload after restart, corrupt or
  colliding files skipped.
- Guard: each blocked range (IPv4, IPv6 and IPv4-mapped), a public name
  resolving to a private address, and a redirect to a private host.
- Manager: add, replace and remove while polling; validation failure saves
  nothing; limit and name rules; config sources immutable; references block
  removal without `force`; concurrent writes serialize.
- Secret leakage: the address never appears in any response, log line, status,
  snapshot or error.
- API: status codes above, scope enforcement (`admin:*` only).
- Live: add a public Google iCal feed on an isolated server, reference it from
  an automation, replace and remove it, restart and confirm it persists.

## Out of scope

Google OAuth (one sign-in for all Google calendars), per-tenant calendar
sources, and sharing a source between tenants.
