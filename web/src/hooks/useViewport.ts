import { useSyncExternalStore } from "react";

// Layout breakpoints used across the app. Mobile keeps the bottom sheet + FAB;
// narrow viewports treat the assistant as an overlay drawer; wide viewports
// render it as a persistent right sidebar.
//
// Tiers:
//   mobile  — width <= 720px            (touch UX, single column)
//   narrow  — 721px .. 1099px            (desktop, but too tight for a 3-pane layout)
//   wide    — width >= 1100px            (room for content + persistent sidebar)
const MOBILE_QUERY = "(max-width: 720px)";
const NARROW_QUERY = "(max-width: 1099px)";

export type ViewportTier = "mobile" | "narrow" | "wide";

function subscribe(cb: () => void): () => void {
  // Subscribing to "resize" alone misses some embed scenarios; combining it
  // with the matchMedia "change" events covers both the breakpoint crossings
  // and the continuous resize-during-drag case (we read width directly).
  const mqlMobile = window.matchMedia(MOBILE_QUERY);
  const mqlNarrow = window.matchMedia(NARROW_QUERY);
  mqlMobile.addEventListener("change", cb);
  mqlNarrow.addEventListener("change", cb);
  window.addEventListener("resize", cb);
  return () => {
    mqlMobile.removeEventListener("change", cb);
    mqlNarrow.removeEventListener("change", cb);
    window.removeEventListener("resize", cb);
  };
}

function readTier(): ViewportTier {
  if (window.matchMedia(MOBILE_QUERY).matches) return "mobile";
  if (window.matchMedia(NARROW_QUERY).matches) return "narrow";
  return "wide";
}

// useViewport returns the current viewport tier and updates on resize. Used to
// decide when the assistant is a sidebar vs an overlay vs a bottom sheet.
export function useViewport(): ViewportTier {
  return useSyncExternalStore(
    subscribe,
    readTier,
    () => "wide", // SSR / first paint default
  );
}
