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
import { NewGoalModal } from "./automations/NewGoalModal";
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
  const [creating, setCreating] = useState(false);
  const [, setBusy] = useState(false);
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
        case "n":
          setCreating(true);
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
        <div style={{ flex: 1 }} />
        {subTab === "automations" && (
          <button
            className="btn sm primary"
            onClick={() => setCreating(true)}
            title="New goal (n)"
          >
            + New goal
          </button>
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
          hint="Goal automations reconcile feature progress toward an objective. Press n to create one."
        />
      ) : (
        <div>
          {goals.map((g, i) => (
            <GoalRow
              key={g.goal_id}
              goal={g}
              cursored={i === cursor}
              last={i === goals.length - 1}
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
      {creating && (
        <NewGoalModal
          onClose={() => setCreating(false)}
          onCreated={() => void qc.invalidateQueries({ queryKey: ["goals"] })}
        />
      )}
    </div>
  );
}

function bar(done: number, total: number, width = 8): string {
  if (total <= 0) return "";
  const filled = Math.round((done / total) * width);
  return "█".repeat(filled) + "░".repeat(Math.max(0, width - filled));
}

function GoalRow({
  goal,
  cursored,
  last,
  onConfigure,
  onReconcile,
  onToggle,
}: {
  goal: GoalSummary;
  cursored: boolean;
  last: boolean;
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
  const active = goal.status === "active";
  return (
    <div
      className={`tree-row ${cursored ? "cursor" : ""}`}
      data-cursor={cursored ? "1" : undefined}
      style={{ gap: 4 }}
      onClick={onConfigure}
    >
      <span className="connector">{last ? "└─ " : "├─ "}</span>
      <span
        className="glyph"
        style={{ color: cursored ? undefined : active ? "var(--green)" : "var(--fg-faint)" }}
        title={active ? "active — Space to disable" : "archived — Space to enable"}
        onClick={(e) => { e.stopPropagation(); onToggle(); }}
      >
        {active ? "◉" : "○"}
      </span>
      <span className="title truncate">
        {goal.title} <span style={{ color: "var(--purple)" }}>[goal]</span>
      </span>
      {p && p.total > 0 && (
        <span className="suffix" title={`${p.completed}/${p.total} complete`}>
          <span style={{ color: "var(--green)" }}>{bar(p.completed, p.total)}</span>{" "}
          <span className="faint">
            {p.completed}/{p.total}
            {p.blocked > 0 && <span style={{ color: "var(--red)" }}> ✗{p.blocked}</span>}
          </span>
        </span>
      )}
      {goal.config?.trigger_source && (
        <span className="suffix faint">on:{goal.config.trigger_source}</span>
      )}
      <span
        className="suffix"
        style={{ cursor: "pointer", color: "var(--blue)" }}
        title="Reconcile (x)"
        onClick={(e) => { e.stopPropagation(); onReconcile(); }}
      >
        ⟳
      </span>
    </div>
  );
}
