/**
 * Tests for lib/automationRunTarget — what a run row can open, and what
 * it says when it can't.
 *
 * These four outcomes are the feature: the whole point of the run list
 * is reaching the session an automation's agent ran in, and three of the
 * four paths end without one for reasons the user must be able to tell
 * apart.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import { resolveRunTarget } from "./automationRunTarget";
import { parseAutomationRun } from "./automationRuns";
import type { BrainEntry, OpencodeInstance, Task } from "./types";

function mkRun(taskLine: string): ReturnType<typeof parseAutomationRun> {
  return parseAutomationRun({
    id: "run1",
    path: "projects/demo/automation_run/run1.md",
    title: "Automation Run: a1",
    type: "automation_run",
    status: "queued",
    created: "2026-08-21T10:49:19Z",
    content: [
      "## Automation Run Audit",
      "",
      "automation_id: a1",
      "trigger_type: cron",
      "skip_reason: " + (taskLine === "- none" ? "cooldown" : ""),
      "",
      "### Generated Tasks",
      taskLine,
    ].join("\n"),
  } as BrainEntry);
}

function mkTask(over: Partial<Task> = {}): Task {
  return {
    id: "t1",
    path: "",
    title: "Automation: a1",
    priority: "medium",
    status: "completed",
    projectId: "demo",
    ...over,
  } as Task;
}

const NO_INSTANCES: OpencodeInstance[] = [];

test("a run that generated nothing says so, and names the reason", () => {
  const t = resolveRunTarget(mkRun("- none"), new Map(), NO_INSTANCES, true);
  assert.equal(t.task, undefined);
  assert.equal(t.sref, undefined);
  assert.match(t.reason ?? "", /started no task/);
  assert.match(t.reason ?? "", /cooldown/);
});

test("a task missing from a LOADED snapshot reads as deleted", () => {
  const t = resolveRunTarget(mkRun("- t1"), new Map(), NO_INSTANCES, true);
  assert.match(t.reason ?? "", /no longer in this project's task list/);
});

test("a task missing from an UNLOADED snapshot reads as pending, not deleted", () => {
  const t = resolveRunTarget(mkRun("- t1"), new Map(), NO_INSTANCES, false);
  assert.match(t.reason ?? "", /Waiting for this project's task snapshot/);
});

test("a task with no recorded session offers the log, not a dead button", () => {
  const task = mkTask();
  const t = resolveRunTarget(
    mkRun("- t1"),
    new Map([["t1", task]]),
    NO_INSTANCES,
    true,
  );
  assert.equal(t.task, task);
  assert.equal(t.sref, undefined);
  assert.match(t.reason ?? "", /script-executor runs never open one/);
});

test("a recorded session resolves to a history ref on its own runner", () => {
  const task = mkTask({
    sessions: {
      ses_old: { runner_id: "r1", timestamp: "2026-08-01T00:00:00Z" },
      ses_new: { runner_id: "r2", timestamp: "2026-08-21T00:00:00Z" },
    },
  });
  const t = resolveRunTarget(
    mkRun("- t1"),
    new Map([["t1", task]]),
    NO_INSTANCES,
    true,
  );
  assert.equal(t.reason, undefined);
  assert.equal(t.sref?.mode, "history");
  // Newest wins, and it carries the runner that actually holds it —
  // sessions span runners across retries.
  assert.equal(t.sref?.session_id, "ses_new");
  assert.equal(t.sref?.runner_id, "r2");
});

test("a live instance for the task outranks the recorded transcript", () => {
  const task = mkTask({
    status: "in_progress",
    sessions: { ses_old: { runner_id: "r1", timestamp: "2026-08-01T00:00:00Z" } },
  });
  const live: OpencodeInstance[] = [
    {
      instance_id: "i1",
      runner_id: "r9",
      kind: "task",
      task_id: "t1",
      project_id: "demo",
      status: "busy",
      session_ids: ["ses_live"],
    } as OpencodeInstance,
  ];
  const t = resolveRunTarget(mkRun("- t1"), new Map([["t1", task]]), live, true);
  assert.equal(t.sref?.mode, "live");
  assert.equal(t.sref?.instance_id, "i1");
  assert.equal(t.sref?.session_id, "ses_live");
});
