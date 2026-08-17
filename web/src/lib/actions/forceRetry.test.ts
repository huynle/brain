/**
 * Tests for lib/actions/forceRetry.
 *
 * The recovery ladder has exactly four rungs worth pinning: success passes
 * through untouched, non-409 errors are not swallowed, a confirmed 409
 * retries once with force, and a declined 409 becomes a recognisable
 * cancellation rather than a generic failure.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import { ApiError } from "../api";
import {
  ForceDeclinedError,
  isLiveClaimConflict,
  withForceRetry,
} from "./forceRetry";

function conflict(message = "task is claimed by an online runner") {
  return new ApiError(409, message);
}

test("isLiveClaimConflict matches only ApiError 409", () => {
  assert.equal(isLiveClaimConflict(conflict()), true);
  assert.equal(isLiveClaimConflict(new ApiError(500, "boom")), false);
  assert.equal(isLiveClaimConflict(new Error("409")), false);
  assert.equal(isLiveClaimConflict(undefined), false);
});

test("success passes through without asking anything", async () => {
  let asked = false;
  const result = await withForceRetry(
    async (force) => `ok:${force}`,
    async () => {
      asked = true;
      return true;
    },
  );
  assert.equal(result, "ok:false");
  assert.equal(asked, false);
});

test("non-409 errors propagate untouched, no confirmation", async () => {
  let asked = false;
  await assert.rejects(
    withForceRetry(
      async () => {
        throw new ApiError(500, "boom");
      },
      async () => {
        asked = true;
        return true;
      },
    ),
    (err: unknown) => err instanceof ApiError && err.status === 500,
  );
  assert.equal(asked, false);
});

test("409 + confirmed retries exactly once with force=true", async () => {
  const attempts: boolean[] = [];
  const result = await withForceRetry(async (force) => {
    attempts.push(force);
    if (!force) throw conflict();
    return "forced";
  }, async () => true);
  assert.equal(result, "forced");
  assert.deepEqual(attempts, [false, true]);
});

test("the confirmation receives the server's actual message", async () => {
  let seen = "";
  await withForceRetry(async (force) => {
    if (!force) throw conflict("runner amos-1 holds a live claim");
    return null;
  }, async (msg) => {
    seen = msg;
    return true;
  });
  assert.equal(seen, "runner amos-1 holds a live claim");
});

test("409 + declined throws ForceDeclinedError and never retries", async () => {
  const attempts: boolean[] = [];
  await assert.rejects(
    withForceRetry(async (force) => {
      attempts.push(force);
      throw conflict();
    }, async () => false),
    (err: unknown) => err instanceof ForceDeclinedError,
  );
  assert.deepEqual(attempts, [false]);
});

test("a 409 on the forced retry propagates — some gates are force-proof", async () => {
  // Resume's live-claim safety deliberately ignores force; looping on it
  // would never terminate, so the second 409 must surface as an error.
  await assert.rejects(
    withForceRetry(async () => {
      throw conflict();
    }, async () => true),
    (err: unknown) => isLiveClaimConflict(err),
  );
});

// ─── in-band escalation (200 responses that force could change) ────
//
// /run and /resume never 409 — they answer 200 with already_leased /
// per-task skip reasons. The optional needsForce predicate turns those
// into the same confirm-then-force ladder.

test("in-band: needsForce null passes the result through, no confirmation", async () => {
  let asked = false;
  const result = await withForceRetry(
    async (force) => ({ ok: true, force }),
    async () => {
      asked = true;
      return true;
    },
    () => null,
  );
  assert.deepEqual(result, { ok: true, force: false });
  assert.equal(asked, false);
});

test("in-band: refusal message + confirmed retries once with force=true", async () => {
  const attempts: boolean[] = [];
  let seen = "";
  const result = await withForceRetry(
    async (force) => {
      attempts.push(force);
      return { leased: !force };
    },
    async (msg) => {
      seen = msg;
      return true;
    },
    (r) => (r.leased ? "task already holds a dispatch lease" : null),
  );
  assert.deepEqual(result, { leased: false });
  assert.deepEqual(attempts, [false, true]);
  assert.equal(seen, "task already holds a dispatch lease");
});

test("in-band: declined returns the ORIGINAL result instead of throwing", async () => {
  // Unlike a declined 409, the server already completed this request —
  // the caller's toast should summarize what actually happened.
  const attempts: boolean[] = [];
  const result = await withForceRetry(
    async (force) => {
      attempts.push(force);
      return { skipped: 3 };
    },
    async () => false,
    () => "3 task(s) were skipped",
  );
  assert.deepEqual(result, { skipped: 3 });
  assert.deepEqual(attempts, [false]);
});

test("in-band predicate is not consulted on the forced result — one retry max", async () => {
  let calls = 0;
  const result = await withForceRetry(
    async () => "still-refused",
    async () => true,
    () => {
      calls++;
      return "refused";
    },
  );
  assert.equal(result, "still-refused");
  assert.equal(calls, 1);
});
