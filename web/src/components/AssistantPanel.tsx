/**
 * AssistantPanel — wireframe-parity port of `renderAssistantPanel`.
 *
 * Right-side slide-in with:
 *   • Suggested next move (from live attention queue)
 *   • Ask / command composer (real streaming via assistantChatStream)
 *   • Quick actions
 *   • Context summary
 */
import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useWorkspace } from "../store/workspace";
import { useProjects } from "../hooks/useProjects";
import { useLive } from "../lib/sse";
import { useRunners } from "../hooks/useRunners";
import { useUI } from "../store/ui";
import { deriveFeatures } from "../lib/features";
import { assistantChatStream, ApiError } from "../lib/api";

const LIFECYCLE_LABELS: Record<string, string> = {
  "in-progress": "active",
  blocked: "blocked",
  finished: "finished",
  "mr-open": "MR open",
  merged: "merged",
};

export function AssistantPanel(): JSX.Element | null {
  const open = useWorkspace((s) => s.assistantOpen);
  const close = () => useWorkspace.getState().setAssistantOpen(false);
  const setCommandOpen = useWorkspace((s) => s.setCommandOpen);
  const openFeatureDrawer = useWorkspace((s) => s.openFeatureDrawer);
  const { data: projects } = useProjects();
  const liveProjects = useLive((s) => s.projects);
  const { runners } = useRunners();
  const toast = useUI((s) => s.toast);

  const [prompt, setPrompt] = useState("");
  const [reply, setReply] = useState("");
  const [busy, setBusy] = useState(false);
  const abortRef = useRef<AbortController | null>(null);

  // Reserve layout space for the panel on desktop (see `body.assistant-open`
  // rules in global.css) so it docks beside the workspace instead of
  // overlapping it. Mobile keeps the slide-over behavior.
  useEffect(() => {
    document.body.classList.toggle("assistant-open", open);
    return () => {
      document.body.classList.remove("assistant-open");
    };
  }, [open]);

  const attention = useMemo(() => {
    const out: Array<{ projectId: string; featureId: string; name: string; lifecycle: string }> = [];
    for (const pid of projects ?? []) {
      const tasks = liveProjects[pid]?.tasks ?? [];
      const feats = deriveFeatures(tasks, pid);
      for (const f of feats) {
        if (f.lifecycle === "blocked" || f.lifecycle === "mr-open") {
          out.push({
            projectId: pid,
            featureId: f.id,
            name: f.name,
            lifecycle: LIFECYCLE_LABELS[f.lifecycle] ?? f.lifecycle,
          });
        }
      }
    }
    return out;
  }, [projects, liveProjects]);

  if (!open) return null;
  if (typeof document === "undefined") return null;

  const send = async () => {
    if (!prompt.trim() || busy) return;
    setBusy(true);
    setReply("");
    const ac = new AbortController();
    abortRef.current = ac;
    try {
      let acc = "";
      await assistantChatStream(
        { message: prompt.trim() },
        (event) => {
          if (event.type === "delta" && event.delta) {
            acc += event.delta;
            setReply(acc);
            return;
          }
          if (event.type === "done") {
            setReply(event.reply || acc);
            return;
          }
          if (event.type === "error") {
            throw new Error(event.error || "Assistant stream failed");
          }
        },
        ac.signal,
      );
    } catch (err) {
      const msg =
        err instanceof ApiError
          ? `Assistant error: ${err.message}`
          : (err as Error).name === "AbortError"
            ? ""
            : `Assistant error: ${(err as Error).message}`;
      if (msg) toast(msg, "error");
    } finally {
      setBusy(false);
      abortRef.current = null;
    }
  };

  return createPortal(
    <aside className="assistant-panel">
      <div className="assistant-head">
        <div>
          <div className="assistant-kicker">Brain assistant</div>
          <h3>Workflow copilot</h3>
        </div>
        <button className="drawer-close" onClick={close}>
          ×
        </button>
      </div>

      <div className="assistant-card primary">
        <div className="assistant-title">Suggested next move</div>
        {attention.length > 0 ? (
          <>
            <p>
              Review <b>{attention[0].name}</b> — it's{" "}
              {attention[0].lifecycle} and blocking clean execution.
            </p>
            <div className="assistant-actions">
              <button
                onClick={() =>
                  openFeatureDrawer(attention[0].projectId, attention[0].featureId)
                }
              >
                Open suggestion
              </button>
            </div>
          </>
        ) : (
          <p>
            No blockers right now. Queue the next ready feature and keep
            Brain entries updated as work lands.
          </p>
        )}
      </div>

      <div className="assistant-card">
        <div className="assistant-title">Ask</div>
        <textarea
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          placeholder="Ask about project status, generate tasks, summarize entries, or plan the next feature…"
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
              e.preventDefault();
              void send();
            }
          }}
        />
        <div className="assistant-actions">
          <button
            className="primary"
            onClick={() => void send()}
            disabled={busy || !prompt.trim()}
          >
            {busy ? "Sending…" : "Send  ⌘↵"}
          </button>
          {busy && (
            <button onClick={() => abortRef.current?.abort()}>Stop</button>
          )}
        </div>
        {reply && (
          <div
            style={{
              marginTop: 10,
              padding: 8,
              background: "#0a0c0e",
              border: "1px solid #1a1e22",
              borderRadius: 4,
              fontSize: 11,
              whiteSpace: "pre-wrap",
              maxHeight: 240,
              overflowY: "auto",
            }}
          >
            {reply}
          </div>
        )}
      </div>

      <div className="assistant-card">
        <div className="assistant-title">Quick actions</div>
        <button onClick={() => setCommandOpen(true)}>
          Open command palette (⌘K)
        </button>
      </div>

      <div className="assistant-card">
        <div className="assistant-title">Context</div>
        <p>
          {attention.length} attention items ·{" "}
          {(projects ?? []).length} projects ·{" "}
          {runners.filter((r) => r.status === "online").length} runners online.
        </p>
      </div>
    </aside>,
    document.body,
  );
}
