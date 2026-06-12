import { useMemo } from "react";
import { useLive } from "../lib/sse";
import { ALL_PROJECTS } from "../store/ui";
import type { Task, TaskStats } from "../lib/types";

const EMPTY_STATS: TaskStats = {
  total: 0,
  ready: 0,
  waiting: 0,
  blocked: 0,
  not_pending: 0,
};

export interface AggregatedTasks {
  tasks: Task[];
  stats: TaskStats;
  connected: boolean;
  /** any stream reported an error and none is connected */
  anyError: string | null;
}

/** Combine live task state for the active project (or all projects). */
export function useLiveTasks(activeProject: string): AggregatedTasks {
  const projects = useLive((s) => s.projects);

  return useMemo(() => {
    const ids =
      activeProject === ALL_PROJECTS
        ? Object.keys(projects)
        : [activeProject];

    const tasks: Task[] = [];
    const stats: TaskStats = { ...EMPTY_STATS };
    let connected = false;
    let anyError: string | null = null;

    for (const id of ids) {
      const p = projects[id];
      if (!p) continue;
      for (const t of p.tasks) {
        tasks.push(t.projectId ? t : { ...t, projectId: id });
      }
      if (p.stats) {
        stats.total += p.stats.total;
        stats.ready += p.stats.ready;
        stats.waiting += p.stats.waiting;
        stats.blocked += p.stats.blocked;
        stats.not_pending += p.stats.not_pending;
      }
      if (p.connected) connected = true;
      if (p.error) anyError = p.error;
    }

    return { tasks, stats, connected, anyError: connected ? null : anyError };
  }, [projects, activeProject]);
}

/** Derive active/completed counts not directly in TaskStats. */
export function deriveCounts(tasks: Task[]) {
  let active = 0;
  let completed = 0;
  for (const t of tasks) {
    if (t.status === "in_progress" || t.status === "active") active++;
    else if (t.status === "completed" || t.status === "validated") completed++;
  }
  return { active, completed };
}
