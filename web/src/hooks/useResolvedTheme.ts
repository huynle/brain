/**
 * The theme actually in effect, "dark" or "light".
 *
 * The workspace store holds the *preference*, which may be "system".
 * Dashboard resolves it and stamps `data-theme` on <body>, so reading
 * that attribute back is the one place where "system" has already been
 * decided — no second copy of the matchMedia logic to drift.
 */
import { useEffect, useState } from "react";

function readTheme(): "dark" | "light" {
  if (typeof document === "undefined") return "dark";
  return document.body.getAttribute("data-theme") === "light"
    ? "light"
    : "dark";
}

export function useResolvedTheme(): "dark" | "light" {
  const [theme, setTheme] = useState(readTheme);
  useEffect(() => {
    const sync = () => setTheme(readTheme());
    sync();
    const obs = new MutationObserver(sync);
    obs.observe(document.body, {
      attributes: true,
      attributeFilter: ["data-theme"],
    });
    return () => obs.disconnect();
  }, []);
  return theme;
}
