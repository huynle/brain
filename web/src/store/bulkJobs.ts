import { create } from "zustand";
import { api, ApiError, type BulkFilter } from "../lib/api";

export interface BulkJobRequest {
  label?: string;
  request_id: string;
  operation: "archive" | "delete" | "set_status" | "move";
  paths?: string[];
  filters?: BulkFilter[];
  status?: string;
  target_project?: string;
  force?: boolean;
}
export interface BulkJob {
  label?: string; id: string; request_id: string; operation: string; state: string;
  total: number; pending: number; running: number; succeeded: number;
  failed: number; uncertain: number; skipped: number;
}
export interface BulkJobItem {
  sequence: number; path: string; title: string; state: string; attempts: number;
  error?: string; destination?: string;
}
export const isBulkActive = (j: BulkJob) => ["queued", "running", "paused"].includes(j.state);
const KEY = "brain.bulk-submissions";
interface Submission { request: BulkJobRequest; label: string; error?: string }
function readDismissed(): string[] {
  try { return JSON.parse(localStorage.getItem("brain.bulk-dismissed") || "[]") as string[]; } catch { return []; }
}
export function dismissBulkJob(id: string) {
  const dismissed = [...useBulkJobs.getState().dismissed, id].slice(-200);
  try { localStorage.setItem("brain.bulk-dismissed", JSON.stringify(dismissed)); } catch { /* in-memory dismissal still works */ }
  useBulkJobs.setState({ dismissed });
}
function readSubmissions(): Submission[] {
  try { return JSON.parse(localStorage.getItem(KEY) || "[]") as Submission[]; } catch { return []; }
}
function saveSubmissions(pending: Submission[]) {
  // Persist BEFORE submitting. If storage is unavailable, don't send an
  // operation whose ambiguous acknowledgement cannot be recovered on reload.
  localStorage.setItem(KEY, JSON.stringify(pending));
}
export const useBulkJobs = create<{
  jobs: BulkJob[]; submissions: Submission[]; error: string; dismissed: string[];
}>()(() => ({ jobs: [], submissions: readSubmissions(), error: "", dismissed: readDismissed() }));
function receive(job: BulkJob) {
  useBulkJobs.setState(s => ({ jobs: [job, ...s.jobs.filter(j => j.id !== job.id)] }));
}
export async function refreshBulkJobs() {
  try {
    const jobs = await api<BulkJob[]>("bulk-jobs");
    useBulkJobs.setState({ jobs, error: "" });
    // An acknowledged server record resolves a lost POST response, without
    // re-submitting a mutation on page load or under another login.
    const pending = useBulkJobs.getState().submissions.filter(s => !jobs.some(j => j.request_id === s.request.request_id));
    saveSubmissions(pending);
    useBulkJobs.setState({ submissions: pending });
  } catch (e) {
    useBulkJobs.setState({ error: `Progress unavailable: ${e instanceof Error ? e.message : String(e)}. Server jobs may still be running.` });
  }
}
const inFlight = new Set<string>();
export async function retrySubmission(submission: Submission): Promise<void> {
  const id = submission.request.request_id;
  if (inFlight.has(id)) return;
  inFlight.add(id);
  try {
    const job = await api<BulkJob>("bulk-jobs", { method: "POST", body: submission.request });
    receive(job);
    const pending = useBulkJobs.getState().submissions.filter(s => s.request.request_id !== id);
    saveSubmissions(pending); useBulkJobs.setState({ submissions: pending });
  } catch (e) {
    const definite = e instanceof ApiError && e.status >= 400 && e.status < 500;
    const error = `${definite ? "Submission rejected" : "Submission not confirmed"}: ${e instanceof Error ? e.message : String(e)}. Retry uses the same request ID.`;
    const pending = useBulkJobs.getState().submissions.map(s => s.request.request_id === id ? { ...s, error } : s);
    saveSubmissions(pending); useBulkJobs.setState({ submissions: pending });
  } finally { inFlight.delete(id); }
}
export async function submitBulkJob(request: Omit<BulkJobRequest, "request_id">, label: string = `${request.operation.replace("_", " ")} · ${request.filters?.[0]?.project || request.paths?.[0]?.split("/")[1] || "entries"}`): Promise<void> {
  const submission = { request: { ...request, label, request_id: crypto.randomUUID() }, label };
  const pending = [...useBulkJobs.getState().submissions, submission];
  saveSubmissions(pending); useBulkJobs.setState({ submissions: pending });
  await retrySubmission(submission);
}
export async function controlBulkJob(id: string, action: "pause" | "resume" | "retry") {
  receive(await api<BulkJob>(`bulk-jobs/${id}/control`, { method: "POST", body: { action } }));
}
export const getBulkJobItems = (id: string, offset: number) => api<BulkJobItem[]>(`bulk-jobs/${id}/items`, { query: { offset, limit: 50 } });
