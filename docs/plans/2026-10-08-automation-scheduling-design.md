# Automation Scheduling Design

## Goal

Make automations the primary way to run recurring and calendar-driven work, so
that one shared definition (for example Dream Consolidation) can:

- run on a real interval ("every 4 days"), not only cron approximations;
- spread its per-project runs instead of firing every project in the same tick;
- be customized per project (schedule, agent, model) without copying the prompt;
- follow a trading-market calendar or a personal calendar (Google, via iCal);
- start, expire, or stop after a number of runs.

## Background

Brain has three schedulers today:

| Scheduler | Evaluated by | Model | Code |
|---|---|---|---|
| Scheduled task | Runner, for watched projects | One task re-armed to `pending`; history in `runs[]` | `internal/runner/schedule.go` |
| Automation (cron) | API server, every minute | Rule that creates a new task per firing | `internal/service/automation_service.go` |
| Reminder | API server | `repeat: daily/weekly/monthly/yearly` | `internal/types/reminder.go` |

Problems this design addresses:

1. A global cron automation (the built-in Dream Consolidation) fans out to every
   matching project **in the same tick**, with one schedule and one agent for all
   of them. The only per-project alternatives are separate entries that duplicate
   the prompt, or the deprecated `dream_enable` monitor tasks — which can run in
   addition to the global entry and dream a project twice.
2. Automation cron is stateless: `schedule.Matches(now)` once a minute. A missed
   minute (restart, outage) loses the run with no record.
3. "Every N days" is not expressible: `*/N` in day-of-month resets at month end.
4. No calendar awareness (market holidays, personal calendars).
5. Automations have no `starts_at`, `expires_at`, or `max_runs` (scheduled tasks do).
6. `pkg/cron` ANDs day-of-month and day-of-week when both are restricted; standard
   cron ORs them.

Related Brain records: cron design decision `owyrguh4` (scheduled tasks: one task,
many runs — unchanged by this design), readiness report `m3uilnd5`.

## Approaches considered

- **A. Automations become the primary scheduler (chosen).** Extend automation
  triggers with a shared schedule spec, lifecycle, per-project inheritance, and
  calendars. Scheduled tasks later reuse the evaluator.
- **B. Per-project scheduled tasks from a template.** Revive the monitor model.
  Rejected: runner-only evaluation (fires only while a runner watches the project),
  no event triggers, copies drift, and it reverses the monitors → automations
  migration.
- **C. Minimal patches** (lifecycle fields, `cooldown` as an interval, a jitter
  field, webhooks for market days). Rejected: leaves no per-project override, so
  the dream problems (duplicated prompts, double runs) remain.

## Decisions

1. Automations are the primary scheduler. Scheduled tasks remain (decision
   `owyrguh4` stands) and adopt the shared evaluator in a later phase.
2. Scheduling is **slot-based**: each schedule yields scheduled instants (slots);
   an automation fires once per slot per target project.
3. "Last handled slot" is derived from automation run audits (a new structured
   `scheduled_for` field), cached in memory. No new state table, no writes to the
   automation entry, and it survives reindex and database-authoritative storage.
4. Missed slots: the latest missed slot fires once on recovery by default; older
   slots are never replayed. Paused/skipped slots count as handled.
5. Per-project customization uses **bindings**: a project-owned automation with
   `extends: <global id>` that overrides selected fields. One level only.
6. Calendar sources are configured **only in server config**, referenced by name.
   Secret iCal URLs never enter Brain entries.
7. Google Calendar (and other providers) are read via the **secret iCal URL**,
   polled. Google OAuth/push is out of scope.
8. A new `re:` filter form (RE2) is added to the shared filter matcher.
9. Day-of-month + day-of-week switches to standard cron OR semantics, with a
   startup report of affected entries.

## Model

Each automation answers three questions:

