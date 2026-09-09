import { submitBulkJob } from "./bulkJobs";
import { TERMINAL_STATUSES } from "../lib/actions/taskActions";

export interface ArchivePlan {
  projectId: string;
  taskPaths: string[];
  featureIds: string[];
  total: number;
}

/** One immutable server job; the browser only submits and observes it. */
export function startArchive(plan: ArchivePlan, submit = submitBulkJob): Promise<void> {
  return submit({
    operation: "archive",
    paths: [...new Set(plan.taskPaths)],
    filters: [...new Set(plan.featureIds)].flatMap(feature_id =>
      [...TERMINAL_STATUSES].filter(status => status !== "archived").map(status =>
        ({ project: plan.projectId, feature_id, type: "task", status }))),
  }, `Archive tasks · ${plan.projectId}`);
}
