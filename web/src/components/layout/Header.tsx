import { useLive } from "../../lib/sse";
import { ALL_PROJECTS, useUI } from "../../store/ui";
import { useLiveTasks, deriveCounts } from "../../hooks/useLiveTasks";

function shortName(id: string): string {
  const parts = id.split(/[/\\]/);
  return parts[parts.length - 1] || id;
}

export function Header({
  projects,
  onOpenSettings,
}: {
  projects: string[];
  onOpenSettings: () => void;
}) {
  const view = useUI((s) => s.view);
  const activeProject = useUI((s) => s.activeProject);
  const setActiveProject = useUI((s) => s.setActiveProject);
  const liveProjects = useLive((s) => s.projects);

  const { tasks, stats, connected } = useLiveTasks(activeProject);
  const { active, completed } = deriveCounts(tasks);

  const showProjectUI = view === "tasks" || view === "logs";

  return (
    <header className="app-header">
      <div className="statusbar">
        <div className="brand">
          <img src="/favicon.svg" alt="" />
          <span>Brain</span>
        </div>
        <span
          className={`conn-dot ${connected ? "live" : "down"}`}
          title={connected ? "Live" : "Reconnecting…"}
        />
        <div className="spacer" />
        <button
          className="icon-btn"
          onClick={onOpenSettings}
          aria-label="settings"
          title="Settings"
        >
          ⚙
        </button>
      </div>

      {showProjectUI && (
        <>
          <div className="tabs" role="tablist">
            <Tab
              label="All"
              count={Object.values(liveProjects).reduce(
                (n, p) => n + p.tasks.length,
                0,
              )}
              active={activeProject === ALL_PROJECTS}
              onClick={() => setActiveProject(ALL_PROJECTS)}
            />
            {projects.map((p) => (
              <Tab
                key={p}
                label={shortName(p)}
                count={liveProjects[p]?.tasks.length}
                active={activeProject === p}
                onClick={() => setActiveProject(p)}
              />
            ))}
          </div>

          {view === "tasks" && (
            <div className="stat-pills">
              <StatPill label="ready" n={stats.ready} color="var(--yellow)" />
              <StatPill label="active" n={active} color="var(--blue)" />
              <StatPill label="waiting" n={stats.waiting} color="var(--fg-dim)" />
              <StatPill label="blocked" n={stats.blocked} color="var(--red)" />
              <StatPill label="done" n={completed} color="var(--green)" />
            </div>
          )}
        </>
      )}
    </header>
  );
}

function Tab({
  label,
  count,
  active,
  onClick,
}: {
  label: string;
  count?: number;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      className={`tab ${active ? "active" : ""}`}
      onClick={onClick}
      role="tab"
      aria-selected={active}
    >
      {label}
      {count !== undefined && count > 0 && (
        <span className="count">{count}</span>
      )}
    </button>
  );
}

function StatPill({
  label,
  n,
  color,
}: {
  label: string;
  n: number;
  color: string;
}) {
  return (
    <span className="pill" style={{ color }}>
      <strong>{n}</strong> {label}
    </span>
  );
}
