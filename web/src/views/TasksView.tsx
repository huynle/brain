import { lazy, Suspense, useEffect, useMemo, useRef, useState } from "react";
import { useUI, ALL_PROJECTS } from "../store/ui";
import { useNav } from "../store/nav";
import { useScope } from "../store/scope";
import { listNavHandlers } from "../lib/keymap/listNav";
import { useActions } from "../lib/keymap/useActions";
import { FEATURE_SORT_FIELDS, TASKS_SPECS } from "./tasks/keymap";
import { usePaneNavigation } from "../lib/usePaneNavigation";
import { useIsMobile } from "../hooks/useIsMobile";
import { useLiveTasks } from "../hooks/useLiveTasks";
import { filterTasks, groupByFeature, isReadyTask, UNGROUPED, type FeatureSortMode } from "./tasks/grouping";
import { mergeReadyFeatures } from "./tasks/mergeAttention";
import { useQuery } from "@tanstack/react-query";
import { buildTaskTree } from "./tasks/tree";
import { MetadataModal } from "./tasks/MetadataModal";
import { BatchMetadataModal } from "./tasks/BatchMetadataModal";
import { FeatureMetadataModal } from "./tasks/FeatureMetadataModal";
import { Panel } from "../components/layout/Panel";
import { TaskSessionPane } from "../components/layout/TaskSessionPane";
import { PaneSplitterRow, PaneSplitterColumn } from "../components/layout/PaneSplitters";
import { ConfirmDialog } from "../components/common/Modal";
import { deleteEntry, listEntries, listInstances, runFeature, runOrTriggerTask, setTaskStatus, summarizeRunFeatureResult, summarizeTriggerResults } from "../lib/api";
import {
  isActive,
  relativeTime,
  statusColor,
} from "../lib/format";
import type { Task } from "../lib/types";

const ComposeModal = lazy(() =>
  import("../components/compose/ComposeModal").then((m) => ({
    default: m.ComposeModal,
  })),
);

// CodeMirror is heavy — load the editor only when the user edits.
const EntryEditModal = lazy(() =>
  import("./brain/EntryEditModal").then((m) => ({ default: m.EntryEditModal })),
);
const EntryRawViewModal = lazy(() =>
  import("./brain/EntryRawViewModal").then((m) => ({ default: m.EntryRawViewModal })),
);

const TERMINAL = ["completed", "cancelled", "archived", "superseded"];
// Done-mode sort: flat newest-completion-first, or grouped by feature.
const DONE_SORT_FIELDS = ["completed", "feature"] as const;
function completionMs(t: Task): number {
  const n = Date.parse(t.completed_at || t.modified || "");
  return Number.isNaN(n) ? 0 : n;
}
const FEATURE_SORT_LABELS: Record<FeatureSortMode, string> = {
  completed: "done",
  created: "new",
  name: "name",
  status: "status",
  priority: "prio",
};
const taskKey = (t: Task) => `${t.projectId}:${t.id}`;

function glyph(t: Task): { ch: string; color: string } {
  switch (t.status) {
    case "in_progress":
    case "active":
      return { ch: "▶", color: "var(--blue)" };
    case "blocked":
      return { ch: "✗", color: "var(--red)" };
    case "completed":
    case "validated":
      return { ch: "✓", color: "var(--green)" };
    case "cancelled":
    case "archived":
    case "superseded":
      return { ch: "⊘", color: "var(--fg-faint)" };
    case "draft":
      return { ch: "◇", color: "var(--purple)" };
    default: {
      const waiting = (t.waiting_on?.length ?? 0) > 0 || (t.blocked_by?.length ?? 0) > 0;
      return waiting
        ? { ch: "○", color: "var(--yellow)" }
        : { ch: "●", color: "var(--green)" };
    }
  }
}

type Row =
  | { kind: "header"; feature: string; label: string; count: number }
  | { kind: "task"; task: Task; lead: string; inCycle: boolean };

