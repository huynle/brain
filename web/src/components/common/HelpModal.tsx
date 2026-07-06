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
