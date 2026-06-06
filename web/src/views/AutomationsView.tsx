import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useUI, ALL_PROJECTS } from "../store/ui";
import { useNav } from "../store/nav";
import { useViewKeyboard, handleListNavKey } from "../lib/keyboard";
import {
  getRunnerStatus,
  goalProgress,
  listGoals,
  pauseAutomations,
  resumeAutomations,
  runGoal,
  updateGoal,
} from "../lib/api";
import { Pill } from "../components/common/Badge";
import { EmptyState, ErrorState, Loading } from "../components/common/states";
import { GoalConfigModal } from "./automations/GoalConfigModal";
import { DreamPane, type DreamHandle } from "./automations/DreamPane";
import type { GoalSummary } from "../lib/types";

type SubTab = "automations" | "dream";

export function AutomationsView() {
  const activeProject = useUI((s) => s.activeProject);
  const project = activeProject === ALL_PROJECTS ? undefined : activeProject;
  const toast = useUI((s) => s.toast);
  const qc = useQueryClient();

  const [subTab, setSubTab] = useState<SubTab>("automations");
  const [editing, setEditing] = useState<GoalSummary | null>(null);
  const [busy, setBusy] = useState(false);
  const dreamRef = useRef<DreamHandle>(null);

  const goalsQ = useQuery({ queryKey: ["goals"], queryFn: listGoals });
  const statusQ = useQuery({
    queryKey: ["runner-status"],
    queryFn: getRunnerStatus,
    refetchInterval: 10_000,
  });

  const goals = useMemo(
    () =>
      (goalsQ.data ?? []).filter(
        (g) => activeProject === ALL_PROJECTS || !g.project || g.project === activeProject,
      ),
    [goalsQ.data, activeProject],
  );

  const scope = `automations:${project ?? "all"}`;
  const cursor = useNav((s) =>
    Math.min(s.cursor[scope] ?? 0, Math.max(0, goals.length - 1)),
  );

  async function run(label: string, fn: () => Promise<unknown>) {
    setBusy(true);
    try {
      await fn();
      toast(label, "success");
    } catch (e) {
      toast(e instanceof Error ? e.message : "Action failed", "error");
    } finally {
      setBusy(false);
    }
  }

  function reconcile(g: GoalSummary) {
    void run("Reconcile triggered", () => runGoal(g.goal_id)).then(() => {
      void qc.invalidateQueries({ queryKey: ["goal-progress", g.goal_id] });
      void qc.invalidateQueries({ queryKey: ["goal-audit", g.goal_id] });
    });
  }

  function toggleEnable(g: GoalSummary) {
    const next = g.status === "active" ? "archived" : "active";
    void run(next === "active" ? "Enabled" : "Disabled", () =>
      updateGoal(g.goal_id, { status: next }),
    ).then(() => void qc.invalidateQueries({ queryKey: ["goals"] }));
  }

  const automationsPaused = statusQ.data?.automationsPaused;

  useViewKeyboard(
    (e) => {
      if (e.key === "C") {
        setSubTab((s) => (s === "automations" ? "dream" : "automations"));
        return true;
      }
      if (subTab === "dream") {
        switch (e.key) {
          case "/":
            dreamRef.current?.focusSearch();
            return true;
          case "n":
            dreamRef.current?.next();
            return true;
          case "N":
            dreamRef.current?.prev();
            return true;
          case "g":
            dreamRef.current?.top();
            return true;
          case "G":
            dreamRef.current?.bottom();
            return true;
          case "r":
            void qc.invalidateQueries({ queryKey: ["dream"] });
            return true;
          default:
            return false;
        }
      }
      // automations subtab
      if (handleListNavKey(e, scope, goals.length)) return true;
      const g = goals[cursor];
      switch (e.key) {
        case "Enter":
        case "e":
          if (g) setEditing(g);
          return true;
        case "x":
          if (g) reconcile(g);
          return true;
        case " ":
          if (g) toggleEnable(g);
          return true;
        case "p":
          void run(
            automationsPaused ? "Automations resumed" : "Automations paused",
            automationsPaused ? resumeAutomations : pauseAutomations,
          ).then(() => void qc.invalidateQueries({ queryKey: ["runner-status"] }));
          return true;
        case "r":
          void goalsQ.refetch();
          return true;
        default:
          return false;
      }
    },
    [subTab, goals, cursor, scope, automationsPaused],
  );

  useEffect(() => {
    const el = document.querySelector<HTMLElement>('[data-cursor="1"]');
    el?.scrollIntoView({ block: "nearest" });
  }, [cursor]);

  return (
    <div>
      <div className="subtabs">
        <button
          className={subTab === "automations" ? "on" : ""}
          onClick={() => setSubTab("automations")}
        >
          Automations
        </button>
        <button
          className={subTab === "dream" ? "on" : ""}
          onClick={() => setSubTab("dream")}
        >
          ☾ Dream
        </button>
        {subTab === "automations" && automationsPaused && (
          <Pill color="var(--red)">paused</Pill>
        )}
      </div>

      {subTab === "dream" ? (
        <DreamPane ref={dreamRef} project={project} />
      ) : goalsQ.isLoading ? (
        <Loading label="Loading automations…" />
      ) : goalsQ.error ? (
        <ErrorState error={goalsQ.error} onRetry={() => void goalsQ.refetch()} />
      ) : !goals.length ? (
        <EmptyState
          glyph="⟳"
          title="No goals"
          hint="Goal automations reconcile feature progress toward an objective."
        />
      ) : (
        <div className="section-pad">
          {goals.map((g, i) => (
            <GoalCard
              key={g.goal_id}
              goal={g}
              cursored={i === cursor}
              busy={busy}
              onConfigure={() => setEditing(g)}
              onReconcile={() => reconcile(g)}
              onToggle={() => toggleEnable(g)}
            />
          ))}
        </div>
      )}

      {editing && (
        <GoalConfigModal goal={editing} onClose={() => setEditing(null)} />
      )}
    </div>
  );
}

