/**
 * mockShell — unit tests.
 *
 * The mock shell is a pure function that takes a runner + an input
 * command and returns an array of `ShellLine`s to append to the
 * terminal. We test:
 *   - each command's expected line count + kinds
 *   - unknown commands emit an "err" line
 *   - the `help` command lists every known command
 *   - the `clear` command emits zero lines (the caller handles state)
 *   - the `exit` command emits a farewell then defers close to caller
 *   - runner metadata (hostname, executor, active_tasks) is echoed
 *     by `status`, `uname`, `ps` (`ps` prints active_tasks count)
 *   - command echoing: every non-clear invocation emits a leading
 *     "cmd" line so the transcript reads like a real terminal.
 *
 * We do NOT test the `MockShell` component here — this file is the
 * pure-logic layer only. The component is tested (SSR-only) in
 * v2.test.tsx along with the other Phase 6 modals.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import { runMockCommand, KNOWN_COMMANDS } from "./mockShell";
import type { RunnerInfo } from "./types";

function fakeRunner(overrides: Partial<RunnerInfo> = {}): RunnerInfo {
  return {
    runner_id: "runner-abc",
    hostname: "test-host",
    max_parallel: 4,
    active_tasks: 2,
    registered_at: "2025-01-01T00:00:00Z",
    last_heartbeat: "2025-01-01T00:01:00Z",
    status: "online",
    executors: ["opencode", "pi"],
    labels: { env: "dev", region: "us-west" },
    ...overrides,
  };
}

// ─── command echoing ─────────────────────────────────────────────

test("mockShell: every command emits a leading 'cmd' echo line", () => {
  const r = fakeRunner();
  for (const c of KNOWN_COMMANDS) {
    if (c === "clear") continue; // clear intentionally emits nothing
    const lines = runMockCommand(r, c);
    assert.ok(
      lines.length > 0 && lines[0].kind === "cmd",
      `expected leading cmd echo for '${c}', got ${JSON.stringify(lines[0])}`,
    );
    assert.match(lines[0].text, /\$/, `cmd echo should look like a shell prompt`);
    assert.match(
      lines[0].text,
      new RegExp(c.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")),
      `cmd echo should contain the command`,
    );
  }
});

// ─── help ────────────────────────────────────────────────────────

test("mockShell: help lists every known command", () => {
  const lines = runMockCommand(fakeRunner(), "help");
  const body = lines.map((l) => l.text).join("\n");
  for (const c of KNOWN_COMMANDS) {
    assert.match(body, new RegExp(`\\b${c}\\b`), `help should mention ${c}`);
  }
});

test("mockShell: help output uses 'out' or 'dim' kinds (after the cmd echo)", () => {
  const lines = runMockCommand(fakeRunner(), "help").slice(1);
  assert.ok(lines.length >= 6, `help should produce several lines, got ${lines.length}`);
  for (const l of lines) {
    assert.ok(
      l.kind === "out" || l.kind === "dim",
      `help line should be out/dim, got ${l.kind}`,
    );
  }
});

// ─── status ──────────────────────────────────────────────────────

test("mockShell: status echoes runner_id, hostname, status, active/max", () => {
  const r = fakeRunner({
    runner_id: "runner-xyz",
    hostname: "prod-1",
    status: "busy" as unknown as RunnerInfo["status"],
    active_tasks: 3,
    max_parallel: 8,
  });
  const body = runMockCommand(r, "status")
    .map((l) => l.text)
    .join("\n");
  assert.match(body, /runner-xyz/);
  assert.match(body, /prod-1/);
  assert.match(body, /busy/);
  assert.match(body, /3\s*\/\s*8/);
});

// ─── ps ──────────────────────────────────────────────────────────

test("mockShell: ps reports active_tasks count", () => {
  const lines = runMockCommand(fakeRunner({ active_tasks: 5 }), "ps");
  const body = lines.map((l) => l.text).join("\n");
  assert.match(body, /5/);
  assert.match(body, /active/i);
});

test("mockShell: ps with zero active_tasks reports 'no active tasks'", () => {
  const lines = runMockCommand(fakeRunner({ active_tasks: 0 }), "ps");
  const body = lines.map((l) => l.text).join("\n");
  assert.match(body, /no active tasks|0 active/i);
});

// ─── tasks ───────────────────────────────────────────────────────

test("mockShell: tasks emits a friendly message (mock — no real task list)", () => {
  const lines = runMockCommand(fakeRunner(), "tasks");
  assert.ok(lines.length >= 2, `tasks should have echo + body`);
  const body = lines
    .slice(1)
    .map((l) => l.text)
    .join("\n");
  assert.match(body, /mock/i);
});

// ─── logs ────────────────────────────────────────────────────────

test("mockShell: logs emits at least one dim line indicating mock", () => {
  const lines = runMockCommand(fakeRunner(), "logs");
  const dimCount = lines.filter((l) => l.kind === "dim").length;
  assert.ok(dimCount >= 1, `logs should emit >= 1 dim line, got ${dimCount}`);
});

// ─── uname ───────────────────────────────────────────────────────

test("mockShell: uname prints hostname + executors joined", () => {
  const r = fakeRunner({
    hostname: "green-hill",
    executors: ["opencode", "pi", "script"],
  });
  const body = runMockCommand(r, "uname").map((l) => l.text).join("\n");
  assert.match(body, /green-hill/);
  assert.match(body, /opencode/);
  assert.match(body, /pi/);
});

// ─── clear ───────────────────────────────────────────────────────

test("mockShell: clear returns an empty array (caller resets history)", () => {
  const lines = runMockCommand(fakeRunner(), "clear");
  assert.deepEqual(lines, []);
});

// ─── exit ────────────────────────────────────────────────────────

test("mockShell: exit emits a farewell 'dim' line for the caller to append before closing", () => {
  const lines = runMockCommand(fakeRunner(), "exit");
  assert.ok(lines.length >= 2, `exit should have echo + farewell`);
  const farewell = lines[lines.length - 1];
  assert.equal(farewell.kind, "dim");
  assert.match(farewell.text, /bye|closing|goodbye/i);
});

// ─── unknown commands ────────────────────────────────────────────

test("mockShell: unknown commands emit an 'err' explanation line", () => {
  const lines = runMockCommand(fakeRunner(), "nosuchthing");
  const errs = lines.filter((l) => l.kind === "err");
  assert.ok(errs.length >= 1, `unknown command should emit >= 1 err line`);
  assert.match(errs[0].text, /not found|unknown/i);
});

test("mockShell: unknown commands suggest 'help'", () => {
  const body = runMockCommand(fakeRunner(), "asdf")
    .map((l) => l.text)
    .join("\n");
  assert.match(body, /help/);
});

// ─── whitespace tolerance ────────────────────────────────────────

test("mockShell: leading/trailing whitespace on the command is trimmed", () => {
  const a = runMockCommand(fakeRunner(), "status");
  const b = runMockCommand(fakeRunner(), "  status  ");
  // Same command → same body length (echo may differ slightly).
  assert.equal(a.length, b.length);
});

test("mockShell: empty command emits only a bare prompt echo", () => {
  const lines = runMockCommand(fakeRunner(), "");
  // Either zero lines OR one echoed-empty prompt — implementation choice.
  // The important part is we never crash and never emit an err.
  assert.ok(lines.length <= 1);
  for (const l of lines) {
    assert.notEqual(l.kind, "err");
  }
});
