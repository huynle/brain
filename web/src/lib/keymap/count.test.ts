import { strict as assert } from "node:assert";
import { test } from "node:test";
import { setTimeout as sleep } from "node:timers/promises";
import { makeCountMachine } from "./count";

test("digit then countable chord flushes as count", () => {
  const replays: number[] = [];
  const m = makeCountMachine({ onReplayDigit: (d) => replays.push(d) });
  assert.ok(m.feedDigit("5"));
  assert.equal(m.resolveForChord(true), 5);
  assert.deepEqual(replays, []);
  assert.equal(m.pending(), "");
  m.dispose();
});

test("multi-digit counts extend the buffer", () => {
  const m = makeCountMachine({ onReplayDigit: () => {} });
  m.feedDigit("1");
  m.feedDigit("2");
  assert.equal(m.pending(), "12");
  assert.equal(m.resolveForChord(true), 12);
  m.dispose();
});

test("bare 0 never starts a count but extends one", () => {
  const m = makeCountMachine({ onReplayDigit: () => {} });
  assert.ok(!m.feedDigit("0"));
  m.feedDigit("1");
  assert.ok(m.feedDigit("0"));
  assert.equal(m.resolveForChord(true), 10);
  m.dispose();
});

test("non-countable chord replays a lone 1-9 digit as project jump", () => {
  const replays: number[] = [];
  const m = makeCountMachine({ onReplayDigit: (d) => replays.push(d) });
  m.feedDigit("3");
  assert.equal(m.resolveForChord(false), 1);
  assert.deepEqual(replays, [3]);
  m.dispose();
});

test("non-countable chord drops a multi-digit buffer silently", () => {
  const replays: number[] = [];
  const m = makeCountMachine({ onReplayDigit: (d) => replays.push(d) });
  m.feedDigit("1");
  m.feedDigit("2");
  assert.equal(m.resolveForChord(false), 1);
  assert.deepEqual(replays, []);
  m.dispose();
});

test("timeout replays a lone digit and clears the buffer", async () => {
  const replays: number[] = [];
  const m = makeCountMachine({ timeoutMs: 20, onReplayDigit: (d) => replays.push(d) });
  m.feedDigit("7");
  await sleep(50);
  assert.deepEqual(replays, [7]);
  assert.equal(m.pending(), "");
  // Buffer is gone: next resolve is count 1.
  assert.equal(m.resolveForChord(true), 1);
  m.dispose();
});

test("timeout drops a multi-digit buffer without replay", async () => {
  const replays: number[] = [];
  const m = makeCountMachine({ timeoutMs: 20, onReplayDigit: (d) => replays.push(d) });
  m.feedDigit("1");
  m.feedDigit("5");
  await sleep(50);
  assert.deepEqual(replays, []);
  assert.equal(m.pending(), "");
  m.dispose();
});
