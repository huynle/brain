/**
 * CardAutomations — wireframe-parity port.
 *
 * Lists automations for a project. Each row:
 *   [status-toggle glyph] name · last run · trigger · Run
 *
 * The "last run" cell is the answer to the question the card is usually
 * open for — "is this thing still working?" — without a click: the
 * newest audit's outcome glyph and age, or "never run" when an enabled
 * automation has no audit at all, which is itself the finding. Clicking
 * it opens the automation on its Runs tab.
 * The leading glyph doubles as the enable/pause toggle -- clicking it
 * flips `status` between "active" and "archived" without needing a
 * separate button. Body click still opens the automation modal.
 *
 * Verbs come from `lib/actions/automationActions` via `useRowActions`,
 * so right-click, long-press and keyboard offer the identical set —
 * same registry pattern as tasks, features and goals. The inline
 * glyph toggle and Run button remain as one-click shortcuts to the
 * same context effects.
 */
import { useAutomations } from "../../hooks/useAutomations";
import { useAutomationRuns } from "../../hooks/useAutomationRuns";
import { useMemo } from "react";
import { useAutomationActionContext } from "../../hooks/useAutomationActionContext";
import { useRowActions } from "../../hooks/useRowActions";
import { useActionRunner } from "../../hooks/useActionRunner";
import { useWorkspace } from "../../store/workspace";
import {
  buildAutomationActions,
  isEnabledAutomation,
} from "../../lib/actions/automationActions";
import { Loading } from "../common/Loading";
import { ErrorState } from "../common/ErrorState";
import { relativeTime } from "../../lib/format";
import { describeCron, nextCronRun } from "../../lib/cronSchedule";
import {
  latestRunByAutomation,
  outcomeGlyph,
  outcomeLabel,
  outcomeTone,
  runOutcome,
  runTime,
} from "../../lib/automationRuns";

export interface CardAutomationsProps {
  projectId: string;
}

export function CardAutomations({
  projectId,
}: CardAutomationsProps): JSX.Element {
  const { automations, isLoading, error, refetch } = useAutomations(projectId);
  // One project-wide run query for the whole card, folded to the newest
  // run per automation. Deliberately NOT one query per row: a project
  // with a dozen automations would otherwise issue a dozen over-fetch
  // scans of the largest table in the store on every poll.
  const { runs, windowFull, limit: runWindow } = useAutomationRuns(projectId);
  const lastRuns = useMemo(() => latestRunByAutomation(runs), [runs]);

  // Activation docks the automation in the side panel instead of opening
  // a modal: acting on an automation is a loop (run → read the run →
  // open its session → run again), and a modal blocks the workspace
  // behind it and closes the moment you open anything from it.
  // Reuse, don't stack: the detail pane is a viewer onto whichever row
  // you activated, so clicking through six automations moves one pane
  // six times instead of leaving six tabs behind.
  const openDetail = (automationId: string, name: string) =>
    openOrReuseInSidebar(
      "automation-detail",
      { projectId, automationId },
      name,
    );
  const openOrReuseInSidebar = useWorkspace((s) => s.openOrReuseInSidebar);
  const ctx = useAutomationActionContext(projectId);
  const { rowProps, overlays } = useRowActions();
  const runner = useActionRunner();

  if (isLoading) return <Loading size="sm" label="Loading automations…" />;
  if (error) return <ErrorState error={error} onRetry={refetch} />;
  if (automations.length === 0) {
    return (
      <div style={{ color: "#6b757e", fontSize: 11, padding: "6px 0" }}>
        No automations yet.
      </div>
    );
  }

  return (
    <div>
      {automations.map((a) => {
        const enabled = isEnabledAutomation(a);
        const errored = a.status === "blocked";
        const name = a.title || a.id;
        const actions = buildAutomationActions(a, ctx);
        const byId = new Map(actions.map((act) => [act.id, act]));
        // The glyph is a one-click shortcut for the state pair: pause
        // when enabled (or errored — stops the retry loop), enable
        // when paused. Routing through the runner keeps toasts and
        // error handling identical to the menu's.
        const toggleAction =
          enabled || errored ? byId.get("pause") : byId.get("enable");
        const runAction = byId.get("run");

        // Glyph legend:
        //   ✓ (green .ok)   = active / click to pause
        //   ○ (muted)       = archived / paused / click to enable
        //   ✕ (red .blk)    = errored — clicking pauses to stop the
        //                     retry loop; re-enable from the menu (or
        //                     click again) after fixing the cause.
        const glyph = enabled ? "✓" : errored ? "✕" : "○";
        const glyphKind = enabled ? "ok" : errored ? "blk" : "";
        const glyphTitle = enabled
          ? "Enabled — click to pause (sets status to archived; triggers stop firing)"
          : errored
            ? "Errored — click to pause and stop retries; re-enable after fixing the underlying issue"
            : "Paused — click to re-enable (sets status back to active)";

        return (
          <div
            key={a.id}
            className="trow auto-row"
            {...rowProps(actions, name, () => openDetail(a.id, name))}
            onClick={(e) => {
              if ((e.target as HTMLElement).closest("button")) return;
              openDetail(a.id, name);
            }}
            title={a.title}
            style={enabled ? undefined : { opacity: 0.55 }}
          >
            <button
              type="button"
              className={`glyph ${glyphKind}`}
              onClick={(e) => {
                e.stopPropagation();
                if (toggleAction) runner.run(toggleAction);
              }}
              title={glyphTitle}
              aria-pressed={enabled}
              aria-label={enabled ? "Pause automation" : "Enable automation"}
              style={{
                background: "transparent",
                border: 0,
                padding: 0,
                cursor: "pointer",
                font: "inherit",
                color: "inherit",
              }}
            >
              {glyph}
            </button>
            <span className="name">{name}</span>
            <LastRun
              run={lastRuns.get(a.id)}
              enabled={enabled}
              inconclusive={windowFull}
              window={runWindow}
              onOpen={(e) => {
                e.stopPropagation();
                // Same destination as the row: the docked view leads with
                // the run history, so there is no second surface to route
                // to and no tab to preselect.
                openDetail(a.id, name);
              }}
            />
            <NextRun trigger={a.trigger} enabled={enabled} />
            <span className="status">{a.trigger?.type || "manual"}</span>
            <button
              className="id"
              style={{ padding: "0 4px", fontSize: 10 }}
              onClick={(e) => {
                e.stopPropagation();
                if (runAction) runner.run(runAction);
              }}
              title="Run now"
            >
              Run
            </button>
          </div>
        );
      })}
      {overlays}
      {runner.dialog}
    </div>
  );
}

