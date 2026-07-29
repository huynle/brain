/**
 * TaskActionsModal — surfaces the Resume affordance for a single task.
 *
 * State machine:
 *   • menu           — main choice: Resume (or Force Resume when unabandoned)
 *   • confirmForce   — two-step confirm before force=true (destructive-ish)
 *
 * Reads taskId + projectId from useModal().target (matching the panes-v2
 * modal store contract). Task itself is read from useLive; we never take
 * task props so the modal always shows fresh data.
 */
import { useMemo, useState } from "react";
import { Modal } from "../common/Modal";
import { resumeTask } from "../../lib/api";
import { useLive } from "../../lib/sse";
import { useModal } from "../../store/modal";
import { useUI } from "../../store/ui";
import type { Task } from "../../lib/types";
import { computeTaskResumeState } from "./taskActions";

type View = "menu" | "confirmForce";

const EMPTY_TASKS: readonly Task[] = Object.freeze([]);

export function TaskActionsModal(): JSX.Element {
  const target = useModal((s) => s.target);
  const close = useModal((s) => s.close);
  const toast = useUI((s) => s.toast);

  const taskId = (target?.taskId as string | undefined) ?? "";
  const projectId = (target?.projectId as string | undefined) ?? "";

  const projectTasks = useLive((s) => s.projects[projectId]?.tasks) ?? EMPTY_TASKS;
  const task = useMemo(
    () => projectTasks.find((t) => t.id === taskId) || null,
    [projectTasks, taskId],
  );

  const state = useMemo(() => computeTaskResumeState(task), [task]);
  const [view, setView] = useState<View>("menu");
  const [busy, setBusy] = useState(false);

  if (!task) {
    return (
      <Modal
        title={taskId ? `Task not found: ${taskId}` : "Task"}
        onClose={close}
      >
        <div className="muted">
          No matching task in project <code>{projectId}</code>.
        </div>
      </Modal>
    );
  }

  async function submit(force: boolean) {
    if (!task) return;
    setBusy(true);
    try {
      const result = await resumeTask(projectId, task.id, { force });
      if (result.resumed) {
        toast(
          `Task resumed${result.prior_status ? ` (was ${result.prior_status})` : ""}`,
          "success",
        );
        close();
        return;
      }
      // Non-resume outcome: surface the server's reason as an info toast so
      // the user learns why nothing happened (idempotent replay, still-
      // claimed, terminal, etc.).
      toast(result.reason || "Resume was a no-op", "info");
      close();
    } catch (err) {
      toast(
        `Resume failed: ${err instanceof Error ? err.message : String(err)}`,
        "error",
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title={
        <>
          Actions: {task.title || task.id}{" "}
          {task.is_abandoned && (
            <span
              className="life-badge abandoned"
              style={{ marginLeft: 8 }}
              title={state.reasonHint}
            >
              abandoned
            </span>
          )}
          {state.alreadyResumed && (
            <span
              className="life-badge"
              style={{ marginLeft: 8 }}
              title={state.reasonHint}
            >
              resume pending
            </span>
          )}
        </>
      }
      onClose={
        // Esc in the confirmForce view should return to the menu, not blow
        // away the whole modal — otherwise the "Esc = No" hint lies. Only
        // the menu view lets Esc close the modal entirely.
        view === "confirmForce" ? () => setView("menu") : close
      }
      refocusKey={view}
      footer={
        view === "menu" ? (
          <>
            <span className="faint">Esc closes</span>
            <button onClick={close} disabled={busy}>
              Cancel
            </button>
          </>
        ) : (
          <>
            <span className="faint">Esc = No</span>
            <button
              onClick={() => setView("menu")}
              disabled={busy}
            >
              No
            </button>
            <button
              className="primary"
              style={{ marginLeft: "auto" }}
              onClick={() => void submit(true)}
              disabled={busy}
              data-autofocus="true"
            >
              {busy ? "Submitting..." : "Yes, force resume"}
            </button>
          </>
        )
      }
    >
      {view === "menu" && (
        <div>
          <div className="kv-grid">
            <div className="k">Task id</div>
            <div className="v">{task.id}</div>
            <div className="k">Project</div>
            <div className="v">{projectId}</div>
            <div className="k">Status</div>
            <div className="v">{task.status}</div>
            {task.abandon_reason && (
              <>
                <div className="k">Abandon reason</div>
                <div className="v">{task.abandon_reason}</div>
              </>
            )}
          </div>

          <p className="muted" style={{ marginTop: 12 }}>
            {state.reasonHint || "This task does not appear resumable right now."}
          </p>

          {!state.showResume && (
            <p className="muted">
              Use <strong>Trigger</strong> for a fresh re-run, or wait for the
              runner if this task is currently in flight.
            </p>
          )}

          <div
            style={{ display: "flex", flexDirection: "column", gap: "0.5rem", marginTop: 12 }}
          >
            {state.showResume && !state.alreadyResumed && state.canResumeCleanly && (
              <button
                type="button"
                className="primary"
                onClick={() => void submit(false)}
                disabled={busy}
                data-autofocus="true"
                style={{ padding: "0.6rem 0.9rem", textAlign: "left" }}
              >
                <strong>Resume task</strong>
                <br />
                <span className="faint">
                  Flip to pending with resume flag — runner re-spawns with prior-progress hint
                </span>
              </button>
            )}

            {state.showResume && !state.alreadyResumed && !state.canResumeCleanly && (
              <button
                type="button"
                className="danger"
                onClick={() => setView("confirmForce")}
                disabled={busy}
                data-autofocus="true"
                style={{ padding: "0.6rem 0.9rem", textAlign: "left" }}
              >
                <strong>Force resume</strong>
                <br />
                <span className="faint">{state.forceHint}</span>
              </button>
            )}

            {state.alreadyResumed && (
              <button
                type="button"
                onClick={() => void submit(false)}
                disabled={busy}
                data-autofocus="true"
                style={{ padding: "0.6rem 0.9rem", textAlign: "left" }}
              >
                <strong>Re-request resume (no-op)</strong>
                <br />
                <span className="faint">
                  Confirms the intent — server will report already-resumed
                </span>
              </button>
            )}
          </div>
        </div>
      )}

      {view === "confirmForce" && (
        <div>
          <p style={{ marginTop: 0 }}>
            <strong>Force resume</strong> — this task is not automatically
            detected as abandoned (status={" "}
            <code>{task.status}</code>).
          </p>
          <p className="muted">
            Force bypasses the abandonment gate but NOT the live-claim safety —
            if the current claim is held by an online runner, the server will
            still refuse. Continue?
          </p>
        </div>
      )}
    </Modal>
  );
}
