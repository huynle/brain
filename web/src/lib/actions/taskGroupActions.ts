/**
 * lib/actions/taskGroupActions — the verb matrix for a task group that is
 * NOT a feature.
 *
 * Two groups on the project card hold real tasks and answer to no
 * `feature_id`: the ungrouped "No feature" bucket, and each bucket inside
 * the Archived fold. Until now they were the only rows on the card with no
 * verbs at all — you could look at them and nothing else.
 *
 * ─── Why not just build a fake DerivedFeature ────────────────────
 *
 * Because half the feature verbs would silently dead-end. Six of them
 * (run, run-with-dependents, cancel-chain, resume, assign, unassign) put
 * the feature id in the URL path, and five more terminate in modals that
 * re-derive their target with `deriveFeatures(...).find(f => f.id === id)`
 * — and `deriveFeatures` skips exactly the tasks these groups contain
 * (`if (!fid) continue` and `if (status === "archived") continue`). A
 * synthetic id could never be found again, so those verbs would open
 * "Feature not found". A smaller, honest matrix beats a larger one that
 * lies.
 *
 * ─── Why an explicit path list, never a filter ───────────────────
 *
 * The obvious implementation — `bulkUpdate({project, feature_id: ""})` —
 * is a loaded gun. The storage layer only appends its WHERE clause for a
 * NON-EMPTY value, so an empty feature id reaches the database as no
 * constraint at all while every validation gate upstream still reports
 * the filter as "constrained". It would mutate the first 100 tasks of the
 * whole project. `lib/api`'s `featureFilterGuard` now throws on that, and
 * every verb here goes through `bulkUpdateEntries` / `bulkDeletePaths`
 * with the group's own paths instead.
 *
 * That also makes these verbs SIMPLER than their feature equivalents, not
 * harder: a filter-mode bulk call pages at 100 and can re-serve the same
 * page (the server lists by modified DESC, so freshly-updated rows sort
 * back to the front), which is why the feature path needs the
 * per-source-status baton. Disjoint chunks of an explicit list cannot
 * repeat, so a plain loop drains the group exactly once.
 *
 * Pure by the same rule as every other builder here: no react, no fetch.
 */
import type { Task, TaskStatus } from "../types";
import { STATUS_LABELS } from "./taskActions";
import type { ActionDescriptor } from "./types";

/**
 * A set of tasks the card renders under one header, addressed by their
 * paths rather than by any server-side identity.
 */
export interface TaskGroup {
  projectId: string;
  /** Stable fold/verb key. NOT a feature id — see the module docstring. */
  key: string;
  /** Human label, used in confirms and toasts. */
  label: string;
  /** The group's tasks, in render order. */
  tasks: readonly Task[];
}

export interface TaskGroupCounts {
  total: number;
  /** Tasks a run could actually dispatch. */
  runnable: number;
  archived: number;
  /** completed + validated. */
  done: number;
  /** Tasks not already in a terminal state — what cancel would touch. */
  live: number;
}

const DONE_STATUSES = new Set<TaskStatus>(["completed", "validated"]);
const TERMINAL_STATUSES = new Set<TaskStatus>([
  "completed",
  "validated",
  "cancelled",
  "archived",
]);

export function countTaskGroup(tasks: readonly Task[]): TaskGroupCounts {
  const c: TaskGroupCounts = {
    total: 0,
    runnable: 0,
    archived: 0,
    done: 0,
    live: 0,
  };
  for (const t of tasks) {
    c.total++;
    if (t.status === "archived") c.archived++;
    if (DONE_STATUSES.has(t.status)) c.done++;
    if (!TERMINAL_STATUSES.has(t.status)) c.live++;
    // Only a pending task can be dispatched. A `blocked` one is waiting on
    // a dependency the run would not clear, and in_progress is already
    // going — counting either would make "Run" claim work it cannot do.
    if (t.status === "pending") c.runnable++;
  }
  return c;
}

