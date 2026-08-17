/**
 * CardGoals — goal list for one project card, modeled on CardAutomations.
 *
 * Each row:
 *   [status glyph] name · status · Run
 * Body click opens the goal modal; every row also answers right-click /
 * long-press / keyboard through useRowActions over `buildGoalActions`,
 * so the card offers the same verb matrix as the modal footer and the
 * command palette. Footer button opens goal-create scoped to the project.
 *
 * Goals do not ride SSE — the list comes from the shared useGoals poll,
 * and the action context invalidates it after every mutation.
 */
import { useModal } from "../../store/modal";
import { useGoals } from "../../hooks/useGoals";
import { useGoalActionContext } from "../../hooks/useGoalActionContext";
import { useRowActions } from "../../hooks/useRowActions";
import { useActionRunner } from "../../hooks/useActionRunner";
import {
  buildGoalActions,
  goalStatusLabel,
} from "../../lib/actions/goalActions";
import { Loading } from "../common/Loading";
import { ErrorState } from "../common/ErrorState";
import type { GoalSummary } from "../../lib/types";

export interface CardGoalsProps {
  projectId: string;
}

/** Row glyph: ✓ active (green) · ✓ completed (plain) · ○ paused/archived. */
function glyphFor(goal: GoalSummary): { glyph: string; kind: string } {
  switch (goal.status) {
    case "active":
      return { glyph: "✓", kind: "ok" };
    case "completed":
      return { glyph: "✓", kind: "" };
    case "blocked":
      return { glyph: "○", kind: "" };
    default:
      return { glyph: "○", kind: "" };
  }
}

export function CardGoals({ projectId }: CardGoalsProps): JSX.Element {
  const { byProject, isLoading, error, invalidate } = useGoals();
  const goals = byProject.get(projectId) ?? [];
  const openModal = useModal((s) => s.open);
  const goalCtx = useGoalActionContext();
  const { rowProps, overlays } = useRowActions();
  const runner = useActionRunner();

  const newGoalButton = (
    <button
      className="id"
      style={{ marginTop: 4, padding: "1px 6px", fontSize: 10 }}
      onClick={() => openModal("goal-create", { project: projectId })}
    >
      New goal
    </button>
  );

  if (isLoading) return <Loading size="sm" label="Loading goals…" />;
  if (error) return <ErrorState error={error} onRetry={invalidate} />;
  if (goals.length === 0) {
    return (
      <div>
        <div style={{ color: "#6b757e", fontSize: 11, padding: "6px 0" }}>
          No goals yet.
        </div>
        {newGoalButton}
      </div>
    );
  }

  return (
    <div>
      {goals.map((g) => {
        const actions = buildGoalActions(g, goalCtx);
        const open = () =>
          openModal("goal", { goalId: g.goal_id, projectId });
        const { glyph, kind } = glyphFor(g);
        const runAction = actions.find((a) => a.id === "run");
        return (
          <div
            key={g.goal_id}
            className="trow"
            {...rowProps(actions, g.title || g.goal_id, open)}
            onClick={open}
            title={g.title}
            style={g.status === "active" ? undefined : { opacity: 0.7 }}
          >
            <span className={`glyph ${kind}`}>{glyph}</span>
            <span className="name">{g.title || g.goal_id}</span>
            <span className="status">{goalStatusLabel(g.status)}</span>
            {runAction && (
              <button
                className="id"
                style={{ padding: "0 4px", fontSize: 10 }}
                disabled={!!runAction.disabledReason}
                title={runAction.disabledReason || "Run now"}
                onClick={(e) => {
                  e.stopPropagation();
                  runner.run(runAction);
                }}
              >
                Run
              </button>
            )}
          </div>
        );
      })}
      {newGoalButton}
      {overlays}
      {runner.dialog}
    </div>
  );
}
