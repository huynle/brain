/**
 * EntriesPreview — the Overview grid's Brain-memory carousel (panel 4
 * of the wireframe order, previously unimplemented).
 *
 * Shows the most recently updated knowledge entries as cards; clicking
 * one opens it in the Entries view. Hidden entirely while the store has
 * no knowledge entries so fresh installs don't render an empty shell.
 */
import { useMemo } from "react";
import { useEntryList } from "../../hooks/useEntries";
import { useEntriesStore } from "../../store/entries";
import { useWorkspace } from "../../store/workspace";
import { relativeTime } from "../../lib/format";
import { entryProject, excerptOf } from "../../lib/entries";

const PREVIEW_COUNT = 8;

export function EntriesPreview(): JSX.Element | null {
  const setView = useWorkspace((s) => s.setView);
  const selectEntry = useEntriesStore((s) => s.selectEntry);

  const { entries } = useEntryList({
    typeFilter: "knowledge",
    projectFilter: "",
    statusFilter: "",
    sortBy: "modified",
    sortOrder: "desc",
  });

  const recent = useMemo(() => entries.slice(0, PREVIEW_COUNT), [entries]);

  const openEntry = (path: string) => {
    selectEntry(path);
    setView("entries");
  };

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
            onClick={() => openEntry(e.path)}
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
    </div>
  );
}
