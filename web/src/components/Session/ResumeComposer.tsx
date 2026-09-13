/**
 * ResumeComposer — continue a FINISHED session by relaunching its task with
 * the typed text as supervisor context.
 *
 * This is the finished-session half of the Session view's always-present
 * input (the live half is Composer). The process is gone, so there is nothing
 * to steer; instead a submit calls the resume-with-context backend, which
 * relaunches the task (rehydrate or same_session) — or, if the session turns
 * out to still be live under us, injects into it (live_injected) with no
 * status flip. Because a relaunch is heavier and more visible than a live
 * nudge, submit is a two-step inline confirm, and the returned resume_mode is
 * surfaced honestly (see ADR projects/brain-api/decision/7ihrqpi4.md,
 * Decision 5).
 */
import { useRef, useState } from "react";
import { resumeTaskWithContext } from "../../lib/api";
import type { ResumeMode } from "../../lib/types";
import { useUI } from "../../store/ui";

export interface ResumeTarget {
  project_id: string;
  task_id: string;
}

/**
 * Honest, mode-specific feedback for a successful resume. Exported so the
 * success-surfaces-resume_mode contract (ADR 7ihrqpi4, Decision 5) is unit
 * tested without a DOM harness — the wording per mode is the observable UX
 * state, and it must never be a generic "done" that hides which mode fired.
 */
export function resumeModeToast(mode: ResumeMode | string): string {
  switch (mode) {
    case "live_injected":
      return "Injected into the running session — it acts next turn.";
    case "same_session":
      return "Resuming the previous session with your context…";
    case "rehydrate":
      return "Relaunching with a fresh session seeded with your context…";
    default:
      return "Resume requested — the task will continue with your context.";
  }
}

export function ResumeComposer({
  target,
}: {
  target: ResumeTarget;
}): JSX.Element {
  const toast = useUI((s) => s.toast);
  const [text, setText] = useState("");
  const [sending, setSending] = useState(false);
  const [armResume, setArmResume] = useState(false);
  const armTimer = useRef<ReturnType<typeof setTimeout>>();

  const resume = async () => {
    const body = text.trim();
    if (!body || sending) return;
    // First click arms; a second click within the window fires. Relaunching a
    // dead task is heavier than a live nudge, so it takes an explicit confirm.
    if (!armResume) {
      setArmResume(true);
      clearTimeout(armTimer.current);
      armTimer.current = setTimeout(() => setArmResume(false), 4000);
      return;
    }
    clearTimeout(armTimer.current);
    setArmResume(false);
    setSending(true);
    try {
      const res = await resumeTaskWithContext(target.project_id, target.task_id, {
        injected_context: body,
        prefer_same_session: true,
      });
      // Leave the typed text intact on failure only; on success clear it.
      setText("");
      toast(resumeModeToast(res.resume_mode), "success");
    } catch (err) {
      // Keep the text so the user can retry (400 empty, 404 not-found,
      // live-claim-safety refusal, …).
      toast(`Resume failed: ${(err as Error)?.message ?? err}`, "error");
    } finally {
      setSending(false);
    }
  };

  return (
    <div className="composer" style={{ flexDirection: "column", alignItems: "stretch", gap: 4 }}>
      <div style={{ display: "flex", gap: 8 }}>
        <input
          type="text"
          value={text}
          disabled={sending}
          placeholder="Resume with this context…"
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey || !e.shiftKey)) {
              e.preventDefault();
              void resume();
            }
          }}
        />
        <button
          onClick={() => void resume()}
          disabled={sending || !text.trim()}
          style={{ color: armResume ? "#e06c5f" : undefined }}
          title="Relaunch this task and continue it with your context"
        >
          {sending
            ? "Resuming…"
            : armResume
              ? "Confirm resume?"
              : "Resume"}
        </button>
      </div>
      <div style={{ fontSize: 10, color: "#6b757e" }}>
        This session is finished — submitting relaunches the task and continues
        it with your text as context.
      </div>
    </div>
  );
}
