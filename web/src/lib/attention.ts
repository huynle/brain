import type { Attention, AttentionSeverity } from "./types";

export const PROJECT_FILTER_SIDEBAR = "";
export const PROJECT_FILTER_ALL = "*";
export const PROJECT_FILTER_GLOBAL = "global";

/** The lifecycle tabs the inbox offers. `all` shows everything in scope. */
export type AttentionLifecycle =
  | "all"
  | "unread"
  | "snoozed"
  | "resolved"
  | "dismissed";

export const SEVERITY_FILTER_ALL = "all";
export type SeverityFilter = AttentionSeverity | typeof SEVERITY_FILTER_ALL;

export type AttentionProjectScope =
  | { kind: "all" }
  | { kind: "global" }
  | { kind: "project"; project: string }
  | { kind: "set"; projects: string[] };

export interface AttentionFilterState {
  lifecycle: AttentionLifecycle;
  severity: SeverityFilter;
  projectScope: AttentionProjectScope;
  query: string;
}

export type AttentionGroups = Record<
  Exclude<AttentionLifecycle, "all">,
  Attention[]
>;

export function groupAttention(rows: Attention[]): AttentionGroups {
  const groups: AttentionGroups = {
    unread: [] as Attention[],
    snoozed: [] as Attention[],
    resolved: [] as Attention[],
    dismissed: [] as Attention[],
  };
  for (const item of rows) {
    switch (item.state) {
      case "unread":
        groups.unread.push(item);
        break;
      case "snoozed":
        groups.snoozed.push(item);
        break;
      case "resolved":
        groups.resolved.push(item);
        break;
      case "dismissed":
        groups.dismissed.push(item);
        break;
      // "read" is a terminal-ish state with no dedicated tab; it only
      // shows under "all".
    }
  }
  return groups;
}

export function attentionCounts(rows: Attention[]) {
  const groups = groupAttention(rows);
  return {
    all: rows.length,
    unread: groups.unread.length,
    snoozed: groups.snoozed.length,
    resolved: groups.resolved.length,
    dismissed: groups.dismissed.length,
  };
}

export function resolveAttentionScope(
  projectFilter: string,
  sidebar: { projects: string[]; unfiltered: boolean; loading: boolean },
): AttentionProjectScope {
  if (projectFilter === PROJECT_FILTER_ALL) return { kind: "all" };
  if (projectFilter === PROJECT_FILTER_GLOBAL) return { kind: "global" };
  if (projectFilter !== PROJECT_FILTER_SIDEBAR) {
    return { kind: "project", project: projectFilter };
  }
  if (sidebar.loading || sidebar.unfiltered) return { kind: "all" };
  return { kind: "set", projects: [...sidebar.projects, "global"] };
}

export function filterAttention(
  rows: Attention[],
  filter: AttentionFilterState,
): Attention[] {
  const query = filter.query.trim().toLocaleLowerCase();
  const projectMatches = (item: Attention): boolean => {
    const project = item.project || "global";
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
  const lifecycleMatches = (item: Attention): boolean => {
    switch (filter.lifecycle) {
      case "all":
        return true;
      case "unread":
        return item.state === "unread";
      case "snoozed":
        return item.state === "snoozed";
      case "resolved":
        return item.state === "resolved";
      case "dismissed":
        return item.state === "dismissed";
    }
  };
  const severityMatches = (item: Attention): boolean =>
    filter.severity === SEVERITY_FILTER_ALL ||
    item.severity === filter.severity;
  return rows.filter(
    (item) =>
      projectMatches(item) &&
      lifecycleMatches(item) &&
      severityMatches(item) &&
      (!query ||
        item.title.toLocaleLowerCase().includes(query) ||
        (item.body || "").toLocaleLowerCase().includes(query) ||
        (item.project || "global").toLocaleLowerCase().includes(query)),
  );
}

/**
 * Sort for display: critical first, then warning, then info; within a
 * severity, newest first. Pure and stable for a given input.
 */
const SEVERITY_RANK: Record<AttentionSeverity, number> = {
  critical: 0,
  warning: 1,
  info: 2,
};

export function sortAttention(rows: Attention[]): Attention[] {
  return [...rows].sort((a, b) => {
    const bySeverity =
      SEVERITY_RANK[a.severity] - SEVERITY_RANK[b.severity];
    if (bySeverity !== 0) return bySeverity;
    // Newest first. created_at is RFC3339; string compare is chronological.
    if (a.created_at > b.created_at) return -1;
    if (a.created_at < b.created_at) return 1;
    return 0;
  });
}
