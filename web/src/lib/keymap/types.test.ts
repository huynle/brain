import { strict as assert } from "node:assert";
import { test } from "node:test";
import { chordOf, matchesWhen, prettyChord, type WhenEnv } from "./types";

const ev = (over: Partial<Parameters<typeof chordOf>[0]>) => ({
  key: "a",
  code: "KeyA",
  ctrlKey: false,
  metaKey: false,
  altKey: false,
  shiftKey: false,
  ...over,
});

test("chordOf: bare printables keep case (shift folding)", () => {
  assert.equal(chordOf(ev({ key: "g", code: "KeyG" })), "g");
  assert.equal(chordOf(ev({ key: "G", code: "KeyG", shiftKey: true })), "G");
  assert.equal(chordOf(ev({ key: "?", code: "Slash", shiftKey: true })), "?");
  assert.equal(chordOf(ev({ key: "{", code: "BracketLeft", shiftKey: true })), "{");
});

test("chordOf: modifiers in fixed order, ctrl/meta letters lowercased", () => {
  assert.equal(chordOf(ev({ key: "d", code: "KeyD", ctrlKey: true })), "C-d");
  assert.equal(chordOf(ev({ key: "D", code: "KeyD", ctrlKey: true, shiftKey: true })), "C-d");
  assert.equal(chordOf(ev({ key: ".", code: "Period", metaKey: true })), "M-.");
  assert.equal(chordOf(ev({ key: "d", code: "KeyD", ctrlKey: true, metaKey: true })), "C-M-d");
});

test("chordOf: Alt recovers physical key from code (macOS Option composing)", () => {
  // macOS Option+J produces key "∆" but code "KeyJ".
  assert.equal(chordOf(ev({ key: "∆", code: "KeyJ", altKey: true })), "A-j");
  assert.equal(chordOf(ev({ key: "Ó", code: "KeyH", altKey: true, shiftKey: true })), "A-H");
});

test("chordOf: explicit S- only for non-printables; pure modifiers are null", () => {
  assert.equal(chordOf(ev({ key: "Tab", code: "Tab", shiftKey: true })), "S-Tab");
  assert.equal(chordOf(ev({ key: "Tab", code: "Tab" })), "Tab");
  assert.equal(chordOf(ev({ key: "Shift", code: "ShiftLeft", shiftKey: true })), null);
});

test("matchesWhen filters by focus, mode, selection", () => {
  const env: WhenEnv = { focus: "tasks", mode: "done", hasSelection: false, isMobile: false };
  assert.ok(matchesWhen(undefined, env));
  assert.ok(matchesWhen({ focus: ["tasks"] }, env));
  assert.ok(!matchesWhen({ focus: ["detail"] }, env));
  assert.ok(matchesWhen({ mode: "done" }, env));
  assert.ok(!matchesWhen({ mode: "tasks" }, env));
  assert.ok(matchesWhen({ hasSelection: false }, env));
  assert.ok(!matchesWhen({ hasSelection: true }, env));
});

test("prettyChord renders human forms", () => {
  assert.equal(prettyChord("C-d"), "Ctrl-D");
  assert.equal(prettyChord("M-."), "⌘.");
  assert.equal(prettyChord("A-j"), "Alt-J");
  assert.equal(prettyChord("S-Tab"), "Shift-Tab");
  assert.equal(prettyChord("g g"), "gg");
  assert.equal(prettyChord("Escape"), "Esc");
  assert.equal(prettyChord("Space"), "Space");
});

test("chordOf spells out the space key (sequence-separator collision)", () => {
  assert.equal(chordOf(ev({ key: " ", code: "Space" })), "Space");
});
