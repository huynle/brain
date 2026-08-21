/**
 * RunnerShell — a real streaming terminal for a runner host.
 *
 * Renders inside the Runner modal's Shell tab. Every submitted line that
 * isn't a client built-in is POSTed to
 * `/api/v1/control/runners/{id}/exec`, which answers with a
 * text/event-stream: `started` → `exec_data`* → `exec_exit`. Output is
 * appended as it arrives, so a `tail -f` or a long build streams instead
 * of landing in one lump at the end.
 *
 * Split of concerns:
 *   • lib/shell.ts   — pure line formatting, built-in interception, the
 *                      chunk→line accumulator, history helpers (tested)
 *   • lib/api.ts     — controlExec (fetch-based SSE) + controlExecSignal
 *   • this component — transcript buffer, input state, in-flight exec,
 *                      auto-scroll, keyboard handling
 *
 * Built-ins (`help`, `clear`, `exit`) are intercepted BEFORE any network
 * call — `clear` must never round-trip, and `exit` closes the modal.
 *
 * Styling: `.p2-runner-shell*` in styles/global.css.
 */
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type KeyboardEvent,
} from "react";

import {
  controlExec,
  controlExecSignal,
  type ExecDataEvent,
  type ExecExitEvent,
  type ExecStartedEvent,
  type ExecTruncatedEvent,
} from "../lib/api";
import {
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
} from "../lib/shell";
import { useModal } from "../store/modal";
import type { RunnerInfo } from "../lib/types";

/**
 * Line colour by kind. Theme-aware tokens (not literals) so the terminal
 * stays legible on the light theme, where a #f85149 on white is glaring.
 */
const KIND_COLOR: Record<ShellLine["kind"], string> = {
  cmd: "var(--p2-green)",
  out: "var(--p2-fg)",
  err: "var(--p2-danger)",
  warn: "var(--p2-accent)",
  dim: "var(--p2-fg-faint)",
};

/** Transcript cap — a runaway `yes` shouldn't grow the DOM forever. */
const MAX_LINES = 5000;

