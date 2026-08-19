/**
 * EntriesBrowser — the top-level "Entries" workspace view: browse,
 * search, read, and compare Brain knowledge entries.
 *
 * Layout (desktop):
 *   .entries-pane
 *     .entries-toolbar   search / strategy / project / status / sort
 *     .entry-type-chips  Knowledge · All · per-type chips (with counts)
 *     .entry-compare-tray  (when pins exist)
 *     .entries-split     list (left) | reader / compare / hint (right)
 *
 * Mobile shows one pane at a time: the list, or the selected entry
 * with a Back button.
 *
 * Keyboard (k9s parity): j/k or arrows move the list selection,
 * `c` pins the selected entry for compare, `/` focuses search,
 * Esc clears the search.
 */
import { useEffect, useMemo, useRef, useState } from "react";
import { ErrorState } from "../common/ErrorState";
import { Loading } from "../common/Loading";
import { EntryReader } from "./EntryReader";
import { EntryCompare } from "./EntryCompare";
import { useProjects } from "../../hooks/useProjects";
import { useIsMobile } from "../../hooks/useIsMobile";
import {
  useBrainStats,
  useEntryList,
  useEntrySearch,
} from "../../hooks/useEntries";
import { useEntriesStore } from "../../store/entries";
import { relativeTime } from "../../lib/format";
import {
  ALL_ENTRY_TYPES,
  KNOWLEDGE_TYPES,
  entryBasename,
  entryProject,
  excerptOf,
} from "../../lib/entries";
import type { SearchStrategy } from "../../lib/types";

const STATUS_OPTIONS = [
  "draft",
  "pending",
  "active",
  "in_progress",
  "blocked",
  "cancelled",
  "completed",
  "validated",
  "superseded",
  "archived",
];

const SORT_OPTIONS: Array<{ value: string; label: string }> = [
  { value: "modified:desc", label: "Recently updated" },
  { value: "modified:asc", label: "Oldest updated" },
  { value: "created:desc", label: "Recently created" },
  { value: "created:asc", label: "Oldest created" },
  { value: "title:asc", label: "Title A→Z" },
];

interface RowItem {
  path: string;
  title: string;
  type: string;
  sub: string;
  subTitle?: string;
}

