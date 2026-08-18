import test from "node:test";
import assert from "node:assert/strict";
import { routeFrame } from "./instanceStream";

test("routeFrame: instance_event with valid OpenCode payload", () => {
  const routed = routeFrame({
    event: "instance_event",
    data: JSON.stringify({ type: "message.updated", properties: { sessionID: "s" } }),
  });
  assert.equal(routed.kind, "event");
  assert.equal(routed.kind === "event" && routed.evt.type, "message.updated");
});

test("routeFrame: malformed instance_event payloads are ignored", () => {
  assert.equal(routeFrame({ event: "instance_event", data: "not-json" }).kind, "ignore");
  assert.equal(
    routeFrame({ event: "instance_event", data: JSON.stringify({ no: "type" }) }).kind,
    "ignore",
  );
});

test("routeFrame: stream_closed carries the reason and defaults sanely", () => {
  const closed = routeFrame({
    event: "stream_closed",
    data: JSON.stringify({ reason: "instance_exited" }),
  });
  assert.deepEqual(closed, { kind: "closed", reason: "instance_exited" });
  assert.deepEqual(routeFrame({ event: "stream_closed", data: "garbage" }), {
    kind: "closed",
    reason: "closed",
  });
});

test("routeFrame: connected and heartbeat", () => {
  assert.equal(routeFrame({ event: "connected", data: "{}" }).kind, "connected");
  assert.equal(routeFrame({ event: "heartbeat", data: "{}" }).kind, "ignore");
  assert.equal(routeFrame({ event: "mystery", data: "{}" }).kind, "ignore");
});
