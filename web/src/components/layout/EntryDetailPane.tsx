// Read-only detail pane for a selected brain entry (note, task, automation,
// goal). Shared by the Brain and Automations tabs so they get the same
// list + detail layout the Tasks tab has. Fetches the full entry by path so it
// works from search results (which carry only a path) too.

import { useQuery } from "@tanstack/react-query";
import { getDispatchLease, getEntry } from "../../lib/api";
import { AttachmentGallery } from "./AttachmentGallery";
import { relativeTime } from "../../lib/format";
import type { BrainEntry, DispatchLease } from "../../lib/types";

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  if (children == null || children === "") return null;
  return (
    <div className="detail-field">
      <span className="detail-label faint">{label}</span>
      <span className="detail-value">{children}</span>
    </div>
  );
}

// Map lease state → display color. Matches the runner-status palette used
// elsewhere so users can scan dispatch state without learning new colors.
function leaseStateColor(state: string): string | undefined {
  switch (state) {
    case "pushed": return "var(--yellow)";   // sent but not yet ack'd
    case "acked":  return "var(--green)";    // runner accepted, executing
    case "rejected":
    case "expired": return "var(--red)";
    default: return undefined;
  }
}

// Format a unix-ms timestamp as a short relative string ("in 3m" / "5s
// ago"). Kept inline because relativeTime() takes an ISO string and lease
// timestamps come back as numbers.
function relMs(ms?: number): string | undefined {
  if (!ms) return undefined;
  const deltaSec = Math.round((ms - Date.now()) / 1000);
  const abs = Math.abs(deltaSec);
  const fmt =
    abs < 60 ? `${abs}s` :
    abs < 3600 ? `${Math.round(abs / 60)}m` :
    abs < 86400 ? `${Math.round(abs / 3600)}h` :
    `${Math.round(abs / 86400)}d`;
  return deltaSec >= 0 ? `in ${fmt}` : `${fmt} ago`;
}

function DispatchSection({ lease }: { lease: DispatchLease }) {
  const color = leaseStateColor(lease.state);
  return (
    <>
      <Field label="Dispatch">
        <span style={{ color }}>● {lease.state}</span>
        {" → "}
        <span className="mono">{lease.assigned_runner_id}</span>
      </Field>
      {lease.assigned_machine_id && lease.assigned_machine_id !== lease.assigned_runner_id && (
        <Field label="Machine"><span className="mono">{lease.assigned_machine_id}</span></Field>
      )}
      <Field label="Lease ID"><span className="mono" style={{ fontSize: 11.5 }}>{lease.leaseId}</span></Field>
      {lease.pushed_at ? <Field label="Pushed">{relMs(lease.pushed_at)}</Field> : null}
      {lease.acked_at ? <Field label="Acked">{relMs(lease.acked_at)}</Field> : null}
      {lease.expires_at ? <Field label="Expires">{relMs(lease.expires_at)}</Field> : null}
      {lease.last_error ? <Field label="Last error"><span style={{ color: "var(--red)" }}>{lease.last_error}</span></Field> : null}
    </>
  );
}

export function EntryDetailPane({ path }: { path: string | null }) {
  const q = useQuery({
    queryKey: ["entry-detail", path],
    queryFn: () => getEntry(path as string),
    enabled: !!path,
    staleTime: 5_000,
  });

  const e = q.data as BrainEntry | undefined;
  // Only fetch dispatch lease for task entries — other entry types never
  // have one. The endpoint returns null on 404, so an inactive task simply
  // omits the Dispatch section.
  const isTask = e?.type === "task";
  const projectId = e?.project_id;
  const taskId = e?.id;
  const leaseQ = useQuery({
    queryKey: ["dispatch-lease", projectId, taskId],
    queryFn: () => getDispatchLease(projectId as string, taskId as string),
    enabled: isTask && !!projectId && !!taskId,
    // Lease state changes quickly — refetch on a short interval while a
    // dispatch is in flight so the pane stays current without a manual
    // refresh.
    refetchInterval: 5_000,
    staleTime: 2_000,
  });

  if (!path) return <span className="faint">Nothing selected.</span>;
  if (q.isLoading) return <span className="faint">Loading…</span>;
  if (q.error) return <span className="faint" style={{ color: "var(--red)" }}>{String((q.error as Error).message)}</span>;

  if (!e) return <span className="faint">Not found.</span>;

  const trigger = e.trigger;
  const triggerText = trigger?.type
    ? `${trigger.type}${trigger.event ? `:${trigger.event}` : ""}${trigger.schedule ? `:${trigger.schedule}` : ""}`
    : undefined;
  const action = e.action;
  const actionType = typeof action?.type === "string" ? action.type : undefined;
  const directPrompt = typeof action?.direct_prompt === "string" ? action.direct_prompt : "";
  const body = e.content || directPrompt || "";
  const sessions = e.sessions ? Object.entries(e.sessions) : [];
  const lease = leaseQ.data ?? null;

  return (
    <div className="detail-body">
      <div className="detail-title">{e.title || e.id}</div>
      <Field label="Type">{e.type}</Field>
      <Field label="Status">{e.status}</Field>
      <Field label="ID">{e.id}</Field>
      <Field label="Project">{e.project_id}</Field>
      {triggerText && <Field label="Trigger">{triggerText}</Field>}
      {e.schedule && <Field label="Schedule">{e.schedule}</Field>}
      {actionType && <Field label="Action">{actionType}</Field>}
      {e.executor && <Field label="Executor">{e.executor}</Field>}
      {e.target_workdir && <Field label="Workdir">{e.target_workdir}</Field>}
      {e.tags && e.tags.length > 0 && <Field label="Tags">{e.tags.join(", ")}</Field>}
      {e.modified && <Field label="Modified">{relativeTime(e.modified)}</Field>}
      {lease && <DispatchSection lease={lease} />}
      {sessions.length > 0 && (
        <Field label="Sessions">
          {sessions.map(([sid, info]) => (
            <div key={sid} className="mono" style={{ fontSize: 11.5 }}>
              {sid}
              {info?.hostname ? <span className="faint"> @ {info.hostname}</span> : null}
            </div>
          ))}
        </Field>
      )}
      {body && (
        <div className="detail-content">
          <div className="detail-label faint">Content</div>
          <pre className="detail-pre">{body}</pre>
        </div>
      )}
      <AttachmentGallery attachments={e.attachments} />
    </div>
  );
}
