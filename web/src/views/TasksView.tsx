import { lazy, Suspense, useEffect, useMemo, useRef, useState } from "react";
import { useUI, ALL_PROJECTS } from "../store/ui";
import { useNav } from "../store/nav";
import { useViewKeyboard, handleListNavKey } from "../lib/keyboard";
import { useLiveTasks } from "../hooks/useLiveTasks";
import { filterTasks, groupByFeature, UNGROUPED } from "./tasks/grouping";
import { TaskCard } from "./tasks/TaskCard";
import { TaskDetail } from "./tasks/TaskDetail";
import { MetadataModal } from "./tasks/MetadataModal";
import { BatchMetadataModal } from "./tasks/BatchMetadataModal";
import { EntryView } from "./brain/EntryView";
import { ConfirmDialog } from "../components/common/Modal";
import { EmptyState } from "../components/common/states";
import {
  deleteEntry,
  setTaskStatus,
  triggerTask,
  updateEntry,
} from "../lib/api";
import { isActive, relativeTime } from "../lib/format";
import { StatusBadge } from "../components/common/Badge";
import type { Task } from "../lib/types";

const ComposeModal = lazy(() =>
  import("../components/compose/ComposeModal").then((m) => ({
    default: m.ComposeModal,
  })),
);

const TERMINAL = ["completed", "cancelled", "archived", "superseded"];
const taskKey = (t: Task) => `${t.projectId}:${t.id}`;

type Row =
  | { kind: "header"; feature: string; label: string; count: number }
  | { kind: "task"; task: Task };

