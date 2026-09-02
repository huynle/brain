/**
 * useAutoArchive — the effects behind the auto-archive checkbox.
 *
 * Ticking creates a per-project automation; unticking deletes it. Both go
 * through the ordinary entry API, so the switch is visible and editable
 * in the Automations tab like anything else — it is not hidden state.
 *
 * `enabled` is derived from the automation list rather than kept locally,
 * so a switch someone paused, edited or deleted from the Automations tab
 * is reflected here instead of the checkbox insisting on its own version
 * of the truth.
 */
import { useCallback } from "react";

import { useUI } from "../store/ui";
import { createEntry, deleteEntry, updateEntry } from "../lib/api";
import {
  AUTO_ARCHIVE_TITLE,
  autoArchiveEntry,
  findAutoArchive,
  isAutoArchiveOn,
} from "../lib/autoArchive";
import { useAutomations } from "./useAutomations";

export interface UseAutoArchiveResult {
  enabled: boolean;
  /** True until the automation list has been read once. */
  isLoading: boolean;
  toggle: () => Promise<void>;
}

export function useAutoArchive(projectId: string): UseAutoArchiveResult {
  const toast = useUI((s) => s.toast);
  const { automations, isLoading, refetch } = useAutomations(projectId);

  const enabled = isAutoArchiveOn(automations);

  const toggle = useCallback(async () => {
    const existing = findAutoArchive(automations);
    // A switch that EXISTS but is not active reads as OFF, so a click on
    // it means "turn this on". Branching on existence alone would take
    // the delete path and destroy the automation the user was trying to
    // enable — the opposite of the requested action, unconfirmed, with
    // the box left unticked either way. `enabled` is what the checkbox
    // shows, so `enabled` is what the toggle has to invert.
    if (existing && !enabled) {
      await updateEntry(existing.path, { status: "active" });
      refetch();
      toast(`${projectId}: auto-archive on`, "success");
      return;
    }
    if (existing) {
      // Delete rather than pause: the checkbox is a two-state control and
      // leaving a paused husk behind would make the Automations tab show
      // a switch the card says is off.
      await deleteEntry(existing.path);
      refetch();
      toast(`${projectId}: auto-archive off`, "success");
      return;
    }
    await createEntry(autoArchiveEntry(projectId));
    refetch();
    toast(
      `${projectId}: ${AUTO_ARCHIVE_TITLE.toLowerCase()} — a feature's tasks ` +
        `move to Archived as soon as it completes`,
      "success",
    );
  }, [automations, enabled, projectId, refetch, toast]);

  return { enabled, isLoading, toggle };
}
