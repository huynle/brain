/**
 * CardAutomations — wireframe-parity port.
 *
 * Lists automations for a project. Each row:
 *   [status-toggle glyph] name · trigger · Run
 * The leading glyph doubles as the enable/pause toggle -- clicking it
 * flips `status` between "active" and "archived" without needing a
 * separate button. Body click still opens the automation modal.
 */
import { useAutomations } from "../../hooks/useAutomations";
import { useModal } from "../../store/modal";
import { useUI } from "../../store/ui";
import { executeAutomation, updateEntry, ApiError } from "../../lib/api";
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
  const toast = useUI((s) => s.toast);

  if (isLoading) return <Loading size="sm" label="Loading automations…" />;
  if (error) return <ErrorState error={error} onRetry={refetch} />;
  if (automations.length === 0) {
    return (
      <div style={{ color: "#6b757e", fontSize: 11, padding: "6px 0" }}>
        No automations yet.
      </div>
    );
  }

  const doRun = async (a: (typeof automations)[number]) => {
    try {
      // executeAutomation expects the entry path (e.g.
      // "projects/x/automation/y.md"), not the short id.
      await executeAutomation(a.path, projectId);
      toast(`Ran ${a.title || a.id}`, "success");
      refetch();
    } catch (err) {
      const msg =
        err instanceof ApiError
          ? `Run failed: ${err.message}`
          : `Run failed: ${(err as Error).message ?? "unknown"}`;
      toast(msg, "error");
    }
  };

  // Toggle an automation between active <-> archived. The trigger
  // dispatcher only fires automations whose entry status is "active"
  // (see internal/service/goal_automation.go and each Ensure*Automation
  // reconciler), so "archived" is the clean off state -- event and cron
  // ticks continue firing other automations, the paused one just no-ops
  // on match. Built-in automations survive the toggle across restarts:
  // Ensure...() only recreates a built-in when NONE with its GeneratedBy
  // marker exists, regardless of status.
  const doToggle = async (a: (typeof automations)[number]) => {
    const enabled = a.status === "active";
    const nextStatus = enabled ? "archived" : "active";
    try {
      await updateEntry(a.path, { status: nextStatus });
      toast(
        enabled
          ? `Paused ${a.title || a.id}`
          : `Enabled ${a.title || a.id}`,
        "success",
      );
      refetch();
    } catch (err) {
      const msg =
        err instanceof ApiError
          ? `Toggle failed: ${err.message}`
          : `Toggle failed: ${(err as Error).message ?? "unknown"}`;
      toast(msg, "error");
    }
  };

  return (
    <div>
      {automations.map((a) => {
        const enabled = a.status === "active";
        const errored = a.status === "blocked";
        // Glyph doubles as the toggle:
        //   ✓ (green .ok)   = active / click to pause
        //   ○ (muted)       = archived / paused / click to enable
        //   ✕ (red .blk)    = errored -- keep the visual for signal;
        //                     clicking still routes through doToggle
        //                     so an operator can flip it to archived
        //                     to stop the retry loop, or back to active
        //                     after fixing the underlying issue.
        const glyph = enabled ? "✓" : errored ? "✕" : "○";
        const glyphKind = enabled ? "ok" : errored ? "blk" : "";
        const glyphTitle = enabled
          ? "Enabled — click to pause (sets status to archived; triggers stop firing)"
          : errored
            ? "Errored — click to archive and stop retries, or fix the underlying issue and click again to re-enable"
            : "Paused — click to re-enable (sets status back to active)";
        return (
          <div
            key={a.id}
            className="trow"
            onClick={() =>
              openModal("automation", {
                projectId,
                automationId: a.id,
              })
            }
            title={a.title}
            style={enabled ? undefined : { opacity: 0.55 }}
          >
            <button
              type="button"
              className={`glyph ${glyphKind}`}
              onClick={(e) => {
                e.stopPropagation();
                void doToggle(a);
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
            <span className="name">{a.title || a.id}</span>
            <span className="status">{a.trigger?.type || "manual"}</span>
            <button
              className="id"
              style={{ padding: "0 4px", fontSize: 10 }}
              onClick={(e) => {
                e.stopPropagation();
                void doRun(a);
              }}
              title="Run now"
            >
              Run
            </button>
          </div>
        );
      })}
    </div>
  );
}
