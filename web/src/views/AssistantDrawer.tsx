import { useEffect, useRef, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { useQuery } from "@tanstack/react-query";
import {
  assistantChat,
  assistantStatus,
  uploadAttachment,
  type AssistantChatResponse,
} from "../lib/api";
import { ALL_PROJECTS, useUI } from "../store/ui";
import { attachmentsFromDataTransfer, type Attachment } from "./control/images";
import { Spinner } from "../components/common/states";

interface Message {
  role: "user" | "assistant";
  text: string;
  result?: AssistantChatResponse;
}

export function AssistantDrawer({ open, onClose }: { open: boolean; onClose: () => void }) {
  const activeProject = useUI((s) => s.activeProject);
  const project = activeProject === ALL_PROJECTS ? "" : activeProject;
  const toast = useUI((s) => s.toast);
  const [text, setText] = useState("");
  const [messages, setMessages] = useState<Message[]>([]);
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [busy, setBusy] = useState(false);
  const [dragOver, setDragOver] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);

  const statusQ = useQuery({
    queryKey: ["assistant-status"],
    queryFn: assistantStatus,
    enabled: open,
    staleTime: 30_000,
    retry: false,
  });

  useEffect(() => {
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messages, busy, open]);

  if (!open) return null;

  async function send() {
    const prompt = text.trim();
    if (!prompt && attachments.length === 0) return;
    if (!project && attachments.length > 0) {
      toast("Select a project before sending attachments", "error");
      return;
    }
    const files = attachments;
    setText("");
    setAttachments([]);
    setMessages((m) => [...m, { role: "user", text: prompt || `[${files.length} image${files.length === 1 ? "" : "s"}]` }]);
    setBusy(true);
    try {
      const uploaded: string[] = [];
      for (const f of files) {
        const blob = await (await fetch(f.dataUrl)).blob();
        const res = await uploadAttachment(project, blob, f.filename, { source: "assistant" });
        uploaded.push(res.attachment.id);
      }
      const res = await assistantChat({
        project: project || undefined,
        message: prompt,
        attachments: uploaded,
        context: { view: "assistant" },
      });
      setMessages((m) => [...m, { role: "assistant", text: res.reply, result: res }]);
    } catch (e) {
      toast(e instanceof Error ? e.message : "Assistant failed", "error");
      setMessages((m) => [...m, { role: "assistant", text: "Assistant request failed." }]);
    } finally {
      setBusy(false);
    }
  }

  async function addImages(dt: DataTransfer | null) {
    const imgs = await attachmentsFromDataTransfer(dt);
    if (imgs.length) setAttachments((a) => [...a, ...imgs]);
  }

  return (
    <div className="assistant-shell" role="dialog" aria-label="Brain Assistant">
      <div className="assistant-backdrop" onClick={onClose} />
      <aside className="assistant-drawer">
        <header className="assistant-head">
          <div>
            <strong>Brain Assistant</strong>
            <div className="faint">
              {statusQ.data?.available
                ? `${statusQ.data.mode}${statusQ.data.model ? ` · ${statusQ.data.model}` : ""}`
                : statusQ.data?.reason || "checking assistant..."}
            </div>
          </div>
          <button className="icon-btn" onClick={onClose} title="Close assistant">×</button>
        </header>

        <div className="assistant-scroll" ref={scrollRef}>
          {messages.length === 0 && (
            <div className="assistant-empty">
              Ask Brain to create tasks, goals, automations, or entries. Explicit create requests run immediately.
            </div>
          )}
          {messages.map((m, i) => (
            <div key={i} className={`assistant-msg ${m.role}`}>
              <div className="ctl-msg-role">{m.role === "user" ? "you" : "assistant"}</div>
              <div className="ctl-part-text"><ReactMarkdown remarkPlugins={[remarkGfm]}>{m.text}</ReactMarkdown></div>
              {m.result?.executed_actions?.length ? (
                <div className="assistant-actions">
                  {m.result.executed_actions.map((a, idx) => (
                    <div key={idx} className="ctl-tool">
                      <div className="ctl-tool-head">executed · {a.type} · {a.status}</div>
                      {a.error ? <pre className="ctl-tool-out">{a.error}</pre> : null}
                    </div>
                  ))}
                </div>
              ) : null}
              {m.result?.proposed_actions?.length ? (
                <div className="assistant-actions">
                  {m.result.proposed_actions.map((a, idx) => (
                    <div key={idx} className="ctl-tool">
                      <div className="ctl-tool-head">proposed · {a.type}</div>
                      <pre className="ctl-tool-out">{JSON.stringify(a.payload, null, 2)}</pre>
                    </div>
                  ))}
                </div>
              ) : null}
            </div>
          ))}
          {busy && <div className="assistant-msg assistant"><Spinner /> Thinking...</div>}
        </div>

        <div
          className={`assistant-composer ctl-composer ${dragOver ? "dragover" : ""}`}
          onPaste={(e) => void addImages(e.clipboardData)}
          onDragOver={(e) => { e.preventDefault(); setDragOver(true); }}
          onDragLeave={() => setDragOver(false)}
          onDrop={(e) => { e.preventDefault(); setDragOver(false); void addImages(e.dataTransfer); }}
        >
          {attachments.length > 0 && (
            <div className="ctl-attach-row">
              {attachments.map((a) => (
                <div className="ctl-attach" key={a.id} title={a.filename}>
                  <img src={a.dataUrl} alt={a.filename} />
                  <button className="ctl-attach-x" onClick={() => setAttachments((xs) => xs.filter((x) => x.id !== a.id))}>×</button>
                </div>
              ))}
            </div>
          )}
          <textarea
            rows={3}
            value={text}
            placeholder="Create a task, goal, automation, or brain entry..."
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                void send();
              }
            }}
          />
          <div className="btn-row">
            <span className="faint">project: {project || "select one"}</span>
            <button className="btn primary sm" disabled={busy || !statusQ.data?.available} onClick={() => void send()}>
              {busy ? "Sending..." : "Send"}
            </button>
          </div>
        </div>
      </aside>
    </div>
  );
}
