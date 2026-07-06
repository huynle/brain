import { strict as assert } from "node:assert";
import { test } from "node:test";
import {
  deriveTaskPaused,
  deriveAutomationsPaused,
} from "./StatusBar";
import type { RunnerStatusResponse } from "../../lib/types";
import { ALL_PROJECTS } from "../../store/ui";

// deriveTaskPaused / deriveAutomationsPaused convert the runner-status
// snapshot into the boolean the StatusChip needs. The previous inline
// logic used `data?.pausedProjects?.includes(active)` which returned
// `undefined` (rendered as faint "…") when the API serialized an empty
// per-project list as JSON `null` rather than `[]`. The chip then read
// "empty list" as "unknown", surfacing `autos: …` even though the runtime
// behavior was clearly "not paused for this project". An empty/null list
// for a specific project must mean `false`, not `undefined`.

const PROJECT = "personal-productivity";

test("deriveTaskPaused: undefined when status data not yet loaded", () => {
  assert.equal(deriveTaskPaused(undefined, ALL_PROJECTS), undefined);
  assert.equal(deriveTaskPaused(undefined, PROJECT), undefined);
});

test("deriveTaskPaused: ALL_PROJECTS reads global flag", () => {
  const data = mkStatus({ paused: true });
  assert.equal(deriveTaskPaused(data, ALL_PROJECTS), true);
  const data2 = mkStatus({ paused: false });
  assert.equal(deriveTaskPaused(data2, ALL_PROJECTS), false);
});

test("deriveTaskPaused: per-project returns true when project in list", () => {
  const data = mkStatus({ pausedProjects: [PROJECT, "other-project"] });
  assert.equal(deriveTaskPaused(data, PROJECT), true);
});

test("deriveTaskPaused: per-project returns false when project not in list", () => {
  const data = mkStatus({ pausedProjects: ["other-project"] });
  assert.equal(deriveTaskPaused(data, PROJECT), false);
});

test("deriveTaskPaused: per-project returns false for empty list (not undefined)", () => {
  const data = mkStatus({ pausedProjects: [] });
  assert.equal(deriveTaskPaused(data, PROJECT), false);
});

test("deriveTaskPaused: per-project returns false when API serializes null (not undefined)", () => {
  // This is the actual production shape — the Go API emits null for nil slices.
  const data = mkStatus({ pausedProjects: null as unknown as string[] });
  assert.equal(deriveTaskPaused(data, PROJECT), false);
});

test("deriveAutomationsPaused: undefined when status data not yet loaded", () => {
  assert.equal(deriveAutomationsPaused(undefined, ALL_PROJECTS), undefined);
  assert.equal(deriveAutomationsPaused(undefined, PROJECT), undefined);
});

test("deriveAutomationsPaused: ALL_PROJECTS reads global flag", () => {
  const data = mkStatus({ automationsPaused: true });
  assert.equal(deriveAutomationsPaused(data, ALL_PROJECTS), true);
});

test("deriveAutomationsPaused: per-project returns true when project in list", () => {
  const data = mkStatus({ automationPausedProjects: [PROJECT] });
  assert.equal(deriveAutomationsPaused(data, PROJECT), true);
});

test("deriveAutomationsPaused: per-project returns false when project not in list", () => {
  const data = mkStatus({ automationPausedProjects: ["other-project"] });
  assert.equal(deriveAutomationsPaused(data, PROJECT), false);
});

test("deriveAutomationsPaused: per-project returns false for empty list", () => {
  const data = mkStatus({ automationPausedProjects: [] });
  assert.equal(deriveAutomationsPaused(data, PROJECT), false);
});

test("deriveAutomationsPaused: per-project returns false when API serializes null", () => {
  // The regression case — production API returns
  //   "automationPausedProjects": null
  // and the previous `?.includes(activeProject)` chain short-circuited to
  // `undefined`, causing the badge to render `autos: …` even when autos
  // were running for that project.
  const data = mkStatus({
    automationPausedProjects: null as unknown as string[],
  });
  assert.equal(deriveAutomationsPaused(data, PROJECT), false);
});

function mkStatus(overrides: Partial<RunnerStatusResponse>): RunnerStatusResponse {
  return {
    running: true,
    paused: false,
    pausedProjects: [],
    automationsPaused: false,
    automationPausedProjects: [],
    ...overrides,
  };
}