export function RunnerShell({ runner }: { runner: RunnerInfo }): JSX.Element {
  const runnerId = runner.runner_id;

  const [lines, setLines] = useState<ShellLine[]>(() => [
    greetingLine(runnerId),
  ]);
  const [input, setInput] = useState("");
  const [running, setRunning] = useState(false);
  const [workdir, setWorkdir] = useState("");
  const [history, setHistory] = useState<string[]>([]);

  const bodyRef = useRef<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);
  /** exec_id of the in-flight command, once the server has assigned one. */
  const execIdRef = useRef<string | null>(null);
  /** Aborts the in-flight stream (unmount, or Ctrl+C before exec_id). */
  const abortRef = useRef<AbortController | null>(null);
  /** History cursor; null = not browsing (the live input line). */
  const cursorRef = useRef<number | null>(null);

  const append = useCallback((incoming: ShellLine[]) => {
    if (incoming.length === 0) return;
    setLines((prev) => {
      const next = [...prev, ...incoming];
      return next.length > MAX_LINES ? next.slice(-MAX_LINES) : next;
    });
  }, []);

  // Auto-scroll to the bottom whenever the transcript grows. Depends on
  // .p2-runner-shell__body having a bounded height and overflow-y:auto.
  useEffect(() => {
    const el = bodyRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [lines]);

  // Tearing down the tab (or the whole modal) must not leave a stream
  // reading into an unmounted component.
  useEffect(
    () => () => {
      abortRef.current?.abort();
      abortRef.current = null;
    },
    [],
  );

  async function dispatch(cmd: string): Promise<void> {
    append([promptEcho(runnerId, cmd)]);

    const acc = newAccumulator();
    const controller = new AbortController();
    abortRef.current = controller;
    execIdRef.current = null;
    setRunning(true);

    try {
      await controlExec(
        runnerId,
        { command: cmd },
        {
          onStarted: (e: ExecStartedEvent) => {
            execIdRef.current = e.exec_id;
            if (e.workdir) setWorkdir(e.workdir);
          },
          onData: (e: ExecDataEvent) => {
            append(pushChunk(acc, e.stream, e.chunk));
          },
          onTruncated: (e: ExecTruncatedEvent) => {
            // Output was lost between the runner and here. Say so inline,
            // where the gap actually is, rather than letting the transcript
            // read as complete.
            const marker = truncationLine(e.dropped_chunks, e.dropped_bytes);
            if (marker) append([...flushAccumulator(acc), marker]);
          },
          onExit: (e: ExecExitEvent) => {
            // Flush the unterminated tail BEFORE the exit marker so a
            // trailing `echo -n` line isn't printed after `[exit N]`.
            const tail = flushAccumulator(acc);
            const marker = exitLine(e.exit_code, e.error);
            append(marker ? [...tail, marker] : tail);
          },
        },
        controller.signal,
      );
      // Stream closed cleanly. Normally exec_exit already flushed; this
      // covers a server that drops the connection without one.
      append(flushAccumulator(acc));
    } catch (err) {
      const aborted =
        controller.signal.aborted ||
        (err as { name?: string })?.name === "AbortError";
      const tail = flushAccumulator(acc);
      append(aborted ? tail : [...tail, errorLine(err)]);
    } finally {
      abortRef.current = null;
      execIdRef.current = null;
      setRunning(false);
    }
  }

  function submit(raw: string): void {
    if (running) return;
    const cmd = raw.trim();
    cursorRef.current = null;
    if (cmd) setHistory((h) => pushHistory(h, cmd));

    // Built-ins first — `clear` and `exit` must never hit the network.
    const local = interceptLocalCommand(runnerId, cmd);
    if (local) {
      if (local.action === "clear") {
        setLines([]);
        return;
      }
      append(local.lines);
      if (local.action === "exit") {
        // Give the caller a beat to see the farewell before closing.
        // (Called as window.setTimeout, not a detached reference — an
        // unbound Window method throws "Illegal invocation" in Chrome.)
        const closeModal = () => useModal.getState().close();
        if (typeof window !== "undefined") window.setTimeout(closeModal, 150);
        else closeModal();
      }
      return;
    }

    void dispatch(cmd);
  }

  // Submitting the prompt. Enter is handled explicitly in onKeyDown rather
  // than left to the form's implicit submission: this form has no submit
  // button, so implicit submission only works while exactly one field is
  // present — a second input added later would silently break Enter, the
  // one keystroke a terminal cannot afford to lose.
  function submitCurrent(): void {
    const val = input;
    setInput("");
    submit(val);
  }

  function interrupt(): void {
    if (!running) return;
    append([{ kind: "dim", text: "^C" }]);
    const execId = execIdRef.current;
    if (!execId) {
      // No exec_id yet (the `started` frame hasn't landed): the only
      // lever is dropping the request.
      abortRef.current?.abort();
      return;
    }
    controlExecSignal(runnerId, execId, "int").catch((err: unknown) => {
      append([errorLine(err)]);
    });
  }

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>): void {
    if (e.key === "Enter") {
      e.preventDefault();
      submitCurrent();
      return;
    }
    // Ctrl+C only claims the keystroke while something is running, so
    // copy-to-clipboard keeps working the rest of the time.
    if (e.ctrlKey && (e.key === "c" || e.key === "C")) {
      if (!running) return;
      e.preventDefault();
      interrupt();
      return;
    }
    if (e.ctrlKey && (e.key === "l" || e.key === "L")) {
      e.preventDefault();
      setLines([]);
      return;
    }
    if (e.key === "ArrowUp" || e.key === "ArrowDown") {
      if (history.length === 0) return;
      e.preventDefault();
      const from = cursorRef.current ?? history.length;
      const next = recallHistory(
        history,
        from,
        e.key === "ArrowUp" ? "up" : "down",
      );
      cursorRef.current = next.cursor;
      setInput(next.value);
    }
  }

  return (
    <div className={`p2-runner-shell${running ? " is-running" : ""}`}>
      <div className="p2-runner-shell__head">
        <span className="p2-runner-shell__title">
          {runnerId}
          {workdir ? ` · ${workdir}` : ""}
        </span>
        {running && (
          <span className="p2-runner-shell__status">
            <span className="p2-runner-shell__spinner" aria-hidden="true" />
            running… ⌃C to interrupt
          </span>
        )}
      </div>

      <div
        className="p2-runner-shell__body"
        ref={bodyRef}
        role="log"
        aria-live="polite"
      >
        {lines.map((line, i) => (
          <div
            key={i}
            className="p2-runner-shell__line"
            style={{ color: KIND_COLOR[line.kind] }}
          >
            {line.text}
          </div>
        ))}
      </div>

      <form
        className="p2-runner-shell__prompt"
        onSubmit={(e) => {
          e.preventDefault();
          submitCurrent();
        }}
      >
        <span className="p2-runner-shell__prompt-glyph">$</span>
        <input
          ref={inputRef}
          type="text"
          value={input}
          onChange={(e) => {
            cursorRef.current = null;
            setInput(e.target.value);
          }}
          onKeyDown={onKeyDown}
          placeholder={
            running ? "running… Ctrl+C to interrupt" : "type a command…"
          }
          autoComplete="off"
          spellCheck={false}
          // autoFocus lands the caret in the terminal as soon as the
          // Shell tab mounts.
          autoFocus
          data-autofocus="true"
          // readOnly, not disabled: a disabled input receives no keydown,
          // which would kill Ctrl+C exactly when it's needed.
          readOnly={running}
          aria-busy={running}
          aria-label={`Shell command input for runner ${runnerId}`}
        />
      </form>
    </div>
  );
}
