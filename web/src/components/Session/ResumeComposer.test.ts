/**
 * ResumeComposer — the FINISHED-session half of the Session view's
 * always-present input. This package has no DOM harness (see Toasts.test.ts
 * for the same constraint), so these pin the two things a unit test CAN see:
 *
 *   1. resumeModeToast — the observable UX state on a SUCCESSFUL resume. The
 *      task requires "success surfaces resume_mode"; the wording is
 *      mode-specific by ADR 7ihrqpi4 Decision 5, and must never collapse to a
 *      generic message that hides which mode (rehydrate / same_session /
 *      live_injected) actually fired.
 *   2. Source-level guarantees the render depends on and a DOM test would
 *      otherwise catch: the adaptive resume placeholder (vs the live steerer's
 *      "Steer…"), the submit-disabled-while-sending invariant, the two-step
 *      confirm before a relaunch, and that an error surfaces its message
 *      rather than being swallowed.
 *
 * These FAIL against pre-feature behavior: before this feature a finished
 * session had NO working input at all (SessionPane suppressed the composer for
 * anything that could not be steered), so neither ResumeComposer nor
 * resumeModeToast existed.
 */
import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

import { resumeModeToast } from "./ResumeComposer";

const src = readFileSync(
  fileURLToPath(new URL("./ResumeComposer.tsx", import.meta.url)),
  "utf8",
);

// ─── resumeModeToast: success surfaces the resume_mode ──────────────

test("resumeModeToast: each real resume_mode gets its own honest wording", () => {
  // live_injected must say it went into a RUNNING session (no relaunch); the
  // two relaunch modes must say the task is being relaunched/resumed. The
  // three must be distinct so the user can tell what happened.
  const injected = resumeModeToast("live_injected");
  const same = resumeModeToast("same_session");
  const rehydrate = resumeModeToast("rehydrate");

  assert.match(injected, /running session/i);
  assert.match(same, /previous session/i);
  assert.match(rehydrate, /fresh session/i);

  const all = [injected, same, rehydrate];
  assert.equal(new Set(all).size, 3, "each mode must produce distinct wording");
});

test("resumeModeToast: live_injected does NOT claim a relaunch", () => {
  // live_injected means the session was still up and the text was injected —
  // reporting a "relaunch" there would be a lie about what the backend did.
  const injected = resumeModeToast("live_injected");
  assert.doesNotMatch(injected, /relaunch/i);
  // …while the two dead-session modes are honest that work is (re)starting.
  assert.match(resumeModeToast("same_session"), /resum/i);
  assert.match(resumeModeToast("rehydrate"), /relaunch/i);
});

test("resumeModeToast: an unknown mode degrades to a safe non-empty message", () => {
  // The backend's resume_mode is a free string on the wire; a value the UI
  // does not recognise must still produce a real message, never "" or crash.
  const fallback = resumeModeToast("something_new");
  assert.ok(fallback.length > 0);
  assert.match(fallback, /context/i);
  assert.equal(resumeModeToast(""), fallback);
});

// ─── source-level UX-state guarantees (no DOM harness) ──────────────

test("ResumeComposer: adaptive placeholder is the resume wording, not steer", () => {
  // The task's "adaptive placeholder (steer vs resume)" requirement: the
  // finished-session input must invite RESUMING with context, and must not
  // borrow the live steerer's "Steer…" copy.
  assert.match(src, /placeholder="Resume with this context…"/);
  assert.doesNotMatch(src, /placeholder="Steer/);
});

test("ResumeComposer: submit is disabled while a resume is in flight", () => {
  // "submitting/disabled": the input and the button both go disabled while
  // sending, so a relaunch cannot be double-fired.
  assert.match(src, /disabled=\{sending\}/); // the text input
  assert.match(src, /disabled=\{sending \|\| !text\.trim\(\)\}/); // the button
  // The button label reflects the in-flight state.
  assert.match(src, /"Resuming…"/);
});

test("ResumeComposer: a relaunch takes a two-step confirm before firing", () => {
  // Relaunching a dead task is heavier and more visible than a live nudge, so
  // the first submit ARMS and only a second within the window fires — the
  // guard against an accidental relaunch.
  assert.match(src, /armResume/);
  assert.match(src, /"Confirm resume\?"/);
  assert.match(src, /setArmResume\(true\)/);
});

test("ResumeComposer: an error surfaces its message rather than being swallowed", () => {
  // "error surfaces message": a failed resume (400 empty, 404 not-found,
  // live-claim-safety refusal, …) must toast the error text, and the typed
  // text must be kept for a retry (only cleared on success).
  assert.match(src, /catch \(err\)/);
  assert.match(src, /Resume failed: \$\{\(err as Error\)\?\.message \?\? err\}/);
  assert.match(src, /"error"/);
  // Success clears the text; the clear must NOT be in the catch branch.
  const catchBody = src.slice(src.indexOf("catch (err)"));
  assert.doesNotMatch(
    catchBody.slice(0, catchBody.indexOf("finally")),
    /setText\(""\)/,
    "typed text must be preserved on error for retry",
  );
});

test("ResumeComposer: submit calls resume-with-context with the typed text as injected_context", () => {
  // Route → backend contract: a finished-session submit relaunches via
  // resumeTaskWithContext, passing the typed text as injected_context and
  // prefer_same_session:true (the live steer/prompt_async path is NOT used).
  assert.match(src, /resumeTaskWithContext\(/);
  assert.match(src, /injected_context: body/);
  assert.match(src, /prefer_same_session: true/);
  // The live steer path must not be reachable from here.
  assert.doesNotMatch(src, /prompt_async|controlPrompt/);
});
