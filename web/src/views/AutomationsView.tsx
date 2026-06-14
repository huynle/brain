import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useUI } from "../store/ui";
import { useNav } from "../store/nav";
import { useViewKeyboard, handleListNavKey } from "../lib/keyboard";
import { useIsMobile } from "../hooks/useIsMobile";
import {
  executeAutomation,
  getEntry,
  getRunnerStatus,
  listAutomationData,
  listGoals,
  listInstances,
  pauseAutomations,
  resumeAutomations,
  runGoal,
  updateEntry,
} from "../lib/api";
import { Pill } from "../components/common/Badge";
import { EmptyState, ErrorState, Loading } from "../components/common/states";
import { ListDetail } from "../components/layout/ListDetail";
import { GoalConfigModal } from "./automations/GoalConfigModal";
import { NewGoalModal } from "./automations/NewGoalModal";
import {
  type AutomationRow,
  childRunTasks,
  normalizeAutomationRows,
  triggerLabel,
} from "./automations/rows";
import type { BrainEntry, GoalSummary, OpencodeInstance, SessionInfo } from "../lib/types";

// pickLatestSession returns the most recently recorded session pointer on a
// task entry (by timestamp), or null if none were recorded.
function pickLatestSession(
  sessions: Record<string, SessionInfo> | undefined,
): { sessionId: string; info: SessionInfo } | null {
  if (!sessions) return null;
  const entries = Object.entries(sessions);
  if (entries.length === 0) return null;
  entries.sort((a, b) => (b[1]?.timestamp ?? "").localeCompare(a[1]?.timestamp ?? ""));
  return { sessionId: entries[0][0], info: entries[0][1] };
}

interface GoalProgress {
  completed: number;
  total: number;
  blocked: number;
}

// A flattened display entry: an automation row, or one of its run-task children
// (shown when the automation is expanded).
type DisplayEntry =
  | { kind: "auto"; row: AutomationRow }
  | { kind: "task"; task: BrainEntry; parent: AutomationRow };

