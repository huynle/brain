/**
 * CardLogs — wireframe-parity port.
 *
 * Streaming logs mini-window for the project.
 * DOM:
 *   .log-mini
 *     .head (live-dot · title · pause btn)
 *     .body
 *       .l[.new|.err|.ok|.wrn] × N
 */
import { useMemo } from "react";
import { useLive } from "../../lib/sse";

export interface CardLogsProps {
  projectId: string;
}

const MAX_LOG_LINES = 40;

function levelClass(line: string): "err" | "ok" | "wrn" | "" {
  const upper = line.toUpperCase();
  if (upper.includes("ERROR") || upper.includes("FATAL")) return "err";
  if (upper.includes("WARN")) return "wrn";
  if (upper.includes("OK") || upper.includes("SUCCESS")) return "ok";
  return "";
}

function levelLabel(line: string, cls: string): string {
  if (cls === "err") return "ERR";
  if (cls === "wrn") return "WARN";
  if (cls === "ok") return "OK";
  return "INFO";
}

function formatTime(ts: number): string {
  try {
    const d = new Date(ts);
    return `${d.getHours().toString().padStart(2, "0")}:${d
      .getMinutes()
      .toString()
      .padStart(2, "0")}:${d.getSeconds().toString().padStart(2, "0")}`;
  } catch {
    return "";
  }
}

export function CardLogs({ projectId }: CardLogsProps): JSX.Element {
  const logs = useLive((s) => s.logs);
  const projectLogs = useMemo(
    () =>
      logs
        .filter((r) => r.projectId === projectId)
        .slice(-MAX_LOG_LINES)
        .reverse(),
    [logs, projectId],
  );

  return (
    <div className="log-mini">
      <div className="head">
        <span className="live-dot" />
        <span className="title">Runner logs · {projectId}</span>
      </div>
      <div className="body">
        {projectLogs.length === 0 && (
          <div style={{ color: "#4b545c", padding: 4, fontSize: 10.5 }}>
            No log lines yet.
          </div>
        )}
        {projectLogs.map((r, idx) => {
          const cls = levelClass(r.line.content ?? "");
          const ts = r.line.timestamp
            ? formatTime(new Date(r.line.timestamp).getTime())
            : "";
          return (
            <div
              key={`${r.seq}-${idx}`}
              className={`l ${cls}${idx === 0 ? " new" : ""}`}
            >
              <span className="ts">{ts}</span>
              <span className="lvl">
                {levelLabel(r.line.content ?? "", cls) ||
                  r.line.level ||
                  "INFO"}
              </span>
              <span className="msg">{r.line.content}</span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
