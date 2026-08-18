/**
 * CardTasks — wireframe-parity port.
 *
 * Groups tasks by feature. Each feature shows:
 *   .feat[state]
 *     .feat-head (caret · name · life-badge · age · assign-chip · progress bar · % text)
 *     .trow × N (glyph · name · status · id)
 *
 * Within a feature the rows form a dependency tree rather than a flat
 * list: a task that depends on another renders indented beneath it, so
 * a plan reads top-down in execution order. Features whose tasks have
 * no dependencies look exactly as they did before — the forest is then
 * all roots. See `lib/taskTree` for the edge rules.
 *
 * Every row's verbs come from `lib/actions` via `useRowActions`, so
 * right-click, long-press and keyboard all offer the identical set —
 * including the ungrouped "No feature" rows, which previously had no
 * action affordance at all.
 */
import { useMemo } from "react";
import { useModal } from "../../store/modal";
import { useSelection } from "../../store/selection";
import { useWorkspace } from "../../store/workspace";
import { useRunners } from "../../hooks/useRunners";
import { useRowActions } from "../../hooks/useRowActions";
import { useTaskActionContext } from "../../hooks/useTaskActionContext";
import { useFeatureActionContext } from "../../hooks/useFeatureActionContext";
import { DepGuide } from "../common/DepGuide";
import { beginDrag, endDrag } from "../../hooks/useDragDrop";
import { buildTaskActions } from "../../lib/actions/taskActions";
import { buildFeatureActions } from "../../lib/actions/featureActions";
import { buildTaskForest } from "../../lib/taskTree";
import { flattenDepForest, type DepRow } from "../../lib/depTree";
import type { Task } from "../../lib/types";
import type { DerivedFeature } from "../../lib/features";

const LIFECYCLE_TONE = {
  "in-progress": { tone: "active", label: "active" },
  blocked: { tone: "blocked", label: "blocked" },
  finished: { tone: "finished", label: "finished" },
  "mr-open": { tone: "mr", label: "MR open" },
  merged: { tone: "merged", label: "merged" },
} as const;