| Question | Fields |
|---|---|
| **When** does it fire? | Clock: `schedule` (cron) or `every` + `at`, with `timezone`, `stagger`, `catch_up`. Calendar event: `type: calendar` + `match` + `at` + `offset` |
| **Which days count?** | `calendar` (built-in day calendar), `skip_if_event`, `only_if_event` |
| **For how long?** | Top-level `starts_at`, `expires_at`, `max_runs` (all trigger types) |
| **For which projects, and how?** | `filter.project` selects the default set; project bindings (`extends`) override or opt out |

All new fields are siblings of existing ones; current entries need no change.

```yaml
# global/automation/dream-consolidation.md
type: automation
trigger:
  type: cron
  schedule: "0 3 * * *"          # or: every: 4d, at: "03:00"
  timezone: America/New_York
  stagger: 2h
  filter: { project: "*" }
expires_at: 2027-06-30T00:00:00-04:00   # optional
action: { type: prompt, agent: general, direct_prompt: ... }
```

```yaml
# projects/hindsight/automation/dream.md
type: automation
extends: <dream-consolidation id>
trigger: { every: 2d, at: "01:00" }
action:
  agent: explore
  prompt_append: "Weight decisions about the ingestion pipeline more heavily."
```

## Schedule evaluation

Every minute, for each automation and target project:

```
slot = latest slot <= now          (stagger applied, closed days skipped)
due  = slot > last_handled(automation, project)
       and now - slot <= catch_up
```

- **Cron:** latest slot via a new `pkg/cron` `PrevAtOrBefore`.
- **Intervals:** `every: Nd` + `at: "HH:MM"` steps calendar days in the
  automation's timezone from the anchor (`starts_at`, else the entry's created
  date), so DST never shifts the time of day. Sub-day intervals (`every: 90m`) add
  absolute durations. "Every other Monday" is `every: 14d` with a Monday `starts_at`.
- **Stagger:** `offset = hash(automation ID + project) mod stagger`. Stable per
  project; unaffected by other projects being added or removed.
- **Catch-up:** default fires the latest missed slot once, any lateness.
  `catch_up: <duration>` caps lateness; `catch_up: none` disables it. Slots
  skipped for pause, `max_concurrent`, or `cooldown` are recorded as handled.
- **State:** run audits gain `scheduled_for`. `last_handled` is the newest
  `scheduled_for` per (automation, project), loaded at startup and cached.
- **Dedup:** generated tasks get `generated_key: sched:<automation>:<project>:<slot>`,
  so a slot can never produce two tasks.
- **Upgrade safety:** an automation with no audit history starts its baseline at
  the upgrade, so deploying this does not trigger catch-up runs.
- **Day filters** run in the slot step: a slot on a closed day is skipped, not
  shifted.

## Per-project inheritance (`extends`)

Bindings are field-level overlays: unset fields inherit from the parent.

| Overridable | Not overridable |
|---|---|
| Timing: `schedule`, `every`, `at`, `timezone`, `stagger`, `calendar`, `skip_if_event`, `only_if_event`, `catch_up` | `trigger.type`, `action.type` (create a separate automation instead) |
| Execution: `agent`, `model`, `executor`, `target_workdir`, `execution_mode`, `timeout` | `direct_prompt` (use `prompt_append`) |
| Lifecycle: `starts_at`, `expires_at`, `max_runs`, `status` | `filter.project` (a binding is scoped to its own project) |

Target set for a global parent:

```
targets = projects matching parent filter.project
        + projects with an active binding      (opt in)
        - projects with a non-active binding   (opt out: inactive, expired, completed)
```

Rules:

- The parent is the master switch: a disabled or paused parent stops all projects.
- At most one binding per (parent, project); a second is rejected on save.
- One level: a binding cannot extend a binding.
- A deleted parent leaves its bindings inert; the PWA flags them as broken.
- Scheduler and event loops skip entries with `extends`; bindings are read only
  while resolving a parent's effective config for a project.
- History, dedup keys, `cooldown`, `max_concurrent`, and the stagger offset stay
  keyed to (parent, project). Generated tasks keep `generated_by: automation:<parent>`
  and add `binding: <id>`.
