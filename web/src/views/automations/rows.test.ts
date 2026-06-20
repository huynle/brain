import { strict as assert } from "node:assert";
import { test } from "node:test";
import type { BrainEntry } from "../../lib/types";
import {
  AUTOMATION_RUN_TASK_PAGE_SIZE,
  type AutomationRow,
  childRunTasks,
  flattenAutomationDisplay,
  normalizeAutomationRows,
} from "./rows";

function row(id: string): AutomationRow {
  return {
    id,
    path: `${id}.md`,
    title: id,
    source: "automation",
    scope: "project",
    status: "active",
    enabled: true,
    isGoal: false,
    featureId: "",
    triggerKind: "event",
    triggerDetail: "task.completed",
    runSummary: "",
    runTaskID: "",
  };
}

function runTask(id: string, automationID = "auto1", modified = id): BrainEntry {
  return {
    id,
    path: `${id}.md`,
    title: id,
    type: "task",
    status: "completed",
    generated_by: `automation:${automationID}`,
    modified,
  } as BrainEntry;
}

test("childRunTasks returns automation children newest first", () => {
  const tasks = [
    runTask("older", "auto1", "2026-06-01T00:00:00Z"),
    runTask("newer", "auto1", "2026-06-02T00:00:00Z"),
    runTask("other", "auto2", "2026-06-03T00:00:00Z"),
  ];

  assert.deepEqual(childRunTasks("auto1", tasks).map((task) => task.id), ["newer", "older"]);
});

test("flattenAutomationDisplay pages expanded run tasks and adds show-more row", () => {
  const rows = [row("auto1")];
  const tasks = Array.from({ length: 25 }, (_, i) =>
    runTask(`task${String(i).padStart(2, "0")}`, "auto1", `2026-06-${String(i + 1).padStart(2, "0")}T00:00:00Z`),
  );

  const display = flattenAutomationDisplay(rows, tasks, "auto1", {});

  assert.equal(display.length, AUTOMATION_RUN_TASK_PAGE_SIZE + 2);
  assert.equal(display[0].kind, "auto");
  assert.deepEqual(
    display.slice(1, 11).map((entry) => entry.kind === "task" ? entry.task.id : "not-task"),
    ["task24", "task23", "task22", "task21", "task20", "task19", "task18", "task17", "task16", "task15"],
  );
  assert.deepEqual(display.at(-1), {
    kind: "show-more",
    parent: rows[0],
    shown: 10,
    total: 25,
    remaining: 15,
  });
});

test("flattenAutomationDisplay uses visible limit and omits show-more when all tasks are visible", () => {
  const rows = [row("auto1")];
  const tasks = Array.from({ length: 15 }, (_, i) => runTask(`task${String(i).padStart(2, "0")}`));

  const display = flattenAutomationDisplay(rows, tasks, "auto1", { auto1: 20 });

  assert.equal(display.length, 16);
  assert.equal(display.some((entry) => entry.kind === "show-more"), false);
});


test("normalizeAutomationRows treats global built-ins as disabled templates", () => {
  const rows = normalizeAutomationRows([
    {
      id: "dream-consolidation",
      path: "global/automation/dream-consolidation.md",
      title: "Dream Consolidation",
      type: "automation",
      status: "active",
    } as BrainEntry,
  ], [], []);

  assert.equal(rows.length, 1);
  assert.equal(rows[0].scope, "built-in");
  assert.equal(rows[0].status, "archived");
  assert.equal(rows[0].enabled, false);
});

test("normalizeAutomationRows prefers project copy over global built-in template", () => {
  const rows = normalizeAutomationRows([
    {
      id: "dream-global",
      path: "global/automation/dream-consolidation.md",
      title: "Dream Consolidation",
      type: "automation",
      status: "active",
    } as BrainEntry,
    {
      id: "dream-project",
      path: "projects/brain/automation/dream-consolidation.md",
      title: "Dream Consolidation",
      type: "automation",
      status: "archived",
      project_id: "brain",
    } as BrainEntry,
  ], [], []);

  assert.equal(rows.length, 1);
  assert.equal(rows[0].id, "dream-project");
  assert.equal(rows[0].scope, "project");
  assert.equal(rows[0].enabled, false);
});
