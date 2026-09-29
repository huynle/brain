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
import { useState } from "react";
import { useIsMobile } from "../hooks/useIsMobile";
import { Modal } from "./common/Modal";
import { useWorkspace } from "../store/workspace";
import { ReminderBell } from "./ReminderBell";
import { countLeaves } from "../lib/dock";

export function Topbar({
  onOpenNavigation,
}: {
  onOpenNavigation?: () => void;
}): JSX.Element {
  const mobile = useIsMobile();
  const [toolsOpen, setToolsOpen] = useState(false);
  const view = useWorkspace((s) => s.view);
  const setView = useWorkspace((s) => s.setView);
  const setCommandOpen = useWorkspace((s) => s.setCommandOpen);
  const toggleAssistant = useWorkspace((s) => s.toggleAssistant);
  const assistantOpen = useWorkspace((s) => s.assistantOpen);
  const theme = useWorkspace((s) => s.theme);
  const cycleTheme = useWorkspace((s) => s.cycleTheme);
  const toggleSidebarCollapsed = useWorkspace((s) => s.toggleSidebarCollapsed);
  const sidebarDockOpen = useWorkspace((s) => s.sidebarDockOpen);
  const toggleSidebarDockOpen = useWorkspace((s) => s.toggleSidebarDockOpen);

  /*
   * Both dock trees persist across reloads, so panes parked in them
   * outlive the session that opened them. Without a count here the
   * Focus tab looks identical whether it holds nothing or holds the
   * three-pane layout you set up yesterday — which is most of why the
   * workspace reads as an empty room. Zero renders no badge; a badge
   * that always says "0" is just noise.
   */
  const focusPanes = useWorkspace((s) => countLeaves(s.docks.focus));
  const sidebarPanes = useWorkspace((s) => countLeaves(s.docks.sidebar));

  const tools = (
    <>
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
        {mobile
          ? `Theme: ${theme}`
          : theme === "dark"
            ? "🌙"
            : theme === "light"
              ? "☀"
              : "◐"}
      </button>

      <button
        className={"icon-btn" + (sidebarDockOpen ? " active" : "")}
        title={sidebarDockOpen ? "Close side panel" : "Open side panel"}
        onClick={toggleSidebarDockOpen}
      >
        Panel
        {sidebarPanes > 0 && (
          <span className="dock-count">{sidebarPanes}</span>
        )}{" "}
        {sidebarDockOpen ? "▸" : "◂"}
      </button>
      {!mobile && (
        <button
          className="icon-btn"
          title="Assistant"
          aria-label="Assistant"
          onClick={toggleAssistant}
        >
          Assistant {assistantOpen ? "▾" : "▸"}
        </button>
      )}
    </>
  );
  return (
    <div className="topbar">
      <button
        className="icon-btn"
        title={mobile ? "Open workspace navigation" : "Toggle sidebar"}
        aria-label={mobile ? "Open workspace navigation" : "Toggle sidebar"}
        onClick={mobile ? onOpenNavigation : toggleSidebarCollapsed}
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
          title={
            focusPanes === 0
              ? "Focus workspace — split panes for watching work run"
              : `Focus workspace — ${focusPanes} pane${focusPanes === 1 ? "" : "s"} open`
          }
        >
          Focus
          {focusPanes > 0 && <span className="dock-count">{focusPanes}</span>}
        </button>
        <button
          className={view === "entries" ? "active" : ""}
          onClick={() => setView("entries")}
        >
          Entries
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
        aria-label="Search and commands"
        onClick={() => setCommandOpen(true)}
      >
        {mobile ? "Search" : "⌘K"}
      </button>
      <ReminderBell />
      {mobile ? (
        <button
          className="icon-btn"
          aria-label="More tools"
          onClick={() => setToolsOpen(true)}
        >
          More ⋯
        </button>
      ) : (
        tools
      )}
      {mobile && toolsOpen && (
        <Modal
          title="Tools"
          className="mobile-tools"
          onClose={() => setToolsOpen(false)}
        >
          <div
            onClick={(e) => {
              if ((e.target as HTMLElement).closest("button"))
                setToolsOpen(false);
            }}
          >
            {tools}
          </div>
        </Modal>
      )}
    </div>
  );
}
