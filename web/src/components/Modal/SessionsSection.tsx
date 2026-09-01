/**
 * SessionsSection — recorded sessions for a task, newest first.
 *
 * Shared between TaskModal and TaskDetailLeaf (the panes-v2 task pane,
 * docked in either the Focus tab or the sidebar dock) so the two "full
 * detail" surfaces can't drift. Every recorded entry is listed, not
 * just the newest: after an abandonment + resume the pre-abandonment
 * transcript is exactly the one worth inspecting. Each row gates on its
 * OWN recorded runner (sessions can span runners across retries).
 *
 * `onView` is the caller's choice of what "View" does — TaskModal routes
 * it to the full-page session view (`openSession`); TaskDetailLeaf
 * routes it to `openSessionInDrawer` so the row opens as a sibling pane
 * in the sidebar dock instead of navigating away. "Continue" always
 * reopens the session live via `continueSession` — spawning a fresh
 * instance is a big enough action that it earns the full-page view
 * regardless of where the click came from.
 */
import { useTaskActionContext } from "../../hooks/useTaskActionContext";
import { historySessionRefs } from "../../lib/sessionRef";
import { useUI } from "../../store/ui";
import type { SessionRef, Task } from "../../lib/types";

export function SessionsSection({
  task,
  projectId,
  onView,
}: {
  task: Task;
  projectId: string;
  onView: (task: Task, ref: SessionRef) => void;
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
                onClick={() => onView(task, ref)}
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
