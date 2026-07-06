import { useRef, useState, type KeyboardEvent } from "react";
import { Modal } from "../../components/common/Modal";
import { ALL_STATUSES, type Task } from "../../lib/types";
import { moveEntry, updateEntry } from "../../lib/api";
import { useUI } from "../../store/ui";

const KEEP = "__keep__";
const BOOL_KEEP = "keep";
const PRIORITIES = ["high", "medium", "low"];
const EXECUTION_MODES = ["worktree", "current_branch"];
const MERGE_POLICIES = ["prompt_only", "auto_pr", "auto_merge"];
const MERGE_STRATEGIES = ["squash", "merge", "rebase"];

type BooleanChoice = "keep" | "true" | "false";

function boolPatchValue(value: BooleanChoice): boolean | undefined {
  if (value === "true") return true;
  if (value === "false") return false;
  return undefined;
}

function addTextPatch(patch: Record<string, unknown>, key: string, value: string) {
  if (value.trim()) patch[key] = value.trim();
}

function csv(value: string): string[] {
  return value.split(",").map((v) => v.trim()).filter(Boolean);
}

function focusable(root: HTMLElement | null): HTMLElement[] {
  if (!root) return [];
  return Array.from(root.querySelectorAll<HTMLElement>(
    'input:not([disabled]), select:not([disabled]), textarea:not([disabled]), button:not([disabled])',
  )).filter((el) => el.offsetParent !== null);
}

function BooleanSelect({
  label,
  value,
  onChange,
}: {
  label: string;
  value: BooleanChoice;
  onChange: (value: BooleanChoice) => void;
}) {
  return (
    <div className="field">
      <label>{label}</label>
      <select value={value} onChange={(e) => onChange(e.target.value as BooleanChoice)}>
        <option value={BOOL_KEEP}>keep</option>
        <option value="true">true</option>
        <option value="false">false</option>
      </select>
    </div>
  );
}

