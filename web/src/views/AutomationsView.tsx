import { lazy, Suspense, useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useUI, ALL_PROJECTS } from "../store/ui";
import { useNav } from "../store/nav";
import { listNavHandlers } from "../lib/keymap/listNav";
import { useActions } from "../lib/keymap/useActions";
import { AUTOMATIONS_SPECS } from "./automations/keymap";
import { usePaneNavigation } from "../lib/usePaneNavigation";
import { useIsMobile } from "../hooks/useIsMobile";
import {
  createEntry,
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
  deleteEntry,
} from "../lib/api";
import { Pill } from "../components/common/Badge";
import { ConfirmDialog } from "../components/common/Modal";
import { EmptyState, ErrorState, Loading } from "../components/common/states";
import { ListDetail } from "../components/layout/ListDetail";
import { deriveAutomationsPaused } from "../components/layout/StatusBar";
import { GoalConfigModal } from "./automations/GoalConfigModal";
import { NewAutomationModal } from "./automations/NewGoalModal";
import {
  AUTOMATION_RUN_TASK_PAGE_SIZE,
  type AutomationDisplayEntry,
  type AutomationRow,
  automationShowMoreKey,
  childRunTasks,
  flattenAutomationDisplay,
  normalizeAutomationRows,
  triggerLabel,
} from "./automations/rows";
import type { BrainEntry, GoalSummary, OpencodeInstance, SessionInfo } from "../lib/types";
import { relativeTime } from "../lib/format";

// CodeMirror is heavy — load the editor only when the user edits.
const EntryEditModal = lazy(() =>
  import("./brain/EntryEditModal").then((m) => ({ default: m.EntryEditModal })),
);

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

const runTaskKey = (task: BrainEntry) => `automation-task:${task.id}`;

