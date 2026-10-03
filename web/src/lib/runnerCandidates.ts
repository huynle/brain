import type { RunnerCandidate } from "./types";

/** IDs rendered by an assignment picker. The current pin is retained even
 * when it is offline or incompatible so an invalid assignment never vanishes. */
export function assignmentRunnerIDs(
  candidates: RunnerCandidate[],
  currentRunnerID?: string,
): string[] {
  const ids: string[] = [];
  if (
    currentRunnerID &&
    candidates.some((candidate) => candidate.runner.runner_id === currentRunnerID)
  ) {
    ids.push(currentRunnerID);
  }
  for (const candidate of candidates) {
    const id = candidate.runner.runner_id;
    if (id !== currentRunnerID && candidate.compatible) {
      ids.push(id);
    }
  }
  return ids;
}
