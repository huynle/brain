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
import { bindingProjectOf, type BindingOverrides } from "../automationBindings";
import type { BrainEntry } from "../types";
import type { ActionDescriptor } from "./types";

/**
 * Where an action is being offered from. A global automation viewed inside
 * a project turns on or off for that project only, so its toggles write the
 * project's binding and never the global entry.
 */
export interface ProjectScope {
  projectId: string;
  /** The binding that governs this project, or null when none does. */
  binding: BrainEntry | null;
  /** Whether the scheduler fires this automation for projectId (effective view). */
  targeted: boolean;
}

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
  /** Opens the automation modal on its Runs tab — the run history. */
  openHistory: (a: BrainEntry) => void;
  /** Opens the project's dockable runs pane, filtered to this one. */
  openRunsPane: (a: BrainEntry) => void;
  /** Project scope, global automation: opt this project out. Writes the binding only. */
  turnOffHere: (a: BrainEntry, scope: ProjectScope) => Promise<void>;
  /** Project scope, global automation: opt this project in, or drop its opt-out. */
  turnOnHere: (a: BrainEntry, scope: ProjectScope) => Promise<void>;
  /** Project scope, global automation: save the binding's overrides (creates one if needed). */
  saveOverrides: (
    a: BrainEntry,
    scope: ProjectScope,
    overrides: BindingOverrides,
  ) => Promise<void>;
}

export function automationName(a: BrainEntry): string {
  return a.title || a.id;
}

/** Only "active" automations fire on their trigger. */
export function isEnabledAutomation(a: BrainEntry): boolean {
  return a.status === "active";
}

/** A global automation has no owning project; it is shared by every project. */
export function isGlobalAutomation(a: BrainEntry): boolean {
  return bindingProjectOf(a) === "";
}

/**
 * Whether the automation is on for the scope's project. A global automation
 * is on here when the scheduler targets this project; anything else uses its
 * own status.
 */
export function isEnabledHere(a: BrainEntry, scope: ProjectScope): boolean {
  return isGlobalAutomation(a) ? scope.targeted : isEnabledAutomation(a);
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

/**
 * Why a global automation cannot be turned on in this project, or "" when it
 * can. Turning on does nothing while the global entry is not active.
 */
function turnOnHereBlockedReason(a: BrainEntry, scope: ProjectScope): string {
  if (scope.targeted) return "Already on in this project";
  if (a.status !== "active") {
    const state = a.status === "blocked" ? "errored" : "paused";
    return `Automation is ${state} everywhere, so it cannot be turned on here. Enable it globally first.`;
  }
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
  scope?: ProjectScope,
): ActionDescriptor[] {
  const name = automationName(a);
  const actions: ActionDescriptor[] = [];
  const here = scope && isGlobalAutomation(a) ? scope : undefined;

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
  if (here) {
    // A global automation in a project tab. The verbs act on this project's
    // binding only. The global status is never written from here.
    actions.push({
      id: "enable",
      label: "Turn on here",
      group: "state",
      key: "r",
      disabledReason: turnOnHereBlockedReason(a, here),
      run: () => ctx.turnOnHere(a, here),
    });
    actions.push({
      id: "pause",
      label: "Turn off here",
      group: "state",
      key: "p",
      disabledReason: here.targeted ? "" : "Already off in this project",
      run: () => ctx.turnOffHere(a, here),
    });
  } else {
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
  }

  // ─── navigate ───────────────────────────────────────────────────
  actions.push({
    id: "details",
    label: "Automation details",
    group: "navigate",
    run: async () => ctx.openDetails(a),
  });

  // History is a verb, not just a click target on the row's last-run
  // cell: the registry is what gives it right-click, long-press AND a
  // keyboard accelerator at once, which is the only way to reach it on
  // touch without hunting for a 10px cell.
  actions.push({
    id: "history",
    label: "Run history",
    group: "navigate",
    key: "h",
    run: async () => ctx.openHistory(a),
  });

  actions.push({
    id: "runs-pane",
    label: "Open runs pane",
    group: "navigate",
    run: async () => ctx.openRunsPane(a),
  });

  // ─── danger ─────────────────────────────────────────────────────
  actions.push({
    id: "delete",
    background: true,
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
