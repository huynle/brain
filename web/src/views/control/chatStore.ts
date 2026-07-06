// Per-attached-instance chat state: messages merged from hydration +
// live events, session statuses, pending permissions, and the EventSource
// lifecycle (reconnect with backoff).

import { create } from "zustand";
import { controlEventsUrl, controlListMessages } from "../../lib/api";
import type { OcEvent, OcMessage, OcPart, OcPermission } from "../../lib/types";

export interface ChatState {
  runnerId: string;
  instanceId: string;
  connected: boolean;
  busy: boolean; // session-level busy indicator (set on prompt, cleared on idle)
  order: string[]; // message ids in arrival order
  messages: Record<string, OcMessage>;
  permissions: OcPermission[]; // pending, newest last
  hydrated: boolean;
}

interface ChatStore {
  chats: Record<string, ChatState>; // key: `${runnerId}/${instanceId}`
  attach: (runnerId: string, instanceId: string, sessionId: string) => () => void;
  hydrate: (runnerId: string, instanceId: string, sessionId: string) => Promise<void>;
  setBusy: (key: string, busy: boolean) => void;
  removePermission: (key: string, permissionId: string) => void;
  optimisticUserMessage: (key: string, sessionId: string, text: string) => void;
}

export const chatKey = (runnerId: string, instanceId: string) =>
  `${runnerId}/${instanceId}`;

function emptyChat(runnerId: string, instanceId: string): ChatState {
  return {
    runnerId,
    instanceId,
    connected: false,
    busy: false,
    order: [],
    messages: {},
    permissions: [],
    hydrated: false,
  };
}

// Module-level EventSource registry (not in zustand state — not renderable).
const sources: Record<string, { es: EventSource | null; refs: number; retry: number; timer?: number }> = {};

export const useChat = create<ChatStore>((set, get) => ({
  chats: {},

  hydrate: async (runnerId, instanceId, sessionId) => {
    const key = chatKey(runnerId, instanceId);
    try {
      const msgs = await controlListMessages(runnerId, instanceId, sessionId, 80);
      set((s) => {
        const chat = { ...(s.chats[key] ?? emptyChat(runnerId, instanceId)) };
        // Live events may have arrived during hydration — merge, don't replace.
        const messages = { ...chat.messages };
        const order = [...chat.order];
        for (const m of msgs ?? []) {
          if (!m?.info?.id || m.info.sessionID !== sessionId) continue;
          const existing = messages[m.info.id];
          messages[m.info.id] = existing
            ? { info: { ...m.info, ...existing.info }, parts: mergeParts(m.parts, existing.parts) }
            : m;
          if (!order.includes(m.info.id)) order.push(m.info.id);
        }
        order.sort((a, b) => msgTime(messages[a]) - msgTime(messages[b]));
        chat.messages = messages;
        chat.order = order;
        chat.hydrated = true;
        return { chats: { ...s.chats, [key]: chat } };
      });
    } catch {
      // Hydration failure is non-fatal; live events still render.
      set((s) => {
        const chat = { ...(s.chats[key] ?? emptyChat(runnerId, instanceId)), hydrated: true };
        return { chats: { ...s.chats, [key]: chat } };
      });
    }
  },

  attach: (runnerId, instanceId, sessionId) => {
    const key = chatKey(runnerId, instanceId);
    set((s) => ({
      chats: { ...s.chats, [key]: s.chats[key] ?? emptyChat(runnerId, instanceId) },
    }));

    const reg = (sources[key] ??= { es: null, refs: 0, retry: 0 });
    reg.refs++;
    if (!reg.es) openSource(key, runnerId, instanceId, sessionId, set, get);

    return () => {
      reg.refs--;
      if (reg.refs <= 0) {
        if (reg.timer) window.clearTimeout(reg.timer);
        reg.es?.close();
        reg.es = null;
        set((s) => {
          const chat = s.chats[key];
          if (!chat) return s;
          return { chats: { ...s.chats, [key]: { ...chat, connected: false } } };
        });
      }
    };
  },

  setBusy: (key, busy) =>
    set((s) => {
      const chat = s.chats[key];
      if (!chat) return s;
      return { chats: { ...s.chats, [key]: { ...chat, busy } } };
    }),

  removePermission: (key, permissionId) =>
    set((s) => {
      const chat = s.chats[key];
      if (!chat) return s;
      return {
        chats: {
          ...s.chats,
          [key]: { ...chat, permissions: chat.permissions.filter((p) => p.id !== permissionId) },
        },
      };
    }),

  optimisticUserMessage: (key, sessionId, text) =>
    set((s) => {
      const chat = s.chats[key];
      if (!chat) return s;
      const id = `local_${Date.now()}`;
      const msg: OcMessage = {
        info: { id, sessionID: sessionId, role: "user", time: { created: Date.now() } },
        parts: [{ id: `${id}_p0`, type: "text", text }],
      };
      return {
        chats: {
          ...s.chats,
          [key]: {
            ...chat,
            busy: true,
            messages: { ...chat.messages, [id]: msg },
            order: [...chat.order, id],
          },
        },
      };
    }),
}));

function msgTime(m: OcMessage | undefined): number {
  return m?.info?.time?.created ?? 0;
}

function mergeParts(base: OcPart[] = [], overlay: OcPart[] = []): OcPart[] {
  const byId = new Map<string, OcPart>();
  for (const p of base) if (p?.id) byId.set(p.id, p);
  for (const p of overlay) if (p?.id) byId.set(p.id, p);
  return [...byId.values()];
}

