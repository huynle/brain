/**
 * Transcript — ordered OpenCode message list.
 *
 * Renders OcMessage[] using the wireframe's `.msg` classes (user /
 * assistant / tool accents come from `.session-view .stream` CSS).
 * Pure presentation: data arrives via useSessionTranscript, deltas are
 * folded upstream by lib/transcript.applyEvent.
 *
 * Autoscroll: pinned-to-bottom while the user is at (or near) the
 * bottom; scrolling away detaches, scrolling back re-pins.
 */
import { useEffect, useRef } from "react";
import type { OcMessage, OcPart } from "../../lib/types";
import { isInjectedCheckin } from "../../lib/transcript";

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
      return part.text ? <pre>{part.text}</pre> : null;
    case "reasoning":
      return part.text ? (
        <details>
          <summary style={{ cursor: "pointer", color: "var(--p2-fg-faint, #6b757e)", fontSize: 11 }}>
            reasoning
          </summary>
          <pre style={{ opacity: 0.75 }}>{part.text}</pre>
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
          {input !== undefined && (
            <pre style={{ opacity: 0.8 }}>{typeof input === "string" ? input : JSON.stringify(input, null, 2)}</pre>
          )}
          {output && <pre>{output}</pre>}
          {error && <pre style={{ color: "var(--p2-danger, #e06c5f)" }}>{error}</pre>}
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
      {message.parts.map((p) => (
        <PartView key={p.id} part={p} />
      ))}
    </div>
  );
}

export interface TranscriptProps {
  messages: OcMessage[];
  className?: string;
  style?: React.CSSProperties;
  emptyText?: string;
}

export function Transcript({
  messages,
  className,
  style,
  emptyText = "No messages yet.",
}: TranscriptProps): JSX.Element {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const pinnedRef = useRef(true);

  const onScroll = () => {
    const el = containerRef.current;
    if (!el) return;
    pinnedRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 48;
  };

  useEffect(() => {
    const el = containerRef.current;
    if (el && pinnedRef.current) {
      el.scrollTop = el.scrollHeight;
    }
  }, [messages]);

  return (
    <div ref={containerRef} className={className} style={style} onScroll={onScroll}>
      {messages.length === 0 && (
        <div style={{ color: "var(--p2-fg-faint, #6b757e)", padding: 12 }}>{emptyText}</div>
      )}
      {messages.map((m) => (
        <MessageView key={m.info.id} message={m} />
      ))}
    </div>
  );
}
