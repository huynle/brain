/**
 * Sidebar — wireframe-parity port of `renderSidebar` from panes-v2.js.
 *
 * DOM:
 *   .sidebar
 *     .sb-top (title + collapse)
 *     .sb-filters (status filters: All / Active / Ready / Blocked / Done / Archived)
 *     .sb-section (Projects)
 *     .sb-section (Sessions)
 *     .sb-section (Runners)
 *     .sb-foot (avatar + settings)
 */
import { useMemo } from "react";
import { useWorkspace } from "../../store/workspace";
import { useModal } from "../../store/modal";
import { ProjectsSection } from "./ProjectsSection";
import { SessionsSection } from "./SessionsSection";
import { RunnersSection } from "./RunnersSection";
import { useLive } from "../../lib/sse";
import { useProjects } from "../../hooks/useProjects";
import { useEdgeResize } from "../../hooks/useEdgeResize";
import type { StatusFilter } from "../../store/workspace";

export function Sidebar(): JSX.Element {
  const collapsed = useWorkspace((s) => s.sidebarCollapsed);
  const toggleCollapsed = useWorkspace((s) => s.toggleSidebarCollapsed);
  const openModal = useModal((s) => s.open);
  const statusFilter = useWorkspace((s) => s.statusFilter);
  const setStatusFilter = useWorkspace((s) => s.setStatusFilter);
  const setSidebarWidth = useWorkspace((s) => s.setSidebarWidth);

  // ─── right-edge drag-resize ─────────────────────────────────────
  // The sidebar is anchored to the viewport's LEFT edge, so its width
  // is simply the pointer's distance from that edge (unlike the
  // drawer, which is anchored right and has to subtract from
  // innerWidth).
  const startResize = useEdgeResize({
    computeWidth: (clientX) => clientX,
    onResize: setSidebarWidth,
    bodyClass: "sidebar-resizing",
  });

  // Subscribe to the live projects map (stable object reference between
  // updates), then derive totals in useMemo so we don't return a fresh
  // object from the zustand selector on every render (which triggers
  // "getSnapshot should be cached" + infinite render).
  const { data: projects } = useProjects();
  const liveProjects = useLive((s) => s.projects);
  const totals = useMemo(() => {
    let active = 0;
    let ready = 0;
    let blocked = 0;
    let completed = 0;
    let archived = 0;
    for (const p of projects ?? []) {
      const live = liveProjects[p];
      if (!live) continue;
      for (const t of live.tasks) {
        if (t.status === "in_progress") active++;
        else if (t.status === "pending") ready++;
        else if (t.status === "blocked") blocked++;
        else if (t.status === "completed" || t.status === "validated")
          completed++;
        else if (t.status === "archived") archived++;
      }
    }
    return { active, ready, blocked, completed, archived };
  }, [projects, liveProjects]);

  if (collapsed) {
    // Wireframe uses `body.sidebar-collapsed` + a floating restore
    // pill that lives outside the sidebar. We render the restore
    // pill here so it can share workspace state without extra portal
    // plumbing.
    return (
      <button
        className="sidebar-restore"
        onClick={toggleCollapsed}
        title="Show sidebar"
      >
        ☰ <span>sidebar</span>
      </button>
    );
  }

  return (
    <div className="sidebar">
      <div className="sb-top">
        <span className="sb-title">workspace</span>
        <button
          className="collapse-btn"
          onClick={toggleCollapsed}
          title="Collapse sidebar"
        >
          ⇤
        </button>
      </div>

      <div className="sb-filters">
        {(
          [
            { key: "all", label: "All", count: null },
            { key: "active", label: "Active", count: totals.active },
            { key: "ready", label: "Ready", count: totals.ready },
            { key: "blocked", label: "Blocked", count: totals.blocked },
            { key: "done", label: "Done", count: totals.completed },
            { key: "archived", label: "Archived", count: totals.archived },
          ] as Array<{ key: StatusFilter; label: string; count: number | null }>
        ).map((c) => (
          <button
            key={c.key}
            type="button"
            className={`chip ${statusFilter === c.key ? "active" : ""}`}
            onClick={() => setStatusFilter(c.key)}
            aria-pressed={statusFilter === c.key}
            title={
              c.key === "all"
                ? "Show every project"
                : `Show only projects with ${c.label.toLowerCase()} tasks`
            }
          >
            {c.label}
            {c.count !== null && <span className="n">{c.count}</span>}
          </button>
        ))}
      </div>

      <ProjectsSection />
      <SessionsSection />
      <RunnersSection />

      <div className="sb-foot">
        <span className="avatar">e</span>
        <span>you</span>
        <span className="spacer" style={{ flex: 1 }} />
        <button
          className="icon-btn"
          title="Settings"
          onClick={() => openModal("settings", {})}
        >
          ⚙
        </button>
      </div>

      <div className="sidebar-resizer" onPointerDown={startResize} />
    </div>
  );
}
