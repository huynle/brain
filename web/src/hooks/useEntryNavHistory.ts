/**
 * Two-way sync between the entries reader's selection and the browser's
 * history, so following an entry link is a real navigation.
 *
 *   click a `[link](ab12cd34)` → PUSH `?entry=ab12cd34`
 *   Back                       → the entry you came from
 *   Forward                    → back in again
 *   reload / shared URL        → that entry, in the Entries view
 *
 * Mounted once by the Dashboard rather than by `EntriesBrowser`: the
 * back stack outlives the Entries view, so a Back press from Overview
 * has to be able to bring the view back with the entry it names.
 *
 * Rules that keep the two sides from chasing each other:
 *   • both effects no-op the moment URL and store agree — the state
 *     each of them is driving toward;
 *   • the store is read through `getState()` inside the effects, so a
 *     selection made by an earlier effect in the same commit is seen
 *     rather than the render's stale copy;
 *   • a selection change that did NOT bump `navSeq` is a
 *     canonicalization (short id → full path, see `canonicalizeRef`) and
 *     rewrites the current history entry instead of pushing — otherwise
 *     every link click would cost two Back presses;
 *   • react-router reports the initial render as a POP, which is not a
 *     Back press. `mountSynced` holds the Back/Forward handler off until
 *     the mount-time reconciliation has run, so a persisted selection is
 *     never mistaken for a stale URL and wiped;
 *   • both effects read `window.location` rather than the render's copy
 *     of it. StrictMode replays mount effects with the pre-write
 *     location, and a Back handler acting on that stale URL would undo
 *     the selection the mount-time write had just recorded.
 *
 * Scope: the Entries view's reader pane. Entries opened into a Focus or
 * sidebar dock leaf deliberately stay out of it — those open as new
 * panes and are navigated by the dock's own tabs.
 */
import { useEffect, useRef } from "react";
import { useLocation, useNavigate, useNavigationType } from "react-router-dom";
import { entryRefFromSearch, searchWithEntry } from "../lib/entryNav";
import { useEntriesStore } from "../store/entries";
import { useWorkspace } from "../store/workspace";

export function useEntryNavHistory(): void {
  const location = useLocation();
  const navigate = useNavigate();
  const navigationType = useNavigationType();
  // Subscribed purely to re-run the effects below; the values they act
  // on are read fresh from the store.
  const selectedPath = useEntriesStore((s) => s.selectedPath);
  const navSeq = useEntriesStore((s) => s.navSeq);

  const urlRef = entryRefFromSearch(location.search);
  /** `navSeq` as of the last time URL and store agreed. */
  const syncedSeq = useRef(navSeq);
  /** Set once the mount-time URL/store reconciliation has run. */
  const mountSynced = useRef(false);

  // A URL carrying an entry wins over the persisted selection on the
  // first paint — a shared or reloaded link should show its own entry,
  // in the view that can display it.
  useEffect(() => {
    const initial = entryRefFromSearch(window.location.search);
    if (!initial) return;
    const store = useEntriesStore.getState();
    if (store.selectedPath !== initial) store.applyHistoryEntry(initial);
    syncedSeq.current = useEntriesStore.getState().navSeq;
    useWorkspace.getState().setView("entries");
  }, []);

  // Back / Forward: the URL is the intent, the store follows.
  useEffect(() => {
    if (!mountSynced.current) return;
    if (navigationType !== "POP") return;
    const live = entryRefFromSearch(window.location.search);
    const store = useEntriesStore.getState();
    if (store.selectedPath === live) return;
    store.applyHistoryEntry(live);
    syncedSeq.current = useEntriesStore.getState().navSeq;
    if (live) useWorkspace.getState().setView("entries");
  }, [location.key, navigationType, urlRef]);

  // Selection changed in the app: record it in the history.
  useEffect(() => {
    const firstRun = !mountSynced.current;
    mountSynced.current = true;
    const { selectedPath: current, navSeq: seq } = useEntriesStore.getState();
    const liveSearch = window.location.search;
    if (current === entryRefFromSearch(liveSearch)) {
      syncedSeq.current = seq;
      return;
    }
    const search = searchWithEntry(liveSearch, current);
    // Belt and braces: a write that wouldn't change the URL can't be
    // waited on, so leave the disagreement rather than spin on it.
    if (search === liveSearch) return;
    // The mount-time write only corrects the URL to match a restored
    // selection; a later one that bumped navSeq is a real navigation.
    const replace = firstRun || seq === syncedSeq.current;
    syncedSeq.current = seq;
    navigate({ pathname: window.location.pathname, search }, { replace });
  }, [
    selectedPath,
    navSeq,
    urlRef,
    navigate,
    location.pathname,
    location.search,
  ]);
}
