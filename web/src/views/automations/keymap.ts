// Automations view action table. Static specs — help derives from these;
// handlers close over view state in AutomationsView.
//
// Note: p (pause autos) and r (refresh) shadow the global keys here with
// automation-scoped equivalents — an intentional, registry-visible override.

import { listNavSpecs } from "../../lib/keymap/listNav";
import type { ActionSpec } from "../../lib/keymap/types";

const listOnly = { focus: ["tasks" as const] };

export const AUTOMATIONS_SPECS: ActionSpec[] = [
  ...listNavSpecs("automations", "Move through automations and run tasks").map((s) => ({ ...s, when: listOnly })),
  { id: "automations.toggleDetail", keys: ["T"], desc: "Toggle detail pane", group: "automations" },
  { id: "automations.toggleLogs", keys: ["z"], desc: "Toggle logs pane", group: "automations" },
  { id: "automations.open", keys: ["o", "O"], desc: "Open / review run-task session", hint: "Open", group: "automations", when: listOnly },
  { id: "automations.enter", keys: ["Enter"], desc: "Expand automation / configure goal / load more runs", hint: "Expand", group: "automations", when: listOnly },
  { id: "automations.run", keys: ["x"], desc: "Run automation now", hint: "Run", group: "automations", when: listOnly },
  { id: "automations.edit", keys: ["e"], desc: "Edit automation config or run-task file", hint: "Edit", group: "automations", when: listOnly },
  { id: "automations.select", keys: ["Space"], desc: "Select run task / toggle automation on-off", hint: "Toggle", group: "automations", when: listOnly },
  { id: "automations.selectAll", keys: ["A"], desc: "Select all run tasks", group: "automations", when: listOnly },
  { id: "automations.deselect", keys: ["D"], desc: "Clear selection", group: "automations", when: listOnly },
  { id: "automations.delete", keys: ["d", "Backspace"], desc: "Delete selected run tasks", hint: "Del", group: "automations", when: listOnly },
  { id: "automations.pause", keys: ["p"], desc: "Pause/resume automations (active project scope)", hint: "Pause", group: "automations" },
  { id: "automations.refresh", keys: ["r"], desc: "Refresh automation data", group: "automations" },
  { id: "automations.new", keys: ["n"], desc: "New automation", hint: "New", group: "automations" },
  { id: "automations.filter", keys: ["/"], desc: "Filter automations and run tasks", hint: "Filter", group: "automations" },
];
