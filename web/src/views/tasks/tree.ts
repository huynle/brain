// Dependency-tree builder mirroring the TUI (internal/tui/tasktree.go BuildTree).
//
// Tasks nest under the task(s) they depend on (depends_on / resolved_deps) and
// under their parent_id. parent_id takes precedence; depends_on edges only apply
// to tasks without a parent_id ancestor in the set. Cycles are detected (marked
// ↺), cycle edges are skipped, each task is rendered once (diamond dedup).

import type { Task } from "../../lib/types";

export interface TreeRow {
  task: Task;
  /** ancestor continuation prefix (│ / spaces) + this node's branch (├─/└─/""). */
  lead: string;
  inCycle: boolean;
}

const PRIO: Record<string, number> = { high: 0, medium: 1, low: 2 };
const STATUS: Record<string, number> = {
  in_progress: 0,
  active: 0,
  pending: 1,
  blocked: 2,
  draft: 3,
  validated: 4,
  completed: 5,
  cancelled: 6,
  superseded: 7,
  archived: 8,
};

function depsOf(t: Task): string[] {
  return (t.resolved_deps?.length ? t.resolved_deps : t.depends_on) ?? [];
}

/** Build an ordered, flattened dependency tree for a set of tasks. */
export function buildTaskTree(tasks: Task[]): TreeRow[] {
  const byId = new Map(tasks.map((t) => [t.id, t]));

  const merged = new Map<string, string[]>();
  const hasParent = new Set<string>(); // has an in-set parent (dep or parent_id)
  const hasParentId = new Set<string>();
  const addChild = (p: string, c: string) => {
    const arr = merged.get(p);
    if (arr) arr.push(c);
    else merged.set(p, [c]);
  };

  // parent_id edges (only when the parent is in the visible set)
  for (const t of tasks) {
    if (t.parent_id && byId.has(t.parent_id) && t.parent_id !== t.id) {
      addChild(t.parent_id, t.id);
      hasParent.add(t.id);
      hasParentId.add(t.id);
    }
  }
  // depends_on edges (skip tasks that already nest under a parent_id)
  for (const t of tasks) {
    if (hasParentId.has(t.id)) continue;
    for (const d of depsOf(t)) {
      if (byId.has(d) && d !== t.id) {
        addChild(d, t.id);
        hasParent.add(t.id);
      }
    }
  }

  // cycle detection (DFS with recursion stack)
  const inCycle = new Set<string>();
  const visited = new Set<string>();
  const stack = new Set<string>();
  const dfs = (id: string): boolean => {
    visited.add(id);
    stack.add(id);
    let cyc = false;
    for (const c of merged.get(id) ?? []) {
      if (!visited.has(c)) {
        if (dfs(c)) {
          inCycle.add(id);
          cyc = true;
        }
      } else if (stack.has(c)) {
        inCycle.add(id);
        inCycle.add(c);
        cyc = true;
      }
    }
    stack.delete(id);
    return cyc;
  };
  for (const t of tasks) if (!visited.has(t.id)) dfs(t.id);

  const sortIds = (ids: string[]): string[] =>
    [...new Set(ids)].sort((a, b) => {
      const ta = byId.get(a)!;
      const tb = byId.get(b)!;
      // topological among siblings: a dependency sorts before its dependent
      if (depsOf(tb).includes(a) && !depsOf(ta).includes(b)) return -1;
      if (depsOf(ta).includes(b) && !depsOf(tb).includes(a)) return 1;
      const pr = (PRIO[ta.priority] ?? 1) - (PRIO[tb.priority] ?? 1);
      if (pr) return pr;
      const sr = (STATUS[ta.status] ?? 9) - (STATUS[tb.status] ?? 9);
      if (sr) return sr;
      return (ta.title || "").localeCompare(tb.title || "");
    });

  const rendered = new Set<string>();
  const rows: TreeRow[] = [];

  const emit = (id: string, prefix: string, connector: string, isLast: boolean) => {
    if (rendered.has(id)) return;
    rendered.add(id);
    const t = byId.get(id);
    if (!t) return;
    rows.push({ task: t, lead: prefix + connector, inCycle: inCycle.has(id) });

    const childPrefix = connector === "" ? prefix : prefix + (isLast ? "   " : "│  ");
    const kids = sortIds(merged.get(id) ?? []).filter(
      (c) => !(inCycle.has(id) && inCycle.has(c)) && !rendered.has(c),
    );
    kids.forEach((c, i) => {
      const last = i === kids.length - 1;
      emit(c, childPrefix, last ? "└─ " : "├─ ", last);
    });
  };

  const roots = sortIds(tasks.filter((t) => !hasParent.has(t.id)).map((t) => t.id));
  for (const id of roots) emit(id, "", "", false);
  // orphans (e.g. tasks only reachable via skipped cycle edges)
  for (const t of tasks) if (!rendered.has(t.id)) emit(t.id, "", "", false);

  return rows;
}
