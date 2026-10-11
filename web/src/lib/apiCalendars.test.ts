/**
 * Wire-format tests for the runtime calendar client.
 *
 * Same hand-rolled fetch capture as apiGoals.test.ts: the exact URL, method
 * and JSON body are the contract with internal/api/calendars.go. The address
 * travels only in the request body, never in the URL or a query string.
 */
import { strict as assert } from "node:assert";
import { test, beforeEach, afterEach } from "node:test";

import {
  createCalendar,
  deleteCalendar,
  listCalendars,
  updateCalendar,
} from "./calendars";
import { useAuth } from "./auth";

type Captured = { url: string; method: string; body: unknown };

let captured: Captured[] = [];
let nextBody: unknown = {};
let nextStatus = 200;
const originalFetch = globalThis.fetch;

beforeEach(() => {
  captured = [];
  nextBody = {};
  nextStatus = 200;
  useAuth.setState({ token: "test-token" });

  globalThis.fetch = (async (
    input: RequestInfo | URL,
    init?: RequestInit,
  ): Promise<Response> => {
    const url =
      typeof input === "string"
        ? input
        : input instanceof URL
          ? input.toString()
          : (input as Request).url;
    let bodyJson: unknown = undefined;
    if (init?.body && typeof init.body === "string") {
      bodyJson = JSON.parse(init.body);
    }
    captured.push({
      url,
      method: (init?.method ?? "GET").toUpperCase(),
      body: bodyJson,
    });
    return new Response(JSON.stringify(nextBody), {
      status: nextStatus,
      headers: { "Content-Type": "application/json" },
    });
  }) as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

test("listCalendars GETs /calendars and unwraps the list", async () => {
  nextBody = {
    calendars: [
      { name: "personal", kind: "ics", origin: "runtime", poll: "5m" },
    ],
  };
  const rows = await listCalendars();
  assert.equal(rows.length, 1);
  assert.equal(rows[0].name, "personal");
  assert.equal(captured[0].method, "GET");
  assert.equal(captured[0].url, "/api/v1/calendars");
});

test("listCalendars treats a null list as empty", async () => {
  nextBody = { calendars: null };
  assert.deepEqual(await listCalendars(), []);
});

test("createCalendar POSTs name, url and poll in the body only", async () => {
  nextBody = { name: "personal", kind: "ics", origin: "runtime" };
  await createCalendar({
    name: "personal",
    url: "https://calendar.google.com/calendar/ical/x/basic.ics",
    poll: "10m",
  });
  assert.equal(captured[0].method, "POST");
  assert.equal(captured[0].url, "/api/v1/calendars");
  assert.deepEqual(captured[0].body, {
    name: "personal",
    url: "https://calendar.google.com/calendar/ical/x/basic.ics",
    poll: "10m",
  });
});

test("createCalendar omits poll when the caller leaves it unset", async () => {
  nextBody = { name: "personal", kind: "ics", origin: "runtime" };
  await createCalendar({ name: "personal", url: "https://example.com/a.ics" });
  assert.deepEqual(captured[0].body, {
    name: "personal",
    url: "https://example.com/a.ics",
  });
  assert.equal("poll" in (captured[0].body as object), false);
});

test("updateCalendar PUTs only the fields it was given to the encoded name", async () => {
  nextBody = { name: "a/b", kind: "ics", origin: "runtime" };
  await updateCalendar("a/b", { poll: "15m" });
  assert.equal(captured[0].method, "PUT");
  assert.equal(captured[0].url, "/api/v1/calendars/a%2Fb");
  assert.deepEqual(captured[0].body, { poll: "15m" });
});

test("updateCalendar can replace the address", async () => {
  nextBody = { name: "personal", kind: "ics", origin: "runtime" };
  await updateCalendar("personal", { url: "https://example.com/new.ics" });
  assert.deepEqual(captured[0].body, { url: "https://example.com/new.ics" });
});

test("deleteCalendar DELETEs without a query by default", async () => {
  nextBody = { name: "personal", removed: true, references: [] };
  const r = await deleteCalendar("personal");
  assert.equal(r.removed, true);
  assert.equal(captured[0].method, "DELETE");
  assert.equal(captured[0].url, "/api/v1/calendars/personal");
  assert.equal(captured[0].body, undefined);
});

test("deleteCalendar sends force=true only when forced", async () => {
  nextBody = { name: "personal", removed: true, references: [] };
  await deleteCalendar("personal", { force: true });
  assert.equal(captured[0].url, "/api/v1/calendars/personal?force=true");
});

test("a 409 from delete surfaces as an ApiError carrying the raw body", async () => {
  nextStatus = 409;
  nextBody = {
    error: "Conflict",
    message: "calendar is referenced by automations",
    references: [
      { id: "a1", path: "projects/p/automation/a1.md", title: "Nightly", field: "trigger.calendar" },
    ],
  };
  await assert.rejects(deleteCalendar("personal"), (err: unknown) => {
    const e = err as { status?: number; body?: string; message?: string };
    assert.equal(e.status, 409);
    assert.equal(e.message, "calendar is referenced by automations");
    assert.match(e.body ?? "", /"references"/);
    return true;
  });
});
