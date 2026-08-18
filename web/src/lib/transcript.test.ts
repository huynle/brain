import test from "node:test";
import assert from "node:assert/strict";
import {
  applyEvent,
  eventSessionID,
  isInjectedCheckin,
  isTranscriptEvent,
  mergeBacklog,
  buildCheckinPreset,
} from "./transcript";
import type { OcMessage } from "./types";

const SID = "ses_1";

function msg(id: string, role = "assistant"): OcMessage {
  return { info: { id, sessionID: SID, role }, parts: [] };
}

test("isTranscriptEvent: message events yes, session/permission no", () => {
  assert.equal(isTranscriptEvent("message.updated"), true);
  assert.equal(isTranscriptEvent("message.part.updated"), true);
  assert.equal(isTranscriptEvent("message.removed"), true);
  assert.equal(isTranscriptEvent("message.part.removed"), true);
  assert.equal(isTranscriptEvent("session.idle"), false);
  assert.equal(isTranscriptEvent("permission.updated"), false);
});

test("eventSessionID: read from properties, info, or part", () => {
  assert.equal(eventSessionID({ type: "x", properties: { sessionID: SID } }), SID);
  assert.equal(
    eventSessionID({ type: "x", properties: { info: { sessionID: SID } } }),
    SID,
  );
  assert.equal(
    eventSessionID({
      type: "x",
      properties: { part: { id: "p", sessionID: SID, type: "text" } },
    }),
    SID,
  );
  assert.equal(eventSessionID({ type: "x" }), undefined);
});

test("applyEvent: message.updated appends new and replaces existing info", () => {
  const m1 = applyEvent(
    [],
    { type: "message.updated", properties: { info: { id: "m1", sessionID: SID, role: "user" } } },
    SID,
  );
  assert.equal(m1.length, 1);
  assert.equal(m1[0].info.role, "user");

  const m2 = applyEvent(
    m1,
    { type: "message.updated", properties: { info: { id: "m1", sessionID: SID, role: "user", agent: "dev" } } },
    SID,
  );
  assert.equal(m2.length, 1);
  assert.equal(m2[0].info.agent, "dev");
});

test("applyEvent: wrong session or irrelevant type returns same reference", () => {
  const state = [msg("m1")];
  const other = applyEvent(
    state,
    { type: "message.updated", properties: { info: { id: "mX", sessionID: "ses_other" } } },
    SID,
  );
  assert.equal(other, state);
  const idle = applyEvent(state, { type: "session.idle", properties: { sessionID: SID } }, SID);
  assert.equal(idle, state);
});

test("applyEvent: part upsert replaces by id and appends otherwise", () => {
  const base = [msg("m1")];
  const p1 = applyEvent(
    base,
    {
      type: "message.part.updated",
      properties: { part: { id: "p1", messageID: "m1", sessionID: SID, type: "text", text: "hel" } },
    },
    SID,
  );
  assert.equal(p1[0].parts.length, 1);
  const p2 = applyEvent(
    p1,
    {
      type: "message.part.updated",
      properties: { part: { id: "p1", messageID: "m1", sessionID: SID, type: "text", text: "hello" } },
    },
    SID,
  );
  assert.equal(p2[0].parts.length, 1);
  assert.equal(p2[0].parts[0].text, "hello");
  const p3 = applyEvent(
    p2,
    {
      type: "message.part.updated",
      properties: { part: { id: "p2", messageID: "m1", sessionID: SID, type: "tool", tool: "bash" } },
    },
    SID,
  );
  assert.equal(p3[0].parts.length, 2);
});

