/**
 * lib/actions/taskActions — the verb matrix for a single task.
 *
 * Pure: takes a Task plus a set of effect callbacks, returns descriptors.
 * All the "can I do this right now, and if not why not" logic lives here
 * so it can be tested exhaustively without rendering anything.
 *
 * Resume deliberately reuses `computeTaskResumeState` from
 * `components/Modal/taskActions.ts` rather than restating its rules — that
 * module already encodes the server's abandonment semantics (including the
 * stuck-pending force path), and two copies would drift.
 */
import { computeTaskResumeState } from "../../components/Modal/taskActions";
import { ALL_STATUSES, type Task, type TaskStatus } from "../types";
import type { ActionDescriptor } from "./types";

/**
 * Statuses from which a task cannot meaningfully be run or cancelled.
 * Mirrors the server's terminal set in ResumeTask.
 */
export const TERMINAL_STATUSES: ReadonlySet<string> = new Set([
  "completed",
  "validated",
  "cancelled",
  "superseded",
  "archived",
]);

/** Human-readable labels for the status picker. */
export const STATUS_LABELS: Record<TaskStatus, string> = {
  draft: "Draft",
  pending: "Pending",
  active: "Active",
  in_progress: "In progress",
  blocked: "Blocked",
  cancelled: "Cancelled",
  completed: "Completed",
  validated: "Validated",
  superseded: "Superseded",
  archived: "Archived",
};

/**
 * Effects a task action can perform. The component supplies real
 * implementations; tests supply recorders.
 */
export interface TaskActionContext {
  runTask: (task: Task) => Promise<void>;
  setStatus: (task: Task, status: TaskStatus) => Promise<void>;
  deleteTask: (task: Task) => Promise<void>;
  openResume: (task: Task) => void;
  openDetails: (task: Task) => void;
  openLogs: (task: Task) => void;
  openMetadata: (task: Task) => void;
  /** Open the status picker; separate from applying a status. */
  openStatusPicker: (task: Task) => void;
}

/**
 * Why a task cannot be deleted right now, or "" when it can.
 *
 * The server enforces this too (409 on a live claim) — the client check
 * exists to disable the affordance up front rather than let the user
 * commit to a confirm dialog that will fail.
 */
export function deleteBlockedReason(task: Task): string {
  if (task.status === "in_progress" && !task.is_abandoned) {
    return "Task is running — abort the runner first, or use Force delete";
  }
  return "";
}

/** Why a task cannot be run right now, or "" when it can. */
export function runBlockedReason(task: Task): string {
  if (TERMINAL_STATUSES.has(task.status)) {
    return `Task is ${task.status} — change its status first to re-run it`;
  }
  if (task.status === "in_progress" && !task.is_abandoned) {
    return "Task is already running";
  }
  if ((task.blocked_by?.length ?? 0) > 0) {
    return `Blocked by ${task.blocked_by?.length} unmet dependency${
      task.blocked_by?.length === 1 ? "" : "ies"
    }`;
  }
  return "";
}

/** Why a task's status cannot be changed to `next`, or "" when it can. */
export function statusChangeBlockedReason(
  task: Task,
  next: TaskStatus,
): string {
  if (task.status === next) return `Already ${STATUS_LABELS[next] ?? next}`;
  if (task.status === "in_progress" && !task.is_abandoned) {
    return "Task is running — changing status now will not stop the runner";
  }
  return "";
}

/**
 * Build the full action list for a task.
 *
 * Every action is always present; unavailable ones carry a
 * `disabledReason`. See the module docstring in ./types for why.
 */
export function buildTaskActions(
  task: Task,
  ctx: TaskActionContext,
): ActionDescriptor[] {
  const actions: ActionDescriptor[] = [];
  const resume = computeTaskResumeState(task);

  // ─── run ────────────────────────────────────────────────────────
  const runBlocked = runBlockedReason(task);
  actions.push({
    id: "run",
    label: "Run now",
    group: "run",
    key: "x",
    disabledReason: runBlocked,
    run: () => ctx.runTask(task),
  });

  if (resume.showResume) {
    actions.push({
      id: "resume",
      label: resume.alreadyResumed
        ? "Resume (already requested)"
        : "Resume task",
      group: "run",
      key: "r",
      // Never disabled: the Resume dialog is where force lives, and the
      // dialog itself explains the force path. Disabling it here would
      // hide the only route to recovery for a force-only case.
      run: async () => ctx.openResume(task),
    });
  }

  // ─── state ──────────────────────────────────────────────────────
  actions.push({
    id: "status",
    label: "Change status…",
    group: "state",
    key: "s",
    run: async () => ctx.openStatusPicker(task),
  });

  const cancelBlocked = TERMINAL_STATUSES.has(task.status)
    ? `Task is already ${task.status}`
    : "";
  actions.push({
    id: "cancel",
    label: "Cancel task",
    group: "state",
    disabledReason: cancelBlocked,
    // Cancelling a running task is worth a beat of thought — the runner
    // keeps going, which surprises people. Reversible, so no type-to-confirm.
    confirm:
      task.status === "in_progress"
        ? {
            title: "Cancel a running task?",
            body:
              "The task will be marked cancelled, but the runner executing it will keep going " +
              "until it finishes or is aborted separately.",
            confirmLabel: "Cancel task",
          }
        : undefined,
    run: () => ctx.setStatus(task, "cancelled"),
  });

  // ─── edit ───────────────────────────────────────────────────────
  actions.push({
    id: "metadata",
    label: "Edit metadata…",
    group: "edit",
    key: "e",
    run: async () => ctx.openMetadata(task),
  });

  // ─── navigate ───────────────────────────────────────────────────
  actions.push({
    id: "details",
    label: "Open in focus pane",
    group: "navigate",
    run: async () => ctx.openDetails(task),
  });
  actions.push({
    id: "logs",
    label: "Open logs in focus pane",
    group: "navigate",
    key: "l",
    run: async () => ctx.openLogs(task),
  });

  // ─── danger ─────────────────────────────────────────────────────
  actions.push({
    id: "delete",
    label: "Delete task",
    group: "danger",
    key: "d",
    danger: true,
    disabledReason: deleteBlockedReason(task),
    confirm: {
      title: "Delete this task?",
      body: `"${task.title || task.id}" and its history will be removed permanently. This cannot be undone.`,
      confirmLabel: "Delete",
    },
    run: () => ctx.deleteTask(task),
  });

  return actions;
}

/**
 * Build the per-status entries for the status picker. The task's current
 * status is present but disabled, so the picker doubles as a display of
 * where the task actually is.
 */
export function buildStatusActions(
  task: Task,
  ctx: Pick<TaskActionContext, "setStatus">,
): ActionDescriptor[] {
  return ALL_STATUSES.map((status) => ({
    id: `status:${status}`,
    label: STATUS_LABELS[status] ?? status,
    group: "state" as const,
    disabledReason: statusChangeBlockedReason(task, status),
    run: () => ctx.setStatus(task, status),
  }));
}
