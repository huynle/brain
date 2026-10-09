/**
 * Tests for lib/cronSchedule.
 *
 * These pin this module to `pkg/cron`, not to cron folklore. The cases
 * that matter most are the ones a "reasonable" reimplementation gets
 * wrong: the Vixie day-field rule (OR only when NEITHER day field starts
 * with a star — so a stepped star still ANDs), `V/S` stepping to the
 * field max, and DST (a skipped wall time never fires, a repeated one
 * fires once, at its first occurrence). Getting them wrong shows up as a
 * next-run column that is confidently incorrect on exactly the
 * expressions users find surprising.
 *
 * SHARED VECTORS: every case marked "pkg/cron <file>:<test>" is copied
 * verbatim (expression, instant, expected instant) from the Go tests, so
 * the two implementations are held to the same answers. Change one side
 * and the other must change with it.
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

/**
 * pkg/cron's Matches(at), expressed through nextCronRun: a schedule fires at
 * `at` exactly when the next run strictly after the preceding minute is `at`.
 */
function firesAt(expr: string, iso: string, timeZone = "UTC"): boolean {
  const at = new Date(iso);
  const next = nextCronRun(expr, timeZone, new Date(at.getTime() - 60_000));
  return next !== null && next.getTime() === at.getTime();
}

// Reference dates (2026, UTC), as in pkg/cron/days_test.go:
//   Sun 03-22, Mon 03-23, Tue 03-24, Wed 03-25, Mon 03-30, Wed 04-01,
//   Mon 04-13, Mon 06-01.

test("both day fields restricted: a day fires if EITHER matches (Vixie OR)", () => {
  // pkg/cron days_test.go:TestMatches_DayFields_BothRestricted_IsOR
  const cases: [string, string, boolean][] = [
    ["0 3 1 * 1", "2026-03-23T03:00:00Z", true], // Monday, not the 1st
    ["0 3 1 * 1", "2026-04-01T03:00:00Z", true], // the 1st, a Wednesday
    ["0 3 1 * 1", "2026-06-01T03:00:00Z", true], // both
    ["0 3 1 * 1", "2026-03-24T03:00:00Z", false], // neither
    ["0 3 1-7 * 1", "2026-03-23T03:00:00Z", true],
    ["0 3 15 * 7", "2026-03-22T03:00:00Z", true], // 7 = Sunday, via DOW
    ["0 3 15 * 7", "2026-03-15T03:00:00Z", true], // via DOM
    ["0 3 15 * 7", "2026-03-16T03:00:00Z", false],
  ];
  for (const [expr, iso, want] of cases) {
    assert.equal(firesAt(expr, iso), want, `${expr} at ${iso}`);
  }
});

test("a day field starting with * keeps AND, even when stepped", () => {
  // pkg/cron days_test.go:TestMatches_DayFields_StarPrefixed_IsAND
  const cases: [string, string, boolean][] = [
    ["0 3 */2 * 1", "2026-03-23T03:00:00Z", true], // odd day AND Monday
    ["0 3 */2 * 1", "2026-03-30T03:00:00Z", false], // Monday, even day
    ["0 3 */2 * 1", "2026-03-25T03:00:00Z", false], // odd day, Wednesday
    ["0 3 1 * */1", "2026-04-01T03:00:00Z", true], // "*/1" DOW is starred
    ["0 3 1 * */1", "2026-03-23T03:00:00Z", false], // starred DOW → DOM decides
    ["0 3 * * 1", "2026-03-23T03:00:00Z", true],
    ["0 3 * * 1", "2026-04-01T03:00:00Z", false],
    ["0 3 1 * *", "2026-04-01T03:00:00Z", true],
    ["0 3 1 * *", "2026-03-23T03:00:00Z", false],
  ];
  for (const [expr, iso, want] of cases) {
    assert.equal(firesAt(expr, iso), want, `${expr} at ${iso}`);
  }
});

