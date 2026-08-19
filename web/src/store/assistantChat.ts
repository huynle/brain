/**
 * Assistant chat store — the AssistantPanel's conversation state.
 *
 * Lives outside the component so the thread survives closing the panel,
 * switching views, and (via zustand persist) full page reloads. Two
 * parallel shapes are kept:
 *
 *   - `turns`    — the rendered transcript (user/assistant turns + tool chips)
 *   - `history`  — the compact AssistantHistoryMessage replay sent back to
 *                  the stateless server on each request (see replayHistory
 *                  in internal/api/assistant_loop.go)
 *
 * `busy` and any in-flight streaming flags are transient: they are not
 * persisted, and rehydrate normalizes a transcript that was cut off
 * mid-stream by a reload.
 */
import { create } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";
import type { AssistantHistoryMessage } from "../lib/api";

/** Versioned localStorage key. Bump the suffix on breaking schema changes. */
export const ASSISTANT_CHAT_STORAGE_KEY = "panes-v2:assistant-chat:v1";

/** Caps so a long-lived conversation doesn't grow the cache unbounded. */
const MAX_TURNS = 100;
const MAX_HISTORY = 200;

export interface AssistantToolChip {
  id: string;
  name: string;
  args: string;
  tier: string;
  status: string; // "running" until a tool_result lands
}

export interface AssistantChatTurn {
  role: "user" | "assistant";
  content: string;
  tools: AssistantToolChip[];
  streaming?: boolean;
}

interface AssistantChatState {
  turns: AssistantChatTurn[];
  history: AssistantHistoryMessage[];
  busy: boolean;

  /** Push the user turn + a streaming assistant placeholder; marks busy. */
  beginTurn(userContent: string): void;
  /** Patch the trailing assistant turn (streamed content / tool chips). */
  patchAssistant(patch: Partial<AssistantChatTurn>): void;
  /** Settle the trailing assistant turn and append the replay entries. */
  finishTurn(entries: AssistantHistoryMessage[]): void;
  /** Wipe the conversation. */
  clear(): void;
}

/** Detect whether localStorage is usable (jsdom, node --test, SSR bail). */
function safeStorage() {
  if (typeof window === "undefined") return undefined;
  try {
    const probe = "__ac_probe__";
    window.localStorage.setItem(probe, "1");
    window.localStorage.removeItem(probe);
    return window.localStorage;
  } catch {
    return undefined;
  }
}

/** Fallback storage used when window.localStorage is unavailable. */
const noopStorage = {
  getItem: () => null,
  setItem: () => undefined,
  removeItem: () => undefined,
};

/**
 * Best-effort validation for a persisted transcript. Unrecognized shapes
 * are dropped (start fresh) rather than crashing the panel on a stale
 * cache. Turns cut off mid-stream by a reload are settled: streaming is
 * cleared and never-answered tool chips are marked "interrupted".
 */
function coerceTurns(raw: unknown): AssistantChatTurn[] {
  if (!Array.isArray(raw)) return [];
  const out: AssistantChatTurn[] = [];
  for (const t of raw) {
    if (!t || typeof t !== "object") return [];
    const turn = t as Partial<AssistantChatTurn>;
    if (turn.role !== "user" && turn.role !== "assistant") return [];
    if (typeof turn.content !== "string") return [];
    const tools: AssistantToolChip[] = [];
    if (turn.tools != null) {
      if (!Array.isArray(turn.tools)) return [];
      for (const c of turn.tools) {
        if (!c || typeof c.id !== "string" || typeof c.name !== "string") return [];
        tools.push({
          id: c.id,
          name: c.name,
          args: typeof c.args === "string" ? c.args : "{}",
          tier: typeof c.tier === "string" ? c.tier : "",
          status: c.status === "running" ? "interrupted" : String(c.status ?? ""),
        });
      }
    }
    out.push({ role: turn.role, content: turn.content, tools });
  }
  return out;
}

function coerceHistory(raw: unknown): AssistantHistoryMessage[] {
  if (!Array.isArray(raw)) return [];
  for (const h of raw) {
    if (!h || typeof h !== "object") return [];
    const role = (h as { role?: unknown }).role;
    if (role !== "user" && role !== "assistant" && role !== "tool") return [];
  }
  return raw as AssistantHistoryMessage[];
}

export const useAssistantChat = create<AssistantChatState>()(
  persist(
    (set) => ({
      turns: [],
      history: [],
      busy: false,

      beginTurn: (userContent) =>
        set((s) => ({
          busy: true,
          turns: [
            ...s.turns,
            { role: "user" as const, content: userContent, tools: [] },
            {
              role: "assistant" as const,
              content: "",
              tools: [],
              streaming: true,
            },
          ].slice(-MAX_TURNS),
        })),

      patchAssistant: (patch) =>
        set((s) => {
          const turns = s.turns.slice();
          const last = turns[turns.length - 1];
          if (last?.role !== "assistant") return s;
          turns[turns.length - 1] = { ...last, ...patch };
          return { turns };
        }),

      finishTurn: (entries) =>
        set((s) => {
          const turns = s.turns.slice();
          const last = turns[turns.length - 1];
          if (last?.role === "assistant") {
            turns[turns.length - 1] = { ...last, streaming: false };
          }
          return {
            busy: false,
            turns,
            history: [...s.history, ...entries].slice(-MAX_HISTORY),
          };
        }),

      clear: () => set({ turns: [], history: [], busy: false }),
    }),
    {
      name: ASSISTANT_CHAT_STORAGE_KEY,
      partialize: (s) => ({ turns: s.turns, history: s.history }),
      storage: createJSONStorage(() => safeStorage() ?? noopStorage),
      version: 1,
      merge: (persistedState, currentState) => {
        const p = (persistedState ?? {}) as Partial<AssistantChatState>;
        return {
          ...currentState,
          turns: coerceTurns(p.turns),
          history: coerceHistory(p.history),
          busy: false,
        };
      },
    },
  ),
);
