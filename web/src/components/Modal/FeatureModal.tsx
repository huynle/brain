/**
 * FeatureModal — wireframe-parity.
 *
 * Uses the shared `Modal` primitive (which now renders wireframe
 * `.modal / .modal-head / .modal-body / .modal-foot` classes).
 */
import { useMemo } from "react";
import { Modal } from "../common/Modal";
import { ActionBar } from "../common/ActionBar";
import { LifecycleBadge } from "../common/LifecycleBadge";
import { useModal } from "../../store/modal";
import { useLive } from "../../lib/sse";
import { useActionRunner } from "../../hooks/useActionRunner";
import { useFeatureActionContext } from "../../hooks/useFeatureActionContext";
import { usePauseState } from "../../hooks/usePauseState";
import { isFeaturePaused } from "../../lib/pause";
import { useGoals, useGoalProgress } from "../../hooks/useGoals";
import { useMergeRequests } from "../../hooks/useMergeRequests";
import { useRowActions, type RowActionProps } from "../../hooks/useRowActions";
import { useTaskActionContext } from "../../hooks/useTaskActionContext";
import { useGoalActionContext } from "../../hooks/useGoalActionContext";
import { buildTaskActions } from "../../lib/actions/taskActions";
import { buildFeatureActions } from "../../lib/actions/featureActions";
import { buildGoalActions, goalStatusLabel } from "../../lib/actions/goalActions";
import { deriveFeatures } from "../../lib/features";
import type { GoalSummary, Task } from "../../lib/types";

const EMPTY_TASKS: readonly Task[] = Object.freeze([]);

/** life-badge tone for a goal status (same classes CardGoals uses). */
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
 * One goal row with its tiny progress readout. A child component so the
 * per-goal progress query is a hook call per row, not a hook in a loop.
 */
function GoalRow({
  goal,
  onOpen,
  actionProps,
}: {
  goal: GoalSummary;
  onOpen: () => void;
  /** Built by the parent's useRowActions so the goal verbs ride the
   *  modal's shared overlays — right-click, long-press and keyboard,
   *  same registry as CardGoals rows. */
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

export function FeatureModal(): JSX.Element {
  const target = useModal((s) => s.target);
  const openModal = useModal((s) => s.open);
  const close = useModal((s) => s.close);

  const featureId =
    (target?.featureId as string | undefined) ??
    (target?.id as string | undefined) ??
    "";
  const projectId = (target?.projectId as string | undefined) ?? "";

  const tasks = useLive((s) => s.projects[projectId]?.tasks) ?? EMPTY_TASKS;
  const { openByProject } = useMergeRequests();
  const feature = useMemo(
    () =>
      deriveFeatures(tasks, projectId, openByProject.get(projectId)).find(
        (f) => f.id === featureId,
      ),
    [tasks, projectId, featureId, openByProject],
  );

  const featureTasks = useMemo(
    () => tasks.filter((t) => t.feature_id === featureId),
    [tasks, featureId],
  );

  const featureCtx = useFeatureActionContext(projectId);
  const { pause } = usePauseState();
  const taskCtx = useTaskActionContext(projectId);
  const goalCtx = useGoalActionContext();
  // Right-click / long-press / keyboard verbs on the modal's task rows —
  // the same registry the cards use, so the modal is not a dead end.
  const { rowProps, overlays } = useRowActions();
  const { forFeature } = useGoals();
  const featureGoals = forFeature(projectId, featureId);
  const runner = useActionRunner();
  // Built unconditionally so hook order stays stable across the
  // not-found early return.
  const actions = useMemo(
    () =>
      feature
        ? buildFeatureActions(feature, featureCtx, {
            paused: isFeaturePaused(pause, projectId, feature.id),
          })
        : [],
    [feature, featureCtx],
  );

  const abandonedCount = useMemo(
    () => featureTasks.filter((t) => t.is_abandoned).length,
    [featureTasks],
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

  const pct = Math.round(feature.progress * 100);

  return (
    <Modal
      title={
        <>
          {feature.name}{" "}
          <LifecycleBadge
            lifecycle={feature.lifecycle}
            href={feature.prUrl}
            style={{ marginLeft: 8 }}
          />
        </>
      }
      onClose={close}
      footer={
        <>
          {/* Shared registry — same verbs the row menu and touch sheet
              offer, so the three surfaces cannot drift apart. */}
          <ActionBar
            actions={actions}
            onRun={runner.run}
            primary={["run", "resume", "checkout"]}
          />
          <button className="primary" onClick={close}>
            Done
          </button>
          {runner.dialog}
        </>
      }
    >
      {abandonedCount > 0 && (
        <div
          role="status"
          className="life-badge abandoned"
          style={{
            display: "block",
            marginBottom: 12,
            padding: "6px 10px",
            fontSize: 12,
            lineHeight: 1.4,
          }}
        >
          {abandonedCount === 1
            ? "1 task in this feature looks abandoned"
            : `${abandonedCount} tasks in this feature look abandoned`}
          {" — use Resume below to recover them."}
        </div>
      )}
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
            {...rowProps(buildTaskActions(t, taskCtx), t.title || t.id, () =>
              openModal("task", { projectId, taskId: t.id }),
            )}
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
      {overlays}

      <h4 style={{ margin: "12px 0 6px", color: "#f4b23a", fontSize: 11 }}>
        Goals ({featureGoals.length})
      </h4>
      <div>
        {featureGoals.length === 0 && (
          <div style={{ color: "#6b757e", fontSize: 11 }}>
            No goals watching this feature.
          </div>
        )}
        {featureGoals.map((g) => (
          <GoalRow
            key={g.goal_id}
            goal={g}
            onOpen={() =>
              openModal("goal", { goalId: g.goal_id, projectId })
            }
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
            openModal("goal-create", { project: projectId, featureId })
          }
        >
          Add goal
        </button>
      </div>
    </Modal>
  );
}
