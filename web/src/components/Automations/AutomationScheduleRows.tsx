/**
 * Read-only rows for an automation's trigger cadence and lifecycle window,
 * for the config section's kv-grid.
 *
 * Shows only what is set, so an automation with no calendar, stagger or
 * window renders nothing and the config list does not grow for the common
 * case. Nothing here edits: the PWA has no scheduling editor.
 */
import * as React from "react";

import type { BrainEntry, CalendarEventFilter, TriggerConfig } from "../../lib/types";

export interface AutomationScheduleRowsProps {
  automation: Pick<BrainEntry, "trigger" | "timezone" | "starts_at" | "expires_at" | "max_runs">;
}

interface Row {
  k: string;
  v: string;
}

/** "calendar=xnys, all_day=true" for the set fields of a filter, or null. */
function filterText(f?: CalendarEventFilter): string | null {
  if (!f) return null;
  const parts = Object.entries(f)
    .filter(([, v]) => v)
    .map(([k, v]) => `${k}=${v}`);
  return parts.length ? parts.join(", ") : null;
}

/**
 * "none" turns catch-up off; any other value is a duration that caps how far
 * back missed runs are replayed. An empty value is the server's default and
 * is not shown (see automationScheduleRows).
 */
function catchUpText(v: string): string {
  if (v === "none") return "none (missed runs are skipped)";
  return `missed runs within ${v}`;
}

/** The rows for the fields that are set, in display order. */
export function automationScheduleRows(
  a: AutomationScheduleRowsProps["automation"],
): Row[] {
  const t: TriggerConfig = a.trigger ?? {};
  const rows: Row[] = [];
  if (t.every) rows.push({ k: "Every", v: t.every });
  if (t.at) rows.push({ k: "At", v: t.at });
  if (t.stagger) rows.push({ k: "Stagger", v: `per-project offset within ${t.stagger}` });
  if (t.catch_up) rows.push({ k: "Catch-up", v: catchUpText(t.catch_up) });
  if (t.calendar) rows.push({ k: "Calendar", v: t.calendar });
  const skip = filterText(t.skip_if_event);
  if (skip) rows.push({ k: "Skip if event", v: skip });
  const only = filterText(t.only_if_event);
  if (only) rows.push({ k: "Only if event", v: only });
  const tz = t.timezone || a.timezone;
  if (tz) rows.push({ k: "Timezone", v: tz });
  if (a.starts_at) rows.push({ k: "Starts", v: a.starts_at });
  if (a.expires_at) rows.push({ k: "Expires", v: a.expires_at });
  // max_runs of 0 means no cap, matching TaskScheduleSection.
  if (a.max_runs && a.max_runs > 0) rows.push({ k: "Max runs", v: String(a.max_runs) });
  return rows;
}

export function AutomationScheduleRows({ automation }: AutomationScheduleRowsProps): JSX.Element {
  return (
    <>
      {automationScheduleRows(automation).map((r) => (
        <React.Fragment key={r.k}>
          <div className="k">{r.k}</div>
          <div className="v">{r.v}</div>
        </React.Fragment>
      ))}
    </>
  );
}
