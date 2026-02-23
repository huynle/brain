import { describe, test, expect } from "bun:test";
import { createProjectRealtimeHub, publishProjectDirty } from "./realtime-hub";

describe("ProjectRealtimeHub", () => {
  test("publishes only to subscribers in the same project", () => {
    const hub = createProjectRealtimeHub();
    const alphaEvents: string[] = [];
    const betaEvents: string[] = [];

    hub.subscribe("alpha", ({ event }) => alphaEvents.push(event));
    hub.subscribe("beta", ({ event }) => betaEvents.push(event));

    hub.publish("alpha", { event: "tasks_snapshot", payload: { count: 1 } });

    expect(alphaEvents).toEqual(["tasks_snapshot"]);
    expect(betaEvents).toEqual([]);
  });

  test("unsubscribe removes project subscriber", () => {
    const hub = createProjectRealtimeHub();
    const unsubscribe = hub.subscribe("alpha", () => {
      // no-op
    });

    expect(hub.getSubscriberCount("alpha")).toBe(1);

    unsubscribe();

    expect(hub.getSubscriberCount("alpha")).toBe(0);
  });

  test("publishProjectDirty emits project_dirty event with SSE payload", () => {
    const hub = createProjectRealtimeHub();
    const events: Array<{ event: string; payload: unknown }> = [];

    hub.subscribe("alpha", (event) => events.push(event));

    publishProjectDirty(hub, "alpha");

    expect(events).toHaveLength(1);
    expect(events[0]?.event).toBe("project_dirty");
    expect(events[0]?.payload).toMatchObject({
      type: "project_dirty",
      transport: "sse",
      projectId: "alpha",
    });
  });
});
