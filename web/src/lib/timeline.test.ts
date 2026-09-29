import { strict as assert } from "node:assert";
import { test } from "node:test";
import {
  filterTimelineEvents,
  horizontalTimelineLayout,
  anchoredZoomScrollLeft,
  dragScrollLeft,
  timelineTicks,
  timelineDetailLevel,
  filterTimelineByRange,
  focusedTimelineRange,
  timelineSpatialDetail,
  timelineDestination,
  timelineFamily,
  type TimelineEvent,
} from "./timeline";

const event = (overrides: Partial<TimelineEvent>): TimelineEvent => ({
  id: "evt-1",
  type: "task.completed",
  source: "runner",
  timestamp: "2026-09-29T12:00:00Z",
  project_id: "brain-api",
  ...overrides,
});

test("timelineFamily maps modular event namespaces", () => {
  assert.equal(timelineFamily(event({ type: "feature.completed" })), "feature");
  assert.equal(timelineFamily(event({ type: "entry.created" })), "entry");
  assert.equal(timelineFamily(event({ type: "session.activity" })), "session");
  assert.equal(timelineFamily(event({ type: "runner.started" })), "runner");
  assert.equal(timelineFamily(event({ type: "project.paused" })), "project");
  assert.equal(timelineFamily(event({ type: "webhook.received" })), "other");
});

test("filterTimelineEvents scopes projects and enabled families newest first", () => {
  const events = [
    event({ id: "old-feature", type: "feature.completed", timestamp: "2026-09-27T12:00:00Z" }),
    event({ id: "new-entry", type: "entry.created", timestamp: "2026-09-29T12:00:00Z" }),
    event({ id: "other-project", type: "feature.completed", project_id: "orion-ai", timestamp: "2026-09-30T12:00:00Z" }),
  ];
  assert.deepEqual(
    filterTimelineEvents(events, { projects: new Set(["brain-api"]), families: new Set(["feature", "entry"]) }).map((e) => e.id),
    ["new-entry", "old-feature"],
  );
  assert.deepEqual(
    filterTimelineEvents(events, { projects: null, families: new Set(["feature"]) }).map((e) => e.id),
    ["other-project", "old-feature"],
  );
});

test("timelineDestination resolves real dock targets and leaves dead entries inert", () => {
  assert.deepEqual(
    timelineDestination(event({ task_id: "task-1", feature_id: "feature-1", task_title: "Ship timeline" })),
    { kind: "task-detail", target: { projectId: "brain-api", taskId: "task-1" }, title: "Ship timeline" },
  );
  assert.deepEqual(
    timelineDestination(event({ type: "feature.completed", task_id: undefined, feature_id: "feature-1" })),
    { kind: "feature-detail", target: { projectId: "brain-api", featureId: "feature-1" }, title: "feature-1" },
  );
  assert.deepEqual(
    timelineDestination(event({ type: "entry.created", task_id: undefined, task_path: "projects/brain-api/plan/abc12345.md" })),
    { kind: "entry", target: { path: "projects/brain-api/plan/abc12345.md" }, title: "abc12345" },
  );
  assert.equal(
    timelineDestination(event({ type: "entry.deleted", task_id: undefined, task_path: "projects/brain-api/plan/deleted.md" })),
    null,
  );
});

test("horizontalTimelineLayout spaces events by time and zoom", () => {
  const events = [
    event({ id: "old", timestamp: "2026-09-29T10:00:00Z" }),
    event({ id: "new", timestamp: "2026-09-29T12:00:00Z" }),
  ];
  const normal = horizontalTimelineLayout(events, { start: Date.parse("2026-09-29T09:00:00Z"), pixelsPerHour: 120 });
  const zoomed = horizontalTimelineLayout(events, { start: Date.parse("2026-09-29T09:00:00Z"), pixelsPerHour: 240 });
  assert.deepEqual(normal.map((item) => item.x), [120, 360]);
  assert.deepEqual(zoomed.map((item) => item.x), [240, 720]);
});

test("timelineTicks covers the requested infinite-window segment", () => {
  const ticks = timelineTicks({
    start: Date.parse("2026-09-29T09:00:00Z"),
    end: Date.parse("2026-09-29T13:00:00Z"),
    intervalHours: 2,
  });
  assert.deepEqual(ticks.map((tick) => new Date(tick).toISOString()), [
    "2026-09-29T10:00:00.000Z",
    "2026-09-29T12:00:00.000Z",
  ]);
});

test("dragScrollLeft pans opposite pointer movement and clamps at zero", () => {
  assert.equal(dragScrollLeft({ initialScrollLeft: 800, pointerStartX: 400, pointerX: 300 }), 900);
  assert.equal(dragScrollLeft({ initialScrollLeft: 20, pointerStartX: 100, pointerX: 180 }), 0);
});

test("anchoredZoomScrollLeft preserves the time under the pointer", () => {
  assert.equal(anchoredZoomScrollLeft({ scrollLeft: 1000, pointerX: 250, oldScale: 50, newScale: 100 }), 2250);
  assert.equal(anchoredZoomScrollLeft({ scrollLeft: 20, pointerX: 10, oldScale: 100, newScale: 50 }), 5);
});

test("timelineDetailLevel progressively reveals event information", () => {
  assert.equal(timelineDetailLevel(0), "dot");
  assert.equal(timelineDetailLevel(1), "dot");
  assert.equal(timelineDetailLevel(2), "label");
  assert.equal(timelineDetailLevel(3), "title");
  assert.equal(timelineDetailLevel(4), "detail");
});

test("filterTimelineByRange supports preset and custom windows", () => {
  const events = [
    event({ id: "old", timestamp: "2026-08-01T12:00:00Z" }),
    event({ id: "week", timestamp: "2026-09-25T12:00:00Z" }),
    event({ id: "new", timestamp: "2026-09-29T12:00:00Z" }),
  ];
  assert.deepEqual(filterTimelineByRange(events, { preset: "7d" }).map((item) => item.id), ["week", "new"]);
  assert.deepEqual(filterTimelineByRange(events, { preset: "all" }).map((item) => item.id), ["old", "week", "new"]);
  assert.deepEqual(filterTimelineByRange(events, { preset: "custom", start: "2026-09-24", end: "2026-09-26" }).map((item) => item.id), ["week"]);
});

test("focusedTimelineRange tightly pads all visible events", () => {
  const events = [
    event({ timestamp: "2026-09-29T10:00:00Z" }),
    event({ id: "new", timestamp: "2026-09-29T14:00:00Z" }),
  ];
  assert.deepEqual(focusedTimelineRange(events, 2), {
    start: Date.parse("2026-09-29T08:00:00Z"),
    end: Date.parse("2026-09-29T16:00:00Z"),
  });
});

test("timelineSpatialDetail hides labels when scale or neighbor spacing is tight", () => {
  assert.equal(timelineSpatialDetail({ pixelsPerHour: 8, nearestDistance: 100 }), "dot");
  assert.equal(timelineSpatialDetail({ pixelsPerHour: 80, nearestDistance: 25 }), "dot");
  assert.equal(timelineSpatialDetail({ pixelsPerHour: 80, nearestDistance: 90 }), "label");
  assert.equal(timelineSpatialDetail({ pixelsPerHour: 140, nearestDistance: 190 }), "title");
  assert.equal(timelineSpatialDetail({ pixelsPerHour: 220, nearestDistance: 260 }), "detail");
});
