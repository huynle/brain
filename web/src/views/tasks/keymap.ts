// Tasks view action table — the flagship. Static specs; handlers close over
// view state in TasksView. List-scoped actions carry when:{focus:["tasks"]}
// so pane scroll wins while detail/logs have focus.

import { listNavSpecs } from "../../lib/keymap/listNav";
import type { ActionSpec } from "../../lib/keymap/types";

const listOnly = { focus: ["tasks" as const] };

export const FEATURE_SORT_FIELDS = ["completed", "created", "name", "status", "priority"] as const;

export const TASKS_SPECS: ActionSpec[] = [
  ...listNavSpecs("tasks", "Move through tasks").map((s) => ({ ...s, when: listOnly })),
  { id: "tasks.enter", keys: ["Enter"], desc: "Open task session; on a feature header: descend into the feature (Esc backs out)", hint: "Open", group: "tasks", when: listOnly },
  { id: "tasks.select", keys: ["Space"], desc: "Select task; on a feature header: collapse/expand", hint: "Select", group: "tasks", when: listOnly },
  { id: "tasks.complete", keys: ["c"], desc: "Mark completed", hint: "Done", group: "tasks", when: listOnly },
  { id: "tasks.run", keys: ["x"], desc: "Run / dispatch task (whole feature on a header)", hint: "Run", group: "tasks", when: listOnly },
  { id: "tasks.cancel", keys: ["X"], desc: "Cancel (in-progress)", group: "tasks", when: listOnly },
  { id: "tasks.delete", keys: ["d", "Backspace"], desc: "Delete task(s)", hint: "Del", group: "tasks", when: listOnly },
  { id: "tasks.editMeta", keys: ["s"], desc: "Edit metadata (feature settings on a header)", hint: "Edit", group: "tasks", when: listOnly },
  { id: "tasks.editFile", keys: ["e"], desc: "Edit full task file", group: "tasks", when: listOnly },
  { id: "tasks.copyTitle", keys: ["y"], desc: "Copy task title", group: "tasks", when: listOnly },
  { id: "tasks.selectAll", keys: ["A"], desc: "Select all visible tasks", group: "tasks", when: listOnly },
  { id: "tasks.deselect", keys: ["D"], desc: "Clear selection", group: "tasks", when: listOnly },
  { id: "tasks.collapseAll", keys: ["{"], desc: "Collapse all feature groups", group: "tasks", when: listOnly },
  { id: "tasks.expandAll", keys: ["}"], desc: "Expand all feature groups", group: "tasks", when: listOnly },
  { id: "tasks.sortCycle", keys: ["o"], desc: "Cycle feature sort field (completed → created → name → status → priority)", hint: "Sort", group: "tasks", when: listOnly },
  { id: "tasks.sortReverse", keys: ["O"], desc: "Reverse sort direction", group: "tasks", when: listOnly },
  { id: "tasks.filter", keys: ["/"], desc: "Filter (supports status:x feature:y tag:z, status:ready)", hint: "Filter", group: "tasks" },
  { id: "tasks.mode", keys: ["C"], desc: "Toggle Tasks ⇄ Schedules", group: "tasks" },
  { id: "tasks.new", keys: ["n"], desc: "New task", hint: "New", group: "tasks" },
  { id: "tasks.toggleDetail", keys: ["T"], desc: "Toggle detail pane", group: "tasks" },
  { id: "tasks.toggleLogs", keys: ["z"], desc: "Toggle logs pane", group: "tasks" },
];
