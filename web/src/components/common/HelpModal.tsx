import { Modal } from "./Modal";
import { useUI, type View } from "../../store/ui";

interface Row {
  keys: string[];
  desc: string;
}
interface Group {
  id: string;
  title: string;
  rows: Row[];
}

// Per-view groups shown first (highlighted) when that tab is active, so the
// help adapts to wherever you are. Global + Lists always follow.
const VIEW_GROUPS: Record<string, Group> = {
  tasks: {
    id: "tasks",
    title: "Tasks",
    rows: [
      { keys: ["Space"], desc: "Select / collapse group" },
      { keys: ["A", "D"], desc: "Select all / deselect all" },
      { keys: ["c"], desc: "Complete" },
      { keys: ["x"], desc: "Run / execute" },
      { keys: ["X"], desc: "Cancel (in-progress)" },
      { keys: ["d", "⌫"], desc: "Delete" },
      { keys: ["s"], desc: "Edit metadata" },
      { keys: ["e"], desc: "Edit content" },
      { keys: ["/"], desc: "Filter" },
      { keys: ["C"], desc: "Tasks ⇄ Schedules" },
      { keys: ["n"], desc: "New task" },
      { keys: ["T"], desc: "Toggle detail pane" },
      { keys: ["z"], desc: "Toggle logs pane" },
      { keys: ["Tab"], desc: "Cycle panel focus" },
    ],
  },
  brain: {
    id: "brain",
    title: "Brain",
    rows: [
      { keys: ["Enter", "e"], desc: "Open / edit entry" },
      { keys: ["/"], desc: "Search" },
      { keys: ["n"], desc: "New entry" },
      { keys: ["b", "B"], desc: "Embed project / all" },
      { keys: ["F", "A"], desc: "Re-embed project / all" },
      { keys: ["T"], desc: "Toggle detail pane" },
      { keys: ["z"], desc: "Toggle logs pane" },
    ],
  },
  automations: {
    id: "automations",
    title: "Automations",
    rows: [
      { keys: ["x"], desc: "Run automation" },
      { keys: ["Enter"], desc: "Expand / configure" },
      { keys: ["o"], desc: "Open / review session" },
      { keys: ["Space"], desc: "Enable / disable" },
      { keys: ["e"], desc: "Configure goal" },
      { keys: ["n"], desc: "New goal" },
      { keys: ["p"], desc: "Pause automations" },
      { keys: ["T"], desc: "Toggle detail pane" },
      { keys: ["z"], desc: "Toggle logs pane" },
    ],
  },
  control: {
    id: "control",
    title: "Control",
    rows: [
      { keys: ["Click"], desc: "Attach to instance / session" },
      { keys: ["Enter"], desc: "Send prompt" },
      { keys: ["▶"], desc: "Resume reviewed session" },
      { keys: ["◼"], desc: "Stop / interrupt" },
    ],
  },
  runners: {
    id: "runners",
    title: "Runners",
    rows: [
      { keys: ["s"], desc: "Shut down runner" },
      { keys: ["p", "P"], desc: "Pause / pause all" },
    ],
  },
  logs: {
    id: "logs",
    title: "Logs (server requests)",
    rows: [
      { keys: ["j", "k"], desc: "Scroll" },
      { keys: ["g", "G"], desc: "Top / bottom" },
      { keys: ["f"], desc: "Follow / live tail" },
    ],
  },
};

const GLOBAL: Group = {
  id: "global",
  title: "Global",
  rows: [
    { keys: ["h", "l", "[", "]"], desc: "Previous / next tab" },
    { keys: ["H", "L"], desc: "Previous / next project" },
    { keys: ["1–9"], desc: "Jump to project tab" },
    { keys: ["R"], desc: "Jump to Runners" },
    { keys: ["S"], desc: "Settings" },
    { keys: ["w"], desc: "Toggle text wrap" },
    { keys: ["p", "P"], desc: "Pause project / all" },
    { keys: ["r"], desc: "Refresh / reconnect" },
    { keys: ["?"], desc: "Toggle this help" },
    { keys: ["Esc"], desc: "Clear selection / close" },
  ],
};

const LISTS: Group = {
  id: "lists",
  title: "Lists (all tabs)",
  rows: [
    { keys: ["j", "k"], desc: "Move cursor" },
    { keys: ["g", "G"], desc: "Top / bottom" },
    { keys: ["Enter"], desc: "Open / expand" },
  ],
};

const VIEW_LABEL: Record<string, string> = {
  tasks: "Tasks",
  brain: "Brain",
  automations: "Automations",
  control: "Control",
  runners: "Runners",
  logs: "Logs",
};

export function HelpModal({ onClose }: { onClose: () => void }) {
  const view = useUI((s) => s.view) as View;
  const current = VIEW_GROUPS[view];

  // Current tab first (highlighted), then Global + Lists, then the other tabs.
  const others = Object.values(VIEW_GROUPS).filter((g) => g.id !== view);
  const ordered: Group[] = [...(current ? [current] : []), GLOBAL, LISTS, ...others];

  return (
    <Modal title={`Keyboard shortcuts — ${VIEW_LABEL[view] ?? ""}`} onClose={onClose}>
      <div className="help-grid">
        {ordered.map((g) => (
          <div key={g.id} className={`help-group ${g.id === view ? "help-current" : ""}`}>
            <h3>
              {g.title}
              {g.id === view && <span className="faint" style={{ fontWeight: 400 }}> · current tab</span>}
            </h3>
            {g.rows.map((r, i) => (
              <div key={i} className="help-row">
                <span className="help-keys">
                  {r.keys.map((k) => (
                    <kbd key={k}>{k}</kbd>
                  ))}
                </span>
                <span className="help-desc">{r.desc}</span>
              </div>
            ))}
          </div>
        ))}
      </div>
      <div className="faint" style={{ fontSize: 12, marginTop: "0.8rem", textAlign: "center" }}>
        Shortcuts work on desktop; tap targets work everywhere.
      </div>
    </Modal>
  );
}
