/**
 * TerminalText — render captured terminal output as text a human reads.
 *
 * Agent stdout is not a string, it is a terminal recording: SGR colour
 * escapes, cursor/erase sequences, and carriage returns that redraw a
 * progress line in place. Dropped into the DOM verbatim, an SGR reset
 * shows as a literal `[0m` and a spinner expands into hundreds of
 * frames. This is the ONE renderer for that: `lib/ansi.terminalSpans`
 * (CR overwrite + styled spans), then one `<span>` per style run.
 *
 * It lives here rather than inside a log pane because every surface
 * showing agent output needs it — the raw-log panes AND the chat
 * transcript, whose tool payloads are exactly the colourised `npm` /
 * `pytest` / `cargo` output the log panes carry.
 *
 * Unstyled text short-circuits to a bare string, so the common case
 * costs no wrapper element per line.
 */
import { useMemo } from "react";
import { ansiStyleToCss, hasAnsiStyle, terminalSpans } from "../../lib/ansi";

export function TerminalText({ text }: { text?: string }): JSX.Element | null {
  const spans = useMemo(() => terminalSpans(text), [text]);
  if (spans.length === 0) return null;
  if (spans.length === 1 && !hasAnsiStyle(spans[0].style)) {
    return <>{spans[0].text}</>;
  }
  return (
    <>
      {spans.map((s, i) => (
        <span key={i} style={ansiStyleToCss(s.style)}>
          {s.text}
        </span>
      ))}
    </>
  );
}
