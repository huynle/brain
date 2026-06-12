import { useState } from "react";
import { Modal } from "../../components/common/Modal";
import { ALL_STATUSES, type Task } from "../../lib/types";
import { updateEntry } from "../../lib/api";
import { useUI } from "../../store/ui";

const KEEP = "__keep__";
const PRIORITIES = ["high", "medium", "low"];

/** Edit a subset of fields across many tasks; only changed fields are applied. */
export function BatchMetadataModal({
  tasks,
  onClose,
  onDone,
}: {
  tasks: Task[];
  onClose: () => void;
  onDone: () => void;
}) {
  const toast = useUI((s) => s.toast);
  const [status, setStatus] = useState(KEEP);
  const [priority, setPriority] = useState(KEEP);
  const [feature, setFeature] = useState("");
  const [agent, setAgent] = useState("");
  const [busy, setBusy] = useState(false);

  async function save() {
    const patch: Record<string, unknown> = {};
    if (status !== KEEP) patch.status = status;
    if (priority !== KEEP) patch.priority = priority;
    if (feature.trim()) patch.feature_id = feature.trim();
    if (agent.trim()) patch.agent = agent.trim();
    if (Object.keys(patch).length === 0) {
      onClose();
      return;
    }
    setBusy(true);
    let ok = 0;
    let fail = 0;
    await Promise.all(
      tasks.map((t) =>
        updateEntry(t.path, patch).then(
          () => ok++,
          () => fail++,
        ),
      ),
    );
    setBusy(false);
    toast(
      fail ? `Updated ${ok}, ${fail} failed` : `Updated ${ok} tasks`,
      fail ? "error" : "success",
    );
    onDone();
    onClose();
  }

  return (
    <Modal
      title={`Edit ${tasks.length} tasks`}
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
            {busy ? "Applying…" : `Apply to ${tasks.length}`}
          </button>
        </>
      }
    >
      <p className="muted" style={{ marginTop: 0 }}>
        Only fields you change are applied. Leave others as “keep”.
      </p>
      <div className="field">
        <label>Status</label>
        <select value={status} onChange={(e) => setStatus(e.target.value)}>
          <option value={KEEP}>— keep —</option>
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
          <option value={KEEP}>— keep —</option>
          {PRIORITIES.map((p) => (
            <option key={p} value={p}>
              {p}
            </option>
          ))}
        </select>
      </div>
      <div className="field">
        <label>Feature (set)</label>
        <input value={feature} onChange={(e) => setFeature(e.target.value)} />
      </div>
      <div className="field">
        <label>Agent (set)</label>
        <input value={agent} onChange={(e) => setAgent(e.target.value)} />
      </div>
    </Modal>
  );
}