function openSource(
  key: string,
  runnerId: string,
  instanceId: string,
  sessionId: string,
  set: (fn: (s: { chats: Record<string, ChatState> }) => Partial<{ chats: Record<string, ChatState> }>) => void,
  get: () => ChatStore,
) {
  const reg = sources[key];
  if (!reg) return;

  const es = new EventSource(controlEventsUrl(runnerId, instanceId));
  reg.es = es;

  const update = (fn: (chat: ChatState) => ChatState) =>
    set((s) => {
      const chat = s.chats[key] ?? emptyChat(runnerId, instanceId);
      return { chats: { ...s.chats, [key]: fn(chat) } };
    });

  es.addEventListener("connected", () => {
    reg.retry = 0;
    update((c) => ({ ...c, connected: true }));
  });

  es.addEventListener("instance_event", (e) => {
    let evt: OcEvent;
    try {
      evt = JSON.parse((e as MessageEvent).data) as OcEvent;
    } catch {
      return;
    }
    applyEvent(key, sessionId, evt, update);
  });

  es.onerror = () => {
    update((c) => ({ ...c, connected: false }));
    es.close();
    if (reg.es !== es) return;
    reg.es = null;
    if (reg.refs <= 0) return;
    const delay = Math.min(15_000, 1000 * 2 ** reg.retry++);
    reg.timer = window.setTimeout(() => {
      if (reg.refs > 0 && !reg.es) {
        openSource(key, runnerId, instanceId, sessionId, set, get);
        // Re-hydrate to fill any gap from the disconnect.
        void get().hydrate(runnerId, instanceId, sessionId);
      }
    }, delay);
  };
}

// applyEvent merges a single OpenCode event into chat state. Events for
// other sessions on the same instance are filtered out (except permissions,
// which carry their own sessionID and are kept per-session too).
function applyEvent(
  _key: string,
  sessionId: string,
  evt: OcEvent,
  update: (fn: (chat: ChatState) => ChatState) => void,
) {
  const props = evt.properties ?? {};
  const evtSession =
    (props.sessionID as string) ??
    ((props.info as Record<string, unknown>)?.sessionID as string) ??
    ((props.part as OcPart)?.sessionID as string);

  switch (evt.type) {
    case "message.updated": {
      const info = props.info as OcMessage["info"] | undefined;
      if (!info?.id || (evtSession && evtSession !== sessionId)) return;
      update((c) => {
        const existing = c.messages[info.id];
        const merged: OcMessage = existing
          ? { info: { ...existing.info, ...info }, parts: existing.parts }
          : { info, parts: [] };
        let order = c.order.includes(info.id) ? c.order : [...c.order, info.id];
        let messages = { ...c.messages, [info.id]: merged };
        // The server echoed the real user message — drop the optimistic
        // local placeholder so the prompt doesn't render twice.
        if (info.role === "user" && !info.id.startsWith("local_")) {
          const locals = order.filter((id) => id.startsWith("local_"));
          if (locals.length > 0) {
            order = order.filter((id) => !id.startsWith("local_"));
            for (const id of locals) delete messages[id];
          }
        }
        return { ...c, messages, order, busy: true };
      });
      return;
    }

    case "message.part.updated": {
      const part = props.part as OcPart | undefined;
      if (!part?.id || !part.messageID || (evtSession && evtSession !== sessionId)) return;
      update((c) => {
        const msg = c.messages[part.messageID!] ?? {
          info: { id: part.messageID!, sessionID: sessionId, role: "assistant" },
          parts: [],
        };
        const idx = msg.parts.findIndex((p) => p.id === part.id);
        const parts = [...msg.parts];
        if (idx >= 0) parts[idx] = { ...parts[idx], ...part };
        else parts.push(part);
        const order = c.order.includes(part.messageID!) ? c.order : [...c.order, part.messageID!];
        return {
          ...c,
          messages: { ...c.messages, [part.messageID!]: { ...msg, parts } },
          order,
        };
      });
      return;
    }

    case "message.part.delta": {
      // Streaming text delta: append to the addressed part.
      const messageID = props.messageID as string | undefined;
      const partID = (props.partID as string) ?? (props.id as string);
      const delta = (props.delta as string) ?? (props.text as string) ?? "";
      if (!messageID || !partID || !delta || (evtSession && evtSession !== sessionId)) return;
      update((c) => {
        const msg = c.messages[messageID];
        if (!msg) return c;
        const idx = msg.parts.findIndex((p) => p.id === partID);
        const parts = [...msg.parts];
        if (idx >= 0) {
          parts[idx] = { ...parts[idx], text: (parts[idx].text ?? "") + delta };
        } else {
          parts.push({ id: partID, messageID, type: "text", text: delta });
        }
        return { ...c, messages: { ...c.messages, [messageID]: { ...msg, parts } } };
      });
      return;
    }

    case "permission.updated":
    case "permission.asked": {
      const perm = (props.info ?? props) as unknown as OcPermission;
      if (!perm?.id) return;
      update((c) => ({
        ...c,
        permissions: [...c.permissions.filter((p) => p.id !== perm.id), perm],
      }));
      return;
    }

    case "permission.replied": {
      const permID =
        (props.permissionID as string) ??
        (props.id as string) ??
        ((props.info as Record<string, unknown>)?.id as string);
      if (!permID) return;
      update((c) => ({
        ...c,
        permissions: c.permissions.filter((p) => p.id !== permID),
      }));
      return;
    }

    case "session.idle": {
      if (evtSession && evtSession !== sessionId) return;
      update((c) => ({ ...c, busy: false }));
      return;
    }

    case "session.error": {
      if (evtSession && evtSession !== sessionId) return;
      update((c) => ({ ...c, busy: false }));
      return;
    }

    default:
      return;
  }
}
