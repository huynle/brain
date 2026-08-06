/**
 * Statusbar — wireframe-parity port.
 *
 * DOM:
 *   .statusbar
 *     .live-dot · streaming
 *     N live · M projects · P runners
 *     .spacer
 *     v2 tag
 */
import { useWorkspace } from "../store/workspace";
import { useProjects } from "../hooks/useProjects";
import { useRunners } from "../hooks/useRunners";
import { useSessions } from "../hooks/useSessions";

export function Statusbar(): JSX.Element {
  const streaming = useWorkspace((s) => s.streaming);
  const { data: projects } = useProjects();
  const { runners } = useRunners();
  const { sessions } = useSessions();

  const projectCount = projects?.length ?? 0;
  const liveSessions = sessions.filter(
    (s) => s.status === "busy" || s.status === "starting",
  ).length;
  const onlineRunners = runners.filter((r) => r.status === "online").length;

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
      <span>·</span>
      <span>{liveSessions} live</span>
      <span>·</span>
      <span>{projectCount} projects</span>
      <span>·</span>
      <span>
        {onlineRunners}/{runners.length} runners
      </span>
      <span className="spacer" />
      <span>panes-v2</span>
    </div>
  );
}
