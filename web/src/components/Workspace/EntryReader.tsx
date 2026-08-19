/**
 * EntryReader — the reading surface for one Brain entry.
 *
 * Header (type / status / title / meta / tags / actions), rendered
 * markdown body (EntryMarkdown) with an optional table of contents,
 * and a backlinks + related footer fed by the graph endpoints.
 *
 * Used by the Entries browser detail pane, the `entry` focus-pane
 * leaf, and (in `compact` mode, sans graph footer) the Compare view.
 */
import { useEffect, useMemo, useRef, useState } from "react";
import { Chip } from "../common/Chip";
import { Dot, type DotVariant } from "../common/Dot";
import { ErrorState } from "../common/ErrorState";
import { Loading } from "../common/Loading";
import { EntryMarkdown } from "./EntryMarkdown";
import { useEntry, useEntryGraph } from "../../hooks/useEntries";
import { useEntriesStore } from "../../store/entries";
import { useWorkspace } from "../../store/workspace";
import { useUI } from "../../store/ui";
import { relativeTime, statusLabel } from "../../lib/format";
import { entryProject, extractHeadings } from "../../lib/entries";
import type { BrainEntry } from "../../lib/types";

function entryDotVariant(status: string): DotVariant {
  switch (status) {
    case "active":
    case "completed":
    case "validated":
      return "on";
    case "in_progress":
      return "busy";
    case "blocked":
      return "err";
    default:
      return "stale";
  }
}

export function EntryReader({
  path,
  onOpenEntry,
  onCanonicalPath,
  compact = false,
}: {
  path: string;
  onOpenEntry: (ref: string) => void;
  /** Called when the API resolves `path` (which may be a short id or
   *  entry-link ref) to a different canonical path. */
  onCanonicalPath?: (canonical: string) => void;
  compact?: boolean;
}): JSX.Element {
  const { entry, loading, error } = useEntry(path);

  const entryPath = entry?.path;
  useEffect(() => {
    if (entryPath && entryPath !== path) onCanonicalPath?.(entryPath);
  }, [entryPath, path, onCanonicalPath]);

  if (error) {
    return (
      <div className="entry-reader">
        <ErrorState error={error} title={`Failed to load ${path}`} />
      </div>
    );
  }
  if (loading || !entry) {
    return (
      <div className="entry-reader">
        <Loading label="Loading entry…" />
      </div>
    );
  }
  return (
    <LoadedReader entry={entry} onOpenEntry={onOpenEntry} compact={compact} />
  );
}

