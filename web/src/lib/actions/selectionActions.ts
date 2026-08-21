/**
 * lib/actions/selectionActions — the verbs for an entire selection.
 *
 * Right-clicking (or long-pressing) a MARKED row while selection mode
 * is active offers these instead of the row's own verbs: the gesture
 * targets "everything I marked", not the one row under the cursor.
 *
 * Pure, like every other builder. Archive and Delete do not carry a
 * `confirm` here on purpose: SelectionBar owns the real ladders
 * (dry-run preview, typed confirmation, force retry), so these verbs
 * just post a request to the selection store and the bar takes over.
 * A confirm on the descriptor would double-prompt.
 */
import type { ActionDescriptor } from "./types";

export interface SelectionActionContext {
  /** Marked rows, tasks + features, for the labels. */
  count: number;
  /** Posts an archive/delete request for SelectionBar to consume. */
  requestVerb: (verb: "archive" | "delete") => void;
  clearSelection: () => void;
}

export function buildSelectionActions(
  ctx: SelectionActionContext,
): ActionDescriptor[] {
  const n = ctx.count;
  return [
    {
      id: "selection-clear",
      label: "Clear selection",
      group: "select",
      run: async () => ctx.clearSelection(),
    },
    {
      id: "selection-archive",
      label: `Archive selection (${n})`,
      group: "state",
      run: async () => ctx.requestVerb("archive"),
    },
    {
      id: "selection-delete",
      label: `Delete selection (${n})`,
      group: "danger",
      danger: true,
      run: async () => ctx.requestVerb("delete"),
    },
  ];
}
