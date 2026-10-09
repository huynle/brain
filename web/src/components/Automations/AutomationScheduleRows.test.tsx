import { strict as assert } from "node:assert";
import { test } from "node:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { AutomationScheduleRows } from "./AutomationScheduleRows";

const render = (automation: Parameters<typeof AutomationScheduleRows>[0]["automation"]) =>
  renderToStaticMarkup(createElement(AutomationScheduleRows, { automation }));

test("renders nothing for an automation with no scheduling fields set", () => {
  assert.equal(render({ trigger: { type: "event", event: "task.completed" } }), "");
  assert.equal(render({}), "");
});

test("renders every trigger and lifecycle field that is set, read-only", () => {
  const html = render({
    trigger: {
      type: "cron",
      every: "1d",
      at: "03:00",
      stagger: "2h",
      catch_up: "none",
      calendar: "xnys",
      skip_if_event: { calendar: "xnys", all_day: "true" },
      only_if_event: { title: "FOMC" },
      timezone: "America/New_York",
    },
    starts_at: "2026-01-01T00:00:00Z",
    expires_at: "2027-01-01T00:00:00Z",
    max_runs: 30,
  });
  // Label cell, then value cell, in the kv-grid style the config section uses.
  assert.match(html, /<div class="k">Every<\/div><div class="v">1d<\/div>/);
  assert.match(html, /<div class="k">At<\/div><div class="v">03:00<\/div>/);
  assert.match(html, /<div class="k">Stagger<\/div><div class="v">per-project offset within 2h<\/div>/);
  assert.match(html, /<div class="k">Catch-up<\/div><div class="v">none \(missed runs are skipped\)<\/div>/);
  assert.match(html, /<div class="k">Calendar<\/div><div class="v">xnys<\/div>/);
  assert.match(html, /<div class="k">Skip if event<\/div><div class="v">calendar=xnys, all_day=true<\/div>/);
  assert.match(html, /<div class="k">Only if event<\/div><div class="v">title=FOMC<\/div>/);
  assert.match(html, /<div class="k">Timezone<\/div><div class="v">America\/New_York<\/div>/);
  assert.match(html, /<div class="k">Starts<\/div><div class="v">2026-01-01T00:00:00Z<\/div>/);
  assert.match(html, /<div class="k">Expires<\/div><div class="v">2027-01-01T00:00:00Z<\/div>/);
  assert.match(html, /<div class="k">Max runs<\/div><div class="v">30<\/div>/);
});

test("catch_up describes a duration cap, and an empty value shows nothing", () => {
  assert.match(
    render({ trigger: { type: "cron", catch_up: "6h" } }),
    /<div class="v">missed runs within 6h<\/div>/,
  );
  assert.doesNotMatch(render({ trigger: { type: "cron", catch_up: "" } }), /Catch-up/);
});

test("timezone falls back to the automation's own zone when the trigger has none", () => {
  const html = render({ timezone: "Europe/London", trigger: { type: "cron", every: "1h" } });
  assert.match(html, /<div class="k">Timezone<\/div><div class="v">Europe\/London<\/div>/);
});

test("a zero max_runs is no cap and renders nothing", () => {
  assert.doesNotMatch(render({ max_runs: 0 }), /Max runs/);
});
