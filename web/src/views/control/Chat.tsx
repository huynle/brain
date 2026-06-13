// Full chat experience against one OpenCode session on a remote instance:
// streaming assistant messages, collapsed tool cards, permission banners
// with allow/always/deny, abort, and a composer with agent/model selection.

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import {
  controlAbort,
  controlAgents,
  controlPrompt,
  controlProviders,
  controlRespondPermission,
} from "../../lib/api";
import { useUI } from "../../store/ui";
import { Spinner } from "../../components/common/states";
import type { OcMessage, OcPart, OcPermission, OcProvider } from "../../lib/types";
import { chatKey, useChat } from "./chatStore";
import {
  type Attachment,
  attachmentsFromDataTransfer,
  fileToAttachment,
} from "./images";

export function Chat({
  runnerId,
  instanceId,
  sessionId,
  sessionLabel,
}: {
  runnerId: string;
  instanceId: string;
  sessionId: string;
  sessionLabel?: string;
}) {
  const key = chatKey(runnerId, instanceId);
  const toast = useUI((s) => s.toast);
  const chat = useChat((s) => s.chats[key]);
  const attach = useChat((s) => s.attach);
  const hydrate = useChat((s) => s.hydrate);
  const optimistic = useChat((s) => s.optimisticUserMessage);
  const removePermission = useChat((s) => s.removePermission);
  const setBusy = useChat((s) => s.setBusy);

  const [text, setText] = useState("");
  const [agent, setAgent] = useState("");
  const [model, setModel] = useState(""); // "providerID/modelID"
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [dragOver, setDragOver] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  // Attach the event stream first, then hydrate (dedupe by message id).
  useEffect(() => {
    const detach = attach(runnerId, instanceId, sessionId);
    void hydrate(runnerId, instanceId, sessionId);
    return detach;
  }, [runnerId, instanceId, sessionId, attach, hydrate]);

  const agentsQ = useQuery({
    queryKey: ["control-agents", runnerId, instanceId],
    queryFn: () => controlAgents(runnerId, instanceId),
    staleTime: 60_000,
    retry: false,
  });
  const providersQ = useQuery({
    queryKey: ["control-providers", runnerId, instanceId],
    queryFn: () => controlProviders(runnerId, instanceId),
    staleTime: 60_000,
    retry: false,
  });

  const modelOptions = useMemo(() => flattenProviders(providersQ.data), [providersQ.data]);

  const messages = useMemo(() => {
    if (!chat) return [];
    return chat.order
      .map((id) => chat.messages[id])
      .filter((m): m is OcMessage => !!m && m.info.sessionID === sessionId);
  }, [chat, sessionId]);

  const sessionPermissions = useMemo(
    () => (chat?.permissions ?? []).filter((p) => !p.sessionID || p.sessionID === sessionId),
    [chat?.permissions, sessionId],
  );

  // Stick to bottom on new content.
  useEffect(() => {
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messages, sessionPermissions.length, chat?.busy]);

  async function send() {
    const t = text.trim();
    const files = attachments;
    if (!t && files.length === 0) return;
    setText("");
    setAttachments([]);
    optimistic(key, sessionId, t || `[${files.length} image${files.length > 1 ? "s" : ""}]`);
    try {
      const body: Parameters<typeof controlPrompt>[3] = { text: t };
      if (agent) body.agent = agent;
      if (model) {
        const [providerID, ...rest] = model.split("/");
        body.model = { providerID, modelID: rest.join("/") };
      }
      if (files.length > 0) {
        body.files = files.map((f) => ({
          mime: f.mime,
          url: f.dataUrl,
          filename: f.filename,
        }));
      }
      await controlPrompt(runnerId, instanceId, sessionId, body);
    } catch (e) {
      setBusy(key, false);
      toast(e instanceof Error ? e.message : "Prompt failed", "error");
    }
  }

  async function addFiles(list: Attachment[]) {
    if (list.length > 0) setAttachments((a) => [...a, ...list]);
  }

  async function onPaste(e: React.ClipboardEvent) {
    const imgs = await attachmentsFromDataTransfer(e.clipboardData);
    if (imgs.length > 0) {
      e.preventDefault();
      void addFiles(imgs);
    }
  }

  async function onDrop(e: React.DragEvent) {
    e.preventDefault();
    setDragOver(false);
    void addFiles(await attachmentsFromDataTransfer(e.dataTransfer));
  }

  async function onPickFiles(e: React.ChangeEvent<HTMLInputElement>) {
    const files = e.target.files ? Array.from(e.target.files) : [];
    const imgs: Attachment[] = [];
    for (const f of files) if (f.type.startsWith("image/")) imgs.push(await fileToAttachment(f));
    void addFiles(imgs);
    e.target.value = "";
  }

  function removeAttachment(id: string) {
    setAttachments((a) => a.filter((x) => x.id !== id));
  }

  async function respond(perm: OcPermission, response: "once" | "always" | "reject") {
    removePermission(key, perm.id);
    try {
      await controlRespondPermission(
        runnerId,
        instanceId,
        (perm.sessionID as string) || sessionId,
        perm.id,
        response,
      );
    } catch (e) {
      toast(e instanceof Error ? e.message : "Permission response failed", "error");
    }
  }

  async function abort() {
    try {
      await controlAbort(runnerId, instanceId, sessionId);
      setBusy(key, false);
      toast("Aborted", "info");
    } catch (e) {
      toast(e instanceof Error ? e.message : "Abort failed", "error");
    }
  }

  return (
    <div className="ctl-chat">
      <div className="ctl-chat-head">
        {sessionLabel && sessionLabel !== sessionId ? (
          <>
            <span className="truncate" style={{ fontWeight: 600 }}>{sessionLabel}</span>
            <span className="mono faint truncate" style={{ fontSize: 11 }}>{sessionId}</span>
          </>
        ) : (
          <span className="mono faint truncate">{sessionId}</span>
        )}
        <span style={{ flex: 1 }} />
        {chat?.connected ? (
          <span style={{ color: "var(--green)", fontSize: 12 }}>● live</span>
        ) : (
          <span style={{ color: "var(--yellow)", fontSize: 12 }}>● connecting…</span>
        )}
        {chat?.busy ? (
          <>
            <span style={{ color: "var(--yellow)", fontSize: 12 }}>working</span>
            <Spinner />
            <button
              className="btn sm"
              style={{ background: "var(--red)", color: "#fff", borderColor: "var(--red)" }}
              onClick={() => void abort()}
              title="Stop the current generation"
            >
              ◼ stop
            </button>
          </>
        ) : (
          <button
            className="btn sm ghost"
            onClick={() => void abort()}
            title="Interrupt the session (no-op if already idle)"
          >
            ◼ stop
          </button>
        )}
      </div>

      <div className="ctl-chat-scroll" ref={scrollRef}>
        {!chat?.hydrated && <div className="faint" style={{ padding: 12 }}>Loading messages…</div>}
        {messages.map((m) => (
          <MessageRow key={m.info.id} message={m} />
        ))}
        {messages.length === 0 && chat?.hydrated && (
          <div className="faint" style={{ padding: 12 }}>
            No messages yet — send a prompt below.
          </div>
        )}
      </div>

      {sessionPermissions.map((perm) => (
        <div key={perm.id} className="ctl-perm">
          <div className="ctl-perm-title">
            ⚠ Permission requested{perm.type ? `: ${perm.type}` : ""}
          </div>
          <div className="ctl-perm-body mono">
            {perm.title || perm.pattern || perm.id}
          </div>
          <div className="row" style={{ gap: 6, marginTop: 6 }}>
            <button className="btn sm" onClick={() => void respond(perm, "once")}>
              Allow
            </button>
            <button className="btn sm ghost" onClick={() => void respond(perm, "always")}>
              Allow always
            </button>
            <button
              className="btn sm ghost"
              style={{ color: "var(--red)" }}
              onClick={() => void respond(perm, "reject")}
            >
              Deny
            </button>
          </div>
        </div>
      ))}

      <div
        className={`ctl-composer ${dragOver ? "dragover" : ""}`}
        onDragOver={(e) => {
          if (e.dataTransfer.types.includes("Files")) {
            e.preventDefault();
            setDragOver(true);
          }
        }}
        onDragLeave={() => setDragOver(false)}
        onDrop={(e) => void onDrop(e)}
      >
        {attachments.length > 0 && (
          <div className="ctl-attach-row">
            {attachments.map((a) => (
              <div key={a.id} className="ctl-attach" title={a.filename}>
                <img src={a.dataUrl} alt={a.filename} />
                <button
                  className="ctl-attach-x"
                  onClick={() => removeAttachment(a.id)}
                  title="Remove"
                >
                  ✕
                </button>
              </div>
            ))}
          </div>
        )}
        <textarea
          value={text}
          placeholder={
            dragOver
              ? "Drop image to attach…"
              : "Send a prompt… (Enter to send, Shift+Enter for newline, paste an image to attach)"
          }
          rows={2}
          onChange={(e) => setText(e.target.value)}
          onPaste={(e) => void onPaste(e)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              void send();
            }
          }}
        />
        <div className="row wrap" style={{ gap: 6 }}>
          <input
            ref={fileInputRef}
            type="file"
            accept="image/*"
            multiple
            style={{ display: "none" }}
            onChange={(e) => void onPickFiles(e)}
          />
          <button
            className="btn sm ghost"
            onClick={() => fileInputRef.current?.click()}
            title="Attach image(s)"
          >
            📎 image
          </button>
          <select value={agent} onChange={(e) => setAgent(e.target.value)} title="Agent">
            <option value="">agent: default</option>
            {(agentsQ.data ?? []).map((a) => (
              <option key={a.name} value={a.name}>
                {a.name}
              </option>
            ))}
          </select>
          <select value={model} onChange={(e) => setModel(e.target.value)} title="Model">
            <option value="">model: default</option>
            {modelOptions.map((m) => (
              <option key={m.value} value={m.value}>
                {m.label}
              </option>
            ))}
          </select>
          <span style={{ flex: 1 }} />
          <button
            className="btn"
            disabled={!text.trim() && attachments.length === 0}
            onClick={() => void send()}
          >
            Send ↵
          </button>
        </div>
      </div>
    </div>
  );
}

