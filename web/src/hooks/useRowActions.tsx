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
       * When true (a card in selection mode passes its selActive flag),
       * plain taps on touch devices toggle the row's selection instead
       * of opening detail — long-press the first row, then tap through
       * the rest. Desktop clicks keep opening detail; the visible
       * checkboxes are the toggle surface there.
       */
      tapSelects?: boolean;
    },
  ) => RowActionProps;
  /** Render once per consumer, at the end of its tree. */
  overlays: JSX.Element;
}

/** Convert descriptors into ContextMenu items, preserving group separators. */
function toMenuItems(
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
      opts?: { tapSelects?: boolean },
    ): RowActionProps => {
      const selectAction = selectActionOf(actions);
      // Long-press: the selection gesture on touch. Sheet remains the
      // fallback for rows with no Select verb; every other verb stays
      // reachable through the row's detail modal ActionBar.
      const press = createLongPressHandlers(() => {
        if (selectAction) runner.run(selectAction);
        else openSheet(label, actions);
      });

      const openMenuAt = (x: number, y: number) => {
        // Touch devices get the sheet; a context menu positioned under a
        // fingertip is unusable.
        if (isMobile) openSheet(label, actions);
        else ctx.open(x, y, toMenuItems(actions, runner.run));
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
          // Selection mode on touch: taps toggle instead of opening
          // detail, so extending a selection is tap-tap-tap rather than
          // long-press each time.
          if (opts?.tapSelects && isMobile && selectAction) {
            e.preventDefault();
            e.stopPropagation();
            runner.run(selectAction);
            return;
          }
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
