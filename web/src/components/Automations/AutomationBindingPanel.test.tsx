/**
 * Tests for AutomationBindingPanel's presentational view.
 *
 * Worth pinning: a broken view is warned about in words, not silently
 * trusted; each field says inherited or overridden, and only an overridden
 * one offers a reset; the customize form offers only the editable fields;
 * and saving a new binding for a project the filter does not select says it
 * keeps that project off.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { BindingPanelView, type BindingPanelViewProps } from "./AutomationBindingPanel";
import type { AutomationEffectiveView } from "../../lib/automationBindings";
import type { BrainEntry } from "../../lib/types";

const EFFECTIVE: AutomationEffectiveView = {
  id: "nightly",
  project: "p1",
  automation: {
    id: "nightly",
    trigger: { type: "cron", every: "1d", at: "03:00", stagger: "2h" },
    action: { agent: "dream" },
    timezone: "UTC",
  },
  fields: {
    "trigger.schedule": "inherited",
    "trigger.every": "overridden",
    "trigger.at": "overridden",
    "trigger.stagger": "inherited",
    "action.agent": "inherited",
  },
  binding_id: "bind0001",
  binding_status: "active",
  targeted: true,
  broken: false,
};

const BINDING = {
  id: "bind0001",
  path: "projects/p1/automation/nightly--bind0001.md",
  status: "active",
  extends: "nightly",
  trigger: { every: "1d", at: "06:30" },
} as unknown as BrainEntry;

function render(over: Partial<BindingPanelViewProps> = {}): string {
  const props: BindingPanelViewProps = {
    projectId: "p1",
    effective: EFFECTIVE,
    loading: false,
    error: null,
    onRetry: () => {},
    binding: BINDING,
    busy: false,
    saveError: null,
    onSave: () => {},
    onReset: () => {},
    ...over,
  };
  return renderToStaticMarkup(createElement(BindingPanelView, props));
}

test("a broken view shows a warning that says why it cannot be trusted", () => {
  const html = render({
    effective: { ...EFFECTIVE, broken: true, broken_reason: "duplicate_binding" },
  });
  assert.match(html, /role="alert"/);
  assert.match(html, /more than one binding/i);
});

test("a healthy view shows no warning", () => {
  assert.doesNotMatch(render(), /role="alert"/);
});

test("an overridden field says so and offers a reset; an inherited one does neither", () => {
  const html = render();
  assert.match(html, /<span class="health overridden">overridden<\/span>/);
  assert.match(html, /<span class="health inherited">inherited<\/span>/);
  assert.match(html, /aria-label="Reset Schedule \(cron, or every \+ at\) to inherited"/);
  assert.equal((html.match(/Reset to inherited/g) ?? []).length, 1, "only the overridden field has a reset");
});

test("while the view loads, it says so and shows no fields", () => {
  const html = render({ loading: true, effective: null });
  assert.match(html, /Checking this project/);
  assert.doesNotMatch(html, /Customize for this project/);
});

test("when the view fails, it shows the failure, not the fields", () => {
  const html = render({ effective: null, error: new Error("store unavailable") });
  assert.doesNotMatch(html, /Customize for this project/);
});

test("the customize form is closed until asked, and offers only the editable fields", () => {
  const closed = render();
  assert.match(closed, /aria-expanded="false"/);
  assert.doesNotMatch(closed, /<input/);

  const open = render({ defaultCustomizeOpen: true });
  for (const label of ["Cron schedule", "Every", "At (HH:MM)", "Stagger", "Max runs", "Agent", "Appended prompt"]) {
    assert.match(open, new RegExp(`<label[^>]*>${label.replace(/[()+]/g, "\\$&")}<`), `missing label ${label}`);
  }
  assert.doesNotMatch(open, /<label[^>]*>Skip if event</, "skip_if_event is shown, not edited");
  assert.doesNotMatch(open, /<label[^>]*>Workdir</, "target_workdir is shown, not edited");
});

test("a project the filter does not select, with no binding yet, is told that saving keeps it off", () => {
  const html = render({
    binding: null,
    effective: { ...EFFECTIVE, targeted: false, binding_id: undefined, binding_status: undefined },
    defaultCustomizeOpen: true,
  });
  assert.match(html, /keeps it off/);
});

test("a failed save shows its message as an alert", () => {
  const html = render({ defaultCustomizeOpen: true, saveError: "Max runs must be a whole number." });
  assert.match(html, /role="alert"[^>]*>[^<]*Max runs must be a whole number/);
});
