/**
 * lib/actions/projectActions — the verb matrix for a project card.
 *
 * ProjectCard used to hand-roll a ContextMenu with three items, which made
 * it the one surface outside the registry: no long-press, no keyboard, no
 * disabled-with-reason. Moving the items here puts the project card on the
 * same rails as tasks and features.
 *
 * A project is still not a CRUD entity, but it does carry two independent
 * pause dials the API exposes and the UI previously left unreachable:
 *
 *   tasks       POST /tasks/runner/pause/{projectId}            | resume
 *   automations POST /tasks/runner/automations/pause/{projectId} | resume
 *
 * They are SEPARATE. Pausing tasks does not pause automation-generated
 * work and vice versa, so they get two independent status-aware pairs
 * rather than one toggle. The third dial in the system — runner-scoped
 * pause — is not a project verb at all and lives in ./runnerActions.
 */
import type { RunnerStatusResponse } from "../types";
import type { ActionDescriptor } from "./types";

export interface ProjectActionContext {
  /** Dispatch every ready feature in the project (POST /tasks/{p}/run). */
  runProject: (projectId: string) => Promise<void>;
  /** Open the project's task list in the focus workspace. */
  openTaskList: (projectId: string) => void;
  /** Hide the card from the overview grid. */
  hideProject: (projectId: string) => void;
  /** POST /tasks/runner/pause/{projectId} — stop new task dispatch. */
  pauseProject: (projectId: string) => Promise<void>;
  /** POST /tasks/runner/resume/{projectId}. */
  resumeProject: (projectId: string) => Promise<void>;
  /** POST /tasks/runner/automations/pause/{projectId}. */
  pauseAutomations: (projectId: string) => Promise<void>;
  /** POST /tasks/runner/automations/resume/{projectId}. */
  resumeAutomations: (projectId: string) => Promise<void>;
}

/**
 * Whether THIS project's task dial is paused.
 *
 * Read only the per-project list. The response's top-level `paused` is
 * derived server-side as `len(pausedProjects) > 0` (service/runner.go
 * GetStatus) — it means "at least one project somewhere is paused", NOT
 * "this project is paused" and NOT "the runner is paused". Folding it in
 * would mark every project on the board paused the moment any single one
 * was. The slice arrives as JSON `null` when empty (Go nil slice), hence
 * the guard.
 */
export function isProjectTasksPaused(
  status: RunnerStatusResponse | undefined,
  projectId: string,
): boolean | undefined {
  if (!status) return undefined;
  return (status.pausedProjects ?? []).includes(projectId);
}

/**
 * Whether THIS project's automation dial is paused. Same rule as above:
 * `automationsPaused` is the any-project rollup, not this project's state.
 */
export function isProjectAutomationsPaused(
  status: RunnerStatusResponse | undefined,
  projectId: string,
): boolean | undefined {
  if (!status) return undefined;
  return (status.automationPausedProjects ?? []).includes(projectId);
}

/**
 * Why a dial cannot be moved to `want`, or "" when it can.
 *
 * `paused === undefined` means the status query has not resolved yet. Like
 * an unknown task count, missing data must not disable a verb — and both
 * endpoints are idempotent, so acting on an unknown state is safe.
 */
export function pauseDialBlockedReason(
  paused: boolean | undefined,
  want: boolean,
  what: string,
): string {
  if (paused === undefined) return "";
  if (paused === want) {
    return want
      ? `${what} are already paused`
      : `${what} are not paused`;
  }
  return "";
}

export function buildProjectActions(
  projectId: string,
  ctx: ProjectActionContext,
  opts: {
    taskCount?: number;
    /** From `isProjectTasksPaused`; undefined while status is loading. */
    tasksPaused?: boolean;
    /** From `isProjectAutomationsPaused`. */
    automationsPaused?: boolean;
  } = {},
): ActionDescriptor[] {
  const { tasksPaused, automationsPaused } = opts;

  return [
    {
      id: "run",
      label: "Run all ready features",
      group: "run",
      key: "x",
      disabledReason:
        opts.taskCount === 0 ? "Project has no tasks" : "",
      run: () => ctx.runProject(projectId),
    },

    // ─── state: the two project dials ───────────────────────────────
    // Labels say "new dispatch" deliberately. Pause does not stop work
    // that is already running — that process runs to completion — and a
    // bare "Pause project" would promise otherwise.
    {
      id: "pause",
      label: "Pause project (stop new dispatch)",
      group: "state",
      key: "p",
      disabledReason: pauseDialBlockedReason(tasksPaused, true, "Tasks"),
      run: () => ctx.pauseProject(projectId),
    },
    {
      id: "resume",
      label: "Resume project",
      group: "state",
      key: "r",
      disabledReason: pauseDialBlockedReason(tasksPaused, false, "Tasks"),
      run: () => ctx.resumeProject(projectId),
    },
    {
      id: "pause-automations",
      label: "Pause project automations",
      group: "state",
      disabledReason: pauseDialBlockedReason(
        automationsPaused,
        true,
        "Automations",
      ),
      run: () => ctx.pauseAutomations(projectId),
    },
    {
      id: "resume-automations",
      label: "Resume project automations",
      group: "state",
      disabledReason: pauseDialBlockedReason(
        automationsPaused,
        false,
        "Automations",
      ),
      run: () => ctx.resumeAutomations(projectId),
    },

    {
      id: "focus-tasks",
      label: "Open task list in focus",
      group: "navigate",
      run: async () => ctx.openTaskList(projectId),
    },
    {
      id: "hide",
      label: "Hide from workspace",
      group: "navigate",
      run: async () => ctx.hideProject(projectId),
    },
  ];
}
