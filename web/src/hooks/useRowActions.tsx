/**
 * useRowActions — binds an ActionDescriptor list to a row on all three
 * input surfaces at once.
 *
 * Consumers get a props bundle to spread onto the row element, plus an
 * `overlays` node to render. Adding a verb to a builder makes it appear on
 * right-click, long-press and the keyboard simultaneously — which is the
 * whole reason the registry exists.
 *
 *   desktop   right-click        → ContextMenu
 *   touch     long-press         → ActionSheet
 *   keyboard  single-key / Enter → direct invoke
 *
 * Keyboard note: accelerators only fire while the row has focus, and only
 * for enabled actions (`findByKey` filters). Bare letter keys are safe
 * here precisely because focus is scoped to a row — they are inert
 * everywhere else, and typing contexts are excluded below.
 */
import React, { useCallback, useState } from "react";

import { ActionSheet } from "../components/common/ActionSheet";
import {
  useContextMenu,
  type ContextMenuItem,
} from "../components/common/ContextMenu";
import { useActionRunner } from "./useActionRunner";
import { useIsMobile } from "./useIsMobile";
import { createLongPressHandlers } from "../lib/longPress";
import {
  findByKey,
  groupActions,
  isEnabled,
  type ActionDescriptor,
} from "../lib/actions/types";

/**
 * The row's Select verb, when it has one that is enabled. Long-press runs
 * this directly (marking the row and entering selection mode) instead of
 * opening the action sheet — hover does not exist on touch, so the
 * checkbox reveal needs a first-class gesture. Rows without a Select verb
 * (goals, automations) keep the sheet on long-press.
 */
export function selectActionOf(
  actions: readonly ActionDescriptor[],
): ActionDescriptor | undefined {
  return actions.find((a) => a.id === "select" && isEnabled(a));
}

export interface RowActionProps {
  onContextMenu: (e: React.MouseEvent) => void;
  onKeyDown: (e: React.KeyboardEvent) => void;
  onTouchStart: (e: React.TouchEvent) => void;
  onTouchMove: (e: React.TouchEvent) => void;
  onTouchEnd: () => void;
  onTouchCancel: () => void;
  onClickCapture: (e: React.MouseEvent) => void;
  tabIndex: number;
  role: string;
  "aria-label": string;
}

export interface UseRowActionsAPI {
  /**
   * Build the props for one row.
   *
   * @param actions   the row's descriptors
   * @param label     accessible name, and the action sheet's heading
   * @param onActivate what Enter/Space does (usually "open detail")
   */
  rowProps: (
    actions: readonly ActionDescriptor[],
    label: string,
    onActivate?: () => void,
    opts?: {
      /**
       * The selection-wide verbs (lib/actions/selectionActions). A card
       * passes them for rows that are MARKED while selection mode is
       * active: right-click, long-press and the `m` key then offer the
       * verbs for the whole selection instead of the row's own — the
       * gesture targets "everything I marked", not the row under the
       * cursor. Unmarked rows keep their own menu.
       */
      selectionActions?: readonly ActionDescriptor[];
      /**
       * Touch equivalent of shift-click: long-press on an UNMARKED row
       * ranges from the selection anchor to this row (and with no
       * anchor yet, selects just this row — which is exactly what the
       * old long-press-to-mark did, plus it seeds the anchor). Marked
       * rows are unaffected: their long-press opens the selection
       * sheet above.
       */
      onRangeSelect?: () => void;
    },
  ) => RowActionProps;
  /** Render once per consumer, at the end of its tree. */
  overlays: JSX.Element;
}

/** Convert descriptors into ContextMenu items, preserving group separators.
 *  Exported so ActionBar's desktop "More…" menu renders the identical list. */
export function toMenuItems(
  actions: readonly ActionDescriptor[],
  run: (a: ActionDescriptor) => void,
): ContextMenuItem[] {
  const groups = groupActions(actions);
  const items: ContextMenuItem[] = [];
  groups.forEach((group, gi) => {
    if (gi > 0) items.push({ id: `sep-${gi}`, separator: true, label: "" });
    for (const a of group) {
      items.push({
        id: a.id,
        // The accelerator is shown inline so the menu teaches the shortcut
        // rather than hiding it in a help overlay nobody opens.
        label: a.key ? `${a.label}  (${a.key})` : a.label,
        disabled: !isEnabled(a),
        // A greyed item must explain itself — the sheet shows the reason
        // as a subtitle; the menu shows it on hover.
        tooltip: a.disabledReason || undefined,
        danger: a.danger,
        onClick: () => run(a),
      });
    }
  });
  return items;
}

