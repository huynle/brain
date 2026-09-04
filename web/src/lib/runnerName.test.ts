import test from "node:test";
import assert from "node:assert/strict";

import { runnerLabel, runnerName } from "./runnerName";

test("runnerName reads the advertised name label", () => {
  assert.equal(
    runnerName({ runner_id: "runner_26e7c256", labels: { name: "pve-1" } }),
    "pve-1",
  );
});

test("runnerName is empty for a runner that advertises none", () => {
  assert.equal(runnerName({ runner_id: "runner_26e7c256" }), "");
  assert.equal(runnerName({ runner_id: "runner_26e7c256", labels: {} }), "");
});

// A whitespace-only label would otherwise render as a blank chip half that
// still pushes the id sideways.
test("runnerName treats a whitespace-only label as no name", () => {
  assert.equal(
    runnerName({ runner_id: "runner_26e7c256", labels: { name: "  " } }),
    "",
  );
});

test("runnerName trims a padded label", () => {
  assert.equal(
    runnerName({ runner_id: "runner_26e7c256", labels: { name: " pve-1 " } }),
    "pve-1",
  );
});

test("runnerLabel keeps the id alongside the name", () => {
  assert.equal(
    runnerLabel({ runner_id: "runner_26e7c256", labels: { name: "pve-1" } }),
    "pve-1 · runner_26e7c256",
  );
});

test("runnerLabel falls back to the id alone", () => {
  assert.equal(runnerLabel({ runner_id: "runner_352e9fd5" }), "runner_352e9fd5");
});
