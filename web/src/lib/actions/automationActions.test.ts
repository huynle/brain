/**
 * Tests for lib/actions/automationActions.
 *
 * Worth pinning: enable/pause are a status-aware pair with exactly one
 * enabled for the active/archived states (both enabled for "blocked" —
 * an errored automation can be stopped or re-armed), delete demands
 * typing the automation id and is disabled outright for built-ins
 * (the server recreates them), and run stays enabled even while
 * paused so an operator can test before re-enabling.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  buildAutomationActions,
  deleteAutomationBlockedReason,
  enableAutomationBlockedReason,
  isBuiltinAutomation,
  isEnabledAutomation,
  pauseAutomationBlockedReason,
  type AutomationActionContext,
} from "./automationActions";
import { isEnabled } from "./types";
import type { BrainEntry } from "../types";

function mkAutomation(over: Partial<BrainEntry> = {}): BrainEntry {
  return {
    id: "nightly-cleanup",
    path: "projects/p/automation/nightly-cleanup.md",
    title: "Nightly Cleanup",
    status: "active",
    ...over,
  } as BrainEntry;
}

function recorder() {
  const calls: string[] = [];
  const ctx: AutomationActionContext = {
    runAutomation: async (a) => void calls.push(`run:${a.id}`),
    enableAutomation: async (a) => void calls.push(`enable:${a.id}`),
    pauseAutomation: async (a) => void calls.push(`pause:${a.id}`),
    deleteAutomation: async (a) => void calls.push(`delete:${a.id}`),
    openDetails: (a) => void calls.push(`details:${a.id}`),
    openHistory: (a) => void calls.push(`history:${a.id}`),
    openRunsPane: (a) => void calls.push(`runs-pane:${a.id}`),
  };
  return { calls, ctx };
}

function byId(a: BrainEntry, ctx: AutomationActionContext) {
  return new Map(buildAutomationActions(a, ctx).map((act) => [act.id, act]));
}

// ─── presence ──────────────────────────────────────────────────────

test("every automation verb is present regardless of status", () => {
  const { ctx } = recorder();
  for (const status of ["active", "archived", "blocked"]) {
    const ids = [...byId(mkAutomation({ status }), ctx).keys()];
    assert.deepEqual(ids.sort(), [
      "delete",
      "details",
      "enable",
      "history",
      "pause",
      "run",
      "runs-pane",
    ]);
  }
});

// ─── state pair ────────────────────────────────────────────────────

test("active: pause enabled, enable disabled", () => {
  const a = mkAutomation({ status: "active" });
  assert.equal(pauseAutomationBlockedReason(a), "");
  assert.notEqual(enableAutomationBlockedReason(a), "");
  assert.equal(isEnabledAutomation(a), true);
});

test("archived: enable enabled, pause disabled", () => {
  const a = mkAutomation({ status: "archived" });
  assert.equal(enableAutomationBlockedReason(a), "");
  assert.notEqual(pauseAutomationBlockedReason(a), "");
  assert.equal(isEnabledAutomation(a), false);
});

test("blocked (errored): both state verbs enabled", () => {
  const a = mkAutomation({ status: "blocked" });
  assert.equal(enableAutomationBlockedReason(a), "");
  assert.equal(pauseAutomationBlockedReason(a), "");
});

test("run stays enabled while paused — testing before re-enabling", () => {
  const { ctx } = recorder();
  const run = byId(mkAutomation({ status: "archived" }), ctx).get("run")!;
  assert.equal(isEnabled(run), true);
});

// ─── delete ────────────────────────────────────────────────────────

test("delete demands typing the automation id", () => {
  const { ctx } = recorder();
  const del = byId(mkAutomation(), ctx).get("delete")!;
  assert.equal(del.danger, true);
  assert.equal(del.confirm?.typeToConfirm, "nightly-cleanup");
});

test("delete is disabled for built-ins", () => {
  const a = mkAutomation({
    generated_by: "brain:builtin-feature-checkout",
  });
  assert.equal(isBuiltinAutomation(a), true);
  assert.notEqual(deleteAutomationBlockedReason(a), "");
  assert.equal(isBuiltinAutomation(mkAutomation()), false);
  assert.equal(deleteAutomationBlockedReason(mkAutomation()), "");
});

// ─── effects route to the context ──────────────────────────────────

test("verbs call their context effects with the entry", async () => {
  const { calls, ctx } = recorder();
  const acts = byId(mkAutomation({ status: "blocked" }), ctx);
  for (const id of ["run", "enable", "pause", "details", "delete"]) {
    await acts.get(id)!.run();
  }
  assert.deepEqual(calls, [
    "run:nightly-cleanup",
    "enable:nightly-cleanup",
    "pause:nightly-cleanup",
    "details:nightly-cleanup",
    "delete:nightly-cleanup",
  ]);
});

// ─── history navigation ────────────────────────────────────────────

test("history verbs are always enabled and route to their own surfaces", async () => {
  const { calls, ctx } = recorder();
  const a = mkAutomation({ status: "archived" });
  const acts = byId(a, ctx);

  // A paused automation still has a past worth reading — that is often
  // exactly why it was paused.
  assert.equal(isEnabled(acts.get("history")!), true);
  assert.equal(isEnabled(acts.get("runs-pane")!), true);

  await acts.get("history")!.run();
  await acts.get("runs-pane")!.run();
  assert.deepEqual(calls, [
    `history:${a.id}`,
    `runs-pane:${a.id}`,
  ]);
});
