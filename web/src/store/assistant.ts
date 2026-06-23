import { create } from "zustand";
import type { AssistantChatResponse } from "../lib/api";

export interface AssistantMessage {
  role: "user" | "assistant";
  text: string;
  result?: AssistantChatResponse;
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
      ? [...messages.slice(0, -1), { ...last, text, result }]
      : [...messages, { role: "assistant", text, result, ts: Date.now() }];
    persist(next);
    set({ messages: next });
  },
  clear: () => {
    persist([]);
    set({ messages: [] });
  },
  setBusy: (busy) => set({ busy }),
}));
