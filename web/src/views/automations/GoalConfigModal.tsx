import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Modal } from "../../components/common/Modal";
import { Pill } from "../../components/common/Badge";
import { ALL_STATUSES, type GoalSummary } from "../../lib/types";
import { goalAudit, updateGoal } from "../../lib/api";
import { useUI } from "../../store/ui";
import { relativeTime } from "../../lib/format";

const TRIGGER_SOURCES = ["task", "feature", "both"];

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

export function GoalConfigModal({
  goal,
  onClose,
}: {
  goal: GoalSummary;
  onClose: () => void;
}) {
  const toast = useUI((s) => s.toast);
  const qc = useQueryClient();
  const cfg = goal.config || { id: goal.goal_id };
  const action = goal.action || {};

  const [title, setTitle] = useState(goal.title || "");
  const [criteria, setCriteria] = useState(cfg.criteria || "");
  const [validation, setValidation] = useState(cfg.validation || "");
  const [workdir, setWorkdir] = useState(cfg.workdir || "");
  const [triggerSource, setTriggerSource] = useState(cfg.trigger_source || "task");
  const [complete, setComplete] = useState<string[]>(cfg.complete_statuses || []);
  const [blocked, setBlocked] = useState<string[]>(cfg.blocked_statuses || []);
  const [agent, setAgent] = useState(action.agent || "");
  const [model, setModel] = useState(action.model || "");
  const [busy, setBusy] = useState(false);

  const auditQ = useQuery({
    queryKey: ["goal-audit", goal.goal_id],
    queryFn: () => goalAudit(goal.goal_id, 5),
  });

  function toggle(list: string[], set: (v: string[]) => void, s: string) {
    set(list.includes(s) ? list.filter((x) => x !== s) : [...list, s]);
  }

  async function save() {
    setBusy(true);
    try {
      await updateGoal(goal.goal_id, {
        title,
        criteria,
        validation,
        workdir,
        trigger_source: triggerSource,
        complete_statuses: complete,
        blocked_statuses: blocked,
        action: { ...action, agent, model },
      });
      toast("Goal updated", "success");
      void qc.invalidateQueries({ queryKey: ["goals"] });
      onClose();
    } catch (e) {
      toast(e instanceof Error ? e.message : "Update failed", "error");
    } finally {
      setBusy(false);
    }
  }

  const lastAudit = auditQ.data?.audit?.[0];

  return (
    <Modal
      title="Goal configuration"
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
        <label>Objective</label>
        <textarea rows={2} value={title} onChange={(e) => setTitle(e.target.value)} />
      </div>
      <div className="field">
        <label>Success criteria</label>
        <textarea
          rows={3}
          value={criteria}
          onChange={(e) => setCriteria(e.target.value)}
        />
      </div>
      <div className="field">
        <label>Validation</label>
        <textarea
          rows={3}
          value={validation}
          onChange={(e) => setValidation(e.target.value)}
        />
      </div>
      <div className="field">
        <label>Trigger source</label>
        <select
          value={triggerSource}
          onChange={(e) => setTriggerSource(e.target.value)}
        >
          {TRIGGER_SOURCES.map((t) => (
            <option key={t} value={t}>
              {t}
            </option>
          ))}
        </select>
      </div>
      <div className="field">
        <label>Workdir</label>
        <input className="mono" value={workdir} onChange={(e) => setWorkdir(e.target.value)} />
      </div>
      <div className="row" style={{ gap: "0.6rem" }}>
        <div className="field" style={{ flex: 1 }}>
          <label>Agent</label>
          <input value={agent} onChange={(e) => setAgent(e.target.value)} />
        </div>
        <div className="field" style={{ flex: 1 }}>
          <label>Model</label>
          <input value={model} onChange={(e) => setModel(e.target.value)} />
        </div>
      </div>
      <div className="field">
        <label>Complete statuses</label>
        <StatusChips selected={complete} onToggle={(s) => toggle(complete, setComplete, s)} />
      </div>
      <div className="field">
        <label>Blocked statuses</label>
        <StatusChips selected={blocked} onToggle={(s) => toggle(blocked, setBlocked, s)} />
      </div>

      {lastAudit && (
        <div className="field">
          <label>Last reconcile</label>
          <div className="card section-pad" style={{ fontSize: 13 }}>
            <div className="row" style={{ gap: "0.4rem" }}>
              <Pill
                color={
                  lastAudit.decision === "complete"
                    ? "var(--green)"
                    : lastAudit.decision === "block"
                      ? "var(--red)"
                      : "var(--yellow)"
                }
              >
                {lastAudit.decision}
              </Pill>
              <span className="faint">{relativeTime(lastAudit.timestamp)}</span>
            </div>
            <div className="muted" style={{ marginTop: "0.4rem" }}>
              {lastAudit.reason}
            </div>
          </div>
        </div>
      )}
    </Modal>
  );
}
