/**
 * Client-side mirror of `pkg/cron` — 5-field cron parsing and next-run
 * prediction for the automations list.
 *
 * This exists to answer "when does this fire next?" on a row that
 * otherwise only says "cron". It is a PREDICTION of what the server will
 * do, so it has to agree with the server's own matcher rather than with
 * cron folklore. Three places where the two differ, all deliberate here:
 *
 *   1. Day fields follow pkg/cron's Vixie rule. When NEITHER day-of-month
 *      nor day-of-week starts with `*`, a day qualifies if EITHER matches,
 *      so `0 3 1 * 1` runs on the 1st and on every Monday. If either field
 *      starts with `*` (a stepped star included), the two AND together, so
 *      a stepped day-of-month with a Monday means only the odd-numbered
 *      Mondays. See dayMatches.
 *   2. `V/S` (a step on a bare value, e.g. `5/15`) means "from V to the
 *      field maximum, stepping by S" — same as pkg/cron's parseFieldPart.
 *   3. DST, as pkg/cron resolves it: a wall time skipped by a spring-forward
 *      never fires, and a wall time repeated by a fall-back fires once, at
 *      its first occurrence.
 *
 * Timezone handling matches the server's shape: the expression is matched
 * against WALL CLOCK in the automation's IANA zone, and the result is
 * returned as a real instant. Iteration is done in naive calendar space
 * (cheap integer arithmetic) and converted to an instant only once a match
 * is found — searching in instant space would need an Intl lookup per
 * candidate minute, which is far too slow for a 366-day worst case.
 *
 * Known limit: this module steps wall-clock minutes and resolves each
 * candidate to an instant afterwards, where pkg/cron steps instants. The
 * shared DST vectors in cronSchedule.test.ts pin both to the same answers;
 * a transition shape outside those vectors has not been checked against
 * the server. This is a display hint, not a dispatch decision.
 */

/** Inclusive bounds per field, in pkg/cron's field order. */
const LIMITS: ReadonlyArray<{ min: number; max: number }> = [
  { min: 0, max: 59 }, // minute
  { min: 0, max: 23 }, // hour
  { min: 1, max: 31 }, // day of month
  { min: 1, max: 12 }, // month
  { min: 0, max: 7 }, // day of week (0 and 7 both mean Sunday)
];

/** A parsed 5-field expression: the allowed value set for each field. */
export interface CronSchedule {
  minute: Set<number>;
  hour: Set<number>;
  dayOfMonth: Set<number>;
  month: Set<number>;
  dayOfWeek: Set<number>;
  /** True when the day-of-month field starts with `*`; see dayMatches. */
  domStar: boolean;
  /** True when the day-of-week field starts with `*`; see dayMatches. */
  dowStar: boolean;
}

/**
 * Parses one comma-free part of a field — a bare "*", a value, a range, or
 * any of those with a trailing step — into `out`. Returns false on anything
 * pkg/cron would reject, so an expression the server cannot run never
 * renders a confident next-run.
 */
function parseFieldPart(
  part: string,
  lim: { min: number; max: number },
  out: Set<number>,
): boolean {
  let stepStr = "";
  const slash = part.indexOf("/");
  if (slash >= 0) {
    stepStr = part.slice(slash + 1);
    part = part.slice(0, slash);
  }

  let start: number;
  let end: number;

  const int = (s: string): number | null => {
    // Number() accepts "", "0x10", "1e2" and " 1 "; cron does not.
    if (!/^\d+$/.test(s)) return null;
    return Number(s);
  };

  if (part === "*") {
    start = lim.min;
    end = lim.max;
  } else {
    const dash = part.indexOf("-");
    if (dash >= 0) {
      const a = int(part.slice(0, dash));
      const b = int(part.slice(dash + 1));
      if (a === null || b === null || a > b) return false;
      start = a;
      end = b;
    } else {
      const v = int(part);
      if (v === null) return false;
      start = v;
      // A step on a bare value runs from that value to the field max.
      end = stepStr !== "" ? lim.max : v;
    }
  }

  if (start < lim.min || start > lim.max) return false;
  if (end < lim.min || end > lim.max) return false;

  let step = 1;
  if (stepStr !== "") {
    const s = int(stepStr);
    if (s === null || s <= 0) return false;
    step = s;
  }

  for (let v = start; v <= end; v += step) out.add(v);
  return true;
}

