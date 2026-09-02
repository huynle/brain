/**
 * useTaskRowRenderer — the one task row, for every surface that has one.
 *
 * The row carries a lot: the status glyph and its abandoned override, the
 * dependency guide, the hold chip explaining why a task is not running,
 * the feature-dep warning, the selection checkbox and its shift-click
 * range, the plain-click active highlight, the double-click open, and
 * drag-to-a-pane. Copying that is how surfaces drift apart —
 * `FeatureDetailLeaf` has its own copy and has already silently lost nine
 * of those behaviours, which nobody notices until a right-click offers a
 * different menu.
 *
 * A HOOK rather than a component, because the expensive pieces —
 * `useRowActions`' action runner and its overlays — must be created once
 * per surface, not once per row. The caller owns those and passes them
 * in; everything else here is a cheap store selector.
 *
 * `orderedTaskIds` is the one genuinely per-surface input: a shift-click
 * range walks it, so it must mirror what THAT surface actually renders.
 * Handing in another view's list lets a range reach rows the user cannot
 * see.
 */
import { useSelection } from "../../store/selection";
import { useWorkspace } from "../../store/workspace";
import { usePauseState } from "../../hooks/usePauseState";
import { useDeferredPreview } from "../../hooks/useDeferredPreview";
import { useRowActions } from "../../hooks/useRowActions";
import { useTaskActionContext } from "../../hooks/useTaskActionContext";
import { DepGuide } from "../common/DepGuide";
import { beginDrag, endDrag } from "../../hooks/useDragDrop";
import { buildTaskActions } from "../../lib/actions/taskActions";
import { isRangeKey } from "../../lib/selection";
import { featureDepWarning, taskHoldReason } from "../../lib/pause";
import type { DepRow } from "../../lib/depTree";
import type { ActionDescriptor } from "../../lib/actions/types";
import type { Task } from "../../lib/types";

export function taskGlyph(
  status: Task["status"],
  isAbandoned = false,
): {
  glyph: string;
  cls: string;
} {
  // Abandoned tasks override the status glyph with a "\u27f3" (recovery) marker
  // in amber. Distinct from \u2715 (blocked) and \u25b8 (in-progress) so a user
  // scanning the row can spot resumable work without opening the modal.
  if (isAbandoned) {
    return { glyph: "\u27f3", cls: "abandoned" };
  }
  switch (status) {
    case "in_progress":
      return { glyph: "\u25b8", cls: "busy" };
    case "blocked":
      return { glyph: "\u2715", cls: "blk" };
    case "completed":
    case "validated":
      return { glyph: "\u2713", cls: "ok" };
    case "pending":
      return { glyph: "\u25aa", cls: "" };
    default:
      return { glyph: "\u25cb", cls: "" };
  }
}

export interface TaskRowRendererOptions {
  projectId: string;
  /** Visual order of the rows THIS surface renders, for shift-click. */
  orderedTaskIds: string[];
  /** From the surface's single `useRowActions()`. */
  rowProps: ReturnType<typeof useRowActions>["rowProps"];
  /** Whole-selection verbs, offered on rows that are marked. */
  selectionActions: ActionDescriptor[] | null;
}

