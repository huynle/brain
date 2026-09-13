import * as React from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useAuth } from "../../lib/auth";
import { useUI } from "../../store/ui";
import { useBulkJobs, dismissBulkJob, refreshBulkJobs, controlBulkJob, getBulkJobItems, retrySubmission, isBulkActive, type BulkJob, type BulkJobItem } from "../../store/bulkJobs";

export function BulkJobs() {
  const { jobs, submissions, error, dismissed } = useBulkJobs();
  const status = useAuth(s => s.status);
  const queryClient = useQueryClient();
  React.useEffect(() => {
    if (status !== "authenticated" && status !== "anonymous") return;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      const before = JSON.stringify(useBulkJobs.getState().jobs);
      await refreshBulkJobs();
      if (stopped) return;
      if (JSON.stringify(useBulkJobs.getState().jobs) !== before) void queryClient.invalidateQueries();
      timer = setTimeout(() => { void poll(); }, useBulkJobs.getState().jobs.some(isBulkActive) ? 1000 : 5000);
    };
    void poll();
    return () => { stopped = true; clearTimeout(timer); };
  }, [status, queryClient]);
  if (status !== "authenticated" && status !== "anonymous") return null;
  const visible = jobs.filter(j => !dismissed.includes(j.id));
  if (!visible.length && !submissions.length && !error) return null;
  return <aside className="background-operations" aria-label="Server bulk jobs">
    {error && <section className="background-operation error" role="status">{error}</section>}
    {submissions.map(s => <section className="background-operation" key={s.request.request_id}>
      <strong>{s.label}</strong><p role="status">{s.error || "Submitting job…"}</p>
      {s.error && <button onClick={() => { void retrySubmission(s); }}>Retry submission</button>}
    </section>)}
    {visible.map(job => <BulkJobCard key={job.id} job={job} />)}
  </aside>;
}

export function BulkJobCard({ job }: { job: BulkJob }) {
  const [expanded, setExpanded] = React.useState(false);
  const [offset, setOffset] = React.useState(0);
  const [items, setItems] = React.useState<BulkJobItem[]>([]);
  const [error, setError] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const done = job.succeeded + job.failed + job.uncertain + job.skipped;
  React.useEffect(() => {
    if (!expanded) return;
    let cancelled = false;
    getBulkJobItems(job.id, offset).then(rows => { if (!cancelled) { setItems(rows); setError(""); } })
      .catch(e => { if (!cancelled) setError(String(e)); });
    return () => { cancelled = true; };
  }, [expanded, job.id, offset, done]);
  const control = async (action: "pause" | "resume" | "retry") => {
    setBusy(true);
    try { await controlBulkJob(job.id, action); }
    catch (e) { useUI.getState().toast(String(e), "error"); }
    finally { setBusy(false); }
  };
  const label = `${job.label || job.operation.replace("_", " ")} · ${job.total} entries`;
  return <section className={`background-operation ${job.state === "needs_attention" ? "error" : ""}`}>
    <div className="background-operation-heading"><strong>{label}</strong>
      {!isBulkActive(job) && <button aria-label={`Dismiss ${label}`} onClick={() => dismissBulkJob(job.id)}>×</button>}
    </div>
    <div role="status">{job.state.replaceAll("_", " ")} · {job.succeeded} succeeded; {job.skipped} unchanged; {job.failed} failed; {job.uncertain} need review</div>
    <progress aria-label={`${label} progress`} max={Math.max(1, job.total)} value={done} />
    {isBulkActive(job) && <small>Runs on the server. You can close this tab.</small>}
    {job.total === 0 && <p>No entries matched the selection. Nothing changed.</p>}
    {job.uncertain > 0 && <p>An unconfirmed write may have taken effect. These entries will not be retried automatically; inspect their results below.</p>}
    <div>
      {["queued", "running"].includes(job.state) && <button disabled={busy} onClick={() => { void control("pause"); }}>Pause</button>}
      {job.state === "paused" && <button disabled={busy} onClick={() => { void control("resume"); }}>Resume</button>}
      {job.state === "needs_attention" && job.failed > 0 && <button disabled={busy} onClick={() => { void control("retry"); }}>Retry failed entries</button>}
      <button aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>{expanded ? "Hide results" : "View results"}</button>
    </div>
    {expanded && <div style={{ maxHeight: 260, overflow: "auto", overflowWrap: "anywhere" }}>
      {error && <p role="alert">{error}</p>}
      {items.map(i => <p key={i.sequence}><strong>{i.title || i.path}</strong> · {i.state}<br />{i.path}{i.destination && ` → ${i.destination}`}{i.error && <><br />{i.error}</>}</p>)}
      <button disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - 50))}>Previous</button>
      <span> {Math.min(offset + 1, job.total)}–{Math.min(offset + 50, job.total)} of {job.total} </span>
      <button disabled={offset + 50 >= job.total} onClick={() => setOffset(offset + 50)}>Next</button>
    </div>}
  </section>;
}
