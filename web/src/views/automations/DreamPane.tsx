import {
  forwardRef,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
} from "react";
import { useQuery } from "@tanstack/react-query";
import { getEntry, search } from "../../lib/api";
import { EmptyState, ErrorState, Loading } from "../../components/common/states";

export interface DreamHandle {
  focusSearch: () => void;
  next: () => void;
  prev: () => void;
  top: () => void;
  bottom: () => void;
}

/** The TUI fetches the latest type:dream entry per project and shows its body. */
async function fetchDream(project?: string): Promise<string> {
  const res = await search({
    query: "Project Dream",
    type: "dream",
    project,
    limit: 1,
  });
  if (!res.results.length) return "";
  const entry = await getEntry(res.results[0].path);
  return entry.content || "";
}

export const DreamPane = forwardRef<
  DreamHandle,
  { project?: string }
>(function DreamPane({ project }, ref) {
  const q = useQuery({
    queryKey: ["dream", project ?? "all"],
    queryFn: () => fetchDream(project),
    refetchInterval: 30_000,
  });

  const [query, setQuery] = useState("");
  const [current, setCurrent] = useState(0);
  const searchRef = useRef<HTMLInputElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);

  const content = q.data ?? "";

  // Split content into segments, marking matches.
  const segments = useMemo(() => {
    const ql = query.trim().toLowerCase();
    if (!ql) return [{ text: content, match: false }];
    const out: { text: string; match: boolean }[] = [];
    const lc = content.toLowerCase();
    let i = 0;
    while (i < content.length) {
      const idx = lc.indexOf(ql, i);
      if (idx === -1) {
        out.push({ text: content.slice(i), match: false });
        break;
      }
      if (idx > i) out.push({ text: content.slice(i, idx), match: false });
      out.push({ text: content.slice(idx, idx + ql.length), match: true });
      i = idx + ql.length;
    }
    return out;
  }, [content, query]);

  const matchCount = segments.filter((s) => s.match).length;

  function scrollToMatch(n: number) {
    const marks = scrollRef.current?.querySelectorAll("mark");
    if (!marks || !marks.length) return;
    const idx = ((n % marks.length) + marks.length) % marks.length;
    marks.forEach((m, i) => m.classList.toggle("current", i === idx));
    marks[idx]?.scrollIntoView({ block: "center" });
  }

  useImperativeHandle(ref, () => ({
    focusSearch: () => searchRef.current?.focus(),
    next: () => {
      setCurrent((c) => {
        const n = c + 1;
        scrollToMatch(n);
        return n;
      });
    },
    prev: () => {
      setCurrent((c) => {
        const n = c - 1;
        scrollToMatch(n);
        return n;
      });
    },
    top: () => {
      if (scrollRef.current) scrollRef.current.scrollTop = 0;
    },
    bottom: () => {
      if (scrollRef.current)
        scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    },
  }));

  if (q.isLoading) return <Loading label="Loading dream…" />;
  if (q.error) return <ErrorState error={q.error} onRetry={() => void q.refetch()} />;
  if (!content)
    return (
      <EmptyState
        glyph="☾"
        title="No dream yet"
        hint={
          project
            ? "No dream monitor output for this project."
            : "Pick a project tab to see its dream."
        }
      />
    );

  return (
    <div>
      <div className="search-bar">
        <input
          ref={searchRef}
          placeholder="Search dream… ( / , then n/N )"
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setCurrent(0);
          }}
          onKeyDown={(e) => {
            if (e.key === "Escape") e.currentTarget.blur();
            if (e.key === "Enter") {
              e.preventDefault();
              scrollToMatch(0);
              e.currentTarget.blur();
            }
          }}
        />
        {query && (
          <span className="pill faint" style={{ alignSelf: "center" }}>
            {matchCount ? `${((current % matchCount) + matchCount) % matchCount + 1}/${matchCount}` : "0"}
          </span>
        )}
      </div>
      <div
        ref={scrollRef}
        className="dream-content"
        style={{
          padding: "0.6rem 0.9rem",
          whiteSpace: "pre-wrap",
          fontFamily: "var(--mono)",
          fontSize: 13,
          lineHeight: 1.6,
          color: "var(--fg)",
        }}
      >
        {segments.map((s, i) =>
          s.match ? <mark key={i}>{s.text}</mark> : <span key={i}>{s.text}</span>,
        )}
      </div>
    </div>
  );
});
