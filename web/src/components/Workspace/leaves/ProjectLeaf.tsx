/**
 * ProjectLeaf — a whole project, docked.
 *
 * "Open this project in Focus" was already a promised verb in two places
 * — the project row's `Open task list in focus` and the card's ⤢ button —
 * and both opened a `task-detail` leaf with a projectId and no taskId,
 * which resolves no task and renders `Task "" not found in project "x"`.
 * There was no leaf kind that could show a project at all.
 *
 * This renders the same `ProjectCard` the overview grid does, so the
 * docked copy has the identical tabs, dial, verbs and folds rather than a
 * second, drifting rendering of the same thing. `fill` drops the card's
 * overview-grid height cap: in a pane the pane decides the height.
 */
import { ProjectCard } from "../ProjectCard";
import { ErrorState } from "../../common/ErrorState";

export interface ProjectLeafProps {
  target: Record<string, unknown>;
}

export function ProjectLeaf({ target }: ProjectLeafProps): JSX.Element {
  const projectId = (target.projectId as string | undefined) ?? "";
  if (!projectId) {
    return (
      <ErrorState
        error="This pane was opened without a project."
        title="No project"
      />
    );
  }
  return <ProjectCard projectId={projectId} fill />;
}