/** True when a keystroke landed in a text-entry context. */
function isTypingTarget(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null;
  if (!el) return false;
  return (
    el.tagName === "INPUT" ||
    el.tagName === "TEXTAREA" ||
    el.tagName === "SELECT" ||
    el.isContentEditable
  );
}

export function useRowActions(): UseRowActionsAPI {
  const ctx = useContextMenu();
  const runner = useActionRunner();
  const isMobile = useIsMobile();
  const [sheet, setSheet] = useState<{
    title: string;
    actions: readonly ActionDescriptor[];
  } | null>(null);

  const openSheet = useCallback(
    (title: string, actions: readonly ActionDescriptor[]) => {
      setSheet({ title, actions });
    },
    [],
  );

  const rowProps = useCallback(
    (
      actions: readonly ActionDescriptor[],
      label: string,
      onActivate?: () => void,
      opts?: {
        selectionActions?: readonly ActionDescriptor[];
        onRangeSelect?: () => void;
      },
    ): RowActionProps => {
      const selectAction = selectActionOf(actions);
      // A marked row's menu surfaces act on the whole selection.
      const selectionActions =
        opts?.selectionActions && opts.selectionActions.length > 0
          ? opts.selectionActions
          : null;
      const menuActions = selectionActions ?? actions;
      const menuLabel = selectionActions ? "Selection" : label;
      // Long-press: the selection gesture on touch. On a marked row it
      // opens the selection sheet (the touch right-click); on an
      // unmarked row it ranges from the anchor — the touch shift-click
      // (with no anchor that degrades to marking just this row, the
      // old behavior plus the anchor seed). The full sheet remains the
      // fallback for rows with neither.
      const press = createLongPressHandlers(() => {
        if (selectionActions) openSheet(menuLabel, selectionActions);
        else if (opts?.onRangeSelect) opts.onRangeSelect();
        else if (selectAction) runner.run(selectAction);
        else openSheet(label, actions);
      });

      const openMenuAt = (x: number, y: number) => {
        // Touch devices get the sheet; a context menu positioned under a
        // fingertip is unusable.
        if (isMobile) openSheet(menuLabel, menuActions);
        else ctx.open(x, y, toMenuItems(menuActions, runner.run));
      };

      return {
        role: "button",
        // 0, not -1: rows must be reachable by Tab at all. Anything more
        // ergonomic is a refinement on top of this, not a replacement.
        tabIndex: 0,
        "aria-label": label,

        onContextMenu: (e: React.MouseEvent) => {
          e.preventDefault();
          openMenuAt(e.clientX, e.clientY);
        },

        onKeyDown: (e: React.KeyboardEvent) => {
          // Never fight browser/OS chords, and never hijack typing.
          if (e.metaKey || e.ctrlKey || e.altKey) return;
          if (isTypingTarget(e.target)) return;

          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            onActivate?.();
            return;
          }

          // "m" opens the full menu — the discovery path for anyone who
          // has not memorised the accelerators.
          if (e.key === "m") {
            e.preventDefault();
            const rect = (
              e.currentTarget as HTMLElement
            ).getBoundingClientRect();
            openMenuAt(rect.left + 8, rect.bottom);
            return;
          }

          const hit = findByKey(actions, e.key);
          if (hit) {
            e.preventDefault();
            runner.run(hit);
          }
        },

        onTouchStart: press.onTouchStart as (e: React.TouchEvent) => void,
        onTouchMove: press.onTouchMove as (e: React.TouchEvent) => void,
        onTouchEnd: press.onTouchEnd,
        onTouchCancel: press.onTouchCancel,
        onClickCapture: (e: React.MouseEvent) => {
          (press.onClickCapture as (e: React.MouseEvent) => void)(e);
        },
      };
    },
    [ctx, isMobile, openSheet, runner],
  );

  const overlays = (
    <>
      {ctx.menu}
      {sheet && (
        <ActionSheet
          title={sheet.title}
          actions={sheet.actions}
          onSelect={runner.run}
          onClose={() => setSheet(null)}
        />
      )}
      {runner.dialog}
    </>
  );

  return { rowProps, overlays };
}
