/**
 * lib/actions/goalActions — the verb matrix for a goal.
 *
 * A goal is an automation entry (generated_by=brain-goal) with a
 * lifecycle status the PATCH endpoint flips:
 *
 *   active    — reconciling            (resume sets this)
 *   blocked   — paused                 (pause sets this)
 *   completed — criteria met           (server flips it; resume reactivates)
 *   archived  — hidden from lists      (archive sets this)
 *
 * Pause/resume are a status-aware pair: exactly one is enabled at a time,
 * the other carries a `disabledReason` explaining the current state —
 * disabled-never-hidden, like every other builder in this module.
 *
 * Pure: takes a GoalSummary plus effect callbacks, returns descriptors.
 * Goals do NOT ride SSE, so every mutating effect in the context is
 * responsible for invalidating the ["goals"] query afterwards (see
 * hooks/useGoalActionContext).
 */
import type { GoalReconcileAudit, GoalSummary } from "../types";
import type { ActionDescriptor } from "./types";

/**
 * Effects a goal action can perform. The component supplies real
 * implementations; tests supply recorders.
 */
export interface GoalActionContext {
  /** POST /goals/{id}/run — manual reconcile. */
  runGoal: (goal: GoalSummary) => Promise<void>;
  /** PATCH status=blocked. */
  pauseGoal: (goal: GoalSummary) => Promise<void>;
  /** PATCH status=active. */
  resumeGoal: (goal: GoalSummary) => Promise<void>;
  /** PATCH status=archived. */
  archiveGoal: (goal: GoalSummary) => Promise<void>;
  /** DELETE /goals/{id} — permanent. */
  deleteGoal: (goal: GoalSummary) => Promise<void>;
  /** Opens the goal modal in edit mode. */
  openEdit: (goal: GoalSummary) => void;
  /** Opens the goal modal (view mode). */
  openDetails: (goal: GoalSummary) => void;
}

/** Human-readable status labels. "blocked" reads as Paused in the UI —
 *  that is what the pause verb sets, and "blocked" would suggest a
 *  dependency problem rather than a deliberate stop. */
export const GOAL_STATUS_LABELS: Record<string, string> = {
  active: "Active",
  blocked: "Paused",
  completed: "Completed",
  archived: "Archived",
};

export function goalStatusLabel(status: string): string {
  return GOAL_STATUS_LABELS[status] ?? status;
}

/** Why a goal cannot be run right now, or "" when it can. */
export function runGoalBlockedReason(goal: GoalSummary): string {
  if (goal.status === "archived") {
    return "Goal is archived — resume it before running";
  }
  return "";
}

/** Why a goal cannot be paused right now, or "" when it can. */
export function pauseGoalBlockedReason(goal: GoalSummary): string {
  if (goal.status !== "active") {
    return `Goal is ${goalStatusLabel(goal.status).toLowerCase()} — only an active goal can be paused`;
  }
  return "";
}

/** Why a goal cannot be resumed right now, or "" when it can. */
export function resumeGoalBlockedReason(goal: GoalSummary): string {
  if (goal.status === "active") return "Goal is already active";
  return "";
}

/** Why a goal cannot be archived right now, or "" when it can. */
export function archiveGoalBlockedReason(goal: GoalSummary): string {
  if (goal.status === "archived") return "Goal is already archived";
  return "";
}

/**
 * Build the full action list for a goal. Every action is always present;
 * unavailable ones carry a `disabledReason`. See ./types for why.
 */
export function buildGoalActions(
  goal: GoalSummary,
  ctx: GoalActionContext,
): ActionDescriptor[] {
  const actions: ActionDescriptor[] = [];
  const name = goal.title || goal.goal_id;

  // ─── run ────────────────────────────────────────────────────────
  actions.push({
    id: "run",
    label: "Run goal now",
    group: "run",
    key: "x",
    disabledReason: runGoalBlockedReason(goal),
    run: () => ctx.runGoal(goal),
  });

  // ─── state ──────────────────────────────────────────────────────
  actions.push({
    id: "pause",
    label: "Pause goal",
    group: "state",
    key: "p",
    disabledReason: pauseGoalBlockedReason(goal),
    run: () => ctx.pauseGoal(goal),
  });

  actions.push({
    id: "resume",
    label:
      goal.status === "completed" ? "Reactivate goal" : "Resume goal",
    group: "state",
    key: "r",
    disabledReason: resumeGoalBlockedReason(goal),
    run: () => ctx.resumeGoal(goal),
  });

  actions.push({
    id: "archive",
    label: "Archive goal",
    group: "state",
    disabledReason: archiveGoalBlockedReason(goal),
    // Reversible (resume brings it back), so confirm without
    // type-to-confirm — same weight as cancelling a feature.
    confirm: {
      title: `Archive ${name}?`,
      body:
        "The goal stops reacting to events and leaves the default list. " +
        "This is reversible — resume it later from the archived filter.",
      confirmLabel: "Archive goal",
    },
    run: () => ctx.archiveGoal(goal),
  });

  // ─── edit ───────────────────────────────────────────────────────
  actions.push({
    id: "edit",
    label: "Edit goal…",
    group: "edit",
    key: "e",
    run: async () => ctx.openEdit(goal),
  });

  // ─── navigate ───────────────────────────────────────────────────
  actions.push({
    id: "details",
    label: "Goal details",
    group: "navigate",
    run: async () => ctx.openDetails(goal),
  });

  // ─── danger ─────────────────────────────────────────────────────
  actions.push({
    id: "delete",
    label: "Delete goal",
    group: "danger",
    key: "d",
    danger: true,
    confirm: {
      title: `Delete ${name}?`,
      body:
        "This permanently removes the goal, its trigger, and its config. " +
        "Generated tasks are kept. It cannot be undone — archive instead " +
        "if you only want it out of the way.",
      // Irreversible ⇒ type-to-confirm, keyed on the stable goal id.
      typeToConfirm: goal.goal_id,
      confirmLabel: "Delete permanently",
    },
    run: () => ctx.deleteGoal(goal),
  });

  return actions;
}

