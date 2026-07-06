import { useMemo, useState } from "react";
import { Modal, ConfirmDialog } from "../../components/common/Modal";
import { StatusBadge, PriorityTag, Pill } from "../../components/common/Badge";
import { MetadataModal } from "./MetadataModal";
import { useLive } from "../../lib/sse";
import { useUI } from "../../store/ui";
import {
  deleteEntry,
  runOrTriggerTask,
  setTaskStatus,
  summarizeTriggerResults,
} from "../../lib/api";
import { isActive, relativeTime } from "../../lib/format";
import type { Task } from "../../lib/types";

export function TaskDetail({
  task: initial,
  onClose,
}: {
  task: Task;
  onClose: () => void;
}) {
  const toast = useUI((s) => s.toast);
  const showLogsFor = useUI((s) => s.showLogsFor);
  const projects = useLive((s) => s.projects);
  const [editing, setEditing] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [busy, setBusy] = useState(false);

  // Keep the sheet in sync with live updates.
  const task = useMemo(() => {
    const pid = initial.projectId;
    const list = pid ? projects[pid]?.tasks : undefined;
    return list?.find((t) => t.id === initial.id) ?? initial;
  }, [projects, initial]);

  const projectId = task.projectId;

  async function run(label: string, fn: () => Promise<unknown>) {
    setBusy(true);
    try {
      await fn();
      toast(label, "success");
    } catch (e) {
      toast(e instanceof Error ? e.message : "Action failed", "error");
    } finally {
      setBusy(false);
    }
  }

  // Run this task. Prefers /run (push dispatch to runner immediately);
  // falls back to /trigger when the server doesn't expose /run.
  async function runTrigger() {
    if (!projectId) return;
    setBusy(true);
    try {
      const resp = await runOrTriggerTask(projectId, task.id);
      const { message, kind } = summarizeTriggerResults([resp]);
      toast(message, kind);
    } catch (e) {
      toast(e instanceof Error ? e.message : "Run failed", "error");
    } finally {
      setBusy(false);
    }
  }

  const description =
    task.content || task.direct_prompt || task.user_original_request || "";

  return (
    <>
      <Modal
        title="Task"
        onClose={onClose}
        footer={
          <div className="btn-row" style={{ width: "100%" }}>
            {projectId && (
              <button
                className="btn primary sm"
                disabled={busy}
                onClick={() => void runTrigger()}
              >
                ▶ Run
              </button>
            )}
            {task.status !== "completed" && (
              <button
                className="btn sm"
                disabled={busy}
                onClick={() =>
                  void run("Marked complete", () =>
                    setTaskStatus(task, "completed"),
                  )
                }
              >
                ✓ Complete
              </button>
            )}
            {isActive(task.status) && (
              <button
                className="btn sm"
                disabled={busy}
                onClick={() =>
                  void run("Cancelled", () => setTaskStatus(task, "cancelled"))
                }
              >
                ⊘ Cancel
              </button>
            )}
            <button className="btn sm" onClick={() => setEditing(true)}>
              ✎ Edit
            </button>
            <button
              className="btn danger sm"
              style={{ marginLeft: "auto" }}
              onClick={() => setConfirmDelete(true)}
            >
              Delete
            </button>
          </div>
        }
      >
        <h2 style={{ margin: "0 0 0.5rem", fontSize: 17, lineHeight: 1.3 }}>
          {task.in_cycle && <span style={{ color: "var(--red)" }}>↺ </span>}
          {task.title || task.id}
        </h2>

        <div className="row wrap" style={{ gap: "0.35rem", marginBottom: "0.7rem" }}>
          <StatusBadge status={task.status} />
          <PriorityTag priority={task.priority} />
          {task.feature_id && (
            <Pill color="var(--purple)">⊞ {task.feature_id}</Pill>
          )}
          {projectId && (
            <Pill color="var(--cyan)">{projectId.split(/[/\\]/).pop()}</Pill>
          )}
        </div>

        <Field label="ID" mono value={task.id} />
        {task.agent && <Field label="Agent" value={task.agent} />}
        {task.model && <Field label="Model" mono value={task.model} />}
        {task.executor && <Field label="Executor" value={task.executor} />}
        {task.workdir && <Field label="Workdir" mono value={task.workdir} />}
        {task.git_branch && <Field label="Branch" mono value={task.git_branch} />}
        {task.execution_mode && (
          <Field label="Execution" value={task.execution_mode} />
        )}
        {task.schedule && <Field label="Schedule" mono value={task.schedule} />}
        {task.created && (
          <Field label="Created" value={relativeTime(task.created)} />
        )}
        {task.tags && task.tags.length > 0 && (
          <Field label="Tags" value={task.tags.join(", ")} />
        )}

        {task.depends_on && task.depends_on.length > 0 && (
          <div className="field">
            <label>Depends on</label>
            <div className="row wrap" style={{ gap: "0.3rem" }}>
              {task.depends_on.map((d) => {
                const resolved = task.resolved_deps?.includes(d);
                return (
                  <Pill
                    key={d}
                    color={resolved ? "var(--green)" : "var(--yellow)"}
                  >
                    {resolved ? "✓" : "⧖"} {d}
                  </Pill>
                );
              })}
            </div>
          </div>
        )}

        {task.blocked_by && task.blocked_by.length > 0 && (
          <div className="field">
            <label>Blocked by</label>
            <div className="muted">
              {task.blocked_by_reason || task.blocked_by.join(", ")}
            </div>
          </div>
        )}

        {description && (
          <div className="field">
            <label>Description</label>
            <div
              className="card section-pad"
              style={{ whiteSpace: "pre-wrap", fontSize: 14 }}
            >
              {description}
            </div>
          </div>
        )}

        {task.sessions && Object.keys(task.sessions).length > 0 && (
          <button
            className="btn sm"
            onClick={() => {
              showLogsFor(task.id);
              onClose();
            }}
          >
            ▤ View logs
          </button>
        )}
      </Modal>

      {editing && (
        <MetadataModal task={task} onClose={() => setEditing(false)} />
      )}
      {confirmDelete && (
        <ConfirmDialog
          title="Delete task?"
          danger
          confirmLabel="Delete"
          message={
            <>
              This permanently deletes <strong>{task.title || task.id}</strong>.
            </>
          }
          busy={busy}
          onClose={() => setConfirmDelete(false)}
          onConfirm={() =>
            void run("Deleted", () => deleteEntry(task.path)).then(() => {
              setConfirmDelete(false);
              onClose();
            })
          }
        />
      )}
    </>
  );
}

function Field({
  label,
  value,
  mono,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="field" style={{ marginBottom: "0.5rem" }}>
      <label>{label}</label>
      <div className={mono ? "mono" : ""} style={{ wordBreak: "break-word" }}>
        {value}
      </div>
    </div>
  );
}
