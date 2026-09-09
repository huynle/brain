import { strict as assert } from "node:assert";
import { test } from "node:test";
import { startBackgroundOperation, useBackgroundOperations, reportBackgroundResult } from "./backgroundOperations";
import { startArchive } from "./archiveJob";
import type { BulkUpdateResponse } from "../lib/api";

const page = (updated = 0, failed = 0, truncated = false): BulkUpdateResponse => ({ dry_run: false, updated, failed, truncated, total: updated + failed, results: [] });
const reset = () => useBackgroundOperations.setState({ operations: [] });

test("background lifecycle: immediate, guarded, progress, persistent result", async () => {
  reset();
  let release!: () => void;
  const gate = new Promise<void>((resolve) => { release = resolve; });
  const done = startBackgroundOperation("Move", async (report) => {
    report({ detail: "Moved 2 of 3", completed: 2, total: 3 });
    await gate;
    reportBackgroundResult("Moved 2; 1 failed", true);
  });
  const first = useBackgroundOperations.getState().operations[0];
  assert.equal(first.state, "running");
  assert.throws(() => startBackgroundOperation("Duplicate", async () => {}), /already|Another/);
  useBackgroundOperations.getState().dismiss(first.id);
  assert.equal(useBackgroundOperations.getState().operations.length, 1);
  await Promise.resolve();
  assert.equal(useBackgroundOperations.getState().operations[0].completed, 2);
  release();
  await done;
  assert.equal(useBackgroundOperations.getState().operations[0].state, "finished");
  assert.equal(useBackgroundOperations.getState().operations[0].issue, true);
  useBackgroundOperations.getState().dismiss(first.id);
  assert.equal(useBackgroundOperations.getState().operations.length, 0);
});

test("a rejected background operation retains the failure without an unhandled rejection", async () => {
  reset();
  await startBackgroundOperation("Delete", async () => { throw new Error("Disconnected"); });
  assert.equal(useBackgroundOperations.getState().operations[0].state, "error");
  assert.match(useBackgroundOperations.getState().operations[0].detail, /Disconnected.*Earlier changes/);
});

test("archive bounds concurrency, batches all explicit paths, and drains feature pages", async () => {
  reset();
  let active = 0, peak = 0;
  const seen: string[] = [];
  const featureCalls = new Map<string, number>();
  const tick = async () => {
    active++; peak = Math.max(active, peak);
    await new Promise((resolve) => setTimeout(resolve, 1));
    active--;
  };
  await startArchive({ projectId: "p", taskPaths: Array.from({ length: 106 }, (_, i) => `task/${i}`), featureIds: ["f", "f"], total: 157 }, {
    bulkUpdateEntries: async (paths) => {
      assert.ok(paths.length <= 25);
      seen.push(...paths);
      await tick();
      return page(paths.length);
    },
    bulkUpdate: async (filter, updates) => {
      assert.equal(filter.project, "p"); assert.equal(filter.feature_id, "f");
      assert.equal(updates.status, "archived"); assert.notEqual(filter.status, "archived");
      const source = String(filter.status);
      const call = (featureCalls.get(source) ?? 0) + 1;
      featureCalls.set(source, call);
      await tick();
      return source === "completed" ? page(call < 3 ? 25 : 1, 0, call < 3) : page();
    },
  });
  assert.equal(peak, 3);
  assert.equal(new Set(seen).size, 106);
  assert.equal(featureCalls.get("completed"), 3);
  assert.equal(featureCalls.size, 4);
  const result = useBackgroundOperations.getState().operations[0];
  assert.equal(result.state, "finished");
  assert.match(result.detail, /157 archived/);
  assert.equal(result.completed, result.total);
});

test("archive reports no-progress and network errors while finishing independent groups", async () => {
  reset();
  await startArchive({ projectId: "p", taskPaths: ["bad"], featureIds: ["f"], total: 10 }, {
    bulkUpdateEntries: async () => { throw new Error("Network lost"); },
    bulkUpdate: async (filter) => filter.status === "completed" ? page(0, 1, true) : page(),
  });
  const result = useBackgroundOperations.getState().operations[0];
  assert.equal(result.state, "error");
  assert.match(result.detail, /Network lost/);
  assert.match(result.detail, /tasks remaining/);
});

test("the global tray renders accessible determinate and indeterminate progress", async () => {
  const { createElement } = await import("react");
  const { renderToStaticMarkup } = await import("react-dom/server");
  const { BackgroundOperationsView } = await import("../components/common/BackgroundOperations");
  reset();
  try {
    useBackgroundOperations.setState({ operations: [{ id: 100, label: "Archive", state: "running", detail: "25 archived", completed: 1, total: 4 }] });
    const html = renderToStaticMarkup(createElement(BackgroundOperationsView, { operations: useBackgroundOperations.getState().operations, dismiss: () => {} }));
    assert.match(html, /<progress[^>]+max="4"[^>]+value="1"/);
    assert.match(html, /Keep this tab open/);
    assert.doesNotMatch(html, /Dismiss Archive/);
    useBackgroundOperations.setState({ operations: [{ id: 100, label: "Delete", state: "running", detail: "Working…" }] });
    const unknown = renderToStaticMarkup(createElement(BackgroundOperationsView, { operations: useBackgroundOperations.getState().operations, dismiss: () => {} }));
    assert.match(unknown, /<progress/);
    assert.doesNotMatch(unknown, /value="/);
    useBackgroundOperations.setState({ operations: [{ id: 100, label: "Delete", state: "error", detail: "Network lost" }] });
    const error = renderToStaticMarkup(createElement(BackgroundOperationsView, { operations: useBackgroundOperations.getState().operations, dismiss: () => {} }));
    assert.match(error, /Needs attention.*Network lost/);
    assert.match(error, /Dismiss Delete/);
    assert.doesNotMatch(error, /<progress/);
  } finally {
    reset();
  }
});
