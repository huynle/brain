/**
 * lib/actions/automationActions — the verb matrix for an automation row.
 *
 * An automation is a BrainEntry whose status the trigger dispatcher
 * reads: only "active" automations fire, "archived" is the clean
 * paused state, and "blocked" marks one that errored. Pause/enable are
 * a status-aware pair like a goal's pause/resume — exactly one is
 * enabled at a time, the other says why it isn't.
 *
 * Built-in automations (generated_by "brain:builtin-…") cannot be
 * meaningfully deleted: the server's Ensure*Automation reconcilers
 * recreate a built-in whenever none with its marker exists. Delete is
 * therefore disabled for them — pause is the honest off switch.
 *
 * Pure: takes the entry plus effect callbacks, returns descriptors.
 * Automations do not ride SSE; every mutating effect in the context
 * invalidates the ["v2", "automations", project] query afterwards
 * (see hooks/useAutomationActionContext).
 */
import type { BrainEntry } from "../types";
import type { ActionDescriptor } from "./types";

/**
 * Effects an automation action can perform. The component supplies
 * real implementations; tests supply recorders.
 */
export interface AutomationActionContext {
  /** POST execute — manual run, regardless of paused state. */
  runAutomation: (a: BrainEntry) => Promise<void>;
  /** PATCH status=active — triggers fire again. */
  enableAutomation: (a: BrainEntry) => Promise<void>;
  /** PATCH status=archived — triggers stop firing. */
  pauseAutomation: (a: BrainEntry) => Promise<void>;
  /** DELETE the entry — permanent (non-built-ins only). */
  deleteAutomation: (a: BrainEntry) => Promise<void>;
  /** Opens the automation modal. */
  openDetails: (a: BrainEntry) => void;
}

export function automationName(a: BrainEntry): string {
  return a.title || a.id;
}

/** Only "active" automations fire on their trigger. */
export function isEnabledAutomation(a: BrainEntry): boolean {
  return a.status === "active";
}

/** Built-ins carry the server reconciler's marker. */
export function isBuiltinAutomation(a: BrainEntry): boolean {
  return (a.generated_by ?? "").startsWith("brain:builtin");
}

/** Why the automation cannot be paused right now, or "" when it can. */
export function pauseAutomationBlockedReason(a: BrainEntry): string {
  if (a.status === "archived") return "Automation is already paused";
  return "";
}

/** Why the automation cannot be enabled right now, or "" when it can. */
export function enableAutomationBlockedReason(a: BrainEntry): string {
  if (a.status === "active") return "Automation is already enabled";
  return "";
}

/** Why the automation cannot be deleted, or "" when it can. */
export function deleteAutomationBlockedReason(a: BrainEntry): string {
  if (isBuiltinAutomation(a)) {
    return "Built-in automation — the server recreates deleted built-ins; pause it instead";
  }
  return "";
}

/**
 * Build the full action list for an automation. Every action is always
 * present; unavailable ones carry a `disabledReason`. See ./types.
 */
export function buildAutomationActions(
  a: BrainEntry,
  ctx: AutomationActionContext,
): ActionDescriptor[] {
  const name = automationName(a);
  const actions: ActionDescriptor[] = [];

  // ─── run ────────────────────────────────────────────────────────
  actions.push({
    id: "run",
    label: "Run automation now",
    group: "run",
    key: "x",
    // Manual runs work even while paused — that is how an operator
    // tests one before re-enabling its trigger.
    run: () => ctx.runAutomation(a),
  });

  // ─── state ──────────────────────────────────────────────────────
  actions.push({
    id: "enable",
    label:
      a.status === "blocked" ? "Re-enable automation" : "Enable automation",
    group: "state",
    key: "r",
    disabledReason: enableAutomationBlockedReason(a),
    run: () => ctx.enableAutomation(a),
  });

  actions.push({
    id: "pause",
    label:
      a.status === "blocked"
        ? "Pause automation (stop retries)"
        : "Pause automation",
    group: "state",
    key: "p",
    disabledReason: pauseAutomationBlockedReason(a),
    run: () => ctx.pauseAutomation(a),
  });

  // ─── navigate ───────────────────────────────────────────────────
  actions.push({
    id: "details",
    label: "Automation details",
    group: "navigate",
    run: async () => ctx.openDetails(a),
  });

  // ─── danger ─────────────────────────────────────────────────────
  actions.push({
    id: "delete",
    label: "Delete automation",
    group: "danger",
    key: "d",
    danger: true,
    disabledReason: deleteAutomationBlockedReason(a),
    confirm: {
      title: `Delete ${name}?`,
      body:
        "This permanently removes the automation and its trigger. " +
        "It cannot be undone — pause it instead if you only want it " +
        "to stop firing.",
      // Irreversible ⇒ type-to-confirm, keyed on the stable id.
      typeToConfirm: a.id,
      confirmLabel: "Delete permanently",
    },
    run: () => ctx.deleteAutomation(a),
  });

  return actions;
}