- Bindings work for event-triggered globals too (resolved per project at match time).
- `GET /automations/{id}/effective?project=P` returns the merged config. The PWA
  offers "Customize for this project" and "Turn off here", and labels each field
  inherited or overridden.

## Calendars

### Sources

Configured in server config only; automations reference them by name. This keeps
secret URLs out of Brain and prevents entry authors from making the server fetch
arbitrary URLs.

```yaml
calendars:
  work:
    type: ics
    url_env: BRAIN_CAL_WORK_ICS      # or url_file: /run/secrets/cal_work
    poll: 5m                         # minimum 1m
  xnys:
    type: builtin
    market: XNYS
    extra_closed: ["2025-01-09"]
```

### ICS poller (`internal/calendar`)

- Conditional GET (ETag / If-Modified-Since), timeout, and size cap.
- Parses VEVENTs and expands RRULE/RDATE/EXDATE and RECURRENCE-ID overrides over
  [now − 1d, now + 14d]. Drops cancelled events; recognizes all-day events; maps
  TZIDs to IANA zones.
- Keeps the last good snapshot in memory and in the data directory (not as entries).
- On repeated failure it serves the last good snapshot for 24h, then marks the
  source stale and raises an attention notification.
- `GET /calendars` reports name, type, last fetch, event count, and last error.
  It never returns the URL.

### Built-in XNYS (NYSE/NASDAQ)

Weekends closed. Holidays: New Year's Day, Martin Luther King Jr. Day, Washington's
Birthday, Good Friday (Gregorian Easter computus), Memorial Day, Juneteenth (from
2022), Independence Day, Labor Day, Thanksgiving, Christmas. Saturday holidays are
observed Friday and Sunday holidays Monday, except New Year's Day on a Saturday,
which is not observed. Known historic one-off closures ship in code; `extra_closed`
and `extra_open` cover new ones without a release. Early closes are out of scope.

### Day filters

Evaluated on the slot's local date in the automation's timezone.

- `calendar: xnys`: slot allowed only on days the built-in calendar is open.
  Only built-in day calendars are valid here.
- `skip_if_event` / `only_if_event`: `{ calendar: work, title: "re:..." }`. A
  multi-day event counts on every day it covers.

### Calendar event trigger

```yaml
trigger:
  type: calendar
  calendar: work
  match:
    title: "re:(?i)^1:1 (?P<person>.+)$"
  at: start          # start | end
  offset: -15m       # within ±7d
```

- `match` keys: `title`, `description`, `location`, `all_day`, using the shared
  filter forms (`*`, `in:`, `has:`, `re:`).
- Named capture groups become `{{.Match.<name>}}`. Event fields are available as
  `{{.Event.Title}}`, `.Start`, `.End`, `.Location`, `.Description`, `.Calendar`,
  `.AllDay`.
- Slot = occurrence start or end + `offset`. Default `catch_up: 1h`.
- Dedup key `cal:<automation>:<uid>:<occurrence-start>`: each occurrence fires
  once; a moved meeting fires at its new time; a cancelled one does not fire.
- Runs in the automation's own project; never fans out; `extends` is invalid.
- Validation on save: unknown calendar name, regex that fails to compile, or
  `offset` outside ±7d are rejected.
- **Prompt injection:** invite senders control event text. `Description` and
  `Location` render inside a block labelled as untrusted calendar data, and the
  docs tell prompt authors to treat it as data.
- Known limitation: ICS does not reliably expose your own RSVP, so declined
  meetings still match.

## Lifecycle

Applies to every trigger type.

| Field | Behavior |
|---|---|
| `starts_at` | Nothing evaluated before it; also the `every` anchor |
| `expires_at` | After it, status becomes `completed` with an "Expired" note (same as scheduled tasks) |
| `max_runs` | Counted per (automation, project); only runs that produced work count. A project-owned automation becomes `completed`; for a global parent, only that project stops |

## Migration and compatibility

