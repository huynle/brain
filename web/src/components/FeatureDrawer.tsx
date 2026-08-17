/**
 * FeatureDrawer — wireframe-parity port of `renderFeatureDrawer`.
 *
 * Right-side slide-in showing feature detail, tasks, and links.
 * Opened via `useWorkspace.openFeatureDrawer(pid, fid)`; closed via
 * the × button or Esc.
 *
 * The header carries the full feature verb set via `useRowActions`
 * (right-click / long-press / keyboard), same as CardFeatures rows.
 * The assign buttons call the real assignment API — they used to write
 * only the local zustand mirror, which looked assigned while the server
 * was never told.
 */
import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { useWorkspace } from "../store/workspace";
import { useModal } from "../store/modal";
import { useUI } from "../store/ui";
import { useLive } from "../lib/sse";
import { useRunners } from "../hooks/useRunners";
import { useRowActions } from "../hooks/useRowActions";
import { useFeatureActionContext } from "../hooks/useFeatureActionContext";
import {
  ApiError,
  assignFeatureToRunner,
  clearFeatureAssignment,
} from "../lib/api";
import { buildFeatureActions } from "../lib/actions/featureActions";
import { deriveFeatures } from "../lib/features";
import type { Task } from "../lib/types";

const EMPTY_TASKS: readonly Task[] = Object.freeze([]);

const LIFECYCLE_TONE = {
  "in-progress": { tone: "active", label: "active" },
  blocked: { tone: "blocked", label: "blocked" },
  finished: { tone: "finished", label: "finished" },
  "mr-open": { tone: "mr", label: "MR open" },
  merged: { tone: "merged", label: "merged" },
} as const;

