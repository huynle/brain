# Timeline Future Projection Design

**Date:** 2026-10-03  
**Status:** Approved design  
**Scope:** Server-generated, read-only timeline forecasts for all time-based Brain work

## Goals

The Timeline should show both recorded history and expected future activity so a
user can answer:

- What is scheduled to happen next?
- Which tasks, automations, reminders, or features will become active or expire?
- How frequently will recurring work run?
- What source record controls a projected occurrence?

The default forecast horizon is 30 days. Projections are advisory and read-only;
they must never create tasks, dispatch work, or persist synthetic events.

## Decisions

- Generate projections on the server, not in the browser.
- Include every time-based source and milestone supported by the data model.
- Expand every recurrence inside the requested window.
- Aggregate very frequent schedules by source and local calendar day.
- Keep projected items visually and structurally distinct from actual events.
- Include both projected items and Now in Timeline Fit bounds.

## Considered Approaches

### Server-generated forecast endpoint (selected)

The server owns schedule parsing, timezone interpretation, eligibility, and
cross-project data access. This keeps the forecast consistent with runner
behavior and gives every client one reusable API.

### Browser-generated projections

This would reuse existing list endpoints but duplicate recurrence behavior,
require cross-project request fan-out, and risk drifting from Go scheduling
semantics. It is unsuitable as the source of truth.

### Persisted synthetic events

Persisting future occurrences would make queries simple but create stale or
misleading records when schedules change. Forecasts therefore remain ephemeral.

## API

Add a read-only endpoint:

```text
GET /api/v1/timeline?from=<RFC3339>&to=<RFC3339>&project=<optional>
```

The server applies a 30-day future window when `to` is omitted. Explicit ranges
are bounded by a configured maximum to prevent unbounded expansion.

Response shape:

```json
{
  "from": "2026-10-03T10:00:00Z",
  "to": "2026-11-02T10:00:00Z",
  "generated_at": "2026-10-03T10:00:01Z",
  "items": [],
  "warnings": [],
  "truncated": false
}
```

`warnings` reports malformed or unsupported source schedules without failing the
entire forecast. `truncated` indicates that an expansion safety budget was hit.

## Timeline Item Model

A timeline item preserves existing event navigation fields and adds forecast
semantics:

```text
id
type
source
timestamp
project_id
task_id / feature_id / task_path
title / summary
temporal_state: actual | projected
temporal_kind: execution | reminder | start | deadline | expiry
source_kind: task | automation | reminder | feature
source_id
source_path
timezone
projection_rule
occurrence_count
window_start / window_end
metadata
```

Actual items are backed by persisted event records. Projected item IDs are
deterministic within a response, based on source identity, kind, and occurrence
time or aggregate date. They are not valid entry IDs.

## Projection Sources

### Tasks

Project recurring `schedule` occurrences and one-time `run_once_at` execution.
Respect `schedule_enabled`, `next_run`, `starts_at`, `expires_at`, `timezone`,
`max_runs`, existing run count, and scheduler-eligible statuses. Also emit start
and expiry milestones.

### Features

Project `feature_schedule`, `feature_run_once_at`, `feature_starts_at`, and
`feature_expires_at`. Feature fields copied onto multiple resolved tasks are
deduplicated by project and feature ID.

### Automations

Project active cron triggers using the trigger schedule and timezone. Event,
webhook, and session triggers have no knowable future occurrence and are not
fabricated.

### Reminders

Project `remind_at` plus `daily`, `weekly`, `monthly`, and `yearly` repeats until
`repeat_until`. Use calendar arithmetic in the reminder timezone so month length
and daylight-saving boundaries remain correct. Undated, done, and paused
reminders do not produce future occurrences.

### Other time-based milestones

Any supported start, expiration, deadline, or one-time execution field is emitted
as its own `temporal_kind`. Unknown metadata that merely resembles a date is not
interpreted.

## Recurrence and Aggregation

Cron expansion must reuse `pkg/cron` parsing and matching semantics, including
Brain's deliberate day-of-month/day-of-week behavior. The service walks only the
requested range and stops when source eligibility ends.

The service maintains both a response-wide expansion budget and a per-source
threshold. When a source exceeds the dense threshold, occurrences are grouped by
source, temporal kind, timezone, and local calendar date. An aggregate carries
`occurrence_count`, `window_start`, and `window_end`. This preserves workload
shape without returning thousands of minute-level markers.

## Timeline UX

The Now line separates history from forecast. The future side receives a subtle
tint and a “Forecast · next 30 days” label.

- Actual events use solid dots and stems.
- Projected items use hollow dots and dashed stems.
- Starts, deadlines, expiries, reminders, and executions have distinct compact
  labels while retaining their source-family color.
- A new Actual/Forecast control can hide either temporal state.
- Existing project and family filters apply to projections.
- Fit includes all visible actual items, projections, and Now.
- Clicking a projection opens its real source task, automation, reminder, or
  feature.
- A projected occurrence explains its local time, timezone, recurrence rule,
  and eligibility.
- A daily aggregate reveals its occurrence count and contained times on demand.

Loading or projection warnings do not replace usable timeline data. If forecast
loading fails, historical events remain available with a retryable forecast
warning.

## Caching and Refresh

The PWA queries the endpoint through React Query using range and project scope as
part of the key. Forecast data is refreshed on window focus and at a modest
interval. Schedule mutations invalidate the timeline query. Forecast responses
may use short private cache headers but must not outlive source updates for long.

## Safety

- Endpoint is GET-only and performs no writes or dispatches.
- Validate and cap requested ranges.
- Apply per-source and response-wide occurrence budgets.
- Aggregate rather than silently drop dense occurrences.
- Return source-specific warnings for malformed schedules.
- Respect tenant and project boundaries in every source query.
- Do not expose source content beyond fields already available to the caller.

## Verification

Backend tests cover:

- cron parity and timezone boundaries;
- start, expiry, run-count, and status eligibility;
- reminder recurrence across short months and DST;
- feature deduplication;
- daily aggregation and expansion budgets;
- malformed-source warning isolation;
- API range validation, filtering, authorization, and read-only behavior.

Frontend tests cover:

- response adaptation and deterministic destination mapping;
- actual/projected filters and styling;
- Fit bounds including forecasts and Now;
- aggregate details and source navigation;
- loading, partial-warning, and forecast-error states;
- responsive layouts.

Browser verification proves that a 30-day forecast appears to the right of Now,
all normal occurrences are visible, dense schedules aggregate, and navigation
opens the real source item.

## Delivery Sequence

1. Define timeline response types and the pure projection service.
2. Add task, feature, automation, and reminder projectors with tests.
3. Add aggregation budgets and warnings.
4. Expose and test the read-only endpoint.
5. Add the PWA API hook and replace the seeded adapter.
6. Add forecast visuals, filters, aggregate details, and navigation.
7. Run backend, web, and browser verification against isolated seeded data.
