import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useUI, ALL_PROJECTS } from "../store/ui";
import { useNav } from "../store/nav";
import { useViewKeyboard, handleListNavKey } from "../lib/keyboard";
import {
  getRunnerStatus,
  listAutomationData,
  listGoals,
  pauseAutomations,
  resumeAutomations,
  runGoal,
  updateEntry,
} from "../lib/api";
import { Pill } from "../components/common/Badge";
import { EmptyState, ErrorState, Loading } from "../components/common/states";
import { GoalConfigModal } from "./automations/GoalConfigModal";
import { NewGoalModal } from "./automations/NewGoalModal";
import { DreamPane, type DreamHandle } from "./automations/DreamPane";
import {
  type AutomationRow,
  childRunTasks,
  normalizeAutomationRows,
  triggerLabel,
} from "./automations/rows";
import type { BrainEntry, GoalSummary } from "../lib/types";

type SubTab = "automations" | "dream";

interface GoalProgress {
  completed: number;
  total: number;
  blocked: number;
}

export function AutomationsView() {
  const activeProject = useUI((s) => s.activeProject);
  const project = activeProject === ALL_PROJECTS ? undefined : activeProject;
  const toast = useUI((s) => s.toast);
  const qc = useQueryClient();

  const [subTab, setSubTab] = useState<SubTab>("automations");
  const [editing, setEditing] = useState<GoalSummary | null>(null);
  const [creating, setCreating] = useState(false);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [, setBusy] = useState(false);
  const dreamRef = useRef<DreamHandle>(null);

  const dataQ = useQuery({
    queryKey: ["automation-data", project ?? "all"],
    queryFn: () => listAutomationData(project),
    refetchInterval: 12_000,
  });
  const goalsQ = useQuery({ queryKey: ["goals"], queryFn: listGoals });
  const statusQ = useQuery({
    queryKey: ["runner-status"],
    queryFn: getRunnerStatus,
    refetchInterval: 10_000,
  });

  const automations = dataQ.data?.automations ?? [];
  const tasks = dataQ.data?.tasks ?? [];
  const runs = dataQ.data?.runs ?? [];

  const rows = useMemo(
    () => normalizeAutomationRows(automations, tasks, runs),
    [automations, tasks, runs],
  );

  // Correlate goal automation rows to their GoalSummary (by entry_id) so the
  // goal-specific configure/reconcile actions stay available.
  const goalByEntryId = useMemo(() => {
    const m = new Map<string, GoalSummary>();
    for (const g of goalsQ.data ?? []) if (g.entry_id) m.set(g.entry_id, g);
    return m;
  }, [goalsQ.data]);

  // Goal progress computed client-side from task feature_id (mirrors the TUI's
  // goalProgressByFeature) — no extra per-goal API calls.
  const progressByFeature = useMemo(() => {
    const m = new Map<string, GoalProgress>();
    const goalFeatures = new Set(
      (goalsQ.data ?? []).map((g) => g.feature_id).filter(Boolean) as string[],
    );
    for (const t of tasks) {
      if (!t.feature_id || !goalFeatures.has(t.feature_id)) continue;
      const p = m.get(t.feature_id) ?? { completed: 0, total: 0, blocked: 0 };
      p.total++;
      if (t.status === "completed" || t.status === "validated") p.completed++;
      else if (t.status === "blocked") p.blocked++;
      m.set(t.feature_id, p);
    }
    return m;
  }, [tasks, goalsQ.data]);

  const scope = `automations:${project ?? "all"}`;
  const cursor = useNav((s) =>
    Math.min(s.cursor[scope] ?? 0, Math.max(0, rows.length - 1)),
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

  function refresh() {
    void qc.invalidateQueries({ queryKey: ["automation-data"] });
    void qc.invalidateQueries({ queryKey: ["goals"] });
  }

  function toggle(row: AutomationRow) {
    const patch =
      row.source === "automation"
        ? { status: row.enabled ? "archived" : "active" }
        : { schedule_enabled: !row.enabled };
    void run(row.enabled ? "Disabled" : "Enabled", () =>
      updateEntry(row.path || row.id, patch),
    ).then(refresh);
  }

  function reconcile(row: AutomationRow) {
    const goal = goalByEntryId.get(row.id);
    if (!goal) {
      toast("Only goal automations can be reconciled here", "info");
      return;
    }
    void run("Reconcile triggered", () => runGoal(goal.goal_id)).then(refresh);
  }

  function configure(row: AutomationRow) {
    const goal = goalByEntryId.get(row.id);
    if (goal) setEditing(goal);
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
      if (handleListNavKey(e, scope, rows.length)) return true;
      const row = rows[cursor];
      switch (e.key) {
        case "Enter":
          if (row && childRunTasks(row.id, tasks).length > 0) {
            setExpandedId((id) => (id === row.id ? null : row.id));
          } else if (row) {
            configure(row);
          }
          return true;
        case "e":
          if (row) configure(row);
          return true;
        case "x":
          if (row) reconcile(row);
          return true;
        case " ":
          if (row) toggle(row);
          return true;
        case "p":
          void run(
            automationsPaused ? "Automations resumed" : "Automations paused",
            automationsPaused ? resumeAutomations : pauseAutomations,
          ).then(() => void qc.invalidateQueries({ queryKey: ["runner-status"] }));
          return true;
        case "r":
          refresh();
          return true;
        case "n":
          setCreating(true);
          return true;
        default:
          return false;
      }
    },
    [subTab, rows, cursor, scope, automationsPaused, tasks, goalByEntryId],
  );

  useEffect(() => {
    const el = document.querySelector<HTMLElement>('[data-cursor="1"]');
    el?.scrollIntoView({ block: "nearest" });
  }, [cursor]);

  const loading = dataQ.isLoading && !dataQ.data;

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
      ) : loading ? (
        <Loading label="Loading automations…" />
      ) : dataQ.error ? (
        <ErrorState error={dataQ.error} onRetry={() => void dataQ.refetch()} />
      ) : !rows.length ? (
        <EmptyState
          glyph="⟳"
          title="No automations"
          hint="Built-in automations, scheduled tasks, and goals appear here. Press n to create a goal."
        />
      ) : (
        <div>
          {rows.map((row, i) => {
            const children = childRunTasks(row.id, tasks);
            const goal = goalByEntryId.get(row.id);
            const progress =
              row.isGoal && goal?.feature_id
                ? progressByFeature.get(goal.feature_id)
                : undefined;
            return (
              <div key={row.id}>
                <AutomationRowView
                  row={row}
                  cursored={i === cursor}
                  last={i === rows.length - 1 && expandedId !== row.id}
                  hasChildren={children.length > 0}
                  expanded={expandedId === row.id}
                  isGoalConfigurable={!!goal}
                  progress={progress}
                  onToggle={() => toggle(row)}
                  onReconcile={() => reconcile(row)}
                  onConfigure={() => configure(row)}
                  onExpand={() =>
                    setExpandedId((id) => (id === row.id ? null : row.id))
                  }
                />
                {expandedId === row.id &&
                  children.map((task, j) => (
                    <RunTaskRow
                      key={task.id}
                      task={task}
                      last={j === children.length - 1}
                    />
                  ))}
              </div>
            );
          })}
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
  if (total <= 0) return "░".repeat(width);
  let filled = Math.round((done / total) * width);
  if (filled === 0 && done > 0) filled = 1;
  if (filled > width) filled = width;
  return "█".repeat(filled) + "░".repeat(Math.max(0, width - filled));
}