/** Parses a 5-field cron expression. Returns null if the server would reject it. */
export function parseCron(expr: string): CronSchedule | null {
  const parts = (expr || "").trim().split(/\s+/).filter(Boolean);
  if (parts.length !== 5) return null;

  const sets: Set<number>[] = [];
  for (let i = 0; i < 5; i++) {
    const out = new Set<number>();
    for (const part of parts[i].split(",")) {
      if (!parseFieldPart(part, LIMITS[i], out)) return null;
    }
    if (out.size === 0) return null;
    sets.push(out);
  }

  // Day-of-week 7 and 0 both mean Sunday.
  if (sets[4].has(7)) sets[4].add(0);

  return {
    minute: sets[0],
    hour: sets[1],
    dayOfMonth: sets[2],
    month: sets[3],
    dayOfWeek: sets[4],
    domStar: parts[2].startsWith("*"),
    dowStar: parts[4].startsWith("*"),
  };
}

/**
 * The day predicate shared by matching and searching, mirroring pkg/cron's
 * dayMatches. When NEITHER day field starts with `*`, a day qualifies if it
 * matches day-of-month OR day-of-week. Otherwise both must match; an
 * unrestricted `*` matches every day, so that reduces to the restricted field.
 */
function dayMatches(s: CronSchedule, day: number, weekday: number): boolean {
  const dom = s.dayOfMonth.has(day);
  const dow = s.dayOfWeek.has(weekday);
  if (s.domStar || s.dowStar) return dom && dow;
  return dom || dow;
}

/** Milliseconds `timeZone` is ahead of UTC at `instant`, to the minute. */
function offsetMsAt(instant: Date, timeZone: string): number {
  const floored = Math.floor(instant.getTime() / 60_000) * 60_000;
  return wallMsIn(instant, timeZone) - floored;
}

/** The wall clock `instant` shows in `timeZone`, read as if it were UTC. */
function wallMsIn(instant: Date, timeZone: string): number {
  const w = wallClockIn(instant, timeZone);
  return Date.UTC(w.year, w.month - 1, w.day, w.hour, w.minute);
}

/**
 * Moves `instant` back to the first pass of the wall clock `wallMs` when that
 * wall clock repeats (a fall-back). A repeated span is at most a couple of
 * hours, so an earlier pass lies within two hours before `instant`.
 */
function firstPass(instant: Date, wallMs: number, timeZone: string): Date {
  const off = offsetMsAt(instant, timeZone);
  const prevOff = offsetMsAt(new Date(instant.getTime() - 2 * 3_600_000), timeZone);
  if (prevOff > off) {
    const u = new Date(instant.getTime() - (prevOff - off));
    if (offsetMsAt(u, timeZone) === prevOff && wallMsIn(u, timeZone) === wallMs) {
      return u;
    }
  }
  return instant;
}

/** Wall-clock fields, timezone-free. */
interface WallClock {
  year: number;
  month: number; // 1-12
  day: number;
  hour: number;
  minute: number;
}

/**
 * Reads the wall-clock fields an instant shows in `timeZone`. An unknown
 * zone falls back to UTC, matching pkg/cron.LoadTimezone.
 */
function wallClockIn(instant: Date, timeZone: string): WallClock {
  let parts: Intl.DateTimeFormatPart[];
  try {
    parts = new Intl.DateTimeFormat("en-US", {
      timeZone,
      hour12: false,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
    }).formatToParts(instant);
  } catch {
    return {
      year: instant.getUTCFullYear(),
      month: instant.getUTCMonth() + 1,
      day: instant.getUTCDate(),
      hour: instant.getUTCHours(),
      minute: instant.getUTCMinutes(),
    };
  }
  const get = (t: string) => Number(parts.find((p) => p.type === t)?.value ?? 0);
  // "24" appears for midnight in some ICU versions under hour12:false.
  const hour = get("hour") % 24;
  return {
    year: get("year"),
    month: get("month"),
    day: get("day"),
    hour,
    minute: get("minute"),
  };
}

