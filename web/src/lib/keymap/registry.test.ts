import { strict as assert } from "node:assert";
import { test } from "node:test";
import { setTimeout as sleep } from "node:timers/promises";
import {
  dispatchChord,
  findDuplicateBindings,
  helpBarHints,
  helpModalGroups,
  isCountable,
  registerScope,
  resetSequence,
} from "./registry";
import type { ActionCtx, WhenEnv } from "./types";

const env = (over: Partial<WhenEnv> = {}): WhenEnv => ({
  focus: "tasks",
  mode: undefined,
  hasSelection: false,
  isMobile: false,
  ...over,
});

const ctx = (): ActionCtx => ({ event: {} as KeyboardEvent, count: 1 });

test("dispatch respects tier order: pane beats view beats global", () => {
  const hits: string[] = [];
  const un1 = registerScope({
    scopeId: "t:global", tier: "global",
    specs: [{ id: "g.x", keys: ["x"], desc: "g", group: "global" }],
    handlers: { "g.x": () => void hits.push("global") },
  });
  const un2 = registerScope({
    scopeId: "t:view", tier: "view",
    specs: [{ id: "v.x", keys: ["x"], desc: "v", group: "tasks" }],
    handlers: { "v.x": () => void hits.push("view") },
  });
  const un3 = registerScope({
    scopeId: "t:pane", tier: "pane",
    specs: [{ id: "p.x", keys: ["x"], desc: "p", group: "panes" }],
    handlers: { "p.x": () => void hits.push("pane") },
  });
  try {
    assert.ok(dispatchChord("x", ctx(), env()));
    assert.deepEqual(hits, ["pane"]);
  } finally {
    un1(); un2(); un3();
  }
});

test("handler returning false falls through to the next tier", () => {
  const hits: string[] = [];
  const un1 = registerScope({
    scopeId: "t:view", tier: "view",
    specs: [{ id: "v.x", keys: ["x"], desc: "v", group: "tasks" }],
    handlers: { "v.x": () => { hits.push("view"); return false; } },
  });
  const un2 = registerScope({
    scopeId: "t:global", tier: "global",
    specs: [{ id: "g.x", keys: ["x"], desc: "g", group: "global" }],
    handlers: { "g.x": () => void hits.push("global") },
  });
  try {
    assert.ok(dispatchChord("x", ctx(), env()));
    assert.deepEqual(hits, ["view", "global"]);
  } finally {
    un1(); un2();
  }
});

test("when clauses filter dispatch by focus and selection", () => {
  const hits: string[] = [];
  const un = registerScope({
    scopeId: "t:view", tier: "view",
    specs: [
      { id: "v.detailOnly", keys: ["y"], desc: "", group: "tasks", when: { focus: ["detail"] } },
      { id: "v.selOnly", keys: ["y"], desc: "", group: "tasks", when: { hasSelection: true } },
    ],
    handlers: {
      "v.detailOnly": () => void hits.push("detail"),
      "v.selOnly": () => void hits.push("sel"),
    },
  });
  try {
    assert.ok(!dispatchChord("y", ctx(), env()));
    assert.ok(dispatchChord("y", ctx(), env({ focus: "detail" })));
    assert.ok(dispatchChord("y", ctx(), env({ hasSelection: true })));
    assert.deepEqual(hits, ["detail", "sel"]);
  } finally {
    un();
  }
});

test("tier restriction limits which scopes dispatch", () => {
  const hits: string[] = [];
  const un = registerScope({
    scopeId: "t:global", tier: "global",
    specs: [{ id: "g.x", keys: ["x"], desc: "", group: "global" }],
    handlers: { "g.x": () => void hits.push("global") },
  });
  try {
    assert.ok(!dispatchChord("x", ctx(), env(), ["pane", "view"]));
    assert.ok(dispatchChord("x", ctx(), env(), ["global"]));
    assert.deepEqual(hits, ["global"]);
  } finally {
    un();
  }
});

test("two-key sequence: g g fires, broken sequence dispatches second chord", () => {
  const hits: string[] = [];
  const un = registerScope({
    scopeId: "t:view", tier: "view",
    specs: [
      { id: "v.top", keys: ["g g"], desc: "", group: "tasks" },
      { id: "v.other", keys: ["z"], desc: "", group: "tasks" },
    ],
    handlers: {
      "v.top": () => void hits.push("gg"),
      "v.other": () => void hits.push("z"),
    },
  });
  try {
    assert.ok(dispatchChord("g", ctx(), env())); // arms
    assert.ok(dispatchChord("g", ctx(), env())); // completes
    assert.deepEqual(hits, ["gg"]);

    assert.ok(dispatchChord("g", ctx(), env())); // arms again
    assert.ok(dispatchChord("z", ctx(), env())); // breaks; z dispatches normally
    assert.deepEqual(hits, ["gg", "z"]);
  } finally {
    resetSequence();
    un();
  }
});