/**
 * The row's next-run cell.
 *
 * "cron" alone told you the automation is scheduled but not WHEN, so a
 * list of a dozen crons read identically whether an entry fired nightly
 * at 02:00 or every single minute — the schedule was only legible by
 * opening each one, and then only from a run it had already recorded.
 *
 * Only cron triggers have a next run: an event or webhook automation
 * fires when something happens, which is not a time, and rendering "—"
 * for it would suggest a missing value rather than an inapplicable one.
 * Those rows get an empty cell that still holds the column's width.
 *
 * A paused automation is not scheduled at all — its trigger is inert
 * until re-enabled — so it shows the schedule without a prediction
 * rather than a time that will not happen.
 */
function NextRun({
  trigger,
  enabled,
}: {
  trigger?: import("../../lib/types").TriggerConfig;
  enabled: boolean;
}): JSX.Element {
  const expr = trigger?.type === "cron" ? trigger?.schedule || "" : "";
  const tz = trigger?.timezone || "UTC";
  // Recomputed each render; the card refetches on a 20s interval, which
  // is what keeps the relative label from going stale. The search is
  // bounded and cheap for real schedules (an every-minute cron settles
  // in one step, a daily one in under a day of minutes).
  const next = expr ? nextCronRun(expr, tz) : null;

  if (!expr) return <span className="auto-nextrun" />;

  const gloss = describeCron(expr);
  // The gloss can BE the expression when it models no shorthand for it;
  // showing both would just repeat the same string twice.
  const exprNote = gloss === expr ? expr : `${gloss} (${expr})`;

  if (!enabled) {
    return (
      <span
        className="auto-nextrun paused"
        title={`Schedule ${exprNote} in ${tz}. Paused, so it is not scheduled to run — re-enable to resume.`}
      >
        {gloss}
      </span>
    );
  }

  if (!next) {
    // Either the server would reject the expression, or it matches no
    // real date within a year (a February 30th). Both mean "this will
    // never fire", which is worth saying plainly on the row.
    return (
      <span
        className="auto-nextrun never"
        title={`Schedule ${exprNote} in ${tz} matches no date within the next year, so this automation will not fire. Check the expression.`}
      >
        no next run
      </span>
    );
  }

  // relativeTime floors to whole minutes, so the 45–59s band before a run
  // reads "in 0m". Collapse anything inside the next minute to "soon" —
  // the same word it already uses below 45s.
  const dueInMs = next.getTime() - Date.now();
  const label =
    dueInMs < 60_000 ? "soon" : relativeTime(next.toISOString());

  return (
    <span
      className="auto-nextrun"
      title={`Next run ${next.toLocaleString()} (${exprNote} in ${tz}).`}
    >
      {label}
    </span>
  );
}

/**
 * The row's last-run cell.
 *
 * The card reads ONE project-wide window of runs, so "no run here" has
 * two meanings and they must not be conflated: on a project with a
 * minutely cron, that window is entirely that cron, and every other
 * automation would read "never run" while firing nightly. Absence is
 * only conclusive when the window came back short — i.e. we have seen
 * every run the project has.
 *
 * "Never run" is also only worth flagging for an ENABLED automation: a
 * paused one records no runs by definition.
 */
function LastRun({
  run,
  enabled,
  inconclusive,
  window: runWindow,
  onOpen,
}: {
  run?: ReturnType<typeof latestRunByAutomation> extends Map<string, infer R>
    ? R
    : never;
  enabled: boolean;
  inconclusive: boolean;
  window: number;
  onOpen: (e: React.MouseEvent) => void;
}): JSX.Element {
  if (!run) {
    return (
      <span
        className="auto-lastrun never"
        title={
          inconclusive
            ? `Nothing from this automation is in the project's newest ${runWindow} runs — open the runs pane to search its own history.`
            : enabled
              ? "No run has ever been recorded for this automation."
              : "Paused automations do not fire, so they record no runs."
        }
      >
        {inconclusive ? "no recent run" : "never run"}
      </span>
    );
  }
  const outcome = runOutcome(run);
  return (
    <button
      type="button"
      className="auto-lastrun"
      onClick={onOpen}
      title={`Last run ${runTime(run)} — ${outcomeLabel(run)}. Click for this automation's run history.`}
      style={{
        background: "transparent",
        border: 0,
        padding: 0,
        cursor: "pointer",
        font: "inherit",
      }}
    >
      <span className={`glyph ${outcomeTone(outcome)}`}>
        {outcomeGlyph(outcome)}
      </span>
      {relativeTime(runTime(run))}
    </button>
  );
}
