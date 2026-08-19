/**
 * EntryCompare — two pinned entries, side by side or as a unified diff.
 *
 * Side-by-side reuses two compact EntryReaders (independent scroll).
 * Diff mode runs the dependency-free line diff from `lib/diff.ts` over
 * the two raw bodies and renders a unified listing with dual line-number
 * gutters. Comparing revisions isn't possible (Brain keeps no version
 * history), so this compares two *entries* — e.g. a walkthrough against
 * the summary that superseded it.
 */
import { useMemo } from "react";
import { EntryReader } from "./EntryReader";
import { ErrorState } from "../common/ErrorState";
import { Loading } from "../common/Loading";
import { useEntry } from "../../hooks/useEntries";
import { useEntriesStore } from "../../store/entries";
import { entryBasename } from "../../lib/entries";
import { diffLines, diffStats } from "../../lib/diff";

export function EntryCompare({
  aPath,
  bPath,
  onOpenEntry,
}: {
  aPath: string;
  bPath: string;
  onOpenEntry: (ref: string) => void;
}): JSX.Element {
  const diffMode = useEntriesStore((s) => s.diffMode);
  const setDiffMode = useEntriesStore((s) => s.setDiffMode);
  const setCompareOpen = useEntriesStore((s) => s.setCompareOpen);
  const clearPins = useEntriesStore((s) => s.clearPins);

  return (
    <div className="entry-compare">
      <div className="entry-compare-head">
        <span className="entry-compare-title" title={aPath}>
          {entryBasename(aPath)}
        </span>
        <span className="entry-compare-vs">⇄</span>
        <span className="entry-compare-title" title={bPath}>
          {entryBasename(bPath)}
        </span>
        <span className="spacer" />
        <div className="viewmode">
          <button
            className={diffMode ? "" : "active"}
            onClick={() => setDiffMode(false)}
          >
            Side by side
          </button>
          <button
            className={diffMode ? "active" : ""}
            onClick={() => setDiffMode(true)}
          >
            Diff
          </button>
        </div>
        <button
          className="entry-act"
          title="Close compare (keeps pins)"
          onClick={() => setCompareOpen(false)}
        >
          ✕ Close
        </button>
        <button
          className="entry-act"
          title="Close and clear both pins"
          onClick={clearPins}
        >
          Clear pins
        </button>
      </div>
      {diffMode ? (
        <DiffView aPath={aPath} bPath={bPath} />
      ) : (
        <div className="entry-compare-split">
          <div className="entry-compare-cell">
            <EntryReader path={aPath} onOpenEntry={onOpenEntry} compact />
          </div>
          <div className="entry-compare-cell">
            <EntryReader path={bPath} onOpenEntry={onOpenEntry} compact />
          </div>
        </div>
      )}
    </div>
  );
}

function DiffView({
  aPath,
  bPath,
}: {
  aPath: string;
  bPath: string;
}): JSX.Element {
  const a = useEntry(aPath);
  const b = useEntry(bPath);

  const rows = useMemo(() => {
    if (!a.entry || !b.entry) return null;
    return diffLines(a.entry.content || "", b.entry.content || "");
  }, [a.entry, b.entry]);

  if (a.error || b.error) {
    return <ErrorState error={a.error || b.error} title="Diff failed" />;
  }
  if (!rows) return <Loading label="Diffing…" />;

  const stats = diffStats(rows);
  return (
    <div className="entry-diff">
      <div className="entry-diff-stats">
        <span className="add">+{stats.added}</span>
        <span className="del">−{stats.removed}</span>
        <span className="ctx">{stats.unchanged} unchanged</span>
        <span className="names">
          − {aPath} · + {bPath}
        </span>
      </div>
      <div className="entry-diff-body">
        {rows.map((r, i) => (
          <div key={i} className={`entry-diff-row ${r.kind}`}>
            <span className="ln">{r.aLine ?? ""}</span>
            <span className="ln">{r.bLine ?? ""}</span>
            <span className="sign">
              {r.kind === "add" ? "+" : r.kind === "del" ? "−" : " "}
            </span>
            <span className="txt">{r.text || " "}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
