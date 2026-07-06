// Touch counterpart of the desktop ContextBar + Esc chain: a slim strip under
// the status bar showing the drill breadcrumb, tasks mode, and active filter,
// with tap targets to back out of each. Rendered only on mobile and only when
// there is context to show — zero cost on a clean view.

import { describeFilter, parseFilter } from "../../lib/filter";
import { useScope } from "../../store/scope";
import { useUI, type View } from "../../store/ui";

const VIEW_LABEL: Record<string, string> = {
  tasks: "Tasks",
  brain: "Brain",
  automations: "Automations",
  runners: "Runners",
  logs: "Logs",
};

export function MobileContextStrip() {
  const view = useUI((s) => s.view) as View;
  const tasksMode = useUI((s) => s.tasksMode);
  const setTasksMode = useUI((s) => s.setTasksMode);
  const doneMergeOnly = useUI((s) => s.doneMergeOnly);
  const setDoneMergeOnly = useUI((s) => s.setDoneMergeOnly);
  const stack = useScope((s) => s.stack);
  const pop = useScope((s) => s.pop);
  const rawFilter = useScope((s) => s.filter[view] ?? "");
  const clearFilter = useScope((s) => s.clearFilter);

  const frames = stack.filter((f) => f.view === view);
  const modeChip = view === "tasks" && tasksMode !== "tasks";
  const hasContext = frames.length > 0 || !!rawFilter || modeChip;
  if (!hasContext) return null;

  // The touch "Esc": pop a drill frame first, then the filter, then the mode.
  function back() {
    if (pop()) return;
    if (clearFilter(view)) return;
    if (modeChip) {
      setDoneMergeOnly(false);
      setTasksMode("tasks");
    }
  }

  return (
    <div className="mctx">
      <button type="button" className="mctx-back" onClick={back} aria-label="Back">
        ◂
      </button>
      <span className="mctx-crumbs">
        {VIEW_LABEL[view] ?? view}
        {modeChip && (
          <button
            type="button"
            className="mctx-chip mctx-mode"
            onClick={() => {
              setDoneMergeOnly(false);
              setTasksMode("tasks");
            }}
          >
            {tasksMode === "done" ? (doneMergeOnly ? "done · ⇡ merge" : "done") : "schedules"} ✕
          </button>
        )}
        {frames.map((f) => (
          <span key={`${f.kind}:${f.id}`} className="mctx-frame">
            ▸ {f.label}
          </span>
        ))}
      </span>
      {rawFilter && (
        <button type="button" className="mctx-chip mctx-filter" onClick={() => clearFilter(view)}>
          /{describeFilter(parseFilter(rawFilter))} ✕
        </button>
      )}
    </div>
  );
}
