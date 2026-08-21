import test from "node:test";
import assert from "node:assert/strict";
import { clockTime } from "./format";

/*
 * The log grids give the timestamp a fixed 54px track (46px on mobile).
 * `toLocaleTimeString()` renders "12:04:31 PM" under a 12-hour locale —
 * wider than the track, painting over the level column beside it. This
 * formatter is 24-hour and fixed-width in every locale.
 */
test("clockTime: 24-hour, zero-padded, no meridiem", () => {
  // Local-time construction, so the assertion holds in any TZ.
  const afternoon = new Date(2026, 7, 20, 12, 4, 31);
  assert.equal(clockTime(afternoon.toISOString()), "12:04:31");
  assert.equal(clockTime(afternoon.getTime()), "12:04:31");
  assert.equal(clockTime(new Date(2026, 7, 20, 23, 59, 5).toISOString()), "23:59:05");
  assert.equal(clockTime(new Date(2026, 7, 20, 0, 0, 0).toISOString()), "00:00:00");
});

test("clockTime: always exactly 8 characters wide", () => {
  for (const h of [0, 9, 13, 23]) {
    assert.equal(clockTime(new Date(2026, 7, 20, h, 7, 3).toISOString()).length, 8);
  }
});

test("clockTime: missing or unparseable input is empty, never NaN", () => {
  assert.equal(clockTime(undefined), "");
  assert.equal(clockTime(""), "");
  assert.equal(clockTime("not a date"), "");
});
