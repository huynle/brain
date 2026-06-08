import { useState } from "react";
import { Modal } from "../../components/common/Modal";
import { ALL_STATUSES, type Task } from "../../lib/types";
import { updateEntry } from "../../lib/api";
import { useUI } from "../../store/ui";

const PRIORITIES = ["high", "medium", "low"];

export function MetadataModal({
  task,
  onClose,
}: {
  task: Task;
  onClose: () => void;
}) {
  const toast = useUI((s) => s.toast);
  const [title, setTitle] = useState(task.title || "");
  const [status, setStatus] = useState(task.status);
  const [priority, setPriority] = useState(task.priority || "medium");
  const [agent, setAgent] = useState(task.agent || "");
  const [model, setModel] = useState(task.model || "");
  const [feature, setFeature] = useState(task.feature_id || "");
  const [tags, setTags] = useState((task.tags || []).join(", "));
  const [workdir, setWorkdir] = useState(task.workdir || "");
  const [busy, setBusy] = useState(false);

  async function save() {
    const patch: Record<string, unknown> = {};
    if (title !== task.title) patch.title = title;
    if (status !== task.status) patch.status = status;
    if (priority !== task.priority) patch.priority = priority;
    if (agent !== (task.agent || "")) patch.agent = agent;
    if (model !== (task.model || "")) patch.model = model;
    if (feature !== (task.feature_id || "")) patch.feature_id = feature;
    if (workdir !== (task.workdir || "")) patch.workdir = workdir;
    const tagList = tags
      .split(",")
      .map((t) => t.trim())
      .filter(Boolean);
    if (tagList.join(",") !== (task.tags || []).join(","))
      patch.tags = tagList;

    if (Object.keys(patch).length === 0) {
      onClose();
      return;
    }
    setBusy(true);
    try {
      await updateEntry(task.path, patch);
      toast("Task updated", "success");
      onClose();
    } catch (e) {
      toast(e instanceof Error ? e.message : "Update failed", "error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title="Edit task"
      onClose={onClose}
      footer={
        <>
          <button className="btn ghost" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button
            className="btn primary"
            style={{ marginLeft: "auto" }}
            onClick={() => void save()}
            disabled={busy}
          >
            {busy ? "Saving…" : "Save"}
          </button>
        </>
      }
    >
      <div className="field">
        <label>Title</label>
        <textarea
          rows={2}
          value={title}
          onChange={(e) => setTitle(e.target.value)}
        />
      </div>
      <div className="field">
        <label>Status</label>
        <select value={status} onChange={(e) => setStatus(e.target.value as Task["status"])}>
          {ALL_STATUSES.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
      </div>
      <div className="field">
        <label>Priority</label>
        <select value={priority} onChange={(e) => setPriority(e.target.value)}>
          {PRIORITIES.map((p) => (
            <option key={p} value={p}>
              {p}
            </option>
          ))}
        </select>
      </div>
      <div className="field">
        <label>Feature</label>
        <input value={feature} onChange={(e) => setFeature(e.target.value)} />
      </div>
      <div className="field">
        <label>Agent</label>
        <input value={agent} onChange={(e) => setAgent(e.target.value)} />
      </div>
      <div className="field">
        <label>Model</label>
        <input value={model} onChange={(e) => setModel(e.target.value)} />
      </div>
      <div className="field">
        <label>Tags (comma-separated)</label>
        <input value={tags} onChange={(e) => setTags(e.target.value)} />
      </div>
      <div className="field">
        <label>Workdir</label>
        <input
          className="mono"
          value={workdir}
          onChange={(e) => setWorkdir(e.target.value)}
        />
      </div>
    </Modal>
  );
}
