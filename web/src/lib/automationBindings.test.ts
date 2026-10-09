/**
 * Tests for lib/automationBindings — the pure side of per-project
 * customization of global automations.
 *
 * Worth pinning: bindings never surface as rows of their own; an
 * opt-out is a binding write that never touches the global entry; a
 * status change never sends trigger/action (which would wipe overrides);
 * a customize round-trip rebuilds the same overrides; a reset removes
 * exactly one field group; a broken view has a message.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  FIELD_GROUPS,
  OPT_OUT_STATUS,
  automationEffectivePath,
  bindingCreateBody,
  bindingFor,
  bindingPatchBody,
  bindingProjectOf,
  bindingStatusPatch,
  brokenMessage,
  describeFields,
  draftFromOverrides,
  emptyOverrides,
  hasOverrides,
  mergeAutomationRows,
  overridesFromDraft,
  overridesOf,
  planTurnOff,
  planTurnOn,
  resetGroup,
  type AutomationEffectiveView,
  type BindingDraft,
} from "./automationBindings";
import type { BrainEntry } from "./types";

const PARENT = {
  id: "nightly",
  path: "global/automation/nightly.md",
  title: "Nightly",
  type: "automation",
  status: "active",
  content: "",
  created: "2026-09-01T00:00:00Z",
} as BrainEntry;

const OWNED = {
  id: "owned1",
  path: "projects/p1/automation/owned1.md",
  project_id: "p1",
  title: "Owned",
  type: "automation",
  status: "active",
  content: "",
} as BrainEntry;

const BINDING_PATH = "projects/p1/automation/nightly--bind0001.md";

function binding(over: Partial<BrainEntry> = {}): BrainEntry {
  return {
    id: "bind0001",
    path: BINDING_PATH,
    title: "Nightly",
    type: "automation",
    status: "active",
    content: "",
    project_id: "p1",
    extends: "nightly",
    created: "2026-10-01T00:00:00Z",
    ...over,
  } as BrainEntry;
}

function blankDraft(): BindingDraft {
  return draftFromOverrides(emptyOverrides());
}

const EFFECTIVE: AutomationEffectiveView = {
  id: "nightly",
  project: "p1",
  automation: {
    id: "nightly",
    trigger: { type: "cron", every: "1d", at: "03:00", stagger: "2h" },
    action: { agent: "dream", model: "base" },
    timezone: "UTC",
  },
  fields: {
    "trigger.schedule": "inherited",
    "trigger.every": "overridden",
    "trigger.at": "overridden",
    "trigger.stagger": "inherited",
    "action.agent": "inherited",
    "action.model": "inherited",
    timezone: "inherited",
  },
  binding_id: "bind0001",
  binding_status: "active",
  targeted: true,
  broken: false,
};

// ─── Rows and ownership ──────────────────────────────────────────

test("a binding never appears as a row; it is returned on its own", () => {
  const { rows, bindings } = mergeAutomationRows([binding(), OWNED], [PARENT]);
  assert.deepEqual(rows.map((r) => r.id).sort(), ["nightly", "owned1"]);
  assert.deepEqual(bindings.map((b) => b.id), ["bind0001"]);
});

test("an entry present in both the scoped and global lists is one row", () => {
  const { rows } = mergeAutomationRows([PARENT, OWNED], [PARENT]);
  assert.deepEqual(rows.map((r) => r.id).sort(), ["nightly", "owned1"]);
});

test("bindingProjectOf reads project_id, falls back to the path, and is empty for global entries", () => {
  assert.equal(bindingProjectOf(binding()), "p1");
  assert.equal(
    bindingProjectOf(binding({ project_id: undefined, path: "projects/p9/automation/x.md" })),
    "p9",
  );
  assert.equal(bindingProjectOf(PARENT), "");
});

test("bindingFor returns this project's binding, oldest first, and ignores other projects", () => {
  const newer = binding({ id: "b-new", created: "2026-10-05T00:00:00Z" });
  const older = binding({ id: "b-old", created: "2026-10-02T00:00:00Z" });
  const other = binding({
    id: "b-p2",
    project_id: "p2",
    path: "projects/p2/automation/nightly--b-p2.md",
  });
  assert.equal(bindingFor([newer, other, older], "nightly", "p1")?.id, "b-old");
  assert.equal(bindingFor([other], "nightly", "p1"), null);
  assert.equal(bindingFor([older], "some-other-parent", "p1"), null);
});

test("an opt-out in one project leaves another project's binding untouched", () => {
  const p1Off = binding({ status: "archived" });
  const p2 = binding({
    id: "b-p2",
    project_id: "p2",
    path: "projects/p2/automation/nightly--b-p2.md",
    status: "active",
  });
  assert.equal(bindingFor([p1Off, p2], "nightly", "p2")?.status, "active");
  assert.equal(bindingFor([p1Off, p2], "nightly", "p3"), null);
});

test("the opt-out status is archived, the only non-active status the API accepts", () => {
  assert.equal(OPT_OUT_STATUS, "archived");
});

// ─── Overrides: reading and resetting ────────────────────────────

test("overridesOf reads only what a binding sets, with timezone lifted to the top level", () => {
  const o = overridesOf(
    binding({
      trigger: {
        every: "1d",
        at: "06:30",
        stagger: "2h",
        catch_up: "none",
        timezone: "America/New_York",
        skip_if_event: { calendar: "xnys" },
      },
      action: { agent: "dream", prompt_append: "Also check X" },
    }),
  );
  assert.deepEqual(o.trigger, {
    every: "1d",
    at: "06:30",
    stagger: "2h",
    catch_up: "none",
    skip_if_event: { calendar: "xnys" },
  });
  assert.equal(o.timezone, "America/New_York");
  assert.deepEqual(o.action, { agent: "dream", prompt_append: "Also check X" });
  assert.equal(hasOverrides(o), true);
});

test("a binding that sets nothing has no overrides", () => {
  assert.equal(hasOverrides(overridesOf(binding())), false);
  assert.equal(hasOverrides(overridesOf(null)), false);
});

test("resetGroup removes one field and keeps every other override", () => {
  const o = overridesOf(
    binding({
      trigger: { every: "1d", at: "06:30", stagger: "2h" },
      action: { agent: "dream", model: "m2" },
    }),
  );
  const next = resetGroup(o, "stagger");
  assert.deepEqual(next.trigger, { every: "1d", at: "06:30" });
  assert.deepEqual(next.action, { agent: "dream", model: "m2" });
  assert.equal(o.trigger.stagger, "2h", "resetGroup must not mutate its input");
});

test("resetting the timing group clears schedule, every and at together", () => {
  const every = overridesOf(binding({ trigger: { every: "1d", at: "06:30", stagger: "2h" } }));
  assert.deepEqual(resetGroup(every, "timing").trigger, { stagger: "2h" });
  const cron = overridesOf(binding({ trigger: { schedule: "0 6 * * *" } }));
  assert.deepEqual(resetGroup(cron, "timing").trigger, {});
});

test("resetting a top-level field clears only that field", () => {
  const o = overridesOf(
    binding({
      timezone: "Europe/London",
      starts_at: "2026-11-01T00:00:00Z",
      max_runs: 5,
    } as Partial<BrainEntry>),
  );
  const next = resetGroup(o, "timezone");
  assert.equal(next.timezone, undefined);
  assert.equal(next.starts_at, "2026-11-01T00:00:00Z");
  assert.equal(next.max_runs, 5);
});

// ─── Customize form: draft round-trip ────────────────────────────

test("a draft round-trips to the same overrides when nothing is edited", () => {
  const o = overridesOf(
    binding({
      trigger: { every: "1d", at: "06:30", stagger: "2h", catch_up: "none", calendar: "xnys" },
      timezone: "UTC",
      starts_at: "2026-11-01T00:00:00Z",
      expires_at: "2027-11-01T00:00:00Z",
      max_runs: -1,
      action: {
        agent: "dream",
        model: "m",
        executor: "opencode",
        prompt_append: "x",
        target_workdir: "/w",
        timeout: "5m",
      },
    } as Partial<BrainEntry>),
  );
  assert.notDeepEqual(o, emptyOverrides(), "the fixture must carry overrides, or the round-trip proves nothing");
  assert.deepEqual(overridesFromDraft(o, draftFromOverrides(o)), o);
});

test("an edited draft sets, clears and moves overrides", () => {
  const o = overridesOf(binding({ trigger: { every: "1d", at: "06:30" }, action: { agent: "dream" } }));
  const edited: BindingDraft = {
    ...draftFromOverrides(o),
    schedule: "0 9 * * 1-5",
    every: "",
    at: "",
    agent: "",
    model: "gpt",
    max_runs: "0",
    timezone: "Europe/Paris",
  };
  const next = overridesFromDraft(o, edited);
  assert.deepEqual(next.trigger, { schedule: "0 9 * * 1-5" }, "a schedule replaces every and at as one unit");
  assert.deepEqual(next.action, { model: "gpt" });
  assert.equal(next.max_runs, undefined, "max_runs 0 inherits, so it is no override");
  assert.equal(next.timezone, "Europe/Paris");
});

test("max_runs takes -1 for unlimited and rejects a non-integer", () => {
  assert.equal(overridesFromDraft(emptyOverrides(), { ...blankDraft(), max_runs: "-1" }).max_runs, -1);
  assert.throws(
    () => overridesFromDraft(emptyOverrides(), { ...blankDraft(), max_runs: "soon" }),
    /whole number/,
  );
});

// ─── Payloads ────────────────────────────────────────────────────

test("a full save replaces trigger and action and clears removed lifecycle fields", () => {
  const body = bindingPatchBody(
    { trigger: { every: "1d", at: "06:30" }, action: { prompt_append: "x" } },
    "active",
  );
  assert.deepEqual(body, {
    status: "active",
    trigger: { every: "1d", at: "06:30" },
    action: { prompt_append: "x" },
    timezone: "",
    starts_at: "",
    expires_at: "",
    max_runs: 0,
  });
});

test("a status-only change never sends trigger or action, so it cannot wipe overrides", () => {
  assert.deepEqual(bindingStatusPatch("archived"), { status: "archived" });
});

test("a create body is a project-owned binding that extends the parent, never a global entry", () => {
  const body = bindingCreateBody(
    PARENT,
    "p1",
    { trigger: { every: "1d", at: "06:30" }, action: { agent: "dream" } },
    "active",
  );
  assert.equal(body.type, "automation");
  assert.equal(body.project, "p1");
  assert.equal(body.extends, "nightly");
  assert.equal(body.status, "active");
  assert.equal(body.title, "Nightly");
  assert.equal("global" in body, false);
  assert.ok(String(body.content).length > 0, "the API requires content on create");
  assert.equal("direct_prompt" in body, false, "a binding never replaces the prompt");
  assert.equal("type" in (body.trigger as object), false, "overrides carry no trigger type");
});

test("a create body omits every override that is not set", () => {
  const body = bindingCreateBody(PARENT, "p1", emptyOverrides(), "archived");
  for (const key of ["trigger", "action", "timezone", "starts_at", "expires_at", "max_runs"]) {
    assert.equal(key in body, false, `${key} must be absent`);
  }
  assert.equal(body.status, "archived");
});

// ─── Toggle plans ────────────────────────────────────────────────

test("turning off with no binding creates an archived binding for this project", () => {
  assert.deepEqual(planTurnOff(null), { kind: "create", status: "archived" });
});

test("turning off an active binding archives the binding, never the global entry", () => {
  const plan = planTurnOff(binding());
  assert.deepEqual(plan, { kind: "patch", path: BINDING_PATH, status: "archived" });
  assert.notEqual(plan.kind === "patch" ? plan.path : "", PARENT.path);
});

test("turning off an opt-out that already exists does nothing", () => {
  assert.deepEqual(planTurnOff(binding({ status: "archived" })), { kind: "none" });
});

test("turning on with no binding opts this project in", () => {
  assert.deepEqual(planTurnOn(null), { kind: "create", status: "active" });
});

test("turning on drops an opt-out that carries no overrides", () => {
  assert.deepEqual(planTurnOn(binding({ status: "archived" })), { kind: "delete", path: BINDING_PATH });
});

test("turning on reactivates an opt-out that still carries overrides", () => {
  const withOverrides = binding({ status: "archived", trigger: { every: "1d", at: "06:30" } });
  assert.deepEqual(planTurnOn(withOverrides), { kind: "patch", path: BINDING_PATH, status: "active" });
});

test("turning on an active binding does nothing", () => {
  assert.deepEqual(planTurnOn(binding()), { kind: "none" });
});

// ─── Effective view ──────────────────────────────────────────────

test("the effective path encodes the id and carries the project as a query", () => {
  assert.deepEqual(automationEffectivePath("global/x y", "p 1"), {
    path: "/api/v1/automations/global%2Fx%20y/effective",
    query: { project: "p 1" },
  });
});

test("describeFields labels each group inherited or overridden from the effective view", () => {
  const rows = describeFields(EFFECTIVE, overridesOf(binding({ trigger: { every: "1d", at: "06:30" } })));
  assert.deepEqual(rows.map((r) => r.id), FIELD_GROUPS.map((g) => g.id));
  const timing = rows.find((r) => r.id === "timing");
  assert.equal(timing?.state, "overridden");
  assert.equal(timing?.value, "every 1d at 03:00");
  assert.equal(rows.find((r) => r.id === "stagger")?.state, "inherited");
  assert.equal(rows.find((r) => r.id === "agent")?.value, "dream");
  assert.equal(rows.find((r) => r.id === "timezone")?.value, "UTC");
});

test("only the agreed fields are editable in the customize form", () => {
  assert.deepEqual(
    FIELD_GROUPS.filter((g) => g.editable).map((g) => g.id).sort(),
    [
      "agent",
      "calendar",
      "catch_up",
      "executor",
      "expires_at",
      "max_runs",
      "model",
      "prompt_append",
      "stagger",
      "starts_at",
      "timezone",
      "timing",
    ].sort(),
  );
});

test("every broken reason has its own message, and an unknown one still warns", () => {
  assert.match(brokenMessage("parent_missing"), /no longer exists/);
  assert.match(brokenMessage("duplicate_binding"), /more than one binding/i);
  assert.match(brokenMessage("parent_not_automation"), /no longer a global automation/);
  assert.match(brokenMessage("something_new"), /cannot be trusted/);
});
