// k9s-style context header: navigation breadcrumb (project ▸ view ▸ drill
// frames), the active filter chip, the sort chip, and shown/total counts —
// all reading the scope store. Rendered on the right half of the ContentTabs
// row on desktop; hidden on mobile (MobileNav names the view, sheets own
// drill-back there).
//
// Breadcrumb segments are buttons: clicking a frame pops back to it — the
// touch/mouse affordance for the Esc chain.

import { describeFilter, parseFilter } from "../../lib/filter";
import { ALL_PROJECTS, useUI, type View } from "../../store/ui";
import { useScope } from "../../store/scope";

const VIEW_LABEL: Record<string, string> = {
  tasks: "Tasks",
  brain: "Brain",
  automations: "Automations",
  runners: "Runners",
  logs: "Logs",
};

function shortName(id: string): string {
  if (id === ALL_PROJECTS) return "all";
  return id.split(/[/\\]/).pop() || id;
}

export function ContextBar() {
  const view = useUI((s) => s.view) as View;
  const activeProject = useUI((s) => s.activeProject);
  const setProjectSheetOpen = useUI((s) => s.setProjectSheetOpen);
  const stack = useScope((s) => s.stack);
  const popTo = useScope((s) => s.popTo);
  const rawFilter = useScope((s) => s.filter[view] ?? "");
  const sort = useScope((s) => s.sort[view]);
  const counts = useScope((s) => s.counts[view]);

  const frames = stack.filter((f) => f.view === view);
  const filterText = rawFilter ? describeFilter(parseFilter(rawFilter)) : "";

  return (
    <div className="context-bar">
      <span className="context-crumbs">
        <button className="context-crumb" onClick={() => setProjectSheetOpen(true)} title="Switch project (⌘;)">
          {shortName(activeProject)}
        </button>
        <span className="context-sep">▸</span>
        <button className="context-crumb" onClick={() => popTo(-1)} title="Back to the full view (Esc)">
          {VIEW_LABEL[view] ?? view}
        </button>
        {frames.map((f) => (
          <span key={`${f.kind}:${f.id}`}>
            <span className="context-sep">▸</span>
            <button
              className="context-crumb context-crumb-frame"
              onClick={() => popTo(stack.indexOf(f))}
              title={`${f.kind}: ${f.id}`}
            >
              {f.label}
            </button>
          </span>
        ))}
      </span>
      {filterText && (
        <span className="context-chip context-filter" title="Active filter — Esc clears">
          /{filterText}
        </span>
      )}
      {sort && (
        <span className="context-chip context-sort" title="Sort — o cycles field, O flips direction">
          sort:{sort.field}
          {sort.dir === "desc" ? "↓" : "↑"}
        </span>
      )}
      {counts && (
        <span className="context-counts">
          {counts.shown === counts.total ? counts.total : `${counts.shown}/${counts.total}`}
        </span>
      )}
    </div>
  );
}
