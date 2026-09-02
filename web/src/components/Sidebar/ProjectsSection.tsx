/**
 * Sidebar — Projects section (wireframe-parity).
 *
 * DOM:
 *   .sb-section
 *     .sb-head (▾ Projects · N/M · ＋)
 *     .sb-list
 *       .proj-row × N (dial button + name + autos tag + stats + × close)
 *       ▸ Hidden (N)   (collapsed by default)
 *         .proj-row × N hidden — click to restore
 *
 * The leading glyph is a BUTTON, not a dot: one click flips the project's
 * task dial. See `ProjectPauseButton` for what the glyph and the colour
 * each mean, and `projectRunIndicator` for how the state is derived. The
 * old "paused" text tag is gone with it — the ⏸ glyph says the same thing
 * one column to the left. The `autos` tag stays: that is the OTHER dial.
 */
import { useState } from "react";
import { useWorkspace } from "../../store/workspace";
import { useProjects } from "../../hooks/useProjects";
import { useVisibleProjects } from "../../hooks/useVisibleProjects";
import { usePauseState } from "../../hooks/usePauseState";
import { useRowActions } from "../../hooks/useRowActions";
import { useLive } from "../../lib/sse";
import {
  buildProjectActions,
  isProjectAutomationsPaused,
  isProjectTasksPaused,
} from "../../lib/actions/projectActions";
import { useProjectActionContext } from "../../hooks/useProjectActionContext";
import { Loading } from "../common/Loading";
import { ErrorState } from "../common/ErrorState";
import { ProjectPauseButton } from "../common/ProjectPauseButton";
import { projectPauseBadges, projectRunIndicator } from "../../lib/pause";

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
  const showProject = useWorkspace((s) => s.showProject);
  const hideProject = useWorkspace((s) => s.hideProject);
  const { data: projects, isLoading, error, refetch } = useProjects();
  const projectCtx = useProjectActionContext();
  const { pause, isLoading: pauseLoading } = usePauseState();
  const liveProjects = useLive((s) => s.projects);
  const [hiddenExpanded, setHiddenExpanded] = useState(false);
  const { rowProps, overlays } = useRowActions();

  // "Visible" means (a) not user-hidden AND (b) matching the current
  // status chip. Shared with the Entries browser via useVisibleProjects,
  // so "the projects in my sidebar" means one thing across the app.
  const { visible: visibleProjectIds, hidden: hiddenProjectIds } =
    useVisibleProjects();

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
          const indicator = projectRunIndicator(tasks, {
            paused: badges.tasks,
            projectId: pid,
          });
          const active = tasks.filter((t) => t.status === "in_progress").length;
          const ready = tasks.filter((t) => t.status === "pending").length;
          const blocked = tasks.filter((t) => t.status === "blocked").length;
          // Right-click used to hide the project outright — a menu-less
          // shortcut nobody could predict. Now it opens the same verb
          // set as the project card header (run / the two pause dials /
          // open in focus / hide), with the × button as the one-click
          // hide it always was.
          //
          // Dial state comes from usePauseState (all three dials, one
          // shared poll) rather than a second runner-status query.
          const actions = buildProjectActions(pid, projectCtx, {
            taskCount: tasks.length,
            // undefined while loading: unknown must not disable the verb.
            tasksPaused: pauseLoading
              ? undefined
              : isProjectTasksPaused(pause, pid),
            automationsPaused: pauseLoading
              ? undefined
              : isProjectAutomationsPaused(pause, pid),
          });
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
                badges.automations ? `${pid} — automations paused` : pid
              }
            >
              <ProjectPauseButton
                projectId={pid}
                indicator={indicator}
                taskCount={tasks.length}
                pauseLoading={pauseLoading}
              />
              <span className="name">{pid}</span>
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
                  transform: hiddenExpanded ? "rotate(0deg)" : "rotate(-90deg)",
                }}
              >
                ▾
              </span>
              Hidden
              <span style={{ marginLeft: "auto", color: "#4b545c" }}>
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
