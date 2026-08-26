/**
 * Invalidator for the project-scope pause dials.
 *
 * The polling hook that used to live here was folded into
 * `usePauseState`/`usePauseSync` during the #35 + #37 merge. That matters
 * for more than tidiness: this hook set `refetchInterval` on every
 * observer, and react-query schedules that per observer rather than per
 * query — with the statusbar, the sidebar and one mount per project card
 * all reading pause state, the effective poll rate collapsed to seconds.
 * `usePauseSync` now owns the single timer and every reader shares the
 * cache entry.
 *
 * What remains is the write-side invalidator: the dials are moved by
 * context-menu actions rather than by SSE, so an effect that flips one
 * must invalidate the shared key to avoid showing a stale dial until the
 * next poll.
 */
import { useQueryClient } from "@tanstack/react-query";

/** Shared with usePauseState — both must name the same cache entry. */
export const RUNNER_STATUS_KEY = ["v2", "runner-status"] as const;

/** Invalidator for effects that move a project dial. */
export function useInvalidateRunnerStatus(): () => void {
  const queryClient = useQueryClient();
  return () => {
    void queryClient.invalidateQueries({ queryKey: RUNNER_STATUS_KEY });
  };
}
