/**
 * Tests for lib/actions/bulkBaton.
 *
 * The loop's contract: aggregate every page, stop when the server stops
 * reporting truncation, and refuse to spin — the iteration cap and the
 * no-progress backstop are the two ways out of a loop that isn't
 * converging, and both must mark the outcome as stopped.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  BULK_BATON_MAX_ITERATIONS,
  runBulkBaton,
  summarizeBatonOutcome,
  type BulkPage,
} from "./bulkBaton";

interface FakePage extends BulkPage {
  deleted: number;
}

/** Build a runPage stub that serves the given pages in order. */
function pages(...list: FakePage[]) {
  let i = 0;
  const calls: number[] = [];
  const run = async (iteration: number) => {
    calls.push(iteration);
    const page = list[Math.min(i, list.length - 1)];
    i++;
    return page;
  };
  return { run, calls };
}

const okOf = (p: FakePage) => p.deleted;

test("a single non-truncated page finishes in one iteration", async () => {
  const { run } = pages({ deleted: 7, failed: 0, total: 7 });
  const out = await runBulkBaton(run, okOf);
  assert.deepEqual(out, {
    ok: 7,
    failed: 0,
    total: 7,
    iterations: 1,
    stopped: false,
  });
});

test("truncated pages repeat until the server reports done, aggregating", async () => {
  const { run, calls } = pages(
    { deleted: 100, failed: 0, total: 100, truncated: true, matched_total: 230 },
    { deleted: 100, failed: 0, total: 100, truncated: true, matched_total: 130 },
    { deleted: 30, failed: 0, total: 30 },
  );
  const out = await runBulkBaton(run, okOf);
  assert.equal(out.ok, 230);
  assert.equal(out.iterations, 3);
  assert.equal(out.stopped, false);
  assert.deepEqual(calls, [0, 1, 2]);
});

test("partial failures aggregate across pages", async () => {
  const { run } = pages(
    { deleted: 98, failed: 2, total: 100, truncated: true, matched_total: 150 },
    { deleted: 50, failed: 2, total: 52 },
  );
  const out = await runBulkBaton(run, okOf);
  assert.equal(out.ok, 148);
  assert.equal(out.failed, 4);
  assert.equal(out.total, 152);
});

test("the iteration cap stops a loop that will not converge", async () => {
  const { run, calls } = pages({
    deleted: 100,
    failed: 0,
    total: 100,
    truncated: true,
    matched_total: 999999,
  });
  const out = await runBulkBaton(run, okOf, { maxIterations: 4 });
  assert.equal(out.iterations, 4);
  assert.equal(out.stopped, true);
  assert.equal(calls.length, 4);
});

test("default cap is 50", () => {
  assert.equal(BULK_BATON_MAX_ITERATIONS, 50);
});

test("a truncated page with zero successes stops immediately (no-progress backstop)", async () => {
  // If nothing succeeded, the match set did not shrink; repeating the call
  // would return the same page until the cap. One page is enough to know.
  const { run, calls } = pages({
    deleted: 0,
    failed: 100,
    total: 100,
    truncated: true,
    matched_total: 300,
  });
  const out = await runBulkBaton(run, okOf);
  assert.equal(out.iterations, 1);
  assert.equal(out.stopped, true);
  assert.equal(out.failed, 100);
  assert.equal(calls.length, 1);
});

test("onProgress reports each page with running totals", async () => {
  const { run } = pages(
    { deleted: 100, failed: 0, total: 100, truncated: true, matched_total: 130 },
    { deleted: 30, failed: 0, total: 30 },
  );
  const seen: Array<{ processed: number; matched: number; iteration: number }> = [];
  await runBulkBaton(run, okOf, { onProgress: (p) => void seen.push(p) });
  assert.deepEqual(seen, [
    { processed: 100, matched: 130, iteration: 1 },
    { processed: 130, matched: 130, iteration: 2 },
  ]);
});

// ─── summaries ─────────────────────────────────────────────────────

test("summary: clean completion", () => {
  const r = summarizeBatonOutcome(
    { ok: 230, failed: 0, total: 230, iterations: 3, stopped: false },
    "deleted",
  );
  assert.equal(r.kind, "success");
  assert.match(r.message, /230 tasks deleted/);
});

test("summary: a stopped baton says work remains", () => {
  const r = summarizeBatonOutcome(
    { ok: 500, failed: 0, total: 500, iterations: 50, stopped: true },
    "updated",
  );
  assert.equal(r.kind, "warning");
  assert.match(r.message, /more remain/i);
  assert.match(r.message, /run again/i);
});

test("summary: total failure is an error", () => {
  const r = summarizeBatonOutcome(
    { ok: 0, failed: 5, total: 5, iterations: 1, stopped: false },
    "deleted",
  );
  assert.equal(r.kind, "error");
});

test("summary: partial failure names both counts", () => {
  const r = summarizeBatonOutcome(
    { ok: 7, failed: 2, total: 9, iterations: 1, stopped: false },
    "updated",
  );
  assert.equal(r.kind, "warning");
  assert.match(r.message, /7 of 9/);
  assert.match(r.message, /2 failed/);
});

test("summary: zero matches is a warning, not a success", () => {
  const r = summarizeBatonOutcome(
    { ok: 0, failed: 0, total: 0, iterations: 1, stopped: false },
    "deleted",
  );
  assert.equal(r.kind, "warning");
  assert.match(r.message, /nothing matched/i);
});
