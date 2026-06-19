// Normalizes automation entries, scheduled tasks, and automation_run records
// into a single ordered row list — a TypeScript port of the TUI's
// internal/tui/automation_list.go so the PWA shows the same unified view.

import { GOAL_GENERATED_BY, type BrainEntry } from "../../lib/types";

export const AUTOMATION_RUN_TASK_PAGE_SIZE = 10;

export type AutomationDisplayEntry =
  | { kind: "auto"; row: AutomationRow }
  | { kind: "task"; task: BrainEntry; parent: AutomationRow }
  | { kind: "show-more"; parent: AutomationRow; shown: number; total: number; remaining: number };

export interface AutomationRow {
  id: string;
  path: string;
  title: string;
  source: "automation" | "task";
  scope: string; // "built-in" | "project" | "unknown"
  status: string;
  enabled: boolean;
  isGoal: boolean;
  featureId: string;
  triggerKind: string;
  triggerDetail: string;
  priority?: string;
  runSummary: string;
  runTaskID: string;
}

interface RunSummary {
  pending: number;
  active: number;
  inactive: number;
  latestID: string;
  latestStatus: string;
  realRunCount: number;
}

function summaryText(s: RunSummary): string {
  if (s.realRunCount > 0) {
    return `last ${s.latestStatus || "unknown"} (${s.realRunCount} runs)`;
  }
  const parts: string[] = [];
  if (s.pending > 0) parts.push(`${s.pending} queued`);
  if (s.active > 0) parts.push(`${s.active} running`);
  if (s.inactive > 0) parts.push(`${s.inactive} done`);
  return parts.join(", ");
}

function runContentField(content: string | undefined, field: string): string {
  if (!content) return "";
  const prefix = field + ":";
  for (const raw of content.split("\n")) {
    const line = raw.trim();
    if (line.startsWith(prefix)) return line.slice(prefix.length).trim();
  }
  return "";
}

// Builds run summaries keyed by automation id from automation_run records and
// automation-generated task entries.
function buildRunSummaries(tasks: BrainEntry[], runs: BrainEntry[]): Map<string, RunSummary> {
  const summaries = new Map<string, RunSummary>();
  const get = (id: string): RunSummary =>
    summaries.get(id) ??
    { pending: 0, active: 0, inactive: 0, latestID: "", latestStatus: "", realRunCount: 0 };

  for (const run of runs) {
    const automationID = runContentField(run.content, "automation_id");
    if (!automationID) continue;
    const s = get(automationID);
    s.realRunCount++;
    s.latestID = run.id;
    s.latestStatus = run.status;
    summaries.set(automationID, s);
  }

  for (const task of tasks) {
    if (!task.generated_by?.startsWith("automation:")) continue;
    const automationID = task.generated_by.slice("automation:".length);
    if (!automationID) continue;
    const s = get(automationID);
    switch (task.status) {
      case "pending":
        s.pending++;
        break;
      case "active":
      case "in_progress":
        s.active++;
        break;
      default:
        s.inactive++;
    }
    if (!s.latestID) {
      s.latestID = task.id;
      s.latestStatus = task.status;
    }
    summaries.set(automationID, s);
  }
  return summaries;
}

function entryScope(entry: BrainEntry): string {
  if (entry.path.startsWith("global/")) return "built-in";
  if (entry.project_id || entry.path.startsWith("projects/")) return "project";
  return "unknown";
}

function triggerOf(entry: BrainEntry): { kind: string; detail: string } {
  const t = entry.trigger;
  if (!t || !t.type) return { kind: "", detail: "" };
  switch (t.type) {
    case "event":
      return { kind: "event", detail: t.event ?? "" };
    case "cron":
      return { kind: "cron", detail: t.schedule ?? "" };
    case "webhook":
      return { kind: "webhook", detail: t.webhook ?? "" };
    case "session":
      return { kind: "session", detail: t.event ?? "runner.session_discovered" };
    default:
      return { kind: t.type, detail: "" };
  }
}

