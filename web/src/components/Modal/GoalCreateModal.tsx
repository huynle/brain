/**
 * GoalCreateModal — create a goal for a fixed scope.
 *
 * The scope (project, optional feature, optional task) is prefilled and
 * read-only: every opener (feature/task "Set goal…" verb, FeatureModal's
 * "Add goal", CardGoals' "New goal") already knows exactly what it is
 * scoping the goal to, and letting the form retarget it would silently
 * decouple the goal from the surface that spawned it.
 *
 * Assembly and validation are pure functions in lib/actions/goalActions
 * (`assembleCreateGoalRequest` / `goalCreateValidationError`) so the wire
 * shape is unit-tested without rendering. On success: invalidate the
 * goals queries (no SSE for goals) and open the new goal's modal.
 */
import { useState, type CSSProperties } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { Modal } from "../common/Modal";
import { useModal } from "../../store/modal";
import { useUI } from "../../store/ui";
import { createGoal, ApiError } from "../../lib/api";
import {
  assembleCreateGoalRequest,
  goalCreateValidationError,
  type GoalCreateForm,
} from "../../lib/actions/goalActions";

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

const LABEL_STYLE: CSSProperties = { fontSize: 11, color: "#9098a1" };

export function GoalCreateModal(): JSX.Element {
  const target = useModal((s) => s.target);
  const openModal = useModal((s) => s.open);
  const close = useModal((s) => s.close);
  const toast = useUI((s) => s.toast);
  const queryClient = useQueryClient();

  // Scope comes from the opener; accept both camel and snake spellings
  // so callers can pass API-shaped targets straight through.
  const project =
    (target?.project as string | undefined) ??
    (target?.projectId as string | undefined) ??
    "";
  const featureId =
    (target?.featureId as string | undefined) ??
    (target?.feature_id as string | undefined) ??
    "";
  const taskId =
    (target?.taskId as string | undefined) ??
    (target?.task_id as string | undefined) ??
    "";

  const [title, setTitle] = useState("");
  const [criteria, setCriteria] = useState("");
  const [validation, setValidation] = useState("");
  const [prompt, setPrompt] = useState("");
  const [agent, setAgent] = useState("");
  const [model, setModel] = useState("");
  const [executor, setExecutor] = useState("");
  const [steeringEnabled, setSteeringEnabled] = useState(true);
  const [cooldown, setCooldown] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  function currentForm(): GoalCreateForm {
    const cooldownNum = cooldown.trim() ? Number(cooldown) : undefined;
    return {
      project,
      featureId: featureId || undefined,
      taskId: taskId || undefined,
      title,
      criteria,
      validation,
      prompt,
      agent,
      model,
      executor,
      steeringEnabled,
      steeringCooldownMinutes: cooldownNum,
    };
  }

  async function submit() {
    const form = currentForm();
    const invalid = goalCreateValidationError(form);
    if (invalid) {
      setError(invalid);
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      const summary = await createGoal(assembleCreateGoalRequest(form));
      await queryClient.invalidateQueries({ queryKey: ["goals"] });
      toast(`Created goal ${summary.title || summary.goal_id}`, "success");
      // Land the user on the goal they just made.
      openModal("goal", {
        goalId: summary.goal_id,
        projectId: summary.project,
      });
    } catch (err) {
      setError(
        err instanceof ApiError
          ? err.message
          : ((err as Error).message ?? "unknown error"),
      );
      setSubmitting(false);
    }
  }

  const scope = [
    project || "—",
    featureId && `feature ${featureId}`,
    taskId && `task ${taskId.slice(0, 8)}`,
  ]
    .filter(Boolean)
    .join(" / ");

  return (
    <Modal
      title="New goal"
      onClose={close}
      footer={
        <>
          <button onClick={close} disabled={submitting}>
            Cancel
          </button>
          <button
            className="primary"
            onClick={() => void submit()}
            disabled={submitting}
          >
            {submitting ? "Creating…" : "Create goal"}
          </button>
        </>
      }
    >
      <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
        <div className="kv-grid">
          <div className="k">Scope</div>
          <div className="v" title="Fixed by where you opened this from">
            {scope}
          </div>
        </div>

        <label style={LABEL_STYLE}>
          Title *
          <input
            style={FIELD_STYLE}
            data-autofocus="true"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="Keep the auth flow green"
          />
        </label>
        <label style={LABEL_STYLE}>
          Success criteria
          <textarea
            style={{ ...FIELD_STYLE, minHeight: 56 }}
            value={criteria}
            onChange={(e) => setCriteria(e.target.value)}
            placeholder="What must be true for this goal to count as done"
          />
        </label>
        <label style={LABEL_STYLE}>
          Validation
          <textarea
            style={{ ...FIELD_STYLE, minHeight: 42 }}
            value={validation}
            onChange={(e) => setValidation(e.target.value)}
            placeholder="How completion is checked (tests, commands…)"
          />
        </label>
        <label style={LABEL_STYLE}>
          Prompt (for generated tasks)
          <textarea
            style={{ ...FIELD_STYLE, minHeight: 42 }}
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            placeholder="Extra instructions for the agent; criteria are always included"
          />
        </label>

        <div style={{ display: "flex", gap: 8 }}>
          <label style={{ ...LABEL_STYLE, flex: 1 }}>
            Agent
            <input
              style={FIELD_STYLE}
              value={agent}
              onChange={(e) => setAgent(e.target.value)}
              placeholder="optional"
            />
          </label>
          <label style={{ ...LABEL_STYLE, flex: 1 }}>
            Model
            <input
              style={FIELD_STYLE}
              value={model}
              onChange={(e) => setModel(e.target.value)}
              placeholder="optional"
            />
          </label>
          <label style={{ ...LABEL_STYLE, flex: 1 }}>
            Executor
            <input
              style={FIELD_STYLE}
              value={executor}
              onChange={(e) => setExecutor(e.target.value)}
              placeholder="optional"
            />
          </label>
        </div>

        <div style={{ display: "flex", gap: 12, alignItems: "center" }}>
          <label
            style={{
              ...LABEL_STYLE,
              display: "flex",
              gap: 6,
              alignItems: "center",
            }}
          >
            <input
              type="checkbox"
              checked={steeringEnabled}
              onChange={(e) => setSteeringEnabled(e.target.checked)}
            />
            Steer live sessions
          </label>
          <label style={LABEL_STYLE}>
            Cooldown (min)
            <input
              style={{ ...FIELD_STYLE, width: 70, marginLeft: 6 }}
              placeholder="15"
              value={cooldown}
              disabled={!steeringEnabled}
              onChange={(e) => setCooldown(e.target.value)}
            />
          </label>
        </div>

        {error && (
          <div style={{ color: "#d96060", fontSize: 11 }}>{error}</div>
        )}
      </div>
    </Modal>
  );
}
