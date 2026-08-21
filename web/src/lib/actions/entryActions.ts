/**
 * lib/actions/entryActions — the verb matrix for a Brain entry row.
 *
 * Entries surface in three places with three different "open" gestures:
 * the Entries browser list (click selects into the reader pane), the
 * Overview preview cards (click selects AND switches view), and the
 * reader's graph footer (click follows the link via the surface's
 * onOpenEntry). The builder stays pure by delegating "open" to the
 * context — each surface supplies its own.
 *
 * Two entry-specific wrinkles:
 *
 * - **Pin, not Select.** Compare pins are the entries browser's marking
 *   mechanism (store/entries `comparePins`, max two). The verb lives in
 *   the "select" group so it sorts first like a task's Select, but its
 *   id is deliberately NOT "select": useRowActions special-cases that id
 *   on long-press (runs it instead of opening the sheet), and silently
 *   pinning on a touch long-press would hide every other verb.
 * - **Archive is a plain flip.** Unlike a goal or automation, archiving
 *   a knowledge entry changes only its frontmatter status — nothing
 *   stops firing, and the default browse (any-status) still lists it.
 *   So the pair confirms nothing, same weight as an automation pause.
 *
 * Pure: takes the entry-shaped data plus effect callbacks, returns
 * descriptors. Entry data does not ride SSE; mutating effects in the
 * context invalidate the ["entries"] query prefix afterwards (see
 * hooks/useEntryActionContext).
 */
import { entryBasename } from "../entries";
import type { ActionDescriptor } from "./types";

/**
 * The slice of an entry the builders need. Both list rows (BrainEntry)
 * and search rows (SearchResult) satisfy it structurally, so one
 * builder serves browse mode and search mode.
 */
export interface EntryActionTarget {
  path: string;
  id?: string;
  title?: string;
  status?: string;
}

/**
 * Effects an entry action can perform. The component supplies real
 * implementations; tests supply recorders.
 */
export interface EntryActionContext {
  /** Open the entry however this surface opens one (select / view-switch / link-follow). */
  openEntry: (e: EntryActionTarget) => void;
  /** Toggle the compare pin (store/entries togglePin). */
  togglePin: (e: EntryActionTarget) => void;
  /** Copy the entry path to the clipboard. */
  copyPath: (e: EntryActionTarget) => Promise<void>;
  /** PATCH {status} — the archive/unarchive flip. */
  setEntryStatus: (e: EntryActionTarget, status: string) => Promise<void>;
  /** DELETE the entry file — permanent. */
  deleteEntry: (e: EntryActionTarget) => Promise<void>;
}

/**
 * Stable short identity for type-to-confirm: the entry id when the
 * surface has one, else the path basename — which for canonical entry
 * paths ("projects/x/plan/ab12cd34.md") is the same 8-char short id.
 * Short and visible on the row, so the confirm friction stays honest.
 */
export function entrySlug(e: EntryActionTarget): string {
  return e.id || entryBasename(e.path);
}

export function entryName(e: EntryActionTarget): string {
  return e.title || entrySlug(e);
}

/**
 * Where the row lives:
 *   "browser" — a management surface (browser list, preview cards):
 *               the full verb set.
 *   "link"    — the reader's backlink/related footer: open + pin +
 *               copy only. Those rows are references INTO the graph,
 *               not the entry's management surface — mutating a
 *               neighbour from a footnote is a misclick magnet, and
 *               the full set is one "Open entry" away.
 */
export type EntryActionSurface = "browser" | "link";

export interface EntryActionOptions {
  /** Whether the entry is currently pinned for compare (store state —
   *  the pure builder cannot read it). */
  pinned: boolean;
  surface?: EntryActionSurface;
}

/**
 * Build the action list for an entry. On the "browser" surface every
 * verb is present (see ./types for the disabled-never-hidden rule);
 * archive/unarchive render as one status-aware slot, the same
 * sanctioned toggle exception taskActions uses.
 *
 * Accelerator constraint: EntriesBrowser's pane-level keydown owns
 * "/", "j", "k" and "c" while the row keydown does not stopPropagation,
 * so row keys must avoid those — "c" on both layers would pin and
 * immediately unpin.
 */
export function buildEntryActions(
  e: EntryActionTarget,
  opts: EntryActionOptions,
  ctx: EntryActionContext,
): ActionDescriptor[] {
  const actions: ActionDescriptor[] = [];
  const name = entryName(e);
  const link = opts.surface === "link";

  // ─── select ─────────────────────────────────────────────────────
  actions.push({
    id: "pin",
    label: opts.pinned ? "Unpin from compare" : "Pin for compare",
    group: "select",
    key: "p",
    run: async () => ctx.togglePin(e),
  });

  // ─── state ──────────────────────────────────────────────────────
  if (!link) {
    if (e.status === "archived") {
      actions.push({
        id: "unarchive",
        label: "Unarchive entry",
        group: "state",
        // "active" rather than the pre-archive status (not recorded
        // anywhere): the live default for knowledge entries. Tasks
        // restore "completed" because they represent settled work;
        // knowledge that is un-archived is current again.
        run: () => ctx.setEntryStatus(e, "active"),
      });
    } else {
      actions.push({
        id: "archive",
        label: "Archive entry",
        group: "state",
        run: () => ctx.setEntryStatus(e, "archived"),
      });
    }
  }

  // ─── navigate ───────────────────────────────────────────────────
  actions.push({
    id: "open",
    label: "Open entry",
    group: "navigate",
    key: "o",
    run: async () => ctx.openEntry(e),
  });

  actions.push({
    id: "copy-path",
    label: "Copy entry path",
    group: "navigate",
    run: () => ctx.copyPath(e),
  });

  // ─── danger ─────────────────────────────────────────────────────
  if (!link) {
    actions.push({
      id: "delete",
      label: "Delete entry",
      group: "danger",
      key: "d",
      danger: true,
      confirm: {
        title: `Delete ${name}?`,
        body:
          "This permanently removes the entry file, its links, and its " +
          "search index record. It cannot be undone — archive it instead " +
          "if you only want it marked stale.",
        // Irreversible ⇒ type-to-confirm, keyed on the short id/slug.
        typeToConfirm: entrySlug(e),
        confirmLabel: "Delete permanently",
      },
      run: () => ctx.deleteEntry(e),
    });
  }

  return actions;
}
