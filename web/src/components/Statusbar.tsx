/**
 * Statusbar — wireframe-parity port.
 *
 * DOM:
 *   .statusbar
 *     .live-dot · streaming
 *     [.pause-flag  ⏸ paused: …]     ← only when a dial is off
 *     N live · M projects · P runners
 *     .spacer
 *     v2 tag
 *
 * The pause flag is the single highest-value indicator in the app. Without
 * it this bar read "streaming · live · 1 projects · 1/1 runners" while both
 * the project task dial and the runner dial were off and nothing would ever
 * run. Every other segment here describes *connectivity*; this one is the
 * only one that describes whether work can actually move.
 *
 * It stays absent when nothing is paused — text appearing at all means work
 * is being held, so there is no "healthy" state to learn to ignore.
 */
import { useWorkspace } from "../store/workspace";
import { useProjects } from "../hooks/useProjects";
import { useRunners } from "../hooks/useRunners";
import { useSessions } from "../hooks/useSessions";
import { usePauseState } from "../hooks/usePauseState";
import { pauseSummary } from "../lib/pause";

export function Statusbar(): JSX.Element {
  const streaming = useWorkspace((s) => s.streaming);
  const { data: projects } = useProjects();
  const { runners } = useRunners();
  const { sessions } = useSessions();
  const { pause } = usePauseState();

  const projectCount = projects?.length ?? 0;
  const liveSessions = sessions.filter(
    (s) => s.status === "busy" || s.status === "starting",
  ).length;
  // Paused runners are excluded: they are online in the heartbeat sense but
  // will not accept a dispatch, and counting them as available is exactly
  // the reassurance this bar used to give wrongly.
  const availableRunners = runners.filter(
    (r) => r.status === "online" && !r.paused,
  ).length;

  const summary = pauseSummary(pause, { projectCount });

  return (
    <div className="statusbar">
      <span
        className="live-dot"
        style={{
          background: streaming ? "#6fca7d" : "#4b545c",
          boxShadow: streaming ? "0 0 5px #6fca7d" : "none",
        }}
      />
      <span>{streaming ? "streaming" : "offline"}</span>
      {/* Ahead of the counters on purpose. The bar scrolls rather than wraps
          on a narrow viewport (its grid row cannot grow), so whatever sits
          last is what disappears — and "nothing is running" must never be
          the thing that disappears. */}
      {summary && (
        <>
          <span>·</span>
          <span className={`pause-flag ${summary.tone}`} title={summary.title}>
            ⏸ {summary.text}
          </span>
        </>
      )}
      <span>·</span>
      <span>{liveSessions} live</span>
      <span>·</span>
      <span>{projectCount} projects</span>
      <span>·</span>
      <span
        title={
          pause.runners.size > 0
            ? `${availableRunners} of ${runners.length} runners can accept work (${pause.runners.size} paused)`
            : undefined
        }
      >
        {availableRunners}/{runners.length} runners
      </span>
      <span className="spacer" />
      <span>panes-v2</span>
    </div>
  );
}
