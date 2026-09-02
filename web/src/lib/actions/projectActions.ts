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
import type { ActionDescriptor } from "./types";
import type { DeleteProjectResponse } from "../types";

export interface ProjectActionContext {
  /** Dispatch every ready feature in the project (POST /tasks/{p}/run). */
  runProject: (projectId: string) => Promise<void>;
  /** Dock the whole project card in the focus workspace. */
  openProject: (projectId: string) => void;
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
  /** DELETE /tasks/{projectId} — erase every entry in the project. */
  deleteProject: (projectId: string) => Promise<void>;
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
// Pause predicates live in lib/pause.ts, which models all THREE dials
// (project tasks, project automations, runner) from both sources. This
// module previously carried its own project-only copies reading
// the runner-status response directly; they were folded into the richer model
// during the #35 + #37 merge so the controls and the badges can never
// disagree about whether a project is paused.
export { isProjectTasksPaused, isProjectAutomationsPaused } from "../pause";

/**
 * Why a dial cannot be moved to `want`, or "" when it can.
 *
 * `paused === undefined` means the pause state has not resolved yet. Like
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
    return want ? `${what} are already paused` : `${what} are not paused`;
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
      disabledReason: opts.taskCount === 0 ? "Project has no tasks" : "",
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
      id: "focus-project",
      label: "Open project in focus",
      group: "navigate",
      run: async () => ctx.openProject(projectId),
    },
    {
      id: "hide",
      label: "Hide from workspace",
      group: "navigate",
      run: async () => ctx.hideProject(projectId),
    },

    // ─── danger ─────────────────────────────────────────────────────
    // The only verb here that destroys anything. Hide (above) is its
    // reversible neighbour and the reason the label spells out
    // "permanently": the two are one right-click apart, and a user who
    // wanted the × has to be stopped from landing here by accident.
    //
    // Never disabled on an empty project. An empty-looking project still
    // has a directory, pause dials and possibly non-task entries the
    // sidebar's task counts never showed — "nothing to delete" would be a
    // lie, and the verb is how you get rid of the leftover name.
    {
      id: "delete",
      label: "Delete project…",
      group: "danger",
      danger: true,
      // No accelerator, deliberately. Every other single-key verb here is
      // recoverable; a bare "d" that starts erasing a project is not the
      // kind of thing to have under a finger resting on a row.
      confirm: {
        title: `Delete project ${projectId}?`,
        body:
          `This permanently removes every brain entry in ${projectId} — tasks, notes, ` +
          `automations and goals alike — along with its history and its runner state. ` +
          `It cannot be undone. To take the project off your board without destroying it, ` +
          `hide it instead.`,
        // Type-to-confirm, as on feature deletion: irreversible, and the
        // blast radius is the whole project rather than one entry.
        typeToConfirm: projectId,
        confirmLabel: "Delete permanently",
      },
      run: () => ctx.deleteProject(projectId),
    },
  ];
}

/**
 * One-line toast summary of a project wipe.
 *
 * Lives here rather than beside the fetch wrapper for the same reason the
 * builders do: it is pure data → string, and `node --test` can reach it
 * without dragging in auth and the browser globals api.ts needs.
 *
 * A partial wipe leads with the failure. "Deleted 40 entries" is true and
 * useless when 3 are still there — the leftovers are the only part the user
 * has to act on, so they go first, with the server's own first reason
 * attached rather than a generic "some failed".
 */
export function summarizeDeleteProjectResult(r: DeleteProjectResponse): string {
  const entries = `${r.deleted} ${r.deleted === 1 ? "entry" : "entries"}`;
  if (r.failed > 0) {
    const first = r.errors?.[0] ? ` (${r.errors[0]})` : "";
    return `${r.project}: ${r.failed} failed, ${entries} deleted${first}`;
  }
  // An empty project is a real case, not an error: the sidebar counts only
  // tasks, so "0 entries" here means the name was all that was left.
  if (r.deleted === 0) return `${r.project} deleted — it had no entries`;
  return `${r.project} deleted — ${entries} removed`;
}
