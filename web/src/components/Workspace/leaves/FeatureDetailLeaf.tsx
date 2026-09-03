/**
 * panes-v2 FeatureDetailLeaf.
 *
 * Feature detail docked as a pane (Focus or sidebar) instead of the
 * old single-item `FeatureDrawer`: status, progress bar, merge policy,
 * assign-to-runner controls, a Goals section, and the member task
 * list. Ported from `FeatureDrawer.tsx`'s feature branch — see that
 * file's git history (now `SidebarDock.tsx`) for the pre-generalization
 * version.
 *
 * Target shape: `{ projectId: string, featureId: string }`, mirroring
 * `TaskDetailLeaf`'s `{ projectId, taskId }`.
 */
import { useState } from "react";
import { useModal } from "../../../store/modal";
import { useWorkspace } from "../../../store/workspace";
import { useFeatureAssignments } from "../../../hooks/useFeatureAssignments";
import { useLive } from "../../../lib/sse";
import { useUI } from "../../../store/ui";
import { useRunners } from "../../../hooks/useRunners";
import { useRowActions, type RowActionProps } from "../../../hooks/useRowActions";
import { useFeatureActionContext } from "../../../hooks/useFeatureActionContext";
import { usePauseState } from "../../../hooks/usePauseState";
import { isFeaturePaused } from "../../../lib/pause";
import { useTaskActionContext } from "../../../hooks/useTaskActionContext";
import { useGoals, useGoalProgress } from "../../../hooks/useGoals";
import { useMergeRequests } from "../../../hooks/useMergeRequests";
import { useGoalActionContext } from "../../../hooks/useGoalActionContext";
import { useSelection } from "../../../store/selection";
import {
  ApiError,
  assignFeatureToRunner,
  clearFeatureAssignment,
} from "../../../lib/api";
import { buildFeatureActions } from "../../../lib/actions/featureActions";
import { buildSelectionActions } from "../../../lib/actions/selectionActions";
import { buildTaskActions } from "../../../lib/actions/taskActions";
import { buildGoalActions, goalStatusLabel } from "../../../lib/actions/goalActions";
import { isRangeKey } from "../../../lib/selection";
import { deriveFeatures } from "../../../lib/features";
import { ErrorState } from "../../common/ErrorState";
import { LifecycleBadge } from "../../common/LifecycleBadge";
import type { GoalSummary, Task } from "../../../lib/types";

const EMPTY_TASKS: readonly Task[] = Object.freeze([]);

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
 * FeatureModal's GoalRow / the old FeatureDrawer's DrawerGoalRow so
 * this leaf's feature view has parity. A child component so the
 * per-goal progress query is a hook call per row, not a hook in a
 * loop.
 */
