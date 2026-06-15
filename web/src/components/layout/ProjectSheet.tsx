import { useEffect, useMemo, useRef, useState } from "react";
import { useProjects } from "../../hooks/useProjects";
import { useIsMobile } from "../../hooks/useIsMobile";
import { ALL_PROJECTS, useUI } from "../../store/ui";
import { BottomSheet } from "./BottomSheet";
import { Modal } from "../common/Modal";

function shortName(id: string): string {
  if (id === ALL_PROJECTS) return "All projects";
  return id.split(/[/\\]/).pop() || id;
}

// fuzzyScore scores `query` against `text` as a subsequence match, or returns
// null if it doesn't match at all. Higher is better: contiguous runs,
// word-boundary hits, and prefix matches score more, shorter names break ties.
function fuzzyScore(text: string, query: string): number | null {
  if (!query) return 0;
  const t = text.toLowerCase();
  const q = query.toLowerCase();
  let ti = 0;
  let score = 0;
  let run = 0;
  let firstIdx = -1;
  for (const ch of q) {
    const idx = t.indexOf(ch, ti);
    if (idx === -1) return null;
    if (firstIdx === -1) firstIdx = idx;
    if (idx === ti) {
      run += 1;
      score += 3 + run; // reward contiguous matches
    } else {
      run = 0;
      score += 1;
      const prev = t[idx - 1];
      if (prev === "-" || prev === "_" || prev === "/" || prev === " ") score += 2; // word boundary
    }
    ti = idx + 1;
  }
  if (firstIdx === 0) score += 5; // prefix bonus
  score -= t.length * 0.05; // prefer shorter names
  return score;
}

// Searchable project picker. Opened by the status-bar project name or the
// Cmd/Ctrl+K shortcut. Type to fuzzy-filter; the closest match is highlighted,
// ↑/↓ moves the selection, Enter accepts, Esc closes. Bottom sheet on mobile,
// centered modal on desktop.
export function ProjectSheet() {
  const open = useUI((s) => s.projectSheetOpen);
  const setOpen = useUI((s) => s.setProjectSheetOpen);
  const active = useUI((s) => s.activeProject);
  const setActiveProject = useUI((s) => s.setActiveProject);
  const isMobile = useIsMobile();
  const projectsQ = useProjects();
  const [q, setQ] = useState("");
  const [cursor, setCursor] = useState(0);
  const listRef = useRef<HTMLDivElement>(null);

  const all = useMemo(
    () => [ALL_PROJECTS, ...(projectsQ.data ?? [])],
    [projectsQ.data],
  );

  const filtered = useMemo(() => {
    const needle = q.trim();
    if (!needle) return all;
    return all
      .map((p) => ({ p, s: fuzzyScore(shortName(p), needle) }))
      .filter((x): x is { p: string; s: number } => x.s !== null)
      .sort((a, b) => b.s - a.s)
      .map((x) => x.p);
  }, [all, q]);

  // Highlight the best match (top of the sorted list) whenever the query changes.
  useEffect(() => {
    setCursor(0);
  }, [q]);

  // Keep the highlighted row scrolled into view.
  useEffect(() => {
    listRef.current
      ?.querySelector<HTMLElement>('[data-cursor="1"]')
      ?.scrollIntoView({ block: "nearest" });
  }, [cursor, filtered.length]);

  if (!open) return null;

  function close() {
    setOpen(false);
    setQ("");
    setCursor(0);
  }
  function pick(p?: string) {
    if (!p) return;
    setActiveProject(p);
    close();
  }

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setCursor((c) => Math.min(c + 1, filtered.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setCursor((c) => Math.max(c - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      pick(filtered[cursor]);
    } else if (e.key === "Escape") {
      e.preventDefault();
      close();
    }
  }

  const body = (
    <div className="proj-picker">
      <input
        className="proj-search"
        autoFocus
        placeholder="Search projects…"
        value={q}
        onChange={(e) => setQ(e.target.value)}
        onKeyDown={onKeyDown}
      />
      <div className="proj-list" ref={listRef}>
        {filtered.map((p, i) => (
          <button
            key={p}
            className={`proj-item ${i === cursor ? "cursor" : ""} ${p === active ? "on" : ""}`}
            data-cursor={i === cursor ? "1" : undefined}
            onClick={() => pick(p)}
            onMouseMove={() => setCursor(i)}
          >
            <span className="proj-dot">{p === active ? "●" : "○"}</span>
            <span className="truncate">{shortName(p)}</span>
          </button>
        ))}
        {filtered.length === 0 && (
          <div className="faint" style={{ padding: 10 }}>
            No match for “{q}”.
          </div>
        )}
        {projectsQ.isLoading && (
          <div className="faint" style={{ padding: 10 }}>
            Loading projects…
          </div>
        )}
      </div>
    </div>
  );

  return isMobile ? (
    <BottomSheet title="Project" onClose={close}>
      {body}
    </BottomSheet>
  ) : (
    <Modal title="Switch project" onClose={close}>
      {body}
    </Modal>
  );
}
