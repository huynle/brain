import { strict as assert } from "node:assert";
import { test } from "node:test";
import {
  filterTimelineEvents,
  horizontalTimelineLayout,
  anchoredZoomScrollLeft,
  timelineScrollLeftForTimestamp,
  timelineTimestampAtViewportX,
  visibleTimelineRenderRange,
  centeredTimelineScrollLeft,
  dragScrollLeft,
  timelineTicks,
  timelineTickIntervalHours,
  timelineEdgeExtension,
  timelineDayMarkers,
  continuousTimelineScale,
  continuedTimelineZoomAnchor,
  timelineResolutionLabel,
  boundedTimelineRange,
  timelineDetailLevel,
  filterTimelineByRange,
  focusedTimelineRange,
  fittedTimelineScale,
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

test("timelineFamily uses the source kind for projected automations and reminders", () => {
  assert.equal(timelineFamily(event({ type: "automation.projected", temporal_state: "projected", source_kind: "automation" })), "automation");
  assert.equal(timelineFamily(event({ type: "reminder.projected", temporal_state: "projected", source_kind: "reminder" })), "reminder");
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

test("filterTimelineEvents can independently show actual and forecast items", () => {
  const events = [
    event({ id: "actual", temporal_state: "actual" }),
    event({ id: "future", temporal_state: "projected" }),
  ];
  assert.deepEqual(
    filterTimelineEvents(events, { projects: null, families: new Set(["task"]), temporalStates: new Set(["projected"]) }).map((item) => item.id),
    ["future"],
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
  assert.deepEqual(
    timelineDestination(event({ type: "automation.projected", task_id: undefined, task_path: undefined, temporal_state: "projected", source_kind: "automation", source_path: "projects/brain-api/automation/daily.md" })),
    { kind: "entry", target: { path: "projects/brain-api/automation/daily.md" }, title: "daily" },
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

test("visibleTimelineRenderRange limits rendering to the viewport plus overscan", () => {
  const hour = 60 * 60 * 1000;
  assert.deepEqual(visibleTimelineRenderRange({
    start: 0,
    end: 100 * hour,
    center: 50 * hour,
    pixelsPerHour: 100,
    viewportWidth: 1000,
  }), { start: 35 * hour, end: 65 * hour });
  assert.deepEqual(visibleTimelineRenderRange({
    start: 0,
    end: 100 * hour,
    center: 5 * hour,
    pixelsPerHour: 100,
    viewportWidth: 1000,
  }), { start: 0, end: 20 * hour });
});

test("timelineTickIntervalHours hides fine-grained ticks when zoomed out", () => {
  assert.equal(timelineTickIntervalHours(0.1), null);
  assert.equal(timelineTickIntervalHours(2), null);
  assert.equal(timelineTickIntervalHours(24), 4);
  assert.equal(timelineTickIntervalHours(84), 2);
  assert.equal(timelineTickIntervalHours(3600), 2 / 60);
});

test("timelineEdgeExtension ignores a canvas that already fits the viewport", () => {
  assert.equal(timelineEdgeExtension({ scrollLeft: 0, scrollWidth: 1200, clientWidth: 1200 }), null);
  assert.equal(timelineEdgeExtension({ scrollLeft: 200, scrollWidth: 3000, clientWidth: 1200 }), "before");
  assert.equal(timelineEdgeExtension({ scrollLeft: 1750, scrollWidth: 3000, clientWidth: 1200 }), "after");
});

test("timelineDayMarkers returns local calendar boundaries spanning the window", () => {
  const markers = timelineDayMarkers(
    new Date(2026, 9, 3, 13, 20).getTime(),
    new Date(2026, 9, 6, 1, 15).getTime(),
  ).map((timestamp) => new Date(timestamp));
  assert.deepEqual(markers.map((date) => date.getDate()), [3, 4, 5, 6]);
  assert.ok(markers.every((date) => date.getHours() === 0 && date.getMinutes() === 0));
});

test("dragScrollLeft pans opposite pointer movement and clamps at zero", () => {
  assert.equal(dragScrollLeft({ initialScrollLeft: 800, pointerStartX: 400, pointerX: 300 }), 900);
  assert.equal(dragScrollLeft({ initialScrollLeft: 20, pointerStartX: 100, pointerX: 180 }), 0);
});

test("anchoredZoomScrollLeft preserves the time under the pointer", () => {
  assert.equal(anchoredZoomScrollLeft({ scrollLeft: 1000, pointerX: 250, oldScale: 50, newScale: 100 }), 2250);
  assert.equal(anchoredZoomScrollLeft({ scrollLeft: 20, pointerX: 10, oldScale: 100, newScale: 50 }), 5);
});

test("timeline viewport transforms preserve an anchor when the represented range shifts", () => {
  const hour = 60 * 60 * 1000;
  const timestamp = timelineTimestampAtViewportX({
    start: 10 * hour,
    scrollLeft: 600,
    pointerX: 200,
    pixelsPerHour: 100,
  });
  assert.equal(timestamp, 18 * hour);
  assert.equal(timelineScrollLeftForTimestamp({
    timestamp,
    start: 16 * hour,
    pointerX: 200,
    pixelsPerHour: 400,
  }), 600);
});

test("continuousTimelineScale uses the full wheel delta and clamps at second-level detail", () => {
  assert.ok(continuousTimelineScale({ scale: 100, wheelDelta: -120 }) > 100);
  assert.ok(continuousTimelineScale({ scale: 100, wheelDelta: -240 }) > continuousTimelineScale({ scale: 100, wheelDelta: -120 }));
  assert.equal(continuousTimelineScale({ scale: 3500, wheelDelta: -1000 }), 3600);
  assert.equal(continuousTimelineScale({ scale: 2, wheelDelta: 1000, minScale: 1 }), 1);
});

test("continuedTimelineZoomAnchor retains precision while following pointer movement", () => {
  const hour = 60 * 60 * 1000;
  assert.equal(continuedTimelineZoomAnchor({
    timestamp: 18 * hour,
    previousPointerX: 200,
    pointerX: 200,
    pixelsPerHour: 100,
  }), 18 * hour);
  assert.equal(continuedTimelineZoomAnchor({
    timestamp: 18 * hour,
    previousPointerX: 200,
    pointerX: 250,
    pixelsPerHour: 100,
  }), 18.5 * hour);
});

test("timelineResolutionLabel describes day, hour, minute, and second scales", () => {
  assert.equal(timelineResolutionLabel(0.1), "10 hr/px");
  assert.equal(timelineResolutionLabel(2), "30 min/px");
  assert.equal(timelineResolutionLabel(240), "15 sec/px");
  assert.equal(timelineResolutionLabel(3600), "1 sec");
});

test("boundedTimelineRange contracts high-zoom canvases around the anchor", () => {
  const hour = 60 * 60 * 1000;
  assert.deepEqual(boundedTimelineRange({
    start: 0,
    end: 24 * hour,
    anchor: 12 * hour,
    pixelsPerHour: 3600,
    maxCanvasWidth: 7200,
  }), { start: 11 * hour, end: 13 * hour });
  assert.deepEqual(boundedTimelineRange({
    start: 0,
    end: hour,
    anchor: hour / 2,
    pixelsPerHour: 100,
    maxCanvasWidth: 7200,
  }), { start: 0, end: hour });
});

test("boundedTimelineRange expands a zoomed-out canvas to fill the viewport", () => {
  const hour = 60 * 60 * 1000;
  assert.deepEqual(boundedTimelineRange({
    start: 0,
    end: 10 * hour,
    anchor: 5 * hour,
    pixelsPerHour: 10,
    minCanvasWidth: 200,
    maxCanvasWidth: 7200,
  }), { start: -5 * hour, end: 15 * hour });
});

test("centeredTimelineScrollLeft places a timestamp at the viewport center", () => {
  assert.equal(centeredTimelineScrollLeft({
    timestamp: Date.parse("2026-10-03T12:00:00Z"),
    start: Date.parse("2026-10-03T08:00:00Z"),
    pixelsPerHour: 100,
    viewportWidth: 300,
  }), 250);
  assert.equal(centeredTimelineScrollLeft({
    timestamp: Date.parse("2026-10-03T08:00:00Z"),
    start: Date.parse("2026-10-03T08:00:00Z"),
    pixelsPerHour: 100,
    viewportWidth: 300,
  }), 0);
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

test("focusedTimelineRange can keep orientation timestamps in the fitted window", () => {
  const events = [event({ timestamp: "2026-09-29T10:00:00Z" })];
  assert.deepEqual(focusedTimelineRange(events, 2, [Date.parse("2026-10-03T10:00:00Z")]), {
    start: Date.parse("2026-09-29T08:00:00Z"),
    end: Date.parse("2026-10-03T12:00:00Z"),
  });
});

test("fittedTimelineScale can fit a multi-month forecast below the fixed zoom floor", () => {
  assert.equal(fittedTimelineScale({ viewportWidth: 1200, spanHours: 60 * 24, horizontalPadding: 80 }), 1120 / (60 * 24));
});

test("timelineSpatialDetail hides labels when scale or neighbor spacing is tight", () => {
  assert.equal(timelineSpatialDetail({ pixelsPerHour: 8, nearestDistance: 100 }), "dot");
  assert.equal(timelineSpatialDetail({ pixelsPerHour: 80, nearestDistance: 25 }), "dot");
  assert.equal(timelineSpatialDetail({ pixelsPerHour: 80, nearestDistance: 90 }), "label");
  assert.equal(timelineSpatialDetail({ pixelsPerHour: 140, nearestDistance: 190 }), "title");
  assert.equal(timelineSpatialDetail({ pixelsPerHour: 220, nearestDistance: 260 }), "detail");
});
