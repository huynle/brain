/**
 * Composer — steer a live session by queueing a message.
 *
 * Semantics are honest: prompt_async queues a turn, it does not
 * interrupt the one in flight — the note under the field and the
 * delivery toast both say so ("delivery ≠ effect"). Abort is the
 * separate half, with a two-step inline confirm.
 *
 * The check-in preset mirrors buildGoalSteeringPrompt's shape so a
 * steered agent sees the same format whether the nudge came from a
 * goal or a human.
 */
import { useEffect, useRef, useState } from "react";
import { controlAbort, controlPrompt } from "../../lib/api";
import { buildCheckinPreset } from "../../lib/transcript";
import { useUI } from "../../store/ui";
import { useWorkspace } from "../../store/workspace";

export interface ComposerTarget {
  runner_id: string;
  instance_id: string;
  session_id: string;
}

export function Composer({
  target,
  checkinSeed,
}: {
  target: ComposerTarget;
  checkinSeed?: { title?: string; request?: string };
}): JSX.Element {
  const toast = useUI((s) => s.toast);
  const [text, setText] = useState("");
  const [sending, setSending] = useState(false);
  const [armAbort, setArmAbort] = useState(false);
  const abortTimer = useRef<ReturnType<typeof setTimeout>>();
  const inputRef = useRef<HTMLInputElement | null>(null);
  const steerIntent = useWorkspace((s) => s.steerIntent);
  const setSteerIntent = useWorkspace((s) => s.setSteerIntent);

  // The Steer verb navigates here with a one-shot focus intent.
  useEffect(() => {
    if (steerIntent) {
      inputRef.current?.focus();
      setSteerIntent(false);
    }
  }, [steerIntent, setSteerIntent]);

  const send = async () => {
    const body = text.trim();
    if (!body || sending) return;
    setSending(true);
    try {
      await controlPrompt(target.runner_id, target.instance_id, target.session_id, {
        text: body,
      });
      setText("");
      toast("Delivered — the agent acts on it next turn", "success");
    } catch (err) {
      toast(`Send failed: ${(err as Error)?.message ?? err}`, "error");
    } finally {
      setSending(false);
    }
  };

  const abort = async () => {
    if (!armAbort) {
      setArmAbort(true);
      clearTimeout(abortTimer.current);
      abortTimer.current = setTimeout(() => setArmAbort(false), 3000);
      return;
    }
    clearTimeout(abortTimer.current);
    setArmAbort(false);
    try {
      await controlAbort(target.runner_id, target.instance_id, target.session_id);
      toast("Abort sent — the current turn is being stopped", "success");
    } catch (err) {
      toast(`Abort failed: ${(err as Error)?.message ?? err}`, "error");
    }
  };

  return (
    <div className="composer" style={{ flexDirection: "column", alignItems: "stretch", gap: 4 }}>
      <div style={{ display: "flex", gap: 8 }}>
        <input
          ref={inputRef}
          type="text"
          value={text}
          disabled={sending}
          placeholder="Steer the agent — queued for its next turn"
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey || !e.shiftKey)) {
              e.preventDefault();
              void send();
            }
          }}
          style={{ flex: 1 }}
        />
        <button onClick={() => void send()} disabled={sending || !text.trim()}>
          {sending ? "Sending…" : "Send"}
        </button>
        {checkinSeed && (
          <button
            title="Insert a goal-steering-style check-in prompt"
            onClick={() => setText(buildCheckinPreset(checkinSeed))}
            disabled={sending}
          >
            Check-in
          </button>
        )}
        <button
          onClick={() => void abort()}
          style={{ color: armAbort ? "#e06c5f" : undefined }}
          title="Stop the current turn (does not change task status)"
        >
          {armAbort ? "Confirm abort?" : "Abort turn"}
        </button>
      </div>
      <div style={{ fontSize: 10, color: "#6b757e" }}>
        Queues a message for the agent's next turn — it does not interrupt the current turn.
      </div>
    </div>
  );
}
