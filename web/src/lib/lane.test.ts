import test from "node:test";
import assert from "node:assert/strict";
import { LANE_COLLAPSED_CAP, laneVisible } from "./lane";

test("laneVisible: under cap shows all with nothing hidden", () => {
  const items = ["a", "b"];
  const { visible, hiddenCount } = laneVisible(items, false);
  assert.deepEqual(visible, ["a", "b"]);
  assert.equal(hiddenCount, 0);
});

test("laneVisible: exactly at cap shows all with nothing hidden", () => {
  const items = ["a", "b", "c", "d"];
  assert.equal(items.length, LANE_COLLAPSED_CAP);
  const { visible, hiddenCount } = laneVisible(items, false);
  assert.deepEqual(visible, items);
  assert.equal(hiddenCount, 0);
});

test("laneVisible: over cap collapsed shows first cap items and counts the rest", () => {
  const items = ["a", "b", "c", "d", "e", "f"];
  const { visible, hiddenCount } = laneVisible(items, false);
  assert.deepEqual(visible, ["a", "b", "c", "d"]);
  assert.equal(hiddenCount, 2);
});

test("laneVisible: over cap expanded shows everything", () => {
  const items = ["a", "b", "c", "d", "e", "f"];
  const { visible, hiddenCount } = laneVisible(items, true);
  assert.deepEqual(visible, items);
  assert.equal(hiddenCount, 0);
});
