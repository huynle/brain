/**
 * EntriesPreview — the Overview grid's Brain-memory carousel (panel 4
 * of the wireframe order, previously unimplemented).
 *
 * Shows the most recently updated knowledge entries as cards; clicking
 * one opens it in the Entries view. Hidden entirely while the store has
 * no knowledge entries so fresh installs don't render an empty shell.
 *
 * Card verbs come from `lib/actions/entryActions` via `useRowActions` —
 * the same matrix as the Entries browser rows, with "open" meaning this
 * surface's existing click (select + switch to the Entries view).
 */
import { useCallback, useMemo } from "react";
import { useEntryList } from "../../hooks/useEntries";
import { useVisibleProjects } from "../../hooks/useVisibleProjects";
import { useEntryActionContext } from "../../hooks/useEntryActionContext";
import { useDeferredPreview } from "../../hooks/useDeferredPreview";
import { useRowActions } from "../../hooks/useRowActions";
import {
  buildEntryActions,
  type EntryActionTarget,
} from "../../lib/actions/entryActions";
import { useEntriesStore } from "../../store/entries";
import { useWorkspace } from "../../store/workspace";
import { relativeTime } from "../../lib/format";
import {
  PROJECT_FILTER_SIDEBAR,
  entryProject,
  excerptOf,
  resolveProjectScope,
} from "../../lib/entries";

const PREVIEW_COUNT = 8;

export function EntriesPreview(): JSX.Element | null {
  const setView = useWorkspace((s) => s.setView);
  const selectEntry = useEntriesStore((s) => s.selectEntry);
  const comparePins = useEntriesStore((s) => s.comparePins);

  // This carousel sits inside the Overview grid, which already shows only
  // the sidebar's visible projects — so it follows the same scope rather
  // than surfacing "recent" entries from projects the grid above it hides.
  const sidebar = useVisibleProjects();
  const scope = useMemo(
    () =>
      resolveProjectScope(PROJECT_FILTER_SIDEBAR, {
        projects: sidebar.visible,
        unfiltered: sidebar.unfiltered || sidebar.loading,
      }),
    [sidebar.visible, sidebar.unfiltered, sidebar.loading],
  );

  const { entries } = useEntryList({
    typeFilter: "knowledge",
    scope,
    statusFilter: "",
    sortBy: "modified",
    sortOrder: "desc",
  });

  const recent = useMemo(() => entries.slice(0, PREVIEW_COUNT), [entries]);

  // Single click previews in the side panel, double click pins into Focus
  // — the same contract as every task, feature, session and project row.
  //
  // Clicking a card used to NAVIGATE the whole workspace to the Entries
  // browser, which is a heavier move than "let me glance at this": it
  // replaced whatever the user was looking at, and getting back meant
  // finding their way to Overview again. "Open all →" is still there for
  // when going to the browser IS the intent.
  const preview = useDeferredPreview();
  const previewInSidebar = useWorkspace((s) => s.openOrReuseInSidebar);
  const openInFocus = useWorkspace((s) => s.openInFocus);

  const previewEntry = (path: string, title: string) =>
    previewInSidebar("entry", { path }, title);
  const pinEntry = (path: string, title: string) => {
    preview.cancel();
    openInFocus("entry", { path }, title);
  };

  // Card context-menu wiring. Its "open" verb keeps meaning "take me to
  // the Entries browser" — an explicit navigation, deliberately distinct
  // from the click's glance-in-the-side-panel.
  const openTarget = useCallback(
    (t: EntryActionTarget) => {
      selectEntry(t.path);
      setView("entries");
    },
    [selectEntry, setView],
  );
  const entryCtx = useEntryActionContext(openTarget);
  const { rowProps, overlays } = useRowActions();

  if (recent.length === 0) return null;

  return (
    <div className="entries-preview">
      <div className="ep-head">
        <span>Brain memory · recently updated</span>
        <button className="entry-act" onClick={() => setView("entries")}>
          Open all →
        </button>
      </div>
      <div className="entry-grid">
        {recent.map((e) => (
          <div
            key={e.path}
            className="entry-card"
            {...rowProps(
              buildEntryActions(
                e,
                { pinned: comparePins.includes(e.path) },
                entryCtx,
              ),
              e.title || e.path,
              // Enter previews immediately: no double-click to wait out.
              () => previewEntry(e.path, e.title || e.path),
            )}
            onClick={() =>
              preview.schedule(() => previewEntry(e.path, e.title || e.path))
            }
            onDoubleClick={() => pinEntry(e.path, e.title || e.path)}
            title={e.path}
          >
            <div className="entry-top">
              <span className="entry-type">{e.type}</span>
              <span className="entry-updated" title={e.modified}>
                {entryProject(e)} · {relativeTime(e.modified)}
              </span>
            </div>
            <div className="entry-title">{e.title || e.path}</div>
            <div className="entry-excerpt">{excerptOf(e.content || "")}</div>
          </div>
        ))}
      </div>
      {overlays}
    </div>
  );
}
