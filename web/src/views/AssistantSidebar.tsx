import { useUI } from "../store/ui";
import { useViewport } from "../hooks/useViewport";
import { AssistantPanel } from "./AssistantPanel";
import { AssistantSplitter } from "../components/layout/AssistantSplitter";

// Persistent right-side assistant pane. Renders only when the viewport is
// wide enough (>= 1100px) and the user has the sidebar enabled. On narrower
// or mobile viewports the assistant uses the AssistantDrawer overlay instead.
export function AssistantSidebar() {
  const tier = useViewport();
  const visible = useUI((s) => s.assistantSidebar);
  const width = useUI((s) => s.assistantWidth);
  const setVisible = useUI((s) => s.setAssistantSidebar);

  if (tier !== "wide" || !visible) return null;

  return (
    <>
      <AssistantSplitter />
      <aside
        className="assistant-sidebar"
        style={{ width, flex: `0 0 ${width}px` }}
        aria-label="Brain Assistant"
      >
        <AssistantPanel
          active
          className="assistant-panel-sidebar"
          headerActions={
            <button
              type="button"
              className="icon-btn"
              onClick={() => setVisible(false)}
              title="Collapse assistant (Cmd/Ctrl+.)"
              aria-label="Collapse assistant"
            >
              ⟩
            </button>
          }
        />
      </aside>
    </>
  );
}
