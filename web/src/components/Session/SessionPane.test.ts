/**
 * SessionPane — pins the FEATURE INVARIANT: every session view has a working
 * input, live or finished, and a finished/unresolvable session shows the input
 * DISABLED with a reason rather than HIDDEN (a read-only view is never a dead
 * end). This is the exact behavior that regressed the other way pre-feature:
 * the pane rendered a composer only when `canSteer && live` held and showed
 * nothing otherwise, so a finished/dead session had no input at all.
 *
 * There is no DOM harness in this package (see Toasts.test.ts / ResumeComposer
 * .test.ts), so this asserts at two seams:
 *   • the pure route decision (sessionSubmitRoute, unit-tested exhaustively in
 *     sessionRef.test.ts) always yields one of steer|resume|disabled — never a
 *     "hide" — so the pane always has something to render;
 *   • the pane source maps ALL THREE route kinds to a rendered input element
 *     (Composer / ResumeComposer / a disabled composer note), with no branch
 *     that returns null / renders nothing for a session.
 *
 * Route selection itself (live→steer, finished→resume, missing owner→disabled)
 * lives in sessionRef.test.ts; here we only guarantee the pane HONORS every
 * outcome by rendering an input.
 */
import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

import { sessionSubmitRoute } from "../../lib/sessionRef";
import type { SessionRef } from "../../lib/types";

const src = readFileSync(
  fileURLToPath(new URL("./SessionPane.tsx", import.meta.url)),
  "utf8",
);

const LIVE: SessionRef = {
  mode: "live",
  runner_id: "r1",
  instance_id: "i1",
  session_id: "ses_1",
};
const HISTORY: SessionRef = {
  mode: "history",
  runner_id: "r1",
  session_id: "ses_1",
  task_id: "t1",
  project_id: "p1",
};
const OWNER = { project_id: "p1", task_id: "t1" };

// ─── the route decision always produces a renderable input ──────────

test("every session state resolves to a renderable input, never a hide", () => {
  // A live streaming session → steer input.
  assert.equal(sessionSubmitRoute(LIVE, "streaming", OWNER).kind, "steer");
  // A FINISHED (history) session with an owning task → resume input. This is
  // the pre-feature regression: previously this had NO input.
  assert.equal(sessionSubmitRoute(HISTORY, "none", OWNER).kind, "resume");
  // A live session whose stream ended under us → resume input, not a dead
  // steerer over an exited process.
  assert.equal(sessionSubmitRoute(LIVE, "ended", OWNER).kind, "resume");
  // The remaining states still render an input — DISABLED with a reason, never
  // hidden: starting, task-less finished session, and no ref at all.
  for (const kind of [
    sessionSubmitRoute({ ...LIVE, session_id: undefined }, "streaming", OWNER).kind,
    sessionSubmitRoute(HISTORY, "none", undefined).kind,
    sessionSubmitRoute(undefined, "none", OWNER).kind,
  ]) {
    assert.equal(kind, "disabled");
  }
});

test("a finished session with an owning task is resumable (previously inert)", () => {
  // The core acceptance: a FINISHED/dead session now routes to resume — the
  // input works. Pre-feature this session produced no steer route and the pane
  // rendered nothing.
  const r = sessionSubmitRoute(HISTORY, "none", OWNER);
  assert.deepEqual(r, { kind: "resume", project_id: "p1", task_id: "t1" });
});

// ─── the pane HONORS every route kind by rendering an input ─────────

test("SessionPane renders an input for all three route kinds", () => {
  // steer → <Composer>, resume → <ResumeComposer>, disabled → a composer note.
  // If any branch rendered null instead, a session state would silently lose
  // its input — the exact pre-feature bug.
  assert.match(src, /route\.kind === "steer" \?/);
  assert.match(src, /<Composer target=\{route\.target\}/);
  assert.match(src, /route\.kind === "resume" \?/);
  assert.match(src, /<ResumeComposer/);
  // The disabled fallback is a rendered note element, not a hidden/absent one.
  assert.match(src, /className="composer proc-chat-note"\>\{disabledNote\}/);
});

test("SessionPane no longer gates the input on canSteer (always-on input)", () => {
  // The pre-feature gate was `canSteer && live` around the composer; the whole
  // point of the feature is that this gate is gone. If it comes back the input
  // stops rendering for finished sessions again.
  assert.doesNotMatch(src, /canSteer && live/);
  assert.doesNotMatch(src, /\{canSteer &&/);
  // The route classifier is what decides now.
  assert.match(src, /sessionSubmitRoute\(sref, transcript\.delivery, owner\)/);
});

test("SessionPane resolves the owning task from the ref or the live instance", () => {
  // The resume route needs {project,task}. A history ref carries it directly;
  // a live-ended ref resolves it off the instance registry row. A missing
  // owner is the disabled-with-reason case (a clear message, not a silent
  // no-op) — proven in sessionRef.test.ts.
  assert.match(src, /sref\?\.mode === "history"/);
  assert.match(src, /project_id: sref\.project_id, task_id: sref\.task_id/);
  assert.match(src, /project_id: inst\.project_id, task_id: inst\.task_id/);
});

test("SessionPane keeps the live steer path unchanged (regression guard)", () => {
  // The live steerer must still be the same Composer fed the steer target and
  // the check-in seed — the feature must not perturb live steering.
  assert.match(src, /<Composer target=\{route\.target\} checkinSeed=\{checkinSeed\}/);
});
