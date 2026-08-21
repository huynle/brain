/**
 * shell — pure helpers for the runner ad-hoc terminal.
 *
 * The Runner Shell modal tab talks to a REAL shell on the runner host:
 *
 *   POST /api/v1/control/runners/{id}/exec        (text/event-stream)
 *   POST /api/v1/control/runners/{id}/exec/{e}/signal
 *
 * The transport lives in `lib/api.ts` (`controlExec`, `controlExecSignal`)
 * and the React surface in `components/RunnerShell.tsx`. THIS module is
 * the pure layer in between: no react, no fetch, no zustand — just the
 * text transformations the terminal needs, so they can be unit-tested
 * without a DOM.
 *
 * Responsibilities:
 *   • ShellLine — the one line model the transcript renders
 *   • promptEcho / greetingLine / exitLine / errorLine — line formatting
 *   • interceptLocalCommand — the three built-ins (`help`, `clear`,
 *     `exit`) that this terminal answers itself and must NEVER be sent
 *     to the runner
 *   • an accumulator that turns arbitrarily-chopped `exec_data` chunks
 *     into whole lines without losing or duplicating a partial tail
 *   • readline-style command history helpers
 */

export interface ShellLine {
  /**
   * cmd  — the echoed prompt line for a submitted command
   * out  — stdout (and local informational output)
   * err  — stderr, transport errors, non-zero-with-message failures
   * warn — advisory
   * dim  — chrome: greeting, exit markers, farewells
   */
  kind: "out" | "err" | "warn" | "dim" | "cmd";
  text: string;
}

/** Streams the runner tags its output chunks with. */
export type ExecStreamName = "stdout" | "stderr";

// ─── line formatting ─────────────────────────────────────────────

/**
 * The leading "prompt line" every submitted command echoes back.
 * Format matches a canonical shell: `<runner-id>$ <cmd>`.
 */
export function promptEcho(runnerId: string, cmd: string): ShellLine {
  return { kind: "cmd", text: `${runnerId}$ ${cmd}` };
}

/** First line in a fresh transcript. */
export function greetingLine(runnerId: string): ShellLine {
  return {
    kind: "dim",
    text:
      `connected to ${runnerId} — commands run on the runner host; ` +
      "type `help` for built-ins and keys",
  };
}

/**
 * Reasons a command stopped that are an expected end to the stream rather
 * than a malfunction: the user pressed Ctrl+C, or the server's own timeout
 * stopped a long command. Reported by the runner in the exec_exit error
 * field, so they are matched here on the phrasing the runner emits.
 */
const EXPECTED_TERMINATION = /^(terminated by signal|command timed out)/;

/**
 * Terminal marker for a finished exec.
 *
 * Real shells print nothing on success, so a clean exit stays silent
 * (returns null) and only an abnormal end leaves a mark.
 *   • interrupt / timeout    → a `warn` marker naming the reason
 *   • transport/spawn error  → an `err` line carrying the message
 *   • non-zero exit code     → a `dim` `[exit N]` marker
 *   • exit 0                 → null
 *
 * An interrupt is deliberately NOT an `err`: the user asked for it, and
 * labelling their own Ctrl+C "exec failed" reads as a bug in the shell.
 */
export function exitLine(exitCode: number, error?: string): ShellLine | null {
  const msg = (error ?? "").trim();
  if (msg) {
    if (EXPECTED_TERMINATION.test(msg))
      return { kind: "warn", text: `[${msg}]` };
    return { kind: "err", text: `[exec failed: ${msg}]` };
  }
  if (!exitCode) return null;
  return { kind: "dim", text: `[exit ${exitCode}]` };
}

/**
 * Marker for output the server could not deliver.
 *
 * Output is fanned out to this stream without blocking the runner's
 * connection, so a browser that falls far enough behind loses chunks. The
 * server counts them and reports them here: a transcript that is silently
 * missing a chunk is worse than a slow one, because nothing on screen says
 * the output is incomplete. Returns null when nothing was lost.
 */
export function truncationLine(
  droppedChunks: number,
  droppedBytes: number,
): ShellLine | null {
  if (!droppedChunks || droppedChunks < 0) return null;
  const bytes = droppedBytes > 0 ? ` (~${droppedBytes} bytes)` : "";
  return {
    kind: "warn",
    text: `[output truncated: ${droppedChunks} chunk${
      droppedChunks === 1 ? "" : "s"
    }${bytes} dropped — this shell could not keep up]`,
  };
}

/**
 * Render any thrown value as an `err` line. Used so a rejected fetch or
 * an ApiError lands in the transcript instead of becoming an unhandled
 * rejection.
 */
export function errorLine(err: unknown): ShellLine {
  const msg =
    err instanceof Error ? err.message : typeof err === "string" ? err : "";
  return { kind: "err", text: msg || "shell request failed" };
}

// ─── local built-ins ─────────────────────────────────────────────

/** Built-ins answered client-side; never dispatched to the runner. */
export const LOCAL_COMMANDS = ["help", "clear", "exit"] as const;

export type LocalShellAction =
  /** append `lines`, nothing else */
  | "print"
  /** wipe the transcript (ignore `lines`) */
  | "clear"
  /** append `lines`, then close the modal */
  | "exit";

export interface LocalShellResult {
  action: LocalShellAction;
  lines: ShellLine[];
}

