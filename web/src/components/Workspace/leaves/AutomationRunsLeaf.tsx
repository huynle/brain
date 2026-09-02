/**
 * AutomationRunsLeaf — a project's automation run history as a dockable
 * pane.
 *
 * The modal's Runs tab answers "what has THIS automation been doing".
 * This pane answers the other question: "what has been firing in this
 * project at all" — the one you have when a nightly job's output looks
 * wrong and you don't yet know which of eight automations produced it.
 * Filter by automation and by outcome, select a run to see the audit
 * fields a row has no room for, and open the session from either.
 *
 * target shape: { projectId: string; automationId?: string }
 *   `automationId` pre-selects the filter, so "open runs pane" from one
 *   automation's modal lands on that automation rather than dumping the
 *   whole project on the user.
 *
 * Sessions open in the SIDE PANEL rather than navigating: the point of
 * a pane is that the list survives the drill-down, so you can walk three
 * runs in a row without re-finding your place. (The modal, which is
 * dismissed by opening anything, navigates to the full-page view.)
 */
import { useMemo, useState } from "react";

import { useAutomations } from "../../../hooks/useAutomations";
import {
  useAutomationRuns,
  PROJECT_RUNS_LIMIT,
} from "../../../hooks/useAutomationRuns";
import { useWorkspace } from "../../../store/workspace";
import { Loading } from "../../common/Loading";
import { ErrorState } from "../../common/ErrorState";
import {
  AutomationRunDetail,
  AutomationRunRows,
} from "../../Automations/AutomationRunRows";
import {
  runOutcome,
  type AutomationRun,
  type RunOutcome,
} from "../../../lib/automationRuns";

type OutcomeFilter = "all" | RunOutcome;

const OUTCOME_LABELS: Array<[OutcomeFilter, string]> = [
  ["all", "all outcomes"],
  ["generated", "generated work"],
  ["skipped", "skipped"],
  ["error", "errored"],
  ["noop", "no work"],
];

