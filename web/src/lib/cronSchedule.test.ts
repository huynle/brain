/**
 * Tests for lib/cronSchedule.
 *
 * These pin this module to `pkg/cron`, not to cron folklore. The cases
 * that matter most are the two where the server is deliberately
 * non-standard (DOM ∧ DOW, and `V/S` stepping to the field max): those
 * are the ones a "reasonable" reimplementation gets wrong, and getting
 * them wrong shows up as a next-run column that is confidently incorrect
 * on exactly the expressions users find surprising.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import { describeCron, nextCronRun, parseCron } from "./cronSchedule";

/** Sorted members, for readable assertions. */
const members = (s: Set<number>) => [...s].sort((a, b) => a - b);

test("parses the standard shapes", () => {
  const s = parseCron("*/15 9-17 * * 1-5");
  assert.ok(s);
  assert.deepEqual(members(s.minute), [0, 15, 30, 45]);
  assert.deepEqual(members(s.hour), [9, 10, 11, 12, 13, 14, 15, 16, 17]);
  assert.equal(s.dayOfMonth.size, 31);
  assert.equal(s.month.size, 12);
  assert.deepEqual(members(s.dayOfWeek), [1, 2, 3, 4, 5]);
});

test("comma lists union their parts", () => {
  const s = parseCron("0,30 1,2 * * *");
  assert.ok(s);
  assert.deepEqual(members(s.minute), [0, 30]);
  assert.deepEqual(members(s.hour), [1, 2]);
});

test("a step on a bare value runs to the field maximum", () => {
  // pkg/cron's parseFieldPart: "V/S" sets rangeEnd = lim.max.
  const s = parseCron("5/15 * * * *");
  assert.ok(s);
  assert.deepEqual(members(s.minute), [5, 20, 35, 50]);
});

test("day-of-week 7 is folded onto 0", () => {
  const s = parseCron("0 0 * * 7");
  assert.ok(s);
  assert.ok(s.dayOfWeek.has(0));
});

test("rejects what the server rejects", () => {
  assert.equal(parseCron(""), null);
  assert.equal(parseCron("* * * *"), null, "4 fields");
  assert.equal(parseCron("* * * * * *"), null, "6 fields");
  assert.equal(parseCron("60 * * * *"), null, "minute out of range");
  assert.equal(parseCron("* 24 * * *"), null, "hour out of range");
  assert.equal(parseCron("* * 0 * *"), null, "day-of-month below min");
  assert.equal(parseCron("* * * 13 *"), null, "month out of range");
  assert.equal(parseCron("5-1 * * * *"), null, "reversed range");
  assert.equal(parseCron("*/0 * * * *"), null, "zero step");
  assert.equal(parseCron("abc * * * *"), null, "non-numeric");
  assert.equal(parseCron("0x10 * * * *"), null, "hex is not a cron number");
  assert.equal(parseCron("1e2 * * * *"), null, "exponent is not a cron number");
});

test("day-of-month and day-of-week are ANDed, not ORed", () => {
  // Standard Vixie cron treats this as "the 13th OR any Friday". The
  // server ANDs all five fields, so it means Friday the 13th only.
  const from = new Date("2026-01-01T00:00:00Z");
  const next = nextCronRun("0 0 13 * 5", "UTC", from);
  assert.ok(next);
  assert.equal(next.toISOString(), "2026-02-13T00:00:00.000Z");
  assert.equal(next.getUTCDay(), 5, "must be a Friday");
  assert.equal(next.getUTCDate(), 13, "must be the 13th");
});

test("next run is strictly after the given instant", () => {
  // 12:00 exactly matches "0 12 * * *"; the answer is tomorrow, not today.
  const from = new Date("2026-03-10T12:00:00Z");
  const next = nextCronRun("0 12 * * *", "UTC", from);
  assert.ok(next);
  assert.equal(next.toISOString(), "2026-03-11T12:00:00.000Z");
});

test("every-minute advances by one minute", () => {
  const from = new Date("2026-03-10T12:34:20Z");
  const next = nextCronRun("* * * * *", "UTC", from);
  assert.ok(next);
  assert.equal(next.toISOString(), "2026-03-10T12:35:00.000Z");
});

test("weekday schedule skips the weekend", () => {
  // 2026-03-07 is a Saturday; the next weekday 09:00 is Monday the 9th.
  const from = new Date("2026-03-07T10:00:00Z");
  const next = nextCronRun("0 9 * * 1-5", "UTC", from);
  assert.ok(next);
  assert.equal(next.toISOString(), "2026-03-09T09:00:00.000Z");
  assert.equal(next.getUTCDay(), 1, "Monday");
});

test("resolves the wall clock in the automation's timezone", () => {
  // 02:00 in America/Denver on a winter date is 09:00 UTC (MST, UTC-7).
  const from = new Date("2026-01-15T00:00:00Z");
  const next = nextCronRun("0 2 * * *", "America/Denver", from);
  assert.ok(next);
  assert.equal(next.toISOString(), "2026-01-15T09:00:00.000Z");
});

test("timezone shifts the answer relative to UTC", () => {
  const from = new Date("2026-01-15T00:00:00Z");
  const utc = nextCronRun("0 2 * * *", "UTC", from);
  const denver = nextCronRun("0 2 * * *", "America/Denver", from);
  assert.ok(utc && denver);
  assert.notEqual(
    utc.toISOString(),
    denver.toISOString(),
    "a zoned schedule must not collapse onto UTC",
  );
});

