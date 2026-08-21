/**
 * CardAutomations — wireframe-parity port.
 *
 * Lists automations for a project. Each row:
 *   [status-toggle glyph] name · trigger · Run
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
import { useAutomationActionContext } from "../../hooks/useAutomationActionContext";
import { useRowActions } from "../../hooks/useRowActions";
import { useActionRunner } from "../../hooks/useActionRunner";
import { useModal } from "../../store/modal";
import {
  buildAutomationActions,
  isEnabledAutomation,
} from "../../lib/actions/automationActions";
import { Loading } from "../common/Loading";
import { ErrorState } from "../common/ErrorState";

export interface CardAutomationsProps {
  projectId: string;
}

export function CardAutomations({
  projectId,
}: CardAutomationsProps): JSX.Element {
  const { automations, isLoading, error, refetch } =
    useAutomations(projectId);
  const openModal = useModal((s) => s.open);
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
        const toggleAction = enabled || errored ? byId.get("pause") : byId.get("enable");
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
            className="trow"
            {...rowProps(actions, name, () =>
              openModal("automation", { projectId, automationId: a.id }),
            )}
            onClick={(e) => {
              if ((e.target as HTMLElement).closest("button")) return;
              openModal("automation", { projectId, automationId: a.id });
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
              aria-label={
                enabled ? "Pause automation" : "Enable automation"
              }
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