export function AutomationRunsLeaf({
  target,
}: {
  target: Record<string, unknown>;
}): JSX.Element {
  const projectId = (target.projectId as string | undefined) ?? "";
  const [automationId, setAutomationId] = useState<string>(
    (target.automationId as string | undefined) ?? "",
  );
  const [outcome, setOutcome] = useState<OutcomeFilter>("all");
  const [selectedId, setSelectedId] = useState<string | undefined>();
  // How deep to read. A minutely cron buries a nightly job's history in
  // hours, so "show me more" has to be reachable — but the default stays
  // modest because this is the largest table in the store.
  const [depth, setDepth] = useState(PROJECT_RUNS_LIMIT);

  const { automations } = useAutomations(projectId);
  // The automation filter goes to the SERVER, not to the fetched page.
  // Client-side filtering looks equivalent and is not: on a project with
  // a minutely cron, the newest 200 runs are 200 runs of that cron, so
  // filtering them client-side answers "Nightly Cleanup: no runs" for a
  // job that fires every night. The server's automation_id filter walks
  // a much deeper window to fill the same page (internal/api/
  // automation_runs.go), which is the only way to see a rare automation
  // behind a chatty one. Outcome, which is derived from the audit body
  // and has no server-side filter, stays client-side.
  const { runs, isLoading, error, truncated, refetch } = useAutomationRuns(
    projectId,
    automationId || undefined,
    depth,
  );
  const openInSidebar = useWorkspace((s) => s.openInSidebar);

  const nameById = useMemo(() => {
    const m = new Map<string, string>();
    for (const a of automations) m.set(a.id, a.title || a.id);
    return m;
  }, [automations]);

  // Every automation that has actually run, plus any that hasn't but
  // exists — the picker should be able to say "this one has no runs"
  // rather than silently omitting it.
  // Built from the automation catalog, not from the fetched runs: once a
  // filter is applied the page holds one automation, and a picker built
  // from it would collapse to the option already chosen.
  const pickable = useMemo(
    () =>
      automations
        .map((a) => a.id)
        .sort((a, b) =>
          (nameById.get(a) ?? a).localeCompare(nameById.get(b) ?? b),
        ),
    [automations, nameById],
  );

  const shown = useMemo(
    () =>
      outcome === "all" ? runs : runs.filter((r) => runOutcome(r) === outcome),
    [runs, outcome],
  );

  const selected: AutomationRun | undefined = useMemo(
    () => shown.find((r) => r.id === selectedId),
    [shown, selectedId],
  );

  if (!projectId) {
    return (
      <div style={{ color: "var(--p2-fg-faint)", fontSize: 12 }}>
        No project selected — open this pane from a project card's Automations
        tab.
      </div>
    );
  }
  if (isLoading) return <Loading label="Loading automation runs…" />;
  if (error) return <ErrorState error={error} onRetry={refetch} />;

  return (
    <div>
      <div className="arun-toolbar">
        <select
          value={automationId}
          onChange={(e) => setAutomationId(e.target.value)}
          aria-label="Filter by automation"
        >
          <option value="">all automations</option>
          {pickable.map((id) => (
            <option key={id} value={id}>
              {nameById.get(id) ?? id}
            </option>
          ))}
        </select>
        <select
          value={outcome}
          onChange={(e) => setOutcome(e.target.value as OutcomeFilter)}
          aria-label="Filter by outcome"
        >
          {OUTCOME_LABELS.map(([value, label]) => (
            <option key={value} value={value}>
              {label}
            </option>
          ))}
        </select>
        <select
          value={depth}
          onChange={(e) => setDepth(Number(e.target.value))}
          aria-label="How many runs to read"
          title="How far back to read. Raising this costs a bigger scan server-side."
        >
          <option value={PROJECT_RUNS_LIMIT}>last {PROJECT_RUNS_LIMIT}</option>
          <option value={500}>last 500</option>
          <option value={1000}>last 1000</option>
        </select>
        <span>
          {shown.length} of {runs.length}
          {truncated ? "+" : ""} runs
        </span>
        <span style={{ flex: 1 }} />
        <button
          type="button"
          className="id"
          style={{ padding: "0 6px", fontSize: 10 }}
          onClick={refetch}
          title="Refetch the run history now"
        >
          Refresh
        </button>
      </div>

      {shown.length === 0 ? (
        <div className="arun-note">
          {runs.length > 0
            ? "No run in this window matches that outcome."
            : truncated
              ? "No runs found before the server's scan window ran out. Runs older than that may exist — raise the window above."
              : automationId
                ? "This automation has never recorded a run."
                : "No automation has recorded a run in this project yet."}
        </div>
      ) : (
        <AutomationRunRows
          runs={shown}
          projectId={projectId}
          // Names only make sense when the list spans automations.
          automationName={
            automationId ? undefined : (id) => nameById.get(id) ?? id
          }
          selectedRunId={selectedId}
          onSelect={(run) =>
            setSelectedId((cur) => (cur === run.id ? undefined : run.id))
          }
          // Everything docks in the side panel: this pane IS the place
          // you are working, and replacing it with what you clicked
          // loses the list you were walking.
          onOpenSession={(task, ref) =>
            openInSidebar("session", { ref }, task.title || task.id)
          }
          onOpenLog={(task) =>
            openInSidebar(
              "logs",
              { projectId, taskId: task.id },
              `Logs ${task.id.slice(0, 8)}`,
            )
          }
          onOpenTask={(task) =>
            openInSidebar(
              "task-detail",
              { projectId, taskId: task.id },
              task.title || task.id,
            )
          }
        />
      )}

      {selected && <AutomationRunDetail run={selected} />}

      {truncated && (
        <div className="arun-note">
          The server stopped scanning before the page filled — runs older than
          this window exist and are not shown.
        </div>
      )}
    </div>
  );
}