function taskGlyph(status: Task["status"], isAbandoned = false): {
  glyph: string;
  cls: string;
} {
  // Abandoned tasks override the status glyph with a "⟳" (recovery) marker
  // in amber. Distinct from ✕ (blocked) and ▸ (in-progress) so a user
  // scanning the row can spot resumable work without opening the modal.
  if (isAbandoned) {
    return { glyph: "⟳", cls: "abandoned" };
  }
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

/** Bucket key for tasks with no feature_id. Not a legal feature id, so
 *  it cannot collide with a real one. */
const NO_FEATURE = "__nofeat__";

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
  const openFeatureDrawer = useWorkspace((s) => s.openFeatureDrawer);
  const featureAssignments = useWorkspace((s) => s.featureAssignments);
  const archivedExpanded = useWorkspace(
    (s) => s.archivedExpanded[projectId] ?? false,
  );
  const toggleArchivedExpanded = useWorkspace((s) => s.toggleArchivedExpanded);
  const statusFilter = useWorkspace((s) => s.statusFilter);
  const { runners } = useRunners();

  const taskCtx = useTaskActionContext(projectId);
  const featureCtx = useFeatureActionContext(projectId);
  const { rowProps, overlays } = useRowActions();

  // Subscribed (not getState) so checkboxes and the "marked" tint react
  // to every toggle, from any surface — checkbox, `v` key, or menu.
  const selProjectId = useSelection((s) => s.projectId);
  const selTaskIds = useSelection((s) => s.taskIds);
  const selFeatureIds = useSelection((s) => s.featureIds);
  const toggleTaskSel = useSelection((s) => s.toggleTask);
  const toggleFeatureSel = useSelection((s) => s.toggleFeature);
  const selScoped = selProjectId === projectId;
  // Selection mode: once anything in this project is marked, every row
  // shows its checkbox. Until then boxes appear only on hover/focus.
  const selActive =
    selScoped && (selTaskIds.size > 0 || selFeatureIds.size > 0);

  // Archived tasks leave the default rows entirely (mirroring the server
  // rule that they count toward nothing) and collect in a fold at the
  // bottom, modeled on the merged-features fold in CardFeatures. The
  // sidebar "Archived" filter forces the fold open — the user asked to
  // see exactly these rows.
  const archivedTasks = useMemo(
    () => tasks.filter((t) => t.status === "archived"),
    [tasks],
  );
  const archivedForced = statusFilter === "archived";
  const showArchived =
    archivedTasks.length > 0 && (archivedExpanded || archivedForced);

  // Group tasks by feature_id (using DerivedFeature order), then turn
  // each bucket into dependency-ordered rows. Dependency edges are
  // resolved per bucket, so a cross-feature dep does not drag a task
  // out of its own feature — it simply stays a root here and shows up
  // in the feature-level tree on the Features tab instead.
  const rowsByFeat = useMemo(() => {
    const buckets = new Map<string, Task[]>();
    for (const t of tasks) {
      if (t.status === "archived") continue; // rendered in the fold below
      const key = t.feature_id ?? NO_FEATURE;
      const arr = buckets.get(key);
      if (arr) arr.push(t);
      else buckets.set(key, [t]);
    }
    const m = new Map<string, DepRow<Task>[]>();
    for (const [key, bucket] of buckets) {
      m.set(key, flattenDepForest(buildTaskForest(bucket)));
    }
    return m;
  }, [tasks]);

  const orphanRows = rowsByFeat.get(NO_FEATURE) ?? [];

  // Archived rows keep their dependency ordering but render in one flat
  // group: an all-archived feature derives no DerivedFeature (it left the
  // lanes), so there is no per-feature header to hang them under.
  const archivedRows = useMemo(
    () => flattenDepForest(buildTaskForest(archivedTasks)),
    [archivedTasks],
  );

  /** One task row, identical whether it sits under a feature or not. */
  const renderTaskRow = (row: DepRow<Task>) => {
    const t = row.node.item;
    const { glyph, cls } = taskGlyph(t.status, !!t.is_abandoned);
    const label = t.title || t.id;
    const actions = buildTaskActions(t, taskCtx);
    const marked = selScoped && selTaskIds.has(t.id);

    return (
      <div
        key={t.id}
        className={`trow${marked ? " marked" : ""}`}
        {...rowProps(
          actions,
          label,
          () => openModal("task", { projectId, taskId: t.id }),
          { tapSelects: selActive },
        )}
        onClick={(e) => {
          if ((e.target as HTMLElement).closest(".selbox")) return;
          openModal("task", { projectId, taskId: t.id });
        }}
        draggable
        onDragStart={(e) =>
          beginDrag(e, {
            source: "task-row",
            kind: "task-detail",
            target: { projectId, taskId: t.id },
            title: label,
          })
        }
        onDragEnd={endDrag}
      >
        {/* Checkbox and status glyph share one grid cell; CSS swaps them
            on hover/focus, and `boxed` pins the checkbox while marked or
            in selection mode. Keeps the 4-column row layout intact. */}
        <span className={`glyph-slot${marked || selActive ? " boxed" : ""}`}>
          <span
            className={`selbox${marked ? " on" : ""}`}
            role="checkbox"
            aria-checked={marked}
            aria-label={`Select ${label}`}
            onClick={(e) => {
              e.stopPropagation();
              toggleTaskSel(projectId, t.id);
            }}
          />
          <span className={`glyph ${cls}`}>{glyph}</span>
        </span>
        <span className="name">
          <DepGuide
            prefix={row.prefix}
            inCycle={row.node.inCycle}
            extraDeps={row.node.extraDeps}
            extraLabel="tasks"
          />
          {label}
        </span>
        <span className="status">{t.status}</span>
        <span className="id">{t.id.slice(0, 6)}</span>
      </div>
    );
  };

  return (
    <div>
      {features.map((f) => {
        const rows = rowsByFeat.get(f.id) ?? [];
        const stateClass = featStateClass(f);
        const tone = LIFECYCLE_TONE[f.lifecycle];
        const runnerId = featureAssignments[f.id];
        const runner = runners.find((r) => r.runner_id === runnerId);
        const pct = Math.round(f.progress * 100);
        const featureActions = buildFeatureActions(f, featureCtx);
        const featMarked = selScoped && selFeatureIds.has(f.id);

        return (
          <div key={f.id} className={`feat ${stateClass}`}>
            <div
              className={`feat-head${featMarked ? " marked" : ""}`}
              {...rowProps(
                featureActions,
                f.name,
                () => openFeatureDrawer(projectId, f.id),
                { tapSelects: selActive },
              )}
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
                    "button, .caret, .assign-chip, .selbox",
                  )
                )
                  return;
                openFeatureDrawer(projectId, f.id);
              }}
            >
              {/* The caret's slot doubles as the feature checkbox on
                  hover/selection — no extra column, no layout shift. */}
              <span
                className={`glyph-slot${featMarked || selActive ? " boxed" : ""}`}
              >
                <span
                  className={`selbox${featMarked ? " on" : ""}`}
                  role="checkbox"
                  aria-checked={featMarked}
                  aria-label={`Select feature ${f.name}`}
                  onClick={(e) => {
                    e.stopPropagation();
                    toggleFeatureSel(projectId, f.id);
                  }}
                />
                <span className="caret">▾</span>
              </span>
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
            {rows.map(renderTaskRow)}
          </div>
        );
      })}

      {orphanRows.length > 0 && (
        <div className="feat">
          <div className="feat-head">
            <span className="name" style={{ color: "#6b757e" }}>
              No feature
            </span>
            <span className="age">{orphanRows.length} tasks</span>
          </div>
          {orphanRows.map(renderTaskRow)}
        </div>
      )}

      {showArchived && (
        <div className="feat done">
          <div className="feat-head">
            <span className="name" style={{ color: "#6b757e" }}>
              Archived
            </span>
            <span className="age">{archivedTasks.length} tasks</span>
          </div>
          {archivedRows.map(renderTaskRow)}
        </div>
      )}

      {/* Same dashed expander as the merged-features fold. Hidden while
          the sidebar filter forces the rows open — a toggle that cannot
          take effect would just lie. */}
      {archivedTasks.length > 0 && !archivedForced && (
        <button
          onClick={() => toggleArchivedExpanded(projectId)}
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
          {archivedExpanded ? "▾" : "▸"} {archivedTasks.length} archived task
          {archivedTasks.length === 1 ? "" : "s"}
        </button>
      )}

      {features.length === 0 &&
        orphanRows.length === 0 &&
        archivedTasks.length === 0 && (
          <div style={{ color: "#6b757e", fontSize: 11, padding: "6px 0" }}>
            No tasks yet.
          </div>
        )}

      {overlays}
    </div>
  );
}
