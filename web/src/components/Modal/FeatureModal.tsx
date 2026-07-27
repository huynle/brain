/**
 * FeatureModal — wireframe-parity.
 *
 * Uses the shared `Modal` primitive (which now renders wireframe
 * `.modal / .modal-head / .modal-body / .modal-foot` classes).
 */
import { useMemo } from "react";
import { Modal } from "../common/Modal";
import { useModal } from "../../store/modal";
import { useWorkspace } from "../../store/workspace";
import { useLive } from "../../lib/sse";
import { deriveFeatures } from "../../lib/features";
import type { Task } from "../../lib/types";

const LIFECYCLE_TONE = {
  "in-progress": { tone: "active", label: "active" },
  blocked: { tone: "blocked", label: "blocked" },
  finished: { tone: "finished", label: "finished" },
  "mr-open": { tone: "mr", label: "MR open" },
  merged: { tone: "merged", label: "merged" },
} as const;

const EMPTY_TASKS: readonly Task[] = Object.freeze([]);

export function FeatureModal(): JSX.Element {
  const target = useModal((s) => s.target);
  const openModal = useModal((s) => s.open);
  const close = useModal((s) => s.close);
  const openFeatureDrawer = useWorkspace((s) => s.openFeatureDrawer);

  const featureId =
    (target?.featureId as string | undefined) ??
    (target?.id as string | undefined) ??
    "";
  const projectId = (target?.projectId as string | undefined) ?? "";

  const tasks = useLive((s) => s.projects[projectId]?.tasks) ?? EMPTY_TASKS;
  const feature = useMemo(
    () => deriveFeatures(tasks, projectId).find((f) => f.id === featureId),
    [tasks, projectId, featureId],
  );

  const featureTasks = useMemo(
    () => tasks.filter((t) => t.feature_id === featureId),
    [tasks, featureId],
  );

  if (!feature) {
    return (
      <Modal
        title={featureId ? `Feature not found: ${featureId}` : "Feature"}
        onClose={close}
      >
        <div style={{ color: "#9098a1" }}>
          No matching feature in project <code>{projectId}</code>.
        </div>
      </Modal>
    );
  }

  const tone = LIFECYCLE_TONE[feature.lifecycle];
  const pct = Math.round(feature.progress * 100);

  return (
    <Modal
      title={
        <>
          {feature.name}{" "}
          <span className={`life-badge ${tone.tone}`} style={{ marginLeft: 8 }}>
            {tone.label}
          </span>
        </>
      }
      onClose={close}
      footer={
        <>
          <button
            onClick={() => {
              close();
              openFeatureDrawer(projectId, featureId);
            }}
          >
            Open drawer
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
        <div className="k">Feature id</div>
        <div className="v">{feature.id}</div>
        <div className="k">Progress</div>
        <div className="v">
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 8,
            }}
          >
            <div
              className="bar"
              style={{
                width: 120,
                height: 6,
                background: "#22272c",
                borderRadius: 3,
                overflow: "hidden",
              }}
            >
              <i
                style={{
                  display: "block",
                  height: "100%",
                  width: `${pct}%`,
                  background: "#6fca7d",
                }}
              />
            </div>
            <span>
              {feature.taskCount.completed}/{feature.taskCount.total} ({pct}%)
            </span>
          </div>
        </div>
        {feature.mergePolicy && (
          <>
            <div className="k">Merge policy</div>
            <div className="v">{feature.mergePolicy}</div>
          </>
        )}
        {feature.prUrl && (
          <>
            <div className="k">MR</div>
            <div className="v">
              <a
                href={feature.prUrl}
                target="_blank"
                rel="noopener noreferrer"
                style={{ color: "#6a8bff" }}
              >
                {feature.prUrl}
              </a>
            </div>
          </>
        )}
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

      <h4 style={{ margin: "12px 0 6px", color: "#f4b23a", fontSize: 11 }}>
        Tasks ({featureTasks.length})
      </h4>
      <div>
        {featureTasks.length === 0 && (
          <div style={{ color: "#6b757e", fontSize: 11 }}>No tasks.</div>
        )}
        {featureTasks.map((t) => (
          <div
            key={t.id}
            className="trow"
            onClick={() =>
              openModal("task", { projectId, taskId: t.id })
            }
          >
            <span className="glyph">▸</span>
            <span className="name">{t.title || t.id}</span>
            <span className="status">{t.status}</span>
            <span className="id">{t.id.slice(0, 6)}</span>
          </div>
        ))}
      </div>
    </Modal>
  );
}
