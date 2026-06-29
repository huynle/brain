import { lazy, Suspense, useEffect, useMemo, useRef, useState } from "react";
import { useUI, ALL_PROJECTS } from "../store/ui";
import { useNav } from "../store/nav";
import { useLive } from "../lib/sse";
import { useViewKeyboard, handleListNavKey } from "../lib/keyboard";
import { usePaneNavigation } from "../lib/usePaneNavigation";
import { useIsMobile } from "../hooks/useIsMobile";
import { useLiveTasks } from "../hooks/useLiveTasks";
import { filterTasks, groupByFeature, UNGROUPED, type FeatureSortMode } from "./tasks/grouping";
import { buildTaskTree } from "./tasks/tree";
import { MetadataModal } from "./tasks/MetadataModal";
import { BatchMetadataModal } from "./tasks/BatchMetadataModal";
import { FeatureMetadataModal } from "./tasks/FeatureMetadataModal";
import { Panel } from "../components/layout/Panel";
import { PaneSplitterRow, PaneSplitterColumn } from "../components/layout/PaneSplitters";
import { ConfirmDialog } from "../components/common/Modal";
import { deleteEntry, listInstances, runOrTriggerTask, setTaskStatus, summarizeTriggerResults } from "../lib/api";
import {
  cleanLogContent,
  clockTime,
  isActive,
  logLevelColor,
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

  const { tasks, connected } = useLiveTasks(activeProject);
  const logs = useLive((s) => s.logs);

  const nav = useNav();
  const scope = `tasks:${activeProject}`;
  const cursor = useNav((s) => s.cursor[scope] ?? 0);
  const selected = useNav((s) => s.selected);

  const [mode, setMode] = useState<"tasks" | "schedules">("tasks");
  const [query, setQuery] = useState("");
  const [searchOpen, setSearchOpen] = useState(false);
  const [showDone, setShowDone] = useState(false);
  const [featureSort, setFeatureSort] = useState<FeatureSortMode>("completed");
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
  const logBodyRef = paneNav.logsPaneProps.bodyRef;

  const { rows, taskList, featureKeys, tasksByFeature } = useMemo(() => {
    let list = filterTasks(tasks, query);
    if (mode === "schedules") list = list.filter((t) => t.schedule || t.run_once_at);
    else if (!showDone) list = list.filter((t) => !TERMINAL.includes(t.status));
    const groups = groupByFeature(list, activeProject === ALL_PROJECTS ? featureSort : "name");
    const r: Row[] = [];
    const flat: Task[] = [];
    const keys: string[] = [];
    const byFeature = new Map<string, Task[]>();
    for (const g of groups) {
      byFeature.set(g.feature, g.tasks);
      const showHeader = g.feature !== UNGROUPED || groups.length > 1;
      if (showHeader) {
        keys.push(g.feature);
        r.push({ kind: "header", feature: g.feature, label: g.label, count: g.tasks.length });
      }
      if (!(collapsed[g.feature] ?? collapseDefault)) {
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
  }, [tasks, query, showDone, collapsed, collapseDefault, mode, activeProject, featureSort]);

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

  const selectedTasks = useMemo(
    () => taskList.filter((t) => selected[taskKey(t)]),
    [taskList, selected],
  );
  const selCount = selectedTasks.length;

  // logs for the cursored task (or whole active project)
  const panelLogs = useMemo(() => {
    if (detailTask)
      return logs.filter((l) => l.taskId === detailTask.id).slice(-300);
    if (activeProject !== ALL_PROJECTS)
      return logs.filter((l) => l.projectId === activeProject).slice(-300);
    return logs.slice(-300);
  }, [logs, detailTask, activeProject]);

  useEffect(() => {
    if (logBodyRef.current)
      logBodyRef.current.scrollTop = logBodyRef.current.scrollHeight;
  }, [panelLogs.length]);

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

  useViewKeyboard(
    (e) => {
      // Tab/Shift-Tab + vim-style scroll inside detail/logs panes.
      if (paneNav.handleKey(e)) return true;

      // View-owned panel toggles. (paneNav handles Tab; T and z are still
      // view-scoped because they mutate visibility, not focus.)
      if (e.key === "T") {
        toggleDetail();
        return true;
      }
      if (e.key === "z") {
        toggleLogs();
        return true;
      }
      // When detail/logs are focused, paneNav already handled or rejected
      // j/k/g/G/Ctrl-D/Ctrl-U above. If we got here, the key is unhandled
      // for that focus — don't fall into list nav.
      if (focus === "detail" || focus === "logs") {
        return false;
      }
      // tasks focus
      if (handleListNavKey(e, scope, rows.length)) return true;
      const row = rows[cursor];
      const cur = row?.kind === "task" ? row.task : undefined;
      switch (e.key) {
        case "Enter":
          if (row?.kind === "header")
            setCollapsed((c) => ({ ...c, [row.feature]: !(c[row.feature] ?? collapseDefault) }));
          else if (cur) void openTaskSession(cur);
          return true;
        case " ":
          if (row?.kind === "header")
            setCollapsed((c) => ({ ...c, [row.feature]: !(c[row.feature] ?? collapseDefault) }));
          else if (cur) nav.toggleSelect(taskKey(cur));
          return true;
        case "{": setAllFeatureCollapsed(true); return true;
        case "}": setAllFeatureCollapsed(false); return true;
        case "A": nav.selectMany(taskList.map(taskKey)); return true;
        case "D": nav.clearSelect(); return true;
        case "c": {
          const ts = targets(cur);
          if (ts.length) void run(`Completed ${ts.length}`, () => Promise.all(ts.map((t) => setTaskStatus(t, "completed")))).then(() => nav.clearSelect());
          return true;
        }
        case "x":
          if (row?.kind === "header") {
            const ready = taskList.filter((t) => (t.feature_id || UNGROUPED) === row.feature && ["pending", "active"].includes(t.status));
            if (ready.length) void triggerMany(ready);
          } else if (cur?.projectId) void triggerMany([cur]);
          return true;
        case "X":
          if (cur && isActive(cur.status)) void run("Cancelled", () => setTaskStatus(cur, "cancelled"));
          return true;
        case "d":
        case "Backspace": {
          const ts = targets(cur);
          if (ts.length) setConfirmDel(ts);
          return true;
        }
        case "s": {
          if (row?.kind === "header") {
            const ts = tasksByFeature.get(row.feature) ?? [];
            if (row.feature === UNGROUPED) toast("Ungrouped tasks do not have feature settings", "info");
            else if (ts.length) setFeatureMeta({ feature: row.feature, tasks: ts });
            return true;
          }
          const ts = targets(cur);
          if (ts.length > 1) setBatchMeta(ts);
          else if (cur) setEditMeta(cur);
          return true;
        }
        case "e": if (cur) setEditContent(cur); return true;
        case "y":
          if (cur) { void navigator.clipboard?.writeText(cur.title || cur.id); toast("Copied title"); }
          return true;
        case "/": setSearchOpen(true); return true;
        case "C": setMode((m) => (m === "tasks" ? "schedules" : "tasks")); nav.setCursor(scope, 0); return true;
        case "n": setComposing(true); return true;
        default: return false;
      }
    },
    [rows, cursor, scope, taskList, selectedTasks, focus, collapseDefault, featureKeys, tasksByFeature, openInControl, toast],
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
          <button className="btn sm" onClick={() => setMode((m) => (m === "tasks" ? "schedules" : "tasks"))}>
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
                  title={row.feature === UNGROUPED ? "Enter/Space toggles collapse" : "s opens feature settings · Enter/Space toggles collapse"}
                >
                  <span className="htri">{(collapsed[row.feature] ?? collapseDefault) ? "▸" : "▾"}</span>
                  {row.label}
                  <span className="hcount">({row.count})</span>
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
                {panelLogs.length === 0 ? (
                  <span className="faint">No logs yet.</span>
                ) : (
                  panelLogs.map((r) => (
                    <div key={r.seq} className="logline">
                      <span className="lt">{clockTime(r.line.timestamp)}</span>
                      <span className="ll" style={{ color: logLevelColor(r.line.level) }}>{r.line.level}</span>
                      <span className="lc">{cleanLogContent(r.line.content)}</span>
                    </div>
                  ))
                )}
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
