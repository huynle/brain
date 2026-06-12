import { useState } from "react";
import { Modal } from "../common/Modal";
import { MarkdownEditor } from "../editor/MarkdownEditor";
import { createEntry } from "../../lib/api";
import { ALL_STATUSES } from "../../lib/types";
import { useUI, ALL_PROJECTS } from "../../store/ui";

const PRIORITIES = ["high", "medium", "low"];

export function ComposeModal({
  kind,
  onClose,
  onCreated,
}: {
  kind: "task" | "note";
  onClose: () => void;
  onCreated?: (path: string) => void;
}) {
  const toast = useUI((s) => s.toast);
  const activeProject = useUI((s) => s.activeProject);
  const defaultProject = activeProject === ALL_PROJECTS ? "" : activeProject;

  const [title, setTitle] = useState("");
  const [project, setProject] = useState(defaultProject);
  const [status, setStatus] = useState("pending");
  const [priority, setPriority] = useState("medium");
  const [feature, setFeature] = useState("");
  const [agent, setAgent] = useState("");
  const [content, setContent] = useState("");
  const [busy, setBusy] = useState(false);

  const isTask = kind === "task";
  const canSave = title.trim() !== "" && project.trim() !== "";

  async function save() {
    setBusy(true);
    try {
      const res = await createEntry({
        type: kind,
        title: title.trim(),
        project: project.trim(),
        // content is required server-side; fall back to the title.
        content: content.trim() || title.trim(),
        ...(isTask
          ? {
              status,
              priority,
              ...(feature.trim() ? { feature_id: feature.trim() } : {}),
              ...(agent.trim() ? { agent: agent.trim() } : {}),
            }
          : { status: "active" }),
      });
      toast(`${isTask ? "Task" : "Entry"} created`, "success");
      onCreated?.(res.path);
      onClose();
    } catch (e) {
      toast(e instanceof Error ? e.message : "Create failed", "error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title={isTask ? "New task" : "New entry"}
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
            disabled={busy || !canSave}
          >
            {busy ? "Creating…" : "Create"}
          </button>
        </>
      }
    >
      <div className="field">
        <label>Title</label>
        <textarea
          rows={2}
          autoFocus
          placeholder={isTask ? "What needs doing?" : "Entry title"}
          value={title}
          onChange={(e) => setTitle(e.target.value)}
        />
      </div>
      <div className="field">
        <label>Project</label>
        <input
          placeholder="project id"
          value={project}
          onChange={(e) => setProject(e.target.value)}
        />
      </div>

      {isTask && (
        <>
          <div className="row" style={{ gap: "0.6rem" }}>
            <div className="field" style={{ flex: 1 }}>
              <label>Status</label>
              <select value={status} onChange={(e) => setStatus(e.target.value)}>
                {ALL_STATUSES.map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </select>
            </div>
            <div className="field" style={{ flex: 1 }}>
              <label>Priority</label>
              <select value={priority} onChange={(e) => setPriority(e.target.value)}>
                {PRIORITIES.map((p) => (
                  <option key={p} value={p}>
                    {p}
                  </option>
                ))}
              </select>
            </div>
          </div>
          <div className="row" style={{ gap: "0.6rem" }}>
            <div className="field" style={{ flex: 1 }}>
              <label>Feature</label>
              <input value={feature} onChange={(e) => setFeature(e.target.value)} />
            </div>
            <div className="field" style={{ flex: 1 }}>
              <label>Agent</label>
              <input value={agent} onChange={(e) => setAgent(e.target.value)} />
            </div>
          </div>
        </>
      )}

      <div className="field">
        <label>{isTask ? "Description" : "Content"}</label>
        <MarkdownEditor value={content} onChange={setContent} height="34vh" />
      </div>
    </Modal>
  );
}