test("next run under the OR rule: the 1st and every Monday", () => {
  // pkg/cron days_test.go:TestNextAfter_DayFields_BothRestricted_IsOR
  // From Tuesday the 24th, Monday the 30th comes before the 1st.
  assert.equal(
    nextCronRun("0 3 1 * 1", "UTC", new Date("2026-03-24T03:00:00Z"))?.toISOString(),
    "2026-03-30T03:00:00.000Z",
  );
  // From Monday the 30th after its run, the 1st (a Wednesday) is next.
  // Under AND this would jump to Monday 2026-06-01.
  assert.equal(
    nextCronRun("0 3 1 * 1", "UTC", new Date("2026-03-30T03:00:00Z"))?.toISOString(),
    "2026-04-01T03:00:00.000Z",
  );
});

test("next run with a stepped-star day-of-month stays AND", () => {
  // pkg/cron days_test.go:TestNextAfter_DayFields_StarPrefixed_IsAND
  // Mondays 03-30 and 04-06 are even days; 04-13 is the first odd Monday.
  assert.equal(
    nextCronRun("0 3 */2 * 1", "UTC", new Date("2026-03-24T03:00:00Z"))?.toISOString(),
    "2026-04-13T03:00:00.000Z",
  );
});

test("single-restricted day fields are unchanged", () => {
  // Same reference week; the rule change must not touch these shapes.
  assert.equal(
    nextCronRun("0 3 * * 1", "UTC", new Date("2026-03-24T03:00:00Z"))?.toISOString(),
    "2026-03-30T03:00:00.000Z",
  );
  assert.equal(
    nextCronRun("0 3 15 * *", "UTC", new Date("2026-03-24T03:00:00Z"))?.toISOString(),
    "2026-04-15T03:00:00.000Z",
  );
});

test("an impossible date ORed with a weekday still fires on the weekday", () => {
  // February 30th never exists, but under the OR rule every Monday in
  // February qualifies — the impossible-date pre-check must not reject it.
  // 2027-02-01 is the first February Monday after the reference instant.
  const next = nextCronRun("0 0 30 2 1", "UTC", new Date("2026-09-03T18:00:00Z"));
  assert.ok(next, "a February Monday must resolve");
  assert.equal(next.toISOString(), "2027-02-01T00:00:00.000Z");
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
  // server agrees (pkg/cron search.go resolve: a nonexistent wall time does
  // not fire), so the next run is the 9th.
  const from = new Date("2026-03-07T10:00:00Z");
  const next = nextCronRun("0 2 * * *", "America/Denver", from);
  assert.ok(next);
  assert.equal(next.toISOString(), "2026-03-09T08:00:00.000Z");
});

test("a nonexistent local time does not fire (shared vectors)", () => {
  // pkg/cron dst_test.go:TestNextAfter_SpringForward_NonexistentTimeDoesNotFire
  const cases: [string, string, string, string][] = [
    // 02:30 does not exist on 03-08 in New York: next is 03-09 02:30 EDT.
    ["America/New_York", "30 2 * * *", "2026-03-08T05:00:00Z", "2026-03-09T06:30:00.000Z"],
    // 01:30 does not exist on 03-29 in London: next is 03-30 01:30 BST.
    ["Europe/London", "30 1 * * *", "2026-03-29T00:00:00Z", "2026-03-30T00:30:00.000Z"],
  ];
  for (const [zone, expr, from, want] of cases) {
    assert.equal(nextCronRun(expr, zone, new Date(from))?.toISOString(), want, `${zone} ${expr}`);
  }
});

test("a run inside the fall-back repeated hour fires at its first occurrence", () => {
  // 2026-11-01: 01:00 local happens twice in Denver (MDT, then MST). The
  // server fires a repeated wall time once, at its FIRST occurrence — 01:30
  // MDT, 07:30Z — so the prediction must name that instant, not the second.
  const from = new Date("2026-11-01T00:00:00Z");
  const next = nextCronRun("30 1 * * *", "America/Denver", from);
  assert.ok(next);
  assert.equal(next.toISOString(), "2026-11-01T07:30:00.000Z");
});

