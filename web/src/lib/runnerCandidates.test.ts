import assert from "node:assert/strict";
import test from "node:test";
import type { RunnerCandidate, RunnerInfo } from "./types";
import { assignmentRunnerIDs } from "./runnerCandidates";

const runner = (runner_id: string, status: RunnerInfo["status"] = "online"): RunnerInfo => ({
  runner_id,
  hostname: runner_id,
  max_parallel: 1,
  registered_at: "",
  last_heartbeat: "",
  status,
});

test("assignmentRunnerIDs offers every durably compatible runner", () => {
  const candidates: RunnerCandidate[] = [
    { runner: runner("local"), compatible: true, available: true, reasons: [] },
    { runner: runner("brain-conversation-worker"), compatible: false, available: true, reasons: [{ code: "project_not_allowed", message: "wrong project" }] },
    { runner: runner("offline", "offline"), compatible: true, available: false, reasons: [] },
  ];
  assert.deepEqual(assignmentRunnerIDs(candidates), ["local", "offline"]);
});

test("assignmentRunnerIDs preserves the current runner so an invalid pin remains visible", () => {
  const candidates: RunnerCandidate[] = [
    { runner: runner("current", "offline"), compatible: false, available: false, reasons: [{ code: "missing_task_capability", message: "missing gpu" }] },
    { runner: runner("better"), compatible: true, available: true, reasons: [] },
  ];
  assert.deepEqual(assignmentRunnerIDs(candidates, "current"), ["current", "better"]);
});
