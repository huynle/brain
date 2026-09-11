/**
 * Assistant chat store — unit tests.
 *
 * Runs under `node --test` (see web/package.json) — no DOM, no React.
 * Exercises the turn lifecycle (begin → patch → finish), the clear
 * action, and the caps. Persistence middleware itself is zustand's job;
 * we only check the store constructs in a Node environment without
 * localStorage and that the storage key is versioned.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import { useAssistantChat, ASSISTANT_CHAT_STORAGE_KEY } from "./assistantChat";

function resetStore() {
  useAssistantChat.setState({ turns: [], history: [], busy: false, sessions: [], sessionId: "initial" });
}

test("assistantChat: persisted storage key is versioned", () => {
  assert.equal(ASSISTANT_CHAT_STORAGE_KEY, "panes-v2:assistant-chat:v1");
});

test("assistantChat: beginTurn pushes user turn + streaming placeholder and sets busy", () => {
  resetStore();
  useAssistantChat.getState().beginTurn("hello");
  const s = useAssistantChat.getState();
  assert.equal(s.busy, true);
  assert.equal(s.turns.length, 2);
  assert.equal(s.turns[0].role, "user");
  assert.equal(s.turns[0].content, "hello");
  assert.equal(s.turns[1].role, "assistant");
  assert.equal(s.turns[1].streaming, true);
});

test("assistantChat: patchAssistant updates only a trailing assistant turn", () => {
  resetStore();
  useAssistantChat.getState().beginTurn("q");
  useAssistantChat.getState().patchAssistant({ content: "partial" });
  assert.equal(useAssistantChat.getState().turns[1].content, "partial");

  // With a trailing user turn, the patch is a no-op.
  useAssistantChat.setState({
    turns: [{ role: "user", content: "solo", tools: [] }],
  });
  useAssistantChat.getState().patchAssistant({ content: "x" });
  assert.equal(useAssistantChat.getState().turns[0].content, "solo");
});

test("assistantChat: finishTurn clears busy/streaming and appends history", () => {
  resetStore();
  useAssistantChat.getState().beginTurn("q");
  useAssistantChat.getState().patchAssistant({ content: "answer" });
  useAssistantChat.getState().finishTurn([
    { role: "user", content: "q" },
    { role: "assistant", content: "answer" },
  ]);
  const s = useAssistantChat.getState();
  assert.equal(s.busy, false);
  assert.equal(s.turns[1].streaming, false);
  assert.equal(s.history.length, 2);
  assert.equal(s.history[1].content, "answer");
});

test("assistantChat: clear wipes turns, history, and busy", () => {
  resetStore();
  useAssistantChat.getState().beginTurn("q");
  useAssistantChat.getState().finishTurn([{ role: "user", content: "q" }]);
  useAssistantChat.getState().clear();
  const s = useAssistantChat.getState();
  assert.deepEqual(s.turns, []);
  assert.deepEqual(s.history, []);
  assert.equal(s.busy, false);
});

test("assistantChat: turns and history are capped", () => {
  resetStore();
  for (let i = 0; i < 120; i++) {
    useAssistantChat.getState().beginTurn(`m${i}`);
    useAssistantChat.getState().finishTurn([
      { role: "user", content: `m${i}` },
      { role: "assistant", content: `r${i}` },
    ]);
  }
  const s = useAssistantChat.getState();
  assert.ok(s.turns.length <= 100, `turns ${s.turns.length} > 100`);
  assert.ok(s.history.length <= 200, `history ${s.history.length} > 200`);
  // Newest entries survive the trim.
  assert.equal(s.history[s.history.length - 1].content, "r119");
});


test("sessions preserve separate histories and settle interrupted turns", () => {
  resetStore();
  const chat = () => useAssistantChat.getState();
  chat().beginTurn("First topic");
  chat().patchAssistant({content: "First answer"});
  chat().finishTurn([{role: "user", content: "First topic"}, {role: "assistant", content: "First answer"}]);
  const first = chat().sessionId;
  chat().newSession();
  const second = chat().sessionId;
  assert.notEqual(first, second);
  assert.deepEqual(chat().history, []);
  chat().beginTurn("Second topic");
  chat().patchAssistant({content: "Partial", tools: [{id: "t", name: "recall", args: "{}", tier: "read", status: "running"}]});
  chat().switchSession(first);
  assert.equal(chat().turns[0].content, "First topic");
  assert.equal(chat().history.length, 2);
  chat().switchSession(second);
  assert.equal(chat().turns[0].content, "Second topic");
  assert.equal(chat().busy, false);
  assert.equal(chat().turns[1].streaming, undefined);
  assert.equal(chat().turns[1].tools[0].status, "interrupted");
  assert.deepEqual(chat().history, []);
});

test("legacy single conversation migrates without losing history", () => {
  const merge = useAssistantChat.persist.getOptions().merge!;
  const restored = merge({ turns: [{role: "user", content: "Existing conversation", tools: []}], history: [{role: "user", content: "Existing conversation"}] }, useAssistantChat.getState());
  assert.equal(restored.sessionId, "initial");
  assert.deepEqual(restored.sessions, []);
  assert.equal(restored.turns[0].content, "Existing conversation");
  assert.equal(restored.history[0].content, "Existing conversation");
});