/**
 * Converts a wall clock in `timeZone` back to an instant.
 *
 * Solved by fixed-point rather than a table: interpret the wall clock as
 * if it were UTC, measure how far that lands from the target zone, and
 * correct. Two passes settle the case where the correction itself crosses
 * an offset change. A repeated local time is moved back to its first
 * occurrence (firstPass); a skipped one resolves just past the gap and is
 * rejected by the round-trip check in nextCronRun.
 */
function wallClockToInstant(wc: WallClock, timeZone: string): Date {
  const asIfUTC = Date.UTC(wc.year, wc.month - 1, wc.day, wc.hour, wc.minute);
  let guess = asIfUTC;
  for (let i = 0; i < 2; i++) {
    const shown = wallClockIn(new Date(guess), timeZone);
    const shownAsUTC = Date.UTC(
      shown.year,
      shown.month - 1,
      shown.day,
      shown.hour,
      shown.minute,
    );
    const drift = shownAsUTC - asIfUTC;
    if (drift === 0) break;
    guess -= drift;
  }
  return firstPass(new Date(guess), asIfUTC, timeZone);
}

/** Minutes in 366 days — the server's own search horizon. */
const MAX_SEARCH_MINUTES = 527040;

/** Longest possible length of each month; February allows for a leap year. */
const LONGEST_MONTH = [31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];

/**
 * Whether any (month, day-of-month) pair in the schedule can exist.
 *
 * "0 0 30 2 *" (February 30th) and "0 0 31 4 *" (April 31st) are plausible
 * typos, and without this the search below walks its entire step budget —
 * measured at ~220ms per call — before concluding what arithmetic settles
 * instantly. taskScheduleChip runs inline in TaskRow's render body, and the
 * task list re-renders on every SSE update, so a couple of such rows block
 * the main thread for a noticeable fraction of a second per update.
 */
function hasSatisfiableDate(s: CronSchedule): boolean {
  // Under the OR rule a restricted day-of-week admits every weekday in every
  // month, so "0 0 30 2 1" (February 30th OR Monday) is satisfiable.
  if (!s.domStar && !s.dowStar) return true;
  for (const m of s.month) {
    for (const d of s.dayOfMonth) {
      if (d <= LONGEST_MONTH[m - 1]) return true;
    }
  }
  return false;
}

/**
 * Expressions already proven to match nothing, keyed `expr|timeZone`.
 *
 * Whether a schedule has ANY occurrence is a property of the expression, not
 * of the instant we search from, so a null verdict is safe to keep. This is
 * the backstop for shapes hasSatisfiableDate cannot rule out cheaply — a
 * day-of-month AND day-of-week pair that is merely very rare, say.
 */
const noMatchCache = new Set<string>();

/**
 * The next instant strictly after `from` at which `expr` fires in
 * `timeZone`. Returns null for an unparseable expression, or for one that
 * matches nothing within a year (e.g. `0 0 30 2 *` — February 30th).
 *
 * `from` defaults to now; the search starts at the following minute, so a
 * schedule firing at this very minute reports its NEXT occurrence rather
 * than the one currently landing.
 */
