export type TimelineEvent = {
  id: string;
  type: string;
  source: string;
  timestamp: string;
  project_id?: string;
  task_id?: string;
  task_path?: string;
  task_title?: string;
  feature_id?: string;
  runner_id?: string;
  reason?: string;
  summary?: string;
  metadata?: Record<string, string>;
  temporal_state?: "actual" | "projected";
  temporal_kind?: "execution" | "reminder" | "start" | "deadline" | "expiry";
  source_kind?: "task" | "automation" | "reminder" | "feature";
  source_id?: string;
  source_path?: string;
  timezone?: string;
  projection_rule?: string;
  occurrence_count?: number;
  window_start?: string;
  window_end?: string;
};

export type TimelineFamily =
  | "task"
  | "feature"
  | "entry"
  | "session"
  | "runner"
  | "project"
  | "automation"
  | "reminder"
  | "other";

export type TimelineDestination = {
  kind: "task-detail" | "feature-detail" | "entry" | "project";
  target: Record<string, string>;
  title: string;
};

export type TimelineSource = {
  mode: "seeded" | "live";
  events: TimelineEvent[];
};

export function horizontalTimelineLayout(
  events: TimelineEvent[],
  options: { start: number; pixelsPerHour: number },
): Array<{ event: TimelineEvent; x: number }> {
  const hour = 60 * 60 * 1000;
  return [...events]
    .sort((a, b) => Date.parse(a.timestamp) - Date.parse(b.timestamp))
    .map((event) => ({
      event,
      x: ((Date.parse(event.timestamp) - options.start) / hour) * options.pixelsPerHour,
    }));
}

export function timelineTicks(options: { start: number; end: number; intervalHours: number }): number[] {
  const interval = options.intervalHours * 60 * 60 * 1000;
  const first = Math.ceil(options.start / interval) * interval;
  const ticks: number[] = [];
  for (let tick = first; tick <= options.end; tick += interval) ticks.push(tick);
  return ticks;
}

export function visibleTimelineRenderRange(options: {
  start: number;
  end: number;
  center: number;
  pixelsPerHour: number;
  viewportWidth: number;
  overscanViewports?: number;
}): { start: number; end: number } {
  if (options.viewportWidth <= 0) return { start: options.start, end: options.end };
  const hour = 60 * 60 * 1000;
  const viewportSpan = (options.viewportWidth / options.pixelsPerHour) * hour;
  const halfRenderSpan = viewportSpan * (0.5 + (options.overscanViewports ?? 1));
  return {
    start: Math.max(options.start, options.center - halfRenderSpan),
    end: Math.min(options.end, options.center + halfRenderSpan),
  };
}

export function timelineTickIntervalHours(pixelsPerHour: number, minimumSpacing = 96): number | null {
  const intervalsInSeconds = [1, 2, 5, 10, 15, 30, 60, 120, 300, 600, 900, 1800, 3600, 7200, 14400, 28800, 43200];
  const interval = intervalsInSeconds.find((seconds) => (seconds / 3600) * pixelsPerHour >= minimumSpacing);
  return interval === undefined ? null : interval / 3600;
}

export function timelineEdgeExtension(options: {
  scrollLeft: number;
  scrollWidth: number;
  clientWidth: number;
  threshold?: number;
}): "before" | "after" | null {
  if (options.scrollWidth <= options.clientWidth + 1) return null;
  const threshold = options.threshold ?? 600;
  if (options.scrollLeft < threshold) return "before";
  if (options.scrollWidth - options.clientWidth - options.scrollLeft < threshold) return "after";
  return null;
}

export function timelineDayMarkers(start: number, end: number): number[] {
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return [];
  const cursor = new Date(start);
  cursor.setHours(0, 0, 0, 0);
  const markers: number[] = [];
  while (cursor.getTime() <= end) {
    markers.push(cursor.getTime());
    cursor.setDate(cursor.getDate() + 1);
  }
  return markers;
}

export function dragScrollLeft(options: {
  initialScrollLeft: number;
  pointerStartX: number;
  pointerX: number;
}): number {
  return Math.max(0, options.initialScrollLeft + options.pointerStartX - options.pointerX);
}

export function timelineDragShouldCapture(options: {
  pointerStartX: number;
  pointerX: number;
  threshold?: number;
}): boolean {
  return Math.abs(options.pointerX - options.pointerStartX) > (options.threshold ?? 4);
}

export function anchoredZoomScrollLeft(options: {
  scrollLeft: number;
  pointerX: number;
  oldScale: number;
  newScale: number;
}): number {
  const anchorHour = (options.scrollLeft + options.pointerX) / options.oldScale;
  return Math.max(0, anchorHour * options.newScale - options.pointerX);
}