// DST reference instants (2026), as in pkg/cron/dst_test.go:
//   America/New_York  fall back Sun 11-01 02:00 EDT → 01:00 EST (01:xx twice)
//   Europe/London     fall back Sun 10-25 02:00 BST → 01:00 GMT (01:xx twice)
// Resolving a wall time naively picks the first pass in New York but the
// SECOND in London, so London is where a naive implementation goes wrong.
const FALL_BACKS = [
  {
    zone: "America/New_York",
    first0130: "2026-11-01T05:30:00.000Z",
    second0130: "2026-11-01T06:30:00.000Z",
    dayStart: "2026-11-01T04:00:00.000Z",
    nextDay0130: "2026-11-02T06:30:00.000Z",
  },
  {
    zone: "Europe/London",
    first0130: "2026-10-25T00:30:00.000Z",
    second0130: "2026-10-25T01:30:00.000Z",
    dayStart: "2026-10-24T23:00:00.000Z",
    nextDay0130: "2026-10-26T01:30:00.000Z",
  },
];

const shift = (iso: string, minutes: number) =>
  new Date(new Date(iso).getTime() + minutes * 60_000);

test("a repeated wall time resolves to its first occurrence (shared vectors)", () => {
  // pkg/cron dst_test.go:TestNextAfter_FallBack_ResolvesToFirstOccurrence
  for (const fb of FALL_BACKS) {
    assert.equal(
      nextCronRun("30 1 * * *", fb.zone, new Date(fb.dayStart))?.toISOString(),
      fb.first0130,
      `${fb.zone}: from local midnight`,
    );
  }
});

test("from inside the second pass, the repeated slot does not fire again", () => {
  // pkg/cron dst_test.go:TestNextAfter_FromSecondPass_SkipsRepeatedSlot
  for (const fb of FALL_BACKS) {
    const from = shift(fb.second0130, -20); // second-pass 01:10
    assert.equal(
      nextCronRun("30 1 * * *", fb.zone, from)?.toISOString(),
      fb.nextDay0130,
      `${fb.zone}: from second-pass 01:10`,
    );
  }
});

test("a quarter-hourly schedule skips the second pass of the repeated hour", () => {
  // pkg/cron dst_test.go:TestNextAfter_Every15_SkipsSecondPassOfRepeatedHour
  for (const fb of FALL_BACKS) {
    const from = shift(fb.first0130, 20); // first-pass 01:50
    // The next wall slot not already used is 02:00, after the whole
    // second pass of 01:xx.
    assert.equal(
      nextCronRun("*/15 * * * *", fb.zone, from)?.toISOString(),
      shift(fb.second0130, 30).toISOString(),
      `${fb.zone}: from first-pass 01:50`,
    );
  }
});

test("a transition day has one run per distinct wall slot", () => {
  // pkg/cron dst_test.go:TestTransitionDays_MatchesAndNextAfterAgree — a
  // 23-hour day has 92 quarter-hour runs, a 25-hour day still only 96.
  const cases: [string, string, string, number][] = [
    ["America/New_York", "2026-03-08T05:00:00Z", "2026-03-09T04:00:00Z", 92],
    ["America/New_York", "2026-11-01T04:00:00Z", "2026-11-02T05:00:00Z", 96],
    ["Europe/London", "2026-03-29T00:00:00Z", "2026-03-29T23:00:00Z", 92],
    ["Europe/London", "2026-10-24T23:00:00Z", "2026-10-26T00:00:00Z", 96],
  ];
  for (const [zone, startIso, endIso, want] of cases) {
    const end = new Date(endIso).getTime();
    let chained = 0;
    let c = nextCronRun("*/15 * * * *", zone, shift(startIso, -1));
    while (c && c.getTime() < end) {
      chained++;
      c = nextCronRun("*/15 * * * *", zone, c);
    }
    assert.equal(chained, want, `${zone} day starting ${startIso}`);
  }
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
