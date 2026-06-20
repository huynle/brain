import { lazy, Suspense, useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useUI, ALL_PROJECTS } from "../store/ui";
import { useNav } from "../store/nav";
import { useViewKeyboard, handleListNavKey } from "../lib/keyboard";
import { embedBackfill, listEntries, search } from "../lib/api";
import { Pill } from "../components/common/Badge";
import { EmptyState, ErrorState, Loading } from "../components/common/states";
import { ListDetail } from "../components/layout/ListDetail";
import { useIsMobile } from "../hooks/useIsMobile";
import { EntryView } from "./brain/EntryView";
import { relativeTime } from "../lib/format";
import type { BrainEntry, SearchResult, SearchStrategy } from "../lib/types";
import {
  deserializeHiddenEntryTypes,
  filterEntriesByHiddenTypes,
  serializeHiddenEntryTypes,
  toggleHiddenEntryType,
} from "./brain/entryFilters";

const STRATEGIES: { value: SearchStrategy; label: string }[] = [
  { value: "semantic", label: "Semantic" },
  { value: "fts", label: "Full-text" },
  { value: "hybrid", label: "Hybrid" },
];

const ComposeModal = lazy(() =>
  import("../components/compose/ComposeModal").then((m) => ({
    default: m.ComposeModal,
  })),
);

// CodeMirror is heavy — load the editor only when the user edits.
const EntryEditModal = lazy(() =>
  import("./brain/EntryEditModal").then((m) => ({ default: m.EntryEditModal })),
);

const HIDDEN_TYPES_KEY = "brain.brain_view.hidden_types";
const BROWSE_LIMIT = 1000;

function loadHiddenTypes(): Set<string> {
  try {
    return deserializeHiddenEntryTypes(localStorage.getItem(HIDDEN_TYPES_KEY));
  } catch {
    return new Set();
  }
}

function saveHiddenTypes(hiddenTypes: ReadonlySet<string>) {
  try {
    localStorage.setItem(HIDDEN_TYPES_KEY, serializeHiddenEntryTypes(hiddenTypes));
  } catch {
    // Ignore private-mode/quota errors; filters still work for this session.
  }
}

