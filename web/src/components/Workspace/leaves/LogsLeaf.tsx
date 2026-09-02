/**
 * panes-v2 LogsLeaf.
 *
 * Renders the log stream for a task (or a whole project if only
 * `projectId` is provided), in a taller viewport than CardLogs.
 *
 * For a SINGLE task it merges two sources, the way ProcessRawLog does:
 *
 *   • the live SSE buffer — everything that streamed since this page
 *     connected, and nothing before it;
 *   • the server's stored lines (GET …/tasks/{p}/{t}/logs).
 *
 * The live buffer alone is empty for anything that ran before the tab
 * was opened, which is most of what you open logs FOR — a run from last
 * night, an automation task from last week. That made "open the log" a
 * button that showed "no recent log activity" for every historical task
 * while the lines sat in the store.
 *
 * target shape: { projectId: string; taskId?: string }
 */
import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useLive } from "../../../lib/sse";
import { getTaskLogs } from "../../../lib/api";
import { mergeTaskLogs } from "../../../lib/processes";
import type { LogLine } from "../../../lib/types";

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

  // Stored lines, for one task only: the endpoint is per-task, and a
  // project-wide pane is a live tail by construction.
  const stored = useQuery({
    queryKey: ["v2", "task-logs", projectId, taskId],
    queryFn: () => getTaskLogs(projectId, taskId as string, { limit: 300 }),
    enabled: !!projectId && !!taskId,
    refetchInterval: 5_000,
    staleTime: 4_000,
  });

  const rows = useMemo(() => {
    const live = allLogs.filter(
      (r) => r.projectId === projectId && (!taskId || r.taskId === taskId),
    );
    if (!taskId) return live.slice(-TAIL_ROWS);
    const liveLines: LogLine[] = live.map((r) => r.line);
    return mergeTaskLogs(stored.data?.lines ?? [], liveLines)
      .slice(-TAIL_ROWS)
      .map((line) => ({ projectId, taskId, line }));
  }, [allLogs, projectId, taskId, stored.data]);

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
        {taskId
          ? stored.isPending
            ? "Loading this task's log…"
            : `No log lines stored for task ${taskId}, and nothing streaming now.`
          : `No recent log activity for ${projectId}.`}
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
