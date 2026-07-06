// The anti-drift CI gate: registers the real shipped spec tables (global +
// pane + each view, one view at a time since only one is ever mounted) and
// asserts no two dispatchable specs claim the same chord with overlapping
// when-clauses. A failure here means an ambiguous binding shipped.

import { strict as assert } from "node:assert";
import { test } from "node:test";
import { findDuplicateBindings, registerScope } from "./registry";
import type { ActionHandlers, ActionSpec } from "./types";
import { GLOBAL_SPECS } from "./global";
import { PANE_SPECS } from "../usePaneNavigation";
import { TASKS_SPECS } from "../../views/tasks/keymap";
import { BRAIN_SPECS } from "../../views/brain/keymap";
import { AUTOMATIONS_SPECS } from "../../views/automations/keymap";
import { RUNNERS_SPECS, CONTROL_SPECS } from "../../views/control/keymap";
import { LOGS_SPECS } from "../../views/logs/keymap";

function noopHandlers(specs: ActionSpec[]): ActionHandlers {
  return Object.fromEntries(specs.map((s) => [s.id, () => {}]));
}

const VIEW_TABLES: [string, ActionSpec[]][] = [
  ["tasks", TASKS_SPECS],
  ["brain", BRAIN_SPECS],
  ["automations", AUTOMATIONS_SPECS],
  ["runners", RUNNERS_SPECS],
  ["control", CONTROL_SPECS],
  ["logs", LOGS_SPECS],
];

for (const [view, specs] of VIEW_TABLES) {
  test(`no ambiguous bindings with the ${view} view active`, () => {
    const unregister = [
      registerScope({ scopeId: "global", tier: "global", specs: GLOBAL_SPECS, handlers: noopHandlers(GLOBAL_SPECS) }),
      registerScope({ scopeId: "panes", tier: "pane", specs: PANE_SPECS, handlers: noopHandlers(PANE_SPECS) }),
      registerScope({ scopeId: `view:${view}`, tier: "view", specs, handlers: noopHandlers(specs) }),
    ];
    try {
      assert.deepEqual(findDuplicateBindings(), []);
    } finally {
      unregister.forEach((fn) => fn());
    }
  });
}

test("every spec id has a unique id within its table", () => {
  for (const [view, specs] of VIEW_TABLES) {
    const ids = specs.map((s) => s.id);
    assert.equal(new Set(ids).size, ids.length, `duplicate ids in ${view} table`);
  }
});
