import { describe, test, expect } from "bun:test";
import { createProjectRealtimeHub } from "./realtime-hub";

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
});
