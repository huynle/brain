import { helpBarHints } from "../../lib/keymap/registry";
import { prettyChord } from "../../lib/keymap/types";
import { useKeymapVersion } from "../../lib/keymap/useActions";
import { useNav } from "../../store/nav";
import { useUI } from "../../store/ui";

type Hint = [string, string]; // [keys, label]

// Hints shared by every view, appended after the view-specific ones. Views
// migrated to the keymap registry derive their hints from their ActionSpec
// tables; these cover the global tier the registry marks with `hint`.
const TAIL: Hint[] = [
  ["Tab", "Focus"],
  ["h/l", "Tabs"],
  [":", "Cmd"],
  ["H/L", "Proj"],
  ["?", "Help"],
];

const HINTS: Record<string, Hint[]> = {
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
  useKeymapVersion();

  // Migrated views: hinted ActionSpecs from the registry (drift-proof).
  // Unmigrated views fall back to their static table until phase 5 lands.
  // A view counts as migrated when any view-tier scope contributes hints.
  const all = helpBarHints({ focus, hasSelection: selected > 0, isMobile: false });
  const migrated = all.some((h) => h.tier === "view");
  const derived = all
    .filter((h) => h.tier !== "global")
    .map((h) => [h.keys.map(prettyChord).join("/"), h.hint] as Hint);
  const hints = migrated ? [...derived, ...TAIL] : (HINTS[view] ?? TAIL);
  // Views that have a detail/logs pane resolve the focus label per-pane;
  // others just show the view name.
  const viewsWithPanes = new Set(["tasks", "brain", "automations"]);
  const focusName = viewsWithPanes.has(view)
    ? focus === "detail"
      ? "Details"
      : focus === "logs"
        ? "Logs"
        : FOCUS_LABEL[view]
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
