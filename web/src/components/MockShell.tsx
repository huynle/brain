/**
 * panes-v2 MockShell component.
 *
 * Terminal-UI wrapper around `runMockCommand()`. Renders inside the
 * Runner Shell modal tab, sits below the MockShellBanner.
 *
 * The component owns:
 *   - a rolling transcript (ShellLine[]) — appended to on every submit
 *   - the current input value
 *   - a ref to the body so we can auto-scroll to the bottom after
 *     appending lines
 *
 * Special commands:
 *   - `clear` empties the transcript
 *   - `exit`  appends its farewell line and then closes the modal
 *             via the modal store (`useModal.getState().close()`)
 *
 * Styling lives in `web/src/styles/runner.css` under `.p2-runner-shell`.
 */
import { useEffect, useRef, useState } from "react";

import { runMockCommand, type ShellLine } from "../lib/mockShell";
import { useModal } from "../store/modal";
import type { RunnerInfo } from "../lib/types";

const KIND_COLOR: Record<ShellLine["kind"], string> = {
  cmd: "var(--p2-fg)",
  out: "var(--p2-green)",
  err: "#f85149",
  warn: "#d29922",
  dim: "var(--p2-fg-faint)",
};

const INITIAL: ShellLine[] = [
  {
    kind: "dim",
    text: "mock shell — type `help` to get started",
  },
];

export function MockShell({ runner }: { runner: RunnerInfo }): JSX.Element {
  const [history, setHistory] = useState<ShellLine[]>(INITIAL);
  const [input, setInput] = useState("");
  const bodyRef = useRef<HTMLDivElement | null>(null);

  // Auto-scroll to the bottom whenever the transcript grows.
  useEffect(() => {
    const el = bodyRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [history.length]);

  function submit(raw: string) {
    const trimmed = raw.trim();
    const out = runMockCommand(runner, trimmed);

    if (trimmed === "clear") {
      setHistory([]);
      return;
    }

    setHistory((h) => [...h, ...out]);

    if (trimmed === "exit") {
      // Give the caller a beat to see the farewell before closing.
      const closeAfterFrame =
        typeof window !== "undefined" ? window.setTimeout : null;
      if (closeAfterFrame) {
        closeAfterFrame(() => useModal.getState().close(), 150);
      } else {
        useModal.getState().close();
      }
    }
  }

  return (
    <div className="p2-runner-shell">
      <div className="p2-runner-shell__head">
        <span className="p2-runner-shell__title">
          {runner.runner_id} · mock
        </span>
      </div>
      <div className="p2-runner-shell__body" ref={bodyRef}>
        {history.map((line, i) => (
          <div key={i} style={{ color: KIND_COLOR[line.kind] }}>
            {line.text}
          </div>
        ))}
      </div>
      <form
        className="p2-runner-shell__prompt"
        onSubmit={(e) => {
          e.preventDefault();
          const val = input;
          setInput("");
          submit(val);
        }}
      >
        <span className="p2-runner-shell__prompt-glyph">$</span>
        <input
          type="text"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder="type a command…"
          autoComplete="off"
          spellCheck={false}
          aria-label="Mock shell command input"
        />
      </form>
    </div>
  );
}
