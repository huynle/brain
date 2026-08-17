/**
 * lib/actions/projectActions — the (small) verb matrix for a project card.
 *
 * ProjectCard used to hand-roll a ContextMenu with three items, which made
 * it the one surface outside the registry: no long-press, no keyboard, no
 * disabled-with-reason. Moving the items here puts the project card on the
 * same rails as tasks and features.
 *
 * Deliberately thin — a project is not an entity the API mutates, so the
 * verbs are "run everything ready", navigation, and visibility.
 */
import type { ActionDescriptor } from "./types";

export interface ProjectActionContext {
  /** Dispatch every ready feature in the project (POST /tasks/{p}/run). */
  runProject: (projectId: string) => Promise<void>;
  /** Open the project's task list in the focus workspace. */
  openTaskList: (projectId: string) => void;
  /** Hide the card from the overview grid. */
  hideProject: (projectId: string) => void;
}

export function buildProjectActions(
  projectId: string,
  ctx: ProjectActionContext,
  opts: { taskCount?: number } = {},
): ActionDescriptor[] {
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
