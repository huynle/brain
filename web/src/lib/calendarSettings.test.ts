/**
 * Tests for the Calendars settings logic: client-side validation that mirrors
 * the server rules, status labels for each source, error mapping, the 409
 * reference list, and the submit flows that must clear the secret address.
 *
 * Security points pinned here: an address is sent once, is cleared on every
 * submit attempt, is never part of a returned value, and is never echoed in
 * a validation message.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import { ApiError } from "./api";
import type { CalendarReference, CalendarRemoval } from "./calendars";
import {
  calendarStatusView,
  changePollInterval,
  errorMessageFor,
  isRuntimeCalendar,
  referencesFrom,
  removeCalendarFlow,
  replaceCalendarAddress,
  submitNewCalendar,
  validateCalendarAddress,
  validateCalendarName,
  validatePollInterval,
} from "./calendarSettings";

const REF: CalendarReference = {
  id: "a1",
  path: "projects/p/automation/nightly.md",
  title: "Nightly digest",
  field: "trigger.calendar",
};

test("validateCalendarName accepts the server rule and rejects everything else", () => {
  for (const ok of ["personal", "a", "work-2_x", "0cal", "a".repeat(64)]) {
    assert.equal(validateCalendarName(ok), null, ok);
  }
  for (const bad of ["", "Personal", "-x", "_x", "a b", "x/y", "a".repeat(65)]) {
    assert.notEqual(validateCalendarName(bad), null, bad);
  }
});

test("validatePollInterval allows an empty value and durations of at least one minute", () => {
  assert.equal(validatePollInterval(""), null);
  assert.equal(validatePollInterval("   "), null);
  assert.equal(validatePollInterval("1m"), null);
  assert.equal(validatePollInterval("5m"), null);
  assert.equal(validatePollInterval("1h30m"), null);
  assert.notEqual(validatePollInterval("30s"), null);
  assert.notEqual(validatePollInterval("0m"), null);
  assert.notEqual(validatePollInterval("five minutes"), null);
  assert.notEqual(validatePollInterval("5"), null);
});

test("validateCalendarAddress requires https without user info, up to 2048 characters", () => {
  assert.equal(
    validateCalendarAddress(
      "https://calendar.google.com/calendar/ical/me%40example.com/private-abc/basic.ics",
    ),
    null,
  );
  assert.equal(validateCalendarAddress("  https://calendar.example.com/a.ics  "), null);
  assert.notEqual(validateCalendarAddress(""), null);
  assert.notEqual(validateCalendarAddress("http://calendar.example.com/a.ics"), null);
  assert.notEqual(validateCalendarAddress("webcal://calendar.example.com/a.ics"), null);
  assert.notEqual(validateCalendarAddress("https://user:pw@calendar.example.com/a.ics"), null);
  assert.notEqual(
    validateCalendarAddress("https://calendar.example.com/" + "a".repeat(2048)),
    null,
  );
});

test("address validation messages never echo the address", () => {
  const secret = "https://SECRETHOST.example.com/SECRETPATH.ics";
  for (const candidate of [secret.replace("https", "http"), secret.replace("//", "//u:p@")]) {
    const message = validateCalendarAddress(candidate);
    assert.notEqual(message, null);
    assert.equal(message!.includes("SECRET"), false, message!);
  }
});

test("status shows the server message for errors, and labels stale, never fetched, healthy and built-in", () => {
  assert.deepEqual(
    calendarStatusView({
      name: "a",
      kind: "ics",
      origin: "runtime",
      last_fetch: "2026-10-11T10:00:00Z",
      last_error: "fetch failed: HTTP 404",
      stale: true,
    }),
    { tone: "error", label: "Error", detail: "fetch failed: HTTP 404" },
  );
  assert.deepEqual(
    calendarStatusView({ name: "a", kind: "ics", origin: "runtime" }),
    { tone: "muted", label: "Never fetched" },
  );
  assert.deepEqual(
    calendarStatusView({
      name: "a",
      kind: "ics",
      origin: "runtime",
      last_fetch: "t",
      last_success: "t",
      stale: true,
    }),
    { tone: "warn", label: "Stale" },
  );
  assert.deepEqual(
    calendarStatusView({
      name: "a",
      kind: "ics",
      origin: "runtime",
      last_fetch: "t",
      last_success: "t",
      stale: false,
      event_count: 3,
    }),
    { tone: "ok", label: "Healthy" },
  );
  assert.deepEqual(
    calendarStatusView({ name: "x", kind: "builtin", origin: "config" }),
    { tone: "muted", label: "Built in" },
  );
});

test("isRuntimeCalendar is true only for runtime-origin sources", () => {
  assert.equal(isRuntimeCalendar({ name: "a", kind: "ics", origin: "runtime" }), true);
  assert.equal(isRuntimeCalendar({ name: "a", kind: "ics", origin: "config" }), false);
  assert.equal(isRuntimeCalendar({ name: "a", kind: "ics" }), false);
});

test("errorMessageFor maps 403 to the admin message and keeps other server messages", () => {
  assert.equal(
    errorMessageFor(new ApiError(403, "forbidden", "")),
    "Only admins can manage calendars",
  );
  assert.equal(
    errorMessageFor(new ApiError(422, "address resolves to a private network", "")),
    "address resolves to a private network",
  );
  assert.equal(errorMessageFor(new Error("network down")), "network down");
  assert.equal(errorMessageFor("oops"), "Something went wrong. Try again.");
});

test("referencesFrom reads the references from a 409 body and returns none otherwise", () => {
  const body = JSON.stringify({
    error: "Conflict",
    message: "referenced by automations",
    references: [REF],
  });
  assert.deepEqual(referencesFrom(new ApiError(409, "referenced", body)), [REF]);
  assert.deepEqual(
    referencesFrom(
      new ApiError(
        409,
        "defined in server config",
        JSON.stringify({ error: "Conflict", message: "defined in server config" }),
      ),
    ),
    [],
  );
  assert.deepEqual(referencesFrom(new ApiError(500, "x", "not json")), []);
  assert.deepEqual(referencesFrom(new Error("x")), []);
});

test("removeCalendarFlow lists references on 409, then removes with force on retry", async () => {
  const calls: Array<{ name: string; force: boolean }> = [];
  const remove = async (
    name: string,
    opts: { force?: boolean } = {},
  ): Promise<CalendarRemoval> => {
    calls.push({ name, force: opts.force === true });
    if (!opts.force) {
      throw new ApiError(
        409,
        "referenced by automations",
        JSON.stringify({ error: "Conflict", message: "referenced", references: [REF] }),
      );
    }
    return { name, removed: true, references: [REF] };
  };

  assert.deepEqual(await removeCalendarFlow("personal", { remove }), {
    kind: "references",
    references: [REF],
  });
  assert.deepEqual(await removeCalendarFlow("personal", { remove }, { force: true }), {
    kind: "removed",
  });
  assert.deepEqual(calls, [
    { name: "personal", force: false },
    { name: "personal", force: true },
  ]);
});

test("removeCalendarFlow reports config-owned and unknown sources as plain errors", async () => {
  const configOwned = async () => {
    throw new ApiError(
      409,
      "defined in server config",
      JSON.stringify({ error: "Conflict", message: "defined in server config" }),
    );
  };
  assert.deepEqual(await removeCalendarFlow("xnys", { remove: configOwned }), {
    kind: "error",
    message: "defined in server config",
  });
  const unknown = async () => {
    throw new ApiError(404, "unknown calendar", "");
  };
  assert.deepEqual(await removeCalendarFlow("nope", { remove: unknown }), {
    kind: "error",
    message: "unknown calendar",
  });
});

test("submitNewCalendar sends the trimmed address once, clears it, and never returns it", async () => {
  const address =
    "https://calendar.google.com/calendar/ical/me%40example.com/private-SECRET123/basic.ics";
  const sent: unknown[] = [];
  let cleared = 0;
  const result = await submitNewCalendar(
    { name: "personal", poll: "" },
    `  ${address}  `,
    {
      create: async (input) => {
        sent.push(input);
        return { name: "personal", kind: "ics", origin: "runtime" };
      },
      clear: () => {
        cleared++;
      },
    },
  );
  assert.deepEqual(sent, [{ name: "personal", url: address }]);
  assert.equal(cleared, 1);
  assert.equal(result.ok, true);
  assert.equal(JSON.stringify(result).includes("SECRET123"), false);
});

test("submitNewCalendar passes the poll interval when one is given", async () => {
  const sent: unknown[] = [];
  await submitNewCalendar(
    { name: "personal", poll: "10m" },
    "https://calendar.example.com/a.ics",
    {
      create: async (input) => {
        sent.push(input);
        return { name: "personal", kind: "ics" };
      },
      clear: () => {},
    },
  );
  assert.deepEqual(sent, [
    { name: "personal", url: "https://calendar.example.com/a.ics", poll: "10m" },
  ]);
});

test("submitNewCalendar clears the address when the server rejects it and reports the message", async () => {
  let cleared = 0;
  const result = await submitNewCalendar(
    { name: "personal", poll: "10m" },
    "https://calendar.example.com/SECRET999.ics",
    {
      create: async () => {
        throw new ApiError(422, "address rejected: validation fetch failed", "");
      },
      clear: () => {
        cleared++;
      },
    },
  );
  assert.equal(cleared, 1);
  assert.deepEqual(result, {
    ok: false,
    message: "address rejected: validation fetch failed",
  });
});

test("submitNewCalendar validates locally, sends nothing invalid, and still clears the address", async () => {
  let sent = 0;
  let cleared = 0;
  const deps = {
    create: async () => {
      sent++;
      return { name: "x", kind: "ics" as const };
    },
    clear: () => {
      cleared++;
    },
  };
  const badName = await submitNewCalendar(
    { name: "Bad Name", poll: "" },
    "https://calendar.example.com/a.ics",
    deps,
  );
  const badAddress = await submitNewCalendar(
    { name: "ok", poll: "" },
    "http://calendar.example.com/a.ics",
    deps,
  );
  const badPoll = await submitNewCalendar(
    { name: "ok", poll: "30s" },
    "https://calendar.example.com/a.ics",
    deps,
  );
  assert.equal(badName.ok, false);
  assert.equal(badAddress.ok, false);
  assert.equal(badPoll.ok, false);
  assert.equal(sent, 0);
  assert.equal(cleared, 3);
});

test("replaceCalendarAddress updates only the url and clears the address", async () => {
  const sent: unknown[] = [];
  let cleared = 0;
  const result = await replaceCalendarAddress(
    "personal",
    "https://calendar.example.com/new.ics",
    {
      update: async (name, input) => {
        sent.push({ name, input });
        return { name, kind: "ics" };
      },
      clear: () => {
        cleared++;
      },
    },
  );
  assert.deepEqual(sent, [
    { name: "personal", input: { url: "https://calendar.example.com/new.ics" } },
  ]);
  assert.equal(cleared, 1);
  assert.equal(result.ok, true);
});

test("changePollInterval validates the duration and sends only the poll", async () => {
  const sent: unknown[] = [];
  const deps = {
    update: async (name: string, input: unknown) => {
      sent.push({ name, input });
      return { name, kind: "ics" as const };
    },
  };
  const bad = await changePollInterval("personal", "30s", deps);
  assert.equal(bad.ok, false);
  const good = await changePollInterval("personal", "10m", deps);
  assert.equal(good.ok, true);
  assert.deepEqual(sent, [{ name: "personal", input: { poll: "10m" } }]);
});
