/**
 * AutomationDetail — one automation, as a working surface.
 *
 * This replaced the automation MODAL as the row's activation target.
 * A modal is the wrong shape for this object: acting on an automation
 * is a loop — run it, watch what the run produced, open the session,
 * come back, run it again — and a modal blocks the workspace behind it
 * and dies the moment you open anything. Docked in the side panel the
 * automation stays put while its output opens in Focus beside it.
 *
 * Layout is ordered by how often each part is used, because the panel
 * is ~430px wide and everything below the fold costs a scroll:
 *
 *   1. identity — name, enabled state, trigger
 *   2. verbs — Run now / Pause·Enable, and the last run's outcome
 *   3. runs — the history, which is the reason you opened this
 *   4. config — folded away; it answers "how is this set up", which is
 *      a question you ask once, not every visit
 *
 * `onOpenSession` / `onOpenLog` / `onOpenTask` are supplied by the host
 * so drill-downs land in the right dock: from the side panel they open
 * in Focus, keeping this pane on screen.
 */
import { useState } from "react";

import { useAutomations } from "../../hooks/useAutomations";
import { useAutomationRuns } from "../../hooks/useAutomationRuns";
import { useAutomationActionContext } from "../../hooks/useAutomationActionContext";
import { useActionRunner } from "../../hooks/useActionRunner";
import { useRowActions } from "../../hooks/useRowActions";
import {
  buildAutomationActions,
  isEnabledAutomation,
} from "../../lib/actions/automationActions";
import { Loading } from "../common/Loading";
import { ErrorState } from "../common/ErrorState";
import { relativeTime } from "../../lib/format";
import {
  outcomeGlyph,
  outcomeLabel,
  outcomeTone,
  runOutcome,
  runTime,
  type AutomationRun,
} from "../../lib/automationRuns";
import { AutomationRunRows } from "./AutomationRunRows";
import { AutomationRunDetail } from "./AutomationRunRows";
import type { SessionRef, Task } from "../../lib/types";

export interface AutomationDetailProps {
  projectId: string;
  automationId: string;
  onOpenSession: (task: Task, ref: SessionRef) => void;
  onOpenLog: (task: Task) => void;
  onOpenTask: (task: Task) => void;
  /** Escalate to the project-wide runs pane. */
  onOpenRunsPane?: () => void;
}