export function FeatureDrawer(): JSX.Element | null {
  const drawer = useWorkspace((s) => s.featureDrawer);
  const close = useWorkspace((s) => s.closeFeatureDrawer);
  const assignFeature = useWorkspace((s) => s.assignFeature);
  const unassignFeature = useWorkspace((s) => s.unassignFeature);
  const featureAssignments = useWorkspace((s) => s.featureAssignments);
  const openModal = useModal((s) => s.open);
  const toast = useUI((s) => s.toast);
  const { runners } = useRunners();
  const [assignBusy, setAssignBusy] = useState(false);

  const featureCtx = useFeatureActionContext(drawer?.projectId ?? "");
  const { rowProps, overlays } = useRowActions();

  // Guard against returning a fresh [] on every render when no drawer
  // is open — that triggers zustand "getSnapshot should be cached"
  // and Maximum update depth exceeded.
  const projectTasks = useLive((s) =>
    drawer ? s.projects[drawer.projectId]?.tasks : undefined,
  );
  const tasks = projectTasks ?? EMPTY_TASKS;

  useEffect(() => {
    if (!drawer) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [drawer, close]);

  if (!drawer) return null;
  if (typeof document === "undefined") return null;

  const derived = deriveFeatures(tasks, drawer.projectId);
  const feature = derived.find((f) => f.id === drawer.featureId);

  if (!feature) {
    return createPortal(
      <aside className="feature-drawer">
        <button className="drawer-close" onClick={close}>
          ×
        </button>
        <div style={{ padding: 20 }}>Feature not found.</div>
      </aside>,
      document.body,
    );
  }

  const tone = LIFECYCLE_TONE[feature.lifecycle];
  const runnerId = featureAssignments[feature.id];
  const runner = runners.find((r) => r.runner_id === runnerId);
  const featureTasks = tasks.filter((t) => t.feature_id === feature.id);
  const actions = buildFeatureActions(feature, featureCtx);

  /** Assign for real: server first-class, local mirror for optimism. */
  const doAssign = async (targetRunnerId: string) => {
    if (targetRunnerId === runnerId) return;
    setAssignBusy(true);
    const previous = runnerId;
    assignFeature(feature.id, targetRunnerId);
    try {
      const intent = previous ? "reassign" : "assign";
      try {
        await assignFeatureToRunner(
          drawer.projectId,
          feature.id,
          targetRunnerId,
          { intent },
        );
      } catch (err) {
        // The local mirror can lag the server. A 409 on "assign" means
        // the server has it assigned elsewhere — the click named the
        // runner the user wants, so escalate to reassign once.
        if (
          intent === "assign" &&
          err instanceof ApiError &&
          err.status === 409
        ) {
          await assignFeatureToRunner(
            drawer.projectId,
            feature.id,
            targetRunnerId,
            { intent: "reassign" },
          );
        } else {
          throw err;
        }
      }
      toast(`Assigned ${feature.id} → ${targetRunnerId}`, "success");
    } catch (err) {
      if (previous) assignFeature(feature.id, previous);
      else unassignFeature(feature.id);
      toast(
        `Assign failed: ${err instanceof Error ? err.message : String(err)}`,
        "error",
      );
    } finally {
      setAssignBusy(false);
    }
  };

  const doClear = async () => {
    if (!runnerId) return;
    setAssignBusy(true);
    const previous = runnerId;
    unassignFeature(feature.id);
    try {
      await clearFeatureAssignment(drawer.projectId, feature.id);
      toast(`Cleared runner assignment for ${feature.id}`, "success");
    } catch (err) {
      assignFeature(feature.id, previous);
      toast(
        `Clear failed: ${err instanceof Error ? err.message : String(err)}`,
        "error",
      );
    } finally {
      setAssignBusy(false);
    }
  };

  return createPortal(
    <aside className="feature-drawer">
      <div className="drawer-head" {...rowProps(actions, feature.name)}>
        <div>
          <div className="drawer-kicker">
            {drawer.projectId} · {feature.id}
          </div>
          <h3>{feature.name}</h3>
        </div>
        <button className="drawer-close" onClick={close}>
          ×
        </button>
      </div>

      <div className="drawer-actions">
        <button
          className="primary"
          onClick={() =>
            openModal("feature", {
              projectId: drawer.projectId,
              featureId: feature.id,
            })
          }
        >
          Full detail
        </button>
        {feature.prUrl && (
          <a
            href={feature.prUrl}
            target="_blank"
            rel="noopener noreferrer"
            style={{
              padding: "4px 10px",
              border: "1px solid #2a2f35",
              borderRadius: 4,
              color: "#6a8bff",
              textDecoration: "none",
              fontSize: 11,
            }}
          >
            Open MR
          </a>
        )}
      </div>

      <div className="drawer-section">
        <h4>Status</h4>
        <div className="kv-grid">
          <div className="k">Lifecycle</div>
          <div className="v">
            <span className={`life-badge ${tone.tone}`}>{tone.label}</span>
          </div>
          <div className="k">Progress</div>
          <div className="v">
            {feature.taskCount.completed}/{feature.taskCount.total} (
            {Math.round(feature.progress * 100)}%)
          </div>
          <div className="k">Runner</div>
          <div className="v">
            {runner ? runner.runner_id : "unassigned"}
          </div>
          {feature.finishedAt && (
            <>
              <div className="k">Finished</div>
              <div className="v">{feature.finishedAt}</div>
            </>
          )}
          {feature.mergedAt && (
            <>
              <div className="k">Merged</div>
              <div className="v">{feature.mergedAt}</div>
            </>
          )}
        </div>
      </div>

      <div className="drawer-section">
        <h4>Assign to runner</h4>
        <div style={{ display: "flex", flexWrap: "wrap", gap: 4 }}>
          {runners
            .filter((r) => r.status === "online")
            .map((r) => (
              <button
                key={r.runner_id}
                onClick={() => void doAssign(r.runner_id)}
                disabled={assignBusy}
                style={{
                  background: r.runner_id === runnerId ? "#f4b23a22" : undefined,
                  color: r.runner_id === runnerId ? "#f4b23a" : undefined,
                  borderColor:
                    r.runner_id === runnerId ? "#f4b23a" : undefined,
                }}
              >
                {r.runner_id === runnerId ? "✓ " : ""}
                {r.runner_id}
              </button>
            ))}
          {runnerId && (
            <button onClick={() => void doClear()} disabled={assignBusy}>
              Clear
            </button>
          )}
        </div>
      </div>

      <div className="drawer-section">
        <h4>Tasks ({featureTasks.length})</h4>
        {featureTasks.length === 0 && (
          <div style={{ color: "#6b757e", fontSize: 11 }}>
            No tasks yet.
          </div>
        )}
        {featureTasks.map((t) => (
          <div
            key={t.id}
            className="drawer-task"
            onClick={() =>
              openModal("task", {
                projectId: drawer.projectId,
                taskId: t.id,
              })
            }
          >
            <span>{t.status}</span>
            <b>{t.title || t.id}</b>
          </div>
        ))}
      </div>

      {overlays}
    </aside>,
    document.body,
  );
}
