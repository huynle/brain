/**
 * GoalModal — detail view (and lightweight editor) for one goal.
 *
 * View mode: status chip, scope line (project / feature / task link),
 * criteria + validation, steering config, linked-task progress bar
 * (same bar styling as FeatureModal), and the recent reconcile audit
 * timeline. Footer is the shared ActionBar over `buildGoalActions`, so
 * the modal offers exactly the verbs the card rows and palette do.
 *
 * Edit mode (tab === "edit", reached via the "Edit goal…" verb): a
 * metadata-lite form over title / criteria / validation / prompt /
 * steering. Deliberately small — trigger surgery and status flips go
 * through the dedicated verbs, not the form.
 *
 * Goals do not ride SSE; every save invalidates the ["goals"] queries.
 */
import { useEffect, useMemo, useState, type CSSProperties } from "react";

import { Modal } from "../common/Modal";
import { ActionBar } from "../common/ActionBar";
import { Loading } from "../common/Loading";
import { useModal } from "../../store/modal";
import { useUI } from "../../store/ui";
import { useActionRunner } from "../../hooks/useActionRunner";
import { useGoalActionContext } from "../../hooks/useGoalActionContext";
import { useGoals, useGoalAudit, useGoalProgress } from "../../hooks/useGoals";
import {
  buildGoalActions,
  goalStatusLabel,
} from "../../lib/actions/goalActions";
import { updateGoal, ApiError } from "../../lib/api";
import { relativeTime } from "../../lib/format";
import type { GoalReconcileAudit, GoalSummary } from "../../lib/types";

/** life-badge tone for a goal status (classes from global.css). */
function statusTone(status: string): string {
  switch (status) {
    case "active":
      return "active";
    case "blocked":
      return "blocked";
    case "completed":
      return "finished";
    default:
      return ""; // archived → base muted badge
  }
}

const DECISION_COLORS: Record<string, string> = {
  complete: "#6fca7d",
  block: "#d96060",
  need_work: "#f4b23a",
  steer: "#6a8bff",
  noop: "#6b757e",
};

function DecisionChip({ decision }: { decision: string }): JSX.Element {
  const color = DECISION_COLORS[decision] ?? "#9098a1";
  return (
    <span
      style={{
        flex: "0 0 auto",
        padding: "0 5px",
        border: `1px solid ${color}55`,
        borderRadius: 3,
        color,
        background: `${color}11`,
        fontSize: 9.5,
        textTransform: "uppercase",
        letterSpacing: "0.04em",
      }}
    >
      {decision.replace(/_/g, " ")}
    </span>
  );
}

function AuditRow({ row }: { row: GoalReconcileAudit }): JSX.Element {
  const steerCounts =
    row.decision === "steer"
      ? ` (${row.sessions_steered ?? 0} steered, ${row.sessions_skipped ?? 0} skipped)`
      : "";
  return (
    <div
      style={{
        display: "flex",
        alignItems: "baseline",
        gap: 8,
        padding: "3px 0",
        borderBottom: "1px solid #1a1e22",
        fontSize: 11,
      }}
    >
      <DecisionChip decision={row.decision} />
      <span style={{ flex: 1, minWidth: 0, color: "#c6ccd2" }}>
        {row.reason}
        {steerCounts}
        {row.generated_task_id && (
          <span style={{ color: "#6b757e" }}>
            {" "}
            → task {row.generated_task_id.slice(0, 8)}
          </span>
        )}
      </span>
      <span
        style={{ flex: "0 0 auto", color: "#6b757e" }}
        title={row.timestamp}
      >
        {relativeTime(row.timestamp)}
      </span>
    </div>
  );
}

interface EditState {
  title: string;
  criteria: string;
  validation: string;
  prompt: string;
  steeringEnabled: boolean;
  steeringCooldown: string; // raw input; blank ⇒ server default
}

function editStateFor(goal: GoalSummary): EditState {
  return {
    title: goal.title ?? "",
    criteria: goal.config?.criteria ?? "",
    validation: goal.config?.validation ?? "",
    prompt: goal.action?.direct_prompt ?? "",
    steeringEnabled: goal.config?.steering?.enabled !== false,
    steeringCooldown: goal.config?.steering?.cooldown_minutes
      ? String(goal.config.steering.cooldown_minutes)
      : "",
  };
}

const FIELD_STYLE: CSSProperties = {
  width: "100%",
  boxSizing: "border-box",
  background: "#0a0c0e",
  border: "1px solid #2a2f35",
  borderRadius: 4,
  color: "#c6ccd2",
  font: "inherit",
  fontSize: 12,
  padding: "5px 7px",
};

