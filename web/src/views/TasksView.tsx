import { lazy, Suspense, useEffect, useMemo, useRef, useState } from "react";
import { useUI, ALL_PROJECTS } from "../store/ui";
import { useNav } from "../store/nav";
import { useLive } from "../lib/sse";
import { useViewKeyboard, handleListNavKey } from "../lib/keyboard";
import { useLiveTasks } from "../hooks/useLiveTasks";
import { filterTasks, groupByFeature, UNGROUPED } from "./tasks/grouping";
import { buildTaskTree } from "./tasks/tree";
import { MetadataModal } from "./tasks/MetadataModal";
import { BatchMetadataModal } from "./tasks/BatchMetadataModal";
import { EntryView } from "./brain/EntryView";
import { Panel } from "../components/layout/Panel";
import { ConfirmDialog } from "../components/common/Modal";
import { deleteEntry, setTaskStatus, triggerTask } from "../lib/api";
import {
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

const TERMINAL = ["completed", "cancelled", "archived", "superseded"];
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
  const setFocus = useUI((s) => s.setFocus);
  const cycleFocus = useUI((s) => s.cycleFocus);
  const detailVisible = useUI((s) => s.detailVisible);
  const logsVisible = useUI((s) => s.logsVisible);
  const toggleDetail = useUI((s) => s.toggleDetail);
  const toggleLogs = useUI((s) => s.toggleLogs);

  const { tasks, connected } = useLiveTasks(activeProject);
  const logs = useLive((s) => s.logs);

  const nav = useNav();
  const scope = `tasks:${activeProject}`;
  const cursor = useNav((s) => s.cursor[scope] ?? 0);
  const selected = useNav((s) => s.selected);

  const [mode, setMode] = useState<"tasks" | "schedules">("tasks");
  const [query, setQuery] = useState("");
  const [showDone, setShowDone] = useState(false);
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});

  const [editMeta, setEditMeta] = useState<Task | null>(null);
  const [editContent, setEditContent] = useState<Task | null>(null);
  const [batchMeta, setBatchMeta] = useState<Task[] | null>(null);
  const [confirmDel, setConfirmDel] = useState<Task[] | null>(null);
  const [composing, setComposing] = useState(false);
  const [busy, setBusy] = useState(false);

  const filterRef = useRef<HTMLInputElement>(null);
  const treeBodyRef = useRef<HTMLDivElement>(null);
  const detailBodyRef = useRef<HTMLDivElement>(null);
  const logBodyRef = useRef<HTMLDivElement>(null);

  const { rows, taskList } = useMemo(() => {
    let list = filterTasks(tasks, query);
    if (mode === "schedules") list = list.filter((t) => t.schedule || t.run_once_at);
    else if (!showDone) list = list.filter((t) => !TERMINAL.includes(t.status));
    const groups = groupByFeature(list);
    const r: Row[] = [];
    const flat: Task[] = [];
    for (const g of groups) {
      const showHeader = g.feature !== UNGROUPED || groups.length > 1;
      if (showHeader)
        r.push({ kind: "header", feature: g.feature, label: g.label, count: g.tasks.length });
      if (!collapsed[g.feature]) {
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
    return { rows: r, taskList: flat };
  }, [tasks, query, showDone, collapsed, mode]);

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

  useViewKeyboard(
    (e) => {
      // panel-level keys
      if (e.key === "Tab") {
        cycleFocus();
        return true;
      }
      if (e.key === "T") {
        toggleDetail();
        return true;
      }
      if (e.key === "z") {
        toggleLogs();
        return true;
      }
      // detail / logs focus → scroll those bodies
      if (focus === "detail" || focus === "logs") {
        const el = focus === "detail" ? detailBodyRef.current : logBodyRef.current;
        if (!el) return false;
        if (e.key === "j" || e.key === "ArrowDown") { el.scrollTop += 40; return true; }
        if (e.key === "k" || e.key === "ArrowUp") { el.scrollTop -= 40; return true; }
        if (e.key === "g") { el.scrollTop = 0; return true; }
        if (e.key === "G") { el.scrollTop = el.scrollHeight; return true; }
        return false;
      }
      // tasks focus
      if (handleListNavKey(e, scope, rows.length)) return true;
      const row = rows[cursor];
      const cur = row?.kind === "task" ? row.task : undefined;
      switch (e.key) {
        case "Enter":
          if (row?.kind === "header")
            setCollapsed((c) => ({ ...c, [row.feature]: !c[row.feature] }));
          else if (cur) { setFocus("detail"); }
          return true;
        case " ":
          if (row?.kind === "header")
            setCollapsed((c) => ({ ...c, [row.feature]: !c[row.feature] }));
          else if (cur) nav.toggleSelect(taskKey(cur));
          return true;
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
            if (ready.length) void run(`Triggered ${ready.length}`, () => Promise.all(ready.map((t) => triggerTask(t.projectId!, t.id))));
          } else if (cur?.projectId) void run("Triggered", () => triggerTask(cur.projectId!, cur.id));
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
          const ts = targets(cur);
          if (ts.length > 1) setBatchMeta(ts);
          else if (cur) setEditMeta(cur);
          return true;
        }
        case "e": if (cur) setEditContent(cur); return true;
        case "y":
          if (cur) { void navigator.clipboard?.writeText(cur.title || cur.id); toast("Copied title"); }
          return true;
        case "/": filterRef.current?.focus(); return true;
        case "C": setMode((m) => (m === "tasks" ? "schedules" : "tasks")); nav.setCursor(scope, 0); return true;
        case "n": setComposing(true); return true;
        default: return false;
      }
    },
    [rows, cursor, scope, taskList, selectedTasks, focus, detailVisible, logsVisible, mode],
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
        meta={`${taskList.length}`}
        focused={focus === "tasks"}
        onFocus={() => setFocus("tasks")}
        bodyRef={treeBodyRef}
        style={{ flex: 1 }}
      >
        <div className="search-bar" style={{ padding: "2px 0 4px", position: "static", background: "transparent" }}>
          <input
            ref={filterRef}
            placeholder="filter… (/)"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Escape") { setQuery(""); e.currentTarget.blur(); } }}
          />
          <button className="btn sm" onClick={() => setMode((m) => (m === "tasks" ? "schedules" : "tasks"))}>
            {mode === "schedules" ? "sched" : "tasks"}
          </button>
          {mode === "tasks" && (
            <button className={`btn sm ${showDone ? "primary" : ""}`} onClick={() => setShowDone((v) => !v)}>
              {showDone ? "all" : "active"}
            </button>
          )}
          <button className="btn sm primary" onClick={() => setComposing(true)}>+</button>
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
                  onClick={() => setCollapsed((c) => ({ ...c, [row.feature]: !c[row.feature] }))}
                >
                  <span className="htri">{collapsed[row.feature] ? "▸" : "▾"}</span>
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
                onClick={() => nav.setCursor(scope, i)}
                onDoubleClick={() => setFocus("detail")}
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
        <div className="tui-bottom" style={{ height: "34vh" }}>
          {detailVisible && (
            <Panel
              title="Detail"
              focused={focus === "detail"}
              onFocus={() => setFocus("detail")}
              bodyRef={detailBodyRef}
              style={{ flex: 1 }}
            >
              {detailTask ? (
                <DetailBody task={detailTask} onEditMeta={() => setEditMeta(detailTask)} onEditContent={() => setEditContent(detailTask)} />
              ) : (
                <span className="faint">No task selected.</span>
              )}
            </Panel>
          )}
          {logsVisible && (
            <Panel
              title="Logs"
              meta={detailTask ? detailTask.id : undefined}
              focused={focus === "logs"}
              onFocus={() => setFocus("logs")}
              bodyRef={logBodyRef}
              style={{ flex: 1 }}
            >
              {panelLogs.length === 0 ? (
                <span className="faint">No logs yet.</span>
              ) : (
                panelLogs.map((r) => (
                  <div key={r.seq} className="logline">
                    <span className="lt">{clockTime(r.line.timestamp)}</span>
                    <span className="ll" style={{ color: logLevelColor(r.line.level) }}>{r.line.level}</span>
                    <span className="lc">{r.line.content}</span>
                  </div>
                ))
              )}
            </Panel>
          )}
        </div>
      )}

      {editMeta && <MetadataModal task={editMeta} onClose={() => setEditMeta(null)} />}
      {batchMeta && <BatchMetadataModal tasks={batchMeta} onClose={() => setBatchMeta(null)} onDone={() => nav.clearSelect()} />}
      {editContent && <EntryView path={editContent.path} onClose={() => setEditContent(null)} />}
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
      {task.workdir && <Field l="Workdir" v={task.workdir} />}
      {task.git_branch && <Field l="Branch" v={task.git_branch} />}
      {task.schedule && <Field l="Schedule" v={task.schedule} />}
      {task.created && <Field l="Created" v={relativeTime(task.created)} />}
      {task.tags && task.tags.length > 0 && <Field l="Tags" v={task.tags.join(", ")} />}
      {task.depends_on && task.depends_on.length > 0 && (
        <Field l="Depends on" v={task.depends_on.join(", ")} />
      )}
      {(task.blocked_by?.length ?? 0) > 0 && (
        <Field l="Blocked by" v={task.blocked_by!.join(", ")} color="var(--red)" />
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