export function timelineTimestampAtViewportX(options: {
  start: number;
  scrollLeft: number;
  pointerX: number;
  pixelsPerHour: number;
}): number {
  const hour = 60 * 60 * 1000;
  return options.start + ((options.scrollLeft + options.pointerX) / options.pixelsPerHour) * hour;
}

export function timelineScrollLeftForTimestamp(options: {
  timestamp: number;
  start: number;
  pointerX: number;
  pixelsPerHour: number;
}): number {
  const hour = 60 * 60 * 1000;
  const x = ((options.timestamp - options.start) / hour) * options.pixelsPerHour;
  return Math.max(0, x - options.pointerX);
}

export const MIN_TIMELINE_SCALE = 0.1;
export const MAX_TIMELINE_SCALE = 3600;

export function continuousTimelineScale(options: {
  scale: number;
  wheelDelta: number;
  minScale?: number;
  maxScale?: number;
}): number {
  const minScale = options.minScale ?? MIN_TIMELINE_SCALE;
  const maxScale = options.maxScale ?? MAX_TIMELINE_SCALE;
  const next = options.scale * Math.exp(-options.wheelDelta * 0.002);
  return Math.max(minScale, Math.min(maxScale, next));
}

export function continuedTimelineZoomAnchor(options: {
  timestamp: number;
  previousPointerX: number;
  pointerX: number;
  pixelsPerHour: number;
}): number {
  const hour = 60 * 60 * 1000;
  return options.timestamp + ((options.pointerX - options.previousPointerX) / options.pixelsPerHour) * hour;
}

export function timelineResolutionLabel(pixelsPerHour: number): string {
  const secondsPerPixel = 3600 / Math.max(MIN_TIMELINE_SCALE, pixelsPerHour);
  if (secondsPerPixel <= 1) return "1 sec";
  if (secondsPerPixel < 60) return `${Math.round(secondsPerPixel)} sec/px`;
  if (secondsPerPixel < 3600) return `${Math.round(secondsPerPixel / 60)} min/px`;
  return `${Math.round(secondsPerPixel / 3600)} hr/px`;
}

export function boundedTimelineRange(options: {
  start: number;
  end: number;
  anchor: number;
  pixelsPerHour: number;
  minCanvasWidth?: number;
  maxCanvasWidth: number;
}): { start: number; end: number } {
  const hour = 60 * 60 * 1000;
  const minSpan = ((options.minCanvasWidth ?? 0) / options.pixelsPerHour) * hour;
  const maxSpan = (options.maxCanvasWidth / options.pixelsPerHour) * hour;
  if (options.end - options.start < minSpan) return { start: options.anchor - minSpan / 2, end: options.anchor + minSpan / 2 };
  if (options.end - options.start <= maxSpan) return { start: options.start, end: options.end };
  return { start: options.anchor - maxSpan / 2, end: options.anchor + maxSpan / 2 };
}

export function centeredTimelineScrollLeft(options: {
  timestamp: number;
  start: number;
  pixelsPerHour: number;
  viewportWidth: number;
}): number {
  const hour = 60 * 60 * 1000;
  const x = ((options.timestamp - options.start) / hour) * options.pixelsPerHour;
  return Math.max(0, x - options.viewportWidth / 2);
}

export function timelineDetailLevel(zoomIndex: number): "dot" | "label" | "title" | "detail" {
  if (zoomIndex <= 1) return "dot";
  if (zoomIndex === 2) return "label";
  if (zoomIndex === 3) return "title";
  return "detail";
}

export type TimelineRangeFilter = {
  preset: "all" | "24h" | "7d" | "30d" | "custom";
  start?: string;
  end?: string;
};

export function filterTimelineByRange(events: TimelineEvent[], range: TimelineRangeFilter): TimelineEvent[] {
  if (range.preset === "all" || events.length === 0) return events;
  let start = Number.NEGATIVE_INFINITY;
  let end = Number.POSITIVE_INFINITY;
  if (range.preset === "custom") {
    if (range.start) start = Date.parse(`${range.start}T00:00:00`);
    if (range.end) end = Date.parse(`${range.end}T23:59:59.999`);
  } else {
    const latest = Math.max(...events.map((event) => Date.parse(event.timestamp)));
    const hours = range.preset === "24h" ? 24 : range.preset === "7d" ? 7 * 24 : 30 * 24;
    start = latest - hours * 60 * 60 * 1000;
  }
  return events.filter((event) => {
    const timestamp = Date.parse(event.timestamp);
    return timestamp >= start && timestamp <= end;
  });
}

