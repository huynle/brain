import { create } from "zustand";
import type {
  AssistantChatResponse,
  AssistantToolCall,
  AssistantToolResult,
} from "../lib/api";

// AssistantToolRecord pairs a tool_call event with its eventual tool_result so
// the PWA can render one row per invocation without having to reconcile
// separate arrays. Populated by AssistantPanel.send() as events stream in.
export interface AssistantToolRecord {
  call: AssistantToolCall;
  result?: AssistantToolResult;
}

export interface AssistantMessage {
  role: "user" | "assistant";
  text: string;
  result?: AssistantChatResponse;
  // Tool calls recorded during this assistant turn, in stream order.
  toolCalls?: AssistantToolRecord[];
  // Live status shown while the message is streaming. Cleared once real
  // text begins arriving (or the turn ends). Examples: "thinking\u2026",
  // "calling list_automations\u2026", "waiting on confirmation for
  // delete_entry".
  runtimeStatus?: string;
  // True when the user aborted this turn mid-stream. Preserves any partial
  // text; the UI renders a "(cancelled)" badge and stops the streaming
  // status.
  cancelled?: boolean;
  // Wall-clock when the message was created, used to age out old entries when
  // we rehydrate from localStorage.
  ts: number;
}

// Cap on how many messages we keep in localStorage. The full conversation is
// retained in memory during a session; only the most recent N are persisted to
// keep storage bounded. 50 is plenty for context across a working day without
// risking the 5MB localStorage budget.
const PERSIST_MAX = 50;
const STORAGE_KEY = "brain.assistant.history";

function loadMessages(): AssistantMessage[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed)) return [];
    // Defensive: drop entries that don't match the expected shape.
    return parsed
      .filter((m): m is AssistantMessage => {
        if (!m || typeof m !== "object") return false;
        const o = m as Record<string, unknown>;
        return (
          (o.role === "user" || o.role === "assistant") &&
          typeof o.text === "string" &&
          typeof o.ts === "number"
        );
      })
      .slice(-PERSIST_MAX);
  } catch {
    return [];
  }
}

function persist(messages: AssistantMessage[]): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(messages.slice(-PERSIST_MAX)));
  } catch {
    /* ignore quota / private mode */
  }
}

interface AssistantState {
  messages: AssistantMessage[];
  busy: boolean;
  append: (msg: Omit<AssistantMessage, "ts">) => void;
  updateLastAssistant: (text: string, result?: AssistantChatResponse) => void;
  recordToolCall: (call: AssistantToolCall) => void;
  recordToolResult: (result: AssistantToolResult) => void;
  // Live status for the streaming assistant message. Pass undefined to clear.
  setLastAssistantStatus: (status: string | undefined) => void;
  // Mark the streaming assistant message as cancelled by the user. Preserves
  // whatever partial text has arrived so far.
  markLastAssistantCancelled: () => void;
  clear: () => void;
  setBusy: (busy: boolean) => void;
}

// Single source of truth for the assistant conversation, shared between the
// persistent sidebar and the overlay drawer/sheet so opening either mode picks
// up where the other left off. Persists the most recent messages to
// localStorage; cleared via the header's "clear conversation" button.
export const useAssistant = create<AssistantState>((set, get) => ({
  messages: loadMessages(),
  busy: false,
  append: (msg) => {
    const next = [...get().messages, { ...msg, ts: Date.now() }];
    persist(next);
    set({ messages: next });
  },
  updateLastAssistant: (text, result) => {
    const messages = get().messages;
    const last = messages[messages.length - 1];
    const next: AssistantMessage[] = last?.role === "assistant"
      ? [...messages.slice(0, -1), { ...last, text, result, runtimeStatus: undefined }]
      : [...messages, { role: "assistant", text, result, ts: Date.now() }];
    persist(next);
    set({ messages: next });
  },
  recordToolCall: (call) => {
    const messages = get().messages;
    const last = messages[messages.length - 1];
    if (!last || last.role !== "assistant") return;
    const toolCalls = [...(last.toolCalls || []), { call }];
    const next = [...messages.slice(0, -1), { ...last, toolCalls }];
    persist(next);
    set({ messages: next });
  },
  recordToolResult: (result) => {
    const messages = get().messages;
    const last = messages[messages.length - 1];
    if (!last || last.role !== "assistant" || !last.toolCalls) return;
    const toolCalls = last.toolCalls.map((rec) =>
      rec.call.id === result.id ? { ...rec, result } : rec,
    );
    const next = [...messages.slice(0, -1), { ...last, toolCalls }];
    persist(next);
    set({ messages: next });
  },
  setLastAssistantStatus: (status) => {
    const messages = get().messages;
    const last = messages[messages.length - 1];
    if (!last || last.role !== "assistant") return;
    // Once real text has arrived we never fall back to a status line, so
    // setting a non-empty status while text is present is a no-op.
    if (last.text && status) return;
    const next = [...messages.slice(0, -1), { ...last, runtimeStatus: status }];
    // Don't persist runtime status to localStorage \u2014 it's transient UI state.
    set({ messages: next });
  },
  markLastAssistantCancelled: () => {
    const messages = get().messages;
    const last = messages[messages.length - 1];
    if (!last || last.role !== "assistant") return;
    const next = [
      ...messages.slice(0, -1),
      { ...last, cancelled: true, runtimeStatus: undefined },
    ];
    persist(next);
    set({ messages: next });
  },
  clear: () => {
    persist([]);
    set({ messages: [] });
  },
  setBusy: (busy) => set({ busy }),
}));
