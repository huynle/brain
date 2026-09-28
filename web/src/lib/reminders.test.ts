/** Pure reminder lifecycle, scope, and filter tests (node --test, no DOM). */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  PROJECT_FILTER_ALL,
  PROJECT_FILTER_GLOBAL,
  PROJECT_FILTER_SIDEBAR,
  filterReminders,
  groupReminders,
  reminderCounts,
  resolveReminderScope,
  type ReminderFilter,
} from "./reminders";
import type { ReminderSummary, ReminderState } from "./types";

function reminder(
  id: string,
  state: ReminderState,
  over: Partial<ReminderSummary> = {},
): ReminderSummary {
  return {
    entry_id: `entry-${id}`,
    reminder_id: id,
    title: `Reminder ${id}`,
    status: "active",
    state,
    action: "notify",
    ...over,
  };
}

const sample = [
  reminder("waiting", "fired", { project: "alpha" }),
  reminder("scheduled", "armed", { project: "beta" }),
  reminder("stopped", "paused", { project: "alpha" }),
  reminder("someday", "undated"),
  reminder("executed", "done", { project: "gamma" }),
  reminder("agent", "done", {
    project: "alpha",
    action: "task",
    generated_task_id: "task-123",
  }),
  reminder("task-no-run", "armed", { action: "task", project: "beta" }),
];

test("grouping: lifecycle states stay distinct and agent runs are cross-cutting", () => {
  const groups = groupReminders(sample);

  assert.deepEqual(groups.waiting.map((r) => r.reminder_id), ["waiting"]);
  assert.deepEqual(groups.scheduled.map((r) => r.reminder_id), [
    "scheduled",
    "task-no-run",
  ]);
  assert.deepEqual(groups.stopped.map((r) => r.reminder_id), ["stopped"]);
  assert.deepEqual(groups.someday.map((r) => r.reminder_id), ["someday"]);
  assert.deepEqual(groups.executed.map((r) => r.reminder_id), [
    "executed",
    "agent",
  ]);
  assert.deepEqual(groups.agent.map((r) => r.reminder_id), ["agent"]);
});

test("counts: All counts unique reminders while Agent runs remains cross-cutting", () => {
  assert.deepEqual(reminderCounts(sample), {
    all: 7,
    waiting: 1,
    scheduled: 2,
    stopped: 1,
    someday: 1,
    executed: 2,
    agent: 1,
  });
});

test("scope: sidebar projects include global and loading/unfiltered widens to all", () => {
  assert.deepEqual(
    resolveReminderScope(PROJECT_FILTER_SIDEBAR, {
      projects: ["alpha", "beta"],
      unfiltered: false,
      loading: false,
    }),
    { kind: "set", projects: ["alpha", "beta", "global"] },
  );
  assert.deepEqual(
    resolveReminderScope(PROJECT_FILTER_SIDEBAR, {
      projects: [],
      unfiltered: false,
      loading: true,
    }),
    { kind: "all" },
  );
  assert.deepEqual(
    resolveReminderScope(PROJECT_FILTER_SIDEBAR, {
      projects: ["alpha", "beta"],
      unfiltered: true,
      loading: false,
    }),
    { kind: "all" },
  );
});

test("scope: all, global, and explicit project override the sidebar", () => {
  const sidebar = { projects: ["alpha"], unfiltered: false, loading: false };
  assert.deepEqual(resolveReminderScope(PROJECT_FILTER_ALL, sidebar), {
    kind: "all",
  });
  assert.deepEqual(resolveReminderScope(PROJECT_FILTER_GLOBAL, sidebar), {
    kind: "global",
  });
  assert.deepEqual(resolveReminderScope("beta", sidebar), {
    kind: "project",
    project: "beta",
  });
});

function apply(filter: Partial<ReminderFilter>) {
  return filterReminders(sample, {
    lifecycle: "all",
    projectScope: { kind: "all" },
    query: "",
    ...filter,
  }).map((r) => r.reminder_id);
}

test("filters: lifecycle includes exact states and only completed agent tasks", () => {
  assert.deepEqual(apply({ lifecycle: "stopped" }), ["stopped"]);
  assert.deepEqual(apply({ lifecycle: "executed" }), ["executed", "agent"]);
  assert.deepEqual(apply({ lifecycle: "agent" }), ["agent"]);
});

test("filters: project scope treats absent project as global", () => {
  assert.deepEqual(apply({ projectScope: { kind: "global" } }), ["someday"]);
  assert.deepEqual(apply({ projectScope: { kind: "project", project: "beta" } }), [
    "scheduled",
    "task-no-run",
  ]);
  assert.deepEqual(
    apply({ projectScope: { kind: "set", projects: ["alpha", "global"] } }),
    ["waiting", "stopped", "someday", "agent"],
  );
});

test("filters: search matches title or project case-insensitively and composes", () => {
  const rows = [
    reminder("release", "armed", { title: "Ship Release", project: "alpha" }),
    reminder("ops", "armed", { title: "Review logs", project: "Platform-Ops" }),
    reminder("done", "done", { title: "Release notes", project: "alpha" }),
  ];
  const result = filterReminders(rows, {
    lifecycle: "scheduled",
    projectScope: { kind: "all" },
    query: "RELEASE",
  });
  assert.deepEqual(result.map((r) => r.reminder_id), ["release"]);
  assert.deepEqual(
    filterReminders(rows, {
      lifecycle: "all",
      projectScope: { kind: "all" },
      query: "platform-ops",
    }).map((r) => r.reminder_id),
    ["ops"],
  );
});