function FeatureLeafGoalRow({
  goal,
  onOpen,
  actionProps,
}: {
  goal: GoalSummary;
  onOpen: () => void;
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

export function FeatureDetailLeaf({
  target,
}: {
  target: Record<string, unknown>;
}): JSX.Element {
  const projectId = (target.projectId as string | undefined) ?? "";
  const featureId =
    (target.featureId as string | undefined) ??
    (target.id as string | undefined) ??
    "";

  const openModal = useModal((s) => s.open);
  const toast = useUI((s) => s.toast);
  const { runners } = useRunners();
  const assignFeature = useWorkspace((s) => s.assignFeature);
  const unassignFeature = useWorkspace((s) => s.unassignFeature);
  // Server-resolved (RunnerInfo.feature_assignments), with the local
  // optimistic map layered on. Reading the local map directly is what
  // hid every auto-assignment and lied after a reload elsewhere.
  const featureAssignments = useFeatureAssignments();
  const [assignBusy, setAssignBusy] = useState(false);
  // Archived-tasks fold. Local (not the persisted per-project toggle):
  // this pane is scoped to one feature, so a sticky cross-feature
  // expansion would surprise more than it helps.
  const [archivedOpen, setArchivedOpen] = useState(false);

  const featureCtx = useFeatureActionContext(projectId);
  const { pause, isLoading: pauseLoading } = usePauseState();
  const taskCtx = useTaskActionContext(projectId);
  const { rowProps, overlays } = useRowActions();
  const goalCtx = useGoalActionContext();
  const { forFeature } = useGoals();
  // Without this the leaf derives lifecycle blind to the project's
  // merge_request entries and contradicts every other surface — it read
  // "finished" for a feature the overview, the project card and the
  // feature modal all showed as ready to merge.
  const { openByProject } = useMergeRequests();

  // Same selection model as CardTasks: rows carry the Select verb, so
  // they participate in selection mode and shift-click ranges — with
  // this pane's own visible row order.
  const selProjectId = useSelection((s) => s.projectId);
  const selTaskIds = useSelection((s) => s.taskIds);
  const selFeatureIds = useSelection((s) => s.featureIds);
  const toggleTaskSel = useSelection((s) => s.toggleTask);
  const rangeTaskSel = useSelection((s) => s.rangeTask);
  const requestVerb = useSelection((s) => s.requestVerb);
  const clearSel = useSelection((s) => s.clear);

  const projectTasks = useLive((s) => s.projects[projectId]?.tasks);
  const tasks = projectTasks ?? EMPTY_TASKS;

  if (!projectId || !featureId) {
    return (
      <ErrorState error="This pane has no feature to show." title="No feature" />
    );
  }

  const derived = deriveFeatures(tasks, projectId, openByProject.get(projectId));
  const feature = derived.find((f) => f.id === featureId);

  if (!feature) {
    return (
      <ErrorState
        error={`Feature "${featureId}" not found in project "${projectId}".`}
        title="Feature not found"
      />
    );
  }

  const pct = Math.round(feature.progress * 100);
  const runnerId = featureAssignments[feature.id];
  const runner = runners.find((r) => r.runner_id === runnerId);
  // Archived members fold away, matching the derived feature (which no
  // longer counts them) and the CardTasks archived fold.
  const memberTasks = tasks.filter((t) => t.feature_id === feature.id);
  const featureTasks = memberTasks.filter((t) => t.status !== "archived");
  const archivedTasks = memberTasks.filter((t) => t.status === "archived");
  const abandonedCount = memberTasks.filter((t) => t.is_abandoned).length;
  const actions = buildFeatureActions(feature, featureCtx, {
    paused: pauseLoading ? undefined : isFeaturePaused(pause, projectId, feature.id),
  });
  const featureGoals = forFeature(projectId, feature.id);

  const selScoped = selProjectId === projectId;
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
  // Visible rows, for shift-click ranges: members first, then the
  // archived fold only while it is open.
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
        ? () => toggleTaskSel(projectId, t.id)
        : () => openModal("task", { projectId, taskId: t.id }),
      {
        selectionActions: marked ? selectionActions ?? undefined : undefined,
        // Long-press = the touch shift-click.
        onRangeSelect: () => rangeTaskSel(projectId, orderedTaskIds, t.id),
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
            rangeTaskSel(projectId, orderedTaskIds, t.id);
            return;
          }
          rp.onKeyDown(e);
        }}
        onClick={(e) => {
          // Same gestures as CardTasks rows: shift ranges, selection
          // mode toggles, a plain click opens detail.
          if (e.shiftKey) {
            rangeTaskSel(projectId, orderedTaskIds, t.id);
            return;
          }
          if (selActive) {
            toggleTaskSel(projectId, t.id);
            return;
          }
          openModal("task", { projectId, taskId: t.id });
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
        await assignFeatureToRunner(projectId, feature.id, targetRunnerId, {
          intent,
        });
      } catch (err) {
        // The local mirror can lag the server. A 409 on "assign" means
        // the server has it assigned elsewhere — the click named the
        // runner the user wants, so escalate to reassign once.
        if (
          intent === "assign" &&
          err instanceof ApiError &&
          err.status === 409
        ) {
          await assignFeatureToRunner(projectId, feature.id, targetRunnerId, {
            intent: "reassign",
          });
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
      await clearFeatureAssignment(projectId, feature.id);
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

  return (
    <div>
      <div className="drawer-head" {...rowProps(actions, feature.name)}>
        <div>
          <div className="drawer-kicker">
            {projectId} · {feature.id}
          </div>
          <h3>{feature.name}</h3>
        </div>
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
            marginBottom: 8,
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
            <LifecycleBadge lifecycle={feature.lifecycle} href={feature.prUrl} />
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
          <div className="v">{runner ? runner.runner_id : "unassigned"}</div>
          {/* `feature_depends_on`, spelled out. The Tasks tab nests this
              feature under what it waits on, which shows the SHAPE; the
              ids only ever appeared on the deleted Features tab, so
              without this row a dependency naming a feature that is not
              in the project (a typo, or one that lives elsewhere) has
              nowhere left to be read. */}
          {feature.dependsOn.length > 0 && (
            <>
              <div className="k">Waits on</div>
              <div className="v">{feature.dependsOn.join(", ")}</div>
            </>
          )}
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
          <div style={{ color: "#6b757e", fontSize: 11 }}>No tasks yet.</div>
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
          <FeatureLeafGoalRow
            key={g.goal_id}
            goal={g}
            onOpen={() => openModal("goal", { goalId: g.goal_id, projectId })}
            actionProps={rowProps(
              buildGoalActions(g, goalCtx),
              g.title || g.goal_id,
              () => openModal("goal", { goalId: g.goal_id, projectId }),
            )}
          />
        ))}
        <button
          className="id"
          style={{ marginTop: 4, padding: "1px 6px", fontSize: 10 }}
          onClick={() =>
            openModal("goal-create", { project: projectId, featureId: feature.id })
          }
        >
          Add goal
        </button>
      </div>

      {overlays}
    </div>
  );
}
