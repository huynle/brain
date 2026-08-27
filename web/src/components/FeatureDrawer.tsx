/**
 * FeatureDrawer — right-side panel that hosts a feature, a task, an
 * entry, or a session, driven by the `drawer` discriminated union in
 * the workspace store. The drawer IS full detail — there is no more
 * "Full detail" → Modal hop for feature/task mode; TaskModal/
 * FeatureModal remain reachable from their other callers (command
 * palette, context-menu "Open"), but the drawer no longer routes to
 * them.
 *
 * - Feature mode (double-click a feature): status, assign-to-runner,
 *   member tasks, and a Goals section (parity with FeatureModal).
 * - Task mode (double-click / Enter a task row): the same KV metadata
 *   grid + Sessions list + Content body TaskModal renders. Sessions
 *   rows "View" INLINE in this drawer (`openSessionInDrawer`), not the
 *   full-page session view.
 * - Entry / session mode: dropped in (see below) or, for session,
 *   opened via the task action context's "Open session in sidebar"
 *   verb. These render the SAME `EntryLeaf`/`SessionLeaf` components
 *   the Focus workspace docks — no duplicated rendering logic.
 *
 * Drop target: the open drawer accepts a drag of a task / entry /
 * session (same `readDragPayload`/`endDrag` pattern as FocusPanes.tsx)
 * and REPLACES its content. When the drawer is closed, a slim overlay
 * rail on the right edge appears only while a drag is in flight (see
 * the `!drawer` branch) so the first drop has somewhere to land.
 *
 * The aside is drag-resizable from its LEFT edge; the chosen width is
 * persisted (`drawerWidth` in the store) and applied via a CSS var.
 *
 * Mount strategy: on mobile the drawer stays a fixed-position overlay
 * portaled to `document.body` (unchanged, `.feature-drawer` position:
 * fixed override under `body.mobile`). On desktop it is NOT portaled —
 * Dashboard.tsx mounts `<FeatureDrawer/>` as a direct child of `#app`,
 * and `grid-area: drawer` slots it in as a real, third grid column
 * that pushes the workspace column aside instead of overlaying it.
 *
 * Hook discipline: EVERY hook runs unconditionally at the top (guarding
 * on `drawer` being null), and only the RENDERED JSX branches on
 * `drawer.kind`. No conditional hook calls.
 */
