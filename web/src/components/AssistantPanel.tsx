/**
 * AssistantPanel — wireframe-parity port of `renderAssistantPanel`.
 *
 * Right-side slide-in with:
 *   • Suggested next move (from live attention queue)
 *   • Multi-turn chat thread (streaming via assistantChatStream; prior turns
 *     are replayed to the stateless server through the `history` field)
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
import {
  assistantChatStream,
  ApiError,
  type AssistantHistoryMessage,
} from "../lib/api";

const LIFECYCLE_LABELS: Record<string, string> = {
  "in-progress": "active",
  blocked: "blocked",
  finished: "finished",
  "mr-open": "MR open",
  merged: "merged",
};

// Cap on how many prior history entries are replayed per request. The server
// strips tool payloads already; this just bounds prompt growth on long chats.
const HISTORY_REPLAY_LIMIT = 40;

interface ToolChip {
  id: string;
  name: string;
  args: string;
  tier: string;
  status: string; // "running" until a tool_result lands
}

interface ChatTurn {
  role: "user" | "assistant";
  content: string;
  tools: ToolChip[];
  streaming?: boolean;
}

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
  const [turns, setTurns] = useState<ChatTurn[]>([]);
  const [busy, setBusy] = useState(false);
  const abortRef = useRef<AbortController | null>(null);
  // Compact replay history for the stateless server (survives across sends,
  // not across panel close/reopen — the transcript state carries both).
  const historyRef = useRef<AssistantHistoryMessage[]>([]);
  const threadRef = useRef<HTMLDivElement | null>(null);

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

  // Keep the newest message in view while streaming.
  useEffect(() => {
    const el = threadRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [turns]);

  if (!open) return null;
  if (typeof document === "undefined") return null;

  const clearChat = () => {
    abortRef.current?.abort();
    historyRef.current = [];
    setTurns([]);
  };

  const send = async () => {
    const message = prompt.trim();
    if (!message || busy) return;
    setBusy(true);
    setPrompt("");
    setTurns((t) => [
      ...t,
      { role: "user", content: message, tools: [] },
      { role: "assistant", content: "", tools: [], streaming: true },
    ]);

    const ac = new AbortController();
    abortRef.current = ac;
    let acc = "";
    const tools: ToolChip[] = [];

    const patchAssistant = (patch: Partial<ChatTurn>) =>
      setTurns((t) => {
        const next = t.slice();
        const last = next[next.length - 1];
        if (last?.role === "assistant") next[next.length - 1] = { ...last, ...patch };
        return next;
      });

    try {
      await assistantChatStream(
        { message, history: historyRef.current.slice(-HISTORY_REPLAY_LIMIT) },
        (event) => {
          if (event.type === "delta" && event.delta) {
            acc += event.delta;
            patchAssistant({ content: acc });
            return;
          }
          if (event.type === "tool_call" && event.tool_call) {
            const tc = event.tool_call;
            tools.push({
              id: tc.id,
              name: tc.name,
              args: typeof tc.args === "string" ? tc.args : JSON.stringify(tc.args ?? {}),
              tier: tc.tier,
              status: "running",
            });
            patchAssistant({ tools: tools.slice() });
            return;
          }
          if (event.type === "tool_result" && event.tool_result) {
            const tr = event.tool_result;
            const chip = tools.find((c) => c.id === tr.id);
            if (chip) {
              chip.status = tr.proposed ? "proposed" : tr.status;
              patchAssistant({ tools: tools.slice() });
            }
            return;
          }
          if (event.type === "done") {
            acc = event.reply || acc;
            patchAssistant({ content: acc });
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
      patchAssistant({ streaming: false, content: acc });

      // Record the finished turn in the replay history. Tool calls are
      // flattened into one assistant tool_calls message followed by its tool
      // results — the pairing shape replayHistory on the server requires.
      // Only answered calls are kept (unanswered ones would be dropped
      // server-side anyway).
      historyRef.current.push({ role: "user", content: message });
      const answered = tools.filter((c) => c.status !== "running");
      if (answered.length > 0) {
        historyRef.current.push({
          role: "assistant",
          tool_calls: answered.map((c) => ({ id: c.id, name: c.name, arguments: c.args })),
        });
        for (const c of answered) {
          historyRef.current.push({
            role: "tool",
            tool_call_id: c.id,
            name: c.name,
            status: c.status,
          });
        }
      }
      if (acc) historyRef.current.push({ role: "assistant", content: acc });
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

      <div className="assistant-card assistant-chat">
        <div className="assistant-chat-head">
          <div className="assistant-title">Ask</div>
          {turns.length > 0 && (
            <button className="assistant-chat-clear" onClick={clearChat}>
              New chat
            </button>
          )}
        </div>

        {turns.length > 0 && (
          <div className="assistant-thread" ref={threadRef}>
            {turns.map((turn, i) => (
              <div key={i} className={`assistant-msg ${turn.role}`}>
                <div className="assistant-msg-role">
                  {turn.role === "user" ? "You" : "Assistant"}
                </div>
                {turn.tools.length > 0 && (
                  <div className="assistant-msg-tools">
                    {turn.tools.map((c) => (
                      <details key={c.id} className={`assistant-tool-chip ${c.status}`}>
                        <summary>
                          {c.name}
                          <span className="assistant-tool-status">{c.status}</span>
                        </summary>
                        <pre>{c.args}</pre>
                      </details>
                    ))}
                  </div>
                )}
                <div className="assistant-msg-body">
                  {turn.content ||
                    (turn.streaming ? "Thinking…" : turn.role === "assistant" ? "(no reply)" : "")}
                </div>
              </div>
            ))}
          </div>
        )}

        <textarea
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          placeholder={
            turns.length > 0
              ? "Reply…"
              : "Ask about project status, generate tasks, summarize entries, or plan the next feature…"
          }
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
