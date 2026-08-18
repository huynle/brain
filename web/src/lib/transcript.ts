// Session-transcript state: an ordered OcMessage list plus a pure reducer
// that folds OpenCode /event deltas into it.
//
// The control events stream is per-INSTANCE and delivers every OpenCode
// event under one SSE event name, so consumers must (a) switch on the
// inner `type` and (b) drop events for other sessions hosted by the same
// instance. Both concerns live here.
//
// This module is pure so it stays unit-testable without pulling in
// zustand or react.

import type { OcEvent, OcMessage, OcPart } from "./types";

/** Event types that mutate the transcript (vs session/permission state). */
export function isTranscriptEvent(type: string): boolean {
  return (
    type === "message.updated" ||
    type === "message.removed" ||
    type === "message.part.updated" ||
    type === "message.part.removed"
  );
}

/** The session an event belongs to, wherever OpenCode put it. */
export function eventSessionID(evt: OcEvent): string | undefined {
  const p = evt.properties;
  if (!p) return undefined;
  if (typeof p.sessionID === "string") return p.sessionID;
  const info = p.info as Record<string, unknown> | undefined;
  if (info && typeof info.sessionID === "string") return info.sessionID;
  if (p.part && typeof p.part.sessionID === "string") return p.part.sessionID;
  return undefined;
}

function upsertMessage(
  messages: readonly OcMessage[],
  info: OcMessage["info"],
): OcMessage[] {
  const idx = messages.findIndex((m) => m.info.id === info.id);
  if (idx === -1) {
    return [...messages, { info, parts: [] }];
  }
  const next = messages.slice();
  next[idx] = { info, parts: messages[idx].parts };
  return next;
}

function upsertPart(
  messages: readonly OcMessage[],
  part: OcPart,
): OcMessage[] {
  const messageID = part.messageID;
  if (!messageID) return messages as OcMessage[];
  const idx = messages.findIndex((m) => m.info.id === messageID);
  if (idx === -1) {
    // Part arrived before its message.updated — keep it on a stub so no
    // content is dropped; the info fills in when the message event lands.
    const stub: OcMessage = {
      info: { id: messageID, sessionID: part.sessionID, role: "" },
      parts: [part],
    };
    return [...messages, stub];
  }
  const msg = messages[idx];
  const pIdx = msg.parts.findIndex((p) => p.id === part.id);
  const parts =
    pIdx === -1 ? [...msg.parts, part] : msg.parts.map((p, i) => (i === pIdx ? part : p));
  const next = messages.slice();
  next[idx] = { info: msg.info, parts };
  return next;
}

/**
 * Fold one OpenCode event into the transcript. Returns the SAME array
 * reference when the event is irrelevant (wrong session, non-transcript
 * type, malformed) so React/zustand consumers can bail on ref-equality.
 */
export function applyEvent(
  messages: OcMessage[],
  evt: OcEvent,
  sessionID: string,
): OcMessage[] {
  if (!isTranscriptEvent(evt.type)) return messages;
  const evtSession = eventSessionID(evt);
  if (evtSession !== undefined && evtSession !== sessionID) return messages;
  const p = evt.properties;
  if (!p) return messages;

  switch (evt.type) {
    case "message.updated": {
      const info = p.info as OcMessage["info"] | undefined;
      if (!info || typeof info.id !== "string") return messages;
      return upsertMessage(messages, info);
    }
    case "message.removed": {
      const messageID = p.messageID;
      if (typeof messageID !== "string") return messages;
      const next = messages.filter((m) => m.info.id !== messageID);
      return next.length === messages.length ? messages : next;
    }
    case "message.part.updated": {
      const part = p.part;
      if (!part || typeof part.id !== "string") return messages;
      return upsertPart(messages, part);
    }
    case "message.part.removed": {
      const messageID = p.messageID;
      const partID = p.partID;
      if (typeof messageID !== "string" || typeof partID !== "string") {
        return messages;
      }
      const idx = messages.findIndex((m) => m.info.id === messageID);
      if (idx === -1) return messages;
      const msg = messages[idx];
      const parts = msg.parts.filter((part) => part.id !== partID);
      if (parts.length === msg.parts.length) return messages;
      const next = messages.slice();
      next[idx] = { info: msg.info, parts };
      return next;
    }
    default:
      return messages;
  }
}

/**
 * Reconcile a freshly-fetched backlog with the current (delta-fed)
 * state. The backlog is authoritative for every message it contains;
 * messages present only in `current` are kept (they arrived on the
 * stream after the backlog snapshot was taken) and stay in order after
 * the backlog.
 */
export function mergeBacklog(
  backlog: OcMessage[],
  current: OcMessage[],
): OcMessage[] {
  if (current.length === 0) return backlog;
  const seen = new Set(backlog.map((m) => m.info.id));
  const tail = current.filter((m) => !seen.has(m.info.id));
  return tail.length === 0 ? backlog : [...backlog, ...tail];
}

/**
 * A user-role message that was injected by the goal steerer (or a
 * check-in preset) rather than typed by the watching user. Rendered
 * with an "injected" chip so watchers understand user turns they
 * didn't write.
 */
export function isInjectedCheckin(msg: OcMessage): boolean {
  if (msg.info.role !== "user") return false;
  const firstText = msg.parts.find(
    (p) => p.type === "text" && typeof p.text === "string",
  );
  if (!firstText || typeof firstText.text !== "string") return false;
  const head = firstText.text.trimStart();
  return head.startsWith("## Goal check-in") || head.startsWith("## Check-in");
}

/**
 * The composer's check-in preset (plan Decision 3). Mirrors
 * buildGoalSteeringPrompt's shape so a steered agent sees the same
 * format whether the nudge came from a goal or a human — and starts
 * with the "## Check-in" header so isInjectedCheckin labels it.
 */
export function buildCheckinPreset(seed: {
  title?: string;
  request?: string;
}): string {
  const lines = ["## Check-in"];
  if (seed.title) lines.push(`Task: ${seed.title}`);
  if (seed.request) lines.push("", "### Original request", seed.request);
  lines.push(
    "",
    "Self-assess your progress against the task above and correct course NOW. " +
      "Only complete this task if it is truly done; otherwise keep working toward it.",
  );
  return lines.join("\n");
}
