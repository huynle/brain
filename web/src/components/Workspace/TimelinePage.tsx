import { useEffect, useMemo, useRef, useState } from "react";
import { useDeferredPreview } from "../../hooks/useDeferredPreview";
import { useProjects } from "../../hooks/useProjects";
import { useVisibleProjects } from "../../hooks/useVisibleProjects";
import { anchoredZoomScrollLeft, dragScrollLeft, filterTimelineByRange, filterTimelineEvents, focusedTimelineRange, horizontalTimelineLayout, TIMELINE_FAMILIES, timelineDestination, timelineFamily, timelineSpatialDetail, timelineTicks, timelineTitle, type TimelineEvent, type TimelineFamily, type TimelineRangeFilter } from "../../lib/timeline";
import { seededTimelineSource } from "../../lib/timelineSeed";
import { useWorkspace } from "../../store/workspace";

const FAMILY_LABELS: Record<TimelineFamily, string> = { feature: "Features", task: "Tasks", entry: "Entries", session: "Sessions", runner: "Runners", project: "Projects", other: "Other" };
const ZOOM_LEVELS = [24, 48, 84, 132, 210];
const HOUR = 60 * 60 * 1000;
const INITIAL_WINDOW_HOURS = 24 * 24;
const EXTENSION_HOURS = 14 * 24;

function formatTick(timestamp: number, major: boolean): string {
  return new Intl.DateTimeFormat(undefined, major ? { month: "short", day: "numeric" } : { hour: "numeric" }).format(new Date(timestamp));
}