/**
 * Summarise a manual reconcile for a toast. The run endpoint returns the
 * audit record it produced; the decision maps to a toast tone.
 */
export function summarizeGoalRun(audit: GoalReconcileAudit): {
  message: string;
  kind: "success" | "info" | "warning";
} {
  const reason = audit.reason ? ` — ${audit.reason}` : "";
  switch (audit.decision) {
    case "complete":
      return { message: `Goal complete${reason}`, kind: "success" };
    case "block":
      return { message: `Goal blocked${reason}`, kind: "warning" };
    case "need_work":
      return {
        message: audit.generated_task_id
          ? `Generated task ${audit.generated_task_id}${reason}`
          : `Needs work${reason}`,
        kind: "info",
      };
    case "steer":
      return {
        message: `Steered ${audit.sessions_steered ?? 0} live session${
          (audit.sessions_steered ?? 0) === 1 ? "" : "s"
        }${reason}`,
        kind: "info",
      };
    default:
      return { message: `No action needed${reason}`, kind: "info" };
  }
}

// ─── create-request assembly ─────────────────────────────────────────
// Pure so GoalCreateModal stays a thin shell and the wire shape is
// testable without rendering anything.

export interface GoalCreateForm {
  project: string;
  featureId?: string;
  taskId?: string;
  title: string;
  criteria?: string;
  validation?: string;
  /** Direct prompt for the generated task (action.direct_prompt). */
  prompt?: string;
  agent?: string;
  model?: string;
  executor?: string;
  steeringEnabled: boolean;
  /** Blank ⇒ server default (15). */
  steeringCooldownMinutes?: number;
}

/** Non-empty ⇒ the form cannot be submitted, and this says why. */
export function goalCreateValidationError(form: GoalCreateForm): string {
  if (!form.title.trim()) return "Title is required";
  if (!form.project.trim()) return "Project is required";
  if (
    form.steeringCooldownMinutes !== undefined &&
    (!Number.isFinite(form.steeringCooldownMinutes) ||
      form.steeringCooldownMinutes < 0)
  ) {
    return "Steering cooldown must be a non-negative number of minutes";
  }
  return "";
}

/**
 * Assemble the CreateGoalRequest wire shape from the form.
 *
 * - `config.id` is left empty — the server derives a slug from the title.
 * - `steering` is omitted entirely when it matches the server defaults
 *   (enabled, 15-minute cooldown), so the stored entry stays minimal.
 * - `action.type` is always "prompt"; optional fields are trimmed and
 *   omitted when blank.
 */
export function assembleCreateGoalRequest(
  form: GoalCreateForm,
): import("../types").CreateGoalRequest {
  const trim = (s?: string) => (s ?? "").trim();

  const steering =
    !form.steeringEnabled ||
    (form.steeringCooldownMinutes !== undefined &&
      form.steeringCooldownMinutes > 0)
      ? {
          ...(form.steeringEnabled ? {} : { enabled: false }),
          ...(form.steeringCooldownMinutes
            ? { cooldown_minutes: form.steeringCooldownMinutes }
            : {}),
        }
      : undefined;

  return {
    project: trim(form.project),
    ...(trim(form.featureId) ? { feature_id: trim(form.featureId) } : {}),
    title: trim(form.title),
    config: {
      id: "",
      ...(trim(form.criteria) ? { criteria: trim(form.criteria) } : {}),
      ...(trim(form.validation) ? { validation: trim(form.validation) } : {}),
      ...(trim(form.taskId) ? { task_id: trim(form.taskId) } : {}),
      ...(steering ? { steering } : {}),
    },
    action: {
      type: "prompt",
      ...(trim(form.prompt) ? { direct_prompt: trim(form.prompt) } : {}),
      ...(trim(form.agent) ? { agent: trim(form.agent) } : {}),
      ...(trim(form.model) ? { model: trim(form.model) } : {}),
      ...(trim(form.executor) ? { executor: trim(form.executor) } : {}),
    },
  };
}