export function GoalModal(): JSX.Element {
  const target = useModal((s) => s.target);
  const tab = useModal((s) => s.tab);
  const switchTab = useModal((s) => s.switchTab);
  const openModal = useModal((s) => s.open);
  const close = useModal((s) => s.close);
  const toast = useUI((s) => s.toast);

  const goalId =
    (target?.goalId as string | undefined) ??
    (target?.id as string | undefined) ??
    "";

  const { goals, isLoading, invalidate } = useGoals();
  const primary = goals.find((g) => g.goal_id === goalId);
  // Archived goals are not in the default set; fall back to the archived
  // list only when the primary lookup came back empty-handed.
  const { goals: archivedGoals } = useGoals({
    archived: true,
    enabled: !isLoading && !primary,
  });
  const goal = primary ?? archivedGoals.find((g) => g.goal_id === goalId);

  const { progress } = useGoalProgress(goal ? goalId : "");
  const { audit } = useGoalAudit(goal ? goalId : "", 15);

  const goalCtx = useGoalActionContext();
  const runner = useActionRunner();
  const actions = useMemo(
    () => (goal ? buildGoalActions(goal, goalCtx) : []),
    [goal, goalCtx],
  );

  const editing = tab === "edit";
  const [form, setForm] = useState<EditState | null>(null);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  useEffect(() => {
    // (Re)seed the form each time edit mode is entered; goal identity
    // changes re-seed too so a stale draft never bleeds across goals.
    if (editing && goal) setForm(editStateFor(goal));
    if (!editing) {
      setForm(null);
      setSaveError(null);
    }
  }, [editing, goal?.goal_id]); // eslint-disable-line react-hooks/exhaustive-deps

  async function save() {
    if (!goal || !form) return;
    if (!form.title.trim()) {
      setSaveError("Title is required");
      return;
    }
    const cooldown = form.steeringCooldown.trim()
      ? Number(form.steeringCooldown)
      : 0;
    if (!Number.isFinite(cooldown) || cooldown < 0) {
      setSaveError("Cooldown must be a non-negative number of minutes");
      return;
    }
    setSaving(true);
    setSaveError(null);
    try {
      await updateGoal(goal.goal_id, {
        title: form.title.trim(),
        criteria: form.criteria,
        validation: form.validation,
        steering: {
          enabled: form.steeringEnabled,
          ...(cooldown > 0 ? { cooldown_minutes: cooldown } : {}),
        },
        // The server replaces the action wholesale when provided, so
        // spread the existing one to keep agent/model/executor intact.
        action: { ...(goal.action ?? { type: "prompt" }), direct_prompt: form.prompt },
      });
      invalidate();
      toast(`Saved ${form.title.trim()}`, "success");
      switchTab("view");
    } catch (err) {
      setSaveError(
        err instanceof ApiError
          ? err.message
          : ((err as Error).message ?? "unknown error"),
      );
    } finally {
      setSaving(false);
    }
  }

  if (!goal) {
    return (
      <Modal
        title={goalId ? `Goal: ${goalId}` : "Goal"}
        onClose={close}
      >
        {isLoading ? (
          <Loading label="Loading goals…" />
        ) : (
          <div style={{ color: "#9098a1" }}>
            No goal with id <code>{goalId}</code>.
          </div>
        )}
      </Modal>
    );
  }

  const projectId = goal.project ?? "";
  const cfg = goal.config;
  const steeringEnabled = cfg?.steering?.enabled !== false;
  const cooldown = cfg?.steering?.cooldown_minutes || 15;
  const pct =
    progress && progress.total > 0
      ? Math.round((progress.completed / progress.total) * 100)
      : 0;
  const auditRows = audit?.audit ?? [];

  return (
    <Modal
      title={
        <>
          {goal.title || goal.goal_id}{" "}
          <span
            className={`life-badge ${statusTone(goal.status)}`}
            style={{ marginLeft: 8 }}
          >
            {goalStatusLabel(goal.status)}
          </span>
        </>
      }
      onClose={close}
      refocusKey={editing ? "edit" : "view"}
      footer={
        editing ? (
          <>
            <button onClick={() => switchTab("view")} disabled={saving}>
              Cancel
            </button>
            <button
              className="primary"
              data-autofocus="true"
              onClick={() => void save()}
              disabled={saving}
            >
              {saving ? "Saving…" : "Save goal"}
            </button>
          </>
        ) : (
          <>
            {/* Shared registry — same verbs as card rows and palette. */}
            <ActionBar
              actions={actions}
              onRun={runner.run}
              primary={["run", "resume", "pause"]}
            />
            <button className="primary" onClick={close}>
              Done
            </button>
            {runner.dialog}
          </>
        )
      }
    >
      {editing && form ? (
        <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
          <label style={{ fontSize: 11, color: "#9098a1" }}>
            Title
            <input
              style={FIELD_STYLE}
              value={form.title}
              onChange={(e) => setForm({ ...form, title: e.target.value })}
            />
          </label>
          <label style={{ fontSize: 11, color: "#9098a1" }}>
            Success criteria
            <textarea
              style={{ ...FIELD_STYLE, minHeight: 56 }}
              value={form.criteria}
              onChange={(e) => setForm({ ...form, criteria: e.target.value })}
            />
          </label>
          <label style={{ fontSize: 11, color: "#9098a1" }}>
            Validation
            <textarea
              style={{ ...FIELD_STYLE, minHeight: 42 }}
              value={form.validation}
              onChange={(e) =>
                setForm({ ...form, validation: e.target.value })
              }
            />
          </label>
          <label style={{ fontSize: 11, color: "#9098a1" }}>
            Prompt (for generated tasks)
            <textarea
              style={{ ...FIELD_STYLE, minHeight: 42 }}
              value={form.prompt}
              onChange={(e) => setForm({ ...form, prompt: e.target.value })}
            />
          </label>
          <div style={{ display: "flex", gap: 12, alignItems: "center" }}>
            <label
              style={{
                fontSize: 11,
                color: "#9098a1",
                display: "flex",
                gap: 6,
                alignItems: "center",
              }}
            >
              <input
                type="checkbox"
                checked={form.steeringEnabled}
                onChange={(e) =>
                  setForm({ ...form, steeringEnabled: e.target.checked })
                }
              />
              Steer live sessions
            </label>
            <label style={{ fontSize: 11, color: "#9098a1" }}>
              Cooldown (min)
              <input
                style={{ ...FIELD_STYLE, width: 70, marginLeft: 6 }}
                placeholder="15"
                value={form.steeringCooldown}
                disabled={!form.steeringEnabled}
                onChange={(e) =>
                  setForm({ ...form, steeringCooldown: e.target.value })
                }
              />
            </label>
          </div>
          {saveError && (
            <div style={{ color: "#d96060", fontSize: 11 }}>{saveError}</div>
          )}
        </div>
      ) : (
        <>
          <div className="kv-grid">
            <div className="k">Goal id</div>
            <div className="v">
              <code>{goal.goal_id}</code>
            </div>
            <div className="k">Scope</div>
            <div className="v">
              {projectId || "—"}
              {goal.feature_id && (
                <>
                  {" / "}
                  <a
                    style={{ color: "#6a8bff", cursor: "pointer" }}
                    onClick={() =>
                      openModal("feature", {
                        projectId,
                        featureId: goal.feature_id,
                      })
                    }
                  >
                    {goal.feature_id}
                  </a>
                </>
              )}
              {cfg?.task_id && (
                <>
                  {" / task "}
                  <a
                    style={{ color: "#6a8bff", cursor: "pointer" }}
                    onClick={() =>
                      openModal("task", { projectId, taskId: cfg.task_id })
                    }
                  >
                    {cfg.task_id.slice(0, 8)}
                  </a>
                </>
              )}
            </div>
            <div className="k">Steering</div>
            <div className="v">
              {steeringEnabled
                ? `enabled · ${cooldown}m cooldown`
                : "disabled"}
            </div>
            {progress && (
              <>
                <div className="k">Progress</div>
                <div className="v">
                  <div
                    style={{ display: "flex", alignItems: "center", gap: 8 }}
                  >
                    {/* Same bar styling as FeatureModal. */}
                    <div
                      className="bar"
                      style={{
                        width: 120,
                        height: 6,
                        background: "#22272c",
                        borderRadius: 3,
                        overflow: "hidden",
                      }}
                    >
                      <i
                        style={{
                          display: "block",
                          height: "100%",
                          width: `${pct}%`,
                          background: "#6fca7d",
                        }}
                      />
                    </div>
                    <span>
                      {progress.completed}/{progress.total} ({pct}%)
                      {progress.blocked > 0 &&
                        ` · ${progress.blocked} blocked`}
                      {progress.in_progress > 0 &&
                        ` · ${progress.in_progress} running`}
                    </span>
                  </div>
                </div>
              </>
            )}
          </div>

          {cfg?.criteria && (
            <>
              <h4
                style={{ margin: "12px 0 6px", color: "#f4b23a", fontSize: 11 }}
              >
                Success criteria
              </h4>
              <div style={{ fontSize: 12, whiteSpace: "pre-wrap" }}>
                {cfg.criteria}
              </div>
            </>
          )}
          {cfg?.validation && (
            <>
              <h4
                style={{ margin: "12px 0 6px", color: "#f4b23a", fontSize: 11 }}
              >
                Validation
              </h4>
              <div style={{ fontSize: 12, whiteSpace: "pre-wrap" }}>
                {cfg.validation}
              </div>
            </>
          )}

          <h4 style={{ margin: "12px 0 6px", color: "#f4b23a", fontSize: 11 }}>
            Recent reconciles ({auditRows.length})
          </h4>
          {auditRows.length === 0 ? (
            <div style={{ color: "#6b757e", fontSize: 11 }}>
              No reconciles recorded yet.
            </div>
          ) : (
            <div>
              {auditRows.map((row, i) => (
                <AuditRow key={`${row.timestamp}-${i}`} row={row} />
              ))}
            </div>
          )}
        </>
      )}
    </Modal>
  );
}