export function EntriesBrowser(): JSX.Element {
  const mobile = useIsMobile();

  const typeFilter = useEntriesStore((s) => s.typeFilter);
  const projectFilter = useEntriesStore((s) => s.projectFilter);
  const statusFilter = useEntriesStore((s) => s.statusFilter);
  const sortBy = useEntriesStore((s) => s.sortBy);
  const sortOrder = useEntriesStore((s) => s.sortOrder);
  const query = useEntriesStore((s) => s.query);
  const strategy = useEntriesStore((s) => s.strategy);
  const selectedPath = useEntriesStore((s) => s.selectedPath);
  const comparePins = useEntriesStore((s) => s.comparePins);
  const compareOpen = useEntriesStore((s) => s.compareOpen);

  const setTypeFilter = useEntriesStore((s) => s.setTypeFilter);
  const setProjectFilter = useEntriesStore((s) => s.setProjectFilter);
  const setStatusFilter = useEntriesStore((s) => s.setStatusFilter);
  const setSort = useEntriesStore((s) => s.setSort);
  const setQuery = useEntriesStore((s) => s.setQuery);
  const setStrategy = useEntriesStore((s) => s.setStrategy);
  const selectEntry = useEntriesStore((s) => s.selectEntry);
  const canonicalizeRef = useEntriesStore((s) => s.canonicalizeRef);
  const togglePin = useEntriesStore((s) => s.togglePin);
  const clearPins = useEntriesStore((s) => s.clearPins);
  const setCompareOpen = useEntriesStore((s) => s.setCompareOpen);

  const { data: projects } = useProjects();
  const { stats } = useBrainStats(projectFilter);

  const filters = { typeFilter, projectFilter, statusFilter, sortBy, sortOrder };
  const list = useEntryList(filters);
  const searchRes = useEntrySearch(query, strategy, filters);
  const searching = searchRes.enabled;

  // Debounced search input.
  const [inputValue, setInputValue] = useState(query);
  const searchRef = useRef<HTMLInputElement | null>(null);
  const listRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    const t = window.setTimeout(() => setQuery(inputValue), 250);
    return () => window.clearTimeout(t);
  }, [inputValue, setQuery]);

  const rows: RowItem[] = useMemo(() => {
    if (searching) {
      return searchRes.results.map((r) => ({
        path: r.path,
        title: r.title || entryBasename(r.path),
        type: r.type,
        sub: (r.snippet || "").replace(/\s+/g, " ").trim(),
        subTitle: r.path,
      }));
    }
    return list.entries.map((e) => ({
      path: e.path,
      title: e.title || entryBasename(e.path),
      type: e.type,
      sub: [entryProject(e), relativeTime(e.modified)]
        .filter(Boolean)
        .join(" · "),
      subTitle: excerptOf(e.content || ""),
    }));
  }, [searching, searchRes.results, list.entries]);

  const moveSelection = (dir: 1 | -1) => {
    if (rows.length === 0) return;
    const idx = rows.findIndex((r) => r.path === selectedPath);
    const next = idx === -1 ? (dir === 1 ? 0 : rows.length - 1)
      : Math.min(rows.length - 1, Math.max(0, idx + dir));
    const row = rows[next];
    if (!row) return;
    selectEntry(row.path);
    listRef.current
      ?.querySelector(`[data-path="${CSS.escape(row.path)}"]`)
      ?.scrollIntoView({ block: "nearest" });
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    // Leave shortcuts with modifiers to the browser (Cmd+C copy) and to
    // useGlobalKeyboard (⌘K, ⌘1-3, ⌘/).
    if (e.metaKey || e.ctrlKey || e.altKey) return;
    const t = e.target as HTMLElement;
    const tag = t.tagName;
    if (
      tag === "INPUT" ||
      tag === "TEXTAREA" ||
      tag === "SELECT" ||
      t.isContentEditable
    ) {
      if (e.key === "Escape" && tag === "INPUT") {
        setInputValue("");
        (t as HTMLInputElement).blur();
      }
      return;
    }
    if (e.key === "/") {
      e.preventDefault();
      searchRef.current?.focus();
    } else if (e.key === "j" || e.key === "ArrowDown") {
      e.preventDefault();
      moveSelection(1);
    } else if (e.key === "k" || e.key === "ArrowUp") {
      e.preventDefault();
      moveSelection(-1);
    } else if (e.key === "c" && selectedPath) {
      e.preventDefault();
      togglePin(selectedPath);
    }
  };

  const compareReady =
    comparePins.length === 2 && comparePins[0] !== comparePins[1];
  const showCompare = compareOpen && compareReady;

  const detail = showCompare ? (
    <EntryCompare
      aPath={comparePins[0]}
      bPath={comparePins[1]}
      onOpenEntry={selectEntry}
    />
  ) : selectedPath ? (
    <EntryReader
      path={selectedPath}
      onOpenEntry={selectEntry}
      onCanonicalPath={(p) => canonicalizeRef(selectedPath, p)}
    />
  ) : (
    <div className="entries-hint">
      <div className="entries-hint-title">Brain entries</div>
      <div>
        Select an entry to read it. <b>j/k</b> navigate · <b>/</b> search ·{" "}
        <b>c</b> pin for compare.
      </div>
    </div>
  );

  const listPane = (
    <div className="entries-list" ref={listRef}>
      {searching && searchRes.searching && rows.length === 0 ? (
        <Loading label="Searching…" />
      ) : list.error && !searching ? (
        <ErrorState error={list.error} title="Failed to list entries" />
      ) : rows.length === 0 ? (
        <div className="entries-empty">
          {searching
            ? "No matches."
            : list.loading
              ? ""
              : "No entries for this filter."}
          {!searching && list.loading && <Loading label="Loading entries…" />}
        </div>
      ) : (
        rows.map((r) => (
          <div
            key={r.path}
            data-path={r.path}
            className={
              "entry-row" +
              (r.path === selectedPath ? " selected" : "") +
              (comparePins.includes(r.path) ? " pinned" : "")
            }
            onClick={() => selectEntry(r.path)}
            title={r.subTitle || r.path}
          >
            <div className="entry-row-top">
              <span className="entry-type">{r.type}</span>
              <span className="entry-row-title">{r.title}</span>
              <button
                className={
                  "entry-pin" +
                  (comparePins.includes(r.path) ? " active" : "")
                }
                title={
                  comparePins.includes(r.path)
                    ? "Unpin from compare"
                    : "Pin for compare"
                }
                onClick={(e) => {
                  e.stopPropagation();
                  togglePin(r.path);
                }}
              >
                ⇄
              </button>
            </div>
            {r.sub && <div className="entry-row-sub">{r.sub}</div>}
          </div>
        ))
      )}
    </div>
  );

  // Mobile: one pane at a time.
  const showDetailOnly = mobile && (selectedPath !== null || showCompare);

  return (
    <div className="entries-pane" tabIndex={0} onKeyDown={onKeyDown}>
      <div className="entries-toolbar">
        {showDetailOnly ? (
          <button
            className="entry-act"
            onClick={() => {
              if (showCompare) setCompareOpen(false);
              else selectEntry(null);
            }}
          >
            ‹ Back
          </button>
        ) : (
          <>
            <input
              ref={searchRef}
              type="search"
              placeholder="Search entries…  ( / )"
              value={inputValue}
              onChange={(e) => setInputValue(e.target.value)}
            />
            {searching && (
              <select
                value={strategy}
                title="Search strategy"
                onChange={(e) =>
                  setStrategy(e.target.value as SearchStrategy)
                }
              >
                <option value="fts">text</option>
                <option value="semantic">semantic</option>
                <option value="hybrid">hybrid</option>
              </select>
            )}
            <select
              value={projectFilter}
              title="Project"
              onChange={(e) => setProjectFilter(e.target.value)}
            >
              <option value="">all projects</option>
              <option value="global">global</option>
              {(projects ?? []).map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </select>
            <select
              value={statusFilter}
              title="Status"
              onChange={(e) => setStatusFilter(e.target.value)}
            >
              <option value="">any status</option>
              {STATUS_OPTIONS.map((s) => (
                <option key={s} value={s}>
                  {s.replace(/_/g, " ")}
                </option>
              ))}
            </select>
            <select
              value={`${sortBy}:${sortOrder}`}
              title="Sort"
              onChange={(e) => {
                const [by, order] = e.target.value.split(":");
                setSort(
                  by as typeof sortBy,
                  order as typeof sortOrder,
                );
              }}
            >
              {SORT_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
          </>
        )}
      </div>

      {!showDetailOnly && (
        <div className="entry-type-chips">
          {/* Plain buttons, not <Chip> — Chip's clickable branch hardcodes
              inline border/background that would defeat the .active CSS. */}
          <button
            type="button"
            className={`chip mini${typeFilter === "knowledge" ? " active" : ""}`}
            onClick={() => setTypeFilter("knowledge")}
            title="Human-readable knowledge: summaries, walkthroughs, reports, plans…"
          >
            knowledge
          </button>
          <button
            type="button"
            className={`chip mini${typeFilter === "all" ? " active" : ""}`}
            onClick={() => setTypeFilter("all")}
          >
            all{stats ? ` ${stats.totalEntries}` : ""}
          </button>
          {ALL_ENTRY_TYPES.map((t) => {
            const count = stats?.byType?.[t] ?? 0;
            const isKnowledge = (KNOWLEDGE_TYPES as readonly string[]).includes(
              t,
            );
            if (!isKnowledge && count === 0) return null;
            return (
              <button
                key={t}
                type="button"
                className={`chip mini${typeFilter === t ? " active" : ""}`}
                onClick={() => setTypeFilter(t)}
              >
                {t}
                {stats ? ` ${count}` : ""}
              </button>
            );
          })}
        </div>
      )}

      {comparePins.length > 0 && !showCompare && !showDetailOnly && (
        <div className="entry-compare-tray">
          <span className="entry-compare-tray-label">Compare:</span>
          {comparePins.map((p) => (
            <span
              key={p}
              className="entry-compare-tray-pin"
              title={`${p} — click to unpin`}
              onClick={() => togglePin(p)}
            >
              {entryBasename(p)} ✕
            </span>
          ))}
          {!compareReady && (
            <span className="entry-compare-tray-hint">pin one more…</span>
          )}
          <button
            className="entry-act"
            disabled={!compareReady}
            onClick={() => setCompareOpen(true)}
          >
            ⇄ Compare
          </button>
          <button className="entry-act" onClick={clearPins}>
            Clear
          </button>
        </div>
      )}

      {showDetailOnly ? (
        <div className="entries-detail">{detail}</div>
      ) : (
        <div className="entries-split">
          {listPane}
          {!mobile && <div className="entries-detail">{detail}</div>}
        </div>
      )}
    </div>
  );
}
