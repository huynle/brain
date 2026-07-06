// Shared pause/resume actions with optimistic runner-status updates: the
// StatusBar chips, global p/P/b/B keys, and the :pause/:resume commands all
// route through here so the UI flips instantly and rolls back on failure.

import type { QueryClient } from "@tanstack/react-query";
import {
  pauseAll,
  pauseAutomations,
  pauseProject,
  resumeAll,
  resumeAutomations,
  resumeProject,
} from "./api";
import type { RunnerStatusResponse } from "./types";

export interface PauseTarget {
  kind: "tasks" | "autos";
  /** undefined = all projects (global switch). */
  project?: string;
  pause: boolean;
}

function optimistic(data: RunnerStatusResponse, t: PauseTarget): RunnerStatusResponse {
  const next = { ...data };
  if (t.kind === "tasks") {
    if (!t.project) next.paused = t.pause;
    else {
      const list = new Set(next.pausedProjects ?? []);
      t.pause ? list.add(t.project) : list.delete(t.project);
      next.pausedProjects = [...list];
    }
  } else {
    if (!t.project) next.automationsPaused = t.pause;
    else {
      const list = new Set(next.automationPausedProjects ?? []);
      t.pause ? list.add(t.project) : list.delete(t.project);
      next.automationPausedProjects = [...list];
    }
  }
  return next;
}

function call(t: PauseTarget): Promise<unknown> {
  if (t.kind === "autos") {
    return t.pause ? pauseAutomations(t.project) : resumeAutomations(t.project);
  }
  if (!t.project) return t.pause ? pauseAll() : resumeAll();
  return t.pause ? pauseProject(t.project) : resumeProject(t.project);
}

export function pauseLabel(t: PauseTarget): string {
  const verb = t.pause ? "Paused" : "Resumed";
  const what = t.kind === "autos" ? "automations" : "tasks";
  return `${verb} ${what}${t.project ? ` — ${t.project}` : " (all)"}`;
}

/**
 * applyPause flips the ["runner-status"] cache immediately, fires the API
 * call, and rolls the cache back (plus refetches) on failure.
 */
export async function applyPause(
  qc: QueryClient,
  t: PauseTarget,
  toast: (msg: string, kind?: "info" | "success" | "error") => void,
): Promise<void> {
  const key = ["runner-status"];
  const prev = qc.getQueryData<RunnerStatusResponse>(key);
  if (prev) qc.setQueryData(key, optimistic(prev, t));
  try {
    await call(t);
    toast(pauseLabel(t), "success");
    void qc.invalidateQueries({ queryKey: key });
  } catch (e) {
    if (prev) qc.setQueryData(key, prev);
    void qc.invalidateQueries({ queryKey: key });
    toast(e instanceof Error ? e.message : "Pause action failed", "error");
  }
}