function rowFromAutomation(entry: BrainEntry, runs: Map<string, RunSummary>): AutomationRow {
  const trig = triggerOf(entry);
  const summary = runs.get(entry.id);
  const scope = entryScope(entry);
  const status = scope === "built-in" ? "archived" : entry.status;
  return {
    id: entry.id,
    path: entry.path,
    title: entry.title,
    source: "automation",
    scope,
    status,
    enabled: status === "active",
    isGoal: entry.generated_by === GOAL_GENERATED_BY,
    featureId: entry.feature_id ?? "",
    triggerKind: trig.kind,
    triggerDetail: trig.detail,
    priority: entry.priority,
    runSummary: summary ? summaryText(summary) : "",
    runTaskID: summary?.latestID ?? "",
  };
}

function rowFromScheduledTask(task: BrainEntry): AutomationRow {
  const runOnce = !!task.run_once_at && !task.schedule;
  return {
    id: task.id,
    path: task.path,
    title: task.title,
    source: "task",
    scope: "project",
    status: task.status,
    enabled: task.schedule_enabled !== false,
    isGoal: false,
    featureId: task.feature_id ?? "",
    triggerKind: runOnce ? "run_once" : "cron",
    triggerDetail: runOnce ? (task.run_once_at ?? "") : (task.schedule ?? ""),
    priority: task.priority,
    runSummary: "",
    runTaskID: "",
  };
}


function automationTemplateKey(entry: BrainEntry): string {
  if (entry.generated_by) return `generated:${entry.generated_by}`;
  if (entry.title) return `title:${entry.title.trim().toLowerCase()}`;
  return entry.path || entry.id;
}

// triggerLabel renders the "event:…" / "cron:…" string the TUI shows.
export function triggerLabel(row: AutomationRow): string {
  if (!row.triggerKind) return "manual";
  return row.triggerDetail ? `${row.triggerKind}:${row.triggerDetail}` : row.triggerKind;
}

// normalizeAutomationRows mirrors AutomationList.SetEntryRows: automation
// entries + scheduled task entries, sorted built-ins first then by title.
export function normalizeAutomationRows(
  automations: BrainEntry[],
  tasks: BrainEntry[],
  runs: BrainEntry[],
): AutomationRow[] {
  const summaries = buildRunSummaries(tasks, runs);
  const byTemplate = new Map<string, BrainEntry>();
  for (const entry of automations) {
    const key = automationTemplateKey(entry);
    const existing = byTemplate.get(key);
    if (!existing || entryScope(entry) === "project") byTemplate.set(key, entry);
  }
  const rows: AutomationRow[] = [...byTemplate.values()].map((e) => rowFromAutomation(e, summaries));

  for (const task of tasks) {
    if (task.schedule || task.run_once_at) rows.push(rowFromScheduledTask(task));
  }

  const rank = (r: AutomationRow) => (r.scope === "built-in" ? 0 : r.source === "automation" ? 1 : 2);
  return rows.sort((a, b) => rank(a) - rank(b) || a.title.localeCompare(b.title));
}

// childRunTasks returns the automation-generated task entries for a row,
// newest first (for the expandable run-task list). The `tasks` array is
// already the type=task result, and the list endpoint omits the `type` field,
// so we match on generated_by alone.
export function childRunTasks(rowID: string, tasks: BrainEntry[]): BrainEntry[] {
  const generatedBy = "automation:" + rowID;
  return tasks
    .filter((t) => t.generated_by === generatedBy)
    .sort((a, b) => (b.modified ?? "").localeCompare(a.modified ?? "") || b.id.localeCompare(a.id));
}

export function automationShowMoreKey(parentID: string): string {
  return `automation-show-more:${parentID}`;
}

export function flattenAutomationDisplay(
  rows: AutomationRow[],
  tasks: BrainEntry[],
  expandedID: string | null,
  visibleRunTaskLimits: Record<string, number>,
): AutomationDisplayEntry[] {
  const out: AutomationDisplayEntry[] = [];
  for (const row of rows) {
    out.push({ kind: "auto", row });
    if (expandedID !== row.id) continue;

    const children = childRunTasks(row.id, tasks);
    const limit = visibleRunTaskLimits[row.id] ?? AUTOMATION_RUN_TASK_PAGE_SIZE;
    const shown = Math.min(limit, children.length);
    for (const task of children.slice(0, shown)) {
      out.push({ kind: "task", task, parent: row });
    }
    if (shown < children.length) {
      out.push({
        kind: "show-more",
        parent: row,
        shown,
        total: children.length,
        remaining: children.length - shown,
      });
    }
  }
  return out;
}
