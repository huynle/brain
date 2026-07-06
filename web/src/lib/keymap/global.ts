// Global action table: the cross-view keys that used to live in
// useGlobalKeyboard's switch, declared as ActionSpecs so dispatch and help
// derive from one place. Also registers the static (help-only) groups for
// list navigation, pane navigation, and popup keys — those dispatch through
// their own machinery but must appear in the help modal.

import { useNav } from "../../store/nav";
import { useUI } from "../../store/ui";
import { registerScope } from "./registry";
import type { ActionHandlers, ActionSpec } from "./types";

export interface GlobalKeyboardOpts {
  projects: string[];
  allLabel: string;
  onRefresh: () => void;
  onPauseToggle: () => void;
  onPauseAll: () => void;
}

export const GLOBAL_SPECS: ActionSpec[] = [
  { id: "global.command", keys: [":"], desc: "Command: jump to views, projects, presets, pause/resume", hint: "Cmd", group: "global" },
  { id: "global.help", keys: ["?"], desc: "Toggle this help", hint: "Help", group: "global" },
  { id: "global.settings", keys: ["S"], desc: "Settings", group: "global" },
  { id: "global.wrap", keys: ["w"], desc: "Toggle text wrap", group: "global" },
  { id: "global.viewNext", keys: ["l", "]"], desc: "Next content tab", hint: "Tabs", group: "global" },
  { id: "global.viewPrev", keys: ["h", "["], desc: "Previous content tab", group: "global" },
  { id: "global.runners", keys: ["R"], desc: "Jump to Runners", group: "global" },
  { id: "global.projNext", keys: ["L"], desc: "Next project", hint: "Proj", group: "global" },
  { id: "global.projPrev", keys: ["H"], desc: "Previous project", group: "global" },
  { id: "global.refresh", keys: ["r"], desc: "Refresh / reconnect", group: "global" },
  { id: "global.pauseToggle", keys: ["p"], desc: "Pause/resume tasks for the active project", group: "global" },
  { id: "global.pauseAll", keys: ["P"], desc: "Pause/resume tasks for all projects", group: "global" },
  { id: "global.escape", keys: ["Escape"], desc: "Clear selection / close", group: "global" },
  // Handled by pre-guard chords in keyboard.ts (they work inside inputs);
  // listed here so help stays complete. No handlers → never dispatched.
  { id: "global.projectPicker", keys: ["M-;", "C-;"], desc: "Quick-switch project (fuzzy search)", group: "global" },
  { id: "global.assistant", keys: ["M-.", "C-."], desc: "Toggle Brain Assistant (sidebar / drawer)", group: "global" },
];

export function buildGlobalHandlers(opts: () => GlobalKeyboardOpts): ActionHandlers {
  const ui = () => useUI.getState();
  const nav = () => useNav.getState();
  const tabs = () => [opts().allLabel, ...opts().projects];
  return {
    "global.command": () => nav().setCommandOpen(true),
    "global.help": () => nav().setHelpOpen(true),
    "global.settings": () => ui().setSettingsOpen(true),
    "global.wrap": () => ui().toggleWrap(),
    "global.viewNext": () => ui().cycleView(1),
    "global.viewPrev": () => ui().cycleView(-1),
    "global.runners": () => ui().setView("runners"),
    "global.projNext": () => {
      const t = tabs();
      const i = t.indexOf(ui().activeProject);
      ui().setActiveProject(t[Math.min(i + 1, t.length - 1)]);
    },
    "global.projPrev": () => {
      const t = tabs();
      const i = t.indexOf(ui().activeProject);
      ui().setActiveProject(t[Math.max(i - 1, 0)]);
    },
    "global.refresh": () => opts().onRefresh(),
    "global.pauseToggle": () => opts().onPauseToggle(),
    "global.pauseAll": () => opts().onPauseAll(),
    "global.escape": () => {
      if (nav().selectedCount() > 0) {
        nav().clearSelect();
        return true;
      }
      return false; // nothing to clear — leave Escape to the browser
    },
  };
}

/** 1–9 project-tab jump, invoked by the count machine's replay path. */
export function jumpToProject(n: number, opts: GlobalKeyboardOpts): void {
  const ui = useUI.getState();
  const tabs = [opts.allLabel, ...opts.projects];
  const idx = n - 1;
  if (idx >= 0 && idx < tabs.length) ui.setActiveProject(tabs[idx]);
}

// ---------------------------------------------------------------------------
// Static help-only groups (dispatch happens in handleListNavKey /
// usePaneNavigation / Modal for now; these keep the help modal complete and
// drift-proof).
// ---------------------------------------------------------------------------

const LISTS_SPECS: ActionSpec[] = [
  { id: "lists.move", keys: ["j", "k"], desc: "Move cursor (accepts a count: 5j)", group: "lists" },
  { id: "lists.jump", keys: ["g", "G"], desc: "Top / bottom", group: "lists" },
  { id: "lists.open", keys: ["Enter"], desc: "Open / descend", group: "lists" },
  { id: "lists.project", keys: ["1"], desc: "1–9: jump to project tab", group: "lists" },
];

const PANES_SPECS: ActionSpec[] = [
  { id: "panes.cycle", keys: ["Tab"], desc: "Cycle pane focus (list → detail → logs)", group: "panes" },
  { id: "panes.cycleBack", keys: ["S-Tab"], desc: "Cycle pane focus backward", group: "panes" },
  { id: "panes.scroll", keys: ["j", "k"], desc: "Scroll line down / up (in focused pane)", group: "panes" },
  { id: "panes.top", keys: ["g g"], desc: "Jump to top (vim gg)", group: "panes" },
  { id: "panes.bottom", keys: ["G"], desc: "Jump to bottom", group: "panes" },
  { id: "panes.halfPage", keys: ["C-d", "C-u"], desc: "Half-page down / up", group: "panes" },
  { id: "panes.resizeRow", keys: ["A-j", "A-k"], desc: "Grow / shrink bottom-row height", group: "panes" },
  { id: "panes.resizeSplit", keys: ["A-l", "A-h"], desc: "Grow / shrink detail vs logs width", group: "panes" },
  { id: "panes.resetSplit", keys: ["dbl-click"], desc: "Double-click a separator to reset that split", group: "panes" },
];

const POPUPS_SPECS: ActionSpec[] = [
  { id: "popups.scroll", keys: ["j", "k"], desc: "Scroll", group: "popups" },
  { id: "popups.jump", keys: ["g", "G"], desc: "Top / bottom", group: "popups" },
  { id: "popups.page", keys: ["C-d", "C-u"], desc: "Page down / up", group: "popups" },
  { id: "popups.expand", keys: ["m"], desc: "Expand / restore", group: "popups" },
  { id: "popups.edit", keys: ["e"], desc: "Edit when available", group: "popups" },
  { id: "popups.close", keys: ["q", "Escape"], desc: "Close", group: "popups" },
];

// Registered once at module load; help-only (no handlers → never dispatch).
registerScope({ scopeId: "help:lists", tier: "global", specs: LISTS_SPECS });
registerScope({ scopeId: "help:panes", tier: "pane", specs: PANES_SPECS });
registerScope({ scopeId: "help:popups", tier: "global", specs: POPUPS_SPECS });
