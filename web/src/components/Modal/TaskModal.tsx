/**
 * TaskModal — wireframe-parity.
 */
import { useMemo } from "react";
import { Modal } from "../common/Modal";
import { ActionBar } from "../common/ActionBar";
import { useModal } from "../../store/modal";
import { useLive } from "../../lib/sse";
import { useActionRunner } from "../../hooks/useActionRunner";
import { useTaskActionContext } from "../../hooks/useTaskActionContext";
import { buildTaskActions } from "../../lib/actions/taskActions";
import { historySessionRefs } from "../../lib/sessionRef";
import { TaskKvGrid } from "./TaskKvGrid";
import { useUI } from "../../store/ui";
import type { Task } from "../../lib/types";

const EMPTY_TASKS: readonly Task[] = Object.freeze([]);

export function TaskModal(): JSX.Element {
  const target = useModal((s) => s.target);
  const close = useModal((s) => s.close);

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

  const taskCtx = useTaskActionContext(projectId);
  const runner = useActionRunner();
  // Built unconditionally so the hook order is stable across the
  // not-found early return below.
  const actions = useMemo(
    () => (task ? buildTaskActions(task, taskCtx) : []),
    [task, taskCtx],
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
      title={
        <>
          {task.title || task.id}
          {task.is_abandoned && (
            <span
              className="life-badge abandoned"
              style={{ marginLeft: 8 }}
              title={
                task.abandon_reason
                  ? `Abandoned (${task.abandon_reason})`
                  : "Abandoned"
              }
            >
              abandoned
            </span>
          )}
          {task.resume_requested && !task.is_abandoned && (
            <span
              className="life-badge"
              style={{ marginLeft: 8 }}
              title="Resume requested; runner will pick up on next poll"
            >
              resume pending
            </span>
          )}
        </>
      }
      onClose={close}
      footer={
        <>
          {/* Every verb comes from the shared registry, so this footer,
              the row context menu and the touch sheet cannot drift.
              Watch is promoted beside Run/Resume while the task runs
              (plan Decision 2) so it isn't buried behind "More…". */}
          <ActionBar
            actions={actions}
            onRun={runner.run}
            primary={
              task.status === "in_progress"
                ? ["run", "resume", "watch"]
                : undefined
            }
          />
          <button className="primary" onClick={close}>
            Done
          </button>
          {runner.dialog}
        </>
      }
    >
      <TaskKvGrid task={task} projectId={projectId} />

      <SessionsSection task={task} projectId={projectId} />

      {task.content && (
        <>
          <h4 className="modal-content-heading">Content</h4>
          <pre className="modal-content-pre">{task.content}</pre>
        </>
      )}
    </Modal>
  );
}

/**
 * Recorded sessions for the task, newest first — every entry, not just
 * the newest: after an abandonment + resume the pre-abandonment
 * transcript is exactly the one worth inspecting. Each row gates on its
 * OWN recorded runner (sessions can span runners across retries).
 */
function SessionsSection({
  task,
  projectId,
}: {
  task: Task;
  projectId: string;
}): JSX.Element | null {
  const taskCtx = useTaskActionContext(projectId);
  const toast = useUI((s) => s.toast);
  const refs = historySessionRefs(task);
  if (refs.length === 0) return null;

  return (
    <>
      <h4 className="modal-content-heading">Sessions</h4>
      <div className="kv-grid">
        {refs.map((ref) =>
          ref.mode === "history" ? (
            <div
              key={ref.session_id}
              style={{
                gridColumn: "1 / -1",
                display: "flex",
                alignItems: "center",
                gap: 8,
                fontSize: 12,
              }}
            >
              <code style={{ fontSize: 11 }}>{ref.session_id.slice(0, 18)}…</code>
              <span style={{ color: "#6b757e" }}>
                {(task.sessions?.[ref.session_id]?.timestamp ?? "").slice(0, 16)}
                {task.sessions?.[ref.session_id]?.hostname
                  ? ` · ${task.sessions[ref.session_id].hostname}`
                  : ""}
                {` · ${ref.runner_id}`}
              </span>
              <span style={{ flex: 1 }} />
              <button
                onClick={() => taskCtx.openTranscript(task, ref)}
                style={{
                  border: "1px solid #333a42",
                  background: "transparent",
                  color: "inherit",
                  borderRadius: 4,
                  padding: "2px 8px",
                  fontSize: 11,
                  cursor: "pointer",
                }}
              >
                View
              </button>
              <button
                onClick={() =>
                  taskCtx.continueSession(task, ref).catch((err) => {
                    toast(
                      `Continue failed: ${(err as Error)?.message ?? err}`,
                      "error",
                    );
                  })
                }
                title="Reopen this session on its runner with a fresh instance"
                style={{
                  border: "1px solid #333a42",
                  background: "transparent",
                  color: "inherit",
                  borderRadius: 4,
                  padding: "2px 8px",
                  fontSize: 11,
                  cursor: "pointer",
                }}
              >
                Continue
              </button>
            </div>
          ) : null,
        )}
      </div>
    </>
  );
}
