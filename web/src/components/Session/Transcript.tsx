/**
 * Transcript — ordered OpenCode message list.
 *
 * Renders OcMessage[] using the wireframe's `.msg` classes (user /
 * assistant / tool accents come from `.session-view .stream` CSS).
 * Pure presentation: data arrives via useSessionTranscript, deltas are
 * folded upstream by lib/transcript.applyEvent.
 *
 * Every part renders through <TerminalText>. For tool output/error/input
 * that is load-bearing (they carry raw pty capture). For prose text and
 * reasoning it is a no-op unless the model actually emitted an escape or
 * a CR: with neither present the pipeline returns the string unchanged
 * apart from C0 control characters, so markdown, indentation and blank
 * lines survive exactly as before.
 *
 * Autoscroll: pinned-to-bottom while the user is at (or near) the
 * bottom; scrolling away detaches, scrolling back re-pins. A live
 * transcript grows in two ways React's message-array identity does not
 * capture on its own — a streaming text part reflows to a taller box,
 * a <details> block opens — so a ResizeObserver on the content re-pins
 * on every height change too. While detached, a "Jump to latest" button
 * says so out loud and offers the way back.
 */
import { useCallback, useEffect, useRef, useState } from "react";
import type { OcMessage, OcPart } from "../../lib/types";
import { isInjectedCheckin } from "../../lib/transcript";
import { TerminalText } from "../common/TerminalText";

function toolTitle(part: OcPart): string {
  const state = part.state;
  return state?.title || part.tool || "tool";
}

function toolStatusColor(status?: string): string {
  switch (status) {
    case "completed":
      return "var(--p2-ok, #6fca7d)";
    case "error":
      return "var(--p2-danger, #e06c5f)";
    case "running":
      return "var(--p2-accent, #f4b23a)";
    default:
      return "var(--p2-fg-faint, #6b757e)";
  }
}

function PartView({ part }: { part: OcPart }): JSX.Element | null {
  switch (part.type) {
    case "text":
      return part.text ? (
        <pre>
          <TerminalText text={part.text} />
        </pre>
      ) : null;
    case "reasoning":
      return part.text ? (
        <details>
          <summary style={{ cursor: "pointer", color: "var(--p2-fg-faint, #6b757e)", fontSize: 11 }}>
            reasoning
          </summary>
          <pre style={{ opacity: 0.75 }}>
            <TerminalText text={part.text} />
          </pre>
        </details>
      ) : null;
    case "tool": {
      const status = part.state?.status;
      const input = part.state?.input;
      const output = part.state?.output;
      const error = part.state?.error;
      return (
        <details>
          <summary style={{ cursor: "pointer", fontSize: 11 }}>
            <span style={{ color: "var(--p2-fg-dim, #9098a1)" }}>{toolTitle(part)}</span>{" "}
            <span style={{ color: toolStatusColor(status) }}>· {status || "pending"}</span>
          </summary>
          {/*
            Tool payloads are captured terminal output — a bash/test tool
            under a pty hands back npm/pytest/cargo colour and CR spinner
            frames verbatim. They go through the same renderer the raw-log
            panes use, or the chat pane (now the default) shows the exact
            `[0m` residue the log panes were fixed for.
          */}
          {input !== undefined && (
            <pre style={{ opacity: 0.8 }}>
              <TerminalText
                text={typeof input === "string" ? input : JSON.stringify(input, null, 2)}
              />
            </pre>
          )}
          {output && (
            <pre>
              <TerminalText text={output} />
            </pre>
          )}
          {error && (
            <pre style={{ color: "var(--p2-danger, #e06c5f)" }}>
              <TerminalText text={error} />
            </pre>
          )}
        </details>
      );
    }
    case "step-start":
    case "step-finish":
      return null;
    default:
      return null;
  }
}