function flattenProviders(
  data: { providers?: OcProvider[] } | OcProvider[] | undefined,
): { value: string; label: string }[] {
  if (!data) return [];
  const providers = Array.isArray(data) ? data : (data.providers ?? []);
  const out: { value: string; label: string }[] = [];
  for (const p of providers) {
    for (const [modelId, m] of Object.entries(p.models ?? {})) {
      out.push({
        value: `${p.id}/${modelId}`,
        label: `${p.id}/${m?.name || modelId}`,
      });
    }
  }
  return out;
}

export function MessageRow({ message }: { message: OcMessage }) {
  const role = message.info.role;
  const visible = message.parts.filter(
    (p) =>
      p.type === "text" ||
      p.type === "tool" ||
      p.type === "reasoning" ||
      (p.type === "file" && isImagePart(p)),
  );
  if (visible.length === 0) return null;
  return (
    <div className={`ctl-msg ${role === "user" ? "user" : "assistant"}`}>
      <div className="ctl-msg-role faint">{role === "user" ? "you" : "assistant"}</div>
      {visible.map((p) => (
        <PartView key={p.id} part={p} />
      ))}
    </div>
  );
}

function isImagePart(part: OcPart): boolean {
  const mime = (part as { mime?: string }).mime ?? "";
  return part.type === "file" && mime.startsWith("image/");
}

