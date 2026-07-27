/**
 * TaskModal — wireframe-parity.
 */
import { useMemo } from "react";
import { Modal } from "../common/Modal";
import { useModal } from "../../store/modal";
import { useWorkspace } from "../../store/workspace";
import { useLive } from "../../lib/sse";
import type { Task } from "../../lib/types";

const EMPTY_TASKS: readonly Task[] = Object.freeze([]);

export function TaskModal(): JSX.Element {
  const target = useModal((s) => s.target);
  const openModal = useModal((s) => s.open);
  const close = useModal((s) => s.close);
  const openInFocus = useWorkspace((s) => s.openInFocus);

  const taskId =
    (target?.taskId as string | undefined) ??
    (target?.id as string | undefined) ??
    "";
  const projectId = (target?.projectId as string | undefined) ?? "";

  const tasks = useLive((s) => s.projects[projectId]?.tasks) ?? EMPTY_TASKS;
  const task = useMemo(
    () => tasks.find((t) => t.id === taskId),
    [tasks, taskId],
  );

  if (!task) {
    return (
      <Modal title={taskId ? `Task not found: ${taskId}` : "Task"} onClose={close}>
        <div style={{ color: "#9098a1" }}>
          No matching task in <code>{projectId}</code>.
        </div>
      </Modal>
    );
  }

  return (
    <Modal
      title={task.title || task.id}
      onClose={close}
      footer={
        <>
          <button
            onClick={() => {
              close();
              openInFocus(
                "logs",
                { projectId, taskId: task.id },
                `Logs ${task.id.slice(0, 8)}`,
              );
            }}
          >
            Open logs in focus
          </button>
          <button
            onClick={() => {
              close();
              openInFocus(
                "task-detail",
                { projectId, taskId: task.id },
                task.title || task.id,
              );
            }}
          >
            Open in focus pane
          </button>
          <button className="primary" onClick={close}>
            Done
          </button>
        </>
      }
    >
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

      {task.content && (
        <>
          <h4 className="modal-content-heading">Content</h4>
          <pre className="modal-content-pre">{task.content}</pre>
        </>
      )}
    </Modal>
  );
}
