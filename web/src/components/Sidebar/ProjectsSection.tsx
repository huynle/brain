/**
 * Sidebar — Projects section (wireframe-parity).
 *
 * DOM:
 *   .sb-section
 *     .sb-head (▾ Projects · N/M · ＋)
 *     .sb-list
 *       .proj-row × N (dot + name + stats + × close)
 *       ▸ Hidden (N)   (collapsed by default)
 *         .proj-row × N hidden — click to restore
 */
import { useState } from "react";
import { useWorkspace } from "../../store/workspace";
import { useProjects } from "../../hooks/useProjects";
import { useLive } from "../../lib/sse";
import { Loading } from "../common/Loading";
import { ErrorState } from "../common/ErrorState";
import { projectMatchesStatusFilter } from "../../lib/statusFilter";
import type { Task } from "../../lib/types";

type Dot = "on" | "busy" | "err" | "";

function projectDot(tasks: readonly Task[]): Dot {
  if (tasks.length === 0) return "";
  let hasBusy = false;
  let hasBlocked = false;
  for (const t of tasks) {
    if (t.status === "in_progress") hasBusy = true;
    else if (t.status === "blocked") hasBlocked = true;
  }
  if (hasBusy) return "busy";
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
  const { data: projects, isLoading, error, refetch } = useProjects();
  const liveProjects = useLive((s) => s.projects);
  const statusFilter = useWorkspace((s) => s.statusFilter);
  const [hiddenExpanded, setHiddenExpanded] = useState(false);

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
          const dot = projectDot(tasks);
          const active = tasks.filter(
            (t) => t.status === "in_progress",
          ).length;
          const ready = tasks.filter((t) => t.status === "pending").length;
          const blocked = tasks.filter((t) => t.status === "blocked").length;
          return (
            <div
              key={pid}
              className="proj-row"
              onClick={() => {
                setView("overview");
                setTimeout(() => focusProjectCard(pid), 30);
              }}
              onContextMenu={(e) => {
                e.preventDefault();
                hideProject(pid);
              }}
              title={pid}
            >
              <span className={`dot ${dot}`} />
              <span className="name">{pid}</span>
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
    </div>
  );
}
