// Read-only detail pane for a selected brain entry (note, task, automation,
// goal). Shared by the Brain and Automations tabs so they get the same
// list + detail layout the Tasks tab has. Fetches the full entry by path so it
// works from search results (which carry only a path) too.

import { useQuery } from "@tanstack/react-query";
import { getEntry } from "../../lib/api";
import { relativeTime } from "../../lib/format";
import type { BrainEntry } from "../../lib/types";

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  if (children == null || children === "") return null;
  return (
    <div className="detail-field">
      <span className="detail-label faint">{label}</span>
      <span className="detail-value">{children}</span>
    </div>
  );
}

export function EntryDetailPane({ path }: { path: string | null }) {
  const q = useQuery({
    queryKey: ["entry-detail", path],
    queryFn: () => getEntry(path as string),
    enabled: !!path,
    staleTime: 5_000,
  });

  if (!path) return <span className="faint">Nothing selected.</span>;
  if (q.isLoading) return <span className="faint">Loading…</span>;
  if (q.error) return <span className="faint" style={{ color: "var(--red)" }}>{String((q.error as Error).message)}</span>;

  const e = q.data as BrainEntry | undefined;
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
    </div>
  );
}