1. New fields are optional. Behavior changes only for catch-up (baseline at
   upgrade) and day-of-month + day-of-week (OR semantics). At startup, automations
   and scheduled tasks whose meaning changes are logged and raised as an attention
   notification.
2. `brain migrate automations` (dry run first) converts enabled
   `monitor:dream:project:P` tasks into bindings that keep their schedule, agent,
   and model, then disables the monitor schedules.
3. The shipped dream template gains `stagger: 2h`; the migrate command offers to
   apply it to an installed copy.
4. New fields and endpoints (`/automations/{id}/effective`, `/calendars`) are
   added to `api/openapi.yaml` and `api/operation-policy.yaml` with explicit
   deltas to `internal/sdkcontract` and source-freeze guards (no rebaselines).
   MCP save/update accept the new fields.
5. Run audits gain a structured `scheduled_for` field (not parsed from the body).

## Shared evaluator

`pkg/schedule` owns "when": cron, `every`/`at`, timezone, stagger, day filters,
and catch-up, exposing `LatestSlot` and `NextSlot`. It depends on `pkg/cron`
(plus `PrevAtOrBefore`) and a calendar interface. Automations use it from phase 2;
scheduled tasks adopt it in phase 6. Reminders keep their simple repeat.

## Testing

- `pkg/schedule`: table tests with fixed clocks across DST transitions (both
  directions), month ends, and leap years; property tests that `NextSlot` and
  `LatestSlot` agree; stagger stability.
- XNYS: golden comparison with NYSE's published holiday tables for 2024–2027.
- ICS: fixture feeds (RRULE, EXDATE, RECURRENCE-ID, all-day, cancelled, TZID);
  `httptest` server for conditional GET, failures, staleness, and the notification.
- Automation service with a fake clock: catch-up after downtime, no burst after
  unpause, upgrade baseline, binding precedence and opt-in/out, dedup, expiry.
- Verification: `just check`, plus one end-to-end run against a real Google
  secret iCal feed.

## Phases

Each phase ships independently.

1. Automation lifecycle (`starts_at`, `expires_at`, `max_runs`) and the `re:` filter form.
2. `pkg/schedule`: slots, `every`/`at`, stagger, catch-up, `scheduled_for`, and
   the day-of-month/day-of-week fix. Dreams stop firing all at once.
3. `extends` bindings, effective-config API, PWA customization, and dream-monitor
   migration. Each project gets its own dream schedule and agent.
4. Built-in XNYS day filter.
5. ICS sources: poller, `skip_if_event` / `only_if_event`, `type: calendar`
   trigger, attention notifications.
6. Later: scheduled tasks adopt `pkg/schedule`.

## Out of scope

Market early closes, declined-meeting detection, Google OAuth/push, per-tenant
calendar secrets, full prompt replacement in bindings, multi-level inheritance,
and replaying more than one missed slot.

## Addendum: implementation decisions (2026-10-08)

Codebase validation before tasking found gaps the design left open. These
defaults are chosen so implementation tasks do not have to guess.

### Corrections

- **Lifecycle fields already exist** on entries (`starts_at`, `expires_at`,
  `max_runs`, `timezone`; `internal/types/types.go`). Phase 1 is enforcement plus
  three round-trip fixes: raw-file edits drop them
  (`mapFrontmatterToUpdateRequest`, `internal/api/entries.go`), MCP `save`
  forwards them only for tasks, and lifecycle edits must go through
  `PATCH /entries`, not `/metadata`.
- **Expiry is not "the same as scheduled tasks".** The runner sets
  `schedule_enabled: false` on a task. An expired automation's status becomes
  `completed` with an "Expired: expires_at passed" note. The write is
  revision-guarded (recall, then update with `expected_revision`).

### Decisions

1. **System attention recipients.** `server.attention.system_recipients` lists
   token names. If empty, system notices fan out to recipients that have a push
   device or an existing attention item; if there are none, they are only logged.
   The notifier is injected through a nil-safe setter, because the attention
   service is built after background workers start.
