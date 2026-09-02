/**
 * Automation run audits — parsing and outcome derivation.
 *
 * The server records every automation firing as an `automation_run`
 * entry whose BODY is the audit (internal/service/automation_service.go,
 * `createRunAudit`). None of those fields are columns, so this module is
 * the client-side reader for that format:
 *
 *   ## Automation Run Audit
 *
 *   automation_id: wvnbqz7w
 *   automation_path: projects/demo/automation/wvnbqz7w.md
 *   project: demo
 *   trigger_type: cron
 *   trigger_event: * * * * *
 *   source_event_id: …          (event triggers only)
 *   dedup_key: …
 *   started_at: 2026-08-21T10:49:19Z
 *   completed_at: 2026-08-21T10:49:19Z
 *   duration_ms: 0
 *   skip_reason: cooldown       (skipped runs only)
 *   error: …                    (failed runs only)
 *
 *   ### Trigger Payload Summary
 *   - project_id: demo
 *
 *   ### Generated Tasks
 *   - f9eskoor
 *
 * Pure and dependency-free so it stays unit-testable, and so both the
 * modal's Runs tab and the docked Runs pane read the audit exactly the
 * same way.
 */
import type { BrainEntry } from "./types";

export interface AutomationRun {
  /** The run entry's own id — the thing GET /automation-runs/{id} takes. */
  id: string;
  path: string;
  /** The entry's stored status. Advisory only: see `runOutcome`. */
  entryStatus: string;
  created: string;
  automationId: string;
  automationPath: string;
  project: string;
  triggerType: string;
  triggerEvent: string;
  sourceEventId: string;
  dedupKey: string;
  startedAt: string;
  completedAt: string;
  /** undefined when the audit carried no (or an unparseable) duration. */
  durationMs?: number;
  skipReason: string;
  error: string;
  /** Ids of the tasks this run generated; empty for skips and no-ops. */
  taskIds: string[];
  /** "key: value" lines of the trigger payload summary, in audit order. */
  payload: Array<{ key: string; value: string }>;
}

/**
 * What the run DID, derived from the audit body rather than the entry's
 * status field.
 *
 * The body is written once, by the code that made the decision, and says
 * why. The status field is a generic entry column that later bulk edits
 * and migrations can (and in practice do) overwrite — runs created with
 * status "queued" have been observed sitting at "blocked" with an
 * untouched body. So the body wins, and the status is shown beside it as
 * a secondary fact rather than driving the glyph.
 */
export type RunOutcome = "generated" | "skipped" | "error" | "noop";

export function runOutcome(run: AutomationRun): RunOutcome {
  if (run.error) return "error";
  if (run.skipReason) return "skipped";
  if (run.taskIds.length > 0) return "generated";
  return "noop";
}

export function outcomeGlyph(outcome: RunOutcome): string {
  switch (outcome) {
    case "generated":
      return "✓";
    case "skipped":
      return "⊘";
    case "error":
      return "✕";
    default:
      return "·";
  }
}

/** Maps to the shared `.glyph` tone classes (ok / blk / muted). */
export function outcomeTone(outcome: RunOutcome): string {
  switch (outcome) {
    case "generated":
      return "ok";
    case "error":
      return "blk";
    default:
      return "";
  }
}

export function outcomeLabel(run: AutomationRun): string {
  switch (runOutcome(run)) {
    case "generated":
      return run.taskIds.length === 1
        ? "generated 1 task"
        : `generated ${run.taskIds.length} tasks`;
    case "skipped":
      return `skipped: ${run.skipReason}`;
    case "error":
      return `error: ${run.error}`;
    default:
      return "no work generated";
  }
}

/** Trigger, as one line: "cron · * * * * *". */
export function triggerLabel(run: AutomationRun): string {
  const type = run.triggerType || "manual";
  return run.triggerEvent ? `${type} · ${run.triggerEvent}` : type;
}

/** Human duration for the run's own audit ("0.4s", "1.2m", "" if absent). */
export function durationLabel(run: AutomationRun): string {
  const ms = run.durationMs;
  if (ms === undefined) return "";
  if (ms < 1000) return `${ms}ms`;
  const s = ms / 1000;
  if (s < 60) return `${s < 10 ? s.toFixed(1) : Math.round(s)}s`;
  const m = s / 60;
  return `${m < 10 ? m.toFixed(1) : Math.round(m)}m`;
}