export function nextCronRun(
  expr: string,
  timeZone = "UTC",
  from: Date = new Date(),
): Date | null {
  const sched = parseCron(expr);
  if (!sched) return null;
  if (!hasSatisfiableDate(sched)) return null;
  const cacheKey = `${expr}|${timeZone || "UTC"}`;
  if (noMatchCache.has(cacheKey)) return null;

  const nowWall = wallClockIn(from, timeZone || "UTC");
  // A UTC-based Date used purely as a naive calendar: getUTC* reads back
  // the same wall-clock fields we put in, with correct month/leap-year
  // rollover and weekday, and no zone semantics of its own.
  const cursor = new Date(
    Date.UTC(
      nowWall.year,
      nowWall.month - 1,
      nowWall.day,
      nowWall.hour,
      nowWall.minute,
    ),
  );
  cursor.setUTCMinutes(cursor.getUTCMinutes() + 1);

  for (let i = 0; i < MAX_SEARCH_MINUTES; i++) {
    // Month first, then day: skipping a whole non-matching month costs one
    // check instead of ~44k, which is what keeps a yearly schedule fast.
    if (!sched.month.has(cursor.getUTCMonth() + 1)) {
      cursor.setUTCMonth(cursor.getUTCMonth() + 1, 1);
      cursor.setUTCHours(0, 0, 0, 0);
      continue;
    }
    if (!dayMatches(sched, cursor.getUTCDate(), cursor.getUTCDay())) {
      cursor.setUTCDate(cursor.getUTCDate() + 1);
      cursor.setUTCHours(0, 0, 0, 0);
      continue;
    }
    if (!sched.hour.has(cursor.getUTCHours())) {
      cursor.setUTCHours(cursor.getUTCHours() + 1, 0, 0, 0);
      continue;
    }
    if (!sched.minute.has(cursor.getUTCMinutes())) {
      cursor.setUTCMinutes(cursor.getUTCMinutes() + 1, 0, 0);
      continue;
    }
    const wanted: WallClock = {
      year: cursor.getUTCFullYear(),
      month: cursor.getUTCMonth() + 1,
      day: cursor.getUTCDate(),
      hour: cursor.getUTCHours(),
      minute: cursor.getUTCMinutes(),
    };
    const instant = wallClockToInstant(wanted, timeZone || "UTC");
    // A local time inside a spring-forward gap never happens, so no run
    // lands on it. The server matches on the live clock, which simply
    // never reads 02:30 that day, so the truthful answer is the NEXT
    // occurrence — not the instant the gap normalizes onto. Detect it by
    // round-tripping: if the instant does not show the wall clock we
    // searched for, that wall clock does not exist.
    const shown = wallClockIn(instant, timeZone || "UTC");
    if (
      shown.hour === wanted.hour &&
      shown.minute === wanted.minute &&
      shown.day === wanted.day &&
      // A repeated wall time whose first pass is already behind `from` is
      // not the next run: from inside the second pass, that hour has passed.
      instant.getTime() > from.getTime()
    ) {
      return instant;
    }
    cursor.setUTCMinutes(cursor.getUTCMinutes() + 1);
  }
  // Exhausted the budget: remember it so the next render is free.
  noMatchCache.add(cacheKey);
  return null;
}

/**
 * Human-readable gloss for the handful of shapes that cover most rows.
 * Anything else falls back to the raw expression, which is still more
 * than the row showed before.
 */
export function describeCron(expr: string): string {
  const parts = (expr || "").trim().split(/\s+/).filter(Boolean);
  if (parts.length !== 5) return expr;
  // Gloss only what BOTH parsers accept. Without this, out-of-range fields
  // are rendered as confident human text for schedules that can never run —
  // "0 99 * * *" became "daily 99:00", and "0 2 * * 9" became "Tue 02:00"
  // because the day index was taken modulo 7. Returning the raw expression
  // leaves it visibly odd, which is the truthful outcome.
  if (!parseCron(expr)) return expr;
  const [mi, h, dom, mo, dow] = parts;
  const everyDate = dom === "*" && mo === "*";
  const at = (hh: string, mm: string) =>
    `${hh.padStart(2, "0")}:${mm.padStart(2, "0")}`;
  const isNum = (s: string) => /^\d+$/.test(s);

  if (mi === "*" && h === "*" && everyDate && dow === "*") return "every minute";
  if (/^\*\/\d+$/.test(mi) && h === "*" && everyDate && dow === "*")
    return `every ${mi.slice(2)} min`;
  if (isNum(mi) && h === "*" && everyDate && dow === "*") return "hourly";
  if (isNum(mi) && /^\*\/\d+$/.test(h) && everyDate && dow === "*")
    return `every ${h.slice(2)}h`;
  if (isNum(mi) && isNum(h) && everyDate) {
    const time = at(h, mi);
    if (dow === "*") return `daily ${time}`;
    if (dow === "1-5") return `weekdays ${time}`;
    const DAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
    if (isNum(dow)) return `${DAYS[Number(dow) % 7]} ${time}`;
  }
  return expr;
}
