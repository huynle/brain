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
import { SelectionBar } from "../common/SelectionBar";

export function Workspace(): JSX.Element {
  const view = useWorkspace((s) => s.view);
  const focusSessionId = useWorkspace((s) => s.focusSessionId);
  const focusSessionRef = useWorkspace((s) => s.focusSessionRef);

  let inner: JSX.Element;
  if (view === "session" && focusSessionRef) {
    inner = <SessionFull sref={focusSessionRef} />;
  } else if (view === "session" && focusSessionId) {
    inner = <SessionFull instanceId={focusSessionId} />;
  } else if (view === "focus") {
    inner = <FocusPanes />;
  } else {
    inner = <OverviewGrid />;
  }

  return (
    <div className="workspace">
      {inner}
      {/* Mounted once at workspace level: the selection outlives whichever
          card or view marked the rows, and the bar must survive tab and
          view switches until the user acts on it. */}
      <SelectionBar />
    </div>
  );
}
