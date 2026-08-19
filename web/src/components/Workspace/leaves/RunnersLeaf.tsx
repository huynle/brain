/**
 * RunnersLeaf — runners list for a focus pane, with each runner's
 * executor processes (instances) inlined under its row.
 *
 * Uses wireframe `.runner-row` styling for the runner and `.proc-mini`
 * rows for its processes. Clicking a runner opens the runner modal;
 * clicking a process opens the modal on the Processes tab with that
 * process's log preselected.
 */
import { useMemo } from "react";
import { useRunners } from "../../../hooks/useRunners";
import { useSessions } from "../../../hooks/useSessions";
import { useModal } from "../../../store/modal";
import { Loading } from "../../common/Loading";
import { ErrorState } from "../../common/ErrorState";
import {
  groupInstancesByRunner,
  instanceDot,
} from "../../../lib/processes";
import type { OpencodeInstance, RunnerInfo } from "../../../lib/types";

function runnerDot(status: RunnerInfo["status"]): "on" | "err" | "" {
  if (status === "online") return "on";
  if (status === "stale") return "err";
  return "";
}

function procLabel(p: OpencodeInstance): string {
  if (p.kind === "adhoc") return p.title || "ad-hoc session";
  return p.title || p.task_id || p.instance_id;
}

export function RunnersLeaf(_props: {
  target: Record<string, unknown>;
}): JSX.Element {
  const { runners, isLoading, error, refetch } = useRunners();
  const { allInstances } = useSessions();
  const open = useModal((s) => s.open);

  const byRunner = useMemo(
    () =>
      groupInstancesByRunner(
        allInstances.filter((i) => i.status !== "exited"),
      ),
    [allInstances],
  );

  if (isLoading) return <Loading size="sm" label="Loading runners…" />;
  if (error) return <ErrorState error={error} onRetry={refetch} />;

  if (runners.length === 0) {
    return (
      <div style={{ color: "#6b757e", fontSize: 12, padding: 8 }}>
        No runners are registered.
      </div>
    );
  }

  return (
    <div>
      {runners.map((r) => {
        const procs = byRunner[r.runner_id] ?? [];
        return (
          <div key={r.runner_id} className="runner-group">
            <div
              className="runner-row"
              onClick={() => open("runner", { id: r.runner_id })}
            >
              <span className={`dot ${runnerDot(r.status)}`} />
              <div className="runner-body">
                <div className="runner-name">{r.runner_id}</div>
                <div className="runner-meta">
                  <span>{r.status}</span>
                  <span>
                    {r.active_tasks ?? 0}/{r.max_parallel ?? 0}
                  </span>
                  {r.executors?.[0] && <span>{r.executors[0]}</span>}
                  <span>
                    {procs.length} proc{procs.length === 1 ? "" : "s"}
                  </span>
                </div>
              </div>
            </div>
            {procs.map((p) => (
              <div
                key={p.instance_id}
                className="proc-mini"
                title={p.task_id ? `${p.project_id}/${p.task_id}` : undefined}
                onClick={() =>
                  open(
                    "runner",
                    { id: r.runner_id, instanceId: p.instance_id },
                    "processes",
                  )
                }
              >
                <span className={`dot ${instanceDot(p.status)}`} />
                <span className="proc-mini-name">{procLabel(p)}</span>
                <span className="proc-mini-meta">
                  {p.executor || "opencode"}
                  {p.task_id ? ` · ${p.task_id}` : ""}
                </span>
              </div>
            ))}
          </div>
        );
      })}
    </div>
  );
}