function AutomationRowView({
  row,
  cursored,
  last,
  hasChildren,
  expanded,
  isGoalConfigurable,
  progress,
  onToggle,
  onReconcile,
  onConfigure,
  onExpand,
}: {
  row: AutomationRow;
  cursored: boolean;
  last: boolean;
  hasChildren: boolean;
  expanded: boolean;
  isGoalConfigurable: boolean;
  progress?: GoalProgress;
  onToggle: () => void;
  onReconcile: () => void;
  onConfigure: () => void;
  onExpand: () => void;
}) {
  return (
    <div
      className={`tree-row ${cursored ? "cursor" : ""}`}
      data-cursor={cursored ? "1" : undefined}
      style={{ gap: 4 }}
      onClick={() => (isGoalConfigurable ? onConfigure() : hasChildren ? onExpand() : undefined)}
    >
      <span
        className="connector"
        style={{ cursor: hasChildren ? "pointer" : undefined, color: "var(--cyan, var(--teal))" }}
        onClick={(e) => { if (hasChildren) { e.stopPropagation(); onExpand(); } }}
      >
        {hasChildren ? (expanded ? "▾ " : "▸ ") : last ? "└─ " : "├─ "}
      </span>
      <span
        className="glyph"
        style={{ color: cursored ? undefined : row.enabled ? "var(--green)" : "var(--fg-faint)" }}
        title={row.enabled ? "enabled — click to disable" : "disabled — click to enable"}
        onClick={(e) => { e.stopPropagation(); onToggle(); }}
      >
        {row.enabled ? "◉" : "○"}
      </span>
      <span className="suffix faint" style={{ minWidth: 64 }}>
        {row.scope === "built-in" ? "built-in" : row.source}
      </span>
      <span className={`title truncate ${row.enabled ? "" : "faint"}`}>
        {row.title}
        {row.isGoal && <span style={{ color: "var(--purple)" }}> [goal]</span>}
      </span>
      <span
        className="suffix"
        style={{ color: row.enabled ? "var(--green)" : "var(--fg-faint)" }}
      >
        [{row.enabled ? "enabled" : "disabled"}]
      </span>
      {progress && progress.total > 0 && (
        <span className="suffix" title={`${progress.completed}/${progress.total} complete`}>
          <span style={{ color: progress.completed === progress.total ? "var(--green)" : "var(--yellow)" }}>
            {bar(progress.completed, progress.total)}
          </span>{" "}
          <span className="faint">
            {progress.completed}/{progress.total}
            {progress.blocked > 0 && <span style={{ color: "var(--red)" }}> ✗{progress.blocked}</span>}
          </span>
        </span>
      )}
      <span className="suffix faint truncate">{triggerLabel(row)}</span>
      {row.runSummary && (
        <span className="suffix" style={{ color: "var(--yellow)" }}>
          run: {row.runSummary}
          {row.runTaskID && <span className="faint"> #{row.runTaskID}</span>}
        </span>
      )}
      {isGoalConfigurable && (
        <span
          className="suffix"
          style={{ cursor: "pointer", color: "var(--blue)" }}
          title="Reconcile (x)"
          onClick={(e) => { e.stopPropagation(); onReconcile(); }}
        >
          ⟳
        </span>
      )}
    </div>
  );
}

function RunTaskRow({ task, last }: { task: BrainEntry; last: boolean }) {
  const statusColor =
    task.status === "completed" || task.status === "validated"
      ? "var(--green)"
      : task.status === "blocked"
        ? "var(--red)"
        : task.status === "in_progress" || task.status === "active"
          ? "var(--yellow)"
          : "var(--fg-faint)";
  return (
    <div className="tree-row" style={{ gap: 4 }}>
      <span className="connector">{"   "}{last ? "└─ " : "├─ "}</span>
      <span className="glyph" style={{ color: statusColor }} title={task.status}>
        ●
      </span>
      <span className="suffix faint mono">{task.id}</span>
      <span className="suffix faint">[{task.status || "unknown"}]</span>
      <span className="title truncate">{task.title}</span>
    </div>
  );
}