export function FeatureMetadataModal({
  feature,
  project,
  tasks,
  onClose,
  onDone,
}: {
  feature: string;
  project: string;
  tasks: Task[];
  onClose: () => void;
  onDone: () => void;
}) {
  const toast = useUI((s) => s.toast);
  const bodyRef = useRef<HTMLDivElement>(null);
  const [focusIndex, setFocusIndex] = useState(0);

  const [featurePriority, setFeaturePriority] = useState(KEEP);
  const [featureDependsOn, setFeatureDependsOn] = useState("");
  const [featureSchedule, setFeatureSchedule] = useState("");
  const [featureStartsAt, setFeatureStartsAt] = useState("");
  const [featureExpiresAt, setFeatureExpiresAt] = useState("");
  const [featureRunOnceAt, setFeatureRunOnceAt] = useState("");
  const [featureTimezone, setFeatureTimezone] = useState("");

  const [status, setStatus] = useState(KEEP);
  const [priority, setPriority] = useState(KEEP);
  const [moveProject, setMoveProject] = useState("");

  const [agent, setAgent] = useState("");
  const [model, setModel] = useState("");
  const [executionMode, setExecutionMode] = useState(KEEP);
  const [targetWorkdir, setTargetWorkdir] = useState("");
  const [completeOnIdle, setCompleteOnIdle] = useState<BooleanChoice>("keep");
  const [schedule, setSchedule] = useState("");
  const [scheduleEnabled, setScheduleEnabled] = useState<BooleanChoice>("keep");
  const [runOnceAt, setRunOnceAt] = useState("");
  const [startsAt, setStartsAt] = useState("");
  const [expiresAt, setExpiresAt] = useState("");
  const [timezone, setTimezone] = useState("");

  const [gitBranch, setGitBranch] = useState("");
  const [mergeTargetBranch, setMergeTargetBranch] = useState("");
  const [mergePolicy, setMergePolicy] = useState(KEEP);
  const [mergeStrategy, setMergeStrategy] = useState(KEEP);
  const [openPRBeforeMerge, setOpenPRBeforeMerge] = useState<BooleanChoice>("keep");
  const [busy, setBusy] = useState(false);

  function move(delta: 1 | -1) {
    const items = focusable(bodyRef.current);
    if (!items.length) return;
    const active = document.activeElement as HTMLElement | null;
    const current = Math.max(0, items.findIndex((el) => el === active));
    const next = (current + delta + items.length) % items.length;
    setFocusIndex(next);
    items[next].focus();
  }

  function handleKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    const target = e.target as HTMLElement | null;
    const tag = target?.tagName.toLowerCase();
    const isText = tag === "input" || tag === "textarea" || target?.isContentEditable;
    if (isText) {
      if (e.key === "Escape") target?.blur();
      return;
    }
    if (e.key === "j" || e.key === "ArrowDown") { e.preventDefault(); move(1); return; }
    if (e.key === "k" || e.key === "ArrowUp") { e.preventDefault(); move(-1); return; }
    if (e.key === "g") {
      e.preventDefault();
      const items = focusable(bodyRef.current);
      if (items[0]) { setFocusIndex(0); items[0].focus(); }
      return;
    }
    if (e.key === "G") {
      e.preventDefault();
      const items = focusable(bodyRef.current);
      const last = items.at(-1);
      if (last) { setFocusIndex(items.length - 1); last.focus(); }
      return;
    }
    if (e.key === "i" || e.key === "Enter") {
      e.preventDefault();
      const items = focusable(bodyRef.current);
      (items[focusIndex] ?? items[0])?.focus();
    }
  }

  async function save() {
    const patch: Record<string, unknown> = {};

    if (featurePriority !== KEEP) patch.feature_priority = featurePriority;
    const featureDeps = csv(featureDependsOn);
    if (featureDeps.length > 0) patch.feature_depends_on = featureDeps;
    addTextPatch(patch, "feature_schedule", featureSchedule);
    addTextPatch(patch, "feature_starts_at", featureStartsAt);
    addTextPatch(patch, "feature_expires_at", featureExpiresAt);
    addTextPatch(patch, "feature_run_once_at", featureRunOnceAt);
    addTextPatch(patch, "feature_timezone", featureTimezone);

    if (status !== KEEP) patch.status = status;
    if (priority !== KEEP) patch.priority = priority;

    addTextPatch(patch, "agent", agent);
    addTextPatch(patch, "model", model);
    if (executionMode !== KEEP) patch.execution_mode = executionMode;
    addTextPatch(patch, "target_workdir", targetWorkdir);
    const complete = boolPatchValue(completeOnIdle);
    if (complete !== undefined) patch.complete_on_idle = complete;
    addTextPatch(patch, "schedule", schedule);
    const enabled = boolPatchValue(scheduleEnabled);
    if (enabled !== undefined) patch.schedule_enabled = enabled;
    addTextPatch(patch, "run_once_at", runOnceAt);
    addTextPatch(patch, "starts_at", startsAt);
    addTextPatch(patch, "expires_at", expiresAt);
    addTextPatch(patch, "timezone", timezone);

    addTextPatch(patch, "git_branch", gitBranch);
    addTextPatch(patch, "merge_target_branch", mergeTargetBranch);
    if (mergePolicy !== KEEP) patch.merge_policy = mergePolicy;
    if (mergeStrategy !== KEEP) patch.merge_strategy = mergeStrategy;
    const openPR = boolPatchValue(openPRBeforeMerge);
    if (openPR !== undefined) patch.open_pr_before_merge = openPR;

    const targetProject = moveProject.trim();
    if (Object.keys(patch).length === 0 && !targetProject) {
      onClose();
      return;
    }
    setBusy(true);
    let ok = 0;
    let fail = 0;
    await Promise.all(tasks.map(async (task) => {
      try {
        if (Object.keys(patch).length > 0) await updateEntry(task.path, patch);
        if (targetProject) await moveEntry(task.path, targetProject);
        ok++;
      } catch {
        fail++;
      }
    }));
    setBusy(false);
    toast(fail ? `Updated ${ok}, ${fail} failed` : `Updated ${ok} feature tasks`, fail ? "error" : "success");
    onDone();
    onClose();
  }

  return (
    <Modal
      title={`Feature settings: ${feature}`}
      onClose={onClose}
      className="sheet-wide"
      footer={
        <>
          <span className="faint">j/k move · i edit · Esc leaves field · q closes</span>
          <button className="btn ghost" onClick={onClose} disabled={busy}>Cancel</button>
          <button className="btn primary" style={{ marginLeft: "auto" }} onClick={() => void save()} disabled={busy}>
            {busy ? "Applying..." : `Apply to ${tasks.length}`}
          </button>
        </>
      }
    >
      <div ref={bodyRef} onKeyDown={handleKeyDown} tabIndex={-1}>
        <p className="muted" style={{ marginTop: 0 }}>
          Project <strong>{project}</strong> · {tasks.length} task{tasks.length === 1 ? "" : "s"}. Only changed fields are applied to tasks in this feature.
        </p>

        <section className="field-section">
          <h3>Feature</h3>
          <div className="field-grid">
            <div className="field">
              <label>Feature Priority</label>
              <select data-autofocus="true" value={featurePriority} onChange={(e) => setFeaturePriority(e.target.value)}>
                <option value={KEEP}>keep</option>
                {PRIORITIES.map((p) => <option key={p} value={p}>{p}</option>)}
              </select>
            </div>
            <div className="field"><label>Feature Dependencies</label><input value={featureDependsOn} onChange={(e) => setFeatureDependsOn(e.target.value)} placeholder="comma-separated feature ids" /></div>
            <div className="field"><label>Feature Schedule</label><input value={featureSchedule} onChange={(e) => setFeatureSchedule(e.target.value)} placeholder="cron expression" /></div>
            <div className="field"><label>Feature Run Once At</label><input value={featureRunOnceAt} onChange={(e) => setFeatureRunOnceAt(e.target.value)} placeholder="RFC3339 timestamp" /></div>
            <div className="field"><label>Feature Starts At</label><input value={featureStartsAt} onChange={(e) => setFeatureStartsAt(e.target.value)} placeholder="RFC3339 timestamp" /></div>
            <div className="field"><label>Feature Expires At</label><input value={featureExpiresAt} onChange={(e) => setFeatureExpiresAt(e.target.value)} placeholder="RFC3339 timestamp" /></div>
            <div className="field"><label>Feature Timezone</label><input value={featureTimezone} onChange={(e) => setFeatureTimezone(e.target.value)} placeholder="America/Denver" /></div>
          </div>
        </section>

        <section className="field-section">
          <h3>Task</h3>
          <div className="field-grid">
            <div className="field">
              <label>Status</label>
              <select value={status} onChange={(e) => setStatus(e.target.value)}>
                <option value={KEEP}>keep</option>
                {ALL_STATUSES.map((s) => <option key={s} value={s}>{s}</option>)}
              </select>
            </div>
            <div className="field">
              <label>Priority</label>
              <select value={priority} onChange={(e) => setPriority(e.target.value)}>
                <option value={KEEP}>keep</option>
                {PRIORITIES.map((p) => <option key={p} value={p}>{p}</option>)}
              </select>
            </div>
            <div className="field"><label>Move to Project</label><input value={moveProject} onChange={(e) => setMoveProject(e.target.value)} placeholder="leave blank to keep" /></div>
          </div>
        </section>

        <section className="field-section">
          <h3>Execution</h3>
          <div className="field-grid">
            <div className="field"><label>Agent</label><input value={agent} onChange={(e) => setAgent(e.target.value)} /></div>
            <div className="field"><label>Model</label><input value={model} onChange={(e) => setModel(e.target.value)} /></div>
            <div className="field">
              <label>Execution Mode</label>
              <select value={executionMode} onChange={(e) => setExecutionMode(e.target.value)}>
                <option value={KEEP}>keep</option>
                {EXECUTION_MODES.map((m) => <option key={m} value={m}>{m}</option>)}
              </select>
            </div>
            <div className="field"><label>Target Workdir</label><input className="mono" value={targetWorkdir} onChange={(e) => setTargetWorkdir(e.target.value)} /></div>
            <BooleanSelect label="Complete On Idle" value={completeOnIdle} onChange={setCompleteOnIdle} />
            <div className="field"><label>Schedule</label><input value={schedule} onChange={(e) => setSchedule(e.target.value)} placeholder="cron expression" /></div>
            <BooleanSelect label="Schedule Enabled" value={scheduleEnabled} onChange={setScheduleEnabled} />
            <div className="field"><label>Run Once At</label><input value={runOnceAt} onChange={(e) => setRunOnceAt(e.target.value)} placeholder="RFC3339 timestamp" /></div>
            <div className="field"><label>Starts At</label><input value={startsAt} onChange={(e) => setStartsAt(e.target.value)} placeholder="RFC3339 timestamp" /></div>
            <div className="field"><label>Expires At</label><input value={expiresAt} onChange={(e) => setExpiresAt(e.target.value)} placeholder="RFC3339 timestamp" /></div>
            <div className="field"><label>Timezone</label><input value={timezone} onChange={(e) => setTimezone(e.target.value)} placeholder="America/Denver" /></div>
          </div>
        </section>

        <section className="field-section">
          <h3>Git &amp; Merge</h3>
          <div className="field-grid">
            <div className="field"><label>Git Branch</label><input value={gitBranch} onChange={(e) => setGitBranch(e.target.value)} /></div>
            <div className="field"><label>Merge Target Branch</label><input value={mergeTargetBranch} onChange={(e) => setMergeTargetBranch(e.target.value)} /></div>
            <div className="field">
              <label>Merge Policy</label>
              <select value={mergePolicy} onChange={(e) => setMergePolicy(e.target.value)}>
                <option value={KEEP}>keep</option>
                {MERGE_POLICIES.map((p) => <option key={p} value={p}>{p}</option>)}
              </select>
            </div>
            <div className="field">
              <label>Merge Strategy</label>
              <select value={mergeStrategy} onChange={(e) => setMergeStrategy(e.target.value)}>
                <option value={KEEP}>keep</option>
                {MERGE_STRATEGIES.map((s) => <option key={s} value={s}>{s}</option>)}
              </select>
            </div>
            <BooleanSelect label="Open PR Before Merge" value={openPRBeforeMerge} onChange={setOpenPRBeforeMerge} />
          </div>
        </section>
      </div>
    </Modal>
  );
}