export function AutomationDetail({
  projectId,
  automationId,
  onOpenSession,
  onOpenLog,
  onOpenTask,
  onOpenRunsPane,
}: AutomationDetailProps): JSX.Element {
  const { automations, isLoading, error, refetch } = useAutomations(projectId);
  const runs = useAutomationRuns(projectId, automationId);
  const ctx = useAutomationActionContext(projectId);
  const runner = useActionRunner();
  // Same verb registry the row uses, so right-click here offers exactly
  // what right-click there does — including Delete, which the old modal
  // simply did not have.
  const { rowProps, overlays } = useRowActions();
  const [showConfig, setShowConfig] = useState(false);
  const [selectedRun, setSelectedRun] = useState<AutomationRun | undefined>();

  const automation = automations.find((a) => a.id === automationId);

  if (isLoading) return <Loading label="Loading automation…" />;
  if (error) return <ErrorState error={error} onRetry={refetch} />;
  if (!automation) {
    return (
      <div className="arun-note">
        Automation {automationId} is not in {projectId}'s list — it may have
        been deleted.
      </div>
    );
  }

  const enabled = isEnabledAutomation(automation);
  const errored = automation.status === "blocked";
  const name = automation.title || automation.id;
  const actions = buildAutomationActions(automation, ctx);
  const byId = new Map(actions.map((a) => [a.id, a]));
  const runAction = byId.get("run");
  const toggleAction =
    enabled || errored ? byId.get("pause") : byId.get("enable");
  const last = runs.runs[0];

  const action = (automation as { action?: unknown }).action;
  const actionStr =
    typeof action === "string"
      ? action
      : action
        ? JSON.stringify(action, null, 2)
        : null;

  return (
    <div className="adetail" {...rowProps(actions, name)}>
      <div className="adetail-head">
        <span
          className={`dot ${enabled ? "on" : errored ? "err" : ""}`}
          title={automation.status}
        />
        <span className="adetail-name" title={name}>
          {name}
        </span>
      </div>
      <div className="adetail-badges">
        <span
          className={`health ${enabled ? "active" : errored ? "blocked" : ""}`}
        >
          {automation.status}
        </span>
        <span className="adetail-trigger">
          {automation.trigger?.type || "manual"}
          {automation.trigger?.event ? ` · ${automation.trigger.event}` : ""}
          {automation.schedule ? ` · ${automation.schedule}` : ""}
        </span>
      </div>

      <div className="adetail-actions">
        <button
          className="primary"
          disabled={runner.busy}
          onClick={() => runAction && runner.run(runAction)}
          // Manual runs deliberately work while paused — that is how you
          // test one before re-enabling its trigger.
          title="Run this automation now, even if its trigger is paused"
        >
          Run now
        </button>
        {toggleAction && (
          <button
            disabled={runner.busy}
            onClick={() => runner.run(toggleAction)}
            title={toggleAction.disabledReason || toggleAction.label}
          >
            {enabled || errored ? "Pause" : "Enable"}
          </button>
        )}
        {onOpenRunsPane && (
          <button
            onClick={onOpenRunsPane}
            title="Open the project-wide runs pane, across every automation"
          >
            Runs pane ↗
          </button>
        )}
      </div>

      {/* The one-line health answer, above the fold and above the list:
          "did it run, and did it work". */}
      <div className="adetail-last">
        {runs.isLoading ? (
          "loading run history…"
        ) : last ? (
          <>
            <span className={`glyph ${outcomeTone(runOutcome(last))}`}>
              {outcomeGlyph(runOutcome(last))}
            </span>
            <span title={runTime(last)}>
              last run {relativeTime(runTime(last))} — {outcomeLabel(last)}
            </span>
          </>
        ) : runs.truncated ? (
          "no runs found in the window the server scanned"
        ) : (
          "never run"
        )}
      </div>

      <h4 className="modal-content-heading">
        Runs
        {runs.runs.length > 0 && (
          <span className="adetail-count">
            {runs.runs.length}
            {runs.truncated ? "+" : ""}
          </span>
        )}
      </h4>
      {runs.error ? (
        <ErrorState error={runs.error} onRetry={runs.refetch} />
      ) : runs.isLoading ? (
        <Loading size="sm" label="Loading runs…" />
      ) : runs.runs.length === 0 ? (
        <div className="arun-note">
          {runs.truncated
            ? "No runs found before the server's scan window ran out — older ones may exist. The runs pane can read deeper."
            : "No run has been recorded. Cron and event triggers write an audit every time they fire — including the times they skip."}
        </div>
      ) : (
        <>
          <AutomationRunRows
            runs={runs.runs}
            projectId={projectId}
            selectedRunId={selectedRun?.id}
            onSelect={(run) =>
              setSelectedRun((cur) => (cur?.id === run.id ? undefined : run))
            }
            onOpenSession={onOpenSession}
            onOpenLog={onOpenLog}
            onOpenTask={onOpenTask}
          />
          {selectedRun && <AutomationRunDetail run={selectedRun} />}
        </>
      )}

      {/* Config last and folded: it is the question you ask once. */}
      <button
        className="adetail-fold"
        aria-expanded={showConfig}
        onClick={() => setShowConfig((v) => !v)}
      >
        {showConfig ? "▾" : "▸"} Config
      </button>
      {showConfig && (
        <>
          <div className="kv-grid">
            <div className="k">Project</div>
            <div className="v">{projectId}</div>
            <div className="k">Id</div>
            <div className="v">
              <code>{automation.id}</code>
            </div>
            <div className="k">Path</div>
            <div className="v">
              <code>{automation.path}</code>
            </div>
            {automation.agent && (
              <>
                <div className="k">Agent</div>
                <div className="v">{automation.agent}</div>
              </>
            )}
            {automation.model && (
              <>
                <div className="k">Model</div>
                <div className="v">{automation.model}</div>
              </>
            )}
          </div>
          {actionStr && (
            <>
              <h4 className="modal-content-heading">Action</h4>
              <pre className="modal-content-pre">{actionStr}</pre>
            </>
          )}
        </>
      )}

      {overlays}
      {runner.dialog}
    </div>
  );
}
