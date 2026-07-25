import { useMemo, useRef, useState, type FormEvent } from "react";
import { Modal } from "../../components/common/Modal";
import { checkoutFeature, runBlockedInspectorNow } from "../../lib/api";
import type { FeatureCheckoutOptions, Task } from "../../lib/types";
import { useUI } from "../../store/ui";
import {
  computeFeatureState,
  deriveCheckoutDefaults,
} from "./featureActions";

type View = "menu" | "checkout" | "confirmForce";

const MERGE_POLICIES = ["prompt_only", "auto_pr", "auto_merge"];
const MERGE_STRATEGIES = ["squash", "merge", "rebase"];
const REMOTE_BRANCH_POLICIES = ["keep", "delete"];
const EXECUTION_MODES = ["worktree", "current_branch"];
const CHECKOUT_MODES: Array<{ value: string; label: string; help: string }> = [
  {
    value: "ai",
    label: "AI",
    help: "Runs the feature-checkout skill: reviews work vs original requests, then merges.",
  },
  {
    value: "simple",
    label: "Simple",
    help: "Deterministic squash-merge script. Fast; no review; fails on merge conflicts.",
  },
];

// FeatureActionsModal is opened from the Tasks view when Enter is pressed on
// a non-UNGROUPED feature header. It surfaces state-aware actions that don't
// exist as first-class actions in the surrounding row keymap.
export function FeatureActionsModal({
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
  const state = useMemo(() => computeFeatureState(tasks), [tasks]);
  const [view, setView] = useState<View>("menu");
  // "review" = normal path (gated by allCompleted); "force" = force-merge
  // (any state, requires an extra Yes/No confirmation).
  const [mode, setMode] = useState<"review" | "force">("review");
  const [busy, setBusy] = useState(false);
  const [opts, setOpts] = useState<FeatureCheckoutOptions>(() =>
    deriveCheckoutDefaults(tasks),
  );
  // Ref used by the checkout submit path so the confirmation prompt can call
  // back into the same submit flow without state juggling.
  const submitRef = useRef<() => Promise<void>>(() => Promise.resolve());

  function openCheckout(next: "review" | "force") {
    setMode(next);
    setView("checkout");
  }

  async function submitCheckout() {
    setBusy(true);
    try {
      // Filter out empty strings so we don't clobber server defaults with "".
      const body: FeatureCheckoutOptions = {};
      for (const [k, v] of Object.entries(opts)) {
        if (v === undefined || v === null || v === "") continue;
        (body as Record<string, unknown>)[k] = v;
      }
      const result = await checkoutFeature(project, feature, body);
      toast(
        result.created
          ? `Checkout task created${result.task?.path ? ` (${result.task.path})` : ""}`
          : "Checkout task already exists for this feature",
        "success",
      );
      onDone();
      onClose();
    } catch (err) {
      toast(
        `Checkout failed: ${err instanceof Error ? err.message : String(err)}`,
        "error",
      );
    } finally {
      setBusy(false);
    }
  }
  submitRef.current = submitCheckout;

  function onCheckoutSubmit(e: FormEvent) {
    e.preventDefault();
    if (mode === "force") {
      // Bounce into the confirmation view; the Yes button calls submitRef.
      setView("confirmForce");
      return;
    }
    void submitCheckout();
  }

  async function runInspector() {
    setBusy(true);
    try {
      const resp = await runBlockedInspectorNow(project, feature);
      toast(`Blocked inspector queued (${resp.path})`, "success");
      onDone();
      onClose();
    } catch (err) {
      toast(
        `Failed to queue inspector: ${err instanceof Error ? err.message : String(err)}`,
        "error",
      );
    } finally {
      setBusy(false);
    }
  }

  const summary = `${state.taskCount} task${state.taskCount === 1 ? "" : "s"} · ${state.readyCount} ready · ${state.incompleteCount} incomplete`;

  return (
    <Modal
      title={`Feature: ${feature}`}
      onClose={onClose}
      className={view === "checkout" ? "sheet-wide" : undefined}
      footer={
        view === "menu" ? (
          <>
            <span className="faint">Esc closes</span>
            <button className="btn ghost" onClick={onClose} disabled={busy}>
              Cancel
            </button>
          </>
        ) : view === "checkout" ? (
          <>
            <span className="faint">Esc leaves field · q closes</span>
            <button
              className="btn ghost"
              onClick={() => setView("menu")}
              disabled={busy}
            >
              Back
            </button>
            <button
              className="btn primary"
              style={{ marginLeft: "auto" }}
              onClick={() => submitRef.current && void submitRef.current()}
              disabled={busy}
              form="feature-checkout-form"
              type="submit"
            >
              {busy
                ? "Submitting..."
                : mode === "force"
                  ? "Continue…"
                  : "Create checkout task"}
            </button>
          </>
        ) : (
          <>
            <span className="faint">Esc = No</span>
            <button
              className="btn ghost"
              onClick={() => setView("checkout")}
              disabled={busy}
            >
              No
            </button>
            <button
              className="btn danger"
              style={{ marginLeft: "auto" }}
              onClick={() => void submitCheckout()}
              disabled={busy}
              data-autofocus="true"
            >
              {busy ? "Submitting..." : "Yes, force merge"}
            </button>
          </>
        )
      }
    >
      {view === "menu" && (
        <div>
          <p className="muted" style={{ marginTop: 0 }}>
            Project <strong>{project}</strong> · {summary}
          </p>
          <div
            className="btn-row"
            style={{ display: "flex", flexDirection: "column", gap: "0.75rem" }}
          >
            {state.allCompleted && (
              <ActionButton
                data-autofocus="true"
                title="All tasks completed — kick off the feature-checkout automation to review and merge."
                onClick={() => openCheckout("review")}
                variant="primary"
                label="Review & merge feature"
                sub="All tasks are completed. Creates a checkout task that runs the feature-checkout automation."
              />
            )}
            {!state.allCompleted && (
              <ActionButton
                data-autofocus="true"
                title="Bypass the all-tasks-completed gate. Creates a checkout task now."
                onClick={() => openCheckout("force")}
                variant="danger"
                label="Force merge (override)"
                sub={`${state.incompleteCount} of ${state.taskCount} tasks not yet completed. Requires confirmation.`}
              />
            )}
            {state.anyBlockedOrWaiting && (
              <ActionButton
                title="Runs the Blocked Task Inspector once for this feature."
                onClick={() => void runInspector()}
                disabled={busy}
                label="Run blocked-task inspector now"
                sub="One-shot: creates a pending task that runs and completes on idle. Does not leave a recurring monitor."
              />
            )}
          </div>
        </div>
      )}

      {view === "checkout" && (
        <form
          id="feature-checkout-form"
          onSubmit={onCheckoutSubmit}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.target as HTMLElement).tagName !== "TEXTAREA") {
              // Let the submit button own the primary Enter path so the form
              // submits without buttons stealing focus.
            }
          }}
        >
          <p className="muted" style={{ marginTop: 0 }}>
            {mode === "force"
              ? `Force merge — ${state.incompleteCount} of ${state.taskCount} tasks not yet completed.`
              : `Review & merge — ${state.taskCount} completed tasks.`}
          </p>
          <section className="field-section">
            <h3>Checkout Mode</h3>
            <div className="field-grid">
              {CHECKOUT_MODES.map((m) => (
                <label key={m.value} className="field-check">
                  <input
                    type="radio"
                    name="checkout_mode"
                    value={m.value}
                    checked={opts.checkout_mode === m.value}
                    onChange={() =>
                      setOpts((o) => ({ ...o, checkout_mode: m.value }))
                    }
                    data-autofocus={m.value === "ai" ? "true" : undefined}
                  />
                  <span>
                    <strong>{m.label}</strong>
                    <span className="faint" style={{ marginLeft: "0.5rem" }}>
                      — {m.help}
                    </span>
                  </span>
                </label>
              ))}
            </div>
          </section>

          <section className="field-section">
            <h3>Merge</h3>
            <div className="field-grid">
              <div className="field">
                <label>Merge target branch</label>
                <input
                  value={opts.merge_target_branch ?? ""}
                  onChange={(e) =>
                    setOpts((o) => ({
                      ...o,
                      merge_target_branch: e.target.value,
                    }))
                  }
                  placeholder="main"
                />
              </div>
              <div className="field">
                <label>Merge policy</label>
                <select
                  value={opts.merge_policy ?? "prompt_only"}
                  onChange={(e) =>
                    setOpts((o) => ({ ...o, merge_policy: e.target.value }))
                  }
                >
                  {MERGE_POLICIES.map((p) => (
                    <option key={p} value={p}>
                      {p}
                    </option>
                  ))}
                </select>
              </div>
              <div className="field">
                <label>Merge strategy</label>
                <select
                  value={opts.merge_strategy ?? "squash"}
                  onChange={(e) =>
                    setOpts((o) => ({ ...o, merge_strategy: e.target.value }))
                  }
                >
                  {MERGE_STRATEGIES.map((s) => (
                    <option key={s} value={s}>
                      {s}
                    </option>
                  ))}
                </select>
              </div>
              <div className="field">
                <label>Remote branch policy</label>
                <select
                  value={opts.remote_branch_policy ?? "keep"}
                  onChange={(e) =>
                    setOpts((o) => ({
                      ...o,
                      remote_branch_policy: e.target.value,
                    }))
                  }
                >
                  {REMOTE_BRANCH_POLICIES.map((p) => (
                    <option key={p} value={p}>
                      {p}
                    </option>
                  ))}
                </select>
              </div>
              <label className="field-check">
                <input
                  type="checkbox"
                  checked={!!opts.open_pr_before_merge}
                  onChange={(e) =>
                    setOpts((o) => ({
                      ...o,
                      open_pr_before_merge: e.target.checked,
                    }))
                  }
                />
                <span>Open PR before merge</span>
              </label>
              <div className="field">
                <label>Execution mode</label>
                <select
                  value={opts.execution_mode ?? "worktree"}
                  onChange={(e) =>
                    setOpts((o) => ({ ...o, execution_mode: e.target.value }))
                  }
                >
                  {EXECUTION_MODES.map((m) => (
                    <option key={m} value={m}>
                      {m}
                    </option>
                  ))}
                </select>
              </div>
              <div className="field">
                <label>Execution branch (optional)</label>
                <input
                  value={opts.execution_branch ?? ""}
                  onChange={(e) =>
                    setOpts((o) => ({
                      ...o,
                      execution_branch: e.target.value,
                    }))
                  }
                  placeholder={feature}
                />
              </div>
            </div>
          </section>
        </form>
      )}

      {view === "confirmForce" && (
        <div>
          <p style={{ marginTop: 0 }}>
            <strong>Force merge</strong> — {state.incompleteCount} of{" "}
            {state.taskCount} tasks not yet completed. Continue?
          </p>
          <p className="muted">
            Checkout mode: <strong>{opts.checkout_mode}</strong>. Merge target:{" "}
            <strong>{opts.merge_target_branch || "(default)"}</strong>. This
            queues the checkout task immediately.
          </p>
        </div>
      )}
    </Modal>
  );
}

function ActionButton({
  label,
  sub,
  onClick,
  variant,
  disabled,
  title,
  "data-autofocus": autoFocus,
}: {
  label: string;
  sub: string;
  onClick: () => void;
  variant?: "primary" | "danger";
  disabled?: boolean;
  title?: string;
  "data-autofocus"?: string;
}) {
  const cls = variant ? `btn ${variant}` : "btn";
  return (
    <button
      type="button"
      className={cls}
      onClick={onClick}
      disabled={disabled}
      title={title}
      data-autofocus={autoFocus}
      style={{
        display: "flex",
        flexDirection: "column",
        alignItems: "flex-start",
        gap: "0.25rem",
        padding: "0.6rem 0.9rem",
        textAlign: "left",
      }}
    >
      <strong>{label}</strong>
      <span className="faint" style={{ fontWeight: "normal" }}>
        {sub}
      </span>
    </button>
  );
}
