/**
 * TaskModal — wireframe-parity.
 */
import { useMemo } from "react";
import { Modal } from "../common/Modal";
import { ActionBar } from "../common/ActionBar";
import { useModal } from "../../store/modal";
import { useLive } from "../../lib/sse";
import { useActionRunner } from "../../hooks/useActionRunner";
import { usePauseState } from "../../hooks/usePauseState";
import { useTaskActionContext } from "../../hooks/useTaskActionContext";
import { buildTaskActions } from "../../lib/actions/taskActions";
import { taskHoldReason } from "../../lib/pause";
import { TaskKvGrid } from "./TaskKvGrid";
import { SessionsSection } from "./SessionsSection";
import { DispatchAttemptsSection } from "./DispatchAttemptsSection";
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
  const { pause } = usePauseState();
  // Hooks must run before the not-found early return below, so this is
  // computed against a possibly-undefined task.
  const hold = task ? taskHoldReason(task, { pause, projectId }) : null;
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
          {hold && (
            <span
              className={`life-badge held ${hold.code}`}
              style={{ marginLeft: 8 }}
              title={hold.detail}
            >
              {hold.glyph} {hold.short}
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
      {/* A task at `ready` with nothing running is the case with no visible
          explanation anywhere else. The badge above flags it; this says
          which switch is holding it and what releases it. */}
      {hold && (
        <div className={`hold-banner ${hold.code}`}>
          <b>
            {hold.glyph} Held — not dispatching.
          </b>{" "}
          {hold.detail}
        </div>
      )}

      <TaskKvGrid task={task} projectId={projectId} />

      <SessionsSection
        task={task}
        projectId={projectId}
        onView={(t, ref) => taskCtx.openSession(t, ref)}
      />

      <DispatchAttemptsSection task={task} />

      {task.content && (
        <>
          <h4 className="modal-content-heading">Content</h4>
          <pre className="modal-content-pre">{task.content}</pre>
        </>
      )}
    </Modal>
  );
}