export function AutomationsView() {
  // Automations are a global view (built-ins span all projects).
  const project: string | undefined = undefined;
  const toast = useUI((s) => s.toast);
  const openInControl = useUI((s) => s.openInControl);
  const toggleDetail = useUI((s) => s.toggleDetail);
  const toggleLogs = useUI((s) => s.toggleLogs);
  const openInspect = useUI((s) => s.openInspect);
  const isMobile = useIsMobile();
  const qc = useQueryClient();

  const [editing, setEditing] = useState<GoalSummary | null>(null);
  const [creating, setCreating] = useState(false);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [, setBusy] = useState(false);

  const dataQ = useQuery({
    queryKey: ["automation-data", "all"],
    queryFn: () => listAutomationData(project),
    refetchInterval: 8_000,
  });
  const goalsQ = useQuery({ queryKey: ["goals"], queryFn: listGoals });
  const statusQ = useQuery({
    queryKey: ["runner-status"],
    queryFn: getRunnerStatus,
    refetchInterval: 10_000,
  });
  const instancesQ = useQuery({
    queryKey: ["instances"],
    queryFn: listInstances,
    refetchInterval: 5_000,
  });

  const automations = dataQ.data?.automations ?? [];
  const tasks = dataQ.data?.tasks ?? [];
  const runs = dataQ.data?.runs ?? [];

  const rows = useMemo(
    () => normalizeAutomationRows(automations, tasks, runs),
    [automations, tasks, runs],
  );

  const goalByEntryId = useMemo(() => {
    const m = new Map<string, GoalSummary>();
    for (const g of goalsQ.data ?? []) if (g.entry_id) m.set(g.entry_id, g);
    return m;
  }, [goalsQ.data]);

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

  // Flatten automations + their expanded children into one navigable list.
  const display = useMemo<DisplayEntry[]>(() => {
    const out: DisplayEntry[] = [];
    for (const row of rows) {
      out.push({ kind: "auto", row });
      if (expandedId === row.id) {
        for (const task of childRunTasks(row.id, tasks)) {
          out.push({ kind: "task", task, parent: row });
        }
      }
    }
    return out;
  }, [rows, expandedId, tasks]);

  const scope = "automations";
  const cursor = useNav((s) =>
    Math.min(s.cursor[scope] ?? 0, Math.max(0, display.length - 1)),
  );

  // Map a task id → its live OpenCode instance (for "open in Control").
  const instanceByTaskId = useMemo(() => {
    const m = new Map<string, OpencodeInstance>();
    for (const inst of instancesQ.data ?? []) {
      if (inst.task_id) m.set(inst.task_id, inst);
    }
    return m;
  }, [instancesQ.data]);

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
    void qc.invalidateQueries({ queryKey: ["instances"] });
  }

  function execute(row: AutomationRow) {
    if (row.source !== "automation") {
      toast("Only automations can be executed", "info");
      return;
    }
    void run("Automation triggered", () => executeAutomation(row.path || row.id, "all")).then(() => {
      setExpandedId(row.id); // reveal the new task underneath
      refresh();
    });
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
      toast("Only goal automations can be reconciled", "info");
      return;
    }
    void run("Reconcile triggered", () => runGoal(goal.goal_id)).then(refresh);
  }

  function configure(row: AutomationRow) {
    const goal = goalByEntryId.get(row.id);
    if (goal) setEditing(goal);
  }

  function toggleExpand(row: AutomationRow) {
    setExpandedId((id) => (id === row.id ? null : row.id));
  }

  // Open a run-task's OpenCode session in the Control tab. A live instance is
  // attached directly; otherwise we fall back to the recorded session pointer
  // so a completed session can still be reviewed (and resumed).
  async function openTaskInControl(task: BrainEntry) {
    // On mobile, tapping a run-task opens its Detail/Logs sheet (consistent
    // with Tasks/Brain). Session review/resume stays on the Control tab.
    if (isMobile) {
      openInspect({ path: task.path, title: task.title, taskId: task.id, projectId: task.project_id });
      return;
    }
    const inst = instanceByTaskId.get(task.id);
    if (inst) {
      openInControl({
        mode: "live",
        runnerId: inst.runner_id,
        instanceId: inst.instance_id,
        sessionId: inst.session_ids?.[0],
        taskTitle: task.title,
      });
      return;
    }
    // No live instance — look up where the session was recorded. The list
    // endpoint may omit metadata, so fetch the full entry.
    try {
      const full = task.sessions ? task : await getEntry(task.path);
      const latest = pickLatestSession(full.sessions);
      if (!latest) {
        toast("No session recorded for this task", "info");
        return;
      }
      openInControl({
        mode: "history",
        runnerId: latest.info.runner_id || "",
        sessionId: latest.sessionId,
        machineId: latest.info.machine_id,
        hostname: latest.info.hostname,
        workdir: latest.info.workdir,
        taskTitle: task.title,
      });
    } catch (e) {
      toast(e instanceof Error ? e.message : "Could not open session", "error");
    }
  }

  const automationsPaused = statusQ.data?.automationsPaused;

  useViewKeyboard(
    (e) => {
      if (handleListNavKey(e, scope, display.length)) return true;
      const cur = display[cursor];
      switch (e.key) {
        case "T":
          toggleDetail();
          return true;
        case "z":
          toggleLogs();
          return true;
        case "o":
        case "O":
          if (cur?.kind === "task") void openTaskInControl(cur.task);
          return true;
        case "Enter":
          if (cur?.kind === "task") void openTaskInControl(cur.task);
          else if (cur?.kind === "auto") {
            if (childRunTasks(cur.row.id, tasks).length > 0) toggleExpand(cur.row);
            else configure(cur.row);
          }
          return true;
        case "x":
          if (cur?.kind === "auto") execute(cur.row);
          return true;
        case "e":
          if (cur?.kind === "auto") configure(cur.row);
          return true;
        case " ":
          if (cur?.kind === "auto") toggle(cur.row);
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
    [display, cursor, tasks, automationsPaused, instanceByTaskId, goalByEntryId],
  );

  useEffect(() => {
    const el = document.querySelector<HTMLElement>('[data-cursor="1"]');
    el?.scrollIntoView({ block: "nearest" });
  }, [cursor]);

  const loading = dataQ.isLoading && !dataQ.data;

  const cur = display[cursor];
  const selectedPath = cur ? (cur.kind === "auto" ? cur.row.path : cur.task.path) : null;
  const logTarget =
    cur?.kind === "task"
      ? { taskId: cur.task.id, projectId: cur.task.project_id }
      : null;

  return (
    <ListDetail detailPath={selectedPath} logTarget={logTarget}>
      <div className="row" style={{ gap: 8, padding: "2px 2px 6px", alignItems: "center" }}>
        {automationsPaused && <Pill color="var(--red)">automations paused</Pill>}
        <div style={{ flex: 1 }} />
        <span className="faint" style={{ fontSize: 11.5 }}>
          x run · Spc toggle · Enter expand · o open/review · n new goal
        </span>
        <button className="btn sm primary" onClick={() => setCreating(true)} title="New goal (n)">
          + New goal
        </button>
      </div>

      {loading ? (
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
          {display.map((entry, i) =>
            entry.kind === "auto" ? (
              <AutomationRowView
                key={"a:" + entry.row.id}
                row={entry.row}
                cursored={i === cursor}
                hasChildren={childRunTasks(entry.row.id, tasks).length > 0}
                expanded={expandedId === entry.row.id}
                isGoalConfigurable={!!goalByEntryId.get(entry.row.id)}
                progress={
                  entry.row.isGoal
                    ? progressByFeature.get(goalByEntryId.get(entry.row.id)?.feature_id ?? "")
                    : undefined
                }
                onToggle={() => toggle(entry.row)}
                onExecute={() => execute(entry.row)}
                onReconcile={() => reconcile(entry.row)}
                onConfigure={() => configure(entry.row)}
                onExpand={() => toggleExpand(entry.row)}
              />
            ) : (
              <RunTaskRow
                key={"t:" + entry.task.id}
                task={entry.task}
                cursored={i === cursor}
                live={instanceByTaskId.has(entry.task.id)}
                onOpen={() => void openTaskInControl(entry.task)}
              />
            ),
          )}
        </div>
      )}

      {editing && <GoalConfigModal goal={editing} onClose={() => setEditing(null)} />}
      {creating && (
        <NewGoalModal
          onClose={() => setCreating(false)}
          onCreated={() => void qc.invalidateQueries({ queryKey: ["goals"] })}
        />
      )}
    </ListDetail>
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
  hasChildren,
  expanded,
  isGoalConfigurable,
  progress,
  onToggle,
  onExecute,
  onReconcile,
  onConfigure,
  onExpand,
}: {
  row: AutomationRow;
  cursored: boolean;
  hasChildren: boolean;
  expanded: boolean;
  isGoalConfigurable: boolean;
  progress?: GoalProgress;
  onToggle: () => void;
  onExecute: () => void;
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
        {hasChildren ? (expanded ? "▾ " : "▸ ") : "  "}
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
      <span className="suffix" style={{ color: row.enabled ? "var(--green)" : "var(--fg-faint)" }}>
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
      {row.source === "automation" && (
        <span
          className="suffix"
          style={{ cursor: "pointer", color: "var(--green)" }}
          title="Execute now (x)"
          onClick={(e) => { e.stopPropagation(); onExecute(); }}
        >
          ▶
        </span>
      )}
      {isGoalConfigurable && (
        <span
          className="suffix"
          style={{ cursor: "pointer", color: "var(--blue)" }}
          title="Reconcile"
          onClick={(e) => { e.stopPropagation(); onReconcile(); }}
        >
          ⟳
        </span>
      )}
    </div>
  );
}

function RunTaskRow({
  task,
  cursored,
  live,
  onOpen,
}: {
  task: BrainEntry;
  cursored: boolean;
  live: boolean;
  onOpen: () => void;
}) {
  const statusColor =
    task.status === "completed" || task.status === "validated"
      ? "var(--green)"
      : task.status === "blocked"
        ? "var(--red)"
        : task.status === "in_progress" || task.status === "active"
          ? "var(--yellow)"
          : "var(--fg-faint)";
  return (
    <div
      className={`tree-row ${cursored ? "cursor" : ""}`}
      data-cursor={cursored ? "1" : undefined}
      style={{ gap: 4 }}
      onClick={onOpen}
      title={live ? "Open live session in Control (o)" : "Review session in Control (o)"}
    >
      <span className="connector">{"   ├─ "}</span>
      <span className="glyph" style={{ color: statusColor }} title={task.status}>
        ●
      </span>
      <span className="suffix faint mono">{task.id}</span>
      <span className="suffix faint">[{task.status || "unknown"}]</span>
      <span className="title truncate">{task.title}</span>
      <span
        className="suffix"
        style={{ color: live ? "var(--cyan, var(--teal))" : "var(--blue)" }}
        title={live ? "Open live session in Control (o)" : "Review session in Control (o)"}
      >
        {live ? "⧉ open" : "⊙ review"}
      </span>
    </div>
  );
}