export function focusedTimelineRange(events: TimelineEvent[], paddingHours: number, orientationTimestamps: number[] = []): { start: number; end: number } | null {
  if (events.length === 0) return null;
  const timestamps = [...events.map((event) => Date.parse(event.timestamp)), ...orientationTimestamps];
  const padding = paddingHours * 60 * 60 * 1000;
  return { start: Math.min(...timestamps) - padding, end: Math.max(...timestamps) + padding };
}

export function fittedTimelineScale(options: { viewportWidth: number; spanHours: number; horizontalPadding?: number }): number {
  const available = Math.max(1, options.viewportWidth - (options.horizontalPadding ?? 80));
  return Math.max(0.1, Math.min(210, available / Math.max(1, options.spanHours)));
}

export function timelineSpatialDetail(options: {
  pixelsPerHour: number;
  nearestDistance: number;
}): "dot" | "label" | "title" | "detail" {
  if (options.pixelsPerHour < 32 || options.nearestDistance < 56) return "dot";
  if (options.pixelsPerHour < 120 || options.nearestDistance < 150) return "label";
  if (options.pixelsPerHour < 200 || options.nearestDistance < 230) return "title";
  return "detail";
}

export const TIMELINE_FAMILIES: TimelineFamily[] = [
  "feature",
  "task",
  "automation",
  "reminder",
  "entry",
  "session",
  "runner",
  "project",
];

export function timelineTitle(event: TimelineEvent): string {
  if (event.task_title) return event.task_title;
  if (event.feature_id) return event.feature_id;
  if (event.type.startsWith("entry.") && event.task_path) {
    return (event.task_path.split("/").at(-1) || event.task_path).replace(/\.md$/, "");
  }
  if (event.type.startsWith("runner.") && event.runner_id) return event.runner_id;
  if (event.project_id) return event.project_id;
  return event.type;
}

export function timelineTone(event: TimelineEvent): "good" | "warn" | "bad" | "info" {
  if (/completed|created|resumed|started/.test(event.type)) return "good";
  if (/failed|blocked|deleted|stopped/.test(event.type)) return "bad";
  if (/updated|progress|activity/.test(event.type)) return "warn";
  return "info";
}

export function timelineFamily(event: TimelineEvent): TimelineFamily {
  if (event.temporal_state === "projected" && (event.source_kind === "automation" || event.source_kind === "reminder")) {
    return event.source_kind;
  }
  const namespace = event.type.split(".", 1)[0];
  if (
    namespace === "task" ||
    namespace === "feature" ||
    namespace === "entry" ||
    namespace === "session" ||
    namespace === "runner" ||
    namespace === "project"
  ) {
    return namespace;
  }
  return "other";
}

export function filterTimelineEvents(
  events: TimelineEvent[],
  filters: { projects: Set<string> | null; families: Set<string>; temporalStates?: Set<string> },
): TimelineEvent[] {
  return events
    .filter((event) => !filters.projects || (!!event.project_id && filters.projects.has(event.project_id)))
    .filter((event) => filters.families.has(timelineFamily(event)))
    .filter((event) => !filters.temporalStates || filters.temporalStates.has(event.temporal_state || "actual"))
    .toSorted((a, b) => Date.parse(b.timestamp) - Date.parse(a.timestamp));
}

export function timelineDestination(event: TimelineEvent): TimelineDestination | null {
  if (event.project_id && event.task_id) {
    return {
      kind: "task-detail",
      target: { projectId: event.project_id, taskId: event.task_id },
      title: event.task_title || event.task_id,
    };
  }
  if (event.project_id && event.feature_id) {
    return {
      kind: "feature-detail",
      target: { projectId: event.project_id, featureId: event.feature_id },
      title: event.feature_id,
    };
  }
  if (event.temporal_state === "projected" && event.source_path && (event.source_kind === "automation" || event.source_kind === "reminder")) {
    const filename = event.source_path.split("/").at(-1) || event.source_path;
    return {
      kind: "entry",
      target: { path: event.source_path },
      title: filename.replace(/\.md$/, ""),
    };
  }
  if (event.type !== "entry.deleted" && event.type.startsWith("entry.") && event.task_path) {
    const filename = event.task_path.split("/").at(-1) || event.task_path;
    return {
      kind: "entry",
      target: { path: event.task_path },
      title: filename.replace(/\.md$/, ""),
    };
  }
  if (event.project_id && event.type.startsWith("project.")) {
    return {
      kind: "project",
      target: { projectId: event.project_id },
      title: event.project_id,
    };
  }
  return null;
}
