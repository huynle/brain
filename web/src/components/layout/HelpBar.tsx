import { useNav } from "../../store/nav";
import { useUI } from "../../store/ui";

type Hint = [string, string]; // [keys, label]

const HINTS: Record<string, Hint[]> = {
  tasks: [
    ["j/k", "Nav"],
    ["Spc", "Select"],
    ["Enter", "Open"],
    ["c", "Done"],
    ["x", "Run"],
    ["d", "Del"],
    ["s", "Edit"],
    ["/", "Filter"],
    ["C", "Sched"],
    ["Tab", "Panel"],
    ["?", "Help"],
  ],
  brain: [
    ["j/k", "Nav"],
    ["Enter", "Open"],
    ["/", "Search"],
    ["e", "Edit"],
    ["b/B", "Embed"],
    ["n", "New"],
    ["H/L", "Tabs"],
    ["?", "Help"],
  ],
  automations: [
    ["j/k", "Nav"],
    ["n", "New"],
    ["Spc", "Toggle"],
    ["x", "Run"],
    ["e", "Config"],
    ["p", "Pause"],
    ["C", "Dream"],
    ["?", "Help"],
  ],
  runners: [
    ["j/k", "Nav"],
    ["s", "Shutdown"],
    ["p/P", "Pause"],
    ["H/L", "Tabs"],
    ["?", "Help"],
  ],
  control: [
    ["Click", "Attach"],
    ["Enter", "Send"],
    ["H/L", "Tabs"],
    ["?", "Help"],
  ],
  logs: [
    ["j/k", "Scroll"],
    ["g/G", "Top/Bot"],
    ["f", "Follow"],
    ["H/L", "Tabs"],
    ["?", "Help"],
  ],
};

const FOCUS_LABEL: Record<string, string> = {
  tasks: "Tasks",
  brain: "Brain",
  automations: "Automations",
  runners: "Runners",
  control: "Control",
  logs: "Logs",
};

export function HelpBar() {
  const view = useUI((s) => s.view);
  const focus = useUI((s) => s.focus);
  const selected = useNav((s) => Object.keys(s.selected).length);

  const hints = HINTS[view] ?? HINTS.tasks;
  const focusName =
    view === "tasks"
      ? focus === "detail"
        ? "Details"
        : focus === "logs"
          ? "Logs"
          : "Tasks"
      : FOCUS_LABEL[view];

  return (
    <div className="tui-helpbar">
      {hints.map(([k, l]) => (
        <span className="hk" key={k}>
          <b>{k}</b> {l}
        </span>
      ))}
      {selected > 0 && (
        <span className="hk" style={{ color: "var(--blue)" }}>
          <b>{selected}</b> selected
        </span>
      )}
      <span className="hspacer" />
      <span className="focus">Focus: {focusName}</span>
    </div>
  );
}
