/**
 * FeatureActionsModal — panes-v2 port.
 *
 * Ported from web/src/views/tasks/FeatureActionsModal.tsx (deleted in the
 * panes-v2 cutover). Same two workflow actions the old UI surfaced, wired
 * into the panes-v2 modal system:
 *   • "Review & merge feature" — POST /features/{featureId}/checkout, creates
 *     the checkout task that runs the feature-checkout automation.
 *   • "Force merge" — same, but bypasses the all-tasks-completed gate.
 *   • "Run blocked-inspector now" — one-shot inspector task for the feature.
 *
 * Reads `featureId` + `projectId` from `useModal().target` (matching the
 * panes-v2 modal store contract). Tasks come from `useLive` — no props.
 */
import { useMemo, useRef, useState, type FormEvent } from "react";
import { Modal } from "../common/Modal";
import { checkoutFeature, resumeFeature, runFeature, runBlockedInspectorNow, summarizeResumeResults, summarizeRunFeatureResult } from "../../lib/api";
import { useLive } from "../../lib/sse";
import { useModal } from "../../store/modal";
import { useUI } from "../../store/ui";
import type { FeatureCheckoutOptions, Task } from "../../lib/types";
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

const EMPTY_TASKS: readonly Task[] = Object.freeze([]);

export function FeatureActionsModal(): JSX.Element {
  const target = useModal((s) => s.target);
  const close = useModal((s) => s.close);
  const toast = useUI((s) => s.toast);

  const featureId =
    (target?.featureId as string | undefined) ??
    (target?.id as string | undefined) ??
    "";
  const projectId = (target?.projectId as string | undefined) ?? "";

  const allTasks = useLive((s) => s.projects[projectId]?.tasks) ?? EMPTY_TASKS;
  const tasks = useMemo(
    () => allTasks.filter((t) => t.feature_id === featureId),
    [allTasks, featureId],
  );

  const state = useMemo(() => computeFeatureState(tasks as Task[]), [tasks]);
  const [view, setView] = useState<View>("menu");
  const [mode, setMode] = useState<"review" | "force">("review");
  const [busy, setBusy] = useState(false);
  const [opts, setOpts] = useState<FeatureCheckoutOptions>(() =>
    deriveCheckoutDefaults(tasks as Task[]),
  );
  const submitRef = useRef<() => Promise<void>>(() => Promise.resolve());

  function openCheckout(next: "review" | "force") {
    setMode(next);
    setView("checkout");
  }

  async function submitCheckout() {
    setBusy(true);
    try {
      const body: FeatureCheckoutOptions = {};
      for (const [k, v] of Object.entries(opts)) {
        if (v === undefined || v === null || v === "") continue;
        (body as Record<string, unknown>)[k] = v;
      }
      const result = await checkoutFeature(projectId, featureId, body);
      toast(
        result.created
          ? `Checkout task created${result.task?.path ? ` (${result.task.path})` : ""}`
          : "Checkout task already exists for this feature",
        "success",
      );
      close();
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
      setView("confirmForce");
      return;
    }
    void submitCheckout();
  }

  async function runInspector() {
    setBusy(true);
    try {
      const resp = await runBlockedInspectorNow(projectId, featureId);
      toast(`Blocked inspector queued (${resp.path})`, "success");
      close();
    } catch (err) {
      toast(
        `Failed to queue inspector: ${err instanceof Error ? err.message : String(err)}`,
        "error",
      );
    } finally {
      setBusy(false);
    }
  }

  async function resumeAbandoned() {
    setBusy(true);
    try {
      const result = await resumeFeature(projectId, featureId, { force: false });
      const kind = result.total_resumed > 0 ? "success" : "info";
      toast(summarizeResumeResults(result), kind);
      close();
    } catch (err) {
      toast(
        `Resume failed: ${err instanceof Error ? err.message : String(err)}`,
        "error",
      );
    } finally {
      setBusy(false);
    }
  }

  async function runNow() {
    setBusy(true);
    try {
      const result = await runFeature(projectId, featureId, false);
      const { message, kind } = summarizeRunFeatureResult(result);
      toast(message, kind);
      close();
    } catch (err) {
      toast(
        `Run feature failed: ${err instanceof Error ? err.message : String(err)}`,
        "error",
      );
    } finally {
      setBusy(false);
    }
  }

  const summary = `${state.taskCount} task${state.taskCount === 1 ? "" : "s"} · ${state.readyCount} ready · ${state.incompleteCount} incomplete`;

  return (
    <Modal
      title={`Feature: ${featureId}`}
      onClose={close}
      footer={
        view === "menu" ? (
          <>
            <span className="faint">Esc closes</span>
            <button onClick={close} disabled={busy}>
              Cancel
            </button>
          </>
        ) : view === "checkout" ? (
          <>
            <span className="faint">Esc leaves field · q closes</span>
            <button
              onClick={() => setView("menu")}
              disabled={busy}
            >
              Back
            </button>
            <button
              className="primary"
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
              onClick={() => setView("checkout")}
              disabled={busy}
            >
              No
            </button>
            <button
              className="primary"
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
            Project <strong>{projectId}</strong> · {summary}
          </p>
          <div
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
            {state.hasResumableTasks && (
              <ActionButton
                title="Fans out ResumeTask across every abandoned task in this feature."
                onClick={() => void resumeAbandoned()}
                disabled={busy}
                label={`Resume ${state.resumableCount} abandoned task${state.resumableCount === 1 ? "" : "s"}`}
                sub="Flips each abandoned task back to pending with a resume hint. Skips tasks that are not abandoned."
              />
            )}
            <ActionButton
              title="Push-dispatch every ready task in this feature (up to runner capacity); queues the rest for the feature-scoped cascade."
              onClick={() => void runNow()}
              disabled={busy}
              label="Run feature now"
              sub="Dispatches ready tasks immediately. Cascade continues as slots free — even while the project is paused."
            />
          </div>
        </div>
      )}

      {view === "checkout" && (
        <form id="feature-checkout-form" onSubmit={onCheckoutSubmit}>
          <p className="muted" style={{ marginTop: 0 }}>
            {mode === "force"
              ? `Force merge — ${state.incompleteCount} of ${state.taskCount} tasks not yet completed.`
              : `Review & merge — ${state.taskCount} completed tasks.`}
          </p>
          <section>
            <h3>Checkout Mode</h3>
            <div>
              {CHECKOUT_MODES.map((m) => (
                <label
                  key={m.value}
                  style={{ display: "block", margin: "4px 0" }}
                >
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
                  <span style={{ marginLeft: 6 }}>
                    <strong>{m.label}</strong>
                    <span className="faint" style={{ marginLeft: "0.5rem" }}>
                      — {m.help}
                    </span>
                  </span>
                </label>
              ))}
            </div>
          </section>

          <section style={{ marginTop: 12 }}>
            <h3>Merge</h3>
            <div className="kv-grid">
              <div className="k">Merge target branch</div>
              <div className="v">
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
              <div className="k">Merge policy</div>
              <div className="v">
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
              <div className="k">Merge strategy</div>
              <div className="v">
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
              <div className="k">Remote branch policy</div>
              <div className="v">
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
              <div className="k">Open PR before merge</div>
              <div className="v">
                <label>
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
                </label>
              </div>
              <div className="k">Execution mode</div>
              <div className="v">
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
              <div className="k">Execution branch (optional)</div>
              <div className="v">
                <input
                  value={opts.execution_branch ?? ""}
                  onChange={(e) =>
                    setOpts((o) => ({
                      ...o,
                      execution_branch: e.target.value,
                    }))
                  }
                  placeholder={featureId}
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
  return (
    <button
      type="button"
      className={variant ?? ""}
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
