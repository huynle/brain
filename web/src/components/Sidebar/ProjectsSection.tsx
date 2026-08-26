/**
 * Sidebar — Projects section (wireframe-parity).
 *
 * DOM:
 *   .sb-section
 *     .sb-head (▾ Projects · N/M · ＋)
 *     .sb-list
 *       .proj-row × N (dot + name + pause tags + stats + × close)
 *       ▸ Hidden (N)   (collapsed by default)
 *         .proj-row × N hidden — click to restore
 */
import { useState } from "react";
import { useWorkspace } from "../../store/workspace";
import { useProjects } from "../../hooks/useProjects";
import { usePauseState } from "../../hooks/usePauseState";
import { useRowActions } from "../../hooks/useRowActions";
import { useLive } from "../../lib/sse";
import { useUI } from "../../store/ui";
import { runProject, summarizeRunProjectResult } from "../../lib/api";
import { buildProjectActions } from "../../lib/actions/projectActions";
import { Loading } from "../common/Loading";
import { ErrorState } from "../common/ErrorState";
import { projectMatchesStatusFilter } from "../../lib/statusFilter";
import {
  forceDispatchNote,
  projectPauseBadges,
  withForceNote,
} from "../../lib/pause";
import type { Task } from "../../lib/types";

type Dot = "on" | "busy" | "paused" | "err" | "";

// A paused project outranks its task mix: with the task dial off, "some
// tasks are pending" and "some tasks are blocked" both reduce to "nothing
// is going to happen here", and a green dot said the opposite. Tasks still
// in flight keep the busy dot — pause stops new dispatches, it does not
// stop a process that is already running.
function projectDot(tasks: readonly Task[], paused: boolean): Dot {
  let hasBusy = false;
  let hasBlocked = false;
  for (const t of tasks) {
    if (t.status === "in_progress") hasBusy = true;
    else if (t.status === "blocked") hasBlocked = true;
  }
  if (hasBusy) return "busy";
  if (paused) return "paused";
  if (tasks.length === 0) return "";
  if (hasBlocked) return "err";
  return "on";
}

function focusProjectCard(projectId: string) {
  if (typeof document === "undefined") return;
  const el = document.querySelector<HTMLElement>(
    `.pcard[data-project="${CSS.escape(projectId)}"]`,
  );
  if (!el) return;
  el.scrollIntoView({ behavior: "smooth", block: "center" });
  el.classList.add("focused");
  window.setTimeout(() => el.classList.remove("focused"), 800);
}

