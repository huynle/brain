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
  useAssistantChat.setState({ turns: [], history: [], busy: false });
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