export function TasksView() {
  const activeProject = useUI((s) => s.activeProject);
  const wrap = useUI((s) => s.wrap);
  const toast = useUI((s) => s.toast);
  const focus = useUI((s) => s.focus);
  const detailVisible = useUI((s) => s.detailVisible);
  const logsVisible = useUI((s) => s.logsVisible);
  const bottomHeight = useUI((s) => s.bottomHeight);
  const detailLogsRatio = useUI((s) => s.detailLogsRatio);
  const toggleDetail = useUI((s) => s.toggleDetail);
  const toggleLogs = useUI((s) => s.toggleLogs);
  const openInspect = useUI((s) => s.openInspect);
  const openInControl = useUI((s) => s.openInControl);
  const isMobile = useIsMobile();

  const mode = useUI((s) => s.tasksMode);
  const setTasksMode = useUI((s) => s.setTasksMode);
  const includeCancelled = useUI((s) => s.doneIncludeCancelled);
  const toggleIncludeCancelled = useUI((s) => s.toggleDoneIncludeCancelled);
  const mergeOnly = useUI((s) => s.doneMergeOnly);
  const setMergeOnly = useUI((s) => s.setDoneMergeOnly);

  const { tasks: liveTasks, connected } = useLiveTasks(activeProject);

  // Done mode reaches past the (limit-bounded) SSE snapshot with a completed-
  // history query ordered by the completed_at stamp server-side; live tasks
  // win on id collisions (they are fresher).
  const historyQ = useQuery({
    queryKey: ["done-history", activeProject],
    queryFn: () =>
      listEntries({
        type: "task",
        status: "completed",
        sortBy: "completed",
        sortOrder: "desc",
        limit: 200,
        ...(activeProject !== ALL_PROJECTS ? { project: activeProject } : {}),
      }),
    enabled: false,
    staleTime: 30_000,
  });
  const historyEnabled = mode === "done";
  useEffect(() => {
    if (historyEnabled) void historyQ.refetch();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [historyEnabled, activeProject]);
  const tasks = useMemo(() => {
    if (mode !== "done") return liveTasks;
    const byId = new Map<string, Task>();
    for (const e of historyQ.data?.entries ?? []) {
      byId.set(e.id, {
        ...(e as unknown as Task),
        projectId: (e as { project_id?: string }).project_id,
      });
    }
    for (const t of liveTasks) byId.set(t.id, t);
    return [...byId.values()];
  }, [mode, liveTasks, historyQ.data]);

  const nav = useNav();
  const scope = `tasks:${activeProject}`;
  const cursor = useNav((s) => s.cursor[scope] ?? 0);
  const selected = useNav((s) => s.selected);

  // Filter and sort live in the scope store so the ContextBar can show them
  // and the global Esc chain can clear/unwind them.
  const query = useScope((s) => s.filter["tasks"] ?? "");
  const setScopeFilter = useScope((s) => s.setFilter);
  const setQuery = (q: string) => setScopeFilter("tasks", q);
  const featureSort = (useScope((s) => s.sort["tasks"]?.field) ?? "completed") as FeatureSortMode;
  const sortDir = useScope((s) => s.sort["tasks"]?.dir) ?? "desc";
  const setScopeSort = useScope((s) => s.setSort);
  const setFeatureSort = (f: FeatureSortMode) => setScopeSort("tasks", { field: f, dir: "desc" });
  const cycleSortField = useScope((s) => s.cycleSortField);
  const toggleSortDir = useScope((s) => s.toggleSortDir);
  const setCounts = useScope((s) => s.setCounts);
  const drillStack = useScope((s) => s.stack);
  const pushFrame = useScope((s) => s.push);
  const featureFrame = [...drillStack].reverse().find((f) => f.view === "tasks" && f.kind === "feature");
  const [searchOpen, setSearchOpen] = useState(false);
  const [showDone, setShowDone] = useState(false);
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const [collapseDefault, setCollapseDefault] = useState(activeProject === ALL_PROJECTS);

  const [editMeta, setEditMeta] = useState<Task | null>(null);
  const [viewContent, setViewContent] = useState<Task | null>(null);
  const [editContent, setEditContent] = useState<Task | null>(null);
  const [batchMeta, setBatchMeta] = useState<Task[] | null>(null);
  const [featureMeta, setFeatureMeta] = useState<{ feature: string; tasks: Task[] } | null>(null);
  const [confirmDel, setConfirmDel] = useState<Task[] | null>(null);
  const [composing, setComposing] = useState(false);
  const [busy, setBusy] = useState(false);

  const filterRef = useRef<HTMLInputElement>(null);
  const treeBodyRef = useRef<HTMLDivElement>(null);
  // The bottom row container ref is used by the column splitter to translate
  // cursor X into a detail/logs ratio (we need a stable rect, not the
  // splitter's own — that moves as the user drags).
  const bottomRowRef = useRef<HTMLDivElement>(null);

  // Pane focus + vim-style scroll wiring (Tab/Shift-Tab + j/k/gg/G/Ctrl-D/U
  // inside the focused detail or logs pane). The hook owns the detail and
  // logs body refs so it can drive scrollTop.
  const paneNav = usePaneNavigation();

  const { rows, taskList, featureKeys, tasksByFeature } = useMemo(() => {
    let list = filterTasks(tasks, query);
    if (mode === "schedules") list = list.filter((t) => t.schedule || t.run_once_at);
    else if (mode === "done") {
      const doneSet = includeCancelled
        ? new Set([...TERMINAL, "validated"])
        : new Set(["completed", "validated"]);
      list = list.filter((t) => doneSet.has(t.status));
      if (mergeOnly) {
        const ready = new Set(mergeReadyFeatures(list).map((m) => m.feature));
        list = list.filter((t) => t.feature_id && ready.has(t.feature_id));
      }
    } else if (!showDone) list = list.filter((t) => !TERMINAL.includes(t.status));
    // Drill-down: a feature frame (Enter on its header) scopes the whole
    // view to that feature; Esc pops back out.
    if (featureFrame) {
      list = list.filter((t) => (t.feature_id || UNGROUPED) === featureFrame.id);
    }
    if (mode === "done" && (featureSort as string) !== "feature") {
      // Flat history, newest completion first (completed_at, modified fallback).
      const done = [...list].sort((a, b) => completionMs(b) - completionMs(a));
      const flatRows: Row[] = done.map((t) => ({ kind: "task", task: t, lead: "", inCycle: false }));
      return { rows: flatRows, taskList: done, featureKeys: [], tasksByFeature: new Map() };
    }
    const groups = groupByFeature(list, mode === "done" ? "completed" : activeProject === ALL_PROJECTS ? featureSort : "name", sortDir);
    const r: Row[] = [];
    const flat: Task[] = [];
    const keys: string[] = [];
    const byFeature = new Map<string, Task[]>();
    for (const g of groups) {
      byFeature.set(g.feature, g.tasks);
      const showHeader = (g.feature !== UNGROUPED || groups.length > 1) && !featureFrame;
      if (showHeader) {
        keys.push(g.feature);
        r.push({ kind: "header", feature: g.feature, label: g.label, count: g.tasks.length });
      }
      if (featureFrame || !(collapsed[g.feature] ?? collapseDefault)) {
        if (mode === "schedules") {
          g.tasks.forEach((t) => {
            r.push({ kind: "task", task: t, lead: "", inCycle: false });
            flat.push(t);
          });
        } else {
          // build the dependency tree within this feature group (TUI parity)
          for (const tr of buildTaskTree(g.tasks)) {
            r.push({ kind: "task", task: tr.task, lead: tr.lead, inCycle: tr.inCycle });
            flat.push(tr.task);
          }
        }
      }
    }
    return { rows: r, taskList: flat, featureKeys: keys, tasksByFeature: byFeature };
  }, [tasks, query, showDone, collapsed, collapseDefault, mode, activeProject, featureSort, sortDir, featureFrame, includeCancelled, mergeOnly]);

  // Feed the ContextBar's shown/total counter.
  useEffect(() => {
    setCounts("tasks", taskList.length, tasks.length);
  }, [taskList.length, tasks.length, setCounts]);

  useEffect(() => {
    setCollapsed({});
    setCollapseDefault(activeProject === ALL_PROJECTS);
  }, [activeProject]);

  useEffect(() => {
    if (searchOpen) filterRef.current?.focus();
  }, [searchOpen]);

  useEffect(() => {
    if (cursor > rows.length - 1) nav.setCursor(scope, Math.max(0, rows.length - 1));
  }, [rows.length]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    treeBodyRef.current
      ?.querySelector<HTMLElement>('[data-cursor="1"]')
      ?.scrollIntoView({ block: "nearest" });
  }, [cursor, rows.length]);

  const cursorRow = rows[cursor];
  const detailTask = cursorRow?.kind === "task" ? cursorRow.task : null;

  const setAllFeatureCollapsed = (value: boolean) => {
    setCollapseDefault(value);
    setCollapsed(Object.fromEntries(featureKeys.map((key) => [key, value])));
  };

  const mergeReadySet = useMemo(
    () => new Set(mergeReadyFeatures(tasks).map((m) => m.feature)),
    [tasks],
  );

  const selectedTasks = useMemo(
    () => taskList.filter((t) => selected[taskKey(t)]),
    [taskList, selected],
  );
  const selCount = selectedTasks.length;

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
  const targets = (cur?: Task | null): Task[] =>
    selectedTasks.length ? selectedTasks : cur ? [cur] : [];

  // Run one or more tasks. Prefers /run (push dispatch to a runner) and
  // falls back to /trigger automatically when the server doesn't support
  // /run yet. summarizeTriggerResults still shapes the toast — runOrTrigger
  // returns a TriggerResponse-compatible value.
  //
  // When force=true, this bypasses any existing dispatch lease — used as the
  // recovery action for stuck/orphaned leases (e.g. a runner that crashed
  // silently). Single-task force flows can also surface a follow-up toast
  // action so users don't have to reissue from a different UI.
  async function triggerMany(tasks: Task[], force = false) {
    const eligible = tasks.filter((t) => t.projectId);
    if (!eligible.length) return;
    setBusy(true);
    try {
      const results = await Promise.all(
        eligible.map((t) => runOrTriggerTask(t.projectId!, t.id, force)),
      );
      const { message, kind } = summarizeTriggerResults(results);
      // If exactly one task came back blocked by an existing lease, offer a
      // "Force" action so the user can release+redispatch in one click.
      // Multi-task force is intentionally not auto-offered — batch flows are
      // riskier and the user should be deliberate.
      const blocked = results.find((r) => !r.triggered && r.reasonCode === "already_leased" && r.projectId);
      const isSingleBlocked = !force && results.length === 1 && !!blocked;
      if (isSingleBlocked && blocked) {
        toast(message, kind, {
          action: {
            label: "Force",
            onClick: () => triggerMany(
              eligible.filter((t) => t.id === blocked.taskId),
              true,
            ),
          },
        });
      } else {
        toast(message, kind);
      }
    } catch (e) {
      toast(e instanceof Error ? e.message : "Run failed", "error");
    } finally {
      setBusy(false);
    }
  }

  // runFeatureNow dispatches every ready task in a feature in one server call.
  // Unlike triggerMany (which fires N independent /run requests racing for the
  // same runner slots), this lets the scheduler reserve slots cohesively and
  // queue leftovers for the feature-cascade so the rest fire as slots free —
  // even while the project is paused. Falls back silently to triggerMany on
  // older servers that return 501 Not Implemented.
  async function runFeatureNow(projectId: string, featureId: string, force = false) {
    setBusy(true);
    try {
      const result = await runFeature(projectId, featureId, force);
      const { message, kind } = summarizeRunFeatureResult(result);
      toast(message, kind);
    } catch (e) {
      // 501 fallback: server doesn't support /features/{id}/run yet. Use the
      // per-task path so the user still gets work dispatched.
      const msg = e instanceof Error ? e.message : "";
      if (msg.includes("501") || msg.toLowerCase().includes("not implemented")) {
        const ready = taskList.filter(
          (t) => (t.feature_id || UNGROUPED) === featureId && ["pending", "active"].includes(t.status),
        );
        if (ready.length) {
          await triggerMany(ready, force);
          return;
        }
      }
      toast(msg || "Run feature failed", "error");
    } finally {
      setBusy(false);
    }
  }

  async function openTaskSession(task: Task) {
    if (!isActive(task.status)) {
      setViewContent(task);
      return;
    }
    try {
      const instances = await listInstances();
      const inst = instances.find((i) => i.task_id === task.id && (!task.projectId || i.project_id === task.projectId));
      if (!inst) {
        toast("No live session found for this task", "info");
        return;
      }
      openInControl({
        mode: "live",
        runnerId: inst.runner_id,
        instanceId: inst.instance_id,
        taskTitle: task.title || task.id,
      });
    } catch (e) {
      toast(e instanceof Error ? e.message : "Could not open session", "error");
    }
  }

  // Pane scroll/focus dispatches via the pane-tier scope registered by
  // usePaneNavigation; list-scoped specs carry when:{focus:["tasks"]}.
  useActions(
    "view:tasks",
    "view",
    TASKS_SPECS,
    {
      ...listNavHandlers("tasks", { scope: () => scope, count: () => rows.length }),
      "tasks.toggleDetail": () => toggleDetail(),
      "tasks.toggleLogs": () => toggleLogs(),
      "tasks.enter": () => {
        const row = rows[cursor];
        // Enter DESCENDS (k9s): a feature header pushes a drill frame that
        // scopes the view to that feature (Esc pops back). Collapse
        // toggling lives on Space.
        if (row?.kind === "header") {
          pushFrame({ kind: "feature", id: row.feature, label: row.label, view: "tasks" });
          nav.setCursor(scope, 0);
        } else if (row?.kind === "task") void openTaskSession(row.task);
      },
      "tasks.select": () => {
        const row = rows[cursor];
        if (row?.kind === "header")
          setCollapsed((c) => ({ ...c, [row.feature]: !(c[row.feature] ?? collapseDefault) }));
        else if (row?.kind === "task") nav.toggleSelect(taskKey(row.task));
      },
      "tasks.collapseAll": () => setAllFeatureCollapsed(true),
      "tasks.expandAll": () => setAllFeatureCollapsed(false),
      "tasks.selectAll": () => nav.selectMany(taskList.map(taskKey)),
      "tasks.deselect": () => nav.clearSelect(),
      "tasks.complete": () => {
        const row = rows[cursor];
        const ts = targets(row?.kind === "task" ? row.task : undefined);
        if (ts.length) void run(`Completed ${ts.length}`, () => Promise.all(ts.map((t) => setTaskStatus(t, "completed")))).then(() => nav.clearSelect());
      },
      "tasks.run": () => {
        const row = rows[cursor];
        if (row?.kind === "header") {
          // Whole-feature dispatch: hand the server a single call so it
          // can plan capacity, queue leftovers, and start a cascade for
          // drain-while-paused. UNGROUPED rows fall back to the per-task
          // path because they don't share a feature_id.
          if (row.feature === UNGROUPED) {
            const ready = taskList.filter((t) => !t.feature_id && ["pending", "active"].includes(t.status));
            if (ready.length) void triggerMany(ready);
          } else {
            const sample = taskList.find((t) => t.feature_id === row.feature && t.projectId);
            if (sample?.projectId) void runFeatureNow(sample.projectId, row.feature);
          }
        } else if (row?.kind === "task" && row.task.projectId) void triggerMany([row.task]);
      },
      "tasks.cancel": () => {
        const row = rows[cursor];
        const cur = row?.kind === "task" ? row.task : undefined;
        if (cur && isActive(cur.status)) void run("Cancelled", () => setTaskStatus(cur, "cancelled"));
      },
      "tasks.delete": () => {
        const row = rows[cursor];
        const ts = targets(row?.kind === "task" ? row.task : undefined);
        if (ts.length) setConfirmDel(ts);
      },
      "tasks.editMeta": () => {
        const row = rows[cursor];
        if (row?.kind === "header") {
          const ts = tasksByFeature.get(row.feature) ?? [];
          if (row.feature === UNGROUPED) toast("Ungrouped tasks do not have feature settings", "info");
          else if (ts.length) setFeatureMeta({ feature: row.feature, tasks: ts });
          return;
        }
        const cur = row?.kind === "task" ? row.task : undefined;
        const ts = targets(cur);
        if (ts.length > 1) setBatchMeta(ts);
        else if (cur) setEditMeta(cur);
      },
      "tasks.editFile": () => {
        const row = rows[cursor];
        if (row?.kind === "task") setEditContent(row.task);
      },
      "tasks.copyTitle": () => {
        const row = rows[cursor];
        if (row?.kind === "task") {
          void navigator.clipboard?.writeText(row.task.title || row.task.id);
          toast("Copied title");
        }
      },
      "tasks.sortCycle": () =>
        cycleSortField("tasks", mode === "done" ? DONE_SORT_FIELDS : FEATURE_SORT_FIELDS, "completed"),
      "tasks.sortReverse": () => toggleSortDir("tasks", "completed"),
      "tasks.filter": () => setSearchOpen(true),
      "tasks.mode": () => {
        setTasksMode(mode === "schedules" ? "tasks" : "schedules");
        nav.setCursor(scope, 0);
      },
      "tasks.done": () => {
        setTasksMode(mode === "done" ? "tasks" : "done");
        if (mode === "done") setMergeOnly(false);
        nav.setCursor(scope, 0);
      },
      "tasks.includeCancelled": () => {
        if (mode !== "done") return false;
        toggleIncludeCancelled();
      },
      "tasks.mergeOnly": () => {
        if (mode !== "done") return false;
        setMergeOnly(!mergeOnly);
      },
      "tasks.new": () => setComposing(true),
    },
    [rows, cursor, scope, taskList, selectedTasks, focus, collapseDefault, featureKeys, tasksByFeature, openInControl, toast, featureFrame, mode, mergeOnly, includeCancelled],
  );

  async function doDelete(ts: Task[]) {
    await run(`Deleted ${ts.length}`, () => Promise.all(ts.map((t) => deleteEntry(t.path))));
    nav.clearSelect();
    setConfirmDel(null);
  }

  const showProject = activeProject === ALL_PROJECTS;

  return (
    <>
      <Panel
        title={mode === "schedules" ? "Schedules" : "Tasks"}
        meta={query ? `filter "${query}" · ${taskList.length}` : `${taskList.length}`}
        {...paneNav.tasksPaneProps}
        bodyRef={treeBodyRef}
        style={{ flex: 1 }}
      >
        <div className="search-layer" style={{ display: searchOpen ? undefined : "none" }} onMouseDown={(e) => { if (e.target === e.currentTarget) setSearchOpen(false); }}>
          <div className="search-popup">
          <span className="search-prompt">/</span>
          <input
            ref={filterRef}
            placeholder="filter… (/)"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Escape") setSearchOpen(false); if (e.key === "Enter") setSearchOpen(false); }}
          />
          <button className="btn sm" onClick={() => setTasksMode(mode === "schedules" ? "tasks" : "schedules")}>
            {mode === "schedules" ? "sched" : "tasks"}
          </button>
          {mode === "tasks" && (
            <button className={`btn sm ${showDone ? "primary" : ""}`} onClick={() => setShowDone((v) => !v)}>
              {showDone ? "all" : "active"}
            </button>
          )}
          {activeProject === ALL_PROJECTS && mode === "tasks" && (
            <select
              className="btn sm"
              aria-label="Feature sort"
              title="Sort features"
              value={featureSort}
              onChange={(e) => setFeatureSort(e.target.value as FeatureSortMode)}
            >
              {Object.entries(FEATURE_SORT_LABELS).map(([value, label]) => (
                <option key={value} value={value}>{label}</option>
              ))}
            </select>
          )}
          <button className="btn sm" onClick={() => setAllFeatureCollapsed(true)} title="Collapse all features ({)">{"{"}</button>
          <button className="btn sm" onClick={() => setAllFeatureCollapsed(false)} title="Expand all features (})">{"}"}</button>
          <button className="btn sm primary" onClick={() => { setSearchOpen(false); setComposing(true); }}>+</button>
          </div>
        </div>

        {rows.length === 0 ? (
          <div className="muted" style={{ padding: "8px 4px" }}>
            {!connected && tasks.length === 0
              ? "⧗ connecting to task stream…"
              : query
                ? "no matches"
                : mode === "schedules"
                  ? "no scheduled tasks"
                  : "no active tasks — press n to create one"}
          </div>
        ) : (
          rows.map((row, i) => {
            const isCur = i === cursor;
            if (row.kind === "header") {
              const featureSample = row.feature !== UNGROUPED
                ? taskList.find((t) => t.feature_id === row.feature && t.projectId)
                : undefined;
              return (
                <div
                  key={`h:${row.feature}`}
                  className={`tree-header ${isCur ? "cursor" : ""}`}
                  data-cursor={isCur ? "1" : undefined}
                  onClick={() => setCollapsed((c) => ({ ...c, [row.feature]: !(c[row.feature] ?? collapseDefault) }))}
                  onDoubleClick={() => {
                    const ts = tasksByFeature.get(row.feature) ?? [];
                    if (row.feature !== UNGROUPED && ts.length) setFeatureMeta({ feature: row.feature, tasks: ts });
                  }}
                  title={row.feature === UNGROUPED ? "Enter/Space toggles collapse" : "s opens feature settings · x runs feature · Enter/Space toggles collapse"}
                >
                  <span className="htri">{(collapsed[row.feature] ?? collapseDefault) ? "▸" : "▾"}</span>
                  {row.label}
                  <span className="hcount">
                    {(() => {
                      const ready = (tasksByFeature.get(row.feature) ?? []).filter(isReadyTask).length;
                      return ready > 0 ? (
                        <>({ready}<span style={{ color: "var(--green)" }}>●</span> / {row.count})</>
                      ) : (
                        <>({row.count})</>
                      );
                    })()}
                  </span>
                  {featureSample?.projectId && (
                    <button
                      type="button"
                      className="feature-run"
                      title="Run this feature (dispatch every ready task; queues leftovers as slots free, even if project is paused)"
                      aria-label={`Run feature ${row.feature}`}
                      onClick={(e) => {
                        e.stopPropagation();
                        void runFeatureNow(featureSample.projectId!, row.feature);
                      }}
                    >
                      ▶
                    </button>
                  )}
                </div>
              );
            }
            const t = row.task;
            const g = glyph(t);
            const isSel = !!selected[taskKey(t)];
            return (
              <div
                key={taskKey(t)}
                className={`tree-row ${isCur ? "cursor" : ""}`}
                data-cursor={isCur ? "1" : undefined}
                onClick={() => {
                  nav.setCursor(scope, i);
                  if (isMobile) openInspect({ path: t.path, taskId: t.id, projectId: t.projectId, title: t.title });
                }}
                onDoubleClick={() => void openTaskSession(t)}
              >
                <span className="connector">{row.lead}</span>
                {selCount > 0 && (
                  <span
                    className="checkbox"
                    onClick={(e) => { e.stopPropagation(); nav.toggleSelect(taskKey(t)); }}
                  >
                    {isSel ? "[x] " : "[ ] "}
                  </span>
                )}
                <span className="glyph" style={{ color: isCur ? undefined : g.color }}>{g.ch}</span>
                <span className={`title ${wrap ? "" : "truncate"}`}>
                  {(row.inCycle || t.in_cycle) && <span style={{ color: "var(--red)" }}>↺ </span>}
                  {t.title || t.id}
                  {t.priority === "high" && <span style={{ color: "var(--red)" }}> !</span>}
                </span>
                {showProject && t.projectId && (
                  <span className="suffix faint">{t.projectId.split(/[/\\]/).pop()}</span>
                )}
                {mode === "schedules" && t.schedule && (
                  <span className="suffix" style={{ color: "var(--cyan)" }}>{t.schedule}</span>
                )}
                {mode === "done" && (
                  <>
                    {t.feature_id && mergeReadySet.has(t.feature_id) && (
                      <span className="suffix" style={{ color: "var(--purple)", fontWeight: 700 }} title="Feature fully completed with merge config — probably needs merging">⇡ merge</span>
                    )}
                    {t.feature_id && <span className="suffix" style={{ color: "var(--teal)" }}>{t.feature_id}</span>}
                    {t.executor && <span className="suffix faint">{t.executor}</span>}
                    <span className="suffix faint">{relativeTime(t.completed_at || t.modified || "")}</span>
                  </>
                )}
              </div>
            );
          })
        )}
      </Panel>

      {(detailVisible || logsVisible) && (
        <>
          <PaneSplitterRow />
          <div
            ref={bottomRowRef}
            className="tui-bottom"
            style={{ height: `${bottomHeight}px` }}
          >
            {detailVisible && (
              <Panel
                title="Detail"
                {...paneNav.detailPaneProps}
                style={{ flex: logsVisible ? detailLogsRatio : 1 }}
              >
                {detailTask ? (
                  <DetailBody task={detailTask} onEditMeta={() => setEditMeta(detailTask)} onEditContent={() => setEditContent(detailTask)} />
                ) : (
                  <span className="faint">No task selected.</span>
                )}
              </Panel>
            )}
            {detailVisible && logsVisible && (
              <PaneSplitterColumn containerRef={bottomRowRef} />
            )}
            {logsVisible && (
              <Panel
                title="Logs"
                meta={detailTask ? detailTask.id : undefined}
                {...paneNav.logsPaneProps}
                style={{ flex: detailVisible ? 1 - detailLogsRatio : 1 }}
              >
                <TaskSessionPane
                  taskId={detailTask?.id}
                  projectId={detailTask?.projectId}
                  taskPath={detailTask?.path}
                />
              </Panel>
            )}
          </div>
        </>
      )}

      {editMeta && <MetadataModal task={editMeta} onClose={() => setEditMeta(null)} />}
      {viewContent && (
        <Suspense fallback={null}>
          <EntryRawViewModal
            path={viewContent.path}
            title={viewContent.title || viewContent.id}
            onClose={() => setViewContent(null)}
          />
        </Suspense>
      )}
      {batchMeta && <BatchMetadataModal tasks={batchMeta} onClose={() => setBatchMeta(null)} onDone={() => nav.clearSelect()} />}
      {featureMeta && (
        <FeatureMetadataModal
          feature={featureMeta.feature}
          project={activeProject}
          tasks={featureMeta.tasks}
          onClose={() => setFeatureMeta(null)}
          onDone={() => nav.clearSelect()}
        />
      )}
      {editContent && (
        <Suspense fallback={null}>
          <EntryEditModal
            path={editContent.path}
            title={editContent.title || editContent.id}
            onClose={() => setEditContent(null)}
          />
        </Suspense>
      )}
      {confirmDel && (
        <ConfirmDialog
          title={confirmDel.length > 1 ? `Delete ${confirmDel.length} tasks?` : "Delete task?"}
          danger
          confirmLabel="Delete"
          busy={busy}
          message={confirmDel.length > 1 ? <>This permanently deletes {confirmDel.length} tasks.</> : <>This permanently deletes <strong>{confirmDel[0].title}</strong>.</>}
          onClose={() => setConfirmDel(null)}
          onConfirm={() => void doDelete(confirmDel)}
        />
      )}
      {composing && (
        <Suspense fallback={null}>
          <ComposeModal kind="task" onClose={() => setComposing(false)} />
        </Suspense>
      )}
    </>
  );
}