2. **Run audits.** `automation_run` entries get a typed `scheduled_for` field and
   tags `automation:<parent-id>` (plus `binding:<id>` when a binding applied).
   Body lines stay for existing readers. `/automation-runs` uses the tag as a fast
   path with the body scan as fallback.
3. **Slot floor.** A slot fires only if it is newer than all of: `last_handled`,
   the parent's `modified` time, the binding's `modified` time (if any), and
   `starts_at`. Editing a schedule or creating a binding never fires a past slot.
4. **Catch-up cap.** At most one catch-up run per automation per tick. On-time
   runs (slot within the current minute) are not capped.
5. **`max_runs` counting.** Counts audits that created work (queued, success,
   failed). Skipped runs and manual runs do not count. Manual runs ignore
   lifecycle and schedule, as they already ignore pause.
6. **Day-of-month/day-of-week.** Vixie rule: OR only when neither field starts
   with `*`. Remote runners on older versions keep AND until upgraded (accepted).
7. **DST.** For `every` + `at`: a nonexistent local time fires at the first valid
   instant after it (02:30 → 03:00); a repeated local time fires at its first
   occurrence only. For cron: the existing gap behavior stays (a nonexistent hour
   does not fire); a repeated local time fires once, at its first occurrence.
8. **Stagger and day filters.** Day eligibility uses the base slot's local date,
   before the stagger offset. The offset hash is FNV-1a 64 over
   `automationID + "\x00" + project`.
9. **`every` grammar.** `<positive integer><unit>`, unit `m`, `h`, `d`, or `w`.
   `at: "HH:MM"` (24-hour) is valid only with `d` or `w`. Sub-day intervals
   anchor at `starts_at`, else the entry's created instant.
10. **Binding lookup.** Bindings are auto-tagged `extends:<parent-id>`. The
    scheduler reads bindings of every status, so it sees opt-outs. If concurrent
    saves create two bindings for one (parent, project), the oldest (created, then
    ID) wins and the others are flagged broken.
11. **Overlay presence.** A binding field overrides only when present. Explicit
    zero values: durations accept `0s`, `catch_up` accepts `none`, and `max_runs`
    uses `-1` for unlimited (`0` or absent inherits).
12. **`re:` safety.** Patterns up to 512 characters; matched input truncated at
    4 KiB; bounded compile cache (256); an invalid pattern at runtime matches
    nothing and logs a warning.
13. **Startup scheduling report.** One report, raised as an attention item
    (deduplicated by content hash), lists: expressions whose day-of-month/day-of-week
    meaning changes (automations, tasks, `feature_schedule`), automations whose
    existing lifecycle fields become enforced, and filter values that already start
    with `re:`.
14. **Prompt-injection fencing** covers every event-derived value, including
    `Title` and `{{.Match.*}}`. The fence terminator is neutralized inside data.
15. **ICS fetch hardening.** HTTPS only, at most 5 redirects, 10 MiB cap, 30 s
    timeout, no credentials sent. URLs are stripped from every error (`*url.Error`
    → its inner error) before logging, `GET /calendars`, or attention bodies.
16. **Bindings and tenancy.** Bindings are single-mode only until a tenant
    authorization rule exists, because a project writer can override agent,
    executor, and workdir on a global prompt.
17. **Integration.** SDK-contract changes start after hosted-MCP-on-SDK step 3
    lands. Multi-tenant source-freeze deltas (`internal/p8inventory`,
    `internal/tenantfs`) are applied when the integration branch next merges main,
    not per task on main.

## Implementation notes (2026-10-09)

Where the build differs from, or refines, the design above and the addendum.

- **Opt-out status.** Any non-active binding status opts a project out. The PWA
  writes `archived`; `inactive` is not a valid entry status.
- **Slot floor.** The floor uses the entry's modified time. An out-of-band touch of
  the file can skip a slot that falls in the same minute.
