/**
 * Pure helpers backing the Task Actions modal + the Resume button in TaskModal.
 * Mirrors featureActions.ts. Kept UI-agnostic so both TaskModal and any
 * CardTasks context-menu integration can share the same disable-reason logic.
 */
import type { Task } from "../../lib/types";

export interface TaskResumeState {
  /** True when a Resume affordance should appear at all. False means the task
   *  is not in a state where resume makes sense (e.g. already running, or
   *  never-started). */
  showResume: boolean;
  /** True when Resume can be invoked without force. False means the user needs
   *  to explicitly opt into force=true (or the button is disabled). */
  canResumeCleanly: boolean;
  /** True when the "already resumed, waiting on runner" state is showing —
   *  the button should indicate that resume has already been requested. */
  alreadyResumed: boolean;
  /** Short user-facing string describing why Resume is offered. Empty when
   *  showResume=false. */
  reasonHint: string;
  /** Short user-facing string describing why force is needed (if it is). */
  forceHint: string;
}

/** computeTaskResumeState reads a Task and returns a boolean matrix for the
 *  Resume UI. Never mutates the task. */
export function computeTaskResumeState(task: Task | null | undefined): TaskResumeState {
  const empty: TaskResumeState = {
    showResume: false,
    canResumeCleanly: false,
    alreadyResumed: false,
    reasonHint: "",
    forceHint: "",
  };
  if (!task) return empty;

  // Already-resumed: the runner will pick it up on its next poll. Show the
  // affordance so the user sees the state, but style/label it as a no-op.
  if (task.status === "pending" && task.resume_requested) {
    return {
      showResume: true,
      canResumeCleanly: true,
      alreadyResumed: true,
      reasonHint: "Resume already requested — runner will pick up on next poll",
      forceHint: "",
    };
  }

  // Standard abandonment path.
  if (task.is_abandoned) {
    return {
      showResume: true,
      canResumeCleanly: true,
      alreadyResumed: false,
      reasonHint: abandonReasonCopy(task.abandon_reason),
      forceHint: "",
    };
  }

  // Stuck-pending recovery: the runner can auto-reset a task to pending on
  // claim-renewal failure (runner.go renewClaims 404 → pending). Such tasks
  // don't carry is_abandoned=true (no claim to enrich from) and don't carry
  // resume_requested (never went through /resume). The server explicitly
  // supports resuming these with force=true (see
  // TestResumeTask_StuckPendingUnstuck). Surface a Force affordance so the
  // user can flip it back with the resume prompt.
  if (task.status === "pending" && !task.resume_requested) {
    return {
      showResume: true,
      canResumeCleanly: false,
      alreadyResumed: false,
      reasonHint: "Task is pending but was not started via Resume",
      forceHint: "Force stamps the resume flag so the next runner spawn uses IsResume=true",
    };
  }

  // Non-abandoned tasks in a state that permits force. Terminal statuses are
  // out — trigger is the right button for those. In-progress with a live claim
  // is out — force wouldn't help (server would refuse the live-claim safety).
  const terminalStatuses = new Set([
    "completed", "validated", "cancelled", "superseded", "archived",
  ]);
  if (terminalStatuses.has(task.status)) {
    return empty;
  }

  // Blocked (agent-self-blocked or user-blocked, no reaper marker) — offer
  // force. The Blocked Task Inspector automation is the "right" path for
  // these, but the button is available for manual override.
  if (task.status === "blocked" || task.status === "in_progress") {
    return {
      showResume: true,
      canResumeCleanly: false,
      alreadyResumed: false,
      reasonHint: `Task status is ${task.status}`,
      forceHint: "Not automatically detected as abandoned — force will override the safety gate",
    };
  }

  return empty;
}

/** Convert an AbandonReason enum to a short user-facing hint. */
export function abandonReasonCopy(reason: string | null | undefined): string {
  switch (reason) {
    case "no_claim":
      return "Task claim disappeared — runner likely crashed";
    case "claim_expired":
      return "Task lease expired without renewal";
    case "runner_offline":
      return "Runner holding the claim went offline";
    case "orphan_reaped":
      return "Marked blocked by the runner orphan reaper";
    default:
      return "Task appears abandoned";
  }
}
