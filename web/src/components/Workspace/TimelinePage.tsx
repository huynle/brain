import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useDeferredPreview } from "../../hooks/useDeferredPreview";
import { useProjects } from "../../hooks/useProjects";
import { useTimeline } from "../../hooks/useTimeline";
import { useVisibleProjects } from "../../hooks/useVisibleProjects";
import { boundedTimelineRange, centeredTimelineScrollLeft, continuousTimelineScale, dragScrollLeft, filterTimelineByRange, filterTimelineEvents, fittedTimelineScale, focusedTimelineRange, horizontalTimelineLayout, MAX_TIMELINE_SCALE, MIN_TIMELINE_SCALE, TIMELINE_FAMILIES, timelineDayMarkers, timelineDestination, timelineEdgeExtension, timelineFamily, timelineResolutionLabel, timelineScrollLeftForTimestamp, timelineSpatialDetail, timelineTickIntervalHours, timelineTicks, timelineTimestampAtViewportX, timelineTitle, type TimelineEvent, type TimelineFamily, type TimelineRangeFilter } from "../../lib/timeline";
import { useWorkspace } from "../../store/workspace";

const FAMILY_LABELS: Record<TimelineFamily, string> = { feature: "Features", task: "Tasks", automation: "Automations", reminder: "Reminders", entry: "Entries", session: "Sessions", runner: "Runners", project: "Projects", other: "Other" };
const HOUR = 60 * 60 * 1000;
const INITIAL_WINDOW_HOURS = 24 * 24;
const EXTENSION_HOURS = 14 * 24;
const INITIAL_SCALE = 84;
const MAX_CANVAS_WIDTH = 8_000_000;

function formatTick(timestamp: number, intervalHours: number): string {
  if (intervalHours < 1 / 60) return new Intl.DateTimeFormat(undefined, { hour: "numeric", minute: "2-digit", second: "2-digit" }).format(new Date(timestamp));
  if (intervalHours < 1) return new Intl.DateTimeFormat(undefined, { hour: "numeric", minute: "2-digit" }).format(new Date(timestamp));
  return new Intl.DateTimeFormat(undefined, { hour: "numeric" }).format(new Date(timestamp));
}

