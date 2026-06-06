import type { Task } from "../../lib/types";

const STATUS_ORDER: Record<string, number> = {
  in_progress: 0,
  active: 1,
  blocked: 2,
  pending: 3,
  draft: 4,
  validated: 5,
  completed: 6,
  cancelled: 7,
  superseded: 8,
  archived: 9,
};
const PRIORITY_ORDER: Record<string, number> = {
  high: 0,
  medium: 1,
  low: 2,
};

export const UNGROUPED = "__ungrouped__";

export interface TaskGroup {
  feature: string;
  label: string;
  tasks: Task[];
}

function cmp(a: Task, b: Task): number {
  const sa = STATUS_ORDER[a.status] ?? 50;
  const sb = STATUS_ORDER[b.status] ?? 50;
  if (sa !== sb) return sa - sb;
  const pa = PRIORITY_ORDER[a.priority] ?? 3;
  const pb = PRIORITY_ORDER[b.priority] ?? 3;
  if (pa !== pb) return pa - pb;
  return (a.title || "").localeCompare(b.title || "");
}

export function filterTasks(tasks: Task[], q: string): Task[] {
  const query = q.trim().toLowerCase();
  if (!query) return tasks;
  return tasks.filter(
    (t) =>
      t.title?.toLowerCase().includes(query) ||
      t.id?.toLowerCase().includes(query) ||
      t.feature_id?.toLowerCase().includes(query) ||
      t.status?.toLowerCase().includes(query) ||
      t.tags?.some((tag) => tag.toLowerCase().includes(query)),
  );
}

export function groupByFeature(tasks: Task[]): TaskGroup[] {
  const map = new Map<string, Task[]>();
  for (const t of tasks) {
    const key = t.feature_id || UNGROUPED;
    const arr = map.get(key);
    if (arr) arr.push(t);
    else map.set(key, [t]);
  }
  const groups: TaskGroup[] = [];
  for (const [feature, arr] of map) {
    arr.sort(cmp);
    groups.push({
      feature,
      label: feature === UNGROUPED ? "Ungrouped" : feature,
      tasks: arr,
    });
  }
  // Ungrouped last; otherwise alphabetical by feature.
  groups.sort((a, b) => {
    if (a.feature === UNGROUPED) return 1;
    if (b.feature === UNGROUPED) return -1;
    return a.label.localeCompare(b.label);
  });
  return groups;
}