export function ProjectsSection(): JSX.Element {
  const expanded = useWorkspace((s) => s.sidebarSection.projects);
  const toggle = useWorkspace((s) => s.toggleSidebarSection);
  const setView = useWorkspace((s) => s.setView);
  const hiddenProjects = useWorkspace((s) => s.hiddenProjects);
  const showProject = useWorkspace((s) => s.showProject);
  const hideProject = useWorkspace((s) => s.hideProject);
  const openInFocus = useWorkspace((s) => s.openInFocus);
  const toast = useUI((s) => s.toast);
  const { data: projects, isLoading, error, refetch } = useProjects();
  const { pause } = usePauseState();
  const liveProjects = useLive((s) => s.projects);
  const statusFilter = useWorkspace((s) => s.statusFilter);
  const [hiddenExpanded, setHiddenExpanded] = useState(false);
  const { rowProps, overlays } = useRowActions();

  const hiddenSet = new Set(hiddenProjects);
  // "Visible" means (a) not user-hidden AND (b) matches the current
  // status-chip filter. When statusFilter === "all", filterMatches every
  // project so this reduces to the pre-filter behavior.
  const filterMatch = (pid: string) =>
    projectMatchesStatusFilter(liveProjects[pid]?.tasks ?? [], statusFilter);
  const visibleProjectIds = (projects ?? []).filter(
    (p) => !hiddenSet.has(p) && filterMatch(p),
  );
  const hiddenProjectIds = (projects ?? []).filter((p) => hiddenSet.has(p));

  const rows = (() => {
    if (isLoading) return <Loading size="sm" label="Loading…" />;
    if (error)
      return <ErrorState error={error} onRetry={() => void refetch()} />;
    if (!projects || projects.length === 0) {
      return (
        <div style={{ padding: "6px 10px", color: "#6b757e", fontSize: 11 }}>
          No projects yet.
        </div>
      );
    }
    return (
      <>
        {visibleProjectIds.map((pid) => {
          const live = liveProjects[pid];
          const tasks = live?.tasks ?? [];
          const badges = projectPauseBadges(pause, pid);
          const dot = projectDot(tasks, badges.tasks);
          const active = tasks.filter(
            (t) => t.status === "in_progress",
          ).length;
          const ready = tasks.filter((t) => t.status === "pending").length;
          const blocked = tasks.filter((t) => t.status === "blocked").length;
          // Right-click used to hide the project outright — a menu-less
          // shortcut nobody could predict. Now it opens the same verb
          // set as the project card header (run / open in focus / hide),
          // with the × button as the one-click hide it always was.
          const actions = buildProjectActions(
            pid,
            {
              runProject: async (p) => {
                const r = await runProject(p, false);
                // Run bypasses the project pause dials on purpose; say so,
                // so an intentional override does not read as a bug.
                const note = forceDispatchNote(pause, { projectId: p });
                toast(
                  withForceNote(summarizeRunProjectResult(r), note),
                  r.totalTasksDispatched > 0 ? "success" : "info",
                );
              },
              openTaskList: (p) =>
                openInFocus("task-detail", { projectId: p }, p),
              hideProject: (p) => hideProject(p),
            },
            { taskCount: tasks.length },
          );
          return (
            <div
              key={pid}
              className="proj-row"
              {...rowProps(actions, pid, () => {
                setView("overview");
                setTimeout(() => focusProjectCard(pid), 30);
              })}
              onClick={() => {
                setView("overview");
                setTimeout(() => focusProjectCard(pid), 30);
              }}
              title={
                badges.tasks
                  ? `${pid} — PAUSED`
                  : badges.automations
                    ? `${pid} — automations paused`
                    : pid
              }
            >
              <span
                className={`dot ${dot}`}
                title={badges.tasks ? badges.tasksTitle : undefined}
              />
              <span className="name">{pid}</span>
              {badges.tasks && (
                <span className="pause-tag" title={badges.tasksTitle}>
                  paused
                </span>
              )}
              {badges.automations && (
                <span
                  className="pause-tag autos"
                  title={badges.automationsTitle}
                >
                  autos
                </span>
              )}
              <span className="stats">
                {active > 0 && <span className="active">{active}▸</span>}
                {ready > 0 && <span className="ready">{ready}▪</span>}
                {blocked > 0 && <span className="blocked">{blocked}✕</span>}
              </span>
              <button
                className="proj-row__close"
                onClick={(e) => {
                  e.stopPropagation();
                  hideProject(pid);
                }}
                title="Hide"
                style={{
                  border: 0,
                  background: "transparent",
                  color: "#4b545c",
                  padding: "0 4px",
                  fontSize: 14,
                  lineHeight: 1,
                  cursor: "pointer",
                  borderRadius: 3,
                }}
                onMouseEnter={(e) => {
                  e.currentTarget.style.color = "#d96060";
                  e.currentTarget.style.background = "#2a1a1a";
                }}
                onMouseLeave={(e) => {
                  e.currentTarget.style.color = "#4b545c";
                  e.currentTarget.style.background = "transparent";
                }}
              >
                ×
              </button>
            </div>
          );
        })}

        {hiddenProjectIds.length > 0 && (
          <>
            <div
              onClick={() => setHiddenExpanded((v) => !v)}
              style={{
                padding: "6px 10px",
                marginTop: 4,
                fontSize: 10,
                color: "#6b757e",
                cursor: "pointer",
                display: "flex",
                alignItems: "center",
                gap: 6,
                textTransform: "uppercase",
                letterSpacing: "0.06em",
                userSelect: "none",
              }}
            >
              <span
                style={{
                  display: "inline-block",
                  width: 12,
                  transition: "transform 100ms",
                  transform: hiddenExpanded
                    ? "rotate(0deg)"
                    : "rotate(-90deg)",
                }}
              >
                ▾
              </span>
              Hidden
              <span
                style={{ marginLeft: "auto", color: "#4b545c" }}
              >
                {hiddenProjectIds.length}
              </span>
            </div>
            {hiddenExpanded &&
              hiddenProjectIds.map((pid) => (
                <div
                  key={pid}
                  className="proj-row"
                  style={{ opacity: 0.55 }}
                  onClick={() => showProject(pid)}
                  title={`Show ${pid}`}
                >
                  <span className="dot" />
                  <span className="name">{pid}</span>
                  <span className="stats" style={{ color: "#6b757e" }}>
                    +
                  </span>
                </div>
              ))}
          </>
        )}
      </>
    );
  })();

  const visibleCount = visibleProjectIds.length;
  const totalCount = projects?.length ?? 0;

  return (
    <div className="sb-section">
      <div
        className={`sb-head ${!expanded ? "collapsed" : ""}`}
        onClick={() => toggle("projects")}
      >
        <span className="caret">▾</span>
        Projects
        <span className="count">
          {visibleCount}/{totalCount}
        </span>
      </div>
      {expanded && <div className="sb-list">{rows}</div>}
      {overlays}
    </div>
  );
}
