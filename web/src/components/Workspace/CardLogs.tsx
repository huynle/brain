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
import { toPlainText } from "../../lib/ansi";
import { clockTime } from "../../lib/format";
import { TerminalText } from "../common/TerminalText";

export interface CardLogsProps {
  projectId: string;
}

const MAX_LOG_LINES = 40;

/**
 * Classify from PLAIN text only. Fed raw terminal output, the substring
 * search matches inside escape sequences instead of content — an SGR
 * "OK" is not a success and a colourised word can hide from the match.
 */
function levelClass(line: string): "err" | "ok" | "wrn" | "" {
  const upper = toPlainText(line).toUpperCase();
  if (upper.includes("ERROR") || upper.includes("FATAL")) return "err";
  if (upper.includes("WARN")) return "wrn";
  if (upper.includes("OK") || upper.includes("SUCCESS")) return "ok";
  return "";
}

function levelLabel(cls: string): string {
  if (cls === "err") return "ERR";
  if (cls === "wrn") return "WARN";
  if (cls === "ok") return "OK";
  return "INFO";
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
          const ts = clockTime(r.line.timestamp);
          return (
            <div
              key={`${r.seq}-${idx}`}
              className={`l ${cls}${idx === 0 ? " new" : ""}`}
            >
              <span className="ts">{ts}</span>
              <span className="lvl">
                {levelLabel(cls) ||
                  r.line.level ||
                  "INFO"}
              </span>
              <span className="msg">
                <TerminalText text={r.line.content} />
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