export function TasksView() {
  const activeProject = useUI((s) => s.activeProject);
  const wrap = useUI((s) => s.wrap);
  const toast = useUI((s) => s.toast);
  const { tasks, connected } = useLiveTasks(activeProject);

  const nav = useNav();
  const scope = `tasks:${activeProject}`;
  const cursor = useNav((s) => s.cursor[scope] ?? 0);
  const selected = useNav((s) => s.selected);

  const [mode, setMode] = useState<"tasks" | "schedules">("tasks");
  const [query, setQuery] = useState("");
  const [showDone, setShowDone] = useState(false);
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});

  const [detail, setDetail] = useState<Task | null>(null);
  const [editMeta, setEditMeta] = useState<Task | null>(null);
  const [editContent, setEditContent] = useState<Task | null>(null);
  const [batchMeta, setBatchMeta] = useState<Task[] | null>(null);
  const [confirmDel, setConfirmDel] = useState<Task[] | null>(null);
  const [composing, setComposing] = useState(false);
  const [busy, setBusy] = useState(false);

  const filterRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  // ── Derive visible rows ──────────────────────────────────────
  const { rows, taskList, scheduled } = useMemo(() => {
    let list = filterTasks(tasks, query);
    if (mode === "schedules") {
      const sched = list.filter((t) => t.schedule || t.run_once_at);
      return {
        rows: sched.map((t) => ({ kind: "task" as const, task: t })),
        taskList: sched,
        scheduled: true,
      };
    }
    if (!showDone) list = list.filter((t) => !TERMINAL.includes(t.status));
    const groups = groupByFeature(list);
    const r: Row[] = [];
    const flat: Task[] = [];
    for (const g of groups) {
      const showHeader = g.feature !== UNGROUPED || groups.length > 1;
      if (showHeader)
        r.push({ kind: "header", feature: g.feature, label: g.label, count: g.tasks.length });
      if (!collapsed[g.feature]) {
        for (const t of g.tasks) {
          r.push({ kind: "task", task: t });
          flat.push(t);
        }
      }
    }
    return { rows: r, taskList: flat, scheduled: false };
  }, [tasks, query, showDone, collapsed, mode]);

  // Keep cursor in range.
  useEffect(() => {
    if (cursor > rows.length - 1) nav.setCursor(scope, Math.max(0, rows.length - 1));
  }, [rows.length]); // eslint-disable-line react-hooks/exhaustive-deps

  // Scroll the cursored row into view.
  useEffect(() => {
    const el = listRef.current?.querySelector<HTMLElement>('[data-cursor="1"]');
    el?.scrollIntoView({ block: "nearest" });
  }, [cursor, rows.length]);

  const selectedTasks = useMemo(
    () => taskList.filter((t) => selected[taskKey(t)]),
    [taskList, selected],
  );

  // ── Actions ──────────────────────────────────────────────────
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

  function targets(cur?: Task): Task[] {
    if (selectedTasks.length) return selectedTasks;
    return cur ? [cur] : [];
  }

  async function complete(cur?: Task) {
    const ts = targets(cur);
    if (!ts.length) return;
    await run(`Completed ${ts.length}`, () =>
      Promise.all(ts.map((t) => setTaskStatus(t, "completed"))),
    );
    nav.clearSelect();
  }

  async function doDelete(ts: Task[]) {
    await run(`Deleted ${ts.length}`, () =>
      Promise.all(ts.map((t) => deleteEntry(t.path))),
    );
    nav.clearSelect();
    setConfirmDel(null);
  }

  async function executeFeature(feature: string) {
    const ready = taskList.filter(
      (t) => (t.feature_id || UNGROUPED) === feature && ["pending", "active"].includes(t.status),
    );
    if (!ready.length) {
      toast("No ready tasks in feature", "info");
      return;
    }
    await run(`Triggered ${ready.length}`, () =>
      Promise.all(ready.map((t) => triggerTask(t.projectId!, t.id))),
    );
  }

  function toggleScheduleEnabled(t: Task) {
    void run(t.schedule_enabled ? "Schedule disabled" : "Schedule enabled", () =>
      updateEntry(t.path, { schedule_enabled: !t.schedule_enabled }),
    );
  }

  // ── Keyboard ─────────────────────────────────────────────────
  useViewKeyboard(
    (e) => {
      if (handleListNavKey(e, scope, rows.length)) return true;
      const row = rows[cursor];
      const cur = row?.kind === "task" ? row.task : undefined;
      switch (e.key) {
        case "Enter":
          if (row?.kind === "header")
            setCollapsed((c) => ({ ...c, [row.feature]: !c[row.feature] }));
          else if (cur) setDetail(cur);
          return true;
        case " ":
          if (row?.kind === "header")
            setCollapsed((c) => ({ ...c, [row.feature]: !c[row.feature] }));
          else if (cur) nav.toggleSelect(taskKey(cur));
          return true;
        case "A":
          nav.selectMany(taskList.map(taskKey));
          return true;
        case "D":
          nav.clearSelect();
          return true;
        case "c":
          void complete(cur);
          return true;
        case "x":
          if (row?.kind === "header") void executeFeature(row.feature);
          else if (cur && cur.projectId) void run("Triggered", () => triggerTask(cur.projectId!, cur.id));
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
        case "e":
          if (cur) setEditContent(cur);
          return true;
        case "y":
          if (cur) {
            void navigator.clipboard?.writeText(cur.title || cur.id);
            toast("Copied title");
          }
          return true;
        case "/":
          filterRef.current?.focus();
          return true;
        case "C":
          setMode((m) => (m === "tasks" ? "schedules" : "tasks"));
          nav.setCursor(scope, 0);
          return true;
        case "n":
          setComposing(true);
          return true;
        default:
          return false;
      }
    },
    [rows, cursor, scope, taskList, selectedTasks, scheduled],
  );

  const showProject = activeProject === ALL_PROJECTS;
  const selCount = selectedTasks.length;

  return (
    <div>
      <div className="search-bar">
        <input
          ref={filterRef}
          placeholder="Filter tasks…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              setQuery("");
              e.currentTarget.blur();
            }
          }}
        />
        <button
          className={`btn sm ${mode === "schedules" ? "primary" : ""}`}
          onClick={() => setMode((m) => (m === "tasks" ? "schedules" : "tasks"))}
          title="Tasks ⇄ Schedules (C)"
        >
          {mode === "schedules" ? "Sched" : "Tasks"}
        </button>
        {mode === "tasks" && (
          <button
            className={`btn sm ${showDone ? "primary" : ""}`}
            onClick={() => setShowDone((v) => !v)}
            title="Toggle completed/cancelled"
          >
            {showDone ? "All" : "Active"}
          </button>
        )}
        <button className="btn sm primary" onClick={() => setComposing(true)} title="New task (n)">
          +
        </button>
      </div>

      {selCount > 0 && (
        <div className="selbar">
          <strong>{selCount} selected</strong>
          <div style={{ flex: 1 }} />
          <button className="btn sm" disabled={busy} onClick={() => void complete()}>
            ✓ Complete
          </button>
          <button className="btn sm" onClick={() => setBatchMeta(selectedTasks)}>
            ✎ Edit
          </button>
          <button className="btn sm danger" onClick={() => setConfirmDel(selectedTasks)}>
            Delete
          </button>
          <button className="btn sm ghost" onClick={() => nav.clearSelect()}>
            Clear
          </button>
        </div>
      )}

      {rows.length === 0 ? (
        tasks.length === 0 && !connected ? (
          <EmptyState glyph="⧗" title="Waiting for live data…" hint="Connecting to the task stream." />
        ) : (
          <EmptyState
            glyph={scheduled ? "⏰" : "✓"}
            title={
              query
                ? "No matches"
                : scheduled
                  ? "No scheduled tasks"
                  : "No active tasks"
            }
            hint={scheduled ? "Tasks with a cron schedule appear here." : "Press n to create one."}
          />
        )
      ) : (
        <div className="section-pad" ref={listRef}>
          {rows.map((row, i) => {
            const isCur = i === cursor;
            if (row.kind === "header") {
              return (
                <button
                  key={`h:${row.feature}`}
                  data-cursor={isCur ? "1" : undefined}
                  className={isCur ? "kbd-cursor" : ""}
                  onClick={() =>
                    setCollapsed((c) => ({ ...c, [row.feature]: !c[row.feature] }))
                  }
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: "0.4rem",
                    width: "100%",
                    background: "none",
                    border: "none",
                    padding: "0.35rem 0.3rem",
                    color: "var(--fg-dim)",
                    fontWeight: 600,
                    fontSize: 13,
                    borderRadius: 6,
                  }}
                >
                  <span style={{ color: "var(--purple)" }}>
                    {collapsed[row.feature] ? "▸" : "▾"}
                  </span>
                  {row.label}
                  <span className="faint">({row.count})</span>
                </button>
              );
            }
            const t = row.task;
            if (scheduled) {
              return (
                <ScheduleRow
                  key={taskKey(t)}
                  task={t}
                  cursored={isCur}
                  onToggle={() => toggleScheduleEnabled(t)}
                  onOpen={() => setDetail(t)}
                />
              );
            }
            return (
              <TaskCard
                key={taskKey(t)}
                task={t}
                showProject={showProject}
                wrap={wrap}
                cursored={isCur}
                selected={!!selected[taskKey(t)]}
                showCheck={selCount > 0}
                onClick={() => setDetail(t)}
                onToggleSelect={() => nav.toggleSelect(taskKey(t))}
              />
            );
          })}
        </div>
      )}

      {detail && <TaskDetail task={detail} onClose={() => setDetail(null)} />}
      {editMeta && <MetadataModal task={editMeta} onClose={() => setEditMeta(null)} />}
      {batchMeta && (
        <BatchMetadataModal
          tasks={batchMeta}
          onClose={() => setBatchMeta(null)}
          onDone={() => nav.clearSelect()}
        />
      )}
      {editContent && (
        <EntryView path={editContent.path} onClose={() => setEditContent(null)} />
      )}
      {confirmDel && (
        <ConfirmDialog
          title={confirmDel.length > 1 ? `Delete ${confirmDel.length} tasks?` : "Delete task?"}
          danger
          confirmLabel="Delete"
          busy={busy}
          message={
            confirmDel.length > 1 ? (
              <>This permanently deletes {confirmDel.length} tasks.</>
            ) : (
              <>
                This permanently deletes <strong>{confirmDel[0].title}</strong>.
              </>
            )
          }
          onClose={() => setConfirmDel(null)}
          onConfirm={() => void doDelete(confirmDel)}
        />
      )}
      {composing && (
        <Suspense fallback={null}>
          <ComposeModal kind="task" onClose={() => setComposing(false)} />
        </Suspense>
      )}
    </div>
  );
}

