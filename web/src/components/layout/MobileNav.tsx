import { useUI, type View } from "../../store/ui";

// Bottom tab bar for touch devices — replaces the keyboard HelpBar. Tappable
// tabs (also swipeable via the content area), thumb-reachable. The active
// project is switched from the status bar's project chip (opens a sheet).
const TABS: { view: View; label: string; glyph: string }[] = [
  { view: "tasks", label: "Tasks", glyph: "☰" },
  { view: "brain", label: "Brain", glyph: "◆" },
  { view: "automations", label: "Auto", glyph: "⟳" },
  { view: "control", label: "Control", glyph: "⌁" },
  { view: "logs", label: "Logs", glyph: "▤" },
  { view: "runners", label: "Runners", glyph: "⚙" },
];

export function MobileNav() {
  const view = useUI((s) => s.view);
  const setView = useUI((s) => s.setView);

  return (
    <nav className="mnav" role="tablist">
      {TABS.map((t) => (
        <button
          key={t.view}
          className={`mnav-tab ${view === t.view ? "on" : ""}`}
          aria-selected={view === t.view}
          onClick={() => setView(t.view)}
        >
          <span className="mnav-glyph">{t.glyph}</span>
          <span className="mnav-label">{t.label}</span>
        </button>
      ))}
    </nav>
  );
}
