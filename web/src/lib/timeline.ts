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
};

export type TimelineFamily =
  | "task"
  | "feature"
  | "entry"
  | "session"
  | "runner"
  | "project"
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

export function anchoredZoomScrollLeft(options: {
  scrollLeft: number;
  pointerX: number;
  oldScale: number;
  newScale: number;
}): number {
  const anchorHour = (options.scrollLeft + options.pointerX) / options.oldScale;
  return Math.max(0, anchorHour * options.newScale - options.pointerX);
}

export function timelineZoomTarget(currentIndex: number, direction: -1 | 1, levelCount: number): number | "fit" | null {
  const next = currentIndex + direction;
  if (next < 0) return "fit";
  if (next >= levelCount) return null;
  return next;
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
  filters: { projects: Set<string> | null; families: Set<string> },
): TimelineEvent[] {
  return events
    .filter((event) => !filters.projects || (!!event.project_id && filters.projects.has(event.project_id)))
    .filter((event) => filters.families.has(timelineFamily(event)))
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
