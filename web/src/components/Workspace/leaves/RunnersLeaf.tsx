/**
 * RunnersLeaf — compact runners list for a focus pane.
 *
 * Uses wireframe `.runner-row` styling. Clicking a row opens the
 * runner modal.
 */
import { useRunners } from "../../../hooks/useRunners";
import { useModal } from "../../../store/modal";
import { Loading } from "../../common/Loading";
import { ErrorState } from "../../common/ErrorState";
import type { RunnerInfo } from "../../../lib/types";

function runnerDot(status: RunnerInfo["status"]): "on" | "err" | "" {
  if (status === "online") return "on";
  if (status === "stale") return "err";
  return "";
}

export function RunnersLeaf(_props: {
  target: Record<string, unknown>;
}): JSX.Element {
  const { runners, isLoading, error, refetch } = useRunners();
  const open = useModal((s) => s.open);

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
      {runners.map((r) => (
        <div
          key={r.runner_id}
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
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}
