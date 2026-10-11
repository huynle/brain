/**
 * Tests for the Settings → Calendars presentational views.
 *
 * Worth pinning: config sources are read-only and say where they are defined;
 * runtime sources offer replace, poll and remove; stale, never-fetched and
 * error states read in words with the server's message; the removal prompt
 * lists the automations that block a removal; and the address field is a
 * masked input that never renders a value.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import {
  CalendarAddressInput,
  CalendarRow,
  CalendarsBody,
  RemovalPrompt,
} from "./CalendarsSettings";
import type { CalendarReference, CalendarStatus } from "../../lib/calendars";

const noop = () => {};

const CONFIG_BUILTIN: CalendarStatus = { name: "xnys", kind: "builtin", origin: "config" };
const CONFIG_ICS: CalendarStatus = {
  name: "team",
  kind: "ics",
  origin: "config",
  poll: "10m",
  last_fetch: "2026-10-11T10:00:00Z",
  last_success: "2026-10-11T10:00:00Z",
  event_count: 4,
  stale: false,
};

function runtime(over: Partial<CalendarStatus> = {}): CalendarStatus {
  return {
    name: "personal",
    kind: "ics",
    origin: "runtime",
    poll: "5m",
    last_fetch: "2026-10-11T10:00:00Z",
    last_success: "2026-10-11T10:00:00Z",
    event_count: 12,
    stale: false,
    ...over,
  };
}

const ACTIONS = { onReplace: noop, onChangePoll: noop, onRemove: noop };

function rowHtml(calendar: CalendarStatus, actions?: typeof ACTIONS): string {
  return renderToStaticMarkup(createElement(CalendarRow, { calendar, actions }));
}

test("a built-in config source has no edit controls and says where it is defined", () => {
  const html = rowHtml(CONFIG_BUILTIN);
  assert.match(html, /defined in server config/i);
  assert.doesNotMatch(html, /<button/);
  assert.doesNotMatch(html, /Remove|Replace address|poll interval/i);
});

test("an ICS config source is read-only even though it has a fetch status", () => {
  const html = rowHtml(CONFIG_ICS);
  assert.match(html, /defined in server config/i);
  assert.match(html, /Healthy/);
  assert.doesNotMatch(html, /<button/);
});

test("a runtime source offers replace address, change poll interval and remove", () => {
  const html = rowHtml(runtime(), ACTIONS);
  assert.match(html, />Replace address</);
  assert.match(html, />Change poll interval</);
  assert.match(html, />Remove</);
  assert.doesNotMatch(html, /defined in server config/i);
});

test("status reads healthy, stale, never fetched and error with the server's message", () => {
  assert.match(rowHtml(runtime()), /Healthy/);
  assert.match(rowHtml(runtime({ stale: true })), /Stale/);
  assert.match(
    rowHtml(runtime({ last_fetch: undefined, last_success: undefined, event_count: undefined, stale: undefined })),
    /Never fetched/,
  );
  const failed = rowHtml(runtime({ last_error: "fetch failed: HTTP 404", stale: true }));
  assert.match(failed, /Error/);
  assert.match(failed, /fetch failed: HTTP 404/);
});

test("a row shows the poll interval and event count for an ICS source", () => {
  const html = rowHtml(runtime(), ACTIONS);
  assert.match(html, /5m/);
  assert.match(html, /12 events/);
});

test("the list shows loading, empty, error and populated states distinctly", () => {
  const body = (over: Partial<Parameters<typeof CalendarsBody>[0]>) =>
    renderToStaticMarkup(
      createElement(CalendarsBody, {
        calendars: undefined,
        loading: false,
        error: null,
        onRetry: noop,
        actionsFor: () => undefined,
        ...over,
      }),
    );

  assert.match(body({ loading: true }), /Loading calendars/);

  const failed = body({ error: new Error("boom") });
  assert.match(failed, /role="alert"/);
  assert.match(failed, /boom/);

  assert.match(body({ calendars: [] }), /No calendar sources yet/);

  const listed = body({ calendars: [CONFIG_BUILTIN, runtime()], actionsFor: () => ACTIONS });
  assert.match(listed, /xnys/);
  assert.match(listed, /personal/);
  assert.match(listed, /defined in server config/i);
  assert.match(listed, />Remove</);
});

test("the removal prompt lists each blocking automation and offers a forced removal", () => {
  const references: CalendarReference[] = [
    { id: "a1", path: "projects/p/automation/nightly.md", title: "Nightly digest", field: "trigger.calendar" },
    { id: "a2", path: "projects/p/automation/quiet.md", title: "Quiet hours", field: "trigger.skip_if_event" },
  ];
  const html = renderToStaticMarkup(
    createElement(RemovalPrompt, {
      name: "personal",
      references,
      busy: false,
      error: null,
      onRemoveAnyway: noop,
      onCancel: noop,
    }),
  );
  assert.match(html, /role="alertdialog"/);
  assert.match(html, /Nightly digest/);
  assert.match(html, /Calendar trigger/);
  assert.match(html, /Quiet hours/);
  assert.match(html, /Skip if event/);
  assert.match(html, />Remove anyway</);
  assert.match(html, />Cancel</);
});

test("the address field is a masked input that never renders a value", () => {
  const html = renderToStaticMarkup(
    createElement(CalendarAddressInput, {
      id: "calendar-address",
      inputRef: { current: null },
    }),
  );
  assert.match(html, /type="password"/);
  assert.match(html, /autocomplete="off"/i);
  assert.doesNotMatch(html, /value=/);
  assert.match(html, /Secret address in iCal format/);
  assert.match(html, /cannot be viewed again/);
});
