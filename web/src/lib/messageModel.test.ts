import { strict as assert } from "node:assert";
import { describe, it } from "node:test";

import { messageModel, modelLabel, modelTitle } from "./messageModel";
import type { OcMessageInfo } from "./types";

const info = (extra: Partial<OcMessageInfo>): OcMessageInfo => ({
  id: "m1",
  role: "assistant",
  ...extra,
});

describe("messageModel", () => {
  // Both shapes below are copied from real messages in OpenCode's own
  // storage. Reading either field alone shows a model on some rows and
  // nothing on others, which reads as "this turn had no model".
  it("reads an ASSISTANT message's flat modelID + providerID", () => {
    const m = messageModel(
      info({
        role: "assistant",
        agent: "general",
        modelID: "claude-opus-4-5",
        providerID: "anthropic",
      }),
    );
    assert.deepEqual(m, { id: "claude-opus-4-5", provider: "anthropic" });
  });

  it("reads a USER message's nested model object", () => {
    const m = messageModel(
      info({
        role: "user",
        agent: "tdd-dev",
        model: { providerID: "anthropic", modelID: "claude-opus-4-5" },
      }),
    );
    assert.deepEqual(m, { id: "claude-opus-4-5", provider: "anthropic" });
  });

  it("prefers the flat field when a payload somehow carries both", () => {
    const m = messageModel(
      info({ modelID: "flat", model: { modelID: "nested" } }),
    );
    assert.equal(m?.id, "flat");
  });

  it("splits a plain provider/model string", () => {
    assert.deepEqual(messageModel(info({ model: "anthropic/claude-x" })), {
      id: "claude-x",
      provider: "anthropic",
    });
    assert.deepEqual(messageModel(info({ model: "bare-model" })), {
      id: "bare-model",
      provider: "",
    });
  });

  it("returns null rather than an empty chip when nothing is recorded", () => {
    assert.equal(messageModel(info({})), null);
    assert.equal(messageModel(info({ modelID: "   " })), null);
    assert.equal(messageModel(info({ model: {} })), null);
    assert.equal(messageModel(info({ model: "" })), null);
  });

  it("keeps a missing provider from becoming the string 'undefined'", () => {
    const m = messageModel(info({ modelID: "solo" }));
    assert.deepEqual(m, { id: "solo", provider: "" });
    assert.equal(modelTitle(m!), "solo");
  });
});

describe("model labels", () => {
  it("shows the bare id in the row and the full path in the tooltip", () => {
    const m = { id: "claude-opus-4-5", provider: "anthropic" };
    assert.equal(modelLabel(m), "claude-opus-4-5");
    assert.equal(modelTitle(m), "anthropic/claude-opus-4-5");
  });
});
