/**
 * Topbar — wireframe-parity port of `renderTopbar` from panes-v2.js.
 *
 * DOM:
 *   .topbar
 *     .brand ("brain workspace")
 *     .viewmode (Overview / Focus)
 *     .search (input + ⌘K hint)
 *     .saved-views (Execute / Review / Memory / Automate / Runners)
 *     .spacer
 *     icon buttons (command palette, new session, notifs, theme, assistant)
 */
import { useWorkspace } from "../store/workspace";

export function Topbar(): JSX.Element {
  const view = useWorkspace((s) => s.view);
  const setView = useWorkspace((s) => s.setView);
  const setCommandOpen = useWorkspace((s) => s.setCommandOpen);
  const toggleAssistant = useWorkspace((s) => s.toggleAssistant);
  const assistantOpen = useWorkspace((s) => s.assistantOpen);
  const theme = useWorkspace((s) => s.theme);
  const cycleTheme = useWorkspace((s) => s.cycleTheme);
  const toggleSidebarCollapsed = useWorkspace((s) => s.toggleSidebarCollapsed);

  return (
    <div className="topbar">
      <button
        className="icon-btn"
        title="Toggle sidebar"
        onClick={toggleSidebarCollapsed}
        style={{ padding: "4px 6px" }}
      >
        ☰
      </button>
      <span className="brand">
        brain <span>workspace</span>
      </span>
      <div className="viewmode">
        <button
          className={view === "overview" ? "active" : ""}
          onClick={() => setView("overview")}
        >
          Overview
        </button>
        <button
          className={view === "focus" ? "active" : ""}
          onClick={() => setView("focus")}
        >
          Focus
        </button>
      </div>
      <div className="search" onClick={() => setCommandOpen(true)}>
        <span style={{ color: "#6b757e" }}>⌕</span>
        <input
          type="search"
          placeholder="Search projects, tasks, entries…"
          onFocus={() => setCommandOpen(true)}
          readOnly
        />
        <span className="hint">⌘K</span>
      </div>
      <div className="spacer" />
      <button
        className="icon-btn"
        title="Command palette (⌘K)"
        onClick={() => setCommandOpen(true)}
      >
        ⌘K
      </button>
      <button
        className="icon-btn"
        title={
          theme === "dark"
            ? "Switch to light"
            : theme === "light"
              ? "Switch to system"
              : "Switch to dark"
        }
        onClick={cycleTheme}
      >
        {theme === "dark" ? "🌙" : theme === "light" ? "☀" : "◐"}
      </button>
      <button
        className="icon-btn"
        title="Assistant"
        onClick={toggleAssistant}
      >
        Assistant {assistantOpen ? "▾" : "▸"}
      </button>
    </div>
  );
}
