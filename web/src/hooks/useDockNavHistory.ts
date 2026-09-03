/**
 * Back / Forward for pane navigation.
 *
 * Opening a pane used to be invisible to the browser: the workspace store
 * never touched the URL, so `history.length` stayed put and Back had
 * nothing to pop. Clicking "→ task <id>" in a reminder and pressing Back
 * did literally nothing.
 *
 * WHAT A HISTORY ENTRY HOLDS — an intent, never a layout.
 *
 * The dock trees are already durable in localStorage, so an entry that
 * carried layout would let Back resurrect a pane the user deliberately
 * closed. An entry answers "what was I looking at", not "what was open":
 * `{ view, leaf?: { dock, kind, target } }`. On POP we REVEAL-OR-OPEN —
 * bring the matching pane forward if it still exists, mint it if it does
 * not — and never remove anything. Same semantics as editor history in a
 * tabbed IDE: Back returns your attention, it does not undo your layout.
 *
 * WHY IT DOES NOT FIGHT useEntryNavHistory
 *
 * That hook owns `?entry=` and treats the URL as the intent: on POP, if the
 * popped URL's entry ref differs from the store's selection it REPLACES the
 * selection, and clears it when the URL carries no ref. Dock entries
 * therefore push the CURRENT pathname and search unchanged, differing only
 * in `history.state`. Both of that hook's effects then see "URL and store
 * already agree" and return early, so a Back through a dock entry cannot
 * wipe the reader's selection. No tagging, no coordination, no shared flag.
 *
 * Mounted beside useEntryNavHistory in the Dashboard for the same reason:
 * the back stack outlives any one view.
 */
import { useEffect, useRef } from "react";
import { useLocation, useNavigate, useNavigationType } from "react-router-dom";

import {
  installNavPush,
  withoutNav,
  leafIdentity,
  type NavEntry,
} from "../lib/navBridge";
import { useWorkspace } from "../store/workspace";
import { entryRefFromSearch } from "../lib/entryNav";
import { enclosingTabsId, findNodeInfo, walkLeaves } from "../lib/dock";

/** The slot our intent rides in, inside react-router's own history state. */
interface DockNavState {
  dockNav?: NavEntry;
}

export function useDockNavHistory(): void {
  const navigate = useNavigate();
  const location = useLocation();
  const navigationType = useNavigationType();
  /** Held off until after the first render, so the initial POP is ignored. */
  const ready = useRef(false);

  // Install the store's push implementation.
  useEffect(() => {
    installNavPush((entry: NavEntry) => {
      // Same pathname AND same search: only `state` differs. That is what
      // keeps useEntryNavHistory out of the way (see the module docstring).
      navigate(
        { pathname: window.location.pathname, search: window.location.search },
        { state: { ...(window.history.state?.usr ?? {}), dockNav: entry } },
      );
    });
    return () => installNavPush(null);
  }, [navigate]);

  useEffect(() => {
    // react-router reports the initial render as a POP, which is not a Back
    // press — the same trap useEntryNavHistory documents.
    if (!ready.current) {
      ready.current = true;
      return;
    }
    if (navigationType !== "POP") return;
    const entry = (location.state as DockNavState | null)?.dockNav;
    if (!entry) {
      // A history entry authored by useEntryNavHistory: it records an entry
      // selection but carries no view, and its own POP effect returns early
      // when the URL and the store already agree — which they do, because
      // dock pushes leave `?entry=` untouched. Without this, Back from a
      // pane onto one of those entries left the user staring at the Focus
      // dock while the history said they were reading an entry.
      if (entryRefFromSearch(window.location.search)) {
        withoutNav(() => useWorkspace.getState().setView("entries"));
      }
      return;
    }

    // Everything below mutates the store, which pushes. Suspend, or one
    // Back would append a new entry and Forward would become unreachable.
    withoutNav(() => {
      const ws = useWorkspace.getState();
      if (entry.view && ws.view !== entry.view) ws.setView(entry.view);
      if (!entry.leaf) return;

      const { dock, kind, target, title } = entry.leaf;
      const tree = ws.docks[dock];
      const wanted = leafIdentity(kind, target);

      let foundId: string | null = null;
      if (tree) {
        walkLeaves(tree, (leaf, id) => {
          if (
            foundId === null &&
            leafIdentity(leaf.kind, leaf.target) === wanted
          ) {
            foundId = id;
          }
        });
      }

      if (foundId && tree) {
        // Reveal: bring its tab forward and make it the current pane.
        const tabsId = enclosingTabsId(tree, foundId);
        if (tabsId) {
          const found = findNodeInfo(tree, tabsId);
          const tabs = found?.node;
          if (tabs && tabs.type === "tabs") {
            const idx = tabs.children.findIndex((c) => c.id === foundId);
            if (idx >= 0) {
              if (dock === "focus") ws.setActiveTab(tabsId, idx);
              else ws.setSidebarActiveTab(tabsId, idx);
            }
          }
        }
        if (dock === "focus") ws.setLastFocusLeaf(foundId);
        else ws.setLastSidebarLeaf(foundId);
        // A sidebar pane cannot be "revealed" behind a collapsed column.
        if (dock === "sidebar" && !ws.sidebarDockOpen) {
          ws.toggleSidebarDockOpen();
        }
        return;
      }

      // Gone (closed, or discarded by coerceDockTree). Re-open it rather
      // than leaving Back looking broken — the pane is the destination.
      if (dock === "focus") ws.openInFocus(kind as never, target, title);
      else ws.openOrReuseInSidebar(kind as never, target, title);
    });
  }, [location.key, location.state, navigationType]);
}