/**
 * When the run happened. `started_at` is what the audit means; `created`
 * is the entry's own timestamp and stands in when the body is malformed,
 * so a run is never rendered without a time.
 */
export function runTime(run: AutomationRun): string {
  return run.startedAt || run.created;
}

export function parseAutomationRun(entry: BrainEntry): AutomationRun {
  const content = entry.content ?? "";
  const fields = new Map<string, string>();
  const payload: Array<{ key: string; value: string }> = [];
  const taskIds: string[] = [];

  // Sections are delimited by "### " headings; the fields live in the
  // preamble, and the two lists each own a section. Tracking the section
  // keeps a payload line like "- task_id: x" out of the field map.
  let section: "head" | "payload" | "tasks" | "other" = "head";
  for (const raw of content.split("\n")) {
    const line = raw.trim();
    if (line.startsWith("###")) {
      const heading = line.replace(/^#+\s*/, "").toLowerCase();
      section = heading.startsWith("trigger payload")
        ? "payload"
        : heading.startsWith("generated tasks")
          ? "tasks"
          : "other";
      continue;
    }
    if (!line || line.startsWith("##")) continue;

    if (section === "head") {
      const idx = line.indexOf(":");
      if (idx > 0) {
        const key = line.slice(0, idx).trim();
        // Values can themselves contain colons (an error message, a cron
        // expression), so only the FIRST colon separates.
        if (!fields.has(key)) fields.set(key, line.slice(idx + 1).trim());
      }
      continue;
    }
    if (!line.startsWith("-")) continue;
    const item = line.replace(/^-\s*/, "");
    if (section === "payload") {
      const idx = item.indexOf(":");
      if (idx > 0) {
        payload.push({
          key: item.slice(0, idx).trim(),
          value: item.slice(idx + 1).trim(),
        });
      }
    } else if (section === "tasks") {
      // "- none" is the audit's own placeholder for an empty list, not a
      // task id. Taking it literally put a phantom task on every skip.
      if (item && item !== "none") taskIds.push(item);
    }
  }

  const durationRaw = fields.get("duration_ms");
  const durationMs =
    durationRaw !== undefined &&
    durationRaw !== "" &&
    !Number.isNaN(Number(durationRaw))
      ? Number(durationRaw)
      : undefined;

  return {
    id: entry.id,
    path: entry.path ?? "",
    entryStatus: entry.status ?? "",
    created: entry.created ?? "",
    automationId: fields.get("automation_id") ?? "",
    automationPath: fields.get("automation_path") ?? "",
    project: fields.get("project") ?? entry.project_id ?? "",
    triggerType: fields.get("trigger_type") ?? "",
    triggerEvent: fields.get("trigger_event") ?? "",
    sourceEventId: fields.get("source_event_id") ?? "",
    dedupKey: fields.get("dedup_key") ?? "",
    startedAt: fields.get("started_at") ?? "",
    completedAt: fields.get("completed_at") ?? "",
    durationMs,
    skipReason: fields.get("skip_reason") ?? "",
    error: fields.get("error") ?? "",
    taskIds,
    payload,
  };
}

/** Parse a page of run entries, newest first. */
export function parseAutomationRuns(
  entries: readonly BrainEntry[],
): AutomationRun[] {
  return entries
    .map(parseAutomationRun)
    .sort((a, b) => runTime(b).localeCompare(runTime(a)));
}

/**
 * The newest run per automation, for the "last run" cell on the
 * automation list. Input need not be sorted.
 */
export function latestRunByAutomation(
  runs: readonly AutomationRun[],
): Map<string, AutomationRun> {
  const latest = new Map<string, AutomationRun>();
  for (const run of runs) {
    if (!run.automationId) continue;
    const seen = latest.get(run.automationId);
    if (!seen || runTime(run).localeCompare(runTime(seen)) > 0) {
      latest.set(run.automationId, run);
    }
  }
  return latest;
}

/** How many runs each automation has in the fetched window. */
export function runCountByAutomation(
  runs: readonly AutomationRun[],
): Map<string, number> {
  const counts = new Map<string, number>();
  for (const run of runs) {
    if (!run.automationId) continue;
    counts.set(run.automationId, (counts.get(run.automationId) ?? 0) + 1);
  }
  return counts;
}