function MessageView({ message }: { message: OcMessage }): JSX.Element {
  const role = message.info.role === "user" ? "user" : "assistant";
  const injected = isInjectedCheckin(message);
  const hasTool = message.parts.some((p) => p.type === "tool");
  return (
    <div className={`msg ${hasTool && role === "assistant" ? "tool" : role}`}>
      <div className="role">
        <span>{message.info.role || "…"}</span>
        {message.info.agent && (
          <span style={{ color: "var(--p2-fg-faint, #6b757e)" }}> · {message.info.agent}</span>
        )}
        {injected && (
          <span
            style={{
              marginLeft: 6,
              fontSize: 9,
              padding: "1px 6px",
              borderRadius: 999,
              border: "1px solid var(--p2-border, #333a42)",
              color: "var(--p2-accent, #f4b23a)",
            }}
            title="This user turn was injected (goal steering or a check-in preset), not typed in this view."
          >
            injected
          </span>
        )}
      </div>
      {/*
        Part ids are absent on some payloads (and on the stub a part
        arriving before its message creates). React then warns and, worse,
        remounts the <pre> on every delta — the exact nodes a streaming
        turn is rewriting. Index is a safe fallback: parts are
        append-only within a message and never reordered.
      */}
      {message.parts.map((p, i) => (
        <PartView key={p.id || `${message.info.id}-part-${i}`} part={p} />
      ))}
    </div>
  );
}

/** How close to the bottom still counts as "following the tail" (px). */
const PIN_THRESHOLD = 48;

export interface TranscriptProps {
  messages: OcMessage[];
  className?: string;
  style?: React.CSSProperties;
  emptyText?: string;
  /** Changing this re-pins to the bottom — pass the session id so
   *  switching sessions starts at the newest message, not at whatever
   *  scroll offset the previous one was left at. */
  resetKey?: string;
  /** Show the "Jump to latest" affordance while detached. Off for
   *  read-only history, where nothing new is arriving below. */
  follow?: boolean;
}

export function Transcript({
  messages,
  className,
  style,
  emptyText = "No messages yet.",
  resetKey,
  follow = false,
}: TranscriptProps): JSX.Element {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const contentRef = useRef<HTMLDivElement | null>(null);
  const pinnedRef = useRef(true);
  // Mirrored into state only so the jump button can render; the ref is
  // what the scroll effects read, so they never see a stale value.
  const [pinned, setPinned] = useState(true);

  const setPin = useCallback((next: boolean) => {
    pinnedRef.current = next;
    setPinned((prev) => (prev === next ? prev : next));
  }, []);

  const stickToBottom = useCallback(() => {
    const el = containerRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, []);

  const onScroll = () => {
    const el = containerRef.current;
    if (!el) return;
    setPin(el.scrollHeight - el.scrollTop - el.clientHeight < PIN_THRESHOLD);
  };

  // A different session starts pinned again.
  useEffect(() => {
    setPin(true);
    stickToBottom();
  }, [resetKey, setPin, stickToBottom]);

  useEffect(() => {
    if (pinnedRef.current) stickToBottom();
  }, [messages, stickToBottom]);

  /*
   * Message-array identity is not the only thing that moves the tail.
   * A streaming text part rewrites one existing node — its box grows
   * without the list changing length — and TerminalText, images and
   * <details> all settle their height after the commit that added them.
   * Observing the content box catches every one of those.
   */
  useEffect(() => {
    const content = contentRef.current;
    if (!content || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => {
      if (pinnedRef.current) stickToBottom();
    });
    ro.observe(content);
    return () => ro.disconnect();
  }, [stickToBottom]);

  const jump = () => {
    setPin(true);
    stickToBottom();
  };

  /*
   * The scroll container keeps taking the caller's className and style
   * verbatim — every surface sizes this element itself. The jump button
   * rides along as a sticky child rather than an absolutely-positioned
   * sibling, so no wrapper element gets between the caller's layout and
   * the scroller.
   */
  return (
    <div ref={containerRef} className={className} style={style} onScroll={onScroll}>
      <div ref={contentRef}>
        {messages.length === 0 && (
          <div style={{ color: "var(--p2-fg-faint, #6b757e)", padding: 12 }}>
            {emptyText}
          </div>
        )}
        {/* Same reason as the parts above: messages are append-only. */}
        {messages.map((m, i) => (
          <MessageView key={m.info.id || `msg-${i}`} message={m} />
        ))}
      </div>
      {follow && !pinned && messages.length > 0 && (
        <div className="transcript-jump-wrap">
          <button type="button" className="transcript-jump" onClick={jump}>
            ↓ Jump to latest
          </button>
        </div>
      )}
    </div>
  );
}