export function TimelinePage(): JSX.Element {
  const timelineOrigin = useRef(Date.now()).current;
  const { data: projects } = useProjects();
  const { visible } = useVisibleProjects();
  const [projectScope, setProjectScope] = useState("all");
  const [families, setFamilies] = useState<Set<string>>(() => new Set(TIMELINE_FAMILIES));
  const [temporalStates, setTemporalStates] = useState<Set<string>>(() => new Set(["actual", "projected"]));
  const [selected, setSelected] = useState<string | null>(null);
  const [pixelsPerHour, setPixelsPerHour] = useState(INITIAL_SCALE);
  const [rangeAnchor, setRangeAnchor] = useState(timelineOrigin);
  const [viewportWidth, setViewportWidth] = useState(0);
  const [isFit, setIsFit] = useState(false);
  const [range, setRange] = useState({ before: INITIAL_WINDOW_HOURS / 2, after: INITIAL_WINDOW_HOURS / 2 });
  const [timeFilter, setTimeFilter] = useState<TimelineRangeFilter>({ preset: "all" });
  const [focusedRange, setFocusedRange] = useState<{ start: number; end: number } | null>(null);
  const [clockNow, setClockNow] = useState(timelineOrigin);
  const [viewportTimestamp, setViewportTimestamp] = useState(timelineOrigin);
  const [centerRequest, setCenterRequest] = useState({ id: 0, timestamp: timelineOrigin, behavior: "auto" as ScrollBehavior });
  const scrollerRef = useRef<HTMLDivElement>(null);
  const centeredRequestRef = useRef(-1);
  const viewportFrameRef = useRef<number | null>(null);
  const zoomFrameRef = useRef<number | null>(null);
  const queuedWheelZoomRef = useRef<{ delta: number; pointerX: number } | null>(null);
  const pendingZoomAnchorRef = useRef<{ timestamp: number; pointerX: number } | null>(null);
  const pendingScrollLeftRef = useRef<number | null>(null);
  const extendingRef = useRef(false);
  const dragRef = useRef<{ pointerId: number; x: number; scrollLeft: number; moved: boolean } | null>(null);
  const suppressClickRef = useRef(false);
  const scaleRef = useRef(pixelsPerHour);
  const preview = useDeferredPreview();
  const openSidebar = useWorkspace((state) => state.openOrReuseInSidebar);
  const openFocus = useWorkspace((state) => state.openInFocus);

  const queryFrom = useMemo(() => new Date(timelineOrigin - 30 * 24 * HOUR).toISOString(), [timelineOrigin]);
  const queryTo = useMemo(() => new Date(timelineOrigin + 30 * 24 * HOUR).toISOString(), [timelineOrigin]);
  const timelineQuery = useTimeline(queryFrom, queryTo, projectScope !== "all" && projectScope !== "sidebar" ? projectScope : undefined);

  const projectFilter = useMemo(() => projectScope === "all" ? null : new Set(projectScope === "sidebar" ? visible : [projectScope]), [projectScope, visible]);
  const scopedEvents = useMemo(() => filterTimelineEvents(timelineQuery.data?.items || [], { projects: projectFilter, families, temporalStates }), [families, projectFilter, temporalStates, timelineQuery.data?.items]);
  const events = useMemo(() => filterTimelineByRange(scopedEvents, timeFilter), [scopedEvents, timeFilter]);
  const center = timelineOrigin;
  const rawStart = focusedRange?.start ?? center - range.before * HOUR;
  const rawEnd = focusedRange?.end ?? center + range.after * HOUR;
  const boundedRange = boundedTimelineRange({ start: rawStart, end: rawEnd, anchor: rangeAnchor, pixelsPerHour, minCanvasWidth: viewportWidth, maxCanvasWidth: MAX_CANVAS_WIDTH });
  const start = boundedRange.start;
  const end = boundedRange.end;
  const startRef = useRef(start);
  startRef.current = start;
  const canvasWidth = ((end - start) / HOUR) * pixelsPerHour;
  const laidOut = useMemo(() => horizontalTimelineLayout(events, { start, pixelsPerHour }), [events, start, pixelsPerHour]);
  const tickInterval = timelineTickIntervalHours(pixelsPerHour);
  const ticks = useMemo(() => tickInterval === null ? [] : timelineTicks({ start, end, intervalHours: tickInterval }), [start, end, tickInterval]);
  const dayMarkers = useMemo(() => timelineDayMarkers(start, end), [start, end]);
  const dayLabelEvery = Math.max(1, Math.ceil(120 / Math.max(1, 24 * pixelsPerHour)));
  const nowX = ((clockNow - start) / HOUR) * pixelsPerHour;
  const nowLabel = useMemo(() => new Intl.DateTimeFormat(undefined, { weekday: "short", month: "short", day: "numeric", hour: "numeric", minute: "2-digit" }).format(new Date(clockNow)), [clockNow]);
  const viewportDateLabel = useMemo(() => new Intl.DateTimeFormat(undefined, { weekday: "short", month: "short", day: "numeric", year: "numeric" }).format(new Date(viewportTimestamp)), [viewportTimestamp]);
  const viewportTimeLabel = useMemo(() => new Intl.DateTimeFormat(undefined, { hour: "numeric", minute: "2-digit" }).format(new Date(viewportTimestamp)), [viewportTimestamp]);

  useEffect(() => {
    const interval = window.setInterval(() => setClockNow(Date.now()), 30_000);
    return () => {
      window.clearInterval(interval);
      if (viewportFrameRef.current !== null) window.cancelAnimationFrame(viewportFrameRef.current);
      if (zoomFrameRef.current !== null) window.cancelAnimationFrame(zoomFrameRef.current);
    };
  }, []);

  useLayoutEffect(() => {
    const scroller = scrollerRef.current;
    if (!scroller) return;
    const observer = new ResizeObserver(() => {
      const width = scroller.clientWidth;
      setViewportWidth((current) => {
        if (current === width) return current;
        pendingZoomAnchorRef.current = {
          timestamp: current === 0 ? timelineOrigin : timelineTimestampAtViewportX({
            start: startRef.current,
            scrollLeft: scroller.scrollLeft,
            pointerX: current / 2,
            pixelsPerHour: scaleRef.current,
          }),
          pointerX: width / 2,
        };
        return width;
      });
    });
    observer.observe(scroller);
    return () => observer.disconnect();
  }, []);

  useLayoutEffect(() => {
    const scroller = scrollerRef.current;
    if (!scroller) return;
    const zoomAnchor = pendingZoomAnchorRef.current;
    if (zoomAnchor) {
      scroller.scrollLeft = timelineScrollLeftForTimestamp({
        timestamp: zoomAnchor.timestamp,
        start,
        pointerX: zoomAnchor.pointerX,
        pixelsPerHour,
      });
      pendingZoomAnchorRef.current = null;
    } else if (pendingScrollLeftRef.current !== null) {
      scroller.scrollLeft = pendingScrollLeftRef.current;
      pendingScrollLeftRef.current = null;
    }
  }, [pixelsPerHour, start, viewportWidth]);

  useLayoutEffect(() => {
    const scroller = scrollerRef.current;
    if (!scroller || centeredRequestRef.current === centerRequest.id) return;
    centeredRequestRef.current = centerRequest.id;
    scroller.scrollTo({
      left: centeredTimelineScrollLeft({ timestamp: centerRequest.timestamp, start, pixelsPerHour, viewportWidth: scroller.clientWidth }),
      behavior: centerRequest.behavior,
    });
    setViewportTimestamp(centerRequest.timestamp);
  }, [centerRequest, pixelsPerHour, start]);

  const updateViewportTimestamp = (scroller: HTMLDivElement) => {
    if (viewportFrameRef.current !== null) return;
    viewportFrameRef.current = window.requestAnimationFrame(() => {
      viewportFrameRef.current = null;
      const centerX = scroller.scrollWidth <= scroller.clientWidth ? scroller.scrollWidth / 2 : scroller.scrollLeft + scroller.clientWidth / 2;
      setViewportTimestamp(start + (centerX / pixelsPerHour) * HOUR);
    });
  };

  const returnToNow = () => {
    const timestamp = Date.now();
    setClockNow(timestamp);
    setFocusedRange(null);
    setIsFit(false);
    setCenterRequest((current) => ({ id: current.id + 1, timestamp, behavior: "auto" }));
  };

  const zoomAtScale = (nextScale: number, pointerX: number) => {
    const scroller = scrollerRef.current;
    const currentScale = scaleRef.current;
    if (!scroller || nextScale === currentScale) return;
    const timestamp = timelineTimestampAtViewportX({
      start: startRef.current,
      scrollLeft: scroller.scrollLeft,
      pointerX,
      pixelsPerHour: currentScale,
    });
    pendingZoomAnchorRef.current = { timestamp, pointerX };
    setRangeAnchor(timestamp);
    setPixelsPerHour(nextScale);
    scaleRef.current = nextScale;
    setIsFit(false);
  };

  const extendAtEdge = () => {
    const scroller = scrollerRef.current;
    if (!scroller || extendingRef.current || focusedRange || canvasWidth >= MAX_CANVAS_WIDTH) return;
    const edge = timelineEdgeExtension(scroller);
    if (edge === "before") {
      extendingRef.current = true;
      setRange((current) => ({ ...current, before: current.before + EXTENSION_HOURS }));
      requestAnimationFrame(() => {
        scroller.scrollLeft += EXTENSION_HOURS * pixelsPerHour;
        extendingRef.current = false;
      });
    } else if (edge === "after") {
      extendingRef.current = true;
      setRange((current) => ({ ...current, after: current.after + EXTENSION_HOURS }));
      requestAnimationFrame(() => { extendingRef.current = false; });
    }
  };

  const focusEvents = () => {
    const scroller = scrollerRef.current;
    const bounds = focusedTimelineRange(events, 2, [clockNow]);
    if (!scroller || !bounds) return;
    const spanHours = Math.max(1, (bounds.end - bounds.start) / HOUR);
    const targetScale = fittedTimelineScale({ viewportWidth: scroller.clientWidth, spanHours });
    pendingScrollLeftRef.current = 0;
    setRangeAnchor((bounds.start + bounds.end) / 2);
    setFocusedRange(bounds);
    setPixelsPerHour(targetScale);
    scaleRef.current = targetScale;
    setIsFit(true);
  };

  const stepZoom = (direction: -1 | 1) => {
    const scroller = scrollerRef.current;
    if (!scroller) return;
    const nextScale = continuousTimelineScale({ scale: pixelsPerHour, wheelDelta: direction > 0 ? -180 : 180 });
    zoomAtScale(nextScale, scroller.clientWidth / 2);
  };

  useEffect(() => {
    const scroller = scrollerRef.current;
    if (!scroller) return;
    const handleWheel = (event: WheelEvent) => {
      if (event.shiftKey) {
        event.preventDefault();
        const delta = Math.abs(event.deltaY) >= Math.abs(event.deltaX) ? event.deltaY : event.deltaX;
        if (delta === 0) return;
        const pointerX = event.clientX - scroller.getBoundingClientRect().left;
        const queued = queuedWheelZoomRef.current;
        queuedWheelZoomRef.current = { delta: (queued?.delta ?? 0) + delta, pointerX };
        if (zoomFrameRef.current === null) {
          zoomFrameRef.current = requestAnimationFrame(() => {
            zoomFrameRef.current = null;
            const request = queuedWheelZoomRef.current;
            queuedWheelZoomRef.current = null;
            if (!request) return;
            const nextScale = continuousTimelineScale({ scale: scaleRef.current, wheelDelta: request.delta });
            zoomAtScale(nextScale, request.pointerX);
          });
        }
        return;
      }
      if (Math.abs(event.deltaY) > Math.abs(event.deltaX)) {
        event.preventDefault();
        scroller.scrollLeft += event.deltaY;
      }
    };
    scroller.addEventListener("wheel", handleWheel, { passive: false });
    return () => scroller.removeEventListener("wheel", handleWheel);
  }, []);

  const toggleFamily = (family: TimelineFamily) => setFamilies((current) => {
    setFocusedRange(null);
    setIsFit(false);
    const next = new Set(current);
    if (next.has(family)) next.delete(family); else next.add(family);
    return next;
  });
  const previewEvent = (event: TimelineEvent) => {
    setSelected(event.id);
    const destination = timelineDestination(event);
    if (destination) preview.schedule(() => openSidebar(destination.kind, destination.target, destination.title));
  };
  const focusEvent = (event: TimelineEvent) => {
    const destination = timelineDestination(event);
    if (!destination) return;
    preview.cancel();
    openFocus(destination.kind, destination.target, destination.title);
  };

  return <section className="timeline-page timeline-horizontal">
    <header className="timeline-hero">
      <div><div className="timeline-eyebrow">Activity observatory</div><h1>Timeline</h1><p>Pan through recorded history and the next 30 days of scheduled work.</p></div>
      <div className={`timeline-seed-badge ${timelineQuery.isError ? "error" : ""}`}><span />{timelineQuery.isLoading ? "Loading timeline" : timelineQuery.isError ? "Forecast unavailable" : "Live forecast"}</div>
    </header>
    <div className="timeline-controls horizontal-controls">
      <div className="timeline-filter-fields">
        <label><span>Project scope</span><select aria-label="Project scope" value={projectScope} onChange={(event) => { setFocusedRange(null); setIsFit(false); setProjectScope(event.target.value); }}><option value="all">All projects</option><option value="sidebar">Sidebar projects</option>{(projects || []).map((project) => <option key={project} value={project}>{project}</option>)}</select></label>
        <label className="timeline-range-filter"><span>Time range</span><select aria-label="Time range" value={timeFilter.preset} onChange={(event) => { setFocusedRange(null); setIsFit(false); setTimeFilter({ preset: event.target.value as TimelineRangeFilter["preset"] }); }}><option value="all">All time</option><option value="24h">Last 24 hours</option><option value="7d">Last 7 days</option><option value="30d">Last 30 days</option><option value="custom">Custom range</option></select></label>
      </div>
      <div className="timeline-family-filter timeline-legend" aria-label="Event families">{TIMELINE_FAMILIES.map((family) => <button key={family} className={`family-${family} ${families.has(family) ? "active" : ""}`} aria-pressed={families.has(family)} onClick={() => toggleFamily(family)}><span />{FAMILY_LABELS[family]}</button>)}</div>
      <div className="timeline-state-filter" aria-label="Temporal state">{[["actual", "Actual"], ["projected", "Forecast"]].map(([state, label]) => <button key={state} className={temporalStates.has(state) ? "active" : ""} aria-pressed={temporalStates.has(state)} onClick={() => setTemporalStates((current) => { const next = new Set(current); if (next.has(state)) next.delete(state); else next.add(state); setFocusedRange(null); setIsFit(false); return next; })}>{label}</button>)}</div>
      <div className="timeline-control-actions">
        <button className="timeline-now-button" onClick={returnToNow} title="Center the timeline on the current time"><span>Today</span><time dateTime={new Date(clockNow).toISOString()}>{nowLabel}</time></button>
        <button className="timeline-focus-button" onClick={focusEvents}>Focus events</button>
        <div className="timeline-zoom"><button onClick={() => stepZoom(-1)} disabled={pixelsPerHour <= MIN_TIMELINE_SCALE} aria-label="Zoom out">−</button><span>{isFit ? "Fit" : timelineResolutionLabel(pixelsPerHour)}</span><button onClick={() => stepZoom(1)} disabled={pixelsPerHour >= MAX_TIMELINE_SCALE} aria-label="Zoom in">+</button></div>
      </div>
    </div>
    {timeFilter.preset === "custom" && <div className="timeline-custom-range"><label>From <input type="date" value={timeFilter.start || ""} onChange={(event) => setTimeFilter((current) => ({ ...current, start: event.target.value }))} /></label><label>To <input type="date" value={timeFilter.end || ""} onChange={(event) => setTimeFilter((current) => ({ ...current, end: event.target.value }))} /></label></div>}
    <div className="timeline-summary"><b>{events.length} events in view</b>{(timelineQuery.data?.warnings.length || 0) > 0 && <span className="timeline-warning">{timelineQuery.data!.warnings.length} schedule warning{timelineQuery.data!.warnings.length === 1 ? "" : "s"}</span>}<div className="timeline-viewport-date" aria-live="polite"><small>Viewing</small><strong>{viewportDateLabel}</strong><time dateTime={new Date(viewportTimestamp).toISOString()}>{viewportTimeLabel}</time></div><span className="timeline-help">Drag or wheel to travel · Shift + wheel to zoom · Hover dots for details</span></div>
    <div
      className="timeline-scroll"
      ref={scrollerRef}
      onClickCapture={(event) => {
        if (!suppressClickRef.current) return;
        suppressClickRef.current = false;
        event.preventDefault();
        event.stopPropagation();
      }}
      onDoubleClick={(event) => { if (!(event.target as Element).closest(".timeline-marker")) returnToNow(); }}
      onScroll={(event) => { updateViewportTimestamp(event.currentTarget); extendAtEdge(); }}
      onPointerDown={(event) => {
        if (event.button !== 0) return;
        dragRef.current = { pointerId: event.pointerId, x: event.clientX, scrollLeft: event.currentTarget.scrollLeft, moved: false };
        event.currentTarget.setPointerCapture(event.pointerId);
      }}
      onPointerMove={(event) => {
        const drag = dragRef.current;
        if (!drag || drag.pointerId !== event.pointerId) return;
        if (Math.abs(event.clientX - drag.x) > 4) drag.moved = true;
        event.currentTarget.scrollLeft = dragScrollLeft({ initialScrollLeft: drag.scrollLeft, pointerStartX: drag.x, pointerX: event.clientX });
      }}
      onPointerUp={(event) => {
        const drag = dragRef.current;
        if (!drag || drag.pointerId !== event.pointerId) return;
        suppressClickRef.current = drag.moved;
        dragRef.current = null;
        event.currentTarget.releasePointerCapture(event.pointerId);
      }}
      onPointerCancel={() => { dragRef.current = null; }}
    >
      <div className="timeline-canvas" style={{ width: canvasWidth }}>
        {clockNow >= start && clockNow <= end && <div className="timeline-forecast-field" style={{ left: nowX, width: Math.max(0, canvasWidth - nowX) }}><span>Forecast · next 30 days</span></div>}
        {dayMarkers.map((day, index) => {
          const nextDay = dayMarkers[index + 1] ?? end;
          const x = ((day - start) / HOUR) * pixelsPerHour;
          const width = Math.max(0, ((nextDay - day) / HOUR) * pixelsPerHour);
          const date = new Date(day);
          const isToday = date.toDateString() === new Date(clockNow).toDateString();
          const showLabel = isToday || index % dayLabelEvery === 0;
          return <div key={day} className={`timeline-day-band ${index % 2 ? "alternate" : ""} ${isToday ? "today" : ""}`} style={{ left: x, width }}>
            {showLabel && <time dateTime={date.toISOString()}>{new Intl.DateTimeFormat(undefined, { weekday: "short", month: "short", day: "numeric" }).format(date)}</time>}
          </div>;
        })}
        <div className="timeline-axis" />
        {clockNow >= start && clockNow <= end && <div className={`timeline-now-marker ${nowX > canvasWidth - 150 ? "label-left" : ""}`} style={{ left: nowX }} aria-label={`Current time: ${nowLabel}`}><div className="timeline-now-beacon" /><div className="timeline-now-label"><strong>Now</strong><time dateTime={new Date(clockNow).toISOString()}>{nowLabel}</time></div></div>}
        {ticks.map((tick) => {
          const x = ((tick - start) / HOUR) * pixelsPerHour;
          const major = new Date(tick).getMinutes() === 0;
          return <div key={tick} className={`timeline-tick ${major ? "major" : ""}`} style={{ left: x }}><span>{formatTick(tick, tickInterval!)}</span></div>;
        })}
        {laidOut.map(({ event, x }, index) => {
          const family = timelineFamily(event);
          const destination = timelineDestination(event);
          const previousDistance = index > 0 ? x - laidOut[index - 1].x : Number.POSITIVE_INFINITY;
          const nextDistance = index < laidOut.length - 1 ? laidOut[index + 1].x - x : Number.POSITIVE_INFINITY;
          const detailLevel = timelineSpatialDetail({ pixelsPerHour, nearestDistance: Math.min(previousDistance, nextDistance) });
          const lane = index % 4;
          const above = lane < 2;
          return <article key={event.id} className={`timeline-marker family-${family} detail-${detailLevel} ${event.temporal_state === "projected" ? "projected" : "actual"} ${above ? "above" : "below"} lane-${lane} ${selected === event.id ? "selected" : ""} ${destination ? "navigable" : ""}`} style={{ left: x }} onClick={() => previewEvent(event)} onDoubleClick={(clickEvent) => { clickEvent.stopPropagation(); focusEvent(event); }} onKeyDown={(keyEvent) => { if (keyEvent.key === "Enter" || keyEvent.key === " ") { keyEvent.preventDefault(); if (keyEvent.shiftKey) focusEvent(event); else previewEvent(event); } }} tabIndex={0} aria-label={`${FAMILY_LABELS[family]}: ${timelineTitle(event)}. ${event.summary || event.type}`}>
            <div className="timeline-marker-stem" /><div className="timeline-marker-dot" />
            {detailLevel !== "dot" && <div className="timeline-marker-card"><div className="marker-meta"><span className="marker-family">{FAMILY_LABELS[family]}</span>{event.occurrence_count && event.occurrence_count > 1 && <span>{event.occurrence_count}×</span>}{detailLevel === "detail" && <time>{new Date(event.timestamp).toLocaleString(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" })}</time>}</div>{(detailLevel === "title" || detailLevel === "detail") && <h3>{timelineTitle(event)}</h3>}{detailLevel === "detail" && <><p>{event.summary}</p><footer>{event.project_id || event.runner_id}<code>{event.type}</code></footer></>}</div>}
            <div className="timeline-marker-popover" role="tooltip"><div><span className="marker-family">{FAMILY_LABELS[family]}</span><time>{new Date(event.timestamp).toLocaleString()}</time></div><strong>{timelineTitle(event)}</strong><p>{event.summary || event.type}</p><small>{event.project_id || event.runner_id || event.source} · {event.type}</small></div>
          </article>;
        })}
        {!focusedRange && <><div className="timeline-edge older">← Earlier history continues</div><div className="timeline-edge newer">Later activity continues →</div></>}
      </div>
    </div>
  </section>;
}