export function TimelinePage(): JSX.Element {
  const { data: projects } = useProjects();
  const { visible } = useVisibleProjects();
  const [projectScope, setProjectScope] = useState("all");
  const [families, setFamilies] = useState<Set<string>>(() => new Set(TIMELINE_FAMILIES));
  const [selected, setSelected] = useState<string | null>(null);
  const [zoomIndex, setZoomIndex] = useState(2);
  const [fitScale, setFitScale] = useState<number | null>(null);
  const [range, setRange] = useState({ before: INITIAL_WINDOW_HOURS / 2, after: INITIAL_WINDOW_HOURS / 2 });
  const [timeFilter, setTimeFilter] = useState<TimelineRangeFilter>({ preset: "all" });
  const [focusedRange, setFocusedRange] = useState<{ start: number; end: number } | null>(null);
  const scrollerRef = useRef<HTMLDivElement>(null);
  const extendingRef = useRef(false);
  const dragRef = useRef<{ pointerId: number; x: number; scrollLeft: number; moved: boolean } | null>(null);
  const suppressClickRef = useRef(false);
  const zoomIndexRef = useRef(zoomIndex);
  const preview = useDeferredPreview();
  const openSidebar = useWorkspace((state) => state.openOrReuseInSidebar);
  const openFocus = useWorkspace((state) => state.openInFocus);

  const projectFilter = useMemo(() => projectScope === "all" ? null : new Set(projectScope === "sidebar" ? visible : [projectScope]), [projectScope, visible]);
  const scopedEvents = useMemo(() => filterTimelineEvents(seededTimelineSource.events, { projects: projectFilter, families }), [families, projectFilter]);
  const events = useMemo(() => filterTimelineByRange(scopedEvents, timeFilter), [scopedEvents, timeFilter]);
  const center = Math.max(...seededTimelineSource.events.map((event) => Date.parse(event.timestamp)));
  const start = focusedRange?.start ?? center - range.before * HOUR;
  const end = focusedRange?.end ?? center + range.after * HOUR;
  const pixelsPerHour = fitScale ?? ZOOM_LEVELS[zoomIndex];
  const canvasWidth = ((end - start) / HOUR) * pixelsPerHour;
  const laidOut = useMemo(() => horizontalTimelineLayout(events, { start, pixelsPerHour }), [events, start, pixelsPerHour]);
  const tickInterval = pixelsPerHour >= 130 ? 2 : pixelsPerHour >= 80 ? 4 : pixelsPerHour >= 45 ? 8 : 24;
  const ticks = useMemo(() => timelineTicks({ start, end, intervalHours: tickInterval }), [start, end, tickInterval]);

  useEffect(() => {
    const scroller = scrollerRef.current;
    if (!scroller) return;
    const centerX = ((center - start) / HOUR) * pixelsPerHour;
    scroller.scrollLeft = centerX - scroller.clientWidth / 2;
  }, []);

  const changeZoom = (next: number) => {
    const scroller = scrollerRef.current;
    if (!scroller || next < 0 || next >= ZOOM_LEVELS.length) return;
    const oldScale = pixelsPerHour;
    const anchorHour = (scroller.scrollLeft + scroller.clientWidth / 2) / oldScale;
    setZoomIndex(next);
    zoomIndexRef.current = next;
    setFitScale(null);
    requestAnimationFrame(() => {
      scroller.scrollLeft = anchorHour * ZOOM_LEVELS[next] - scroller.clientWidth / 2;
    });
  };

  const zoomAt = (next: number, pointerX: number) => {
    const scroller = scrollerRef.current;
    if (!scroller || next < 0 || next >= ZOOM_LEVELS.length) return;
    const nextScroll = anchoredZoomScrollLeft({ scrollLeft: scroller.scrollLeft, pointerX, oldScale: pixelsPerHour, newScale: ZOOM_LEVELS[next] });
    setZoomIndex(next);
    zoomIndexRef.current = next;
    setFitScale(null);
    requestAnimationFrame(() => { scroller.scrollLeft = nextScroll; });
  };

  const extendAtEdge = () => {
    const scroller = scrollerRef.current;
    if (!scroller || extendingRef.current || focusedRange) return;
    if (scroller.scrollLeft < 600) {
      extendingRef.current = true;
      setRange((current) => ({ ...current, before: current.before + EXTENSION_HOURS }));
      requestAnimationFrame(() => {
        scroller.scrollLeft += EXTENSION_HOURS * pixelsPerHour;
        extendingRef.current = false;
      });
    } else if (scroller.scrollWidth - scroller.clientWidth - scroller.scrollLeft < 600) {
      extendingRef.current = true;
      setRange((current) => ({ ...current, after: current.after + EXTENSION_HOURS }));
      requestAnimationFrame(() => { extendingRef.current = false; });
    }
  };

  const focusEvents = () => {
    const scroller = scrollerRef.current;
    const bounds = focusedTimelineRange(events, 2);
    if (!scroller || !bounds) return;
    const spanHours = Math.max(1, (bounds.end - bounds.start) / HOUR);
    const targetScale = Math.max(2, Math.min(210, (scroller.clientWidth - 80) / spanHours));
    let nextZoom = 0;
    for (let index = 1; index < ZOOM_LEVELS.length; index += 1) {
      if (Math.abs(ZOOM_LEVELS[index] - targetScale) < Math.abs(ZOOM_LEVELS[nextZoom] - targetScale)) nextZoom = index;
    }
    setFocusedRange(bounds);
    setZoomIndex(nextZoom);
    setFitScale(targetScale);
    requestAnimationFrame(() => { scroller.scrollLeft = 0; });
  };

  const stepZoom = (direction: -1 | 1) => {
    if (fitScale === null) {
      changeZoom(zoomIndex + direction);
      return;
    }
    const next = direction > 0
      ? ZOOM_LEVELS.findIndex((scale) => scale > fitScale)
      : ZOOM_LEVELS.findLastIndex((scale) => scale < fitScale);
    if (next >= 0) changeZoom(next);
  };

  const toggleFamily = (family: TimelineFamily) => setFamilies((current) => {
    setFocusedRange(null);
    setFitScale(null);
    const next = new Set(current);
    if (next.has(family)) next.delete(family); else next.add(family);
    return next;
  });
  const previewEvent = (event: TimelineEvent) => {
    setSelected(event.id);
    if (seededTimelineSource.mode === "seeded") return;
    const destination = timelineDestination(event);
    if (destination) preview.schedule(() => openSidebar(destination.kind, destination.target, destination.title));
  };
  const focusEvent = (event: TimelineEvent) => {
    if (seededTimelineSource.mode === "seeded") return;
    const destination = timelineDestination(event);
    if (!destination) return;
    preview.cancel();
    openFocus(destination.kind, destination.target, destination.title);
  };

  return <section className="timeline-page timeline-horizontal">
    <header className="timeline-hero">
      <div><div className="timeline-eyebrow">Activity observatory</div><h1>Timeline</h1><p>Pan through project history. Zoom from days to hours and identify event families by color.</p></div>
      <div className="timeline-seed-badge"><span /> Seeded wireframe</div>
    </header>
    <div className="timeline-controls horizontal-controls">
      <label><span>Project scope</span><select aria-label="Project scope" value={projectScope} onChange={(event) => { setFocusedRange(null); setFitScale(null); setProjectScope(event.target.value); }}><option value="all">All projects</option><option value="sidebar">Sidebar projects</option>{(projects || []).map((project) => <option key={project} value={project}>{project}</option>)}</select></label>
      <label className="timeline-range-filter"><span>Time range</span><select aria-label="Time range" value={timeFilter.preset} onChange={(event) => { setFocusedRange(null); setFitScale(null); setTimeFilter({ preset: event.target.value as TimelineRangeFilter["preset"] }); }}><option value="all">All time</option><option value="24h">Last 24 hours</option><option value="7d">Last 7 days</option><option value="30d">Last 30 days</option><option value="custom">Custom range</option></select></label>
      <div className="timeline-family-filter timeline-legend" aria-label="Event families">{TIMELINE_FAMILIES.map((family) => <button key={family} className={`family-${family} ${families.has(family) ? "active" : ""}`} aria-pressed={families.has(family)} onClick={() => toggleFamily(family)}><span />{FAMILY_LABELS[family]}</button>)}</div>
      <button className="timeline-focus-button" onClick={focusEvents}>Focus events</button>
      <div className="timeline-zoom"><button onClick={() => stepZoom(-1)} disabled={fitScale === null ? zoomIndex === 0 : fitScale <= ZOOM_LEVELS[0]} aria-label="Zoom out">−</button><span>{fitScale === null ? `${zoomIndex + 1}×` : "Fit"}</span><button onClick={() => stepZoom(1)} disabled={fitScale === null ? zoomIndex === ZOOM_LEVELS.length - 1 : fitScale >= ZOOM_LEVELS.at(-1)!} aria-label="Zoom in">+</button></div>
    </div>
    {timeFilter.preset === "custom" && <div className="timeline-custom-range"><label>From <input type="date" value={timeFilter.start || ""} onChange={(event) => setTimeFilter((current) => ({ ...current, start: event.target.value }))} /></label><label>To <input type="date" value={timeFilter.end || ""} onChange={(event) => setTimeFilter((current) => ({ ...current, end: event.target.value }))} /></label></div>}
    <div className="timeline-summary"><b>{events.length} events in view</b><span>Drag or wheel to travel · Shift + wheel to zoom · Hover dots for details</span></div>
    <div
      className="timeline-scroll"
      ref={scrollerRef}
      onClickCapture={(event) => {
        if (!suppressClickRef.current) return;
        suppressClickRef.current = false;
        event.preventDefault();
        event.stopPropagation();
      }}
      onDoubleClick={(event) => { if (!(event.target as Element).closest(".timeline-marker")) focusEvents(); }}
      onScroll={extendAtEdge}
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
      onWheel={(event) => {
        if (event.shiftKey) {
          event.preventDefault();
          const bounds = event.currentTarget.getBoundingClientRect();
          const delta = Math.abs(event.deltaY) >= Math.abs(event.deltaX) ? event.deltaY : event.deltaX;
          if (delta !== 0) zoomAt(zoomIndexRef.current + (delta < 0 ? 1 : -1), event.clientX - bounds.left);
          return;
        }
        if (Math.abs(event.deltaY) > Math.abs(event.deltaX)) {
          event.currentTarget.scrollLeft += event.deltaY;
          event.preventDefault();
        }
      }}
    >
      <div className="timeline-canvas" style={{ width: canvasWidth }}>
        <div className="timeline-axis" />
        {ticks.map((tick) => {
          const x = ((tick - start) / HOUR) * pixelsPerHour;
          const major = new Date(tick).getHours() === 0;
          return <div key={tick} className={`timeline-tick ${major ? "major" : ""}`} style={{ left: x }}><span>{formatTick(tick, major)}</span></div>;
        })}
        {laidOut.map(({ event, x }, index) => {
          const family = timelineFamily(event);
          const destination = timelineDestination(event);
          const previousDistance = index > 0 ? x - laidOut[index - 1].x : Number.POSITIVE_INFINITY;
          const nextDistance = index < laidOut.length - 1 ? laidOut[index + 1].x - x : Number.POSITIVE_INFINITY;
          const detailLevel = timelineSpatialDetail({ pixelsPerHour, nearestDistance: Math.min(previousDistance, nextDistance) });
          const lane = index % 4;
          const above = lane < 2;
          return <article key={event.id} className={`timeline-marker family-${family} detail-${detailLevel} ${above ? "above" : "below"} lane-${lane} ${selected === event.id ? "selected" : ""} ${destination && seededTimelineSource.mode === "live" ? "navigable" : ""}`} style={{ left: x }} onClick={() => previewEvent(event)} onDoubleClick={(clickEvent) => { clickEvent.stopPropagation(); focusEvent(event); }} onKeyDown={(keyEvent) => { if (keyEvent.key === "Enter" || keyEvent.key === " ") { keyEvent.preventDefault(); if (keyEvent.shiftKey) focusEvent(event); else previewEvent(event); } }} tabIndex={0} aria-label={`${FAMILY_LABELS[family]}: ${timelineTitle(event)}. ${event.summary || event.type}`}>
            <div className="timeline-marker-stem" /><div className="timeline-marker-dot" />
            {detailLevel !== "dot" && <div className="timeline-marker-card"><div className="marker-meta"><span className="marker-family">{FAMILY_LABELS[family]}</span>{detailLevel === "detail" && <time>{new Date(event.timestamp).toLocaleString(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" })}</time>}</div>{(detailLevel === "title" || detailLevel === "detail") && <h3>{timelineTitle(event)}</h3>}{detailLevel === "detail" && <><p>{event.summary}</p><footer>{event.project_id || event.runner_id}<code>{event.type}</code></footer></>}</div>}
            <div className="timeline-marker-popover" role="tooltip"><div><span className="marker-family">{FAMILY_LABELS[family]}</span><time>{new Date(event.timestamp).toLocaleString()}</time></div><strong>{timelineTitle(event)}</strong><p>{event.summary || event.type}</p><small>{event.project_id || event.runner_id || event.source} · {event.type}</small></div>
          </article>;
        })}
        {!focusedRange && <><div className="timeline-edge older">← Earlier history continues</div><div className="timeline-edge newer">Later activity continues →</div></>}
      </div>
    </div>
  </section>;
}
