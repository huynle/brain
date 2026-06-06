import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useUI, ALL_PROJECTS } from "../store/ui";
import { listEntries, search } from "../lib/api";
import { Pill } from "../components/common/Badge";
import { EmptyState, ErrorState, Loading } from "../components/common/states";
import { EntryView } from "./brain/EntryView";
import { relativeTime } from "../lib/format";

export function BrainView() {
  const activeProject = useUI((s) => s.activeProject);
  const project = activeProject === ALL_PROJECTS ? undefined : activeProject;
  const [input, setInput] = useState("");
  const [query, setQuery] = useState("");
  const [openPath, setOpenPath] = useState<string | null>(null);

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

  return (
    <div>
      <form
        className="search-bar"
        onSubmit={(e) => {
          e.preventDefault();
          setQuery(input);
        }}
      >
        <input
          placeholder="Search the brain…"
          value={input}
          onChange={(e) => setInput(e.target.value)}
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
            searchQ.data.results.map((r) => (
              <EntryRow
                key={r.id}
                title={r.title}
                type={r.type}
                meta={r.snippet}
                badge={r.match_source}
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
          browseQ.data.entries.map((e) => (
            <EntryRow
              key={e.id}
              title={e.title}
              type={e.type}
              meta={e.modified ? `updated ${relativeTime(e.modified)}` : e.path}
              tags={e.tags}
              onClick={() => setOpenPath(e.path)}
            />
          ))
        )}
      </div>

      {openPath && (
        <EntryView path={openPath} onClose={() => setOpenPath(null)} />
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
  onClick,
}: {
  title: string;
  type: string;
  meta?: string;
  badge?: string;
  tags?: string[];
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
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
