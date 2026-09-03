/**
 * useFeatureAssignments — feature id → runner id, from the server.
 *
 * One hook so the three surfaces that answer "which runner owns this
 * feature?" — the Tasks tab's feature header, the overview grid and the
 * feature detail pane — cannot drift. They previously read the browser-local
 * optimistic map directly, which meant none of them ever showed an
 * auto-assignment and all three lied after a reload in another browser.
 *
 * `{ poll: false }` is deliberate: react-query schedules `refetchInterval`
 * per OBSERVER over one shared cache entry, so a hook mounted on every
 * project card and every feature row would multiply the runner poll by the
 * number of mounts. This hook only reads the shared snapshot; something else
 * (the sidebar, the runners view) owns the timer, and `runners_update` SSE
 * keeps it live regardless.
 */
import { useEffect, useMemo } from "react";
import { useRunners } from "./useRunners";
import { useWorkspace } from "../store/workspace";
import {
  resolveFeatureAssignments,
  settledAssignments,
} from "../lib/featureAssignments";

export function useFeatureAssignments(): Record<string, string> {
  const { runners } = useRunners({ poll: false });
  const optimistic = useWorkspace((s) => s.featureAssignments);
  const settle = useWorkspace((s) => s.settleFeatureAssignments);

  // Retire overlay entries the server now agrees with. Without this an
  // optimistic value wins for the rest of the session even after the server
  // has moved on — the same "local outlives the truth" bug this replaced,
  // just scoped to one tab.
  useEffect(() => {
    const done = settledAssignments(runners, optimistic);
    if (done.length > 0) settle(done);
  }, [runners, optimistic, settle]);

  return useMemo(
    () => resolveFeatureAssignments(runners, optimistic),
    [runners, optimistic],
  );
}
