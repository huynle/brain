/**
 * lib/actions — the single description of what a user can do to a task or
 * a feature.
 *
 * The problem this solves: before this module, "Run" existed in three
 * places with three labels and three slightly different behaviours, while
 * Delete and Set-status existed nowhere despite working API wrappers. Any
 * new verb meant touching a context menu, a modal footer, and (later) a
 * touch sheet and the command palette — so in practice verbs got added to
 * one surface and forgotten in the others.
 *
 * Here, a verb is described once as data. Four renderers consume the same
 * list:
 *
 *   ContextMenu     desktop right-click
 *   ActionSheet     touch long-press
 *   Modal footer    TaskModal / FeatureModal
 *   CommandPalette  keyboard
 *
 * Design rules worth keeping:
 *
 * - **Disabled, never hidden.** An action that doesn't apply right now
 *   renders greyed with `disabledReason` as its tooltip. Hiding it leaves
 *   the user hunting for something they saw yesterday; showing it with
 *   "task is already completed" teaches them the model.
 * - **Pure.** Builders take plain data and return plain data — no hooks,
 *   no fetch, no react. That is what makes the whole matrix testable in
 *   `node --test` rather than through a rendered DOM.
 * - **Effects live in the context.** The builder never calls the API
 *   directly; it closes over an `ActionContext` the component supplies.
 *   Tests pass a recording stub and assert on intent.
 */

/** Visual/semantic grouping. Renderers order groups exactly like this. */
export type ActionGroup =
  | "select"
  | "run"
  | "state"
  | "edit"
  | "navigate"
  | "danger";

export const ACTION_GROUP_ORDER: readonly ActionGroup[] = [
  // Selection first: marking rows is the cheapest, most reversible verb,
  // and surfacing it at the top teaches the multi-select affordance.
  "select",
  "run",
  "state",
  "edit",
  "navigate",
  "danger",
];

/** Confirmation requirements for an irreversible action. */
export interface ActionConfirm {
  title: string;
  /** Plain-language consequence. Say what will happen, not "are you sure". */
  body: string;
  /**
   * When set, the user must type this exact string to enable the confirm
   * button. Reserved for the genuinely unrecoverable (deleting a feature's
   * tasks), never for a reversible status flip.
   */
  typeToConfirm?: string;
  /** Label for the confirming button. Defaults to the action's label. */
  confirmLabel?: string;
}

export interface ActionDescriptor {
  /** Stable identity. Used as a react key and as a test handle. */
  id: string;
  /** Imperative and specific: "Cancel task", not "Cancel". */
  label: string;
  group: ActionGroup;
  /**
   * Single-key accelerator when a row has focus, k9s style. Only set on
   * actions worth a dedicated key; the rest are reachable via the menu.
   */
  key?: string;
  /**
   * Non-empty ⇒ the action cannot run now, and this says why. Renderers
   * must show it (tooltip / subtitle) rather than dropping the row.
   */
  disabledReason?: string;
  /** Renders in the destructive tone. */
  danger?: boolean;
  /** Route through the confirm dialog before `run`. */
  confirm?: ActionConfirm;
  /**
   * Performs the action. May throw — renderers catch and surface the
   * message as an error toast, so builders need no error handling.
   */
  run: () => Promise<void>;
}

/** True when the action is currently invokable. */
export function isEnabled(a: ActionDescriptor): boolean {
  return !a.disabledReason;
}

/**
 * Sort into the canonical group order, preserving each builder's ordering
 * within a group. Stable, so a builder's deliberate sequence survives.
 */
export function sortActions(
  actions: readonly ActionDescriptor[],
): ActionDescriptor[] {
  const rank = new Map<ActionGroup, number>(
    ACTION_GROUP_ORDER.map((g, i) => [g, i]),
  );
  return [...actions].sort(
    (a, b) => (rank.get(a.group) ?? 99) - (rank.get(b.group) ?? 99),
  );
}

/**
 * Split a sorted action list into contiguous runs of the same group, so a
 * renderer can drop separators between them without hard-coding which
 * groups exist.
 */
export function groupActions(
  actions: readonly ActionDescriptor[],
): ActionDescriptor[][] {
  const sorted = sortActions(actions);
  const out: ActionDescriptor[][] = [];
  let current: ActionDescriptor[] = [];
  let group: ActionGroup | null = null;

  for (const a of sorted) {
    if (a.group !== group) {
      if (current.length > 0) out.push(current);
      current = [];
      group = a.group;
    }
    current.push(a);
  }
  if (current.length > 0) out.push(current);
  return out;
}

/**
 * Find the action bound to a keyboard accelerator. Disabled actions are
 * skipped so a key press never silently does nothing when a later action
 * shares the key — and never fires something the menu shows as unavailable.
 */
export function findByKey(
  actions: readonly ActionDescriptor[],
  key: string,
): ActionDescriptor | undefined {
  return actions.find((a) => a.key === key && isEnabled(a));
}
