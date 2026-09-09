/** Destructive integration check: synthetic loopback server only. */
import assert from "node:assert/strict";
import { startArchive } from "../src/store/archiveJob";
import { useBulkJobs, submitBulkJob, refreshBulkJobs, retrySubmission, controlBulkJob, type BulkJob } from "../src/store/bulkJobs";
import { api, bulkUpdate, getEntry } from "../src/lib/api";
const base = process.argv[2];
assert.match(base ?? "", /^http:\/\/127\.0\.0\.1:\d+$/);
assert.equal(process.env.BRAIN_SYNTHETIC_TEST, "1");
const memory = new Map<string, string>();
Object.defineProperty(globalThis, "localStorage", { value: { getItem: (k: string) => memory.get(k) ?? null, setItem: (k: string, v: string) => memory.set(k, v), removeItem: (k: string) => memory.delete(k) }, configurable: true });
const nativeFetch = globalThis.fetch;
let requests = 0, loseAcknowledgement = false;
globalThis.fetch = async (input, init) => {
  const url = new URL(String(input), base);
  assert.equal(url.origin, base, "Refusing non-test API traffic"); requests++;
  const result = await nativeFetch(url, init);
  if (loseAcknowledgement && init?.method === "POST" && url.pathname.endsWith("/bulk-jobs")) {
    loseAcknowledgement = false; await result.text(); throw new Error("simulated lost acknowledgement");
  }
  return result;
};
async function waitJob(id: string) {
  const deadline = Date.now() + 300_000;
  while (Date.now() < deadline) {
    const j = await api<BulkJob>(`bulk-jobs/${id}`);
    if (["completed", "needs_attention"].includes(j.state)) return j;
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  throw new Error(`Job ${id} timed out`);
}
const latest = () => useBulkJobs.getState().jobs[0];
async function tasks(project: string) { return (await api<{tasks: Array<{path: string;status: string}>}>(`tasks/${project}`)).tasks; }
async function check(name: string, run: () => Promise<void>) {
  const before = requests, start = performance.now(); await run();
  console.log(JSON.stringify({ name, requests: requests - before, seconds: +((performance.now() - start) / 1000).toFixed(3) }));
}
await check("archive 954 entries, pause/resume, recover lost POST acknowledgement", async () => {
  const explicit = (await tasks("bulk_test")).filter(t => t.path.includes("/cancelled-"));
  loseAcknowledgement = true;
  await startArchive({projectId: "bulk_test", taskPaths: explicit.map(t => t.path), featureIds: ["legacy", "normal"], total: 954});
  assert.equal(useBulkJobs.getState().submissions.length, 1);
  await retrySubmission(useBulkJobs.getState().submissions[0]);
  assert.equal(useBulkJobs.getState().submissions.length, 0);
  const id = latest().id;
  assert.equal((await api<BulkJob[]>("bulk-jobs")).length, 1, "one job after a lost response retry");
  await controlBulkJob(id, "pause");
  const paused = await api<BulkJob>(`bulk-jobs/${id}`);
  assert.equal(paused.state, "paused");
  await new Promise(resolve => setTimeout(resolve, 250));
  assert.equal((await api<BulkJob>(`bulk-jobs/${id}`)).succeeded, paused.succeeded);
  useBulkJobs.setState({jobs: []}); // new dashboard state reconnects from server
  await refreshBulkJobs(); assert.equal(latest().id, id);
  await controlBulkJob(id, "resume");
  const done = await waitJob(id); assert.equal(done.succeeded, 954, JSON.stringify(done)); assert.equal(done.state, "completed");
  assert.equal((await tasks("bulk_test")).filter(t => t.status === "archived").length, 1096);
});
await check("unsupported legacy remote cannot reopen", async () => {
  const r = await bulkUpdate({project: "bulk_test", feature_id: "legacy", type: "task", status: "archived"}, {status: "pending"}, {limit: 1});
  assert.equal(r.updated, 0); assert.equal(r.failed, 1);
});
await check("one job deletes 1096 archived tasks and preserves sibling projects", async () => {
  await submitBulkJob({operation: "delete", filters: [{project: "bulk_test", type: "task", status: "archived"}]});
  const done = await waitJob(latest().id); assert.equal(done.succeeded, 1096, JSON.stringify(done)); assert.equal(done.state, "completed");
  assert.equal((await tasks("bulk_test")).filter(t => t.status === "archived").length, 0);
  assert.equal((await tasks("bulkXtest")).length, 42); assert.equal((await tasks("bulk_test-other")).length, 42);
});
await check("205 status changes in both directions", async () => {
  for (const status of ["pending", "completed"]) {
    await submitBulkJob({operation: "set_status", status, filters: [{project: "bulk_test",type: "task", feature_id: "status-moves"}]});
    const done = await waitJob(latest().id); assert.equal(done.succeeded, 205, JSON.stringify(done)); assert.equal(done.state, "completed");
  }
});
await check("one job moves 125 entries between projects", async () => {
  await submitBulkJob({operation: "move",target_project: "destination", filters: [{project: "bulk_test",type: "task",feature_id: "move"}]});
  const done = await waitJob(latest().id); assert.equal(done.succeeded, 125, JSON.stringify(done));
  assert.equal((await tasks("destination")).length, 126);
  assert.equal((await tasks("bulk_test")).filter(t => t.path.includes("/move-")).length, 0);
});
await check("same-project move preserves entry; collision does not overwrite", async () => {
  const same = "projects/destination/task/move-0000.md";
  await submitBulkJob({operation: "move",target_project: "destination",paths: [same,"projects/bulk_test/task/collision.md"]});
  const done = await waitJob(latest().id); assert.equal(done.succeeded,1); assert.equal(done.uncertain,1);
  assert.match((await getEntry(same)).content ?? "", /Synthetic content/);
  assert.match((await getEntry("projects/bulk_test/task/collision.md")).content ?? "", /Source collision/);
  assert.match((await getEntry("projects/destination/task/collision.md")).content ?? "", /Existing destination/);
});
await check("explicit 205-entry deletion", async () => {
  await submitBulkJob({operation: "delete",paths: (await tasks("bulk_test")).filter(t => t.path.includes("/status-moves-")).map(t => t.path)});
  const done = await waitJob(latest().id); assert.equal(done.succeeded,205); assert.equal(done.state,"completed");
});
console.log("PASS: durable bulk lifecycle");
