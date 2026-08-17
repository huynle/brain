/**
 * FeatureAssignModal — pick a runner for a feature (ModalKind
 * "feature-assign").
 *
 * Before this modal, assignment was drag-and-drop only (feature header →
 * sidebar runner row), which is invisible to keyboard and touch users and
 * undiscoverable for everyone else. The "Assign runner…" registry verb
 * opens this instead: every runner, online state visible, one click/tap.
 *
 * Server semantics (see lib/api.ts assignment section): first-time writes
 * use intent "assign"; moving between runners needs "reassign", and the
 * server 409s an "assign" against a feature already assigned elsewhere.
 * The local mirror can be stale, so a 409 on "assign" is retried once as
 * "reassign" — the user just named the runner they want, which IS the
 * reassignment intent.
 */
import { useState } from "react";

import { Modal } from "../common/Modal";
import { useModal } from "../../store/modal";
import { useWorkspace } from "../../store/workspace";
import { useUI } from "../../store/ui";
import { useRunners } from "../../hooks/useRunners";
import {
  ApiError,
  assignFeatureToRunner,
  clearFeatureAssignment,
} from "../../lib/api";

export function FeatureAssignModal(): JSX.Element {
  const target = useModal((s) => s.target);
  const close = useModal((s) => s.close);
  const toast = useUI((s) => s.toast);
  const assignLocal = useWorkspace((s) => s.assignFeature);
  const unassignLocal = useWorkspace((s) => s.unassignFeature);
  const featureAssignments = useWorkspace((s) => s.featureAssignments);
  const { runners, isLoading } = useRunners();

  const featureId = (target?.featureId as string | undefined) ?? "";
  const projectId = (target?.projectId as string | undefined) ?? "";
  const currentRunnerId = featureAssignments[featureId];

  const [busy, setBusy] = useState(false);

  async function assign(runnerId: string) {
    if (runnerId === currentRunnerId) {
      toast(`Already assigned to ${runnerId}`, "info");
      return;
    }
    setBusy(true);
    // Optimistic mirror update; the runners_update SSE reconciles, and a
    // failure rolls back below.
    assignLocal(featureId, runnerId);
    try {
      const intent = currentRunnerId ? "reassign" : "assign";
      try {
        await assignFeatureToRunner(projectId, featureId, runnerId, { intent });
      } catch (err) {
        // Stale mirror: server says the feature is assigned elsewhere.
        // Escalate to reassign once — that is what the click meant.
        if (
          intent === "assign" &&
          err instanceof ApiError &&
          err.status === 409
        ) {
          await assignFeatureToRunner(projectId, featureId, runnerId, {
            intent: "reassign",
          });
        } else {
          throw err;
        }
      }
      toast(`Assigned ${featureId} → ${runnerId}`, "success");
      close();
    } catch (err) {
      if (currentRunnerId) assignLocal(featureId, currentRunnerId);
      else unassignLocal(featureId);
      toast(
        `Assign failed: ${err instanceof Error ? err.message : String(err)}`,
        "error",
      );
    } finally {
      setBusy(false);
    }
  }

  async function clear() {
    setBusy(true);
    const previous = currentRunnerId;
    unassignLocal(featureId);
    try {
      await clearFeatureAssignment(projectId, featureId);
      toast(`Cleared runner assignment for ${featureId}`, "success");
      close();
    } catch (err) {
      if (previous) assignLocal(featureId, previous);
      toast(
        `Clear failed: ${err instanceof Error ? err.message : String(err)}`,
        "error",
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title={`Assign runner: ${featureId}`}
      onClose={close}
      footer={
        <>
          <span className="faint">Esc closes</span>
          {currentRunnerId && (
            <button onClick={() => void clear()} disabled={busy}>
              Clear assignment
            </button>
          )}
          <button
            className="primary"
            style={{ marginLeft: "auto" }}
            onClick={close}
            disabled={busy}
          >
            Done
          </button>
        </>
      }
    >
      <p className="muted" style={{ marginTop: 0 }}>
        Project <strong>{projectId}</strong>
        {currentRunnerId ? (
          <>
            {" · currently assigned to "}
            <strong>{currentRunnerId}</strong>
          </>
        ) : (
          " · currently unassigned"
        )}
      </p>

      {isLoading && runners.length === 0 && (
        <div className="muted">Loading runners…</div>
      )}
      {!isLoading && runners.length === 0 && (
        <div className="muted">
          No runners registered. Start one with <code>brain start</code>.
        </div>
      )}

      <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
        {runners.map((r) => {
          const isCurrent = r.runner_id === currentRunnerId;
          const online = r.status === "online";
          return (
            <button
              key={r.runner_id}
              type="button"
              onClick={() => void assign(r.runner_id)}
              disabled={busy}
              title={
                online
                  ? undefined
                  : `Runner is ${r.status} — assignment is allowed but nothing will execute until it returns`
              }
              style={{
                display: "flex",
                alignItems: "center",
                gap: 8,
                padding: "6px 8px",
                textAlign: "left",
                background: isCurrent ? "#f4b23a22" : "transparent",
                color: isCurrent ? "#f4b23a" : undefined,
                borderColor: isCurrent ? "#f4b23a" : undefined,
              }}
            >
              <span className={`dot ${online ? "on" : ""}`} />
              <strong>{r.runner_id}</strong>
              <span className="faint">
                {r.hostname} · {r.status}
                {typeof r.active_tasks === "number"
                  ? ` · ${r.active_tasks}/${r.max_parallel} tasks`
                  : ""}
              </span>
              {isCurrent && <span style={{ marginLeft: "auto" }}>✓</span>}
            </button>
          );
        })}
      </div>
    </Modal>
  );
}
