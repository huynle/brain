import type { ReminderSummary } from "./types";

export const PROJECT_FILTER_SIDEBAR = "";
export const PROJECT_FILTER_ALL = "*";
export const PROJECT_FILTER_GLOBAL = "global";

export type ReminderLifecycle =
  | "all"
  | "waiting"
  | "scheduled"
  | "stopped"
  | "someday"
  | "executed"
  | "agent";

export type ReminderProjectScope =
  | { kind: "all" }
  | { kind: "global" }
  | { kind: "project"; project: string }
  | { kind: "set"; projects: string[] };

export interface ReminderFilter {
  lifecycle: ReminderLifecycle;
  projectScope: ReminderProjectScope;
  query: string;
}

export type ReminderGroups = Record<
  Exclude<ReminderLifecycle, "all">,
  ReminderSummary[]
>;

export function isAgentRun(reminder: ReminderSummary): boolean {
  return reminder.action === "task" && Boolean(reminder.generated_task_id);
}

export function groupReminders(rows: ReminderSummary[]): ReminderGroups {
  const groups: ReminderGroups = {
    waiting: [] as ReminderSummary[],
    scheduled: [] as ReminderSummary[],
    stopped: [] as ReminderSummary[],
    someday: [] as ReminderSummary[],
    executed: [] as ReminderSummary[],
    agent: [] as ReminderSummary[],
  };
  for (const reminder of rows) {
    if (isAgentRun(reminder)) groups.agent.push(reminder);
    switch (reminder.state) {
      case "fired":
        groups.waiting.push(reminder);
        break;
      case "armed":
        groups.scheduled.push(reminder);
        break;
      case "paused":
        groups.stopped.push(reminder);
        break;
      case "undated":
        groups.someday.push(reminder);
        break;
      case "done":
        groups.executed.push(reminder);
        break;
    }
  }
  return groups;
}

export function reminderCounts(rows: ReminderSummary[]) {
  const groups = groupReminders(rows);
  return {
    all: rows.length,
    waiting: groups.waiting.length,
    scheduled: groups.scheduled.length,
    stopped: groups.stopped.length,
    someday: groups.someday.length,
    executed: groups.executed.length,
    agent: groups.agent.length,
  };
}

export function resolveReminderScope(
  projectFilter: string,
  sidebar: { projects: string[]; unfiltered: boolean; loading: boolean },
): ReminderProjectScope {
  if (projectFilter === PROJECT_FILTER_ALL) return { kind: "all" };
  if (projectFilter === PROJECT_FILTER_GLOBAL) return { kind: "global" };
  if (projectFilter !== PROJECT_FILTER_SIDEBAR) {
    return { kind: "project", project: projectFilter };
  }
  if (sidebar.loading || sidebar.unfiltered) return { kind: "all" };
  return { kind: "set", projects: [...sidebar.projects, "global"] };
}

export function filterReminders(
  rows: ReminderSummary[],
  filter: ReminderFilter,
): ReminderSummary[] {
  const query = filter.query.trim().toLocaleLowerCase();
  const projectMatches = (reminder: ReminderSummary): boolean => {
    const project = reminder.project || "global";
    switch (filter.projectScope.kind) {
      case "all":
        return true;
      case "global":
        return project === "global";
      case "project":
        return project === filter.projectScope.project;
      case "set":
        return filter.projectScope.projects.includes(project);
    }
  };
  const lifecycleMatches = (reminder: ReminderSummary): boolean => {
    switch (filter.lifecycle) {
      case "all":
        return true;
      case "waiting":
        return reminder.state === "fired";
      case "scheduled":
        return reminder.state === "armed";
      case "stopped":
        return reminder.state === "paused";
      case "someday":
        return reminder.state === "undated";
      case "executed":
        return reminder.state === "done";
      case "agent":
        return isAgentRun(reminder);
    }
  };
  return rows.filter(
    (reminder) =>
      projectMatches(reminder) &&
      lifecycleMatches(reminder) &&
      (!query ||
        reminder.title.toLocaleLowerCase().includes(query) ||
        (reminder.project || "global").toLocaleLowerCase().includes(query)),
  );
}
