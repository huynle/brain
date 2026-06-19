import { useUI, type View } from "../../store/ui";

const GLOBAL: { view: View; label: string }[] = [
  { view: "runners", label: "Runners" },
  { view: "logs", label: "Logs" },
];
const PROJECT: { view: View; label: string }[] = [
  { view: "brain", label: "Brain" },
  { view: "tasks", label: "Tasks" },
  { view: "automations", label: "Automations" },
];

export function ContentTabs() {
  const view = useUI((s) => s.view);
  const setView = useUI((s) => s.setView);

  // The project-tab row is intentionally not rendered on any tab. Projects are
  // switched with H/L (shift) and the active project is shown in the status
  // bar — keeping every content tab visually clean.
  return (
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
  );
}