function PartView({ part }: { part: OcPart }) {
  if (part.type === "text") {
    return (
      <div className="ctl-part-text md">
        <ReactMarkdown remarkPlugins={[remarkGfm]}>{part.text ?? ""}</ReactMarkdown>
      </div>
    );
  }
  if (part.type === "reasoning") {
    return <ReasoningPart text={part.text ?? ""} />;
  }
  if (part.type === "tool") {
    return <ToolCard part={part} />;
  }
  if (isImagePart(part)) {
    const url = (part as { url?: string }).url;
    if (url) return <img className="ctl-msg-img" src={url} alt={(part as { filename?: string }).filename ?? "image"} />;
  }
  return null;
}

function ReasoningPart({ text }: { text: string }) {
  const [open, setOpen] = useState(false);
  if (!text) return null;
  return (
    <div className="ctl-tool">
      <button className="ctl-tool-head" onClick={() => setOpen(!open)}>
        <span className="faint">{open ? "▾" : "▸"} thinking</span>
      </button>
      {open && <pre className="ctl-tool-out faint">{text}</pre>}
    </div>
  );
}

function ToolCard({ part }: { part: OcPart }) {
  const [open, setOpen] = useState(false);
  const status = part.state?.status ?? "pending";
  const color =
    status === "completed"
      ? "var(--green)"
      : status === "error"
        ? "var(--red)"
        : "var(--yellow)";
  const summary = part.state?.title || summarizeInput(part.state?.input);
  return (
    <div className="ctl-tool">
      <button className="ctl-tool-head" onClick={() => setOpen(!open)}>
        <span style={{ color }}>●</span>
        <span className="mono">{part.tool ?? "tool"}</span>
        {summary && <span className="faint truncate">{summary}</span>}
        <span style={{ flex: 1 }} />
        <span className="faint">{open ? "▾" : "▸"}</span>
      </button>
      {open && (
        <div>
          {part.state?.input != null && (
            <pre className="ctl-tool-out">{pretty(part.state.input)}</pre>
          )}
          {part.state?.output && <pre className="ctl-tool-out">{part.state.output}</pre>}
          {part.state?.error && (
            <pre className="ctl-tool-out" style={{ color: "var(--red)" }}>
              {part.state.error}
            </pre>
          )}
        </div>
      )}
    </div>
  );
}

function summarizeInput(input: unknown): string {
  if (input == null) return "";
  if (typeof input === "string") return input.slice(0, 80);
  if (typeof input === "object") {
    const o = input as Record<string, unknown>;
    const cand = o.command ?? o.filePath ?? o.path ?? o.pattern ?? o.url ?? o.description;
    if (typeof cand === "string") return cand.slice(0, 80);
  }
  return "";
}

function pretty(v: unknown): string {
  if (typeof v === "string") return v;
  try {
    return JSON.stringify(v, null, 2);
  } catch {
    return String(v);
  }
}