export interface TaskGroupActionContext {
  /** Fold / unfold the group's rows. */
  toggleCollapsed: (group: TaskGroup) => void;
  /** Add every task in the group to the multi-select scope, handing the
   *  user the SelectionBar's own verbs for anything not offered here. */
  selectAll: (group: TaskGroup) => void;
  /** Dispatch every runnable task in the group, one by one. */
  runGroup: (group: TaskGroup) => Promise<void>;
  /** Apply one status to every task in the group. */
  setStatusForAll: (group: TaskGroup, status: TaskStatus) => Promise<void>;
  /** Delete every task in the group. */
  deleteGroup: (group: TaskGroup) => Promise<void>;
}

/** Why the group cannot be run right now, or "" when it can. */
export function runGroupBlockedReason(c: TaskGroupCounts): string {
  if (c.total === 0) return "Group has no tasks";
  if (c.runnable === 0) return "No pending tasks — nothing to dispatch";
  return "";
}

export function buildTaskGroupActions(
  group: TaskGroup,
  ctx: TaskGroupActionContext,
  opts: { collapsed: boolean } = { collapsed: false },
): ActionDescriptor[] {
  const c = countTaskGroup(group.tasks);
  const n = c.total;
  const tasksWord = `${n} task${n === 1 ? "" : "s"}`;

  const statusVerb = (
    id: string,
    label: string,
    status: TaskStatus,
    disabledReason: string,
    danger = false,
  ): ActionDescriptor => ({
    id,
    label,
    group: "state",
    danger,
    disabledReason,
    confirm: {
      title: `${label.replace(/…$/, "")} in ${group.label}?`,
      body:
        `Sets ${tasksWord} in "${group.label}" to ` +
        `${STATUS_LABELS[status] ?? status}. Tasks are updated one by one, ` +
        `so a partial failure leaves the rest applied.`,
      confirmLabel: label.replace(/…$/, ""),
    },
    run: () => ctx.setStatusForAll(group, status),
  });

  return [
    {
      id: "collapse",
      label: opts.collapsed ? "Expand" : "Collapse",
      group: "select",
      run: async () => ctx.toggleCollapsed(group),
    },
    {
      id: "select-all",
      label: `Select all (${n})`,
      group: "select",
      key: "v",
      disabledReason: n === 0 ? "Group has no tasks" : "",
      run: async () => ctx.selectAll(group),
    },

    {
      id: "run",
      label:
        c.runnable > 0 ? `Run ${c.runnable} pending` : "Run pending tasks",
      group: "run",
      key: "x",
      disabledReason: runGroupBlockedReason(c),
      run: () => ctx.runGroup(group),
    },

    // ─── state ──────────────────────────────────────────────────────
    // Archive before cancel, matching the feature matrix: archive is the
    // reversible tidy-up, cancel stops work.
    statusVerb(
      "archive",
      "Archive all…",
      "archived",
      c.total === 0
        ? "Group has no tasks"
        : c.archived === c.total
          ? "Every task is already archived"
          : "",
    ),
    statusVerb(
      "cancel",
      "Cancel all…",
      "cancelled",
      c.total === 0
        ? "Group has no tasks"
        : c.live === 0
          ? "No task here is still live"
          : "",
    ),
    // The way back out of the archive. Without it, archiving a group from
    // this menu is a one-way door — the rows move into the fold and the
    // only route back is opening each task.
    statusVerb(
      "restore",
      "Restore to pending…",
      "pending",
      c.total === 0
        ? "Group has no tasks"
        : c.archived === 0
          ? "Nothing here is archived"
          : "",
    ),

    // ─── danger ─────────────────────────────────────────────────────
    {
      id: "delete",
      label: "Delete all…",
      group: "danger",
      danger: true,
      disabledReason: n === 0 ? "Group has no tasks" : "",
      confirm: {
        title: `Delete ${tasksWord} in ${group.label}?`,
        body:
          `This permanently removes ${tasksWord} and their history. It ` +
          `cannot be undone. To take them out of the way reversibly, ` +
          `archive instead.`,
        typeToConfirm: group.label,
        confirmLabel: "Delete permanently",
      },
      run: () => ctx.deleteGroup(group),
    },
  ];
}
