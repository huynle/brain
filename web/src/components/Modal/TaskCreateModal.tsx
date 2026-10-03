import { useMemo, useState, type CSSProperties } from "react";

import { Modal } from "../common/Modal";
import { createEntry } from "../../lib/api";
import { useProposedTaskRunnerCandidates } from "../../hooks/useRunnerCandidates";
import { runnerLabel } from "../../lib/runnerName";
import { useUI } from "../../store/ui";

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

export interface TaskCreateModalProps {
  projectId: string;
  featureIds: string[];
  onClose: () => void;
}

export function TaskCreateModal({
  projectId,
  featureIds,
  onClose,
}: TaskCreateModalProps): JSX.Element {
  const toast = useUI((s) => s.toast);
  const [title, setTitle] = useState("");
  const [content, setContent] = useState("");
  const [featureId, setFeatureId] = useState("");
  const [executor, setExecutor] = useState("");
  const [capabilities, setCapabilities] = useState("");
  const [runnerId, setRunnerId] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const requiresCapability = useMemo(
    () => capabilities.split(",").map((item) => item.trim()).filter(Boolean),
    [capabilities],
  );
  const candidateSpec = useMemo(
    () => ({
      ...(featureId ? { feature_id: featureId } : {}),
      ...(executor ? { executor } : {}),
      ...(requiresCapability.length > 0
        ? { requires_capability: requiresCapability }
        : {}),
    }),
    [executor, featureId, requiresCapability],
  );
  const candidates = useProposedTaskRunnerCandidates(
    projectId,
    candidateSpec,
  );
  const compatible = (candidates.data?.candidates ?? []).filter(
    (candidate) => candidate.compatible,
  );

  async function submit() {
    if (!title.trim()) {
      setError("Title is required.");
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      const created = await createEntry({
        type: "task",
        title: title.trim(),
        content,
        project: projectId,
        status: "pending",
        ...(featureId ? { feature_id: featureId } : {}),
        ...(executor ? { executor } : {}),
        ...(requiresCapability.length > 0
          ? { requires_capability: requiresCapability }
          : {}),
        ...(runnerId
          ? { runner_id: runnerId, assignment_intent: "assign" }
          : {}),
      });
      toast(`Created task ${created.title || created.id || created.path}`, "success");
      onClose();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
      setSubmitting(false);
    }
  }

  return (
    <Modal
      title={`New task · ${projectId}`}
      onClose={onClose}
      footer={
        <>
          <button type="button" onClick={onClose} disabled={submitting}>Cancel</button>
          <button type="button" className="primary" onClick={() => void submit()} disabled={submitting || candidates.isLoading}>
            {submitting ? "Creating…" : "Create task"}
          </button>
        </>
      }
    >
      <div style={{ display: "flex", flexDirection: "column", gap: 9 }}>
        <label style={LABEL_STYLE}>
          Title *
          <input style={FIELD_STYLE} data-autofocus="true" value={title} onChange={(event) => setTitle(event.target.value)} placeholder="Describe the next concrete outcome" />
        </label>
        <label style={LABEL_STYLE}>
          Instructions
          <textarea style={{ ...FIELD_STYLE, minHeight: 88 }} value={content} onChange={(event) => setContent(event.target.value)} placeholder="Context, constraints, and acceptance criteria" />
        </label>
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
          <label style={LABEL_STYLE}>
            Feature
            <input style={FIELD_STYLE} list="task-create-features" value={featureId} onChange={(event) => { setFeatureId(event.target.value); setRunnerId(""); }} placeholder="none (standalone)" />
            <datalist id="task-create-features">{featureIds.map((id) => <option key={id} value={id} />)}</datalist>
          </label>
          <label style={LABEL_STYLE}>
            Executor
            <input style={FIELD_STYLE} value={executor} onChange={(event) => { setExecutor(event.target.value); setRunnerId(""); }} placeholder="project default" />
          </label>
        </div>
        <label style={LABEL_STYLE}>
          Required capabilities
          <input style={FIELD_STYLE} value={capabilities} onChange={(event) => { setCapabilities(event.target.value); setRunnerId(""); }} placeholder="gpu, docker (comma-separated)" />
        </label>
        <label style={LABEL_STYLE}>
          Runner assignment
          <select style={FIELD_STYLE} value={runnerId} onChange={(event) => setRunnerId(event.target.value)} disabled={candidates.isLoading || candidates.isError}>
            <option value="">Automatic scheduling</option>
            {compatible.map((candidate) => (
              <option key={candidate.runner.runner_id} value={candidate.runner.runner_id}>
                {runnerLabel(candidate.runner)}{candidate.available ? "" : " · assign for later"}
              </option>
            ))}
          </select>
        </label>
        {candidates.isLoading && <div style={LABEL_STYLE}>Checking durable runner compatibility…</div>}
        {candidates.isError && <div className="modal-error">Runner compatibility could not be loaded. Automatic scheduling remains available.</div>}
        {!candidates.isLoading && !candidates.isError && compatible.length === 0 && (
          <div style={LABEL_STYLE}>No compatible runner can be pinned for this task specification.</div>
        )}
        {featureId && candidates.data?.assigned_runner_id && (
          <div style={LABEL_STYLE}>This feature is currently assigned to {candidates.data.assigned_runner_id}; creating the task will preserve and revalidate that feature-wide pin.</div>
        )}
        {error && <div className="modal-error">{error}</div>}
      </div>
    </Modal>
  );
}
