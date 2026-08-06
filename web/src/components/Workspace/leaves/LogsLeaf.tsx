/**
 * panes-v2 LogsLeaf.
 *
 * Renders the recent log stream for a task (or a whole project if
 * only `projectId` is provided). Reuses the same buffer that
 * `CardLogs` reads from, but with a taller viewport suitable for
 * a full pane.
 *
 * target shape: { projectId: string; taskId?: string }
 */
import { useMemo } from "react";
import { useLive } from "../../../lib/sse";

/** Max rows kept in the on-screen tail. Higher than CardLogs's 12
 *  because a focus pane has real vertical room to play with. */
const TAIL_ROWS = 500;

export function LogsLeaf({
  target,
}: {
  target: Record<string, unknown>;
}): JSX.Element {
  const projectId = (target.projectId as string | undefined) ?? "";
  const taskId = target.taskId as string | undefined;

  const allLogs = useLive((s) => s.logs);
  const rows = useMemo(() => {
    let filtered = allLogs.filter((r) => r.projectId === projectId);
    if (taskId) filtered = filtered.filter((r) => r.taskId === taskId);
    return filtered.slice(-TAIL_ROWS);
  }, [allLogs, projectId, taskId]);

  if (!projectId) {
    return (
      <div style={{ color: "var(--p2-fg-faint)", fontSize: 12 }}>
        No project selected — drag a task or open logs from a task card.
      </div>
    );
  }

  if (rows.length === 0) {
    return (
      <div style={{ color: "var(--p2-fg-faint)", fontSize: 12 }}>
        No recent log activity for {taskId ? `task ${taskId}` : projectId}.
      </div>
    );
  }

  return (
    <pre
      style={{
        margin: 0,
        padding: "6px 8px",
        background: "var(--p2-bg-2)",
        color: "var(--p2-fg-dim)",
        border: "1px solid var(--p2-border)",
        borderRadius: "var(--p2-radius-xs)",
        fontSize: 11,
        fontFamily:
          "ui-monospace, SFMono-Regular, 'JetBrains Mono', Menlo, monospace",
        lineHeight: 1.4,
        whiteSpace: "pre-wrap",
        wordBreak: "break-word",
      }}
    >
      {rows
        .map((r) => {
          const stamp = r.line.timestamp
            ? new Date(r.line.timestamp).toLocaleTimeString()
            : "";
          const shortTask =
            r.taskId.length > 8 ? r.taskId.slice(0, 8) : r.taskId;
          return `${stamp.padEnd(10)} ${shortTask.padEnd(9)} ${r.line.content}`;
        })
        .join("\n")}
    </pre>
  );
}
