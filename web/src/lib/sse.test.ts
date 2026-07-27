/**
 * Unit tests for SSE frame parser.
 *
 * Only tests the pure `parseSSEFrame` helper. The Stream manager
 * itself is integration-level (needs fetch + ReadableStream mocks)
 * and is verified live against brain-api.
 */
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { parseSSEFrame } from "./sse";

describe("parseSSEFrame", () => {
  it("parses event + data", () => {
    const f = parseSSEFrame("event: tasks_snapshot\ndata: {\"tasks\":[]}");
    assert.deepEqual(f, { event: "tasks_snapshot", data: "{\"tasks\":[]}" });
  });

  it("defaults event name to 'message'", () => {
    const f = parseSSEFrame("data: hello");
    assert.deepEqual(f, { event: "message", data: "hello" });
  });

  it("returns null for a comment-only frame", () => {
    const f = parseSSEFrame(": heartbeat");
    assert.equal(f, null);
  });

  it("returns null for an empty block", () => {
    const f = parseSSEFrame("");
    assert.equal(f, null);
  });

  it("concatenates multi-line data with \\n", () => {
    const f = parseSSEFrame("event: x\ndata: line1\ndata: line2\ndata: line3");
    assert.deepEqual(f, { event: "x", data: "line1\nline2\nline3" });
  });

  it("handles field without leading space after colon", () => {
    const f = parseSSEFrame("event:foo\ndata:bar");
    assert.deepEqual(f, { event: "foo", data: "bar" });
  });

  it("handles field with no value", () => {
    // 'data:' with no value produces an empty data string, still valid.
    const f = parseSSEFrame("event: ping\ndata:");
    assert.deepEqual(f, { event: "ping", data: "" });
  });

  it("ignores retry + id fields", () => {
    const f = parseSSEFrame(
      "id: 42\nretry: 3000\nevent: t\ndata: {\"n\":1}",
    );
    assert.deepEqual(f, { event: "t", data: "{\"n\":1}" });
  });

  it("preserves colons inside data values", () => {
    const f = parseSSEFrame(
      "event: log\ndata: {\"url\":\"http://x/y:z\"}",
    );
    assert.deepEqual(f, {
      event: "log",
      data: "{\"url\":\"http://x/y:z\"}",
    });
  });
});