export function AutomationsView() {
  // Scope to the active project (built-ins are always included); the "all" tab
  // shows every project's automations.
  const activeProject = useUI((s) => s.activeProject);
  const project = activeProject === ALL_PROJECTS ? undefined : activeProject;
  const toast = useUI((s) => s.toast);
  const openInControl = useUI((s) => s.openInControl);
  const toggleDetail = useUI((s) => s.toggleDetail);
  const toggleLogs = useUI((s) => s.toggleLogs);
  const openInspect = useUI((s) => s.openInspect);
  const isMobile = useIsMobile();
  const qc = useQueryClient();
  const nav = useNav();
  const selected = useNav((s) => s.selected);

  const [editing, setEditing] = useState<GoalSummary | null>(null);
  const [editEntry, setEditEntry] = useState<{ path: string; title?: string } | null>(null);
  const [creating, setCreating] = useState(false);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [visibleRunTaskLimits, setVisibleRunTaskLimits] = useState<Record<string, number>>({});
  const [confirmDel, setConfirmDel] = useState<BrainEntry[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [query, setQuery] = useState("");
  const [searchOpen, setSearchOpen] = useState(false);
  const searchRef = useRef<HTMLInputElement>(null);

  const dataQ = useQuery({
    queryKey: ["automation-data", project ?? "all"],
    queryFn: () => listAutomationData(project),
    refetchInterval: 15_000,
    staleTime: 15_000,
  });
  const goalsQ = useQuery({ queryKey: ["goals"], queryFn: listGoals, staleTime: 30_000 });
  const statusQ = useQuery({
    queryKey: ["runner-status"],
    queryFn: getRunnerStatus,
    refetchInterval: 10_000,
    staleTime: 10_000,
  });
  const instancesQ = useQuery({
    queryKey: ["instances"],
    queryFn: listInstances,
    refetchInterval: 10_000,
    staleTime: 10_000,
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

  const filterText = query.trim().toLowerCase();
  const filteredRows = useMemo(() => {
    if (!filterText) return rows;
    return rows.filter((row) =>
      [row.title, row.id, row.source, row.scope, row.status, row.triggerKind, row.triggerDetail, row.runSummary, row.featureId]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(filterText)),
    );
  }, [rows, filterText]);
  const filteredTasks = useMemo(() => {
    if (!filterText) return tasks;
    return tasks.filter((task) =>
      [task.title, task.id, task.status, task.project_id, task.feature_id]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(filterText)),
    );
  }, [tasks, filterText]);

  const display = useMemo<AutomationDisplayEntry[]>(
    () => flattenAutomationDisplay(filteredRows, filteredTasks, expandedId, visibleRunTaskLimits),
    [filteredRows, filteredTasks, expandedId, visibleRunTaskLimits],
  );

  const scope = "automations";
  const cursor = useNav((s) =>
    Math.min(s.cursor[scope] ?? 0, Math.max(0, display.length - 1)),
  );
  const runTaskList = useMemo(
    () => display.filter((entry): entry is Extract<AutomationDisplayEntry, { kind: "task" }> => entry.kind === "task").map((entry) => entry.task),
    [display],
  );
  const selectedRunTasks = useMemo(
    () => runTaskList.filter((task) => selected[runTaskKey(task)]),
    [runTaskList, selected],
  );
  const selectedRunTaskCount = selectedRunTasks.length;

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

  async function deleteRunTasks(ts: BrainEntry[]) {
    await run(`Deleted ${ts.length}`, () => Promise.all(ts.map((task) => deleteEntry(task.path))));
    nav.clearSelect();
    setConfirmDel(null);
    refresh();
  }

  const deleteTargets = (cur?: AutomationDisplayEntry): BrainEntry[] =>
    selectedRunTasks.length ? selectedRunTasks : cur?.kind === "task" ? [cur.task] : [];

  function execute(row: AutomationRow) {
    if (row.source !== "automation") {
      toast("Only automations can be executed", "info");
      return;
    }
    void run("Automation triggered", () => executeAutomation(row.path || row.id, project ?? "all")).then(() => {
      setExpandedId(row.id); // reveal the new task underneath
      refresh();
    });
  }

  async function toggleBuiltInAutomation(row: AutomationRow, status: string) {
    if (!project) throw new Error("Select a project before toggling built-in automations");
    const key = automationTemplateKey(row);
    const existing = automations.find(
      (entry) => automationTemplateKey(entry) === key && automationEntryScope(entry) === "project",
    );
    if (existing) {
      await updateEntry(existing.path || existing.id, { status });
      return;
    }

    const template = await getEntry(row.path || row.id);
    await createEntry({
      type: "automation",
      title: template.title,
      content: template.content,
      tags: template.tags,
      status,
      priority: template.priority,
      project,
      trigger: template.trigger,
      action: template.action,
      agent: template.agent,
      model: template.model,
      executor: template.executor,
      execution_mode: template.execution_mode,
      complete_on_idle: template.complete_on_idle,
      target_workdir: template.target_workdir,
      generated_by: template.generated_by,
    });
  }

  function toggle(row: AutomationRow) {
    const nextStatus = row.enabled ? "archived" : "active";
    void run(row.enabled ? "Disabled" : "Enabled", () => {
      if (row.source === "automation") {
        if (row.scope === "built-in") return toggleBuiltInAutomation(row, nextStatus);
        return updateEntry(row.path || row.id, { status: nextStatus });
      }
      return updateEntry(row.path || row.id, { schedule_enabled: !row.enabled });
    }).then(refresh);
  }


  function automationEntryScope(entry: BrainEntry): string {
    if (entry.path?.startsWith("global/")) return "built-in";
    if (entry.project_id || entry.path?.startsWith("projects/")) return "project";
    return "unknown";
  }

  function automationTemplateKey(entry: Pick<BrainEntry, "id" | "path" | "title" | "generated_by"> | AutomationRow): string {
    if ("generated_by" in entry && entry.generated_by) return `generated:${entry.generated_by}`;
    if (entry.title) return `title:${entry.title.trim().toLowerCase()}`;
    return entry.path || entry.id;
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

  // Edit the underlying markdown file. Goal automations have a structured config
  // modal instead; everything else (built-in/scheduled automations, run-tasks)
  // opens the full-file editor — parity with the TUI's `e` → $EDITOR.
  function edit(row: AutomationRow) {
    if (goalByEntryId.get(row.id)) configure(row);
    else setEditEntry({ path: row.path || row.id, title: row.title });
  }

  function toggleExpand(row: AutomationRow) {
    setExpandedId((id) => {
      if (id === row.id) {
        setVisibleRunTaskLimits((limits) => {
          const next = { ...limits };
          delete next[row.id];
          return next;
        });
        return null;
      }
      setVisibleRunTaskLimits((limits) => ({ ...limits, [row.id]: AUTOMATION_RUN_TASK_PAGE_SIZE }));
      return row.id;
    });
  }

  function showNextRunTaskPage(row: AutomationRow) {
    setVisibleRunTaskLimits((limits) => ({
      ...limits,
      [row.id]: (limits[row.id] ?? AUTOMATION_RUN_TASK_PAGE_SIZE) + AUTOMATION_RUN_TASK_PAGE_SIZE,
    }));
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

  const automationsPaused = !!deriveAutomationsPaused(statusQ.data, activeProject);

  // Pane focus + vim-style scroll (Tab/Shift-Tab + j/k/gg/G/Ctrl-D/U inside
  // the focused detail or logs pane).
  const paneNav = usePaneNavigation();

  function toggleAutomationPause() {
    const scopedProject = activeProject === ALL_PROJECTS ? undefined : activeProject;
    void run(
      automationsPaused ? "Automations resumed" : "Automations paused",
      () => (automationsPaused ? resumeAutomations(scopedProject) : pauseAutomations(scopedProject)),
    ).then(() => void qc.invalidateQueries({ queryKey: ["runner-status"] }));
  }

  // Pane scroll/focus dispatches via the pane-tier scope registered by
  // usePaneNavigation; list-scoped specs carry when:{focus:["tasks"]}.
  useActions(
    "view:automations",
    "view",
    AUTOMATIONS_SPECS,
    {
      ...listNavHandlers("automations", { scope: () => scope, count: () => display.length }),
      "automations.toggleDetail": () => toggleDetail(),
      "automations.toggleLogs": () => toggleLogs(),
      "automations.open": () => {
        const cur = display[cursor];
        if (cur?.kind === "task") void openTaskInControl(cur.task);
      },
      "automations.enter": () => {
        const cur = display[cursor];
        if (cur?.kind === "task") void openTaskInControl(cur.task);
        else if (cur?.kind === "show-more") showNextRunTaskPage(cur.parent);
        else if (cur?.kind === "auto") {
          if (childRunTasks(cur.row.id, tasks).length > 0) toggleExpand(cur.row);
          else configure(cur.row);
        }
      },
      "automations.run": () => {
        const cur = display[cursor];
        if (cur?.kind === "auto") execute(cur.row);
      },
      "automations.edit": () => {
        const cur = display[cursor];
        if (cur?.kind === "auto") edit(cur.row);
        else if (cur?.kind === "task") setEditEntry({ path: cur.task.path, title: cur.task.title });
      },
      "automations.select": () => {
        const cur = display[cursor];
        if (cur?.kind === "task") nav.toggleSelect(runTaskKey(cur.task));
        else if (cur?.kind === "show-more") showNextRunTaskPage(cur.parent);
        else if (cur?.kind === "auto") toggle(cur.row);
      },
      "automations.selectAll": () => nav.selectMany(runTaskList.map(runTaskKey)),
      "automations.deselect": () => nav.clearSelect(),
      "automations.delete": () => {
        const ts = deleteTargets(display[cursor]);
        if (ts.length) setConfirmDel(ts);
      },
      "automations.pause": () => toggleAutomationPause(),
      "automations.refresh": () => refresh(),
      "automations.new": () => setCreating(true),
      "automations.filter": () => setSearchOpen(true),
    },
    [display, cursor, tasks, automationsPaused, instanceByTaskId, goalByEntryId, runTaskList, selectedRunTasks],
  );

  useEffect(() => {
    if (searchOpen) searchRef.current?.focus();
  }, [searchOpen]);

  useEffect(() => {
    const el = document.querySelector<HTMLElement>('[data-cursor="1"]');
    el?.scrollIntoView({ block: "nearest" });
  }, [cursor]);

  const loading = dataQ.isLoading && !dataQ.data;

  const cur = display[cursor];
  const selectedPath = cur?.kind === "auto" ? cur.row.path : cur?.kind === "task" ? cur.task.path : null;
  const logTarget =
    cur?.kind === "task"
      ? { taskId: cur.task.id, projectId: cur.task.project_id, taskPath: cur.task.path }
      : null;

  return (
    <ListDetail detailPath={selectedPath} logTarget={logTarget} paneNav={paneNav}>
      {searchOpen && (
        <div className="search-layer" onMouseDown={(e) => { if (e.target === e.currentTarget) setSearchOpen(false); }}>
          <div className="search-popup">
            <span className="search-prompt">/</span>
            <input
              ref={searchRef}
              placeholder="filter automations"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Escape") setSearchOpen(false);
                if (e.key === "Enter") setSearchOpen(false);
              }}
            />
            <button className="btn sm" type="button" onClick={() => { setQuery(""); searchRef.current?.focus(); }} disabled={!query}>
              Clear
            </button>
            <button className="btn sm" type="button" onClick={refresh}>
              Refresh
            </button>
            <button className={`btn sm `} type="button" onClick={toggleAutomationPause}>
              {automationsPaused ? "resume" : "pause"}
            </button>
            <button className="btn sm primary" type="button" onClick={() => { setSearchOpen(false); setCreating(true); }}>
              + New
            </button>
          </div>
        </div>
      )}
      <div className="row" style={{ gap: 8, padding: "2px 2px 6px", alignItems: "center" }}>
        {automationsPaused && (
          <Pill color="var(--red)">
            {activeProject === ALL_PROJECTS ? "automations paused" : `automations paused: ${activeProject}`}
          </Pill>
        )}
        {selectedRunTaskCount > 0 && (
          <span className="selbar-inline">
            {selectedRunTaskCount} selected
            <button className="btn sm ghost" onClick={() => nav.clearSelect()}>
              clear
            </button>
            <button className="btn sm danger" onClick={() => setConfirmDel(selectedRunTasks)}>
              delete
            </button>
          </span>
        )}
        <div style={{ flex: 1 }} />
        <button className="btn sm primary" onClick={() => setCreating(true)} title="New automation (n)">
          + New automation
        </button>
      </div>

      {loading ? (
        <Loading label="Loading automations…" />
      ) : dataQ.error ? (
        <ErrorState error={dataQ.error} onRetry={() => void dataQ.refetch()} />
      ) : !display.length ? (
        <EmptyState
          glyph="⟳"
          title="No automations"
          hint={query ? `Nothing matched “”.` : "Built-in automations, scheduled tasks, and goals appear here. Press n to create a new automation."}
        />
      ) : (
        <div>
          {display.map((entry, i) => {
            if (entry.kind === "auto") {
              return (
                <AutomationRowView
                  key={"a:" + entry.row.id}
                  row={entry.row}
                  cursored={i === cursor}
                  hasChildren={childRunTasks(entry.row.id, filteredTasks).length > 0}
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
              );
            }
            if (entry.kind === "show-more") {
              return (
                <ShowMoreRunTasksRow
                  key={automationShowMoreKey(entry.parent.id)}
                  cursored={i === cursor}
                  shown={entry.shown}
                  total={entry.total}
                  remaining={entry.remaining}
                  onShowMore={() => showNextRunTaskPage(entry.parent)}
                />
              );
            }
            return (
              <RunTaskRow
                key={"t:" + entry.task.id}
                task={entry.task}
                cursored={i === cursor}
                live={instanceByTaskId.has(entry.task.id)}
                selected={!!selected[runTaskKey(entry.task)]}
                selecting={selectedRunTaskCount > 0}
                onSelect={() => nav.toggleSelect(runTaskKey(entry.task))}
                onOpen={() => void openTaskInControl(entry.task)}
              />
            );
          })}
        </div>
      )}

      {editing && <GoalConfigModal goal={editing} onClose={() => setEditing(null)} />}
      {editEntry && (
        <Suspense fallback={null}>
          <EntryEditModal
            path={editEntry.path}
            title={editEntry.title}
            onClose={() => setEditEntry(null)}
            onSaved={refresh}
          />
        </Suspense>
      )}
      {creating && (
        <NewAutomationModal
          onClose={() => setCreating(false)}
          onCreated={() => { void qc.invalidateQueries({ queryKey: ["goals"] }); refresh(); }}
        />
      )}
      {confirmDel && (
        <ConfirmDialog
          title={confirmDel.length > 1 ? `Delete ${confirmDel.length} automation tasks?` : "Delete automation task?"}
          danger
          confirmLabel="Delete"
          busy={busy}
          message={
            confirmDel.length > 1
              ? <>This permanently deletes {confirmDel.length} generated automation tasks.</>
              : <>This permanently deletes <strong>{confirmDel[0].title}</strong>.</>
          }
          onClose={() => setConfirmDel(null)}
          onConfirm={() => void deleteRunTasks(confirmDel)}
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
      className={`tree-row auto-row ${cursored ? "cursor" : ""}`}
      data-cursor={cursored ? "1" : undefined}
      style={{ gap: 4 }}
      onClick={() => (isGoalConfigurable ? onConfigure() : hasChildren ? onExpand() : undefined)}
    >
      <span
        className="connector"
        style={{
          display: "inline-block",
          width: "1.2em",
          textAlign: "center",
          flexShrink: 0,
          cursor: hasChildren ? "pointer" : undefined,
          color: "var(--cyan, var(--teal))",
        }}
        onClick={(e) => { if (hasChildren) { e.stopPropagation(); onExpand(); } }}
      >
        {hasChildren ? (expanded ? "▾" : "▸") : ""}
      </span>
      <span
        className="glyph"
        style={{ color: cursored ? undefined : row.enabled ? "var(--green)" : "var(--fg-faint)" }}
        title={row.enabled ? "enabled — click to disable" : "disabled — click to enable"}
        onClick={(e) => { e.stopPropagation(); onToggle(); }}
      >
        {row.enabled ? "◉" : "○"}
      </span>
      <span className="suffix faint auto-source" style={{ minWidth: 64, flexShrink: 0 }}>
        {row.scope === "built-in" ? "built-in" : row.source}
      </span>
      <span className={`title truncate auto-title ${row.enabled ? "" : "faint"}`}>
        {row.title}
        {row.isGoal && <span style={{ color: "var(--purple)" }}> [goal]</span>}
      </span>
      <span className="suffix auto-enabled" style={{ color: row.enabled ? "var(--green)" : "var(--fg-faint)" }}>
        [{row.enabled ? "enabled" : "disabled"}]
      </span>
      {progress && progress.total > 0 && (
        <span className="suffix auto-progress" title={`${progress.completed}/${progress.total} complete`}>
          <span style={{ color: progress.completed === progress.total ? "var(--green)" : "var(--yellow)" }}>
            {bar(progress.completed, progress.total)}
          </span>{" "}
          <span className="faint">
            {progress.completed}/{progress.total}
            {progress.blocked > 0 && <span style={{ color: "var(--red)" }}> ✗{progress.blocked}</span>}
          </span>
        </span>
      )}
      <span className="suffix faint truncate auto-trigger">{triggerLabel(row)}</span>
      {row.runSummary && (
        <span className="suffix auto-run" style={{ color: "var(--yellow)" }}>
          run: {row.runSummary}
          {row.runTaskID && <span className="faint"> #{row.runTaskID}</span>}
        </span>
      )}
      {row.source === "automation" && (
        <span
          className="suffix auto-action"
          style={{ cursor: "pointer", color: "var(--green)" }}
          title="Execute now (x)"
          onClick={(e) => { e.stopPropagation(); onExecute(); }}
        >
          ▶
        </span>
      )}
      {isGoalConfigurable && (
        <span
          className="suffix auto-action"
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

function ShowMoreRunTasksRow({
  cursored,
  shown,
  total,
  remaining,
  onShowMore,
}: {
  cursored: boolean;
  shown: number;
  total: number;
  remaining: number;
  onShowMore: () => void;
}) {
  const nextCount = Math.min(AUTOMATION_RUN_TASK_PAGE_SIZE, remaining);
  return (
    <button
      type="button"
      className={`tree-row ${cursored ? "cursor" : ""}`}
      data-cursor={cursored ? "1" : undefined}
      style={{
        gap: 4,
        width: "100%",
        border: 0,
        background: "transparent",
        color: "var(--cyan, var(--teal))",
        font: "inherit",
        textAlign: "left",
        cursor: "pointer",
        fontStyle: "italic",
      }}
      onClick={onShowMore}
      title="Show the next page of generated automation tasks"
    >
      <span className="connector">{"   └─ "}</span>
      <span className="glyph">▾</span>
      <span className="title truncate">Show {nextCount} more</span>
      <span className="suffix faint">({shown}/{total} shown)</span>
    </button>
  );
}

function RunTaskRow({
  task,
  cursored,
  live,
  selected,
  selecting,
  onSelect,
  onOpen,
}: {
  task: BrainEntry;
  cursored: boolean;
  live: boolean;
  selected: boolean;
  selecting: boolean;
  onSelect: () => void;
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
      className={`tree-row ${cursored ? "cursor" : ""} ${selected ? "kbd-cursor" : ""}`}
      data-cursor={cursored ? "1" : undefined}
      style={{ gap: 4 }}
      onClick={selecting ? onSelect : onOpen}
      title={live ? "Open live session in Control (o)" : "Review session in Control (o)"}
    >
      <span className="connector">{"   ├─ "}</span>
      {selecting && (
        <span
          className="checkbox"
          onClick={(e) => { e.stopPropagation(); onSelect(); }}
        >
          {selected ? "[x] " : "[ ] "}
        </span>
      )}
      <span className="glyph" style={{ color: statusColor }} title={task.status}>
        ●
      </span>
      <span className="suffix faint mono">{task.id}</span>
      <span className="suffix faint">[{task.status || "unknown"}]</span>
      <span className="title truncate">{task.title}</span>
      {(task.modified || task.created) && (
        <span
          className="suffix faint mono"
          title={new Date(task.modified ?? task.created ?? "").toLocaleString()}
        >
          {relativeTime(task.modified ?? task.created)}
        </span>
      )}
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