function DetailBody({
  task,
  onEditMeta,
  onEditContent,
}: {
  task: Task;
  onEditMeta: () => void;
  onEditContent: () => void;
}) {
  const desc = task.content || task.direct_prompt || task.user_original_request || "";
  return (
    <div>
      <div style={{ fontWeight: 700, marginBottom: 4 }}>
        {task.in_cycle && <span style={{ color: "var(--red)" }}>↺ </span>}
        {task.title || task.id}
      </div>
      <Field l="Status" v={task.status} color={statusColor(task.status)} />
      <Field l="Priority" v={task.priority} />
      <Field l="ID" v={task.id} />
      {task.feature_id && <Field l="Feature" v={task.feature_id} />}
      {task.projectId && <Field l="Project" v={task.projectId} />}
      {task.agent && <Field l="Agent" v={task.agent} />}
      {task.model && <Field l="Model" v={task.model} />}
      {task.executor && <Field l="Executor" v={task.executor} />}
      {task.execution_mode && <Field l="Execution" v={task.execution_mode} />}
      {task.workdir && <Field l="Workdir" v={task.workdir} />}
      {task.git_branch && <Field l="Branch" v={task.git_branch} />}
      {task.merge_policy && <Field l="Merge" v={task.merge_policy} />}
      {task.schedule && <Field l="Schedule" v={task.schedule} />}
      {task.next_run && <Field l="Next run" v={relativeTime(task.next_run)} />}
      {task.created && <Field l="Created" v={relativeTime(task.created)} />}
      {task.tags && task.tags.length > 0 && <Field l="Tags" v={task.tags.join(", ")} />}

      {task.depends_on && task.depends_on.length > 0 && (
        <Field l="Depends on" v={task.depends_on.join(", ")} />
      )}
      {(task.waiting_on?.length ?? 0) > 0 && (
        <Field l="Waiting on" v={task.waiting_on!.join(", ")} color="var(--yellow)" />
      )}
      {(task.blocked_by?.length ?? 0) > 0 && (
        <Field l="Blocked by" v={task.blocked_by!.join(", ")} color="var(--red)" />
      )}
      {task.blocked_by_reason && (
        <Field l="Reason" v={task.blocked_by_reason} color="var(--red)" />
      )}

      {task.sessions && Object.keys(task.sessions).length > 0 && (
        <>
          <div className="detail-section">
            Sessions ({Object.keys(task.sessions).length})
          </div>
          {Object.entries(task.sessions)
            .sort((a, b) => (b[1]?.timestamp || "").localeCompare(a[1]?.timestamp || ""))
            .slice(0, 6)
            .map(([sid, info]) => (
              <div className="detail-field" key={sid}>
                <span className="dv" style={{ color: "var(--cyan)" }}>{sid}</span>
                <span className="dl">{relativeTime(info?.timestamp)}</span>
              </div>
            ))}
        </>
      )}

      {task.in_cycle && (
        <div style={{ color: "var(--red)", marginTop: 6 }}>
          ↺ part of a dependency cycle
        </div>
      )}

      {desc && (
        <>
          <div className="detail-section">Description</div>
          <div style={{ whiteSpace: "pre-wrap", marginTop: 2 }}>{desc}</div>
        </>
      )}
      <div className="btn-row" style={{ marginTop: 8 }}>
        <button className="btn sm" onClick={onEditMeta}>s · metadata</button>
        <button className="btn sm" onClick={onEditContent}>e · edit</button>
      </div>
    </div>
  );
}

function Field({ l, v, color }: { l: string; v?: string; color?: string }) {
  if (!v) return null;
  return (
    <div className="detail-field">
      <span className="dl">{l}</span>
      <span className="dv" style={color ? { color } : undefined}>{v}</span>
    </div>
  );
}
