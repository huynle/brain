import { useLive } from "../../lib/sse";
import { ALL_PROJECTS, useUI, type View } from "../../store/ui";

const GLOBAL: { view: View; label: string }[] = [
  { view: "runners", label: "Runners" },
  { view: "control", label: "Control" },
  { view: "logs", label: "Logs" },
];
const PROJECT: { view: View; label: string }[] = [
  { view: "tasks", label: "Tasks" },
  { view: "brain", label: "Brain" },
  { view: "automations", label: "Automations" },
];

function shortName(id: string): string {
  return id.split(/[/\\]/).pop() || id;
}

export function ContentTabs({ projects }: { projects: string[] }) {
  const view = useUI((s) => s.view);
  const setView = useUI((s) => s.setView);
  const activeProject = useUI((s) => s.activeProject);
  const setActiveProject = useUI((s) => s.setActiveProject);
  const liveProjects = useLive((s) => s.projects);

  const showProjectTabs = view === "tasks" || view === "brain" || view === "automations";

  return (
    <>
      <div className="tui-tabbar" role="tablist">
        {GLOBAL.map((t) => (
          <button
            key={t.view}
            className={`tui-tab ${view === t.view ? "on" : ""}`}
            onClick={() => setView(t.view)}
          >
            {t.label}
          </button>
        ))}
        <span className="tui-tabdiv">│</span>
        {PROJECT.map((t) => (
          <button
            key={t.view}
            className={`tui-tab ${view === t.view ? "on" : ""}`}
            onClick={() => setView(t.view)}
          >
            {t.label}
          </button>
        ))}
      </div>

      {showProjectTabs && (
        <div className="tui-tabbar">
          <button
            className={`tui-tab proj ${activeProject === ALL_PROJECTS ? "on" : ""}`}
            onClick={() => setActiveProject(ALL_PROJECTS)}
          >
            all
            <span className="cnt">
              {Object.values(liveProjects).reduce((n, p) => n + p.tasks.length, 0)}
            </span>
          </button>
          {projects.map((p) => (
            <button
              key={p}
              className={`tui-tab proj ${activeProject === p ? "on" : ""}`}
              onClick={() => setActiveProject(p)}
            >
              {shortName(p)}
              {liveProjects[p]?.tasks.length ? (
                <span className="cnt">{liveProjects[p].tasks.length}</span>
              ) : null}
            </button>
          ))}
        </div>
      )}
    </>
  );
}
