import { startBackgroundOperation, type ReportProgress } from "./backgroundOperations";
import { bulkUpdate, bulkUpdateEntries } from "../lib/api";
import { runBulkBaton } from "../lib/actions/bulkBaton";
import { TERMINAL_STATUSES } from "../lib/actions/taskActions";
import { chunkPaths } from "../lib/selection";

export interface ArchivePlan {
  projectId: string;
  taskPaths: string[];
  featureIds: string[];
  total: number;
}

export interface ArchiveJob {
  projectId: string;
  expected: number;
  ok: number;
  failed: number;
  finishedGroups: number;
  totalGroups: number;
  running: boolean;
  errors: string[];
}

const sources = [...TERMINAL_STATUSES].filter((s) => s !== "archived");
const api = { bulkUpdate, bulkUpdateEntries };

/** Owns the operation outside React so navigation/selection changes cannot interrupt it.
 * Three independent groups may run at once; pages within a feature stay serial.
 * A browser reload ends the operation. Never automatically replay uncertain writes.
 */
export function startArchive(plan: ArchivePlan, client = api): Promise<void> {
  return startBackgroundOperation(`Archive tasks · ${plan.projectId}`, (report) => runArchive(plan, client, report));
}

async function runArchive(plan: ArchivePlan, client: typeof api, report: ReportProgress): Promise<void> {
  const paths = chunkPaths([...new Set(plan.taskPaths)], 25);
  const features = [...new Set(plan.featureIds)];
  let job: ArchiveJob = {
    projectId: plan.projectId, expected: plan.total, ok: 0, failed: 0,
    finishedGroups: 0, totalGroups: paths.length + features.length,
    running: true, errors: [],
  };
  const publish = (patch: Partial<ArchiveJob>) => {
    job = { ...job, ...patch };
    report({
      detail: `${job.ok} archived (initial estimate: ${job.expected}); ${job.failed} failed attempts. ${job.finishedGroups}/${job.totalGroups} groups checked.`,
      completed: job.finishedGroups, total: job.totalGroups,
    });
  };
  publish({});
  const absorb = (r: { updated: number; failed: number }) => {
    publish({ ok: job.ok + r.updated, failed: job.failed + r.failed });
  };
  const work: Array<() => Promise<void>> = [
    ...paths.map((chunk) => async () => {
      absorb(await client.bulkUpdateEntries(chunk, { status: "archived" }));
    }),
    ...features.map((fid) => async () => {
      for (const source of sources) {
        const out = await runBulkBaton(async () => {
          const result = await client.bulkUpdate(
            { project: plan.projectId, feature_id: fid, type: "task", status: source },
            { status: "archived" },
            { limit: 25 },
          );
          absorb(result);
          return result;
        }, (r) => r.updated, { maxIterations: 200 });
        if (out.stopped) {
          publish({ errors: [...job.errors, `${fid}: stopped with tasks remaining`] });
        }
      }
    }),
  ];
  let next = 0;
  const worker = async () => {
    while (next < work.length) {
      const run = work[next++];
      try {
        await run();
      } catch (err) {
        publish({ errors: [...job.errors, err instanceof Error ? err.message : String(err)] });
      } finally {
        publish({ finishedGroups: job.finishedGroups + 1 });
      }
    }
  };
  return Promise.all(Array.from({ length: Math.min(3, work.length) }, worker))
    .then(() => {
      publish({ running: false });
      if (job.errors.length || job.failed) {
        throw new Error(`${job.ok} archived; ${job.failed} failed attempts. ${job.errors.slice(0, 3).join("; ") || "Some tasks could not be archived"}`);
      }
    });
}
