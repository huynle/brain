/**
 * AutomationModal — wireframe-parity.
 *
 * Shows a single automation entry (trigger, status, action) and lets
 * the user Run now.
 */
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Modal } from "../common/Modal";
import { Loading } from "../common/Loading";
import { ErrorState } from "../common/ErrorState";
import { useModal } from "../../store/modal";
import { useAutomations } from "../../hooks/useAutomations";
import { executeAutomation, ApiError } from "../../lib/api";
import { useUI } from "../../store/ui";

export function AutomationModal(): JSX.Element {
  const target = useModal((s) => s.target);
  const close = useModal((s) => s.close);

  const automationId =
    (target?.automationId as string | undefined) ??
    (target?.id as string | undefined) ??
    "";
  const path = target?.path as string | undefined;
  const projectId = (target?.projectId as string | undefined) ?? "";

  const { automations, isLoading, error, refetch } = useAutomations(projectId);
  const queryClient = useQueryClient();
  const toast = useUI((s) => s.toast);

  const [running, setRunning] = useState(false);
  const [runError, setRunError] = useState<string | null>(null);

  const automation = automations.find(
    (a) => a.id === automationId || (path && a.path === path),
  );

  async function runNow() {
    if (!automation) return;
    setRunning(true);
    setRunError(null);
    try {
      const res = await executeAutomation(automation.path, projectId);
      if (res.skipped) {
        toast(
          `Automation skipped: ${res.message ?? "no work"}`,
          "info",
        );
      } else if (res.task_id) {
        toast(`Started — task ${res.task_id}`, "success");
      } else {
        toast("Automation queued", "success");
      }
      await queryClient.invalidateQueries({
        queryKey: ["v2", "automations", projectId],
      });
      refetch();
    } catch (err) {
      const msg =
        err instanceof ApiError
          ? err.message
          : (err as Error).message ?? "unknown";
      setRunError(msg);
      toast(`Run failed: ${msg}`, "error");
    } finally {
      setRunning(false);
    }
  }

  if (isLoading) {
    return (
      <Modal title="Automation" onClose={close}>
        <Loading label="Loading automations…" />
      </Modal>
    );
  }
  if (error) {
    return (
      <Modal title="Automation" onClose={close}>
        <ErrorState error={error} onRetry={refetch} />
      </Modal>
    );
  }
  if (!automation) {
    return (
      <Modal
        title={
          automationId ? `Automation not found: ${automationId}` : "Automation"
        }
        onClose={close}
      >
        <div style={{ color: "#9098a1" }}>Not in the current list.</div>
      </Modal>
    );
  }

  const action = (automation as { action?: unknown }).action;
  const actionStr =
    typeof action === "string"
      ? action
      : action
        ? JSON.stringify(action, null, 2)
        : null;

  return (
    <Modal
      title={automation.title || automation.id}
      onClose={close}
      footer={
        <>
          <button
            className="primary"
            onClick={() => void runNow()}
            disabled={running}
          >
            {running ? "Running…" : "Run now"}
          </button>
          <button onClick={close}>Close</button>
        </>
      }
    >
      <div className="kv-grid">
        <div className="k">Project</div>
        <div className="v">{projectId}</div>
        <div className="k">Id</div>
        <div className="v">
          <code>{automation.id}</code>
        </div>
        <div className="k">Status</div>
        <div className="v">{automation.status}</div>
        {automation.trigger?.type && (
          <>
            <div className="k">Trigger</div>
            <div className="v">
              {automation.trigger.type}
              {automation.trigger.event && ` · ${automation.trigger.event}`}
              {automation.schedule && ` · ${automation.schedule}`}
            </div>
          </>
        )}
        {automation.agent && (
          <>
            <div className="k">Agent</div>
            <div className="v">{automation.agent}</div>
          </>
        )}
        {automation.model && (
          <>
            <div className="k">Model</div>
            <div className="v">{automation.model}</div>
          </>
        )}
      </div>

      {runError && (
        <div style={{ color: "#d96060", fontSize: 11, marginTop: 8 }}>
          {runError}
        </div>
      )}

      {actionStr && (
        <>
          <h4
            style={{ margin: "12px 0 6px", color: "#f4b23a", fontSize: 11 }}
          >
            Action
          </h4>
          <pre
            style={{
              margin: 0,
              padding: 8,
              background: "#0a0c0e",
              border: "1px solid #1a1e22",
              borderRadius: 4,
              fontSize: 11,
              maxHeight: 260,
              overflow: "auto",
              whiteSpace: "pre-wrap",
              wordBreak: "break-word",
            }}
          >
            {actionStr}
          </pre>
        </>
      )}
    </Modal>
  );
}