export function BrainView() {
  const activeProject = useUI((s) => s.activeProject);
  const toast = useUI((s) => s.toast);
  const toggleDetail = useUI((s) => s.toggleDetail);
  const toggleLogs = useUI((s) => s.toggleLogs);
  const openInspect = useUI((s) => s.openInspect);
  const isMobile = useIsMobile();
  const project = activeProject === ALL_PROJECTS ? undefined : activeProject;
  const qc = useQueryClient();

  function embed(opts: { project?: string; force?: boolean }, label: string) {
    toast(`${label}…`);
    embedBackfill(opts).then(
      () => toast(`${label} done`, "success"),
      (e) => toast(e instanceof Error ? e.message : "Embed failed", "error"),
    );
  }
  const [input, setInput] = useState("");
  const [query, setQuery] = useState("");
  const [searchOpen, setSearchOpen] = useState(false);
  const [strategy, setStrategy] = useState<SearchStrategy>("semantic");
  const [openPath, setOpenPath] = useState<string | null>(null);
  const [editPath, setEditPath] = useState<{ path: string; title?: string } | null>(null);
  const [composing, setComposing] = useState(false);
  const [hiddenTypes, setHiddenTypes] = useState<Set<string>>(loadHiddenTypes);

  const browseQ = useQuery({
    queryKey: ["entries", project],
    queryFn: () => listEntries({ project, limit: BROWSE_LIMIT }),
    enabled: query.trim() === "",
  });

  const searchQ = useQuery({
    queryKey: ["search", project, query, strategy],
    queryFn: () =>
      // No `global` flag: with a project we scope to it; without one (the All
      // tab) we search across everything. `global:true` would restrict to
      // global/-prefixed entries only.
      search({
        query,
        project,
        strategy,
        include: ["attachments"],
        limit: 50,
      }),
    enabled: query.trim() !== "",
  });

  const searching = query.trim() !== "";

  const rawItems = useMemo<Array<BrainEntry | SearchResult>>(
    () => (searching ? (searchQ.data?.results ?? []) : (browseQ.data?.entries ?? [])),
    [searching, searchQ.data, browseQ.data],
  );

  const entryTypes = useMemo(
    () => Array.from(new Set(rawItems.map((item) => item.type))).sort(),
    [rawItems],
  );


  const items = useMemo<Array<BrainEntry | SearchResult>>(
    () => filterEntriesByHiddenTypes(rawItems, hiddenTypes),
    [rawItems, hiddenTypes],
  );

  const scope = `brain:${project ?? "all"}`;
  const cursor = useNav((s) =>
    Math.min(s.cursor[scope] ?? 0, Math.max(0, items.length - 1)),
  );
  const setCursor = useNav((s) => s.setCursor);
  const searchRef = useRef<HTMLInputElement>(null);

  useViewKeyboard(
    (e) => {
      if (handleListNavKey(e, scope, items.length)) return true;
      const cur = items[cursor];
      switch (e.key) {
        case "T":
          toggleDetail();
          return true;
        case "z":
          toggleLogs();
          return true;
        case "Enter":
          if (cur) setOpenPath(cur.path);
          return true;
        case "e":
          if (cur) setEditPath({ path: cur.path, title: (cur as { title?: string }).title });
          return true;
        case "/":
          setSearchOpen(true);
          return true;
        case "n":
          setComposing(true);
          return true;
        case "b":
          embed({ project, force: false }, "Embedding project");
          return true;
        case "B":
          embed({ force: false }, "Embedding all");
          return true;
        case "F":
          embed({ project, force: true }, "Re-embedding project");
          return true;
        case "A":
          embed({ force: true }, "Re-embedding all");
          return true;
        default:
          return false;
      }
    },
    [items, cursor, scope, project],
  );

  useEffect(() => {
    if (searchOpen) searchRef.current?.focus();
  }, [searchOpen]);

  useEffect(() => {
    const el = document.querySelector<HTMLElement>('[data-cursor="1"]');
    el?.scrollIntoView({ block: "nearest" });
  }, [cursor]);

  // Row tap: on mobile open the Detail/Logs sheet; on desktop open the entry
  // editor (the existing behavior).
  function openEntry(item: { path: string; id?: string; type?: string; project_id?: string; title?: string }) {
    if (isMobile) {
      openInspect({
        path: item.path,
        title: item.title,
        taskId: item.type === "task" ? item.id : undefined,
        projectId: item.project_id,
      });
    } else {
      setOpenPath(item.path);
    }
  }

  const selItem = items[cursor] as { path: string; id?: string; type?: string; project_id?: string } | undefined;
  const selectedPath = selItem?.path ?? null;
  const logTarget =
    selItem?.type === "task" && selItem.id
      ? { taskId: selItem.id, projectId: selItem.project_id }
      : null;

  return (
    <ListDetail detailPath={selectedPath} logTarget={logTarget}>
      <form
        className="search-layer"
        style={{ display: searchOpen ? undefined : "none" }}
        onMouseDown={(e) => { if (e.target === e.currentTarget) setSearchOpen(false); }}

        onSubmit={(e) => {
          e.preventDefault();
          setQuery(input);
          setCursor(scope, 0);
        }}
      >
        <div className="search-popup">
          <span className="search-prompt">/</span>
        <input
          ref={searchRef}
          placeholder="Search the brain…"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") setSearchOpen(false);
            if (e.key === "Enter") setSearchOpen(false);
          }}
        />
        <select
          className="strategy-select"
          value={strategy}
          onChange={(e) => setStrategy(e.target.value as SearchStrategy)}
          title="Search strategy"
          aria-label="Search strategy"
        >
          {STRATEGIES.map((s) => (
            <option key={s.value} value={s.value}>
              {s.label}
            </option>
          ))}
        </select>
        {searching ? (
          <button
            className="btn sm"
            type="button"
            onClick={() => {
              setInput("");
              setQuery("");
            }}
          >
            Clear
          </button>
        ) : (
          <button className="btn sm primary" type="submit" disabled={!input.trim()}>
            Search
          </button>
        )}
        <button
          className="btn sm"
          type="button"
          onClick={() => { setSearchOpen(false); setComposing(true); }}
          title="New entry"
        >
          +
        </button>
        </div>
      </form>

      {entryTypes.length > 0 && (
        <div
          className="search-meta"
          style={{ gap: 8, flexWrap: "wrap" }}
          aria-label="Brain entry type filters"
        >
          <span className="faint" style={{ fontSize: 11 }}>
            Hide types:
          </span>
          {entryTypes.map((type) => {
            const hidden = hiddenTypes.has(type);
            return (
              <button
                key={type}
                type="button"
                className={`btn sm ${hidden ? "" : "primary"}`}
                onClick={() => {
                  setHiddenTypes((prev) => {
                    const next = toggleHiddenEntryType(prev, type);
                    saveHiddenTypes(next);
                    return next;
                  });
                  setCursor(scope, 0);
                }}
                aria-pressed={!hidden}
                title={hidden ? `Show ${type} entries` : `Hide ${type} entries`}
              >
                {hidden ? "⊘" : "✓"} {type}
              </button>
            );
          })}
          {hiddenTypes.size > 0 && (
            <button
              className="btn sm"
              type="button"
              onClick={() => {
                const next = new Set<string>();
                saveHiddenTypes(next);
                setHiddenTypes(next);
                setCursor(scope, 0);
              }}
            >
              Show all
            </button>
          )}
          <span className="faint" style={{ marginLeft: "auto", fontSize: 11 }}>
            {items.length}/{rawItems.length} visible{browseQ.data?.total && browseQ.data.total > rawItems.length ? ` of ${browseQ.data.total}` : ""}
          </span>
        </div>
      )}

      {searching && !searchQ.isLoading && !searchQ.error && (
        <div className="search-meta">
          <Pill color="var(--purple)">{strategy}</Pill>
          <span className="faint">
            {searchQ.data?.results.length ?? 0} result
            {(searchQ.data?.results.length ?? 0) === 1 ? "" : "s"} for “{query}”
          </span>
          {strategy === "semantic" && (
            <span className="faint" style={{ marginLeft: "auto", fontSize: 11 }}>
              falls back to full-text if embeddings are off
            </span>
          )}
        </div>
      )}

      <div>
        {searching ? (
          searchQ.isLoading ? (
            <Loading label="Searching…" />
          ) : searchQ.error ? (
            <ErrorState error={searchQ.error} onRetry={() => void searchQ.refetch()} />
          ) : rawItems.length === 0 ? (
            <EmptyState glyph="◇" title="No results" hint={`Nothing matched “${query}”.`} />
          ) : items.length === 0 ? (
            <EmptyState glyph="⊘" title="All matching entries are hidden" hint="Use the type filters above to show more entries." />
          ) : (
            items.map((item, i, arr) => {
              const r = item as SearchResult;
              return (
              <EntryRow
                key={r.id}
                title={r.title}
                type={r.type}
                meta={r.snippet}
                cursored={i === cursor}
                last={i === arr.length - 1}
                onClick={() => openEntry(r)}
              />
              );
            })
          )
        ) : browseQ.isLoading ? (
          <Loading label="Loading entries…" />
        ) : browseQ.error ? (
          <ErrorState error={browseQ.error} onRetry={() => void browseQ.refetch()} />
        ) : rawItems.length === 0 ? (
          <EmptyState glyph="◆" title="No entries" hint="This project's knowledge base is empty." />
        ) : items.length === 0 ? (
          <EmptyState glyph="⊘" title="All entries are hidden" hint="Use the type filters above to show more entries." />
        ) : (
          items.map((item, i, arr) => {
            const e = item as BrainEntry;
            return (
            <EntryRow
              key={e.id}
              title={e.title}
              type={e.type}
              meta={e.modified ? relativeTime(e.modified) : e.path}
              cursored={i === cursor}
              last={i === arr.length - 1}
              onClick={() => openEntry(e)}
            />
            );
          })
        )}
      </div>

      {openPath && (
        <EntryView path={openPath} onClose={() => setOpenPath(null)} />
      )}
      {editPath && (
        <Suspense fallback={null}>
          <EntryEditModal
            path={editPath.path}
            title={editPath.title}
            onClose={() => setEditPath(null)}
            onSaved={() => {
              void qc.invalidateQueries({ queryKey: ["entries"] });
              void qc.invalidateQueries({ queryKey: ["search"] });
              void qc.invalidateQueries({ queryKey: ["entry", editPath.path] });
              void qc.invalidateQueries({ queryKey: ["entry-detail", editPath.path] });
            }}
          />
        </Suspense>
      )}
      {composing && (
        <Suspense fallback={null}>
          <ComposeModal
            kind="note"
            onClose={() => setComposing(false)}
            onCreated={(path) => {
              void qc.invalidateQueries({ queryKey: ["entries"] });
              setOpenPath(path);
            }}
          />
        </Suspense>
      )}
    </ListDetail>
  );
}

const TYPE_GLYPH: Record<string, { ch: string; color: string }> = {
  note: { ch: "◆", color: "var(--blue)" },
  task: { ch: "▸", color: "var(--green)" },
  automation: { ch: "⟳", color: "var(--purple)" },
  dream: { ch: "☾", color: "var(--cyan)" },
  goal: { ch: "◎", color: "var(--purple)" },
};

function EntryRow({
  title,
  type,
  meta,
  cursored,
  last,
  onClick,
}: {
  title: string;
  type: string;
  meta?: string;
  cursored?: boolean;
  last?: boolean;
  onClick: () => void;
}) {
  const g = TYPE_GLYPH[type] ?? { ch: "◇", color: "var(--fg-dim)" };
  return (
    <div
      className={`tree-row ${cursored ? "cursor" : ""}`}
      data-cursor={cursored ? "1" : undefined}
      onClick={onClick}
    >
      <span className="connector">{last ? "└─ " : "├─ "}</span>
      <span className="glyph" style={{ color: cursored ? undefined : g.color }}>
        {g.ch}
      </span>
      <span className="title truncate">{title}</span>
      <span className="suffix faint">{type}</span>
      {meta && <span className="suffix faint">{meta}</span>}
    </div>
  );
}
