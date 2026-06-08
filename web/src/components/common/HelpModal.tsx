import { Modal } from "./Modal";

interface Row {
  keys: string[];
  desc: string;
}
interface Group {
  title: string;
  rows: Row[];
}

const GROUPS: Group[] = [
  {
    title: "Global",
    rows: [
      { keys: ["H", "L"], desc: "Previous / next tab" },
      { keys: ["h", "l", "[", "]"], desc: "Previous / next project" },
      { keys: ["1–9"], desc: "Jump to project tab" },
      { keys: ["R"], desc: "Jump to Runners" },
      { keys: ["S"], desc: "Settings" },
      { keys: ["w"], desc: "Toggle text wrap" },
      { keys: ["p", "P"], desc: "Pause project / all" },
      { keys: ["r"], desc: "Refresh / reconnect" },
      { keys: ["?"], desc: "Toggle this help" },
      { keys: ["Esc"], desc: "Clear selection / close" },
    ],
  },
  {
    title: "Lists (Tasks · Brain · Automations · Runners)",
    rows: [
      { keys: ["j", "k"], desc: "Move cursor" },
      { keys: ["g", "G"], desc: "Top / bottom" },
      { keys: ["Enter"], desc: "Open / expand" },
    ],
  },
  {
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
      { keys: ["y"], desc: "Yank title" },
      { keys: ["/"], desc: "Filter" },
      { keys: ["C"], desc: "Tasks ⇄ Schedules" },
      { keys: ["n"], desc: "New task" },
    ],
  },
  {
    title: "Brain",
    rows: [
      { keys: ["/"], desc: "Search" },
      { keys: ["e"], desc: "Edit entry" },
      { keys: ["n"], desc: "New entry" },
      { keys: ["b", "B"], desc: "Embed project / all" },
      { keys: ["F", "A"], desc: "Re-embed project / all" },
    ],
  },
  {
    title: "Automations",
    rows: [
      { keys: ["Space"], desc: "Enable / disable" },
      { keys: ["x"], desc: "Run / reconcile" },
      { keys: ["e"], desc: "Configure goal" },
      { keys: ["p"], desc: "Pause automations" },
      { keys: ["C"], desc: "Automations ⇄ Dream" },
    ],
  },
  {
    title: "Dream",
    rows: [
      { keys: ["/"], desc: "Search" },
      { keys: ["n", "N"], desc: "Next / previous match" },
    ],
  },
  {
    title: "Runners · Logs",
    rows: [
      { keys: ["s"], desc: "Shut down runner" },
      { keys: ["p", "P"], desc: "Pause / pause all" },
      { keys: ["f"], desc: "Follow logs" },
    ],
  },
];

export function HelpModal({ onClose }: { onClose: () => void }) {
  return (
    <Modal title="Keyboard shortcuts" onClose={onClose}>
      <div className="help-grid">
        {GROUPS.map((g) => (
          <div key={g.title} className="help-group">
            <h3>{g.title}</h3>
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
