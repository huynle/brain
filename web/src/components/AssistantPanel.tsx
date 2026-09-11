/**
 * AssistantPanel — wireframe-parity port of `renderAssistantPanel`.
 *
 * Right-side slide-in with:
 *   • Suggested next move (from live attention queue)
 *   • Multi-turn chat thread (streaming via assistantChatStream; prior turns
 *     are replayed to the stateless server through the `history` field)
 *   • Quick actions
 *   • Context summary
 *
 * Conversation state lives in useAssistantChat (persisted to localStorage),
 * so the thread survives closing the panel and page reloads.
 */
import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useWorkspace } from "../store/workspace";
import { useProjects } from "../hooks/useProjects";
import { useLive } from "../lib/sse";
import { useRunners } from "../hooks/useRunners";
import { useUI } from "../store/ui";
import { useIsMobile } from "../hooks/useIsMobile";
import { useEdgeResize } from "../hooks/useEdgeResize";
import { deriveFeatures } from "../lib/features";
import { LIFECYCLE_TONE } from "./common/LifecycleBadge";
import { useMergeRequests } from "../hooks/useMergeRequests";
import {
  useAssistantChat,
  type AssistantToolChip,
} from "../store/assistantChat";
import {
  assistantChatStream,
  ApiError,
  type AssistantHistoryMessage,
} from "../lib/api";

// Cap on how many prior history entries are replayed per request. The server
// strips tool payloads already; this just bounds prompt growth on long chats.
const HISTORY_REPLAY_LIMIT = 40;

// One pasted/dropped image queued for the next message.
type PendingImage = {
  id: string;
  name: string;
  dataUrl: string; // "data:image/...;base64,..."
  bytes: number; // approximate decoded size
};

// Per-image and total size caps for inline base64 images. The server accepts a
// 24MB request body; we keep well under it and leave headroom for the message
// text + replayed history. Base64 inflates raw bytes by ~33%.
const MAX_IMAGE_BYTES = 8 * 1024 * 1024; // 8MB per image (raw)
const MAX_TOTAL_IMAGE_BYTES = 16 * 1024 * 1024; // 16MB total (raw) per turn
const MAX_IMAGES = 6;