test("chord bound both alone and as sequence prefix: single fires on timeout", async () => {
  const hits: string[] = [];
  const un = registerScope({
    scopeId: "t:view", tier: "view",
    specs: [
      { id: "v.gTop", keys: ["g"], desc: "", group: "tasks" },
      { id: "v.gg", keys: ["g g"], desc: "", group: "tasks" },
    ],
    handlers: {
      "v.gTop": () => void hits.push("g"),
      "v.gg": () => void hits.push("gg"),
    },
  });
  try {
    assert.ok(dispatchChord("g", ctx(), env())); // arms, defers single-g
    assert.deepEqual(hits, []);
    await sleep(600); // > SEQUENCE_TIMEOUT_MS
    assert.deepEqual(hits, ["g"]); // deferred single fired
    // And the sequence still works when completed in time.
    dispatchChord("g", ctx(), env());
    dispatchChord("g", ctx(), env());
    assert.deepEqual(hits, ["g", "gg"]);
  } finally {
    resetSequence();
    un();
  }
});

test("help derivation: hints honor when/hint, modal groups include help-only scopes", () => {
  const un1 = registerScope({
    scopeId: "t:view", tier: "view",
    specs: [
      { id: "v.a", keys: ["a"], desc: "Action A full", hint: "A", group: "tasks" },
      { id: "v.b", keys: ["b"], desc: "No hint", group: "tasks" },
      { id: "v.c", keys: ["c"], desc: "Detail only", hint: "C", group: "tasks", when: { focus: ["detail"] } },
    ],
    handlers: { "v.a": () => {}, "v.b": () => {}, "v.c": () => {} },
  });
  const un2 = registerScope({
    scopeId: "t:helponly", tier: "global",
    specs: [{ id: "h.x", keys: ["dbl-click"], desc: "Help only row", group: "panes" }],
  });
  try {
    const hints = helpBarHints(env());
    assert.deepEqual(hints.map((h) => h.hint), ["A"]);
    assert.deepEqual(helpBarHints(env({ isMobile: true })), []);

    const groups = helpModalGroups("tasks");
    assert.equal(groups[0].id, "tasks"); // current view first
    const panes = groups.find((g) => g.id === "panes");
    assert.ok(panes && panes.rows.some((r) => r.desc === "Help only row"));
    // Help-only scopes never dispatch.
    assert.ok(!dispatchChord("dbl-click", ctx(), env()));
  } finally {
    un1(); un2();
  }
});

test("findDuplicateBindings flags same-tier overlapping claims only", () => {
  const un1 = registerScope({
    scopeId: "t:viewA", tier: "view",
    specs: [{ id: "a.x", keys: ["x"], desc: "", group: "tasks" }],
    handlers: { "a.x": () => {} },
  });
  // Same chord, same tier, different scope, overlapping when → duplicate.
  const un2 = registerScope({
    scopeId: "t:viewB", tier: "view",
    specs: [{ id: "b.x", keys: ["x"], desc: "", group: "brain" }],
    handlers: { "b.x": () => {} },
  });
  // Same chord but disjoint focus → not a duplicate.
  const un3 = registerScope({
    scopeId: "t:viewC", tier: "view",
    specs: [{ id: "c.x", keys: ["x"], desc: "", group: "tasks", when: { focus: ["detail"] } }],
    handlers: { "c.x": () => {} },
  });
  const un4 = registerScope({
    scopeId: "t:viewD", tier: "view",
    specs: [{ id: "d.x", keys: ["x"], desc: "", group: "tasks", when: { focus: ["logs"] } }],
    handlers: { "d.x": () => {} },
  });
  try {
    const problems = findDuplicateBindings();
    assert.equal(problems.filter((p) => p.includes("a.x") && p.includes("b.x")).length, 1);
    assert.equal(problems.filter((p) => p.includes("c.x") && p.includes("d.x")).length, 0);
  } finally {
    un1(); un2(); un3(); un4();
  }
});

test("isCountable reflects spec flags in env", () => {
  const un = registerScope({
    scopeId: "t:view", tier: "view",
    specs: [
      { id: "v.down", keys: ["j"], desc: "", group: "tasks", countable: true },
      { id: "v.run", keys: ["x"], desc: "", group: "tasks" },
    ],
    handlers: { "v.down": () => {}, "v.run": () => {} },
  });
  try {
    assert.ok(isCountable("j", env()));
    assert.ok(!isCountable("x", env()));
  } finally {
    un();
  }
});