test("applyEvent: part before its message creates a stub that later fills in", () => {
  const p1 = applyEvent(
    [],
    {
      type: "message.part.updated",
      properties: { part: { id: "p1", messageID: "m9", sessionID: SID, type: "text", text: "early" } },
    },
    SID,
  );
  assert.equal(p1.length, 1);
  assert.equal(p1[0].info.id, "m9");
  assert.equal(p1[0].parts[0].text, "early");

  const filled = applyEvent(
    p1,
    { type: "message.updated", properties: { info: { id: "m9", sessionID: SID, role: "assistant" } } },
    SID,
  );
  assert.equal(filled.length, 1);
  assert.equal(filled[0].info.role, "assistant");
  assert.equal(filled[0].parts.length, 1);
});

test("applyEvent: message.removed and part.removed", () => {
  const base = applyEvent(
    [msg("m1"), msg("m2")],
    {
      type: "message.part.updated",
      properties: { part: { id: "p1", messageID: "m1", sessionID: SID, type: "text", text: "x" } },
    },
    SID,
  );
  const removedPart = applyEvent(
    base,
    { type: "message.part.removed", properties: { sessionID: SID, messageID: "m1", partID: "p1" } },
    SID,
  );
  assert.equal(removedPart[0].parts.length, 0);

  const removedMsg = applyEvent(
    removedPart,
    { type: "message.removed", properties: { sessionID: SID, messageID: "m2" } },
    SID,
  );
  assert.equal(removedMsg.length, 1);
  assert.equal(removedMsg[0].info.id, "m1");

  // Removing something already gone keeps the reference.
  const again = applyEvent(
    removedMsg,
    { type: "message.removed", properties: { sessionID: SID, messageID: "m2" } },
    SID,
  );
  assert.equal(again, removedMsg);
});

test("isInjectedCheckin: goal check-in and check-in headers on user turns only", () => {
  const injected: OcMessage = {
    info: { id: "m1", role: "user" },
    parts: [{ id: "p1", type: "text", text: "## Goal check-in\nGoal: ship it" }],
  };
  const preset: OcMessage = {
    info: { id: "m2", role: "user" },
    parts: [{ id: "p1", type: "text", text: "  ## Check-in\nself-assess" }],
  };
  const normal: OcMessage = {
    info: { id: "m3", role: "user" },
    parts: [{ id: "p1", type: "text", text: "please fix the tests" }],
  };
  const assistant: OcMessage = {
    info: { id: "m4", role: "assistant" },
    parts: [{ id: "p1", type: "text", text: "## Goal check-in echo" }],
  };
  assert.equal(isInjectedCheckin(injected), true);
  assert.equal(isInjectedCheckin(preset), true);
  assert.equal(isInjectedCheckin(normal), false);
  assert.equal(isInjectedCheckin(assistant), false);
});

test("mergeBacklog: backlog authoritative, stream-only tail kept in order", () => {
  const backlog = [msg("m1"), msg("m2")];
  const current = [msg("m2"), msg("m3"), msg("m4")];
  const merged = mergeBacklog(backlog, current);
  assert.deepEqual(
    merged.map((m) => m.info.id),
    ["m1", "m2", "m3", "m4"],
  );
  // Empty current returns the backlog reference untouched.
  assert.equal(mergeBacklog(backlog, []), backlog);
  // Fully-covered current collapses to the backlog reference.
  assert.equal(mergeBacklog(backlog, [msg("m1")]), backlog);
});

test("buildCheckinPreset: header matches the injected-chip detector", () => {
  const text = buildCheckinPreset({ title: "Fix auth", request: "make login work" });
  assert.ok(text.startsWith("## Check-in"));
  assert.match(text, /Task: Fix auth/);
  assert.match(text, /### Original request\nmake login work/);
  assert.match(text, /Self-assess/);
  // The invariant that keeps the chip honest: a preset message is
  // recognized as injected by isInjectedCheckin.
  const asMessage: OcMessage = {
    info: { id: "m1", role: "user" },
    parts: [{ id: "p1", type: "text", text }],
  };
  assert.equal(isInjectedCheckin(asMessage), true);
  // Bare preset (no seed) still carries the header + instruction.
  assert.ok(buildCheckinPreset({}).startsWith("## Check-in"));
});