- **Bindings.**
  - Found by the `extends:<parent>` tag. A hand-edited binding without the tag is
    invisible until it is re-saved.
  - Rejected outside the local tenant.
  - Bindings of project-owned parents never run.
  - A binding's `max_runs` writes a skip and never completes the parent.
  - If a binding is removed while the server is down, the parent's latest missed
    slot can fire once after restart.
- **`max_runs`.**
  - Counts audits that created work (`queued`, `success`, `failed`). Manual runs
    (tagged `manual`) and skips do not count.
  - Audits written before `scheduled_for` existed are counted too. The count
    checks only the `automation:<id>` tag and status, not the write date.
  - The count is not atomic across concurrent fires.
- **Catch-up budget.** A skipped catch-up counts toward the one-catch-up-per-tick
  budget.
- **Skip reasons.** Besides `paused`, `cooldown`, `max_concurrent`, `max_runs` and
  `calendar_non_prompt_action`, the build writes `dedup`, when a slot or occurrence
  key already produced a task.
- **ICS and day filters.**
  - ICS sources have no per-source timezone; floating times are UTC.
  - Snapshots cover [now − 1 day, now + 14 days].
  - Without data, or outside the window, `skip_if_event` fails open and
    `only_if_event` fails closed.
- **Calendar triggers.**
  - Accept only prompt actions. A script action could carry invite text into a
    shell command.
  - Fenced fields are `UID`, `Title`, `Description`, `Location`, `Calendar` and
    match captures. `Start`, `End` and `AllDay` are not fenced. The design named
    only `Description` and `Location`.
  - Declined meetings still match.
- **URL redaction.** The poller strips the URL path, query and fragment from every
  error. Hostnames can still appear in DNS and TLS errors, because the provider may
  place the secret in the path. A malformed but parseable feed reports success with
  zero events.
- **System notices.** They go to `server.attention.system_recipients`. Without that,
  they go to users who already own attention items. Users known only by a push
  device are not included yet. With neither, they are logged only.
- **Deferred.** Phase 6 (runner scheduled tasks adopting `pkg/schedule`).

## Verification and late fixes (2026-10-09)

- Every task was merged into `automation-scheduling` only after its own tests,
  `go vet` and golangci-lint passed, then re-verified on the integration branch.
- An independent adversarial review of the ICS poller passed all eight checks
  (no secret in errors, logs, status, snapshots or notices; https-only and
  redirect rules; size and time limits; 0600/0700 files; rotation; stale
  episodes; `-race`; tenant isolation).
- A live end-to-end run against an isolated server (temporary HOME and brain
  dir, port 3399) used Google's public US-holidays iCal feed:
  - `GET /calendars` listed the ICS source (fetched, 1 event in window) and
    `xnys`; the secret URL path appeared in neither the server log nor the
    snapshot (0600 file, 0700 directory).
  - A `type: calendar` automation matching `re:(?i)^(?P<name>columbus) day$`
    fired exactly once at its slot, with the title and capture fenced as
    untrusted data and dedup key `cal:<id>:<uid>:<occurrence start>`.
  - A global dream with `stagger: 2h` projected different per-project times;
    a binding moved one project to `every: 2d` at 01:00 with agent `explore`;
    an `archived` binding opted a project out.
  - `calendar: xnys` at 09:30 New York skipped Thanksgiving and the weekend.
  - A calendar trigger with a script action and an unknown calendar name were
    rejected with 400 on the named field.
- Bugs found late and fixed:
  - Calendar triggers with script actions could render invite text into a shell
    command. Calendar triggers now accept only prompt actions (save-time and
    runtime), and script commands never receive event data.
  - `server.calendars` and `server.attention` never reached a running server:
    the CLI copies server settings through four structs field by field and
    none carried them. Both are threaded through, with a test per hop.
- Existing behaviour worth knowing: wildcard and selector fan-out only reaches
  projects that have a `task/` directory (`TaskServiceImpl.ListProjects`);
  note-only projects are not dreamed until they have a task.