// readImageFile resolves a File/Blob to a base64 data URL, or rejects if it is
// not an image. Size is checked by the caller against the caps above.
function readImageFile(file: File | Blob): Promise<PendingImage> {
  return new Promise((resolve, reject) => {
    if (!file.type.startsWith("image/")) {
      reject(new Error("not an image"));
      return;
    }
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error ?? new Error("read failed"));
    reader.onload = () => {
      const dataUrl = String(reader.result || "");
      if (!dataUrl.startsWith("data:image/")) {
        reject(new Error("not an image"));
        return;
      }
      resolve({
        id: `img-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
        name: (file as File).name || "pasted-image",
        dataUrl,
        bytes: file.size,
      });
    };
    reader.readAsDataURL(file);
  });
}

// Module-level so an in-flight stream stays stoppable across panel
// close/reopen (the portal unmounts but the request keeps running and
// writes into the store).
let activeAbort: AbortController | null = null;

export function AssistantPanel(): JSX.Element | null {
  const open = useWorkspace((s) => s.assistantOpen);
  const close = () => useWorkspace.getState().setAssistantOpen(false);
  const setCommandOpen = useWorkspace((s) => s.setCommandOpen);
  const openInSidebar = useWorkspace((s) => s.openInSidebar);
  const assistantWidth = useWorkspace((s) => s.assistantWidth);
  const setAssistantWidth = useWorkspace((s) => s.setAssistantWidth);
  const { data: projects } = useProjects();
  const liveProjects = useLive((s) => s.projects);
  const { runners } = useRunners();
  const { openByProject } = useMergeRequests();
  const toast = useUI((s) => s.toast);
  const isMobile = useIsMobile();

  const [assistantHome, setAssistantHome] = useState(
    () => localStorage.getItem("brain.mobile.assistantHome") !== "false",
  );
  const [prompt, setPrompt] = useState("");
  // Pasted/dropped images for the NEXT message, as base64 data URLs. Sent to
  // the vision model on send() and cleared afterward (current-turn only; not
  // persisted into history).
  const [pendingImages, setPendingImages] = useState<PendingImage[]>([]);
  const turns = useAssistantChat((s) => s.turns);
  const busy = useAssistantChat((s) => s.busy);
  const threadRef = useRef<HTMLDivElement | null>(null);

  // ─── left-edge drag-resize ────────────────────────────────────────
  // The panel is always the rightmost column, so its width is the
  // distance from the pointer to the viewport's right edge. Mirrors the
  // sidebar dock's resizer (see SidebarDock.tsx).
  const startResize = useEdgeResize({
    computeWidth: (clientX) => window.innerWidth - clientX,
    onResize: setAssistantWidth,
    bodyClass: "assistant-resizing",
  });

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
      const feats = deriveFeatures(tasks, pid, openByProject.get(pid));
      for (const f of feats) {
        if (f.lifecycle === "blocked" || f.lifecycle === "ready-to-merge") {
          out.push({
            projectId: pid,
            featureId: f.id,
            name: f.name,
            lifecycle: LIFECYCLE_TONE[f.lifecycle].label,
          });
        }
      }
    }
    return out;
  }, [projects, liveProjects, openByProject]);

  // Keep the newest message in view while streaming.
  useEffect(() => {
    const el = threadRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [turns]);

  if (!open) return null;
  if (typeof document === "undefined") return null;

  const clearChat = () => {
    activeAbort?.abort();
    useAssistantChat.getState().clear();
  };

  // addImageFiles ingests image blobs from a paste or drop, enforcing the
  // per-image / total / count caps, and appends the survivors to pendingImages.
  const addImageFiles = async (files: (File | Blob)[]) => {
    const images = files.filter((f) => f.type.startsWith("image/"));
    if (images.length === 0) return;
    const accepted: PendingImage[] = [];
    let runningTotal = pendingImages.reduce((sum, p) => sum + p.bytes, 0);
    let count = pendingImages.length;
    for (const file of images) {
      if (count >= MAX_IMAGES) {
        toast(`At most ${MAX_IMAGES} images per message.`, "error");
        break;
      }
      if (file.size > MAX_IMAGE_BYTES) {
        toast(
          `Image too large (${Math.round(file.size / 1024 / 1024)}MB). Max ${MAX_IMAGE_BYTES / 1024 / 1024}MB each.`,
          "error",
        );
        continue;
      }
      if (runningTotal + file.size > MAX_TOTAL_IMAGE_BYTES) {
        toast(
          `Total image size exceeds ${MAX_TOTAL_IMAGE_BYTES / 1024 / 1024}MB for this message.`,
          "error",
        );
        break;
      }
      try {
        const img = await readImageFile(file);
        accepted.push(img);
        runningTotal += file.size;
        count += 1;
      } catch {
        // non-image or unreadable; skip silently
      }
    }
    if (accepted.length > 0) {
      setPendingImages((prev) => [...prev, ...accepted]);
    }
  };

  const removePendingImage = (id: string) =>
    setPendingImages((prev) => prev.filter((p) => p.id !== id));

  const send = async () => {
    const message = prompt.trim();
    const images = pendingImages;
    if ((!message && images.length === 0) || busy) return;
    setPrompt("");
    setPendingImages([]);
    const chat = useAssistantChat.getState();
    chat.beginTurn(
      message ||
        (images.length === 1 ? "(image)" : `(${images.length} images)`),
    );

    const ac = new AbortController();
    activeAbort = ac;
    let acc = "";
    const tools: AssistantToolChip[] = [];
    const patchAssistant = chat.patchAssistant;

    try {
      await assistantChatStream(
        {
          message,
          history: chat.history.slice(-HISTORY_REPLAY_LIMIT),
          ...(images.length > 0
            ? { images: images.map((i) => i.dataUrl) }
            : {}),
        },
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
      if (activeAbort === ac) activeAbort = null;
      patchAssistant({ content: acc });

      // Record the finished turn in the replay history. Tool calls are
      // flattened into one assistant tool_calls message followed by its tool
      // results — the pairing shape replayHistory on the server requires.
      // Only answered calls are kept (unanswered ones would be dropped
      // server-side anyway).
      const historyUserContent =
        message ||
        (images.length > 0
          ? images.length === 1
            ? "(image)"
            : `(${images.length} images)`
          : "");
      const entries: AssistantHistoryMessage[] = [
        { role: "user", content: historyUserContent },
      ];
      const answered = tools.filter((c) => c.status !== "running");
      if (answered.length > 0) {
        entries.push({
          role: "assistant",
          tool_calls: answered.map((c) => ({ id: c.id, name: c.name, arguments: c.args })),
        });
        for (const c of answered) {
          entries.push({
            role: "tool",
            tool_call_id: c.id,
            name: c.name,
            status: c.status,
          });
        }
      }
      if (acc) entries.push({ role: "assistant", content: acc });
      useAssistantChat.getState().finishTurn(entries);
    }
  };

  const asideStyle = {
    ["--assistant-w" as never]: `${assistantWidth}px`,
  } as React.CSSProperties;

  const panel = (
    <aside className="assistant-panel" style={asideStyle}>
      <div className="assistant-resizer" onPointerDown={startResize} />
      <div className="assistant-head">
        <div>
          <div className="assistant-kicker">Brain assistant</div>
          <h3>Workflow copilot</h3>
        </div>
        <button className="drawer-close" aria-label="Close assistant" onClick={close}>
          ×
        </button>
      </div>

      {isMobile && (
        <label className="assistant-home-setting">
          <input
            type="checkbox"
            checked={assistantHome}
            onChange={(e) => {
              setAssistantHome(e.target.checked);
              localStorage.setItem("brain.mobile.assistantHome", String(e.target.checked));
            }}
          />
          Open Assistant when Brain starts on mobile
        </label>
      )}
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
                  openInSidebar(
                    "feature-detail",
                    {
                      projectId: attention[0].projectId,
                      featureId: attention[0].featureId,
                    },
                    attention[0].name,
                  )
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

        {pendingImages.length > 0 && (
          <div className="assistant-image-chips">
            {pendingImages.map((img) => (
              <div key={img.id} className="assistant-image-chip" title={img.name}>
                <img src={img.dataUrl} alt={img.name} />
                <button
                  type="button"
                  className="assistant-image-remove"
                  aria-label={`Remove ${img.name}`}
                  onClick={() => removePendingImage(img.id)}
                >
                  ×
                </button>
              </div>
            ))}
          </div>
        )}

        <textarea
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          placeholder={
            turns.length > 0
              ? "Reply… (paste or drop images)"
              : "Ask about project status, generate tasks, summarize entries, or plan the next feature… (paste or drop images)"
          }
          onPaste={(e) => {
            const files = Array.from(e.clipboardData?.items ?? [])
              .filter((it) => it.kind === "file" && it.type.startsWith("image/"))
              .map((it) => it.getAsFile())
              .filter((f): f is File => f != null);
            if (files.length > 0) {
              e.preventDefault();
              void addImageFiles(files);
            }
          }}
          onDragOver={(e) => {
            if (Array.from(e.dataTransfer?.types ?? []).includes("Files")) {
              e.preventDefault();
            }
          }}
          onDrop={(e) => {
            const files = Array.from(e.dataTransfer?.files ?? []).filter((f) =>
              f.type.startsWith("image/"),
            );
            if (files.length > 0) {
              e.preventDefault();
              void addImageFiles(files);
            }
          }}
          onKeyDown={(e) => {
            // Enter sends; Shift+Enter (or ⌘/Ctrl+Enter) inserts a newline.
            if (e.key === "Enter" && !e.shiftKey && !e.metaKey && !e.ctrlKey) {
              e.preventDefault();
              void send();
            }
          }}
        />
        <div className="assistant-actions">
          <button
            className="primary"
            onClick={() => void send()}
            disabled={busy || (!prompt.trim() && pendingImages.length === 0)}
          >
            {busy ? "Sending…" : "Send  ↵"}
          </button>
          {busy && (
            <button onClick={() => activeAbort?.abort()}>Stop</button>
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
    </aside>
  );

  // Mobile: portal to document.body (fixed overlay). Desktop: render in
  // place — Dashboard mounts <AssistantPanel/> as a direct child of #app
  // so `grid-area: assistant` slots it in as a real grid column that
  // pushes the workspace aside instead of overlaying it. Mirrors
  // SidebarDock.tsx's mount strategy.
  return isMobile ? createPortal(panel, document.body) : panel;
}
