/**
 * ProjectTiles — the project cards, tiled rather than gridded.
 *
 * `.overview` is a plain `repeat(auto-fill, minmax(420px, 1fr))` grid, so
 * every card in a row is as tall as its row. A project with two tasks
 * sitting beside one with four hundred left a column of dead space the
 * height of the taller card — and with the cards now foldable, that gap
 * grows every time someone collapses a feature.
 *
 * ─── How it tiles ────────────────────────────────────────────────
 *
 * The masonry trick every browser supports today: make the implicit rows
 * tiny (`--tile-row`), then give each card a `grid-row-end: span N` sized
 * to its own measured height. A short card spans few rows and the next
 * card in that column starts right underneath it.
 *
 * `grid-template-rows: masonry` would replace all of this, and CSS grid
 * level 3 is not shipped anywhere stable yet.
 *
 * The vertical gap is baked into each span rather than set as `row-gap`,
 * because a row gap between LATTICE rows would multiply by the dozens of
 * rows a card spans. Columns keep a real `column-gap`.
 *
 * Reading order stays left-to-right. CSS multi-column would tile with no
 * JavaScript at all, but it fills top-to-bottom per column, so the second
 * project in the sidebar would land halfway down the page.
 *
 * Without ResizeObserver the spans are never written and the layout
 * degrades to exactly the grid this replaces — no dead cards, just the
 * dead space back.
 */
import { useEffect, useRef } from "react";

import { ProjectCard } from "./ProjectCard";

/** Height of one implicit grid row, in px. Smaller = tighter packing and
 *  more rows to span; 4px keeps the arithmetic cheap and the slack under
 *  one text line. */
const TILE_ROW = 4;
/** Vertical space between stacked cards, folded into every span. */
const TILE_GAP = 12;

function spanFor(height: number): number {
  return Math.max(1, Math.ceil((height + TILE_GAP) / TILE_ROW));
}

export interface ProjectTilesProps {
  projectIds: readonly string[];
}

export function ProjectTiles({ projectIds }: ProjectTilesProps): JSX.Element {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const root = ref.current;
    if (!root) return;
    if (typeof ResizeObserver === "undefined") return;

    const measure = (card: Element) => {
      if (!(card instanceof HTMLElement)) return;
      const next = `span ${spanFor(card.getBoundingClientRect().height)}`;
      // Only write when it CHANGES. This callback runs on a resize and
      // sets a property that affects layout, which is the shape of a
      // ResizeObserver feedback loop ("loop completed with undelivered
      // notifications"). Writing an identical value does not dirty
      // layout, so the guard is what makes the cycle terminate rather
      // than merely usually terminating.
      if (card.style.gridRowEnd !== next) card.style.gridRowEnd = next;
    };

    // A card resizes for reasons React never re-renders for — a feature
    // folded, an SSE task list growing, the window narrowing into a
    // different column count — so the measurement is driven by the
    // browser's own layout, not by a render.
    const ro = new ResizeObserver((entries) => {
      for (const e of entries) {
        // The container itself changing width re-flows every column.
        if (e.target === root) for (const c of root.children) measure(c);
        else measure(e.target);
      }
    });

    const watch = () => {
      for (const c of root.children) {
        measure(c);
        ro.observe(c); // observing twice is a no-op
      }
    };

    // Cards come and go as projects are hidden, deleted or filtered.
    const mo = new MutationObserver(watch);
    mo.observe(root, { childList: true });
    ro.observe(root);
    watch();

    return () => {
      ro.disconnect();
      mo.disconnect();
    };
  }, []);

  return (
    <div className="project-tiles" ref={ref}>
      {projectIds.map((pid) => (
        <ProjectCard key={pid} projectId={pid} />
      ))}
    </div>
  );
}
