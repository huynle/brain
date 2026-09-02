/**
 * lib/autoArchive — the per-project "archive a feature when it finishes"
 * switch, expressed as an automation entry.
 *
 * The checkbox has to survive the browser being closed, so it cannot be a
 * client-side watcher: it creates a real automation on the server, which
 * fires on `feature.completed` and archives that feature's tasks in the
 * API process (see `AutomationService.applyUpdateAction`). Closing the tab
 * changes nothing; so does never opening one.
 *
 * ─── Why `update` and not `script` ───────────────────────────────
 *
 * Every other automation action ends in "queue a task for a runner". For
 * a shell command or a model prompt that is right. For a status write it
 * is not: it would mean this switch silently does nothing unless a runner
 * is online with the script executor enabled — a dependency nobody
 * ticking a checkbox would expect. `update` was already a declared action
 * type with no implementation behind it; it has one now.
 *
 * Pure: identification and payload only. The effects live in
 * `useAutoArchive`.
 */
import type { BrainEntry } from "./types";
import type { CreateEntryRequest } from "./api";

/** Tag that marks an automation as this switch. Tags survive round-trips
 *  through the entry API, which `generated_by` on a user-created entry
 *  does not reliably do. */
export const AUTO_ARCHIVE_TAG = "brain:auto-archive";

export const AUTO_ARCHIVE_TITLE = "Auto-archive completed features";

/**
 * Is this entry the project's auto-archive switch?
 *
 * Matched on the tag AND on the action actually being an archive update —
 * a tag alone would let an unrelated entry someone tagged by hand present
 * itself as the switch, and then the checkbox would claim to be on while
 * nothing archives anything.
 */
export function isAutoArchiveAutomation(entry: BrainEntry): boolean {
  if (entry.type !== "automation") return false;
  if (!(entry.tags ?? []).includes(AUTO_ARCHIVE_TAG)) return false;
  const action = entry.action;
  if (!action) return false;
  return action.type === "update" && action.set_status === "archived";
}

/** The project's switch, if it has one. */
export function findAutoArchive(
  automations: readonly BrainEntry[],
): BrainEntry | undefined {
  return automations.find(isAutoArchiveAutomation);
}

/**
 * True when the switch exists AND is live. A paused automation is still
 * an entry, and reporting it as on would be a checkbox that ticks while
 * nothing happens.
 */
export function isAutoArchiveOn(
  automations: readonly BrainEntry[],
): boolean {
  const found = findAutoArchive(automations);
  return !!found && found.status === "active";
}

/**
 * The entry to create when the box is ticked.
 *
 * `once_per` is deliberately ABSENT. It is the dedup every other action
 * type relies on, and here it would be actively wrong: it fires a
 * feature exactly once, forever, so a feature that gains a task after its
 * first archive pass would strand that task un-archived with no second
 * chance. The update action guards its own loop instead, by writing only
 * the tasks that are not already at the target status — so a firing with
 * nothing to do writes nothing and emits nothing.
 */
export function autoArchiveEntry(projectId: string): CreateEntryRequest {
  return {
    type: "automation",
    title: AUTO_ARCHIVE_TITLE,
    content:
      `Archives every task in a feature as soon as that feature completes, ` +
      `keeping the ${projectId} board to work that is still live. Created ` +
      `by the Auto-archive checkbox on the project card; untick it to ` +
      `remove this automation. Archived tasks stay in the Archived tab and ` +
      `can be restored from there.`,
    status: "active",
    project: projectId,
    tags: [AUTO_ARCHIVE_TAG],
    trigger: {
      type: "event",
      event: "feature.completed",
    },
    action: {
      type: "update",
      set_status: "archived",
    },
  };
}
