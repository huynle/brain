/**
 * Workspace — wireframe-parity port of `renderWorkspace`.
 *
 * Routes between:
 *   • session view (SessionFull, when a session is focused)
 *   • focus mode (dockable panes, when view === "focus")
 *   • overview grid (default)
 *
 * DOM: <div class="workspace">…</div>
 */
import { useWorkspace } from "../../store/workspace";
import { OverviewGrid } from "./OverviewGrid";
import { FocusPanes } from "./FocusPanes";
import { SessionFull } from "./SessionFull";

export function Workspace(): JSX.Element {
  const view = useWorkspace((s) => s.view);
  const focusSessionId = useWorkspace((s) => s.focusSessionId);

  let inner: JSX.Element;
  if (view === "session" && focusSessionId) {
    inner = <SessionFull instanceId={focusSessionId} />;
  } else if (view === "focus") {
    inner = <FocusPanes />;
  } else {
    inner = <OverviewGrid />;
  }

  return <div className="workspace">{inner}</div>;
}
