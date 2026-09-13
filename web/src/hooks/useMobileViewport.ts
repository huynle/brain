import { useEffect } from "react";

// Mobile keyboards resize the visual viewport on Safari without necessarily
// resizing the layout viewport. Keep dialogs/composers above the keyboard.
export function useMobileViewport(enabled: boolean) {
  useEffect(() => {
    if (!enabled) return;
    const viewport = window.visualViewport;
    const root = document.documentElement;
    const update = () => {
      // Pinch zoom must magnify the page rather than reflow it under the user.
      if (viewport && viewport.scale !== 1) return;
      root.style.setProperty(
        "--mobile-viewport-height",
        `${viewport?.height ?? innerHeight}px`,
      );
      root.style.setProperty(
        "--mobile-viewport-top",
        `${viewport?.offsetTop ?? 0}px`,
      );
    };
    update();
    viewport?.addEventListener("resize", update);
    viewport?.addEventListener("scroll", update);
    window.addEventListener("resize", update);
    return () => {
      viewport?.removeEventListener("resize", update);
      viewport?.removeEventListener("scroll", update);
      window.removeEventListener("resize", update);
      root.style.removeProperty("--mobile-viewport-height");
      root.style.removeProperty("--mobile-viewport-top");
    };
  }, [enabled]);
}
