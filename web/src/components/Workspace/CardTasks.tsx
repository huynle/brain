/**
 * CardTasks — wireframe-parity port.
 *
 * Groups tasks by feature. Each feature shows:
 *   .feat[state]
 *     .feat-head (caret · name · life-badge · age · assign-chip · progress bar · % text)
 *     .trow × N (glyph · name · status · id)
 */
import { useMemo } from "react";
import { useModal } from "../../store/modal";
import { useWorkspace } from "../../store/workspace";
import { useUI } from "../../store/ui";
import { useRunners } from "../../hooks/useRunners";
import { useContextMenu } from "../common/ContextMenu";
import { beginDrag, endDrag } from "../../hooks/useDragDrop";
import { runOrTriggerTask, summarizeTriggerResults } from "../../lib/api";
import type { Task } from "../../lib/types";
import type { DerivedFeature } from "../../lib/features";

const LIFECYCLE_TONE = {
  "in-progress": { tone: "active", label: "active" },
  blocked: { tone: "blocked", label: "blocked" },
  finished: { tone: "finished", label: "finished" },
  "mr-open": { tone: "mr", label: "MR open" },
  merged: { tone: "merged", label: "merged" },
} as const;

function taskGlyph(status: Task["status"]): {
  glyph: string;
  cls: string;
} {
  switch (status) {
    case "in_progress":
      return { glyph: "▸", cls: "busy" };
    case "blocked":
      return { glyph: "✕", cls: "blk" };
    case "completed":
    case "validated":
      return { glyph: "✓", cls: "ok" };
    case "pending":
      return { glyph: "▪", cls: "" };
    default:
      return { glyph: "○", cls: "" };
  }
}

function featStateClass(f: DerivedFeature): string {
  if (f.lifecycle === "blocked") return "block";
  if (f.lifecycle === "merged" || f.lifecycle === "finished") return "done";
  return "busy";
}

export interface CardTasksProps {
  projectId: string;
  tasks: readonly Task[];
  features: DerivedFeature[];
}

