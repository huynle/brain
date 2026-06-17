// Logs pane for the highlighted entry, shared by Tasks / Brain / Automations.
// Shows a task's execution output (works for running and completed tasks).
// Non-task entries (plain notes, automations with no run selected) have no
// logs and say so.

import { useQuery } from "@tanstack/react-query";
import { getTaskLogs } from "../../lib/api";
import { cleanLogContent, clockTime, logLevelColor } from "../../lib/format";

export function EntryLogsPane({
  taskId,
  projectId,
}: {
  taskId?: string;
  projectId?: string;
}) {
  const enabled = !!taskId && !!projectId;
  const q = useQuery({
    queryKey: ["entry-logs", projectId, taskId],
    queryFn: () => getTaskLogs(projectId as string, taskId as string, { limit: 500 }),
    enabled,
    refetchInterval: 3_000,
  });

  if (!enabled) {
    return <span className="faint">No task highlighted — logs show for a highlighted task.</span>;
  }
  if (q.isLoading) return <span className="faint">Loading logs…</span>;
  if (q.error) {
    return <span className="faint" style={{ color: "var(--red)" }}>{String((q.error as Error).message)}</span>;
  }
  const lines = q.data?.lines ?? [];
  if (lines.length === 0) return <span className="faint">No logs for this task.</span>;

  return (
    <>
      {lines.map((l, i) => (
        <div key={i} className="logline">
          <span className="lt">{clockTime(l.timestamp)}</span>
          <span className="ll" style={{ color: logLevelColor(l.level) }}>{l.level}</span>
          <span className="lc">{cleanLogContent(l.content)}</span>
        </div>
      ))}
    </>
  );
}
