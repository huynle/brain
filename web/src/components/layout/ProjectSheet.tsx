import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useProjects } from "../../hooks/useProjects";
import { useIsMobile } from "../../hooks/useIsMobile";
import { ALL_PROJECTS, useUI } from "../../store/ui";
import {
  getRunnerStatus,
  pauseAll,
  pauseAutomations,
  pauseProject,
  resumeAll,
  resumeAutomations,
  resumeProject,
  runOrTriggerTask,
  summarizeTriggerResults,
} from "../../lib/api";
import { fuzzyScore } from "../../lib/fuzzy";
import { useLive, type ProjectLive } from "../../lib/sse";
import type { Task } from "../../lib/types";
import { BottomSheet } from "./BottomSheet";
import { Modal } from "../common/Modal";

function shortName(id: string): string {
  if (id === ALL_PROJECTS) return "All projects";
  return id.split(/[/\\]/).pop() || id;
}

const FEATURELESS = "__ungrouped__";

type ProjectRow = { kind: "project"; project: string };
type FeatureRow = {
  kind: "feature";
  project: string;
  feature: string;
  label: string;
  ready: Task[];
  total: number;
};
type PickerRow = ProjectRow | FeatureRow;
type RunnerStatus = Awaited<ReturnType<typeof getRunnerStatus>>;

function rowKey(row: PickerRow): string {
  return row.kind === "project"
    ? `p:${row.project}`
    : `f:${row.project}:${row.feature}`;
}

function isRunnable(t: Task): boolean {
  return t.status === "pending" || t.status === "active";
}

function isTaskPaused(project: string, status?: RunnerStatus): boolean {
  if (!status) return false;
  return project === ALL_PROJECTS
    ? status.paused
    : !!status.pausedProjects?.includes(project);
}

function areAutomationsPaused(project: string, status?: RunnerStatus): boolean {
  if (!status) return false;
  return project === ALL_PROJECTS
    ? status.automationsPaused
    : !!status.automationPausedProjects?.includes(project);
}

function countInProgress(
  project: string,
  liveProjects: Record<string, ProjectLive>,
): number {
  const collect = (tasks: Task[]) =>
    tasks.reduce((n, t) => n + (t.status === "in_progress" ? 1 : 0), 0);
  if (project === ALL_PROJECTS) {
    return Object.values(liveProjects).reduce(
      (n, s) => n + collect(s.tasks),
      0,
    );
  }
  return collect(liveProjects[project]?.tasks ?? []);
}

