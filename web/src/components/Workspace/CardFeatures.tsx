/**
 * CardFeatures — wireframe-parity port.
 *
 * List of features for a project. Each rendered as a `.feat` with
 * head (name · life-badge · age · assign-chip · progress bar) plus
 * a description row and meta row (id · state · age · runner-warn ·
 * priority · checkout · target-branch).
 */
import { useModal } from "../../store/modal";
import { useWorkspace } from "../../store/workspace";
import { useUI } from "../../store/ui";
import { useRunners } from "../../hooks/useRunners";
import { useContextMenu } from "../common/ContextMenu";
import { beginDrag, endDrag } from "../../hooks/useDragDrop";
import { runFeature, summarizeRunFeatureResult } from "../../lib/api";
import type { DerivedFeature } from "../../lib/features";

const LIFECYCLE_TONE = {
  "in-progress": { tone: "active", label: "active" },
  blocked: { tone: "blocked", label: "blocked" },
  finished: { tone: "finished", label: "finished" },
  "mr-open": { tone: "mr", label: "MR open" },
  merged: { tone: "merged", label: "merged" },
} as const;

function featStateClass(f: DerivedFeature): string {
  if (f.lifecycle === "blocked") return "block";
  if (f.lifecycle === "merged" || f.lifecycle === "finished") return "done";
  return "busy";
}

export interface CardFeaturesProps {
  projectId: string;
  features: DerivedFeature[];
}

export function CardFeatures({
  projectId,
  features,
}: CardFeaturesProps): JSX.Element {
  const openModal = useModal((s) => s.open);
  const openFeatureDrawer = useWorkspace((s) => s.openFeatureDrawer);
  const featureAssignments = useWorkspace((s) => s.featureAssignments);
  const toast = useUI((s) => s.toast);
  const mergedExpanded = useWorkspace(
    (s) => s.mergedExpanded[projectId] ?? false,
  );
  const toggleMergedExpanded = useWorkspace((s) => s.toggleMergedExpanded);
  const { runners } = useRunners();
  const ctx = useContextMenu();

  const merged = features.filter((f) => f.lifecycle === "merged");
  const visible = mergedExpanded
    ? features
    : features.filter((f) => f.lifecycle !== "merged");

  if (features.length === 0) {
    return (
      <div style={{ color: "#6b757e", fontSize: 11, padding: "6px 0" }}>
        No features yet.
      </div>
    );
  }

  return (
    <div>
      {visible.map((f) => {
        const tone = LIFECYCLE_TONE[f.lifecycle];
        const runnerId = featureAssignments[f.id];
        const runner = runners.find((r) => r.runner_id === runnerId);
        const pct = Math.round(f.progress * 100);
        const stateClass = featStateClass(f);

        return (
          <div
            key={f.id}
            className={`feat ${stateClass}`}
            style={{ marginBottom: 8, cursor: "pointer" }}
          >
            <div
              className="feat-head"
              draggable
              onDragStart={(e) =>
                beginDrag(e, {
                  source: "feature-header",
                  kind: "assign",
                  target: {
                    projectId,
                    featureId: f.id,
                    currentRunnerId: runnerId,
                  },
                  title: f.name,
                })
              }
              onDragEnd={endDrag}
              onClick={(e) => {
                if (
                  (e.target as HTMLElement).closest(
                    "button, .caret, .assign-chip, a",
                  )
                )
                  return;
                openFeatureDrawer(projectId, f.id);
              }}
              onContextMenu={(e) => {
                e.preventDefault();
                const items: Array<{
                  id: string;
                  label: string;
                  onClick: () => void;
                }> = [
                  {
                    id: "meta",
                    label: "Feature details",
                    onClick: () =>
                      openModal("feature", { projectId, featureId: f.id }),
                  },
                  {
                    id: "run",
                    label: "Run feature now",
                    onClick: async () => {
                      try {
                        const r = await runFeature(projectId, f.id, false);
                        const { message, kind } = summarizeRunFeatureResult(r);
                        toast(message, kind);
                      } catch (err) {
                        toast(
                          `Run feature failed: ${err instanceof Error ? err.message : String(err)}`,
                          "error",
                        );
                      }
                    },
                  },
                  {
                    id: "plan",
                    label: "Open plan drawer",
                    onClick: () => openFeatureDrawer(projectId, f.id),
                  },
                ];
                // Resume as a first-class context-menu action when the
                // feature has any abandoned tasks. Opens the FeatureActions
                // modal at the menu view where Resume is one click away.
                if ((f.resumableCount ?? 0) > 0) {
                  items.push({
                    id: "resume",
                    label: `Resume ${f.resumableCount} abandoned task${f.resumableCount === 1 ? "" : "s"}`,
                    onClick: () =>
                      openModal("feature-actions", {
                        projectId,
                        featureId: f.id,
                      }),
                  });
                }
                if (f.prUrl) {
                  items.push({
                    id: "mr",
                    label: "Open merge request",
                    onClick: () =>
                      window.open(
                        f.prUrl,
                        "_blank",
                        "noopener,noreferrer",
                      ),
                  });
                }
                ctx.open(e.clientX, e.clientY, items);
              }}
            >
              <span className="name">{f.name}</span>
              <span className={`life-badge ${tone.tone}`}>{tone.label}</span>
              {runner ? (
                <span
                  className={`assign-chip ${runner.status !== "online" ? "warn" : ""}`}
                  title="Assigned runner"
                >
                  🖥 {runner.runner_id}
                </span>
              ) : (
                <span className="assign-chip empty">· unassigned ·</span>
              )}
              <span className="bar">
                <i style={{ width: `${pct}%` }} />
              </span>
              <span className="prog">
                {f.taskCount.completed}/{f.taskCount.total}
              </span>
            </div>
            <div
              style={{
                fontSize: 10.5,
                color: "#9098a1",
                padding: "2px 0 2px 6px",
              }}
            >
              {f.id} · {f.taskCount.total} tasks
              {f.prUrl && (
                <>
                  {" · "}
                  <a
                    href={f.prUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    style={{ color: "#6a8bff" }}
                  >
                    MR
                  </a>
                </>
              )}
            </div>
          </div>
        );
      })}

      {!mergedExpanded && merged.length > 0 && (
        <button
          onClick={() => toggleMergedExpanded(projectId)}
          style={{
            border: "1px dashed #22272c",
            padding: "5px 8px",
            width: "100%",
            textAlign: "left",
            color: "#6b757e",
            fontSize: 11,
            marginTop: 6,
          }}
        >
          ▸ {merged.length} merged feature{merged.length === 1 ? "" : "s"}
        </button>
      )}

      {ctx.menu}
    </div>
  );
}
