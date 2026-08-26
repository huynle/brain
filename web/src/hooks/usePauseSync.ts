/**
 * usePauseSync — the ONE place that polls for pause + scheduler state.
 *
 * react-query schedules `refetchInterval` per observer, not per query, while
 * all observers share a single cache entry. The pause indicators are mounted
 * all over the tree — statusbar, sidebar project rows, every project card,
 * every card body, the task modal, the drawer — so giving each of them an
 * interval multiplied the request rate by the number of mounted components
 * (measured: a request every 3s instead of every 12s, with ONE project on
 * screen, and worse with each project added).
 *
 * Dashboard calls this once. Everything else uses `usePauseState()` /
 * `useSchedulerStatus()`, which read the same cache entries with no timer
 * and still re-render the moment a poll lands.
 */
import { usePauseSync as usePauseDialSync } from "./usePauseState";
import { useSchedulerSync } from "./useSchedulerStatus";

export function usePauseSync(): void {
  usePauseDialSync();
  useSchedulerSync();
}
