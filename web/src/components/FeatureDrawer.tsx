/**
 * FeatureDrawer — right-side slide-in that hosts EITHER a feature or a
 * task, driven by the `drawer` discriminated union in the workspace
 * store.
 *
 * - Feature mode (double-click a feature) is the original wireframe
 *   port: feature detail, assign, and the member task list.
 * - Task mode (double-click / Enter a task row) shows the same KV
 *   metadata grid + Content body the Task modal renders, with a
 *   "Full detail" button that opens the full Task modal — so the modal
 *   stays reachable while the drawer is the default detail surface.
 *
 * The aside is drag-resizable from its LEFT edge; the chosen width is
 * persisted (`drawerWidth` in the store) and applied via a CSS var.
 *
 * Hook discipline: EVERY hook runs unconditionally at the top (guarding
 * on `drawer` being null), and only the RENDERED JSX branches on
 * `drawer.kind`. No conditional hook calls.
 */
import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useWorkspace } from "../store/workspace";
import { useModal } from "../store/modal";
import { useSelection } from "../store/selection";
import { useUI } from "../store/ui";
import { useLive } from "../lib/sse";
import { useRunners } from "../hooks/useRunners";
import { useRowActions } from "../hooks/useRowActions";
import { useFeatureActionContext } from "../hooks/useFeatureActionContext";
import { useTaskActionContext } from "../hooks/useTaskActionContext";
import {
  ApiError,
  assignFeatureToRunner,
  clearFeatureAssignment,
} from "../lib/api";
import { buildFeatureActions } from "../lib/actions/featureActions";
import { buildSelectionActions } from "../lib/actions/selectionActions";
import { buildTaskActions } from "../lib/actions/taskActions";
import { taskHoldReason } from "../lib/pause";
import { usePauseState } from "../hooks/usePauseState";
import { isRangeKey } from "../lib/selection";
import { deriveFeatures } from "../lib/features";
import { TaskKvGrid } from "./Modal/TaskKvGrid";
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
  const drawer = useWorkspace((s) => s.drawer);
  const close = useWorkspace((s) => s.closeFeatureDrawer);
  const drawerWidth = useWorkspace((s) => s.drawerWidth);
  const setDrawerWidth = useWorkspace((s) => s.setDrawerWidth);
  const assignFeature = useWorkspace((s) => s.assignFeature);
  const unassignFeature = useWorkspace((s) => s.unassignFeature);
  const featureAssignments = useWorkspace((s) => s.featureAssignments);
  const openModal = useModal((s) => s.open);
  const toast = useUI((s) => s.toast);
  const { runners } = useRunners();
  const { pause } = usePauseState();
  const [assignBusy, setAssignBusy] = useState(false);
  // Archived-tasks fold. Local (not the persisted per-project toggle):
  // the drawer is transient and scoped to one feature, so a sticky
  // cross-feature expansion would surprise more than it helps.
  const [archivedOpen, setArchivedOpen] = useState(false);

  const projectId = drawer?.projectId ?? "";
  const featureCtx = useFeatureActionContext(projectId);
  const taskCtx = useTaskActionContext(projectId);
  const { rowProps, overlays } = useRowActions();

  // Same selection model as CardTasks: drawer rows carry the Select
  // verb, so they participate in selection mode and shift-click
  // ranges — with the drawer's own visible row order.
  const selProjectId = useSelection((s) => s.projectId);
  const selTaskIds = useSelection((s) => s.taskIds);
  const selFeatureIds = useSelection((s) => s.featureIds);
  const toggleTaskSel = useSelection((s) => s.toggleTask);
  const rangeTaskSel = useSelection((s) => s.rangeTask);
  const requestVerb = useSelection((s) => s.requestVerb);
  const clearSel = useSelection((s) => s.clear);

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

  // ─── left-edge drag-resize ────────────────────────────────────────
  const resizerRef = useRef<HTMLDivElement | null>(null);
  const startResize = useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      e.preventDefault();
      const handle = e.currentTarget;
      try {
        handle.setPointerCapture(e.pointerId);
      } catch {
        /* jsdom / non-capturing envs — safe to ignore */
      }
      document.body.classList.add("drawer-resizing");
      handle.classList.add("dragging");

      const onMove = (ev: PointerEvent) => {
        // The drawer is anchored to the right edge, so its width is the
        // distance from the pointer to the viewport's right edge.
        setDrawerWidth(window.innerWidth - ev.clientX);
      };
      const onUp = (ev: PointerEvent) => {
        try {
          handle.releasePointerCapture(ev.pointerId);
        } catch {
          /* ignore */
        }
        document.body.classList.remove("drawer-resizing");
        handle.classList.remove("dragging");
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onUp);
      };
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onUp);
    },
    [setDrawerWidth],
  );

  // Belt-and-braces cleanup: if the drawer unmounts mid-drag, drop the
  // body class so the page doesn't get stuck with col-resize / no-select.
  useEffect(() => {
    return () => {
      if (typeof document !== "undefined") {
        document.body.classList.remove("drawer-resizing");
      }
    };
  }, []);

  if (!drawer) return null;
  if (typeof document === "undefined") return null;

  const asideStyle = { ["--drawer-w" as never]: `${drawerWidth}px` } as React.CSSProperties;
  const Resizer = (
    <div
      ref={resizerRef}
      className="drawer-resizer"
      onPointerDown={startResize}
    />
  );

  // ─── task mode ────────────────────────────────────────────────────
  if (drawer.kind === "task") {
    const task = tasks.find((t) => t.id === drawer.taskId);

    if (!task) {
      return createPortal(
        <aside className="feature-drawer" style={asideStyle}>
          {Resizer}
          <button className="drawer-close" onClick={close}>
            ×
          </button>
          <div style={{ padding: 20 }}>Task not found.</div>
        </aside>,
        document.body,
      );
    }

    const taskActions = buildTaskActions(task, taskCtx);
    // The drawer is the row's primary click target, so the "why is nothing
    // happening" answer has to live here too — not only behind Full detail.
    const hold = taskHoldReason(task, {
      pause,
      projectId: drawer.projectId,
    });

    return createPortal(
      <aside className="feature-drawer" style={asideStyle}>
        {Resizer}
        <div className="drawer-head" {...rowProps(taskActions, task.title || task.id)}>
          <div>
            <div className="drawer-kicker">
              {drawer.projectId} · {task.id}
            </div>
            <h3>{task.title || task.id}</h3>
          </div>
          <button className="drawer-close" onClick={close}>
            ×
          </button>
        </div>

        <div className="drawer-actions">
          <button
            className="primary"
            onClick={() =>
              openModal("task", {
                projectId: drawer.projectId,
                taskId: task.id,
              })
            }
          >
            Full detail
          </button>
        </div>

        {hold && (
          <div className="drawer-section">
            <div className={`hold-banner ${hold.code}`}>
              <b>
                {hold.glyph} Held — not dispatching.
              </b>{" "}
              {hold.detail}
            </div>
          </div>
        )}

        <div className="drawer-section">
          <h4>Details</h4>
          <TaskKvGrid task={task} projectId={drawer.projectId} />
        </div>

        {task.content && (
          <div className="drawer-section">
            <h4 className="modal-content-heading">Content</h4>
            <pre className="modal-content-pre">{task.content}</pre>
          </div>
        )}

        {overlays}
      </aside>,
      document.body,
    );
  }

  // ─── feature mode ─────────────────────────────────────────────────
  const derived = deriveFeatures(tasks, drawer.projectId);
  const feature = derived.find((f) => f.id === drawer.featureId);

  if (!feature) {
    return createPortal(
      <aside className="feature-drawer" style={asideStyle}>
        {Resizer}
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
  // Archived members fold away, matching the derived feature (which no
  // longer counts them) and the CardTasks archived fold.
  const memberTasks = tasks.filter((t) => t.feature_id === feature.id);
  const featureTasks = memberTasks.filter((t) => t.status !== "archived");
  const archivedTasks = memberTasks.filter((t) => t.status === "archived");
  const actions = buildFeatureActions(feature, featureCtx);

  const selScoped = selProjectId === drawer.projectId;
  const selActive =
    selScoped && (selTaskIds.size > 0 || selFeatureIds.size > 0);
  const selCount = selScoped ? selTaskIds.size + selFeatureIds.size : 0;
  const selectionActions =
    selCount > 0
      ? buildSelectionActions({
          count: selCount,
          requestVerb,
          clearSelection: clearSel,
        })
      : null;
  // Visible drawer rows, for shift-click ranges: members first, then
  // the archived fold only while it is open.
  const orderedTaskIds = [
    ...featureTasks.map((t) => t.id),
    ...(archivedOpen ? archivedTasks.map((t) => t.id) : []),
  ];

  const renderTaskRow = (t: Task) => {
    const marked = selScoped && selTaskIds.has(t.id);
    const rp = rowProps(
      buildTaskActions(t, taskCtx),
      t.title || t.id,
      // Selection mode is modal: Enter toggles, it does not open.
      selActive
        ? () => toggleTaskSel(drawer.projectId, t.id)
        : () =>
            openModal("task", {
              projectId: drawer.projectId,
              taskId: t.id,
            }),
      {
        selectionActions: marked ? selectionActions ?? undefined : undefined,
        // Long-press = the touch shift-click.
        onRangeSelect: () =>
          rangeTaskSel(drawer.projectId, orderedTaskIds, t.id),
      },
    );

    return (
      <div
        key={t.id}
        className={`drawer-task${marked ? " marked" : ""}`}
        {...rp}
        onKeyDown={(e) => {
          if (isRangeKey(e)) {
            e.preventDefault();
            rangeTaskSel(drawer.projectId, orderedTaskIds, t.id);
            return;
          }
          rp.onKeyDown(e);
        }}
        onClick={(e) => {
          // Same gestures as CardTasks rows: shift ranges, selection
          // mode toggles, a plain click opens detail.
          if (e.shiftKey) {
            rangeTaskSel(drawer.projectId, orderedTaskIds, t.id);
            return;
          }
          if (selActive) {
            toggleTaskSel(drawer.projectId, t.id);
            return;
          }
          openModal("task", {
            projectId: drawer.projectId,
            taskId: t.id,
          });
        }}
        onMouseDown={(e) => {
          if (e.shiftKey) {
            e.preventDefault();
            e.currentTarget.focus();
          }
        }}
      >
        <span>{t.status}</span>
        <b>{t.title || t.id}</b>
      </div>
    );
  };

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
    <aside className="feature-drawer" style={asideStyle}>
      {Resizer}
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
        {featureTasks.map(renderTaskRow)}
        {archivedTasks.length > 0 && (
          <button
            onClick={() => setArchivedOpen((v) => !v)}
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
            {archivedOpen ? "▾" : "▸"} {archivedTasks.length} archived task
            {archivedTasks.length === 1 ? "" : "s"}
          </button>
        )}
        {archivedOpen && archivedTasks.map(renderTaskRow)}
      </div>

      {overlays}
    </aside>,
    document.body,
  );
}
