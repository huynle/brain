import { useUI, type View } from "../../store/ui";

const ITEMS: { view: View; icon: string; label: string }[] = [
  { view: "tasks", icon: "✓", label: "Tasks" },
  { view: "automations", icon: "⟳", label: "Goals" },
  { view: "brain", icon: "◆", label: "Brain" },
  { view: "logs", icon: "▤", label: "Logs" },
  { view: "runners", icon: "⚙", label: "Runners" },
];

export function BottomNav() {
  const view = useUI((s) => s.view);
  const setView = useUI((s) => s.setView);
  return (
    <nav className="bottom-nav">
      {ITEMS.map((it) => (
        <button
          key={it.view}
          className={view === it.view ? "active" : ""}
          onClick={() => setView(it.view)}
        >
          <span className="nav-icon">{it.icon}</span>
          <span>{it.label}</span>
        </button>
      ))}
    </nav>
  );
}
