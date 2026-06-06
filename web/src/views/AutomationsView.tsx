import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useUI, ALL_PROJECTS } from "../store/ui";
import { goalProgress, listGoals, runGoal } from "../lib/api";
import { Pill } from "../components/common/Badge";
import { EmptyState, ErrorState, Loading } from "../components/common/states";
import { GoalConfigModal } from "./automations/GoalConfigModal";
import type { GoalSummary } from "../lib/types";

export function AutomationsView() {
  const activeProject = useUI((s) => s.activeProject);
  const goalsQ = useQuery({ queryKey: ["goals"], queryFn: listGoals });

  if (goalsQ.isLoading) return <Loading label="Loading automations…" />;
  if (goalsQ.error)
    return <ErrorState error={goalsQ.error} onRetry={() => void goalsQ.refetch()} />;

  const goals = (goalsQ.data ?? []).filter(
    (g) =>
      activeProject === ALL_PROJECTS ||
      !g.project ||
      g.project === activeProject,
  );

  if (!goals.length)
    return (
      <EmptyState
        glyph="⟳"
        title="No goals"
        hint="Goal automations reconcile feature progress toward an objective."
      />
    );

  return (
    <div className="section-pad">
      {goals.map((g) => (
        <GoalCard key={g.goal_id} goal={g} />
      ))}
    </div>
  );
}

function GoalCard({ goal }: { goal: GoalSummary }) {
  const toast = useUI((s) => s.toast);
  const qc = useQueryClient();
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);

  const progQ = useQuery({
    queryKey: ["goal-progress", goal.goal_id],
    queryFn: () => goalProgress(goal.goal_id),
    refetchInterval: 15_000,
  });
  const p = progQ.data;
  const pct = p && p.total > 0 ? Math.round((p.completed / p.total) * 100) : 0;

  async function reconcile() {
    setBusy(true);
    try {
      await runGoal(goal.goal_id);
      toast("Reconcile triggered", "success");
      void qc.invalidateQueries({ queryKey: ["goal-progress", goal.goal_id] });
      void qc.invalidateQueries({ queryKey: ["goal-audit", goal.goal_id] });
    } catch (e) {
      toast(e instanceof Error ? e.message : "Reconcile failed", "error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <div className="card section-pad" style={{ marginBottom: "0.6rem" }}>
        <div className="row" style={{ gap: "0.5rem", alignItems: "flex-start" }}>
          <Pill color="var(--purple)">goal</Pill>
          <strong style={{ flex: 1, lineHeight: 1.3 }}>{goal.title}</strong>
          <Pill
            color={goal.status === "active" ? "var(--green)" : "var(--fg-faint)"}
          >
            {goal.status}
          </Pill>
        </div>

        <div className="row wrap" style={{ gap: "0.35rem", marginTop: "0.5rem" }}>
          {goal.project && <Pill color="var(--cyan)">{goal.project}</Pill>}
          {goal.feature_id && <Pill color="var(--blue)">⊞ {goal.feature_id}</Pill>}
          {goal.config?.trigger_source && (
            <Pill className="faint">on {goal.config.trigger_source}</Pill>
          )}
        </div>

        {p && p.total > 0 && (
          <div style={{ marginTop: "0.7rem" }}>
            <div className="row" style={{ justifyContent: "space-between", fontSize: 12.5 }}>
              <span className="muted">
                {p.completed}/{p.total} complete
                {p.blocked > 0 && (
                  <span style={{ color: "var(--red)" }}> · {p.blocked} blocked</span>
                )}
                {p.in_progress > 0 && (
                  <span style={{ color: "var(--blue)" }}> · {p.in_progress} active</span>
                )}
              </span>
              <span className="faint">{pct}%</span>
            </div>
            <div
              style={{
                height: 6,
                background: "var(--bg-3)",
                borderRadius: 4,
                marginTop: 4,
                overflow: "hidden",
              }}
            >
              <div
                style={{
                  height: "100%",
                  width: `${pct}%`,
                  background: "var(--green)",
                  transition: "width 0.3s ease",
                }}
              />
            </div>
          </div>
        )}

        <div className="btn-row" style={{ marginTop: "0.7rem" }}>
          <button className="btn sm primary" disabled={busy} onClick={() => void reconcile()}>
            ⟳ Reconcile
          </button>
          <button className="btn sm" onClick={() => setEditing(true)}>
            ✎ Configure
          </button>
        </div>
      </div>

      {editing && (
        <GoalConfigModal goal={goal} onClose={() => setEditing(false)} />
      )}
    </>
  );
}
