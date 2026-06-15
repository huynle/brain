// Global Logs tab: a live tail of every HTTP request handled by the Brain
// server, annotated with who made it (runner / client / browser). Per-task and
// per-automation output lives in the Logs *pane* (z) inside those tabs instead.

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { getServerRequests } from "../lib/api";
import { useUI } from "../store/ui";
import { useViewKeyboard } from "../lib/keyboard";
import { clockTime } from "../lib/format";
import { EmptyState } from "../components/common/states";
import type { ServerRequest } from "../lib/types";

function statusColor(status: number): string {
  if (status >= 500) return "var(--red)";
  if (status >= 400) return "var(--yellow)";
  if (status >= 300) return "var(--cyan, var(--teal))";
  return "var(--green)";
}

function methodColor(method: string): string {
  switch (method) {
    case "GET":
      return "var(--blue)";
    case "POST":
      return "var(--green)";
    case "PATCH":
    case "PUT":
      return "var(--yellow)";
    case "DELETE":
      return "var(--red)";
    default:
      return "var(--fg-dim)";
  }
}

function actorLabel(r: ServerRequest): string {
  if (r.actor_name) return r.actor_name;
  if (r.actor_type && r.actor_type !== "anonymous") return r.actor_type;
  return "anon";
}

export function LogsView() {
  const logFilter = useUI((s) => s.logFilter);
  const setLogFilter = useUI((s) => s.setLogFilter);
  const [follow, setFollow] = useState(true);
  const scrollRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);

  const reqQ = useQuery({
    queryKey: ["server-requests"],
    queryFn: () => getServerRequests(0, 800),
    refetchInterval: 2_000,
  });

  const requests = useMemo(() => reqQ.data ?? [], [reqQ.data]);

  const filtered = useMemo(() => {
    const q = logFilter.trim().toLowerCase();
    if (!q) return requests;
    return requests.filter(
      (r) =>
        r.path.toLowerCase().includes(q) ||
        r.method.toLowerCase().includes(q) ||
        actorLabel(r).toLowerCase().includes(q) ||
        String(r.status).includes(q),
    );
  }, [requests, logFilter]);

  useEffect(() => {
    if (follow) bottomRef.current?.scrollIntoView({ block: "end" });
  }, [filtered, follow]);

  function onScroll() {
    const el = scrollRef.current;
    if (!el) return;
    setFollow(el.scrollHeight - el.scrollTop - el.clientHeight < 60);
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
          placeholder="Filter requests (path, method, actor, status)…"
          value={logFilter}
          onChange={(e) => setLogFilter(e.target.value)}
        />
        <span className="faint" style={{ fontSize: 11.5, whiteSpace: "nowrap" }}>
          {filtered.length} req{reqQ.error ? " · offline" : ""}
        </span>
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
          title="No requests yet"
          hint="Every request to the Brain server (runners, clients, browser) appears here in real time."
        />
      ) : (
        <div
          ref={scrollRef}
          onScroll={onScroll}
          style={{ padding: "0.4rem 0.6rem", fontFamily: "var(--mono)", fontSize: 12.5, lineHeight: 1.5 }}
        >
          {filtered.map((r) => (
            <div key={r.seq} className="req-line">
              <span className="req-time faint" style={{ flexShrink: 0 }}>{clockTime(new Date(r.time).toISOString())}</span>
              <span className="req-actor" title={`${r.actor_type}`}>{actorLabel(r)}</span>
              <span className="req-method" style={{ flexShrink: 0, width: 52, color: methodColor(r.method) }}>{r.method}</span>
              <span className="req-status" style={{ flexShrink: 0, width: 34, color: statusColor(r.status) }}>{r.status}</span>
              <span className="req-dur faint" style={{ flexShrink: 0, width: 56, textAlign: "right" }}>{r.duration_ms}ms</span>
              <span className="req-path" style={{ whiteSpace: "pre-wrap", wordBreak: "break-all" }}>{r.path}</span>
            </div>
          ))}
          <div ref={bottomRef} />
        </div>
      )}
    </div>
  );
}