function ScheduleRow({
  task,
  cursored,
  onToggle,
  onOpen,
}: {
  task: Task;
  cursored: boolean;
  onToggle: () => void;
  onOpen: () => void;
}) {
  const enabled = task.schedule_enabled !== false;
  return (
    <div
      data-cursor={cursored ? "1" : undefined}
      className={cursored ? "kbd-cursor" : ""}
      style={{
        display: "flex",
        alignItems: "center",
        gap: "0.6rem",
        background: "var(--bg-1)",
        border: "1px solid var(--border)",
        borderRadius: "var(--radius-sm)",
        padding: "0.6rem 0.7rem",
        marginBottom: "0.4rem",
      }}
    >
      <button
        className={`sel-check ${enabled ? "on" : ""}`}
        onClick={onToggle}
        title="Toggle schedule"
      >
        {enabled ? "⏰" : ""}
      </button>
      <button
        onClick={onOpen}
        style={{ flex: 1, minWidth: 0, textAlign: "left", background: "none", border: "none", color: "inherit" }}
      >
        <div style={{ fontWeight: 500 }}>{task.title || task.id}</div>
        <div className="row wrap" style={{ gap: "0.35rem", marginTop: "0.3rem" }}>
          <span className="pill mono" style={{ color: "var(--cyan)" }}>
            {task.schedule || (task.run_once_at ? "once" : "—")}
          </span>
          {task.next_run && (
            <span className="pill faint">next {relativeTime(task.next_run)}</span>
          )}
          <StatusBadge status={task.status} />
        </div>
      </button>
    </div>
  );
}