function GoalCard({
  goal,
  cursored,
  busy,
  onConfigure,
  onReconcile,
  onToggle,
}: {
  goal: GoalSummary;
  cursored: boolean;
  busy: boolean;
  onConfigure: () => void;
  onReconcile: () => void;
  onToggle: () => void;
}) {
  const progQ = useQuery({
    queryKey: ["goal-progress", goal.goal_id],
    queryFn: () => goalProgress(goal.goal_id),
    refetchInterval: 15_000,
  });
  const p = progQ.data;
  const pct = p && p.total > 0 ? Math.round((p.completed / p.total) * 100) : 0;

  return (
    <div
      className={`card section-pad ${cursored ? "kbd-cursor" : ""}`}
      data-cursor={cursored ? "1" : undefined}
      style={{ marginBottom: "0.6rem" }}
    >
      <div className="row" style={{ gap: "0.5rem", alignItems: "flex-start" }}>
        <Pill color="var(--purple)">goal</Pill>
        <strong style={{ flex: 1, lineHeight: 1.3 }}>{goal.title}</strong>
        <button
          className="sel-check"
          onClick={onToggle}
          title={goal.status === "active" ? "Disable" : "Enable"}
          style={{
            background: goal.status === "active" ? "var(--green)" : "var(--bg-2)",
            borderColor: goal.status === "active" ? "var(--green)" : "var(--border-strong)",
          }}
        >
          {goal.status === "active" ? "✓" : ""}
        </button>
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
              {p.blocked > 0 && <span style={{ color: "var(--red)" }}> · {p.blocked} blocked</span>}
              {p.in_progress > 0 && <span style={{ color: "var(--blue)" }}> · {p.in_progress} active</span>}
            </span>
            <span className="faint">{pct}%</span>
          </div>
          <div style={{ height: 6, background: "var(--bg-3)", borderRadius: 4, marginTop: 4, overflow: "hidden" }}>
            <div style={{ height: "100%", width: `${pct}%`, background: "var(--green)", transition: "width 0.3s ease" }} />
          </div>
        </div>
      )}

      <div className="btn-row" style={{ marginTop: "0.7rem" }}>
        <button className="btn sm primary" disabled={busy} onClick={onReconcile}>
          ⟳ Reconcile
        </button>
        <button className="btn sm" onClick={onConfigure}>
          ✎ Configure
        </button>
      </div>
    </div>
  );
}
