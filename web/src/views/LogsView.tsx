import { useEffect, useMemo, useRef, useState } from "react";
import { useLive } from "../lib/sse";
import { useUI, ALL_PROJECTS } from "../store/ui";
import { useViewKeyboard } from "../lib/keyboard";
import { clockTime, logLevelColor } from "../lib/format";
import { EmptyState } from "../components/common/states";

export function LogsView() {
  const logs = useLive((s) => s.logs);
  const activeProject = useUI((s) => s.activeProject);
  const logFilter = useUI((s) => s.logFilter);
  const setLogFilter = useUI((s) => s.setLogFilter);
  const [follow, setFollow] = useState(true);
  const scrollRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);

  const filtered = useMemo(() => {
    const q = logFilter.trim().toLowerCase();
    return logs.filter((r) => {
      if (activeProject !== ALL_PROJECTS && r.projectId !== activeProject)
        return false;
      if (!q) return true;
      return (
        r.taskId.toLowerCase().includes(q) ||
        r.line.content.toLowerCase().includes(q)
      );
    });
  }, [logs, activeProject, logFilter]);

  useEffect(() => {
    if (follow) bottomRef.current?.scrollIntoView({ block: "end" });
  }, [filtered, follow]);

  function onScroll() {
    const el = scrollRef.current;
    if (!el) return;
    const atBottom =
      el.scrollHeight - el.scrollTop - el.clientHeight < 60;
    setFollow(atBottom);
  }

  useViewKeyboard(
    (e) => {
      const el = scrollRef.current;
      switch (e.key) {
        case "j":
        case "ArrowDown":
          if (el) el.scrollTop += 60;
          setFollow(false);
          return true;
        case "k":
        case "ArrowUp":
          if (el) el.scrollTop -= 60;
          setFollow(false);
          return true;
        case "g":
          if (el) el.scrollTop = 0;
          setFollow(false);
          return true;
        case "G":
          setFollow(true);
          bottomRef.current?.scrollIntoView({ block: "end" });
          return true;
        case "f":
          setFollow((v) => {
            const nv = !v;
            if (nv) bottomRef.current?.scrollIntoView({ block: "end" });
            return nv;
          });
          return true;
        default:
          return false;
      }
    },
    [],
  );

  return (
    <div>
      <div className="search-bar">
        <input
          placeholder="Filter logs (task id or text)…"
          value={logFilter}
          onChange={(e) => setLogFilter(e.target.value)}
        />
        <button
          className={`btn sm ${follow ? "primary" : ""}`}
          onClick={() => {
            setFollow(true);
            bottomRef.current?.scrollIntoView({ block: "end" });
          }}
          title="Auto-scroll to newest"
        >
          {follow ? "Live" : "Follow"}
        </button>
      </div>

      {filtered.length === 0 ? (
        <EmptyState
          glyph="▤"
          title="No logs yet"
          hint="Runner output appears here in real time as tasks execute."
        />
      ) : (
        <div
          ref={scrollRef}
          onScroll={onScroll}
          style={{
            padding: "0.4rem 0.6rem",
            fontFamily: "var(--mono)",
            fontSize: 12.5,
            lineHeight: 1.5,
          }}
        >
          {filtered.map((r) => (
            <div
              key={r.seq}
              style={{ display: "flex", gap: "0.5rem", padding: "1px 0" }}
            >
              <span className="faint" style={{ flexShrink: 0 }}>
                {clockTime(r.line.timestamp)}
              </span>
              <span
                style={{
                  flexShrink: 0,
                  width: 42,
                  color: logLevelColor(r.line.level),
                  textTransform: "uppercase",
                  fontSize: 10.5,
                  paddingTop: 1,
                }}
              >
                {r.line.level}
              </span>
              <span style={{ whiteSpace: "pre-wrap", wordBreak: "break-word" }}>
                {r.line.content}
              </span>
            </div>
          ))}
          <div ref={bottomRef} />
        </div>
      )}
    </div>
  );
}
