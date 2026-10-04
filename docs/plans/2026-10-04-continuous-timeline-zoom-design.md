# Continuous Timeline Zoom Design

## Objective

Replace the Timeline's five fixed zoom levels with smooth, effectively unbounded zoom down to second-level detail. Zoom must remain anchored beneath the mouse pointer, and entering Fit must never trap or disable later zooming.

## Interaction Design

Use a continuous numeric scale ranging from the fitted or broad minimum up to 3,600 pixels per hour, equivalent to one pixel per second. The zoom control displays useful temporal resolution rather than fixed labels such as `1×` through `5×`.

Wheel zoom preserves the timestamp beneath the pointer:

1. Resolve the timeline timestamp at `scrollLeft + pointerX`.
2. Apply an exponential multiplier derived from the wheel delta.
3. Recompute `scrollLeft` so the timestamp returns to the same pointer coordinate.

The `+` and `−` buttons use the viewport center as their anchor because they have no pointer position.

Fit is a command and display state, not a separate zoom mode. Focus Events calculates a fitted range and scale. The next zoom in or out immediately exits Fit while preserving the center timestamp. Both controls remain available unless the true scale minimum or second-level maximum is reached.

## Implementation

`TimelinePage` will hold one authoritative numeric pixels-per-hour scale instead of a zoom index plus an independent fit scale. A lightweight Fit display flag may indicate that Focus Events supplied the current scale, but it must not govern whether zoom is allowed.

Pure helpers in `web/src/lib/timeline.ts` will provide:

- Exponential scale calculation from wheel delta
- Scale clamping
- Pointer- and center-anchored scroll correction
- Human-readable temporal resolution labels
- Safe canvas-range calculations

Wheel input uses the complete delta so trackpads zoom smoothly while mouse wheels remain predictable. Every manual zoom operation clears the Fit display state. Zooming out does not implicitly invoke Focus Events or mutate the selected time filter.

The existing dynamic timeline extension remains available at ordinary scales. At very high scales, the represented range contracts around the current anchor rather than creating a canvas beyond browser layout limits. This preserves the anchored timestamp while supporting second-level detail.

## Verification

Tests will prove:

- Repeated zoom-in reaches second-level resolution without a five-step ceiling.
- Different pointer positions preserve the timestamp beneath the pointer.
- Wheel deltas produce smooth exponential scaling.
- Button zoom remains centered in the viewport.
- Zooming in and out works immediately after Fit.
- Fit remains available as an explicit command.
- Scale and canvas dimensions remain bounded safely.
- Resolution labels cover day, hour, minute, and second scales.
- Existing pan, Today, event focus, and spatial-detail behavior remains intact.