// Searchable project picker. Opened by the status-bar project name or the
// Cmd/Ctrl+; shortcut. Type to fuzzy-filter; the closest match is highlighted,
// ↑/↓ moves the selection, Enter accepts, Esc closes. Bottom sheet on mobile,
// centered modal on desktop.
export function ProjectSheet() {
  const open = useUI((s) => s.projectSheetOpen);
  const setOpen = useUI((s) => s.setProjectSheetOpen);
  const active = useUI((s) => s.activeProject);
  const setActiveProject = useUI((s) => s.setActiveProject);
  const toast = useUI((s) => s.toast);
  const isMobile = useIsMobile();
  const projectsQ = useProjects();
  const statusQ = useQuery({
    queryKey: ["runner-status"],
    queryFn: getRunnerStatus,
    refetchInterval: open ? 8_000 : false,
    enabled: open,
  });
  const qc = useQueryClient();
  const liveProjects = useLive((s) => s.projects);
  const [q, setQ] = useState("");
  const [cursor, setCursor] = useState(0);
  const [selectionMode, setSelectionMode] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  const all = useMemo(
    () => [ALL_PROJECTS, ...(projectsQ.data ?? [])],
    [projectsQ.data],
  );

  const projectMatches = useMemo(() => {
    const needle = q.trim();
    if (!needle) return all;
    return all
      .map((p) => ({ p, s: fuzzyScore(shortName(p), needle) }))
      .filter((x): x is { p: string; s: number } => x.s !== null)
      .sort((a, b) => b.s - a.s)
      .map((x) => x.p);
  }, [all, q]);

  const rows = useMemo<PickerRow[]>(() => {
    const out: PickerRow[] = [];
    for (const project of projectMatches) {
      out.push({ kind: "project", project });
      const tasks = project === ALL_PROJECTS
        ? Object.entries(liveProjects).flatMap(([projectId, state]) =>
            state.tasks.map((t) => (t.projectId ? t : { ...t, projectId })),
          )
        : (liveProjects[project]?.tasks ?? []).map((t) =>
            t.projectId ? t : { ...t, projectId: project },
          );

      const groups = new Map<string, Task[]>();
      for (const task of tasks) {
        if (!isRunnable(task)) continue;
        const feature = task.feature_id || FEATURELESS;
        const arr = groups.get(feature) ?? [];
        arr.push(task);
        groups.set(feature, arr);
      }

      for (const [feature, ready] of [...groups.entries()].sort(([a], [b]) => {
        if (a === FEATURELESS) return 1;
        if (b === FEATURELESS) return -1;
        return a.localeCompare(b);
      })) {
        out.push({
          kind: "feature",
          project,
          feature,
          label: feature === FEATURELESS ? "Ungrouped ready tasks" : feature,
          ready,
          total: ready.length,
        });
      }
    }
    return out;
  }, [liveProjects, projectMatches]);

  // Highlight the best match (top of the sorted list) whenever the query changes.
  useEffect(() => {
    setCursor(0);
    setSelectionMode(false);
  }, [q]);

  // The desktop modal and mobile bottom sheet mount their wrappers around this
  // input, so do not rely on browser autoFocus alone.
  useEffect(() => {
    if (open) inputRef.current?.focus({ preventScroll: true });
  }, [open]);

  // Refetch the project list whenever the picker opens so newly created
  // projects (e.g. from a fresh Brain entry created in another window) appear
  // immediately. Without this, the list is held by React Query for staleTime
  // and the user sees a stale snapshot.
  useEffect(() => {
    if (open) {
      qc.invalidateQueries({ queryKey: ["projects"] });
    }
  }, [open, qc]);

  // Keep the highlighted row scrolled into view.
  useEffect(() => {
    listRef.current
      ?.querySelector<HTMLElement>('[data-cursor="1"]')
      ?.scrollIntoView({ block: "nearest" });
  }, [cursor, rows.length]);

  if (!open) return null;

  function close() {
    setOpen(false);
    setQ("");
    setCursor(0);
    setSelectionMode(false);
  }
  function pick(p?: string) {
    if (!p) return;
    setActiveProject(p);
    close();
  }

  async function act(key: string, label: string, fn: () => Promise<unknown>) {
    setBusy(key);
    try {
      await fn();
      toast(label, "success");
      void qc.invalidateQueries({ queryKey: ["runner-status"] });
    } catch (e) {
      toast(e instanceof Error ? e.message : "Action failed", "error");
    } finally {
      setBusy(null);
    }
  }

  function toggleTasks(project: string) {
    const paused = isTaskPaused(project, statusQ.data);
    const fn = project === ALL_PROJECTS
      ? paused ? resumeAll : pauseAll
      : () => (paused ? resumeProject(project) : pauseProject(project));
    void act(
      `tasks:${project}`,
      paused ? `Resumed ${shortName(project)}` : `Paused ${shortName(project)}`,
      fn,
    );
  }

  function toggleAutomations(project: string) {
    const paused = areAutomationsPaused(project, statusQ.data);
    const scoped = project === ALL_PROJECTS ? undefined : project;
    void act(
      `autos:${project}`,
      paused
        ? `Resumed automations for ${shortName(project)}`
        : `Paused automations for ${shortName(project)}`,
      () => (paused ? resumeAutomations(scoped) : pauseAutomations(scoped)),
    );
  }

  function toggleAllTasksAndAutomations() {
    const tasksPaused = isTaskPaused(ALL_PROJECTS, statusQ.data);
    const autosPaused = areAutomationsPaused(ALL_PROJECTS, statusQ.data);
    const resume = tasksPaused && autosPaused;
    void act(
      "all:tasks-autos",
      resume ? "Resumed all tasks and automations" : "Paused all tasks and automations",
      async () => {
        if (resume) {
          await resumeAll();
          await resumeAutomations();
        } else {
          await pauseAll();
          await pauseAutomations();
        }
      },
    );
  }

  function toggleAllTasks() {
    toggleTasks(ALL_PROJECTS);
  }

  function toggleAllAutomations() {
    toggleAutomations(ALL_PROJECTS);
  }

  function triggerFeature(row: FeatureRow) {
    if (!row.ready.length) return;
    const key = `feature:${row.project}:${row.feature}`;
    setBusy(key);
    void (async () => {
      try {
        const results = await Promise.all(
          row.ready.map((t) => runOrTriggerTask(t.projectId || row.project, t.id)),
        );
        const { message, kind } = summarizeTriggerResults(results);
        toast(message, kind);
        void qc.invalidateQueries({ queryKey: ["runner-status"] });
      } catch (e) {
        toast(e instanceof Error ? e.message : "Run failed", "error");
      } finally {
        setBusy(null);
      }
    })();
  }

  function onKeyDown(e: React.KeyboardEvent) {
    if (selectionMode && (e.key === "j" || e.key === "ArrowDown")) {
      e.preventDefault();
      setCursor((c) => Math.min(c + 1, rows.length - 1));
    } else if (selectionMode && (e.key === "k" || e.key === "ArrowUp")) {
      e.preventDefault();
      setCursor((c) => Math.max(c - 1, 0));
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      setCursor((c) => Math.min(c + 1, rows.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setCursor((c) => Math.max(c - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (!selectionMode) {
        setSelectionMode(true);
        return;
      }
      const row = rows[cursor];
      if (row?.kind === "project") pick(row.project);
    } else if (e.key === "P") {
      e.preventDefault();
      toggleAllTasksAndAutomations();
    } else if (e.key === "T") {
      e.preventDefault();
      const row = rows[cursor];
      if (row) toggleTasks(row.project);
    } else if (e.key === "A") {
      e.preventDefault();
      const row = rows[cursor];
      if (row) toggleAutomations(row.project);
    } else if (e.key === "X") {
      e.preventDefault();
      const row = rows[cursor];
      if (row?.kind === "feature") triggerFeature(row);
    } else if (e.key === "Escape") {
      e.preventDefault();
      close();
    } else if (selectionMode && e.key.length === 1 && !e.metaKey && !e.ctrlKey && !e.altKey) {
      setSelectionMode(false);
    }
  }

  const allTasksPaused = isTaskPaused(ALL_PROJECTS, statusQ.data);
  const allAutomationsPaused = areAutomationsPaused(ALL_PROJECTS, statusQ.data);
  const everythingPaused = allTasksPaused && allAutomationsPaused;

  const body = (
    <div className="proj-picker">
      <div className="proj-global-controls" aria-label="All project runner controls">
        <button
          type="button"
          className="btn sm primary"
          disabled={busy === "all:tasks-autos"}
          onClick={toggleAllTasksAndAutomations}
          title="Shortcut: P"
        >
          {everythingPaused ? "resume all" : "pause all"}
        </button>
        <button
          type="button"
          className="btn sm"
          disabled={busy === `tasks:${ALL_PROJECTS}`}
          onClick={toggleAllTasks}
          title="Shortcut: T on All projects"
        >
          {allTasksPaused ? "resume all tasks" : "pause all tasks"}
        </button>
        <button
          type="button"
          className="btn sm"
          disabled={busy === `autos:${ALL_PROJECTS}`}
          onClick={toggleAllAutomations}
          title="Shortcut: A on All projects"
        >
          {allAutomationsPaused ? "resume all autos" : "pause all autos"}
        </button>
      </div>
      <input
        ref={inputRef}
        className="proj-search"
        autoFocus
        data-autofocus="true"
        placeholder="Search projects…"
        value={q}
        onChange={(e) => setQ(e.target.value)}
        onKeyDown={onKeyDown}
      />
      <div className="proj-list" ref={listRef}>
        {rows.map((row, i) => {
          const project = row.project;
          const taskPaused = isTaskPaused(project, statusQ.data);
          const automationPaused = areAutomationsPaused(project, statusQ.data);
          const inFlight = taskPaused ? countInProgress(project, liveProjects) : 0;
          const pausedRunning = taskPaused && inFlight > 0;
          const isProject = row.kind === "project";
          const isAllProjects = project === ALL_PROJECTS;
          const allPaused = taskPaused && automationPaused;
          const label = isProject ? shortName(project) : row.label;
          return (
            <div
              key={rowKey(row)}
              className={`proj-item ${isProject ? "" : "feature"} ${i === cursor ? "cursor" : ""} ${isProject && project === active ? "on" : ""}`}
              data-cursor={i === cursor ? "1" : undefined}
              onClick={() => (isProject ? pick(project) : triggerFeature(row))}
              onMouseMove={() => setCursor(i)}
              title={isProject ? "Enter switches project. T toggles tasks. A toggles automations." : "X or Trigger tasks starts this feature's ready tasks. T/A control its project."}
            >
              <span
                className="proj-dot"
                style={{
                  color: pausedRunning
                    ? "var(--yellow)"
                    : taskPaused
                      ? "var(--red)"
                      : "var(--green)",
                }}
                title={
                  pausedRunning
                    ? `tasks paused - ${inFlight} in flight`
                    : taskPaused
                      ? "tasks paused"
                      : "tasks running"
                }
              >
                {isProject && project === active ? "●" : "○"}
              </span>
              <span
                className="proj-dot"
                style={{ color: automationPaused ? "var(--orange)" : "var(--teal)" }}
                title={automationPaused ? "automations paused" : "automations running"}
              >
                {automationPaused ? "◌" : "●"}
              </span>
              <span className="truncate proj-label">
                {!isProject && <span className="faint">↳ </span>}
                {label}
              </span>
              {!isProject && <span className="proj-meta">{row.total} ready</span>}
              <span className="proj-actions" aria-label={`controls for ${label}`}>
                {isProject && isAllProjects && (
                  <button
                    type="button"
                    className="proj-action trigger"
                    disabled={busy === "all:tasks-autos"}
                    title={allPaused ? "Resume all tasks and automations" : "Pause all tasks and automations"}
                    onClick={(e) => { e.stopPropagation(); toggleAllTasksAndAutomations(); }}
                  >
                    {allPaused ? "resume all" : "pause all"}
                  </button>
                )}
                <button
                  type="button"
                  className="proj-action"
                  disabled={busy === `tasks:${project}`}
                  title={taskPaused ? "Resume project tasks" : "Pause project tasks"}
                  onClick={(e) => { e.stopPropagation(); toggleTasks(project); }}
                >
                  {taskPaused
                    ? isAllProjects ? "resume all tasks" : "resume tasks"
                    : isAllProjects ? "pause all tasks" : "pause tasks"}
                </button>
                <button
                  type="button"
                  className="proj-action"
                  disabled={busy === `autos:${project}`}
                  title={automationPaused ? "Resume project automations" : "Pause project automations"}
                  onClick={(e) => { e.stopPropagation(); toggleAutomations(project); }}
                >
                  {automationPaused
                    ? isAllProjects ? "resume all autos" : "resume autos"
                    : isAllProjects ? "pause all autos" : "pause autos"}
                </button>
                {!isProject && (
                  <button
                    type="button"
                    className="proj-action trigger"
                    disabled={busy === `feature:${project}:${row.feature}`}
                    onClick={(e) => { e.stopPropagation(); triggerFeature(row); }}
                  >
                    trigger tasks
                  </button>
                )}
              </span>
            </div>
          );
        })}
        {rows.length === 0 && (
          <div className="faint" style={{ padding: 10 }}>
            No match for “{q}”.
          </div>
        )}
        {projectsQ.isLoading && (
          <div className="faint" style={{ padding: 10 }}>
            Loading projects…
          </div>
        )}
      </div>
      <div className="proj-hints faint">Enter: navigate results / choose highlighted project · j/k: move after Enter · T: pause/resume highlighted tasks · A: pause/resume highlighted automations · P: pause/resume all tasks + automations · X: trigger highlighted feature tasks</div>
    </div>
  );

  return isMobile ? (
    <BottomSheet title="Project" onClose={close}>
      {body}
    </BottomSheet>
  ) : (
    <Modal title="Switch project" onClose={close}>
      {body}
    </Modal>
  );
}
