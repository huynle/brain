/**
 * shell — unit tests for the runner terminal's pure layer.
 *
 * Replaces the old mockShell tests: the shell is no longer a synthetic
 * command table, so there is nothing left to test about `status`/`ps`/
 * `uname`. What IS testable without a DOM:
 *   - command echo formatting
 *   - built-in interception (help / clear / exit) and, crucially, that
 *     everything else returns null so it reaches the network
 *   - chunk→line accumulation, including the subtle case: a chunk that
 *     does not end in a newline must be neither lost nor duplicated
 *   - exit-code / error rendering
 *   - readline history push + recall
 *
 * The React component (RunnerShell.tsx) is deliberately NOT tested here
 * — this repo has no DOM test setup, and adding one is out of scope.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  MAX_HISTORY,
  errorLine,
  exitLine,
  flushAccumulator,
  greetingLine,
  interceptLocalCommand,
  newAccumulator,
  promptEcho,
  pushChunk,
  pushHistory,
  recallHistory,
  truncationLine,
  type ShellLine,
} from "./shell";

const RUNNER = "runner-abc";

const texts = (lines: ShellLine[]) => lines.map((l) => l.text);
const joined = (lines: ShellLine[]) => texts(lines).join("\n");

// ─── command echo ────────────────────────────────────────────────

test("promptEcho renders `<runner>$ <cmd>` as a 'cmd' line", () => {
  const echo = promptEcho(RUNNER, "ls -la /tmp");
  assert.equal(echo.kind, "cmd");
  assert.equal(echo.text, "runner-abc$ ls -la /tmp");
});

test("promptEcho does not mangle shell metacharacters", () => {
  const echo = promptEcho(RUNNER, `grep -E "a|b" . | wc -l`);
  assert.equal(echo.text, `runner-abc$ grep -E "a|b" . | wc -l`);
});

test("greetingLine names the runner and is dim chrome", () => {
  const g = greetingLine(RUNNER);
  assert.equal(g.kind, "dim");
  assert.match(g.text, /runner-abc/);
  assert.match(g.text, /help/);
});

// ─── built-in interception ───────────────────────────────────────

test("interceptLocalCommand: arbitrary commands are NOT intercepted", () => {
  for (const cmd of ["ls", "git status", "clearcache", "exitcode", "helper"]) {
    assert.equal(
      interceptLocalCommand(RUNNER, cmd),
      null,
      `${cmd} must be dispatched to the runner, not handled locally`,
    );
  }
});

test("interceptLocalCommand: clear wipes with no output lines", () => {
  const r = interceptLocalCommand(RUNNER, "clear");
  assert.ok(r);
  assert.equal(r.action, "clear");
  assert.deepEqual(r.lines, []);
});

test("interceptLocalCommand: exit echoes then asks the caller to close", () => {
  for (const cmd of ["exit", "quit"]) {
    const r = interceptLocalCommand(RUNNER, cmd);
    assert.ok(r, `${cmd} should be intercepted`);
    assert.equal(r.action, "exit");
    assert.equal(r.lines[0].kind, "cmd");
    const farewell = r.lines[r.lines.length - 1];
    assert.equal(farewell.kind, "dim");
    assert.match(farewell.text, /bye|closing|goodbye/i);
  }
});

test("interceptLocalCommand: help documents the real shell, not a mock", () => {
  const r = interceptLocalCommand(RUNNER, "help");
  assert.ok(r);
  assert.equal(r.action, "print");
  const body = joined(r.lines);
  for (const built of ["help", "clear", "exit"]) {
    assert.match(body, new RegExp(`\\b${built}\\b`), `help should list ${built}`);
  }
  assert.match(body, /Ctrl\+C/, "help should document the interrupt key");
  assert.match(body, /runner-abc/, "help should name the target runner");
  assert.doesNotMatch(body, /mock/i, "help must not still claim to be a mock");
});

test("interceptLocalCommand: '?' is an alias for help", () => {
  const r = interceptLocalCommand(RUNNER, "?");
  assert.ok(r);
  assert.equal(r.action, "print");
  assert.match(joined(r.lines), /Built-ins/);
});

test("interceptLocalCommand: whitespace is trimmed before matching", () => {
  const r = interceptLocalCommand(RUNNER, "   clear   ");
  assert.ok(r);
  assert.equal(r.action, "clear");
});

test("interceptLocalCommand: empty input prints nothing and never errors", () => {
  for (const raw of ["", "   "]) {
    const r = interceptLocalCommand(RUNNER, raw);
    assert.ok(r);
    assert.equal(r.action, "print");
    assert.deepEqual(r.lines, []);
  }
});

// ─── chunk → line accumulation ───────────────────────────────────

test("pushChunk: a whole line arrives as one 'out' line", () => {
  const acc = newAccumulator();
  assert.deepEqual(pushChunk(acc, "stdout", "hello\n"), [
    { kind: "out", text: "hello" },
  ]);
  assert.deepEqual(flushAccumulator(acc), []);
});

test("pushChunk: a partial chunk emits nothing and is not lost", () => {
  const acc = newAccumulator();
  // The subtle one: chunks are cut at byte boundaries, so this is the
  // common case, not an edge case.
  assert.deepEqual(pushChunk(acc, "stdout", "hel"), []);
  assert.deepEqual(pushChunk(acc, "stdout", "lo wor"), []);
  assert.deepEqual(pushChunk(acc, "stdout", "ld\n"), [
    { kind: "out", text: "hello world" },
  ]);
  // …and nothing is emitted a second time on flush.
  assert.deepEqual(flushAccumulator(acc), []);
});

test("pushChunk: an unterminated tail survives until flush, exactly once", () => {
  const acc = newAccumulator();
  assert.deepEqual(pushChunk(acc, "stdout", "a\nb\nc"), [
    { kind: "out", text: "a" },
    { kind: "out", text: "b" },
  ]);
  assert.deepEqual(flushAccumulator(acc), [{ kind: "out", text: "c" }]);
  // Idempotent: a second flush must not duplicate the tail.
  assert.deepEqual(flushAccumulator(acc), []);
});

test("pushChunk: the concatenation of all emitted text equals the input", () => {
  // Property check across a pathological chopping of one payload.
  const payload = "alpha\nbeta\n\ngamma line\ndelta (no newline)";
  const acc = newAccumulator();
  const seen: string[] = [];
  for (let i = 0; i < payload.length; i++) {
    for (const l of pushChunk(acc, "stdout", payload[i])) seen.push(l.text);
  }
  for (const l of flushAccumulator(acc)) seen.push(l.text);
  assert.equal(seen.join("\n"), payload);
});

test("pushChunk: blank lines are preserved, not collapsed", () => {
  const acc = newAccumulator();
  const lines = pushChunk(acc, "stdout", "one\n\n\ntwo\n");
  assert.deepEqual(texts(lines), ["one", "", "", "two"]);
});

test("pushChunk: stderr becomes 'err' lines", () => {
  const acc = newAccumulator();
  assert.deepEqual(pushChunk(acc, "stderr", "boom\n"), [
    { kind: "err", text: "boom" },
  ]);
});

test("pushChunk: stdout and stderr buffer independently", () => {
  const acc = newAccumulator();
  assert.deepEqual(pushChunk(acc, "stdout", "out-par"), []);
  assert.deepEqual(pushChunk(acc, "stderr", "err-par"), []);
  assert.deepEqual(pushChunk(acc, "stdout", "t\n"), [
    { kind: "out", text: "out-part" },
  ]);
  assert.deepEqual(pushChunk(acc, "stderr", "t\n"), [
    { kind: "err", text: "err-part" },
  ]);
});

test("pushChunk: CRLF collapses to one newline even when split across chunks", () => {
  const acc = newAccumulator();
  assert.deepEqual(pushChunk(acc, "stdout", "line\r"), []);
  assert.deepEqual(pushChunk(acc, "stdout", "\nnext\r\n"), [
    { kind: "out", text: "line" },
    { kind: "out", text: "next" },
  ]);
  assert.deepEqual(flushAccumulator(acc), []);
});

test("pushChunk: a bare CR overwrites the line, terminal-style", () => {
  const acc = newAccumulator();
  const lines = pushChunk(acc, "stdout", "10%\r50%\r100%\n");
  assert.deepEqual(lines, [{ kind: "out", text: "100%" }]);
});

test("pushChunk: an empty chunk is a no-op", () => {
  const acc = newAccumulator();
  assert.deepEqual(pushChunk(acc, "stdout", ""), []);
  assert.deepEqual(flushAccumulator(acc), []);
});

test("flushAccumulator: emits stdout before stderr, then resets", () => {
  const acc = newAccumulator();
  pushChunk(acc, "stdout", "o");
  pushChunk(acc, "stderr", "e");
  assert.deepEqual(flushAccumulator(acc), [
    { kind: "out", text: "o" },
    { kind: "err", text: "e" },
  ]);
  assert.deepEqual(acc, { stdout: "", stderr: "" });
});

// ─── exit + error rendering ──────────────────────────────────────

test("exitLine: a clean exit is silent", () => {
  assert.equal(exitLine(0), null);
  assert.equal(exitLine(0, ""), null);
  assert.equal(exitLine(0, "   "), null);
});

test("exitLine: a non-zero code renders a dim marker carrying the code", () => {
  const l = exitLine(127);
  assert.ok(l);
  assert.equal(l.kind, "dim");
  assert.match(l.text, /127/);
  assert.match(l.text, /exit/i);
});

test("exitLine: a transport/spawn error wins over the code and is an err", () => {
  const l = exitLine(0, "runner not connected");
  assert.ok(l);
  assert.equal(l.kind, "err");
  assert.match(l.text, /runner not connected/);
});

test("exitLine: a Ctrl+C interrupt is not reported as a failure", () => {
  // The runner reports SIGINT as "terminated by signal interrupt" with code
  // 130. The user asked for it, so it must not read as an error.
  const l = exitLine(130, "terminated by signal interrupt");
  assert.ok(l);
  assert.equal(l.kind, "warn");
  assert.doesNotMatch(l.text, /failed/);
  assert.match(l.text, /terminated by signal interrupt/);
});

test("exitLine: hitting the server timeout is a warning, not a failure", () => {
  const l = exitLine(124, "command timed out after 15m0s");
  assert.ok(l);
  assert.equal(l.kind, "warn");
  assert.doesNotMatch(l.text, /failed/);
});

test("truncationLine: nothing dropped renders nothing", () => {
  assert.equal(truncationLine(0, 0), null);
  assert.equal(truncationLine(0, 4096), null);
});

test("truncationLine: dropped output is announced with its counts", () => {
  // Silent truncation is the failure mode this exists to prevent: the user
  // must be able to see that the transcript is incomplete.
  const l = truncationLine(12, 196608);
  assert.ok(l);
  assert.equal(l.kind, "warn");
  assert.match(l.text, /truncated/i);
  assert.match(l.text, /12 chunks/);
  assert.match(l.text, /196608 bytes/);
});

test("truncationLine: a single dropped chunk reads as singular", () => {
  const l = truncationLine(1, 16384);
  assert.ok(l);
  assert.match(l.text, /1 chunk\b/);
});

test("errorLine: Error, string, and junk all render as a single err line", () => {
  assert.deepEqual(errorLine(new Error("403 forbidden")), {
    kind: "err",
    text: "403 forbidden",
  });
  assert.deepEqual(errorLine("nope"), { kind: "err", text: "nope" });
  const junk = errorLine({ weird: true });
  assert.equal(junk.kind, "err");
  assert.ok(junk.text.length > 0, "an unrenderable throw still gets a message");
});

// ─── history ─────────────────────────────────────────────────────

test("pushHistory: appends, skips blanks, and skips immediate dupes", () => {
  let h: string[] = [];
  h = pushHistory(h, "ls");
  h = pushHistory(h, "  ");
  h = pushHistory(h, "ls");
  h = pushHistory(h, "pwd");
  h = pushHistory(h, "ls");
  assert.deepEqual(h, ["ls", "pwd", "ls"]);
});

test("pushHistory: caps the ring at max, keeping the newest", () => {
  let h: string[] = [];
  for (let i = 0; i < 10; i++) h = pushHistory(h, `cmd${i}`, 3);
  assert.deepEqual(h, ["cmd7", "cmd8", "cmd9"]);
  assert.ok(MAX_HISTORY > 0);
});

test("recallHistory: up walks back from the live line and clamps at oldest", () => {
  const h = ["a", "b", "c"];
  const one = recallHistory(h, h.length, "up");
  assert.deepEqual(one, { cursor: 2, value: "c" });
  const two = recallHistory(h, one.cursor, "up");
  assert.deepEqual(two, { cursor: 1, value: "b" });
  const three = recallHistory(h, two.cursor, "up");
  assert.deepEqual(three, { cursor: 0, value: "a" });
  assert.deepEqual(recallHistory(h, three.cursor, "up"), {
    cursor: 0,
    value: "a",
  });
});

test("recallHistory: down returns to an empty live input and stays there", () => {
  const h = ["a", "b"];
  assert.deepEqual(recallHistory(h, 0, "down"), { cursor: 1, value: "b" });
  assert.deepEqual(recallHistory(h, 1, "down"), { cursor: 2, value: "" });
  assert.deepEqual(recallHistory(h, 2, "down"), { cursor: 2, value: "" });
});

test("recallHistory: empty history is a no-op in both directions", () => {
  assert.deepEqual(recallHistory([], 0, "up"), { cursor: 0, value: "" });
  assert.deepEqual(recallHistory([], 0, "down"), { cursor: 0, value: "" });
});
