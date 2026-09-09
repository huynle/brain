import { strict as assert } from "node:assert";
import { test } from "node:test";
import { startBackgroundOperation, useBackgroundOperations, reportBackgroundResult } from "./backgroundOperations";
import { startArchive } from "./archiveJob";

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

test("archive submits one deduplicated immutable selection to the server", async () => {
  let calls = 0;
  await startArchive({ projectId: "p", taskPaths: ["a", "a", "b"], featureIds: ["f", "f"], total: 106 }, async (request) => {
    calls++;
    assert.equal(request.operation, "archive");
    assert.deepEqual(request.paths, ["a", "b"]);
    assert.equal(request.filters?.length, 4);
    assert.ok(request.filters?.every(f => f.project === "p" && f.feature_id === "f" && f.status !== "archived"));
  });
  assert.equal(calls, 1);
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
