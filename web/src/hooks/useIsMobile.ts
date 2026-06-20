import { useSyncExternalStore } from "react";

// Single source of truth for "are we on a phone-sized screen". Matches the CSS
// breakpoint used across the mobile styles (max-width: 720px). Implemented with
// useSyncExternalStore so it updates on rotation/resize without effects.
const QUERY = "(max-width: 720px)";

function subscribe(cb: () => void): () => void {
  const mql = window.matchMedia(QUERY);
  // matchMedia "change" covers the normal case; window "resize" is a belt-and-
  // suspenders for environments/embeds that don't reliably fire the MQL event.
  mql.addEventListener("change", cb);
  window.addEventListener("resize", cb);
  return () => {
    mql.removeEventListener("change", cb);
    window.removeEventListener("resize", cb);
  };
}

export function useIsMobile(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => window.matchMedia(QUERY).matches,
    () => false, // SSR / first paint default: desktop
  );
}
