/**
 * mockShell — pure command → output lines mapping.
 *
 * The panes-v2 Runner Shell modal (Phase 6) needs an interactive
 * shell surface, but the backend to actually spawn an adhoc runner
 * shell is BLOCKED on RBAC policy (Brain task `4mymbjen`). Until
 * that lands, the modal ships a client-side mock so the UX can be
 * exercised, styled, and tested end-to-end without server support.
 *
 * This module is deliberately pure: no react, no fetch, no zustand.
 * The React component (`MockShell.tsx`) owns the history buffer and
 * calls `runMockCommand()` on every submitted line.
 *
 * Commands supported:
 *   help    — list commands
 *   status  — runner id / hostname / status / active/max
 *   ps      — how many tasks the runner is currently holding
 *   tasks   — placeholder (real list would come from the API)
 *   logs    — placeholder (real logs stream from useLive)
 *   uname   — hostname + executors
 *   clear   — returns [] (caller wipes its history buffer)
 *   exit    — farewell line (caller closes the modal after appending)
 *
 * Every non-clear invocation returns a leading `cmd` line — the
 * echoed prompt — so the transcript reads like a real terminal.
 */
import type { RunnerInfo } from "./types";

export interface ShellLine {
  kind: "out" | "err" | "warn" | "dim" | "cmd";
  text: string;
}

/** Canonical command list. Also drives `help`. */
export const KNOWN_COMMANDS = [
  "help",
  "status",
  "ps",
  "tasks",
  "logs",
  "uname",
  "clear",
  "exit",
] as const;

const HELP_BODY: readonly { cmd: string; blurb: string }[] = [
  { cmd: "help", blurb: "show this message" },
  { cmd: "status", blurb: "runner state summary" },
  { cmd: "ps", blurb: "count of active tasks on this runner" },
  { cmd: "tasks", blurb: "list tasks (mock — see the Tasks card)" },
  { cmd: "logs", blurb: "tail logs (mock — see the Logs card)" },
  { cmd: "uname", blurb: "hostname + supported executors" },
  { cmd: "clear", blurb: "clear the transcript" },
  { cmd: "exit", blurb: "close the shell modal" },
];

/**
 * Render the leading "prompt line" that each command echoes back.
 * Format matches a canonical shell: `<runner-id>$ <cmd>`.
 */
function promptEcho(runner: RunnerInfo, cmd: string): ShellLine {
  return {
    kind: "cmd",
    text: `${runner.runner_id}$ ${cmd}`,
  };
}

/**
 * Route a trimmed command to its output.
 *
 * Returns:
 *   • [] for `clear` — the caller wipes the transcript.
 *   • an array beginning with a `cmd` echo, followed by 0+ result
 *     lines, otherwise.
 *
 * Never throws. Unknown commands emit an `err` line + a suggestion
 * to run `help`.
 */
export function runMockCommand(runner: RunnerInfo, cmd: string): ShellLine[] {
  const trimmed = (cmd ?? "").trim();

  // Empty input: just show a blank prompt echo (no err). Matches
  // real-shell behaviour where hitting Enter with no input redraws
  // the prompt and moves on.
  if (trimmed === "") return [];

  // clear is special — caller resets its buffer, so we emit nothing.
  if (trimmed === "clear") return [];

  const echo = promptEcho(runner, trimmed);
  const body = runBody(runner, trimmed);
  return [echo, ...body];
}

function runBody(runner: RunnerInfo, cmd: string): ShellLine[] {
  switch (cmd) {
    case "help":
      return helpLines();
    case "status":
      return statusLines(runner);
    case "ps":
      return psLines(runner);
    case "tasks":
      return tasksLines();
    case "logs":
      return logsLines();
    case "uname":
      return unameLines(runner);
    case "exit":
      return exitLines();
    default:
      return unknownLines(cmd);
  }
}

function helpLines(): ShellLine[] {
  const out: ShellLine[] = [
    { kind: "out", text: "Available commands:" },
  ];
  for (const { cmd, blurb } of HELP_BODY) {
    out.push({ kind: "out", text: `  ${cmd.padEnd(8)} ${blurb}` });
  }
  out.push({
    kind: "dim",
    text: "(mock shell — no real runner is being touched)",
  });
  return out;
}

function statusLines(r: RunnerInfo): ShellLine[] {
  const active = r.active_tasks ?? 0;
  return [
    { kind: "out", text: `runner:     ${r.runner_id}` },
    { kind: "out", text: `hostname:   ${r.hostname}` },
    { kind: "out", text: `status:     ${r.status}` },
    { kind: "out", text: `capacity:   ${active} / ${r.max_parallel}` },
    { kind: "out", text: `heartbeat:  ${r.last_heartbeat}` },
  ];
}

function psLines(r: RunnerInfo): ShellLine[] {
  const active = r.active_tasks ?? 0;
  if (active === 0) {
    return [{ kind: "dim", text: "no active tasks" }];
  }
  return [
    { kind: "out", text: `${active} active task${active === 1 ? "" : "s"}` },
    {
      kind: "dim",
      text: "(mock — the real ps would list task ids and states)",
    },
  ];
}

function tasksLines(): ShellLine[] {
  return [
    { kind: "out", text: "tasks: use the Tasks card in the Overview grid." },
    { kind: "dim", text: "(this is a mock shell; no real task list)" },
  ];
}

function logsLines(): ShellLine[] {
  return [
    { kind: "out", text: "logs: use the Logs tab of this modal." },
    { kind: "dim", text: "(mock — real logs stream via SSE)" },
  ];
}

function unameLines(r: RunnerInfo): ShellLine[] {
  const executors = (r.executors ?? []).join(" ");
  return [
    {
      kind: "out",
      text: `${r.hostname} · executors: ${executors || "(none)"}`,
    },
  ];
}

function exitLines(): ShellLine[] {
  return [{ kind: "dim", text: "bye — closing shell" }];
}

function unknownLines(cmd: string): ShellLine[] {
  return [
    { kind: "err", text: `${cmd}: command not found` },
    { kind: "dim", text: "try `help` for available commands" },
  ];
}
