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
import { historySessionRefs } from "../sessionRef";
import { ALL_STATUSES, type SessionRef, type Task, type TaskStatus } from "../types";
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
  /** Open the task modal — the row's double-click / Enter target and the
   *  context-menu "Open" verb. Distinct from openDetails, which opens the
   *  focus pane rather than the modal. */
  openModal: (task: Task) => void;
  /** Toggle this task in the multi-select scope (SelectionBar verbs). */
  toggleSelect: (task: Task) => void;
  /** Whether the task is currently in the multi-select scope. */
  isSelected: (task: Task) => boolean;
  runTask: (task: Task) => Promise<void>;
  setStatus: (task: Task, status: TaskStatus) => Promise<void>;
  deleteTask: (task: Task) => Promise<void>;
  /** Tells the runner holding this task to abort its execution. */
  abortTask: (task: Task) => Promise<void>;
  openResume: (task: Task) => void;
  openDetails: (task: Task) => void;
  openLogs: (task: Task) => void;
  openMetadata: (task: Task) => void;
  /** Open the status picker; separate from applying a status. */
  openStatusPicker: (task: Task) => void;
  /** Opens the goal-create modal prefilled with this task's scope. */
  openGoalCreate: (task: Task) => void;
  /** Live session ref for the task's running instance, if one is up.
   *  A query, not an effect — instance state lives outside Task. */
  liveSessionRef: (task: Task) => SessionRef | undefined;
  /** Open the session view on a ref — live or recorded. One effect for
   *  both: every session surface now streams and steers, so "watch",
   *  "read the transcript" and "steer" are the same navigation. */
  openSession: (task: Task, ref: SessionRef) => void;
  /** Reopen a recorded session on its runner (spawn + address it live). */
  continueSession: (task: Task, ref: SessionRef) => Promise<void>;
  /** Open a session (live or recorded) inline in the right-side drawer —
   *  the "open in sidebar" verb. Distinct from openSession, which
   *  navigates to the full-page session view; same ref either way. */
  openSessionInDrawer: (task: Task, ref: SessionRef) => void;
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
    return 'Task is running — use "Abort runner execution" first';
  }
  return "";
}

/**
 * The runner the client believes is executing this task. The dispatch
 * lease (populated by the server's enrichDispatchDiagnostics on task-list
 * responses) is authoritative; when a snapshot predates the enrichment we
 * fall back to the most recent session's recorded runner.
 */
export function knownRunnerId(task: Task): string | undefined {
  const leased = task.dispatch_lease?.assigned_runner_id;
  if (leased) return leased;
  let best: { ts: string; id: string } | undefined;
  for (const s of Object.values(task.sessions ?? {})) {
    if (!s.runner_id) continue;
    // ISO timestamps compare correctly as strings.
    if (!best || s.timestamp > best.ts) best = { ts: s.timestamp, id: s.runner_id };
  }
  return best?.id;
}

/** Why a task's execution cannot be aborted right now, or "" when it can. */
export function abortBlockedReason(task: Task): string {
  if (task.status !== "in_progress") {
    return `Task is ${task.status} — only a running task can be aborted`;
  }
  if (task.is_abandoned) {
    return "Task is abandoned — nothing is executing it. Use Resume instead";
  }
  if (!knownRunnerId(task)) {
    return "No runner is known to hold this task — check the dispatch lease";
  }
  return "";
}

/**
 * Why a task's live session cannot be watched right now, or "" when it
 * can. `live` is the resolved live ref (ctx.liveSessionRef) — instance
 * state lives outside Task, so the caller supplies it.
 */
export function watchBlockedReason(
  task: Task,
  live: SessionRef | undefined,
): string {
  if (task.executor && task.executor !== "opencode") {
    return `Executor is ${task.executor} — session control requires an OpenCode task`;
  }
  if (task.status !== "in_progress") {
    return `Task is ${task.status} — only a running task has a live session`;
  }
  if (!live) {
    return "Runner is offline or the session is gone — view the transcript instead";
  }
  return "";
}

/**
 * Why a task's recorded transcript cannot be viewed, or "" when it can.
 * Pure over Task: recorded sessions live in `task.sessions` metadata.
 */
export function transcriptBlockedReason(task: Task): string {
  if (historySessionRefs(task).length > 0) return "";
  if (task.executor && task.executor !== "opencode") {
    return `Executor is ${task.executor} — sessions are recorded for OpenCode tasks only`;
  }
  return "No session recorded — discovery may have failed; check runner logs";
}

/**
 * Why a task's session cannot be opened at all, or "" when it can.
 *
 * The menu offers ONE session verb, so this gate is the union of the
 * two it replaced: a live session opens, a recorded one opens, and only
 * a task with neither is blocked. The reason it reports is the one the
 * user can act on — a running task that has no live instance is a
 * runner problem, anything else is a "nothing was ever recorded"
 * problem.
 */
export function sessionBlockedReason(
  task: Task,
  live: SessionRef | undefined,
): string {
  if (live) return "";
  if (historySessionRefs(task).length > 0) return "";
  if (task.status === "in_progress" && (!task.executor || task.executor === "opencode")) {
    return watchBlockedReason(task, live);
  }
  return transcriptBlockedReason(task);
}

/**
 * Why a completed task's session cannot be continued, or "" when it
 * can. Continuation reopens the recorded session on its runner via a
 * fresh ad-hoc instance — it needs a recorded session with a workdir
 * (or the task's own configured workdir as fallback) and only makes
 * sense once the task has settled; a running task is steered instead.
 */
