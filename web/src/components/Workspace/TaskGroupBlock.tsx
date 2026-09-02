/**
 * TaskGroupBlock — a group of tasks that is not a feature.
 *
 * The ungrouped "No feature" bucket in the Tasks tab, and each bucket
 * inside the Archived tab. One component for both, because they are the
 * same object: a header, a fold, and a verb list over an explicit set of
 * tasks.
 *
 * Its own file rather than an export from CardTasks — a component file
 * that also exports helpers loses Fast Refresh, and CardArchived
 * importing from CardTasks made two sibling tabs depend on each other for
 * no reason.
 */
import { useRowActions } from "../../hooks/useRowActions";
import {
  buildTaskGroupActions,
  type TaskGroup,
  type TaskGroupActionContext,
} from "../../lib/actions/taskGroupActions";
import type { DepRow } from "../../lib/depTree";
import type { Task } from "../../lib/types";

/**
 * A group of tasks that is NOT a feature: the ungrouped bucket, and each
 * bucket inside the archive fold.
 *
 * One component for both, because they are the same object — a header, a
 * fold, and a verb list over an explicit set of tasks. The header answers
 * right-click, long-press and the keyboard through `useRowActions` like
 * every other row on the card; it is NOT draggable and carries no
 * selection checkbox, because there is no feature entity behind it to
 * assign to a runner or to mark.
 */
export function TaskGroupBlock({
  group,
  rows,
  collapsed,
  renderRow,
  rowProps,
  ctx,
  nested = false,
}: {
  group: TaskGroup;
  rows: DepRow<Task>[];
  collapsed: boolean;
  renderRow: (row: DepRow<Task>) => JSX.Element;
  rowProps: ReturnType<typeof useRowActions>["rowProps"];
  ctx: TaskGroupActionContext;
  /** Rendered inside the archive fold — indent it under that header. */
  nested?: boolean;
}): JSX.Element {
  const n = group.tasks.length;
  const actions = buildTaskGroupActions(group, ctx, { collapsed });
  const rp = rowProps(actions, group.label, () => ctx.toggleCollapsed(group));
  const word = `task${n === 1 ? "" : "s"}`;

  return (
    <div
      className={`feat${collapsed ? " collapsed" : ""}`}
      style={nested ? { marginLeft: 12 } : undefined}
    >
      <div
        className="feat-head"
        {...rp}
        onKeyDown={(e) => {
          // Same tree convention as a feature head, same modifier guard:
          // ⌘←/Alt+← belongs to the browser, not to a fold.
          if (
            !e.metaKey &&
            !e.ctrlKey &&
            !e.altKey &&
            (e.key === "ArrowLeft" || e.key === "ArrowRight")
          ) {
            e.preventDefault();
            if (collapsed !== (e.key === "ArrowLeft")) ctx.toggleCollapsed(group);
            return;
          }
          rp.onKeyDown(e);
        }}
        onClick={(e) => {
          if ((e.target as HTMLElement).closest("button, .caret")) return;
          ctx.toggleCollapsed(group);
        }}
      >
        <span className="glyph-slot">
          {/* A real <button>, unlike a feature caret: this header has no
              selection checkbox competing for the slot, so nothing hides
              it, and `.feat-head` here is not itself role="button". */}
          <button
            type="button"
            className="caret"
            aria-expanded={!collapsed}
            aria-label={`${collapsed ? "Show" : "Hide"} ${n} ${word} in ${group.label}`}
            title={collapsed ? `Show ${n} ${word}` : `Hide ${n} ${word}`}
            onClick={(e) => {
              e.stopPropagation();
              ctx.toggleCollapsed(group);
            }}
          >
            {collapsed ? "▸" : "▾"}
          </button>
        </span>
        <span className="name" style={{ color: "#6b757e" }}>
          {group.label}
        </span>
        <span className="age">
          {n} {word}
        </span>
      </div>
      {!collapsed && rows.map(renderRow)}
    </div>
  );
}
