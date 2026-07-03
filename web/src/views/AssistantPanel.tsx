import { useEffect, useRef, useState, type ReactNode } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { useQuery } from "@tanstack/react-query";
import {
  assistantChatStream,
  assistantStatus,
  uploadAttachment,
  type AssistantHistoryMessage,
} from "../lib/api";
import { ALL_PROJECTS, useUI } from "../store/ui";
import { useAssistant, type AssistantToolRecord, type AssistantMessage } from "../store/assistant";
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
  const recordToolCall = useAssistant((s) => s.recordToolCall);
  const recordToolResult = useAssistant((s) => s.recordToolResult);
  const setLastAssistantStatus = useAssistant((s) => s.setLastAssistantStatus);
  const markLastAssistantCancelled = useAssistant((s) => s.markLastAssistantCancelled);
  const clear = useAssistant((s) => s.clear);
  const busy = useAssistant((s) => s.busy);
  const setBusy = useAssistant((s) => s.setBusy);

  const [text, setText] = useState("");
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [dragOver, setDragOver] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  // Abort handle for the currently in-flight assistant request. Set inside
  // send() and cleared once the request settles; used by cancel() to abort
  // the fetch (which in turn causes the server to see r.Context() cancel).
  const abortRef = useRef<AbortController | null>(null);
  // Timestamp of the last Escape keypress, used to detect a double-Esc
  // (two presses within 500ms) as the cancellation shortcut.
  const lastEscRef = useRef<number>(0);

  // cancel aborts the in-flight assistant request and marks the streaming
  // message as cancelled. Safe to call when nothing is in flight (no-op).
  function cancel() {
    const controller = abortRef.current;
    if (!controller) return;
    controller.abort();
    abortRef.current = null;
    markLastAssistantCancelled();
    setBusy(false);
  }

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

  // Double-Escape cancels an in-flight assistant request. We track the last
  // Esc timestamp on a ref; two presses within 500ms trigger cancel(). The
  // listener is scoped to the whole document so it works whether the focus
  // is on the textarea, a chip, or elsewhere in the panel.
  useEffect(() => {
    if (!active || !busy) {
      lastEscRef.current = 0;
      return;
    }
    function onKeyDown(e: KeyboardEvent) {
      if (e.key !== "Escape") return;
      const now = Date.now();
      if (now - lastEscRef.current < 500) {
        e.preventDefault();
        e.stopPropagation();
        cancel();
        lastEscRef.current = 0;
      } else {
        lastEscRef.current = now;
      }
    }
    document.addEventListener("keydown", onKeyDown, true);
    return () => document.removeEventListener("keydown", onKeyDown, true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active, busy]);

  async function send() {
    if (busy) return;
    const prompt = text.trim();
    if (!prompt && attachments.length === 0) return;
    if (!project && attachments.length > 0) {
      toast("Select a project before sending attachments", "error");
      return;
    }
    const files = attachments;
    // Snapshot the transcript BEFORE the new user turn is appended so we can
    // serialize it as history for the server. The current turn is sent as
    // the `message` field, not repeated inside history.
    const historySnapshot = buildHistory(useAssistant.getState().messages);
    setText("");
    setAttachments([]);
    append({
      role: "user",
      text: prompt || `[${files.length} attachment${files.length === 1 ? "" : "s"}]`,
    });
    setBusy(true);
    const controller = new AbortController();
    abortRef.current = controller;
    try {
      const uploaded: string[] = [];
      for (const f of files) {
        const blob = await (await fetch(f.dataUrl)).blob();
        const res = await uploadAttachment(project, blob, f.filename, { source: "assistant" });
        uploaded.push(res.attachment.id);
      }
      append({ role: "assistant", text: "" });
      setLastAssistantStatus("thinking…");
      let streamedReply = "";
      await assistantChatStream(
        {
          project: project || undefined,
          message: prompt,
          model: model || undefined,
          attachments: uploaded,
          history: historySnapshot,
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
          if (event.type === "tool_call" && event.tool_call) {
            recordToolCall(event.tool_call);
            setLastAssistantStatus(statusForToolCall(event.tool_call));
            return;
          }
          if (event.type === "tool_result" && event.tool_result) {
            recordToolResult(event.tool_result);
            setLastAssistantStatus(statusForToolResult(event.tool_result));
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
        controller.signal,
      );
    } catch (e) {
      // AbortError comes from the user hitting Stop / double-Esc. That's not
      // a failure — cancel() has already updated the message state.
      if (controller.signal.aborted) {
        return;
      }
      toast(e instanceof Error ? e.message : "Assistant failed", "error");
      append({ role: "assistant", text: "Assistant request failed." });
    } finally {
      // Only clear the abort ref if it still points to our controller. If
      // cancel() already swapped it to null, leave it alone.
      if (abortRef.current === controller) {
        abortRef.current = null;
      }
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
            {m.toolCalls && m.toolCalls.length > 0 ? (
              <ToolCallChips records={m.toolCalls} />
            ) : null}
            <div className="ctl-part-text">
              {m.text ? (
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{m.text}</ReactMarkdown>
              ) : m.runtimeStatus ? (
                <span className="assistant-stream-placeholder">{m.runtimeStatus}</span>
              ) : m.role === "assistant" && !m.cancelled ? (
                <span className="assistant-stream-placeholder">streaming…</span>
              ) : null}
              {m.cancelled ? (
                <span className="assistant-cancelled-badge">cancelled</span>
              ) : null}
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
          {busy ? (
            <button
              className="btn danger sm assistant-stop-btn"
              onClick={() => cancel()}
              title="Cancel this request (or press Esc twice)"
            >
              Stop
            </button>
          ) : (
            <button
              className="btn primary sm"
              disabled={!statusQ.data?.available}
              onClick={() => void send()}
            >
              Send
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

// ToolCallChips renders one collapsible row per tool the assistant called
// during the current turn. Reads render collapsed by default; writes and
// destructive proposals render open so the user can see what happened
// without clicking. Each chip shows tool name, tier badge, and status glyph;
// expanded, it dumps the decoded args + result JSON for inspection.
function ToolCallChips({ records }: { records: AssistantToolRecord[] }) {
  const [open, setOpen] = useState<Record<string, boolean>>({});
  return (
    <div className="assistant-tool-chips">
      {records.map((rec) => {
        const id = rec.call.id;
        const tier = rec.call.tier;
        const status = rec.result?.status || (rec.result ? "completed" : "running");
        const isOpen = open[id] ?? (tier !== "read");
        const glyph =
          status === "completed" ? "✓" :
          status === "failed" ? "✗" :
          status === "proposed" ? "?" :
          "…";
        const argsText = typeof rec.call.args === "string"
          ? rec.call.args
          : rec.call.args
            ? JSON.stringify(rec.call.args, null, 2)
            : "";
        return (
          <div key={id} className={`assistant-tool-chip tier-${tier} status-${status} ${isOpen ? "open" : ""}`}>
            <button
              type="button"
              className="assistant-tool-chip-head"
              onClick={() => setOpen((o) => ({ ...o, [id]: !isOpen }))}
              aria-expanded={isOpen}
            >
              <span className="assistant-tool-chip-glyph">{glyph}</span>
              <span className="assistant-tool-chip-name">{rec.call.name}</span>
              <span className="assistant-tool-chip-tier">{tier}</span>
              {rec.result?.error ? (
                <span className="assistant-tool-chip-err">{rec.result.error}</span>
              ) : null}
            </button>
            {isOpen ? (
              <div className="assistant-tool-chip-body">
                {argsText ? (
                  <div>
                    <div className="assistant-tool-chip-label">args</div>
                    <pre className="assistant-tool-chip-pre">{tryFormatJson(argsText)}</pre>
                  </div>
                ) : null}
                {rec.result?.result !== undefined ? (
                  <div>
                    <div className="assistant-tool-chip-label">result</div>
                    <pre className="assistant-tool-chip-pre">
                      {JSON.stringify(rec.result.result, null, 2)}
                    </pre>
                  </div>
                ) : null}
                {rec.result?.proposed ? (
                  <div className="assistant-tool-chip-note">
                    Proposed — waiting for user confirmation before executing.
                  </div>
                ) : null}
              </div>
            ) : null}
          </div>
        );
      })}
    </div>
  );
}

function tryFormatJson(input: string): string {
  try {
    return JSON.stringify(JSON.parse(input), null, 2);
  } catch {
    return input;
  }
}

// statusForToolCall renders a short, live-status label describing the tool
// the assistant just decided to invoke. Reads say "reading X\u2026", writes say
// "running X\u2026", destructive proposals show a "confirming X\u2026" flavor.
function statusForToolCall(call: { name: string; tier: string }): string {
  switch (call.tier) {
    case "read":
      return `reading ${call.name}\u2026`;
    case "write":
      return `running ${call.name}\u2026`;
    case "destructive":
      return `awaiting confirmation for ${call.name}`;
    default:
      return `calling ${call.name}\u2026`;
  }
}

// statusForToolResult produces the transient status shown between a
// tool_result arriving and the next event (text delta or another tool call).
// For completed tools, "processed X". For failures, expose the error briefly.
// Proposals stay in the "awaiting" state until the user acts.
function statusForToolResult(result: {
  name: string;
  status: string;
  error?: string;
}): string {
  if (result.status === "failed") {
    return `${result.name} failed${result.error ? ": " + result.error : ""}`;
  }
  if (result.status === "proposed") {
    return `${result.name} needs confirmation`;
  }
  return `processed ${result.name}\u2026`;
}

// buildHistory converts the in-memory transcript into the compact history
// payload the server replays into the model. See the AssistantHistoryMessage
// type in lib/api.ts for the wire shape.
//
// For each assistant turn we emit:
//   1. one role="assistant" entry carrying the reply text plus any tool_calls
//      that resolved successfully (proposed calls are omitted — the user
//      hadn't confirmed them yet, so they never really "ran").
//   2. one role="tool" entry per successful tool_call, WITHOUT the result
//      payload. The server substitutes a placeholder body when it forwards
//      to the model, so the model remembers what it invoked and how it
//      resolved without having to re-transmit the raw data.
function buildHistory(messages: AssistantMessage[]): AssistantHistoryMessage[] {
  const out: AssistantHistoryMessage[] = [];
  for (const m of messages) {
    if (m.role === "user") {
      // Skip empty placeholders so the server doesn't see phantom turns.
      if (!m.text) continue;
      out.push({ role: "user", content: m.text });
      continue;
    }
    // assistant turn
    const usable = (m.toolCalls || []).filter(
      (rec): rec is AssistantToolRecord & { result: NonNullable<AssistantToolRecord["result"]> } =>
        !!rec.result && rec.result.status !== "proposed",
    );
    // Only emit an assistant turn if there's something to reference — either
    // reply text, tool calls, or both. An empty assistant slot (from a
    // streaming placeholder that never resolved) is dropped.
    if (!m.text && usable.length === 0) continue;
    out.push({
      role: "assistant",
      content: m.text || "",
      tool_calls: usable.length
        ? usable.map((rec) => ({
            id: rec.call.id,
            name: rec.call.name,
            arguments:
              typeof rec.call.args === "string"
                ? rec.call.args
                : rec.call.args
                  ? JSON.stringify(rec.call.args)
                  : "{}",
          }))
        : undefined,
    });
    for (const rec of usable) {
      out.push({
        role: "tool",
        tool_call_id: rec.call.id,
        name: rec.call.name,
        status: rec.result.status,
      });
    }
  }
  return out;
}