export function continueBlockedReason(task: Task): string {
  if (!TERMINAL_STATUSES.has(task.status)) {
    return task.status === "in_progress"
      ? "Task is running — steer the live session instead"
      : `Task is ${task.status} — continue is for settled tasks`;
  }
  const refs = historySessionRefs(task);
  if (refs.length === 0) {
    return transcriptBlockedReason(task);
  }
  const newest = refs[0];
  const workdir =
    (newest.mode === "history" ? newest.workdir : undefined) ?? task.workdir;
  if (!workdir) {
    return "No workdir recorded for this session — nowhere to reopen it";
  }
  return "";
}

/** Why a task cannot be archived right now, or "" when it can. */
export function archiveBlockedReason(task: Task): string {
  if (task.status === "archived") return "Task is already archived";
  if (!TERMINAL_STATUSES.has(task.status)) {
    return `Task is ${task.status} — archive is for settled work`;
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

  // ─── select ─────────────────────────────────────────────────────
  // "Open" leads the menu: it is the row's primary gesture (double-click
  // / Enter open the modal), so the context menu surfaces the same verb
  // first. Kept in the "select" group so it renders ahead of everything
  // else, just before the multi-select toggle.
  actions.push({
    id: "open",
    label: "Open",
    group: "select",
    run: async () => ctx.openModal(task),
  });
  actions.push({
    id: "select",
    label: ctx.isSelected(task) ? "Deselect" : "Select",
    group: "select",
    key: "v",
    run: async () => ctx.toggleSelect(task),
  });

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

  const continueRefs = historySessionRefs(task);
  actions.push({
    id: "continue",
    label: "Continue session…",
    group: "run",
    disabledReason: continueBlockedReason(task),
    confirm:
      continueRefs.length > 0
        ? {
            title: "Continue this session?",
            body:
              `Reopens the recorded session on runner ${continueRefs[0].runner_id}` +
              `${
                continueRefs[0].mode === "history" && continueRefs[0].workdir
                  ? ` in ${continueRefs[0].workdir}`
                  : ""
              }. A fresh OpenCode instance is spawned there; it stays in Live sessions until you close it.`,
            confirmLabel: "Continue session",
          }
        : undefined,
    run: () => ctx.continueSession(task, continueRefs[0]),
  });

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

  // Archive/Unarchive are a status-aware pair rendered as one slot: an
  // archived task shows Unarchive INSTEAD of a disabled Archive. Like the
  // conditional Resume above, this is a sanctioned exception to
  // disabled-never-hidden — the pair reads as a single toggle, and a
  // permanently-disabled twin would only add noise.
  if (task.status === "archived") {
    actions.push({
      id: "unarchive",
      label: "Unarchive",
      group: "state",
      run: () => ctx.setStatus(task, "completed"),
    });
  } else {
    actions.push({
      id: "archive",
      label: "Archive task",
      group: "state",
      disabledReason: archiveBlockedReason(task),
      // Reversible (Unarchive brings it back), so confirm without
      // type-to-confirm — same weight as archiving a goal.
      confirm: {
        title: "Archive this task?",
        body:
          `"${task.title || task.id}" leaves the default lists and stops ` +
          "counting toward feature progress. This is reversible — " +
          "restore it later from the Archived filter.",
        confirmLabel: "Archive task",
      },
      run: () => ctx.setStatus(task, "archived"),
    });
  }

  // Abort is the missing half of cancel: cancel flips the STATUS while the
  // runner keeps going; abort stops the RUNNER while the status stays. The
  // confirm copy says exactly that so nobody expects one to do both.
  actions.push({
    id: "abort",
    label: "Abort runner execution",
    group: "state",
    key: "a",
    danger: true,
    disabledReason: abortBlockedReason(task),
    confirm: {
      title: "Abort the runner's execution?",
      body:
        `The runner holding "${task.title || task.id}" will be told to abort its session. ` +
        `The task keeps its current status — cancel it, resume it, or re-run it afterwards.`,
      confirmLabel: "Abort execution",
    },
    run: () => ctx.abortTask(task),
  });

  // ─── edit ───────────────────────────────────────────────────────
  actions.push({
    id: "metadata",
    label: "Edit metadata…",
    group: "edit",
    key: "e",
    run: async () => ctx.openMetadata(task),
  });

  // Always available: a goal can watch a task in any status (a completed
  // task's goal keeps validating it against its criteria).
  actions.push({
    id: "set-goal",
    label: "Set goal…",
    group: "edit",
    run: async () => ctx.openGoalCreate(task),
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

  /*
   * ONE session verb, two placements.
   *
   * "Watch session", "View transcript" and "Steer session…" used to be
   * three entries that differed only in which ref they resolved and
   * whether they focused the composer. Every session surface now streams
   * live and carries a composer, so those distinctions no longer describe
   * anything the user would see — they only made the caller guess which
   * entry was the enabled one. The verb resolves the ref instead: the
   * live session while the task is running, the newest recording once it
   * has settled.
   *
   * The task modal's Sessions section still lists every recorded session
   * individually, which is where the multi-session (post-resume) case is
   * chosen explicitly.
   */
  const live = ctx.liveSessionRef(task);
  const recorded = historySessionRefs(task);
  const sessionRef = live ?? recorded[0];
  const sessionReason = sessionBlockedReason(task, live);

  actions.push({
    id: "session",
    label: live ? "Open live session" : "Open session transcript",
    group: "navigate",
    key: "w",
    disabledReason: sessionReason,
    run: async () => ctx.openSession(task, sessionRef!),
  });

  actions.push({
    id: "open-session-sidebar",
    label: "Open session in sidebar",
    group: "navigate",
    key: "t",
    disabledReason: sessionReason,
    run: async () => ctx.openSessionInDrawer(task, sessionRef!),
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
