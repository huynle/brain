import { lazy, Suspense, useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useUI, ALL_PROJECTS } from "../store/ui";
import { useNav } from "../store/nav";
import { useViewKeyboard, handleListNavKey } from "../lib/keyboard";
import { embedBackfill, listEntries, search } from "../lib/api";
import { Pill } from "../components/common/Badge";
import { EmptyState, ErrorState, Loading } from "../components/common/states";
import { EntryView } from "./brain/EntryView";
import { relativeTime } from "../lib/format";

const ComposeModal = lazy(() =>
  import("../components/compose/ComposeModal").then((m) => ({
    default: m.ComposeModal,
  })),
);

export function BrainView() {
  const activeProject = useUI((s) => s.activeProject);
  const toast = useUI((s) => s.toast);
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
  const [openPath, setOpenPath] = useState<string | null>(null);
  const [composing, setComposing] = useState(false);

  const browseQ = useQuery({
    queryKey: ["entries", project],
    queryFn: () => listEntries({ project, limit: 200 }),
    enabled: query.trim() === "",
  });

  const searchQ = useQuery({
    queryKey: ["search", project, query],
    queryFn: () =>
      search({ query, project, global: !project, limit: 50 }),
    enabled: query.trim() !== "",
  });

  const searching = query.trim() !== "";

  const items = useMemo<{ path: string }[]>(
    () =>
      searching
        ? (searchQ.data?.results ?? [])
        : (browseQ.data?.entries ?? []),
    [searching, searchQ.data, browseQ.data],
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
        case "Enter":
        case "e":
          if (cur) setOpenPath(cur.path);
          return true;
        case "/":
          searchRef.current?.focus();
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
    const el = document.querySelector<HTMLElement>('[data-cursor="1"]');
    el?.scrollIntoView({ block: "nearest" });
  }, [cursor]);

  return (
    <div>
      <form
        className="search-bar"
        onSubmit={(e) => {
          e.preventDefault();
          setQuery(input);
          setCursor(scope, 0);
        }}
      >
        <input
          ref={searchRef}
          placeholder="Search the brain…"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") e.currentTarget.blur();
          }}
        />
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
          onClick={() => setComposing(true)}
          title="New entry"
        >
          + New
        </button>
      </form>

      <div className="section-pad">
        {searching ? (
          searchQ.isLoading ? (
            <Loading label="Searching…" />
          ) : searchQ.error ? (
            <ErrorState error={searchQ.error} onRetry={() => void searchQ.refetch()} />
          ) : !searchQ.data?.results.length ? (
            <EmptyState glyph="◇" title="No results" hint={`Nothing matched “${query}”.`} />
          ) : (
            searchQ.data.results.map((r, i) => (
              <EntryRow
                key={r.id}
                title={r.title}
                type={r.type}
                meta={r.snippet}
                badge={r.match_source}
                cursored={i === cursor}
                onClick={() => setOpenPath(r.path)}
              />
            ))
          )
        ) : browseQ.isLoading ? (
          <Loading label="Loading entries…" />
        ) : browseQ.error ? (
          <ErrorState error={browseQ.error} onRetry={() => void browseQ.refetch()} />
        ) : !browseQ.data?.entries.length ? (
          <EmptyState glyph="◆" title="No entries" hint="This project's knowledge base is empty." />
        ) : (
          browseQ.data.entries.map((e, i) => (
            <EntryRow
              key={e.id}
              title={e.title}
              type={e.type}
              meta={e.modified ? `updated ${relativeTime(e.modified)}` : e.path}
              tags={e.tags}
              cursored={i === cursor}
              onClick={() => setOpenPath(e.path)}
            />
          ))
        )}
      </div>

      {openPath && (
        <EntryView path={openPath} onClose={() => setOpenPath(null)} />
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
    </div>
  );
}

function EntryRow({
  title,
  type,
  meta,
  badge,
  tags,
  cursored,
  onClick,
}: {
  title: string;
  type: string;
  meta?: string;
  badge?: string;
  tags?: string[];
  cursored?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      data-cursor={cursored ? "1" : undefined}
      className={cursored ? "kbd-cursor" : ""}
      style={{
        display: "block",
        width: "100%",
        textAlign: "left",
        background: "var(--bg-1)",
        border: "1px solid var(--border)",
        borderRadius: "var(--radius-sm)",
        padding: "0.6rem 0.7rem",
        marginBottom: "0.4rem",
      }}
    >
      <div className="row" style={{ gap: "0.5rem" }}>
        <span style={{ flex: 1, fontWeight: 500 }}>{title}</span>
        <Pill color="var(--purple)">{type}</Pill>
        {badge && <Pill className="faint">{badge}</Pill>}
      </div>
      {meta && (
        <div
          className="muted"
          style={{
            fontSize: 12.5,
            marginTop: "0.3rem",
            overflow: "hidden",
            textOverflow: "ellipsis",
            display: "-webkit-box",
            WebkitLineClamp: 2,
            WebkitBoxOrient: "vertical",
          }}
        >
          {meta}
        </div>
      )}
      {tags && tags.length > 0 && (
        <div className="row wrap" style={{ gap: "0.25rem", marginTop: "0.4rem" }}>
          {tags.slice(0, 5).map((t) => (
            <Pill key={t} color="var(--teal)">
              #{t}
            </Pill>
          ))}
        </div>
      )}
    </button>
  );
}
