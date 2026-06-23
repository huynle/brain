import { useEffect, useRef, useState, type ReactNode } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { useQuery } from "@tanstack/react-query";
import {
  assistantChatStream,
  assistantStatus,
  uploadAttachment,
} from "../lib/api";
import { ALL_PROJECTS, useUI } from "../store/ui";
import { useAssistant } from "../store/assistant";
import { fileToAttachment, isImageType, type Attachment } from "./control/images";

interface Props {
  // Whether this host wants the chat to be active (e.g. don't fetch status if
  // the panel is rendered but not mounted/visible to the user).
  active: boolean;
  // Optional element rendered to the right of the project pill in the header
  // (typically a close × on overlays, a collapse arrow on the sidebar, or both).
  headerActions?: ReactNode;
  // Optional class added to the root <div> so each host can apply its own
  // surface styles (sidebar fills its parent; overlay anchors are absolute).
  className?: string;
}

// Shared chat body for the Brain Assistant. Used by the persistent right
// sidebar (desktop wide) and the overlay drawer/bottom sheet (desktop narrow /
// mobile). Owns its own composer state (text, attachments) but reads the
// conversation history and busy flag from the global useAssistant store so
// switching between sidebar and overlay preserves the conversation.
export function AssistantPanel({ active, headerActions, className }: Props) {
  const activeProject = useUI((s) => s.activeProject);
  const setProjectSheetOpen = useUI((s) => s.setProjectSheetOpen);
  const project = activeProject === ALL_PROJECTS ? "" : activeProject;
  const view = useUI((s) => s.view);
  const toast = useUI((s) => s.toast);
  const focusSeq = useUI((s) => s.assistantFocusSeq);

  const messages = useAssistant((s) => s.messages);
  const append = useAssistant((s) => s.append);
  const updateLastAssistant = useAssistant((s) => s.updateLastAssistant);
  const clear = useAssistant((s) => s.clear);
  const busy = useAssistant((s) => s.busy);
  const setBusy = useAssistant((s) => s.setBusy);

  const [text, setText] = useState("");
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [dragOver, setDragOver] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // Use whatever the server has configured. No client-side override.
  const model = "";

  const statusQ = useQuery({
    queryKey: ["assistant-status"],
    queryFn: assistantStatus,
    enabled: active,
    staleTime: 30_000,
    retry: false,
  });

  // Auto-scroll the message list to the bottom whenever new content arrives or
  // the busy spinner toggles.
  useEffect(() => {
    if (!active) return;
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messages, busy, active]);

  useEffect(() => {
    if (!active) return;
    window.setTimeout(() => textareaRef.current?.focus(), 0);
  }, [active, focusSeq]);

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
    append({
      role: "user",
      text: prompt || `[${files.length} attachment${files.length === 1 ? "" : "s"}]`,
    });
    setBusy(true);
    try {
      const uploaded: string[] = [];
      for (const f of files) {
        const blob = await (await fetch(f.dataUrl)).blob();
        const res = await uploadAttachment(project, blob, f.filename, { source: "assistant" });
        uploaded.push(res.attachment.id);
      }
      append({ role: "assistant", text: "" });
      let streamedReply = "";
      await assistantChatStream(
        {
          project: project || undefined,
          message: prompt,
          model: model || undefined,
          attachments: uploaded,
          // Always feed in current PWA context so the planner defaults its
          // create_task / create_goal / create_automation / create_entry actions
          // to the active project the user is looking at.
          context: {
            view: "assistant",
            active_project: project || ALL_PROJECTS,
            active_view: view,
          },
        },
        (event) => {
          if (event.type === "delta" && event.delta) {
            streamedReply += event.delta;
            updateLastAssistant(streamedReply);
            return;
          }
          if (event.type === "done") {
            const finalReply = event.reply || streamedReply;
            updateLastAssistant(finalReply, {
              reply: finalReply,
              executed_actions: event.executed_actions || [],
              proposed_actions: event.proposed_actions || [],
            });
            return;
          }
          if (event.type === "error") {
            throw new Error(event.error || "Assistant stream failed");
          }
        },
      );
    } catch (e) {
      toast(e instanceof Error ? e.message : "Assistant failed", "error");
      append({ role: "assistant", text: "Assistant request failed." });
    } finally {
      setBusy(false);
    }
  }

  async function addAttachmentsFromTransfer(dt: DataTransfer | null) {
    if (!dt) return;
    const files: File[] = [];
    const items = dt.items ? Array.from(dt.items) : [];
    for (const item of items) {
      if (item.kind !== "file") continue;
      const file = item.getAsFile();
      if (file) files.push(file);
    }
    if (files.length === 0 && dt.files) files.push(...Array.from(dt.files));
    if (files.length === 0) return;
    const next = await Promise.all(files.map((file) => fileToAttachment(file)));
    setAttachments((a) => [...a, ...next]);
  }

  function formatBytes(size: number | undefined): string {
    if (!size) return "";
    if (size < 1024) return String(size) + " B";
    if (size < 1024 * 1024) return String(Math.round(size / 102.4) / 10) + " KB";
    return String(Math.round(size / 1024 / 102.4) / 10) + " MB";
  }

  return (
    <div className={`assistant-panel ${className ?? ""}`}>
      <header className="assistant-head">
        <div className="assistant-head-main">
          <strong>Brain Assistant</strong>
          <div className="faint">
            {statusQ.data?.available
              ? `${statusQ.data.mode}${statusQ.data.model ? ` · ${statusQ.data.model}` : ""}`
              : statusQ.data?.reason || "checking assistant..."}
          </div>
        </div>
        <button
          type="button"
          className={`assistant-project-pill ${project ? "" : "warn"}`}
          onClick={() => setProjectSheetOpen(true)}
          title={
            project
              ? `Acting on project: ${project} (tap to change)`
              : "No project selected — tap to choose"
          }
        >
          {project ? project : "no project"}
        </button>
        {messages.length > 0 && (
          <button
            type="button"
            className="icon-btn"
            onClick={() => clear()}
            title="Clear conversation"
            aria-label="Clear conversation"
          >
            ⟲
          </button>
        )}
        {headerActions}
      </header>

      {!project && (
        <div className="assistant-project-prompt">
          <div className="assistant-project-prompt-text">
            Pick a project so create-task / create-goal / create-automation /
            create-entry actions land in the right place.
          </div>
          <button
            type="button"
            className="btn primary sm"
            onClick={() => setProjectSheetOpen(true)}
          >
            Choose project
          </button>
        </div>
      )}

      <div className="assistant-scroll" ref={scrollRef}>
        {messages.length === 0 && (
          <div className="assistant-empty">
            Ask Brain to create tasks, goals, automations, or entries. Explicit create requests run immediately.
          </div>
        )}
        {messages.map((m, i) => (
          <div key={i} className={`assistant-msg ${m.role}`}>
            <div className="ctl-msg-role">{m.role === "user" ? "you" : "assistant"}</div>
            <div className="ctl-part-text">
              {m.text ? (
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{m.text}</ReactMarkdown>
              ) : (
                <span className="assistant-stream-placeholder">streaming…</span>
              )}
            </div>
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
      </div>

      <div
        className={`assistant-composer ctl-composer ${dragOver ? "dragover" : ""}`}
        onPaste={(e) => void addAttachmentsFromTransfer(e.clipboardData)}
        onDragOver={(e) => {
          e.preventDefault();
          setDragOver(true);
        }}
        onDragLeave={() => setDragOver(false)}
        onDrop={(e) => {
          e.preventDefault();
          setDragOver(false);
          void addAttachmentsFromTransfer(e.dataTransfer);
        }}
      >
        {attachments.length > 0 && (
          <div className="ctl-attach-row">
            {attachments.map((a) => (
              <div
                className={"ctl-attach" + (isImageType(a.mime) ? "" : " file")}
                key={a.id}
                title={a.filename + (a.size ? " · " + formatBytes(a.size) : "")}
              >
                {isImageType(a.mime) ? (
                  <img src={a.dataUrl} alt={a.filename} />
                ) : (
                  <div className="ctl-attach-file">
                    <span className="ctl-attach-file-icon">□</span>
                    <span className="ctl-attach-file-name">{a.filename}</span>
                    <span className="ctl-attach-file-meta">{formatBytes(a.size) || a.mime}</span>
                  </div>
                )}
                <button
                  className="ctl-attach-x"
                  onClick={() => setAttachments((xs) => xs.filter((x) => x.id !== a.id))}
                  title="Remove attachment"
                  aria-label="Remove attachment"
                >
                  ×
                </button>
              </div>
            ))}
          </div>
        )}
        <textarea
          ref={textareaRef}
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
          <span className="faint" title="Configured by the server">
            model: {statusQ.data?.model || "not configured"}
          </span>
          <button
            className="btn primary sm"
            disabled={busy || !statusQ.data?.available}
            onClick={() => void send()}
          >
            {busy ? "Sending..." : "Send"}
          </button>
        </div>
      </div>
    </div>
  );
}
