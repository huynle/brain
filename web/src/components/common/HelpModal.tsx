import { Modal } from "./Modal";
import { useUI, type View } from "../../store/ui";
import { helpModalGroups } from "../../lib/keymap/registry";
import { prettyChord } from "../../lib/keymap/types";
import { useKeymapVersion } from "../../lib/keymap/useActions";

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
      { keys: ["Enter"], desc: "View entry file" },
      { keys: ["s"], desc: "Edit metadata" },
      { keys: ["e"], desc: "Edit full file" },
      { keys: ["y"], desc: "Copy task title" },
      { keys: ["{", "}"], desc: "Collapse / expand all groups" },
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
      { keys: ["Enter"], desc: "Open entry" },
      { keys: ["e"], desc: "Edit entry" },
      { keys: ["/"], desc: "Search" },
      { keys: ["n"], desc: "New entry" },
      { keys: ["b", "B"], desc: "Embed project / all" },
      { keys: ["F", "A"], desc: "Re-embed project / all" },
      { keys: ["T"], desc: "Toggle detail pane" },
      { keys: ["z"], desc: "Toggle logs pane" },
      { keys: ["Tab"], desc: "Cycle panel focus" },
    ],
  },
  automations: {
    id: "automations",
    title: "Automations",
    rows: [
      { keys: ["x"], desc: "Run automation" },
      { keys: ["Enter"], desc: "Expand / configure" },
      { keys: ["o"], desc: "Open / review session" },
      { keys: ["Space"], desc: "Select run task / toggle automation" },
      { keys: ["A", "D"], desc: "Select all run tasks / clear selection" },
      { keys: ["d", "⌫"], desc: "Delete selected run tasks" },
      { keys: ["e"], desc: "Edit (goal config / file)" },
      { keys: ["n"], desc: "New automation" },
      { keys: ["p"], desc: "Pause automations" },
      { keys: ["T"], desc: "Toggle detail pane" },
      { keys: ["z"], desc: "Toggle logs pane" },
      { keys: ["Tab"], desc: "Cycle panel focus" },
    ],
  },
  control: {
    id: "control",
    title: "Control",
    rows: [
      { keys: ["j", "k", "g", "G"], desc: "Move through runner rail" },
      { keys: ["Enter"], desc: "Attach / open instance" },
      { keys: ["n", "+"], desc: "New instance" },
      { keys: ["x", "s"], desc: "Kill ad-hoc instance" },
      { keys: ["Esc", "⌫"], desc: "Back from chat/history to rail" },
      { keys: ["▶"], desc: "Resume reviewed session" },
      { keys: ["◼"], desc: "Stop / interrupt" },
    ],
  },
  runners: {
    id: "runners",
    title: "Runners",
    rows: [
      { keys: ["j", "k", "g", "G"], desc: "Move through runners and instances" },
      { keys: ["Enter", "o"], desc: "Open instance in Control" },
      { keys: ["n", "+"], desc: "Spawn new ad-hoc instance" },
      { keys: ["s"], desc: "Shut down cursored runner" },
      { keys: ["K"], desc: "Kill cursored instance" },
      { keys: ["p", "P"], desc: "Pause/resume active scope" },
      { keys: ["a", "A"], desc: "Pause/resume automations" },
    ],
  },
  logs: {
    id: "logs",
    title: "Logs (server requests)",
    rows: [
      { keys: ["j", "k"], desc: "Scroll" },
      { keys: ["g", "G"], desc: "Top / bottom" },
      { keys: ["/"], desc: "Filter" },
      { keys: ["f"], desc: "Follow / live tail" },
    ],
  },
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
  useKeymapVersion();
  const current = VIEW_GROUPS[view];

  // Shared groups (global / lists / panes / popups) derive from the keymap
  // registry — the same specs that dispatch — so they cannot drift. Per-view
  // groups stay static until each view migrates to ActionSpec tables; a
  // registry group with a view's id takes precedence over its static table.
  const derived = helpModalGroups(view).map((g) => ({
    id: g.id,
    title: g.title,
    rows: g.rows.map((r) => ({ keys: r.keys.map(prettyChord), desc: r.desc })),
  }));
  const derivedIds = new Set(derived.map((g) => g.id));

  // Current tab first (registry version if migrated, else static),
  // then the derived shared groups, then the other static tabs.
  const currentGroup = derivedIds.has(view) ? derived.find((g) => g.id === view) : current;
  const shared = derived.filter((g) => g.id !== view);
  const others = Object.values(VIEW_GROUPS).filter((g) => g.id !== view && !derivedIds.has(g.id));
  const ordered: Group[] = [...(currentGroup ? [currentGroup] : []), ...shared, ...others];

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
