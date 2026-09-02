/**
 * DispatchAttemptsSection — the dispatch/placement history for a task.
 *
 * Mirrors SessionsSection in shape and styling so the two "what happened
 * to this task" lists read the same way. Where Sessions shows completed
 * runs, this shows *attempts to place the task on a runner* and why they
 * did not stick:
 *
 *   - scheduler "no_candidate" decisions (no eligible runner), and
 *   - runner-side "dispatch_rejected" decisions (a runner was pushed the
 *     task and refused it — e.g. worktree mode with no git context,
 *     reported as `workdir_unavailable`).
 *
 * Both are persisted in the append-only task_placement_reasons history
 * (bounded per task) and surfaced on task.placement_reasons. A single
 * dispatch lease only ever holds the LATEST attempt, so without this
 * history a task that is rejected every few seconds shows no trace of
 * how many times it tried or why — it just sits "pending" forever. This
 * section makes the try-count and reason visible.
 *
 * The live dispatch lease (task.dispatch_lease) is shown as a header line
 * when its state is a terminal failure, so the current standing reason is
 * legible even before the next history row lands.
 */
import type { PlacementReason, Task } from "../../lib/types";

function fmtWhen(ms?: number): string {
  if (!ms) return "";
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return "";
  // Match the Sessions row format: YYYY-MM-DD HH:MM (local).
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(
    d.getHours(),
  )}:${pad(d.getMinutes())}`;
}

function decisionLabel(decision?: string): { text: string; color: string } {
  switch (decision) {
    case "dispatch_rejected":
      return { text: "rejected", color: "#e5534b" };
    case "no_candidate":
      return { text: "no runner", color: "#c69026" };
    default:
      return { text: decision || "attempt", color: "#6b757e" };
  }
}

export function DispatchAttemptsSection({
  task,
}: {
  task: Task;
}): JSX.Element | null {
  const reasons = task.placement_reasons ?? [];
  const lease = task.dispatch_lease;

  // Nothing to show if the task has never failed placement and there is no
  // failed lease standing. A successfully dispatched task leaves no
  // placement-reason rows (they only record failures), so this section
  // stays absent for the happy path — exactly like SessionsSection hides
  // when there are no sessions.
  const leaseFailed =
    !!lease && (lease.state === "rejected" || lease.state === "expired");
  if (reasons.length === 0 && !leaseFailed) return null;

  // Newest first — placement_reasons arrives in ascending chronological
  // order from the API.
  const ordered = [...reasons].reverse();

  return (
    <>
      <h4 className="modal-content-heading">
        Dispatch attempts
        {ordered.length > 0 ? ` (${ordered.length})` : ""}
      </h4>

      {leaseFailed && lease && (
        <div
          style={{
            fontSize: 12,
            color: "#e5534b",
            marginBottom: 6,
            lineHeight: 1.4,
          }}
        >
          <b>Current: {lease.state}</b>
          {lease.assigned_runner_id ? ` · ${lease.assigned_runner_id}` : ""}
          {lease.last_error ? (
            <div style={{ color: "#b6bdc4", marginTop: 2 }}>
              {leaseErrorText(lease.last_error)}
            </div>
          ) : null}
        </div>
      )}

      {ordered.length > 0 && (
        <div className="kv-grid">
          {ordered.map((r, i) => (
            <DispatchAttemptRow key={rowKey(r, i)} reason={r} />
          ))}
        </div>
      )}
    </>
  );
}

function rowKey(r: PlacementReason, i: number): string {
  return `${r.created_at ?? 0}:${r.decision ?? ""}:${i}`;
}

function DispatchAttemptRow({
  reason,
}: {
  reason: PlacementReason;
}): JSX.Element {
  const label = decisionLabel(reason.decision);
  return (
    <div
      style={{
        gridColumn: "1 / -1",
        display: "flex",
        alignItems: "baseline",
        gap: 8,
        fontSize: 12,
        lineHeight: 1.5,
      }}
    >
      <span
        style={{
          color: label.color,
          fontWeight: 600,
          minWidth: 62,
          flexShrink: 0,
        }}
        title={reason.decision}
      >
        {label.text}
      </span>
      <span style={{ flex: 1, color: "#b6bdc4", wordBreak: "break-word" }}>
        {reason.reason || "—"}
      </span>
      <span style={{ color: "#6b757e", flexShrink: 0 }}>
        {reason.runner_id ? `${reason.runner_id} · ` : ""}
        {fmtWhen(reason.created_at)}
      </span>
    </div>
  );
}

// The lease's last_error is stored as the JSON-encoded DispatchRejectReason
// ({code, message, details}). Render it human-readably, falling back to the
// raw string when it is not the expected shape.
function leaseErrorText(raw: string): string {
  try {
    const parsed = JSON.parse(raw) as {
      code?: string;
      message?: string;
    };
    if (parsed && (parsed.code || parsed.message)) {
      if (parsed.code && parsed.message) return `${parsed.code}: ${parsed.message}`;
      return parsed.code || parsed.message || raw;
    }
  } catch {
    // not JSON — show as-is
  }
  return raw;
}
