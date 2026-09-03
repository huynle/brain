/**
 * lib/navBridge — the one seam between the workspace store and the router.
 *
 * The store is a zustand store created at module scope, outside React and
 * outside any router context, so it cannot call `useNavigate`. Calling
 * `window.history.pushState` directly is not an option either: react-router
 * owns its own history object and stamps `history.state` with `{idx, key}`,
 * and `useEntryNavHistory` depends on `useNavigationType()` reporting POP
 * correctly — a raw pushState desynchronises both.
 *
 * So the store calls `pushNav`, which does nothing until `useDockNavHistory`
 * installs a real implementation on mount. That keeps every test and every
 * non-React caller working with no router in sight.
 */

/** What a history entry remembers: an INTENT, never a layout. */
export interface NavEntry {
  /** The view that was on screen. */
  view: "overview" | "focus" | "entries" | "session";
  /** The pane that was brought forward, when the navigation opened one. */
  leaf?: {
    dock: "focus" | "sidebar";
    kind: string;
    target: Record<string, unknown>;
    title?: string;
  };
}

type PushFn = (entry: NavEntry) => void;

let pushImpl: PushFn | null = null;
/**
 * Re-entrancy guard. Applying a popped entry calls the very store actions
 * that push, so without this a single Back would push a new entry and the
 * stack would grow forever while Forward became unreachable.
 */
let suspended = 0;

/** Installed once by useDockNavHistory. */
export function installNavPush(fn: PushFn | null): void {
  pushImpl = fn;
}

/** Record a navigation, unless we are in the middle of applying one. */
export function pushNav(entry: NavEntry): void {
  if (suspended > 0) return;
  pushImpl?.(entry);
}

/** Run `fn` without recording anything it does as a navigation. */
export function withoutNav<T>(fn: () => T): T {
  suspended++;
  try {
    return fn();
  } finally {
    suspended--;
  }
}

/**
 * Stable identity for a pane, so a popped intent can find the leaf it
 * refers to.
 *
 * Deliberately NOT a per-kind switch: `DockLeaf.target` is
 * `Record<string, unknown>` across eleven kinds, `coerceDockTree` validates
 * id/kind/title but never `target`, and `defaultLeafTitle` already proves a
 * switch over these kinds silently drifts. A sorted-key stringify with a
 * catch is the shape that cannot rot.
 */
export function leafIdentity(kind: string, target: unknown): string {
  try {
    const t = target as Record<string, unknown> | null | undefined;
    if (!t || typeof t !== "object") return kind + "|";
    const keys = Object.keys(t).sort();
    return (
      kind + "|" + keys.map((k) => k + "=" + JSON.stringify(t[k])).join("&")
    );
  } catch {
    return kind + "|?";
  }
}