function LoadedReader({
  entry,
  onOpenEntry,
  compact,
}: {
  entry: BrainEntry;
  onOpenEntry: (ref: string) => void;
  compact: boolean;
}): JSX.Element {
  const [rawMode, setRawMode] = useState(false);
  const [tocOpen, setTocOpen] = useState(false);
  const bodyRef = useRef<HTMLDivElement | null>(null);

  const toast = useUI((s) => s.toast);
  const comparePins = useEntriesStore((s) => s.comparePins);
  const togglePin = useEntriesStore((s) => s.togglePin);
  const openInFocus = useWorkspace((s) => s.openInFocus);

  const headings = useMemo(
    () => extractHeadings(entry.content || ""),
    [entry.content],
  );
  const pinned = comparePins.includes(entry.path);
  const project = entryProject(entry);

  const jumpTo = (slug: string) => {
    const inst = bodyRef.current
      ?.querySelector(".entry-md")
      ?.getAttribute("data-md-instance");
    if (!inst) return;
    document.getElementById(`${inst}-${slug}`)?.scrollIntoView({
      block: "start",
      behavior: "smooth",
    });
  };

  const copyPath = () => {
    navigator.clipboard
      ?.writeText(entry.path)
      .then(() => toast("Path copied", "info"))
      .catch(() => toast("Copy failed", "error"));
  };

  return (
    <div className="entry-reader">
      <div className="entry-reader-head">
        <div className="entry-reader-toprow">
          <span className="entry-type">{entry.type}</span>
          <span className="entry-status">
            <Dot variant={entryDotVariant(entry.status)} />{" "}
            {statusLabel(entry.status)}
          </span>
          <span className="spacer" />
          {headings.length >= 2 && (
            <button
              className={`entry-act ${tocOpen ? "active" : ""}`}
              title="Table of contents"
              onClick={() => setTocOpen((v) => !v)}
            >
              ☰ TOC
            </button>
          )}
          <button
            className={`entry-act ${rawMode ? "active" : ""}`}
            title="Toggle raw markdown"
            onClick={() => setRawMode((v) => !v)}
          >
            Raw
          </button>
          <button
            className={`entry-act ${pinned ? "active" : ""}`}
            title={pinned ? "Unpin from compare" : "Pin for compare"}
            onClick={() => togglePin(entry.path)}
          >
            ⇄ {pinned ? "Pinned" : "Compare"}
          </button>
          {!compact && (
            <button
              className="entry-act"
              title="Open in a Focus pane"
              onClick={() =>
                openInFocus("entry", { path: entry.path }, entry.title)
              }
            >
              ⊞ Focus
            </button>
          )}
        </div>
        <div className="entry-reader-title">{entry.title || "(untitled)"}</div>
        <div className="entry-reader-meta">
          {project && <span className="entry-meta-project">{project}</span>}
          {entry.modified && (
            <span title={entry.modified}>
              updated {relativeTime(entry.modified)}
            </span>
          )}
          {entry.created && (
            <span title={entry.created}>
              created {new Date(entry.created).toLocaleDateString()}
            </span>
          )}
          <span
            className="entry-meta-path"
            title="Click to copy path"
            onClick={copyPath}
          >
            {entry.path} ⧉
          </span>
        </div>
        {(entry.tags?.length ?? 0) > 0 && (
          <div className="entry-reader-tags">
            {entry.tags!.map((t) => (
              <Chip key={t} variant="mini">
                {t}
              </Chip>
            ))}
          </div>
        )}
      </div>

      <div className="entry-reader-body" ref={bodyRef}>
        {tocOpen && headings.length > 0 && (
          <div className="entry-toc">
            {headings.map((h) => (
              <div
                key={h.slug}
                className="entry-toc-row"
                style={{ paddingLeft: 6 + (h.level - 1) * 12 }}
                onClick={() => jumpTo(h.slug)}
              >
                {h.text}
              </div>
            ))}
          </div>
        )}
        {rawMode ? (
          <pre className="entry-raw">{entry.content || "(empty entry)"}</pre>
        ) : entry.content ? (
          <EntryMarkdown content={entry.content} onOpenEntry={onOpenEntry} />
        ) : (
          <div className="entry-empty-body">(empty entry)</div>
        )}

        {!compact && <GraphFooter entry={entry} onOpenEntry={onOpenEntry} />}
      </div>
    </div>
  );
}

function GraphFooter({
  entry,
  onOpenEntry,
}: {
  entry: BrainEntry;
  onOpenEntry: (ref: string) => void;
}): JSX.Element | null {
  const { backlinks, related } = useEntryGraph(entry.id);
  // Related is co-citation based and can echo the entry itself.
  const rel = related.filter((e) => e.path !== entry.path);
  if (backlinks.length === 0 && rel.length === 0) return null;
  return (
    <div className="entry-graph">
      {backlinks.length > 0 && (
        <GraphSection
          label={`Linked from (${backlinks.length})`}
          items={backlinks}
          onOpenEntry={onOpenEntry}
        />
      )}
      {rel.length > 0 && (
        <GraphSection
          label={`Related (${rel.length})`}
          items={rel}
          onOpenEntry={onOpenEntry}
        />
      )}
    </div>
  );
}

function GraphSection({
  label,
  items,
  onOpenEntry,
}: {
  label: string;
  items: BrainEntry[];
  onOpenEntry: (ref: string) => void;
}): JSX.Element {
  return (
    <div className="entry-graph-section">
      <div className="entry-graph-label">{label}</div>
      {items.map((e) => (
        <div
          key={e.path}
          className="entry-graph-row"
          onClick={() => onOpenEntry(e.path)}
          title={e.path}
        >
          <span className="entry-type">{e.type}</span>
          <span className="entry-graph-title">{e.title || e.path}</span>
        </div>
      ))}
    </div>
  );
}
