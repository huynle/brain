import { strict as assert } from "node:assert";
import { test } from "node:test";
import { resolveCommand, suggest, type CommandCtx } from "./commands";

const ctx: CommandCtx = { projects: ["demo", "beta-proj", "brain-api"] };

test("exact names and aliases resolve to view navigation", () => {
  assert.deepEqual(resolveCommand(":tasks", ctx), { type: "navigate", view: "tasks" });
  assert.deepEqual(resolveCommand("task", ctx), { type: "navigate", view: "tasks" });
  assert.deepEqual(resolveCommand("ta", ctx), { type: "navigate", view: "tasks" });
  assert.deepEqual(resolveCommand("t", ctx), { type: "navigate", view: "tasks" });
  assert.deepEqual(resolveCommand("entries", ctx), { type: "navigate", view: "brain" });
  assert.deepEqual(resolveCommand("auto", ctx), { type: "navigate", view: "automations" });
  assert.deepEqual(resolveCommand("ru", ctx), { type: "navigate", view: "runners" });
});

test("bare projects command opens the picker; with an arg it fuzzy-switches", () => {
  assert.deepEqual(resolveCommand("po", ctx), { type: "projectPicker" });
  assert.deepEqual(resolveCommand("proj demo", ctx), { type: "projectSwitch", project: "demo" });
  assert.deepEqual(resolveCommand("proj bapi", ctx), { type: "projectSwitch", project: "brain-api" });
});

test("view command with a project argument navigates view + project", () => {
  assert.deepEqual(resolveCommand("t demo", ctx), { type: "navigate", view: "tasks", project: "demo" });
  assert.deepEqual(resolveCommand("tasks brainapi", ctx), {
    type: "navigate",
    view: "tasks",
    project: "brain-api",
  });
});

test("ambiguous or unknown project args return error with suggestions, never a guess", () => {
  const out = resolveCommand("t zzz", ctx);
  assert.equal(out.type, "error");
  const amb = resolveCommand("proj b", { projects: ["beta-a", "beta-b"] });
  assert.equal(amb.type, "error");
});

test("fuzzy command names resolve when unambiguous; junk suggests or errors", () => {
  assert.deepEqual(resolveCommand("runrs", ctx), { type: "navigate", view: "runners" });
  const out = resolveCommand("zzz", ctx);
  assert.ok(out.type === "suggest" || out.type === "error");
});

test("pause/resume target grammar: kind and scope in any order", () => {
  assert.deepEqual(resolveCommand("pause", ctx), {
    type: "pauseResume", verb: "pause", kind: "tasks", scope: "active", project: undefined,
  });
  assert.deepEqual(resolveCommand("pause autos", ctx), {
    type: "pauseResume", verb: "pause", kind: "autos", scope: "active", project: undefined,
  });
  assert.deepEqual(resolveCommand("pause all", ctx), {
    type: "pauseResume", verb: "pause", kind: "tasks", scope: "all", project: undefined,
  });
  assert.deepEqual(resolveCommand("resume all autos", ctx), {
    type: "pauseResume", verb: "resume", kind: "autos", scope: "all", project: undefined,
  });
  assert.deepEqual(resolveCommand("pause demo autos", ctx), {
    type: "pauseResume", verb: "pause", kind: "autos", scope: "project", project: "demo",
  });
  assert.deepEqual(resolveCommand("resume tasks beta", ctx), {
    type: "pauseResume", verb: "resume", kind: "tasks", scope: "project", project: "beta-proj",
  });
});

test("suggest: command names first, then argument domains per position", () => {
  const names = suggest("", ctx);
  assert.ok(names.length > 0 && names[0].insert === "tasks");

  const partial = suggest("ta", ctx);
  assert.ok(partial.some((s) => s.insert === "tasks"));

  const args = suggest("tasks ", ctx);
  assert.ok(args.every((s) => s.insert.startsWith("tasks ")));
  assert.ok(args.some((s) => s.insert === "tasks demo"));

  const pauseArgs = suggest("pause ", ctx);
  assert.ok(pauseArgs.some((s) => s.insert === "pause autos"));
  assert.ok(pauseArgs.some((s) => s.insert === "pause all"));

  // Suggestions canonicalize aliases to the primary command name.
  const projArg = suggest("proj de", ctx);
  assert.ok(projArg.some((s) => s.insert === "projects demo"));
});

test("preset commands: done/ready/merge-ready with optional project", () => {
  assert.deepEqual(resolveCommand("done", ctx), { type: "preset", preset: "done" });
  assert.deepEqual(resolveCommand("history demo", ctx), { type: "preset", preset: "done", project: "demo" });
  assert.deepEqual(resolveCommand("ready", ctx), { type: "preset", preset: "ready" });
  assert.deepEqual(resolveCommand("merge-ready", ctx), { type: "preset", preset: "merge-ready" });
  assert.deepEqual(resolveCommand("merge beta", ctx), { type: "preset", preset: "merge-ready", project: "beta-proj" });
});
