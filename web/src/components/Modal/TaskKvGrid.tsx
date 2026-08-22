/**
 * TaskKvGrid — the task metadata key/value grid.
 *
 * Extracted verbatim from TaskModal so the same grid can be rendered
 * both in the full Task modal and inside the right-side task drawer
 * without the two drifting. The "Feature →" row opens the feature
 * modal via the shared modal store.
 */
import { useModal } from "../../store/modal";
import type { Task } from "../../lib/types";

export function TaskKvGrid({
  task,
  projectId,
}: {
  task: Task;
  projectId: string;
}): JSX.Element {
  const openModal = useModal((s) => s.open);

  return (
    <div className="kv-grid">
      <div className="k">Project</div>
      <div className="v">{projectId}</div>
      <div className="k">Task id</div>
      <div className="v">{task.id}</div>
      <div className="k">Status</div>
      <div className="v">{task.status}</div>
      {task.priority && (
        <>
          <div className="k">Priority</div>
          <div className="v">
            <span className="chip">{task.priority}</span>
          </div>
        </>
      )}
      {task.feature_id && (
        <>
          <div className="k">Feature</div>
          <div className="v">
            <button
              onClick={() =>
                openModal("feature", {
                  projectId,
                  featureId: task.feature_id,
                })
              }
              style={{ color: "#f4b23a", border: 0, background: "transparent", cursor: "pointer", padding: 0 }}
            >
              {task.feature_id} →
            </button>
          </div>
        </>
      )}
      {task.agent && (
        <>
          <div className="k">Agent</div>
          <div className="v">{task.agent}</div>
        </>
      )}
      {task.executor && (
        <>
          <div className="k">Executor</div>
          <div className="v">{task.executor}</div>
        </>
      )}
      {task.model && (
        <>
          <div className="k">Model</div>
          <div className="v">{task.model}</div>
        </>
      )}
      {task.git_branch && (
        <>
          <div className="k">Git branch</div>
          <div className="v">
            <code>{task.git_branch}</code>
          </div>
        </>
      )}
      {task.workdir && (
        <>
          <div className="k">Workdir</div>
          <div className="v">
            <code>{task.workdir}</code>
          </div>
        </>
      )}
      {task.merge_target_branch && (
        <>
          <div className="k">Merge target</div>
          <div className="v">
            {task.merge_target_branch}
            {task.merge_strategy ? ` · ${task.merge_strategy}` : ""}
            {task.merge_policy ? ` · ${task.merge_policy}` : ""}
          </div>
        </>
      )}
      {task.tags && task.tags.length > 0 && (
        <>
          <div className="k">Tags</div>
          <div className="v">
            {task.tags.map((t) => (
              <span key={t} className="chip mini" style={{ marginRight: 4 }}>
                {t}
              </span>
            ))}
          </div>
        </>
      )}
      {task.created && (
        <>
          <div className="k">Created</div>
          <div className="v">{task.created}</div>
        </>
      )}
      {task.modified && (
        <>
          <div className="k">Modified</div>
          <div className="v">{task.modified}</div>
        </>
      )}
    </div>
  );
}
