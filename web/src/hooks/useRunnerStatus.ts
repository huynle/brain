/**
 * useRunnerStatus — the project-scope pause dials.
 *
 * GET /tasks/runner/status is the only source of truth for the two
 * PROJECT dials (tasks, automations). It does NOT report runner-scoped
 * pause: that is the `paused` field on each row of GET /runners, served
 * by `useRunners`. Reading this endpoint for runner state is the classic
 * mistake here — see `isProjectTasksPaused` for the matching trap on the
 * response's top-level `paused` flag.
 *
 * The dials are mutated by context-menu actions rather than by SSE, so
 * effects invalidate ["v2", "runner-status"] after writing. The poll is a
 * backstop for changes made from another tab, the TUI, or curl.
 */
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { getRunnerStatus } from "../lib/api";
import type { RunnerStatusResponse } from "../lib/types";

export const RUNNER_STATUS_KEY = ["v2", "runner-status"] as const;

export function useRunnerStatus(): {
  status: RunnerStatusResponse | undefined;
  isLoading: boolean;
  error: unknown;
} {
  const q = useQuery({
    queryKey: RUNNER_STATUS_KEY,
    queryFn: getRunnerStatus,
    refetchInterval: 15_000,
    staleTime: 10_000,
  });
  return { status: q.data, isLoading: q.isLoading, error: q.error };
}

/** Invalidator for effects that move a project dial. */
export function useInvalidateRunnerStatus(): () => void {
  const queryClient = useQueryClient();
  return () => {
    void queryClient.invalidateQueries({ queryKey: RUNNER_STATUS_KEY });
  };
}