test("summer time uses the summer offset", () => {
  // July in Denver is MDT (UTC-6), so 02:00 local is 08:00 UTC.
  const from = new Date("2026-07-15T00:00:00Z");
  const next = nextCronRun("0 2 * * *", "America/Denver", from);
  assert.ok(next);
  assert.equal(next.toISOString(), "2026-07-15T08:00:00.000Z");
});

test("an unknown timezone falls back to UTC rather than throwing", () => {
  const from = new Date("2026-01-15T00:00:00Z");
  const next = nextCronRun("0 2 * * *", "Not/AZone", from);
  assert.ok(next);
  assert.equal(next.toISOString(), "2026-01-15T02:00:00.000Z");
});

test("a date that never occurs returns null instead of hanging", () => {
  // February 30th matches nothing, so the search must exhaust and give up.
  assert.equal(nextCronRun("0 0 30 2 *", "UTC", new Date("2026-01-01T00:00:00Z")), null);
});

test("a yearly schedule still resolves", () => {
  const from = new Date("2026-06-01T00:00:00Z");
  const next = nextCronRun("0 0 1 1 *", "UTC", from);
  assert.ok(next);
  assert.equal(next.toISOString(), "2027-01-01T00:00:00.000Z");
});

test("an invalid expression has no next run", () => {
  assert.equal(nextCronRun("nonsense", "UTC", new Date()), null);
  assert.equal(nextCronRun("", "UTC", new Date()), null);
});

test("describeCron glosses the common shapes and passes the rest through", () => {
  assert.equal(describeCron("* * * * *"), "every minute");
  assert.equal(describeCron("*/5 * * * *"), "every 5 min");
  assert.equal(describeCron("0 * * * *"), "hourly");
  assert.equal(describeCron("0 */4 * * *"), "every 4h");
  assert.equal(describeCron("0 2 * * *"), "daily 02:00");
  assert.equal(describeCron("0 9 * * 1-5"), "weekdays 09:00");
  assert.equal(describeCron("0 16 * * 5"), "Fri 16:00");
  assert.equal(describeCron("30 3 * * 0"), "Sun 03:30");
  // No gloss for a shape it does not model — show the expression itself.
  assert.equal(describeCron("0 0 13 * 5"), "0 0 13 * 5");
  assert.equal(describeCron("bogus"), "bogus");
});

test("a run inside the spring-forward gap moves to the next real occurrence", () => {
  // 2026-03-08 is the US spring-forward date: 02:00–02:59 never happens in
  // Denver, so a 02:00 daily schedule does not fire that day at all. The
  // server's own NextAfter cannot express this — time.Date normalizes the
  // missing 02:00 backward to 01:00, advanceCandidate returns the same
  // instant forever, and NextAfter exhausts its search and yields the zero
  // time. We report the truthful answer instead: the 9th.
  const from = new Date("2026-03-07T10:00:00Z");
  const next = nextCronRun("0 2 * * *", "America/Denver", from);
  assert.ok(next);
  assert.equal(next.toISOString(), "2026-03-09T08:00:00.000Z");
});

test("a run inside the fall-back repeated hour still resolves", () => {
  // 2026-11-01: 01:00 local happens twice in Denver. Either instant is a
  // defensible answer; what matters is that we return one of them rather
  // than looping or giving up.
  const from = new Date("2026-11-01T00:00:00Z");
  const next = nextCronRun("30 1 * * *", "America/Denver", from);
  assert.ok(next);
  const iso = next.toISOString();
  assert.ok(
    iso === "2026-11-01T07:30:00.000Z" || iso === "2026-11-01T08:30:00.000Z",
    `expected one of the two 01:30 instants, got ${iso}`,
  );
});

test("an impossible date is rejected by arithmetic, not by exhausting the search", () => {
  // "0 0 30 2 *" and "0 0 31 4 *" are plausible typos. Walking the full step
  // budget for them measured ~220ms per call, and taskScheduleChip runs
  // inline in TaskRow's render body on every SSE update.
  for (const [expr, tz] of [
    ["0 0 30 2 *", "UTC"],
    ["0 0 31 4 *", "America/New_York"],
    ["0 0 31 6 *", "UTC"],
    ["0 0 31 9 *", "UTC"],
    ["0 0 31 11 *", "UTC"],
  ]) {
    const t0 = process.hrtime.bigint();
    const got = nextCronRun(expr, tz, new Date("2026-09-03T18:00:00Z"));
    const ms = Number(process.hrtime.bigint() - t0) / 1e6;
    assert.equal(got, null, `${expr} matches no real date`);
    assert.ok(ms < 25, `${expr} took ${ms.toFixed(1)}ms; should be near-instant`);
  }
});

test("a rare-but-real date is NOT rejected by the pre-check", () => {
  // February 29 exists; the guard must allow it through to the search, which
  // resolves it four years out.
  const next = nextCronRun("0 0 29 2 *", "UTC", new Date("2026-09-03T18:00:00Z"));
  assert.ok(next, "Feb 29 must still resolve");
  assert.equal(next.toISOString(), "2028-02-29T00:00:00.000Z");
  // And the 31st of a 31-day month.
  const jan = nextCronRun("0 0 31 1 *", "UTC", new Date("2026-09-03T18:00:00Z"));
  assert.ok(jan);
  assert.equal(jan.toISOString(), "2027-01-31T00:00:00.000Z");
});
