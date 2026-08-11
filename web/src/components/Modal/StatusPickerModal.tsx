/**
 * StatusPickerModal — one modal, two modes.
 *
 * Task mode changes a single task's status. Feature mode applies one
 * status across every task sharing the feature id, and says so plainly in
 * the header — a feature-wide mutation should never look like a single
 * edit.
 *
 * The current status is shown but disabled in task mode, so the picker
 * doubles as a display of where the task actually is.
 */
import { Modal } from "../common/Modal";
import { useModal } from "../../store/modal";
import { useLive } from "../../lib/sse";
import { useActionRunner } from "../../hooks/useActionRunner";
import { useTaskActionContext } from "../../hooks/useTaskActionContext";
import { useFeatureActionContext } from "../../hooks/useFeatureActionContext";
import { buildStatusActions } from "../../lib/actions/taskActions";
import { buildFeatureStatusActions } from "../../lib/actions/featureActions";
import { deriveFeatures } from "../../lib/features";
import { isEnabled, type ActionDescriptor } from "../../lib/actions/types";
import { ALL_STATUSES, type Task } from "../../lib/types";
import { useMemo } from "react";

const EMPTY_TASKS: readonly Task[] = Object.freeze([]);

export interface StatusPickerModalProps {
  /** "task" edits one task; "feature" fans out across the feature. */
  mode: "task" | "feature";
}

export function StatusPickerModal({ mode }: StatusPickerModalProps): JSX.Element {
  const target = useModal((s) => s.target);
  const close = useModal((s) => s.close);

  const projectId = (target?.projectId as string | undefined) ?? "";
  const taskId = (target?.taskId as string | undefined) ?? "";
  const featureId = (target?.featureId as string | undefined) ?? "";

  const tasks = useLive((s) => s.projects[projectId]?.tasks) ?? EMPTY_TASKS;
  const taskCtx = useTaskActionContext(projectId);
  const featureCtx = useFeatureActionContext(projectId);
  const runner = useActionRunner();

  const task = useMemo(
    () => tasks.find((t) => t.id === taskId),
    [tasks, taskId],
  );
  const feature = useMemo(
    () => deriveFeatures(tasks, projectId).find((f) => f.id === featureId),
    [tasks, projectId, featureId],
  );

  let heading = "";
  let subtitle = "";
  let actions: ActionDescriptor[] = [];

  if (mode === "task") {
    if (!task) {
      return (
        <Modal title="Task not found" onClose={close}>
          <div style={{ color: "#9098a1" }}>
            No matching task in <code>{projectId}</code>.
          </div>
        </Modal>
      );
    }
    heading = `Status: ${task.title || task.id}`;
    subtitle = `Currently ${task.status}.`;
    actions = buildStatusActions(task, taskCtx);
  } else {
    if (!feature) {
      return (
        <Modal title="Feature not found" onClose={close}>
          <div style={{ color: "#9098a1" }}>
            No matching feature in <code>{projectId}</code>.
          </div>
        </Modal>
      );
    }
    heading = `Status: ${feature.name}`;
    subtitle = `Applies to all ${feature.taskCount.total} ${
      feature.taskCount.total === 1 ? "task" : "tasks"
    } in this feature.`;
    actions = buildFeatureStatusActions(feature, featureCtx, ALL_STATUSES);
  }

  return (
    <Modal
      title={heading}
      onClose={close}
      footer={
        <button className="primary" onClick={close}>
          Close
        </button>
      }
    >
      <p style={{ fontSize: 11, color: "#9098a1", margin: "0 0 10px" }}>
        {subtitle}
      </p>

      <div style={{ display: "flex", flexDirection: "column", gap: 2 }}>
        {actions.map((a) => {
          const enabled = isEnabled(a);
          return (
            <button
              key={a.id}
              disabled={!enabled}
              title={a.disabledReason || undefined}
              onClick={() => {
                runner.run(a);
                // Feature-mode entries carry a confirm dialog, which the
                // runner renders — closing here would unmount it. Task-mode
                // entries apply immediately, so closing is right.
                if (!a.confirm) close();
              }}
              style={{
                textAlign: "left",
                padding: "6px 8px",
                fontSize: 12,
                border: "1px solid #22272c",
                borderRadius: 3,
                background: "transparent",
                color: enabled ? "#eaedef" : "#4b545c",
                cursor: enabled ? "pointer" : "not-allowed",
              }}
            >
              {a.label}
              {!enabled && (
                <span style={{ color: "#6b757e", fontSize: 10 }}>
                  {" — "}
                  {a.disabledReason}
                </span>
              )}
            </button>
          );
        })}
      </div>

      {runner.dialog}
    </Modal>
  );
}
