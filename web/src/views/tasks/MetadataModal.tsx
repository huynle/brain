import { useState } from "react";
import { Modal } from "../../components/common/Modal";
import { ALL_STATUSES, type Task } from "../../lib/types";
import { moveEntry, updateEntry } from "../../lib/api";
import { useUI } from "../../store/ui";

const PRIORITIES = ["high", "medium", "low"];
const EXECUTION_MODES = ["worktree", "current_branch"];
const MERGE_POLICIES = ["prompt_only", "auto_pr", "auto_merge"];
const MERGE_STRATEGIES = ["squash", "merge", "rebase"];

function csv(value: string): string[] {
  return value
    .split(",")
    .map((v) => v.trim())
    .filter(Boolean);
}

function textValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function optionalBoolean(value: unknown): boolean {
  return value === true;
}

function setChangedText(
  patch: Record<string, unknown>,
  key: string,
  next: string,
  previous: unknown,
) {
  if (next !== textValue(previous)) patch[key] = next;
}

function setChangedBoolean(
  patch: Record<string, unknown>,
  key: string,
  next: boolean,
  previous: unknown,
) {
  if (next !== optionalBoolean(previous)) patch[key] = next;
}

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
  const [feature, setFeature] = useState(task.feature_id || "");
  const [moveProject, setMoveProject] = useState("");
  const [agent, setAgent] = useState(task.agent || "");
  const [model, setModel] = useState(task.model || "");
  const [executionMode, setExecutionMode] = useState(task.execution_mode || "");
  const [targetWorkdir, setTargetWorkdir] = useState(task.target_workdir || task.workdir || "");
  const [directPrompt, setDirectPrompt] = useState(task.direct_prompt || "");
  const [completeOnIdle, setCompleteOnIdle] = useState(optionalBoolean(task.complete_on_idle));
  const [schedule, setSchedule] = useState(task.schedule || "");
  const [scheduleEnabled, setScheduleEnabled] = useState(optionalBoolean(task.schedule_enabled));
  const [runOnceAt, setRunOnceAt] = useState(task.run_once_at || "");
  const [startsAt, setStartsAt] = useState(task.starts_at || "");
  const [expiresAt, setExpiresAt] = useState(task.expires_at || "");
  const [timezone, setTimezone] = useState(task.timezone || "");
  const [gitBranch, setGitBranch] = useState(task.git_branch || "");
  const [mergeTargetBranch, setMergeTargetBranch] = useState(task.merge_target_branch || "");
  const [mergePolicy, setMergePolicy] = useState(task.merge_policy || "");
  const [mergeStrategy, setMergeStrategy] = useState(task.merge_strategy || "");
  const [openPRBeforeMerge, setOpenPRBeforeMerge] = useState(optionalBoolean(task.open_pr_before_merge));
  const [tags, setTags] = useState((task.tags || []).join(", "));
  const [busy, setBusy] = useState(false);

  async function save() {
    const patch: Record<string, unknown> = {};
    setChangedText(patch, "title", title, task.title);
    if (status !== task.status) patch.status = status;
    if (priority !== task.priority) patch.priority = priority;
    setChangedText(patch, "feature_id", feature, task.feature_id);
    setChangedText(patch, "agent", agent, task.agent);
    setChangedText(patch, "model", model, task.model);
    setChangedText(patch, "execution_mode", executionMode, task.execution_mode);
    setChangedText(patch, "target_workdir", targetWorkdir, task.target_workdir || task.workdir);
    setChangedText(patch, "direct_prompt", directPrompt, task.direct_prompt);
    setChangedBoolean(patch, "complete_on_idle", completeOnIdle, task.complete_on_idle);
    setChangedText(patch, "schedule", schedule, task.schedule);
    setChangedBoolean(patch, "schedule_enabled", scheduleEnabled, task.schedule_enabled);
    setChangedText(patch, "run_once_at", runOnceAt, task.run_once_at);
    setChangedText(patch, "starts_at", startsAt, task.starts_at);
    setChangedText(patch, "expires_at", expiresAt, task.expires_at);
    setChangedText(patch, "timezone", timezone, task.timezone);
    setChangedText(patch, "git_branch", gitBranch, task.git_branch);
    setChangedText(patch, "merge_target_branch", mergeTargetBranch, task.merge_target_branch);
    setChangedText(patch, "merge_policy", mergePolicy, task.merge_policy);
    setChangedText(patch, "merge_strategy", mergeStrategy, task.merge_strategy);
    setChangedBoolean(patch, "open_pr_before_merge", openPRBeforeMerge, task.open_pr_before_merge);

    const tagList = csv(tags);
    if (tagList.join(",") !== (task.tags || []).join(",")) patch.tags = tagList;

    const targetProject = moveProject.trim();
    if (Object.keys(patch).length === 0 && !targetProject) {
      onClose();
      return;
    }
    setBusy(true);
    try {
      if (Object.keys(patch).length > 0) await updateEntry(task.path, patch);
      if (targetProject) await moveEntry(task.path, targetProject);
      toast(targetProject ? "Task updated and moved" : "Task updated", "success");
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
      className="sheet-wide"
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
            {busy ? "Saving..." : "Save"}
          </button>
        </>
      }
    >
      <section className="field-section">
        <h3>Task</h3>
        <div className="field">
          <label>Title</label>
          <textarea rows={2} value={title} onChange={(e) => setTitle(e.target.value)} />
        </div>
        <div className="field-grid">
          <div className="field">
            <label>Status</label>
            <select value={status} onChange={(e) => setStatus(e.target.value as Task["status"])}>
              {ALL_STATUSES.map((s) => (
                <option key={s} value={s}>{s}</option>
              ))}
            </select>
          </div>
          <div className="field">
            <label>Priority</label>
            <select value={priority} onChange={(e) => setPriority(e.target.value)}>
              {PRIORITIES.map((p) => (
                <option key={p} value={p}>{p}</option>
              ))}
            </select>
          </div>
          <div className="field">
            <label>Feature ID</label>
            <input value={feature} onChange={(e) => setFeature(e.target.value)} />
          </div>
          <div className="field">
            <label>Move to Project</label>
            <input value={moveProject} onChange={(e) => setMoveProject(e.target.value)} placeholder="leave blank to keep" />
          </div>
        </div>
      </section>

      <section className="field-section">
        <h3>Execution</h3>
        <div className="field-grid">
          <div className="field">
            <label>Agent</label>
            <input value={agent} onChange={(e) => setAgent(e.target.value)} />
          </div>
          <div className="field">
            <label>Model</label>
            <input value={model} onChange={(e) => setModel(e.target.value)} />
          </div>
          <div className="field">
            <label>Execution Mode</label>
            <select value={executionMode} onChange={(e) => setExecutionMode(e.target.value)}>
              <option value=""></option>
              {EXECUTION_MODES.map((m) => (
                <option key={m} value={m}>{m}</option>
              ))}
            </select>
          </div>
          <div className="field">
            <label>Target Workdir</label>
            <input className="mono" value={targetWorkdir} onChange={(e) => setTargetWorkdir(e.target.value)} />
          </div>
        </div>
        <div className="field">
          <label>Direct Prompt</label>
          <textarea rows={4} value={directPrompt} onChange={(e) => setDirectPrompt(e.target.value)} />
        </div>
        <label className="field-check">
          <input type="checkbox" checked={completeOnIdle} onChange={(e) => setCompleteOnIdle(e.target.checked)} />
          <span>Complete On Idle</span>
        </label>
        <div className="field-grid">
          <div className="field">
            <label>Schedule</label>
            <input value={schedule} onChange={(e) => setSchedule(e.target.value)} placeholder="cron expression" />
          </div>
          <div className="field">
            <label>Run Once At</label>
            <input value={runOnceAt} onChange={(e) => setRunOnceAt(e.target.value)} placeholder="RFC3339 timestamp" />
          </div>
          <div className="field">
            <label>Starts At</label>
            <input value={startsAt} onChange={(e) => setStartsAt(e.target.value)} placeholder="RFC3339 timestamp" />
          </div>
          <div className="field">
            <label>Expires At</label>
            <input value={expiresAt} onChange={(e) => setExpiresAt(e.target.value)} placeholder="RFC3339 timestamp" />
          </div>
          <div className="field">
            <label>Timezone</label>
            <input value={timezone} onChange={(e) => setTimezone(e.target.value)} placeholder="America/Denver" />
          </div>
        </div>
        <label className="field-check">
          <input type="checkbox" checked={scheduleEnabled} onChange={(e) => setScheduleEnabled(e.target.checked)} />
          <span>Schedule Enabled</span>
        </label>
      </section>

      <section className="field-section">
        <h3>Git &amp; Merge</h3>
        <div className="field-grid">
          <div className="field">
            <label>Git Branch</label>
            <input value={gitBranch} onChange={(e) => setGitBranch(e.target.value)} />
          </div>
          <div className="field">
            <label>Merge Target Branch</label>
            <input value={mergeTargetBranch} onChange={(e) => setMergeTargetBranch(e.target.value)} />
          </div>
          <div className="field">
            <label>Merge Policy</label>
            <select value={mergePolicy} onChange={(e) => setMergePolicy(e.target.value)}>
              <option value=""></option>
              {MERGE_POLICIES.map((p) => (
                <option key={p} value={p}>{p}</option>
              ))}
            </select>
          </div>
          <div className="field">
            <label>Merge Strategy</label>
            <select value={mergeStrategy} onChange={(e) => setMergeStrategy(e.target.value)}>
              <option value=""></option>
              {MERGE_STRATEGIES.map((s) => (
                <option key={s} value={s}>{s}</option>
              ))}
            </select>
          </div>
        </div>
        <label className="field-check">
          <input type="checkbox" checked={openPRBeforeMerge} onChange={(e) => setOpenPRBeforeMerge(e.target.checked)} />
          <span>Open PR Before Merge</span>
        </label>
      </section>

      <section className="field-section">
        <h3>Other</h3>
        <div className="field">
          <label>Tags (comma-separated)</label>
          <input value={tags} onChange={(e) => setTags(e.target.value)} />
        </div>
      </section>
    </Modal>
  );
}
