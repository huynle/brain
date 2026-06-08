import { useState } from "react";
import { Modal } from "../../components/common/Modal";
import { ALL_STATUSES } from "../../lib/types";
import { createGoal } from "../../lib/api";
import { useUI, ALL_PROJECTS } from "../../store/ui";

const TRIGGER_SOURCES = ["task", "feature", "both"];

function slugId(title: string): string {
  const slug =
    title
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 40) || "goal";
  const rand = Math.floor(Math.random() * 0xffff).toString(16).padStart(4, "0");
  return `${slug}-${rand}`;
}

function StatusChips({
  selected,
  onToggle,
}: {
  selected: string[];
  onToggle: (s: string) => void;
}) {
  const set = new Set(selected);
  return (
    <div className="chip-grid">
      {ALL_STATUSES.map((s) => (
        <button
          key={s}
          type="button"
          className={`chip-toggle ${set.has(s) ? "on" : ""}`}
          onClick={() => onToggle(s)}
        >
          <span>{set.has(s) ? "☑" : "☐"}</span>
          {s}
        </button>
      ))}
    </div>
  );
}

export function NewGoalModal({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated?: () => void;
}) {
  const toast = useUI((s) => s.toast);
  const activeProject = useUI((s) => s.activeProject);
  const defaultProject = activeProject === ALL_PROJECTS ? "" : activeProject;

  const [title, setTitle] = useState("");
  const [project, setProject] = useState(defaultProject);
  const [feature, setFeature] = useState("");
  const [criteria, setCriteria] = useState("");
  const [validation, setValidation] = useState("");
  const [workdir, setWorkdir] = useState("");
  const [triggerSource, setTriggerSource] = useState("task");
  const [agent, setAgent] = useState("");
  const [model, setModel] = useState("");
  const [complete, setComplete] = useState<string[]>([]);
  const [blocked, setBlocked] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);

  const canSave = title.trim() !== "" && project.trim() !== "";

  function toggle(list: string[], set: (v: string[]) => void, s: string) {
    set(list.includes(s) ? list.filter((x) => x !== s) : [...list, s]);
  }

  async function save() {
    setBusy(true);
    try {
      await createGoal({
        project: project.trim(),
        title: title.trim(),
        ...(feature.trim() ? { feature_id: feature.trim() } : {}),
        content: criteria.trim() || title.trim(),
        config: {
          id: slugId(title),
          criteria: criteria.trim() || undefined,
          validation: validation.trim() || undefined,
          workdir: workdir.trim() || undefined,
          trigger_source: triggerSource,
          ...(complete.length ? { complete_statuses: complete } : {}),
          ...(blocked.length ? { blocked_statuses: blocked } : {}),
        },
        // action.type is required server-side; "prompt" is the standard kind.
        action: {
          type: "prompt",
          ...(agent.trim() ? { agent: agent.trim() } : {}),
          ...(model.trim() ? { model: model.trim() } : {}),
        },
      });
      toast("Goal created", "success");
      onCreated?.();
      onClose();
    } catch (e) {
      toast(e instanceof Error ? e.message : "Create failed", "error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title="New goal"
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
            {busy ? "Creating…" : "Create goal"}
          </button>
        </>
      }
    >
      <div className="field">
        <label>Objective (title)</label>
        <textarea
          rows={2}
          autoFocus
          placeholder="What should this goal achieve?"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
        />
      </div>
      <div className="row" style={{ gap: "0.6rem" }}>
        <div className="field" style={{ flex: 1 }}>
          <label>Project</label>
          <input
            placeholder="project id"
            value={project}
            onChange={(e) => setProject(e.target.value)}
          />
        </div>
        <div className="field" style={{ flex: 1 }}>
          <label>Feature (optional)</label>
          <input value={feature} onChange={(e) => setFeature(e.target.value)} />
        </div>
      </div>
      <div className="field">
        <label>Success criteria</label>
        <textarea
          rows={3}
          placeholder="When is this goal complete?"
          value={criteria}
          onChange={(e) => setCriteria(e.target.value)}
        />
      </div>
      <div className="field">
        <label>Validation (optional)</label>
        <textarea
          rows={2}
          value={validation}
          onChange={(e) => setValidation(e.target.value)}
        />
      </div>
      <div className="row" style={{ gap: "0.6rem" }}>
        <div className="field" style={{ flex: 1 }}>
          <label>Trigger source</label>
          <select value={triggerSource} onChange={(e) => setTriggerSource(e.target.value)}>
            {TRIGGER_SOURCES.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
        </div>
        <div className="field" style={{ flex: 1 }}>
          <label>Workdir (optional)</label>
          <input className="mono" value={workdir} onChange={(e) => setWorkdir(e.target.value)} />
        </div>
      </div>
      <div className="row" style={{ gap: "0.6rem" }}>
        <div className="field" style={{ flex: 1 }}>
          <label>Agent (optional)</label>
          <input value={agent} onChange={(e) => setAgent(e.target.value)} />
        </div>
        <div className="field" style={{ flex: 1 }}>
          <label>Model (optional)</label>
          <input value={model} onChange={(e) => setModel(e.target.value)} />
        </div>
      </div>
      <div className="field">
        <label>Complete statuses (optional — defaults applied if empty)</label>
        <StatusChips selected={complete} onToggle={(s) => toggle(complete, setComplete, s)} />
      </div>
      <div className="field">
        <label>Blocked statuses (optional)</label>
        <StatusChips selected={blocked} onToggle={(s) => toggle(blocked, setBlocked, s)} />
      </div>
    </Modal>
  );
}