export function CardTasks({
  projectId,
  tasks,
  features,
}: CardTasksProps): JSX.Element {
  const openModal = useModal((s) => s.open);
  const toast = useUI((s) => s.toast);
  const openFeatureDrawer = useWorkspace((s) => s.openFeatureDrawer);
  const featureAssignments = useWorkspace((s) => s.featureAssignments);
  const openInFocus = useWorkspace((s) => s.openInFocus);
  const { runners } = useRunners();
  const ctx = useContextMenu();

  // Group tasks by feature_id (using DerivedFeature order).
  const byFeat = useMemo(() => {
    const m = new Map<string, Task[]>();
    for (const t of tasks) {
      const key = t.feature_id ?? "__nofeat__";
      const arr = m.get(key);
      if (arr) arr.push(t);
      else m.set(key, [t]);
    }
    return m;
  }, [tasks]);

  const orphanTasks = byFeat.get("__nofeat__") ?? [];

  return (
    <div>
      {features.map((f) => {
        const items = byFeat.get(f.id) ?? [];
        const stateClass = featStateClass(f);
        const tone = LIFECYCLE_TONE[f.lifecycle];
        const runnerId = featureAssignments[f.id];
        const runner = runners.find((r) => r.runner_id === runnerId);
        const pct = Math.round(f.progress * 100);

        return (
          <div key={f.id} className={`feat ${stateClass}`}>
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
                  (e.target as HTMLElement).closest("button, .caret, .assign-chip")
                )
                  return;
                openFeatureDrawer(projectId, f.id);
              }}
              onContextMenu={(e) => {
                e.preventDefault();
                ctx.open(e.clientX, e.clientY, [
                  {
                    id: "meta",
                    label: "Feature details",
                    onClick: () =>
                      openModal("feature", {
                        projectId,
                        featureId: f.id,
                      }),
                  },
                  {
                    id: "plan",
                    label: "Open plan drawer",
                    onClick: () => openFeatureDrawer(projectId, f.id),
                  },
                ]);
              }}
            >
              <span className="caret">▾</span>
              <span className="name">{f.name}</span>
              <span className={`life-badge ${tone.tone}`}>{tone.label}</span>
              {runner ? (
                <span
                  className={`assign-chip ${runner.status !== "online" ? "warn" : ""}`}
                  title="Click to unassign"
                >
                  {runner.status !== "online"
                    ? `⚠ ${runner.runner_id}`
                    : `🖥 ${runner.runner_id}`}
                </span>
              ) : (
                <span
                  className="assign-chip empty"
                  title="Drag onto a runner to assign"
                >
                  · unassigned ·
                </span>
              )}
              <span className="bar">
                <i style={{ width: `${pct}%` }} />
              </span>
              <span className="prog">{pct}%</span>
            </div>
            {items.map((t) => {
              const { glyph, cls } = taskGlyph(t.status);
              return (
                <div
                  key={t.id}
                  className="trow"
                  onClick={() =>
                    openModal("task", { projectId, taskId: t.id })
                  }
                  onContextMenu={(e) => {
                    e.preventDefault();
                    const items: Array<{
                      id: string;
                      label: string;
                      onClick: () => void;
                    }> = [
                      {
                        id: "modal",
                        label: "Task details",
                        onClick: () =>
                          openModal("task", { projectId, taskId: t.id }),
                      },
                      {
                        id: "run",
                        label: "Run task now",
                        onClick: async () => {
                          try {
                            const r = await runOrTriggerTask(
                              projectId,
                              t.id,
                              false,
                            );
                            const { message, kind } = summarizeTriggerResults([r]);
                            toast(message, kind);
                          } catch (err) {
                            toast(
                              `Run failed: ${err instanceof Error ? err.message : String(err)}`,
                              "error",
                            );
                          }
                        },
                      },
                      {
                        id: "focus-detail",
                        label: "Open in focus pane",
                        onClick: () =>
                          openInFocus(
                            "task-detail",
                            { projectId, taskId: t.id },
                            t.title || t.id,
                          ),
                      },
                      {
                        id: "focus-logs",
                        label: "Open logs in focus pane",
                        onClick: () =>
                          openInFocus(
                            "logs",
                            { projectId, taskId: t.id },
                            `Logs ${t.id.slice(0, 8)}`,
                          ),
                      },
                    ];
                    // Surface Resume as a context-menu item when the task
                    // looks resumable — otherwise the affordance is buried
                    // two clicks deep in TaskModal → Actions… for the
                    // exact case (abandoned task) where users need it fast.
                    if (t.is_abandoned || t.resume_requested) {
                      items.push({
                        id: "resume",
                        label: t.is_abandoned
                          ? "Resume abandoned task"
                          : "Resume (already requested)",
                        onClick: () =>
                          openModal("task-actions", {
                            projectId,
                            taskId: t.id,
                          }),
                      });
                    }
                    ctx.open(e.clientX, e.clientY, items);
                  }}
                  draggable
                  onDragStart={(e) =>
                    beginDrag(e, {
                      source: "task-row",
                      kind: "task-detail",
                      target: { projectId, taskId: t.id },
                      title: t.title || t.id,
                    })
                  }
                  onDragEnd={endDrag}
                >
                  <span className={`glyph ${cls}`}>{glyph}</span>
                  <span className="name">{t.title || t.id}</span>
                  <span className="status">{t.status}</span>
                  <span className="id">{t.id.slice(0, 6)}</span>
                </div>
              );
            })}
          </div>
        );
      })}

      {orphanTasks.length > 0 && (
        <div className="feat">
          <div className="feat-head">
            <span className="name" style={{ color: "#6b757e" }}>
              No feature
            </span>
            <span className="age">{orphanTasks.length} tasks</span>
          </div>
          {orphanTasks.map((t) => {
            const { glyph, cls } = taskGlyph(t.status);
            return (
              <div
                key={t.id}
                className="trow"
                onClick={() =>
                  openModal("task", { projectId, taskId: t.id })
                }
              >
                <span className={`glyph ${cls}`}>{glyph}</span>
                <span className="name">{t.title || t.id}</span>
                <span className="status">{t.status}</span>
                <span className="id">{t.id.slice(0, 6)}</span>
              </div>
            );
          })}
        </div>
      )}

      {features.length === 0 && orphanTasks.length === 0 && (
        <div style={{ color: "#6b757e", fontSize: 11, padding: "6px 0" }}>
          No tasks yet.
        </div>
      )}

      {ctx.menu}
    </div>
  );
}