function helpLines(runnerId: string): ShellLine[] {
  return [
    {
      kind: "out",
      text: `Commands are executed on ${runnerId} by the runner's shell.`,
    },
    { kind: "out", text: "" },
    {
      kind: "out",
      text: "Built-ins (handled here, never sent to the runner):",
    },
    { kind: "out", text: "  help     show this message" },
    { kind: "out", text: "  clear    clear the transcript" },
    { kind: "out", text: "  exit     close the shell modal" },
    { kind: "out", text: "" },
    { kind: "out", text: "Keys:" },
    { kind: "out", text: "  ↑ / ↓    previous / next command" },
    { kind: "out", text: "  Ctrl+C   interrupt the running command" },
    { kind: "out", text: "  Ctrl+L   clear the transcript" },
    {
      kind: "dim",
      text: "output streams live; long commands are capped by the server timeout",
    },
  ];
}

/**
 * Decide whether a submitted line is handled locally.
 *
 * Returns null when the command must go to the runner — callers MUST
 * check this first so `clear`/`exit`/`help` never hit the network.
 * Empty input returns an empty `print` (a bare Enter redraws the prompt
 * and does nothing, like a real shell).
 *
 * Never throws.
 */
export function interceptLocalCommand(
  runnerId: string,
  raw: string,
): LocalShellResult | null {
  const cmd = (raw ?? "").trim();
  if (cmd === "") return { action: "print", lines: [] };

  const echo = promptEcho(runnerId, cmd);
  switch (cmd) {
    case "clear":
      return { action: "clear", lines: [] };
    case "exit":
    case "quit":
      return {
        action: "exit",
        lines: [echo, { kind: "dim", text: "bye — closing shell" }],
      };
    case "help":
    case "?":
      return { action: "print", lines: [echo, ...helpLines(runnerId)] };
    default:
      return null;
  }
}

// ─── chunk → line accumulation ───────────────────────────────────

/**
 * Per-exec buffer of the not-yet-terminated tail of each stream.
 *
 * `exec_data` chunks are sliced by the runner at arbitrary byte
 * boundaries (ExecChunkBytes), so a chunk routinely ends mid-line — and
 * the next chunk continues it. Rendering each chunk as its own line
 * would shred output; dropping the tail would lose the final line of any
 * command that doesn't end with a newline (`printf`, `echo -n`, most
 * prompts). The accumulator holds the tail until a newline arrives, and
 * `flushAccumulator` emits whatever is left when the exec ends.
 *
 * stdout and stderr buffer independently, so a partial stdout line is
 * not corrupted by an interleaved stderr write.
 */
export interface OutputAccumulator {
  stdout: string;
  stderr: string;
}

export function newAccumulator(): OutputAccumulator {
  return { stdout: "", stderr: "" };
}

/**
 * Collapse carriage-return overwrites the way a terminal would: only the
 * text after the last CR is visible. (CRLF is normalised to LF before
 * this runs, so this only sees bare CRs — progress bars, spinners.)
 */
function applyCarriageReturns(line: string): string {
  if (!line.includes("\r")) return line;
  const parts = line.split("\r");
  return parts[parts.length - 1];
}

/**
 * Feed one streamed chunk into the accumulator.
 *
 * Returns only the lines that are now COMPLETE (newline-terminated).
 * Any trailing partial line is retained on `acc` and emitted by a later
 * push or by `flushAccumulator` — never dropped, never emitted twice.
 * Mutates `acc`.
 */
export function pushChunk(
  acc: OutputAccumulator,
  stream: ExecStreamName,
  chunk: string,
): ShellLine[] {
  const kind: ShellLine["kind"] = stream === "stderr" ? "err" : "out";
  const prev = stream === "stderr" ? acc.stderr : acc.stdout;
  // Concatenate BEFORE normalising so a CRLF split across two chunks
  // still collapses to a single newline.
  const buf = (prev + (chunk ?? "")).replace(/\r\n/g, "\n");

  const parts = buf.split("\n");
  const tail = parts.pop() ?? "";
  if (stream === "stderr") acc.stderr = tail;
  else acc.stdout = tail;

  return parts.map((text) => ({ kind, text: applyCarriageReturns(text) }));
}

/**
 * Emit any retained partial lines and reset the buffers. Call exactly
 * once when the exec finishes (or the stream drops); safe to call again
 * — a flushed accumulator yields nothing.
 */
export function flushAccumulator(acc: OutputAccumulator): ShellLine[] {
  const out: ShellLine[] = [];
  if (acc.stdout) {
    out.push({ kind: "out", text: applyCarriageReturns(acc.stdout) });
    acc.stdout = "";
  }
  if (acc.stderr) {
    out.push({ kind: "err", text: applyCarriageReturns(acc.stderr) });
    acc.stderr = "";
  }
  return out;
}

// ─── readline-style history ──────────────────────────────────────

export const MAX_HISTORY = 200;

/**
 * Append a command to the history ring. Blank lines and an immediate
 * repeat of the previous command are skipped (bash `ignoredups`).
 */
export function pushHistory(
  history: string[],
  cmd: string,
  max = MAX_HISTORY,
): string[] {
  const trimmed = (cmd ?? "").trim();
  if (!trimmed) return history;
  if (history.length && history[history.length - 1] === trimmed) return history;
  const next = [...history, trimmed];
  return next.length > max ? next.slice(-max) : next;
}

/**
 * Move the history cursor.
 *
 * The cursor is an index into `history`; `history.length` means "not
 * browsing" (the live input). Up walks backwards and clamps at the
 * oldest entry; down walks forward and lands on the empty live input.
 */
export function recallHistory(
  history: string[],
  cursor: number,
  dir: "up" | "down",
): { cursor: number; value: string } {
  const len = history.length;
  if (len === 0) return { cursor: 0, value: "" };
  const from = Number.isFinite(cursor) ? cursor : len;
  let next = dir === "up" ? from - 1 : from + 1;
  if (next < 0) next = 0;
  if (next > len) next = len;
  return { cursor: next, value: next === len ? "" : history[next] };
}