import { useCallback, useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { useWorkspace } from "../store/workspace";
import { useModal } from "../store/modal";
import { useSelection } from "../store/selection";
import { useUI } from "../store/ui";
import { useLive } from "../lib/sse";
import { useRunners } from "../hooks/useRunners";
import { useIsMobile } from "../hooks/useIsMobile";
import { useEdgeResize } from "../hooks/useEdgeResize";
import { useRowActions, type RowActionProps } from "../hooks/useRowActions";
import { useFeatureActionContext } from "../hooks/useFeatureActionContext";
import { useTaskActionContext } from "../hooks/useTaskActionContext";
import { useGoals, useGoalProgress } from "../hooks/useGoals";
import { useGoalActionContext } from "../hooks/useGoalActionContext";
import {
  readDragPayload,
  endDrag,
  useDragDrop,
  type DragPayload,
} from "../hooks/useDragDrop";
import {
  ApiError,
  assignFeatureToRunner,
  clearFeatureAssignment,
} from "../lib/api";
import { buildFeatureActions } from "../lib/actions/featureActions";
import { buildSelectionActions } from "../lib/actions/selectionActions";
import { buildTaskActions } from "../lib/actions/taskActions";
import { buildGoalActions, goalStatusLabel } from "../lib/actions/goalActions";
import { taskHoldReason } from "../lib/pause";
import { usePauseState } from "../hooks/usePauseState";
import { isRangeKey } from "../lib/selection";
import { deriveFeatures } from "../lib/features";
import { historySessionRefs } from "../lib/sessionRef";
import { TaskKvGrid } from "./Modal/TaskKvGrid";
import { SessionsSection } from "./Modal/SessionsSection";
import { EntryLeaf } from "./Workspace/leaves/EntryLeaf";
import { SessionLeaf } from "./Workspace/leaves/SessionLeaf";
import type { GoalSummary, Task } from "../lib/types";

const EMPTY_TASKS: readonly Task[] = Object.freeze([]);

const LIFECYCLE_TONE = {
  "in-progress": { tone: "active", label: "active" },
  blocked: { tone: "blocked", label: "blocked" },
  finished: { tone: "finished", label: "finished" },
  "mr-open": { tone: "mr", label: "MR open" },
  merged: { tone: "merged", label: "merged" },
} as const;

/** life-badge tone for a goal status (same classes CardGoals/FeatureModal use). */
function goalTone(status: string): string {
  switch (status) {
    case "active":
      return "active";
    case "blocked":
      return "blocked";
    case "completed":
      return "finished";
    default:
      return "";
  }
}

/**
 * One goal row with its tiny progress readout — ported from
 * FeatureModal's GoalRow so the drawer's feature view has parity. A
 * child component so the per-goal progress query is a hook call per
 * row, not a hook in a loop.
 */
function DrawerGoalRow({
  goal,
  onOpen,
  actionProps,
}: {
  goal: GoalSummary;
  onOpen: () => void;
  /** Built by the parent's useRowActions so the goal verbs ride the
   *  drawer's shared overlays — right-click, long-press and keyboard,
   *  same registry as FeatureModal/CardGoals rows. */
  actionProps: RowActionProps;
}): JSX.Element {
  const { progress } = useGoalProgress(goal.goal_id);
  const pct =
    progress && progress.total > 0
      ? Math.round((progress.completed / progress.total) * 100)
      : 0;
  return (
    <div className="trow" {...actionProps} onClick={onOpen} title={goal.title}>
      <span className="glyph">◎</span>
      <span className="name">{goal.title || goal.goal_id}</span>
      <span
        className={`life-badge ${goalTone(goal.status)}`}
        style={{ marginRight: 6 }}
      >
        {goalStatusLabel(goal.status)}
      </span>
      <span
        className="bar"
        style={{
          width: 60,
          height: 4,
          background: "#22272c",
          borderRadius: 2,
          overflow: "hidden",
          flex: "0 0 auto",
        }}
        title={
          progress ? `${progress.completed}/${progress.total} tasks` : undefined
        }
      >
        <i
          style={{
            display: "block",
            height: "100%",
            width: `${pct}%`,
            background: "#6fca7d",
          }}
        />
      </span>
    </div>
  );
}

export function FeatureDrawer(): JSX.Element | null {
  const drawer = useWorkspace((s) => s.drawer);
  const close = useWorkspace((s) => s.closeFeatureDrawer);
  const drawerWidth = useWorkspace((s) => s.drawerWidth);
  const setDrawerWidth = useWorkspace((s) => s.setDrawerWidth);
  const assignFeature = useWorkspace((s) => s.assignFeature);
  const unassignFeature = useWorkspace((s) => s.unassignFeature);
  const featureAssignments = useWorkspace((s) => s.featureAssignments);
  const openDrawerFromDrag = useWorkspace((s) => s.openDrawerFromDrag);
  const openModal = useModal((s) => s.open);
  const toast = useUI((s) => s.toast);
  const { runners } = useRunners();
  const { pause } = usePauseState();
  // Mobile keeps the original fixed-overlay portal; desktop becomes a
  // real grid column inside #app (see Dashboard.tsx + .feature-drawer
  // CSS). Both paths share this same component — only the mount
  // strategy differs.
  const isMobile = useIsMobile();
  const [assignBusy, setAssignBusy] = useState(false);
  // Archived-tasks fold. Local (not the persisted per-project toggle):
  // the drawer is transient and scoped to one feature, so a sticky
  // cross-feature expansion would surprise more than it helps.
  const [archivedOpen, setArchivedOpen] = useState(false);
  // Visual feedback for the closed-drawer drop rail (see the `!drawer`
  // branch below) — mirrors FocusPanes' `dragover` state.
  const [railHover, setRailHover] = useState(false);
  // Whether ANY drag is currently in flight, anywhere in the app — the
  // drop rail only renders while this is true, so it never occupies
  // permanent screen space.
  const dragActive = useDragDrop((s) => s.payload !== null);

  // `entry`/`session` drawer kinds carry no projectId (an entry/session
  // isn't necessarily scoped to a project the way a task/feature is).
  const projectId =
    drawer?.kind === "feature" || drawer?.kind === "task"
      ? drawer.projectId
      : "";
  const featureCtx = useFeatureActionContext(projectId);
  const taskCtx = useTaskActionContext(projectId);
  const { rowProps, overlays } = useRowActions();
  const goalCtx = useGoalActionContext();
  const { forFeature } = useGoals();

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
  const projectTasks = useLive((s) => s.projects[projectId]?.tasks);
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
  // The drawer is anchored to the right edge (fixed on mobile, the
  // rightmost grid column on desktop), so its width is the distance
  // from the pointer to the viewport's right edge.
  const startResize = useEdgeResize({
    computeWidth: (clientX) => window.innerWidth - clientX,
    onResize: setDrawerWidth,
    bodyClass: "drawer-resizing",
  });

  // ─── drop target: the drawer accepts a task / entry / session drag ──
  // Same readDragPayload/endDrag pattern FocusPanes.tsx uses for its
  // empty-state drop zone. Dropping on an ALREADY-OPEN drawer replaces
  // its content; dropping on the closed-drawer rail (rendered in the
  // `!drawer` branch below) opens it for the first time.
  const handleDrawerDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
  }, []);

  const handleDrawerDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      const payload = readDragPayload(e);
      endDrag();
      if (!payload) return;
      if (!isDrawerAcceptedKind(payload.kind)) return;
      openDrawerFromDrag(payload.kind, payload.target, payload.title);
    },
    [openDrawerFromDrag],
  );

  if (!drawer) {
    // Nothing to show, and no eligible drag in flight — render nothing
    // rather than reserve a permanent sliver of screen space.
    if (isMobile || !dragActive) return null;
    // A drag IS in flight but the drawer is closed: there's currently
    // no grid column to drop onto (the CSS only reserves one when
    // `body.drawer-open`), so this is a fixed-position overlay rail on
    // the right edge — the "open the first item" drop target.
    return (
      <div
        className={"drawer-drop-rail" + (railHover ? " dragover" : "")}
        onDragOver={(e) => {
          handleDrawerDragOver(e);
          setRailHover(true);
        }}
        onDragLeave={() => setRailHover(false)}
        onDrop={(e) => {
          setRailHover(false);
          handleDrawerDrop(e);
        }}
        aria-label="Drop here to open in the side panel"
        title="Drop a task, entry, or session here"
      />
    );
  }
  if (typeof document === "undefined") return null;

  const asideStyle = { ["--drawer-w" as never]: `${drawerWidth}px` } as React.CSSProperties;
  const Resizer = (
    <div className="drawer-resizer" onPointerDown={startResize} />
  );
  // Mobile: portal to document.body (fixed overlay, unchanged from
  // before). Desktop: render in place — Dashboard mounts <FeatureDrawer/>
  // as a direct child of #app so `grid-area: drawer` applies.
  const wrap = (node: JSX.Element) =>
    isMobile ? createPortal(node, document.body) : node;

  // ─── task mode ────────────────────────────────────────────────────
  if (drawer.kind === "task") {
    const task = tasks.find((t) => t.id === drawer.taskId);

    if (!task) {
      return wrap(
        <aside
          className="feature-drawer"
          style={asideStyle}
          onDragOver={handleDrawerDragOver}
          onDrop={handleDrawerDrop}
        >
          {Resizer}
          <button className="drawer-close" onClick={close}>
            ×
          </button>
          <div style={{ padding: 20 }}>Task not found.</div>
        </aside>,
      );
    }

    const taskActions = buildTaskActions(task, taskCtx);
    // The drawer IS full detail now (no more "Full detail" → Modal hop —
    // see the KV grid, Sessions and Content below), so the "why is
    // nothing happening" answer has to live here too.
    const hold = taskHoldReason(task, {
      pause,
      projectId: drawer.projectId,
    });

    return wrap(
      <aside
        className="feature-drawer"
        style={asideStyle}
        onDragOver={handleDrawerDragOver}
        onDrop={handleDrawerDrop}
      >
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

        {historySessionRefs(task).length > 0 && (
          <div className="drawer-section">
            {/* "View" opens the session INLINE in this same drawer (not
                the full-page session view) — consistent with the
                "open-session-sidebar" verb below and with the drawer
                being full detail now. */}
            <SessionsSection
              task={task}
              projectId={drawer.projectId}
              onView={(t, ref) => taskCtx.openSessionInDrawer(t, ref)}
            />
          </div>
        )}

        {task.content && (
          <div className="drawer-section">
            <h4 className="modal-content-heading">Content</h4>
            <pre className="modal-content-pre">{task.content}</pre>
          </div>
        )}

        {overlays}
      </aside>,
    );
  }

  // ─── entry mode (dropped from the sidebar / a Focus pane) ──────────
  // Renders the SAME leaf component the Focus workspace docks — no
  // duplicated entry-reading logic. `EntryLeaf`'s own "open a link"
  // handler pushes into Focus (it doesn't know about the drawer), which
  // is an acceptable seam: navigating an entry-to-entry link is a
  // heavier action than viewing one, and Focus is where that belongs.
  if (drawer.kind === "entry") {
    return wrap(
      <aside
        className="feature-drawer"
        style={asideStyle}
        onDragOver={handleDrawerDragOver}
        onDrop={handleDrawerDrop}
      >
        {Resizer}
        <div className="drawer-head">
          <div>
            <div className="drawer-kicker">Entry</div>
          </div>
          <button className="drawer-close" onClick={close}>
            ×
          </button>
        </div>
        <div
          style={{
            flex: 1,
            minHeight: 0,
            display: "flex",
            flexDirection: "column",
          }}
        >
          <EntryLeaf target={drawer.target} />
        </div>
      </aside>,
    );
  }

  // ─── session mode (dropped, or opened via "Open session in sidebar") ─
  if (drawer.kind === "session") {
    return wrap(
      <aside
        className="feature-drawer"
        style={asideStyle}
        onDragOver={handleDrawerDragOver}
        onDrop={handleDrawerDrop}
      >
        {Resizer}
        <div className="drawer-head">
          <div>
            <div className="drawer-kicker">Session</div>
          </div>
          <button className="drawer-close" onClick={close}>
            ×
          </button>
        </div>
        <div
          style={{
            flex: 1,
            minHeight: 0,
            display: "flex",
            flexDirection: "column",
          }}
        >
          <SessionLeaf target={drawer.target} />
        </div>
      </aside>,
    );
  }

  // ─── feature mode ─────────────────────────────────────────────────
  const derived = deriveFeatures(tasks, drawer.projectId);
  const feature = derived.find((f) => f.id === drawer.featureId);

  if (!feature) {
    return wrap(
      <aside
        className="feature-drawer"
        style={asideStyle}
        onDragOver={handleDrawerDragOver}
        onDrop={handleDrawerDrop}
      >
        {Resizer}
        <button className="drawer-close" onClick={close}>
          ×
        </button>
        <div style={{ padding: 20 }}>Feature not found.</div>
      </aside>,
    );
  }

  const tone = LIFECYCLE_TONE[feature.lifecycle];
  const pct = Math.round(feature.progress * 100);
  const runnerId = featureAssignments[feature.id];
  const runner = runners.find((r) => r.runner_id === runnerId);
  // Archived members fold away, matching the derived feature (which no
  // longer counts them) and the CardTasks archived fold.
  const memberTasks = tasks.filter((t) => t.feature_id === feature.id);
  const featureTasks = memberTasks.filter((t) => t.status !== "archived");
  const archivedTasks = memberTasks.filter((t) => t.status === "archived");
  const abandonedCount = memberTasks.filter((t) => t.is_abandoned).length;
  const actions = buildFeatureActions(feature, featureCtx);
  const featureGoals = forFeature(drawer.projectId, feature.id);

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

  return wrap(
    <aside
      className="feature-drawer"
      style={asideStyle}
      onDragOver={handleDrawerDragOver}
      onDrop={handleDrawerDrop}
    >
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

      {feature.prUrl && (
        <div className="drawer-actions">
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
        </div>
      )}

      {abandonedCount > 0 && (
        <div
          role="status"
          className="life-badge abandoned"
          style={{
            display: "block",
            padding: "6px 10px",
            fontSize: 12,
            lineHeight: 1.4,
          }}
        >
          {abandonedCount === 1
            ? "1 task in this feature looks abandoned"
            : `${abandonedCount} tasks in this feature look abandoned`}
          {" — use Resume on the task to recover it."}
        </div>
      )}

      <div className="drawer-section">
        <h4>Status</h4>
        <div className="kv-grid">
          <div className="k">Lifecycle</div>
          <div className="v">
            <span className={`life-badge ${tone.tone}`}>{tone.label}</span>
          </div>
          <div className="k">Progress</div>
          <div className="v">
            <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
              <div
                className="bar"
                style={{
                  width: 80,
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
                {feature.taskCount.completed}/{feature.taskCount.total} (
                {pct}%)
              </span>
            </div>
          </div>
          <div className="k">Runner</div>
          <div className="v">
            {runner ? runner.runner_id : "unassigned"}
          </div>
          {feature.mergePolicy && (
            <>
              <div className="k">Merge policy</div>
              <div className="v">{feature.mergePolicy}</div>
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

      <div className="drawer-section">
        <h4>Goals ({featureGoals.length})</h4>
        {featureGoals.length === 0 && (
          <div style={{ color: "#6b757e", fontSize: 11 }}>
            No goals watching this feature.
          </div>
        )}
        {featureGoals.map((g) => (
          <DrawerGoalRow
            key={g.goal_id}
            goal={g}
            onOpen={() => openModal("goal", { goalId: g.goal_id, projectId: drawer.projectId })}
            actionProps={rowProps(
              buildGoalActions(g, goalCtx),
              g.title || g.goal_id,
              () =>
                openModal("goal", {
                  goalId: g.goal_id,
                  projectId: drawer.projectId,
                }),
            )}
          />
        ))}
        <button
          className="id"
          style={{ marginTop: 4, padding: "1px 6px", fontSize: 10 }}
          onClick={() =>
            openModal("goal-create", {
              project: drawer.projectId,
              featureId: feature.id,
            })
          }
        >
          Add goal
        </button>
      </div>

      {overlays}
    </aside>,
  );
}

/**
 * The drawer only understands three drag kinds — everything else
 * (`logs` / `runners` / `browser` from a Focus-pane drag, or `assign`
 * from a feature-header→runner drag) stays Focus/runner-only for now.
 * Mirrors `isLeafKind` in FocusPanes.tsx/PaneLeaf.tsx, narrowed further.
 */
function isDrawerAcceptedKind(
  kind: DragPayload["kind"],
): kind is "task-detail" | "entry" | "session" {
  return kind === "task-detail" || kind === "entry" || kind === "session";
}
