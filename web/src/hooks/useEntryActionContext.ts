/**
 * useEntryActionContext — binds the pure entry-action builders to real
 * effects (API calls, the entries store, clipboard, toasts).
 *
 * Entry data does not ride SSE — the mutating effects invalidate the
 * ["entries"] query prefix (list, per-entry, search, graph, stats — see
 * hooks/useEntries) so the 30s stale window doesn't leave a deleted or
 * re-statused row on screen after the user just acted on it.
 *
 * `openEntry` is a parameter, not an implementation: the three surfaces
 * open an entry three different ways (browser selects into the reader,
 * the Overview preview also switches view, the reader footer follows
 * its onOpenEntry link prop). Callers pass a stable callback.
 */
import { useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { useUI } from "../store/ui";
import { useEntriesStore } from "../store/entries";
import { deleteEntry as apiDeleteEntry, updateEntry } from "../lib/api";
import {
  entryName,
  type EntryActionContext,
  type EntryActionTarget,
} from "../lib/actions/entryActions";

export function useEntryActionContext(
  openEntry: (e: EntryActionTarget) => void,
): EntryActionContext {
  const toast = useUI((s) => s.toast);
  const queryClient = useQueryClient();

  return useMemo(() => {
    const invalidate = () =>
      void queryClient.invalidateQueries({ queryKey: ["entries"] });

    return {
      openEntry,

      togglePin: (e: EntryActionTarget) => {
        // getState() rather than a subscription: reading pins at call
        // time keeps the memo stable and the toast truthful even when
        // the menu was opened before another surface changed the pins.
        const s = useEntriesStore.getState();
        const wasPinned = s.comparePins.includes(e.path);
        s.togglePin(e.path);
        toast(
          wasPinned
            ? `Unpinned ${entryName(e)}`
            : `Pinned ${entryName(e)} for compare`,
          "info",
        );
      },

      copyPath: async (e: EntryActionTarget) => {
        // navigator.clipboard is undefined on insecure origins; throw
        // so the action runner surfaces it instead of a silent no-op.
        if (!navigator.clipboard) {
          throw new Error("Clipboard unavailable (insecure context)");
        }
        await navigator.clipboard.writeText(e.path);
        toast("Path copied", "info");
      },

      setEntryStatus: async (e: EntryActionTarget, status: string) => {
        await updateEntry(e.path, { status });
        invalidate();
        toast(
          status === "archived"
            ? `Archived ${entryName(e)}`
            : `${entryName(e)} is now ${status}`,
          "success",
        );
      },

      deleteEntry: async (e: EntryActionTarget) => {
        await apiDeleteEntry(e.path);
        // Drop dangling references before the refetch lands: a reader
        // open on the deleted path would show "not found", and a stale
        // compare pin would poison the next compare.
        const s = useEntriesStore.getState();
        if (s.selectedPath === e.path) s.selectEntry(null);
        if (s.comparePins.includes(e.path)) s.togglePin(e.path);
        invalidate();
        toast(`Deleted ${entryName(e)}`, "success");
      },
    };
  }, [openEntry, toast, queryClient]);
}