export function useTaskRowRenderer({
  projectId,
  orderedTaskIds,
  rowProps,
  selectionActions,
}: TaskRowRendererOptions): (row: DepRow<Task>) => JSX.Element {
  // Preview on single click, pin on double. `openOrReuseInSidebar`
  // retargets the existing task-detail pane in place instead of adding a
  // tab, which is what makes single-click viable at all: `openInSidebar`
  // opens a NEW tab every time, so clicking down a list of twenty tasks
  // would leave twenty panes.
  const previewInSidebar = useWorkspace((s) => s.openOrReuseInSidebar);
  const openInFocus = useWorkspace((s) => s.openInFocus);
  // Holds the single-click preview open long enough for a double-click to
  // cancel it. Enter (via rowProps) previews immediately and deliberately —
  // the keyboard has no double-click to wait for.
  const preview = useDeferredPreview();
  const { pause } = usePauseState();
  const taskCtx = useTaskActionContext(projectId);

  const selProjectId = useSelection((s) => s.projectId);
  const selTaskIds = useSelection((s) => s.taskIds);
  const selFeatureIds = useSelection((s) => s.featureIds);
  const toggleTaskSel = useSelection((s) => s.toggleTask);
  const rangeTaskSel = useSelection((s) => s.rangeTask);
  const selActiveRow = useSelection((s) => s.active);
  const setActive = useSelection((s) => s.setActive);
  const selScoped = selProjectId === projectId;
  const selActive =
    selScoped && (selTaskIds.size > 0 || selFeatureIds.size > 0);

  return (row: DepRow<Task>) => {
    const t = row.node.item;
    const { glyph, cls } = taskGlyph(t.status, !!t.is_abandoned);
    const label = t.title || t.id;
    // Why a task is not running. Null for every task that is running or
    // simply not held — the chip only appears when there is a real answer.
    // Also covers feature_depends_on gating, whose blocking party is a
    // FEATURE and so has no row in this tree to point at.
    const hold = taskHoldReason(t, { pause, projectId });
    // Orthogonal to `hold`: an unresolved feature dep gates nothing, so it
    // can sit on a task that is running perfectly well. Shown alongside
    // rather than instead, because "running" and "ordered by a typo that
    // does nothing" are both true at once.
    const depWarn = featureDepWarning(t);
    const actions = buildTaskActions(t, taskCtx);
    const marked = selScoped && selTaskIds.has(t.id);
    // Single-click select-only highlight — one active row at a time,
    // independent of the checkbox multi-select. Never toggles on
    // selActive: the active state is set directly by a plain click.
    const isActive =
      selActiveRow?.projectId === projectId &&
      selActiveRow.kind === "task" &&
      selActiveRow.id === t.id;
    const rp = rowProps(
      actions,
      label,
      // Selection mode is modal: Enter toggles like a click, it does
      // not open detail.
      selActive
        ? () => toggleTaskSel(projectId, t.id)
        : () =>
            previewInSidebar("task-detail", { projectId, taskId: t.id }, label),
      {
        selectionActions: marked ? (selectionActions ?? undefined) : undefined,
        // Long-press = the touch shift-click.
        onRangeSelect: () => rangeTaskSel(projectId, orderedTaskIds, t.id),
      },
    );

    return (
      <div
        key={t.id}
        className={`trow${marked ? " marked" : ""}${isActive ? " active" : ""}`}
        {...rp}
        onKeyDown={(e) => {
          // Shift+V ranges from the anchor — keyboard parity with
          // shift-click for rows focused via Tab.
          if (isRangeKey(e)) {
            e.preventDefault();
            rangeTaskSel(projectId, orderedTaskIds, t.id);
            return;
          }
          rp.onKeyDown(e);
        }}
        onClick={(e) => {
          if ((e.target as HTMLElement).closest(".selbox")) return;
          // Shift-click anywhere on the row is a selection gesture, not
          // an open: range from the anchor, or start a selection here.
          if (e.shiftKey) {
            rangeTaskSel(projectId, orderedTaskIds, t.id);
            return;
          }
          // Selection mode is modal: clicks toggle, they never open.
          if (selActive) {
            toggleTaskSel(projectId, t.id);
            return;
          }
          // Plain single-click: highlight AND preview in the side panel.
          // The panel is the temporary viewing spot — one pane, reused —
          // so a click is cheap and reversible. Double-click pins the task
          // into Focus, which is the workspace you keep things in.
          // Highlight now, preview shortly. A double-click begins with a
          // single click, so opening the pane here meant the panel flashed
          // open on its way to Focus — and on a slow double-click, Focus
          // never happened. The highlight stays instant so the row still
          // acknowledges the click.
          setActive(projectId, "task", t.id);
          preview.schedule(() =>
            previewInSidebar("task-detail", { projectId, taskId: t.id }, label),
          );
        }}
        onDoubleClick={(e) => {
          if ((e.target as HTMLElement).closest(".selbox")) return;
          // A double-click in multi-select mode must not open — mirror
          // the click guards.
          if (selActive) return;
          preview.cancel();
          openInFocus("task-detail", { projectId, taskId: t.id }, label);
        }}
        // Shift-click would otherwise extend the browser's text
        // selection across the rows before our click handler runs. The
        // explicit focus() keeps what preventDefault suppresses: row
        // accelerators must target the row that was last clicked.
        onMouseDown={(e) => {
          if (e.shiftKey) {
            e.preventDefault();
            e.currentTarget.focus();
          }
        }}
        draggable
        onDragStart={(e) =>
          beginDrag(e, {
            source: "task-row",
            kind: "task-detail",
            target: { projectId, taskId: t.id },
            title: label,
          })
        }
        onDragEnd={endDrag}
      >
        {/* Checkbox and status glyph share one grid cell. The glyph holds
          it unless `boxed` (marked, or selection mode is on) swaps the
          checkbox in. A plain click reads as colour only — `.active`.
          Keeps the 4-column row layout intact. */}
        <span className={`glyph-slot${marked || selActive ? " boxed" : ""}`}>
          <span
            className={`selbox${marked ? " on" : ""}`}
            role="checkbox"
            aria-checked={marked}
            aria-label={`Select ${label}`}
            onClick={(e) => {
              e.stopPropagation();
              if (e.shiftKey) rangeTaskSel(projectId, orderedTaskIds, t.id);
              else toggleTaskSel(projectId, t.id);
            }}
          />
          <span className={`glyph ${cls}`}>{glyph}</span>
        </span>
        <span className="name">
          <DepGuide
            prefix={row.prefix}
            inCycle={row.node.inCycle}
            extraDeps={row.node.extraDeps}
            extraLabel="tasks"
          />
          {label}
        </span>
        <span className="status">
          {t.status}
          {hold && (
            <span className={`hold-chip ${hold.code}`} title={hold.detail}>
              {hold.glyph} {hold.short}
            </span>
          )}
          {depWarn && (
            <span
              className={`hold-chip ${depWarn.code}`}
              title={depWarn.detail}
            >
              {depWarn.glyph} {depWarn.short}
            </span>
          )}
        </span>
        <span className="id">{t.id.slice(0, 6)}</span>
      </div>
    );
  };
}
