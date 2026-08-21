/**
 * panes-v2 ContextMenu primitive.
 *
 * A right-click menu rendered via portal into `document.body`. Dismissed
 * by:
 *   • Escape
 *   • Left-click anywhere outside
 *   • Another right-click anywhere
 *
 * The API has two shapes:
 *
 *   1) Direct render:
 *        <ContextMenu x={e.clientX} y={e.clientY}
 *                     items={[{id, label, onClick}, ...]}
 *                     onClose={...}
 *                     onSelect={...} />
 *      Rendered only when items.length > 0.
 *
 *   2) Hook:
 *        const {menu, open, close} = useContextMenu();
 *        <div onContextMenu={(e) => {
 *          e.preventDefault();
 *          open(e.clientX, e.clientY, itemsForRow);
 *        }} />
 *        {menu}
 *
 * The hook wraps a <ContextMenu> renderer with local state so consumers
 * don't have to manage x/y/items themselves.
 *
 * See web/src/styles/common.css `.p2-ctx`.
 */
import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";

export interface ContextMenuItem {
  id: string;
  label?: React.ReactNode;
  glyph?: React.ReactNode;
  shortcut?: React.ReactNode;
  danger?: boolean;
  disabled?: boolean;
  /** Tooltip (title attr) — used for disabled reasons, so a greyed item
   *  explains itself on hover the way the sheet's subtitle does. */
  tooltip?: string;
  /** Optional: render as a group header instead of a clickable item. */
  section?: boolean;
  /** Optional: render as a horizontal separator instead of an item. */
  separator?: boolean;
  /** Called when the item is chosen (before onSelect on the menu). */
  onClick?: () => void;
}

export interface ContextMenuProps {
  x: number;
  y: number;
  items: ContextMenuItem[];
  onClose: () => void;
  onSelect?: (id: string) => void;
  className?: string;
  /** Viewport size override (for tests). Reads window.* when omitted. */
  viewport?: { width: number; height: number };
}

/**
 * clampContextMenuPosition — pure helper. Given a click point and the
 * menu's measured size, returns the left/top style values that keep
 * the menu on-screen with a 4px inset from the viewport edges.
 */
export function clampContextMenuPosition(input: {
  x: number;
  y: number;
  menuWidth: number;
  menuHeight: number;
  viewportWidth: number;
  viewportHeight: number;
  inset?: number;
}): { left: number; top: number } {
  const inset = input.inset ?? 4;
  const maxLeft = Math.max(0, input.viewportWidth - input.menuWidth - inset);
  const maxTop = Math.max(0, input.viewportHeight - input.menuHeight - inset);
  const left = Math.max(0, Math.min(input.x, maxLeft));
  const top = Math.max(0, Math.min(input.y, maxTop));
  return { left, top };
}

/** Pure predicate: does this key dismiss the context menu? */
export function isDismissContextMenuKey(key: string): boolean {
  return key === "Escape";
}

export function ContextMenu({
  x,
  y,
  items,
  onClose,
  onSelect,
  className,
  viewport,
}: ContextMenuProps): JSX.Element | null {
  const menuRef = useRef<HTMLDivElement | null>(null);
  const [pos, setPos] = useState<{ left: number; top: number }>({
    left: x,
    top: y,
  });

  // After first render, measure and clamp into the viewport.
  useEffect(() => {
    if (!menuRef.current) return;
    const rect = menuRef.current.getBoundingClientRect();
    const vw =
      viewport?.width ?? (typeof window !== "undefined" ? window.innerWidth : 800);
    const vh =
      viewport?.height ??
      (typeof window !== "undefined" ? window.innerHeight : 600);
    setPos(
      clampContextMenuPosition({
        x,
        y,
        menuWidth: rect.width,
        menuHeight: rect.height,
        viewportWidth: vw,
        viewportHeight: vh,
      }),
    );
  }, [x, y, viewport?.width, viewport?.height]);

  // Dismissal listeners.
  useEffect(() => {
    if (typeof document === "undefined") return;
    const onKey = (e: KeyboardEvent) => {
      if (isDismissContextMenuKey(e.key)) {
        e.preventDefault();
        onClose();
      }
    };
    const onDown = (e: MouseEvent) => {
      // Any click outside dismisses; the click on an item is handled by
      // the item's own onClick and closes explicitly.
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        onClose();
      }
    };
    const onCtx = (e: MouseEvent) => {
      // Another right-click anywhere dismisses this menu. The consumer
      // (or the second target) can then open its own menu.
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        onClose();
      }
    };
    // Attach one task later, not synchronously: React flushes this
    // effect during the discrete contextmenu event that OPENED the
    // menu, so the same still-bubbling event would reach these window
    // listeners and dismiss the menu the instant it appeared (it
    // flashed for one frame and closed — synthetic events in tests
    // finish bubbling before effects flush, which is why only real
    // right-clicks ever hit this).
    const attach = window.setTimeout(() => {
      window.addEventListener("keydown", onKey);
      window.addEventListener("mousedown", onDown);
      window.addEventListener("contextmenu", onCtx);
    }, 0);
    return () => {
      window.clearTimeout(attach);
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("mousedown", onDown);
      window.removeEventListener("contextmenu", onCtx);
    };
  }, [onClose]);

  // Nothing to show → render null (matches test expectation).
  if (!items || items.length === 0) return null;

  const cls = ["ctxmenu", className].filter(Boolean).join(" ");

  const menuNode = (
    <div
      ref={menuRef}
      className={cls}
      role="menu"
      style={{ left: pos.left, top: pos.top }}
    >
      {items.map((item) => {
        if (item.separator) {
          return <div key={item.id} className="sep" role="separator" />;
        }
        if (item.section) {
          return (
            <div key={item.id} className="lbl">
              {item.label}
            </div>
          );
        }
        return (
          <button
            key={item.id}
            type="button"
            role="menuitem"
            disabled={item.disabled}
            title={item.tooltip || undefined}
            style={item.danger ? { color: "#d96060" } : undefined}
            onClick={() => {
              item.onClick?.();
              onSelect?.(item.id);
              onClose();
            }}
          >
            {item.glyph !== undefined && (
              <span aria-hidden="true">{item.glyph}</span>
            )}
            <span style={{ flex: 1 }}>{item.label}</span>
            {item.shortcut && (
              <span style={{ color: "#6b757e", fontSize: 10 }}>
                {item.shortcut}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );

  if (typeof document === "undefined") return menuNode;
  return createPortal(menuNode, document.body);
}

/* ─── useContextMenu hook ──────────────────────────────────────────── */

export interface UseContextMenuAPI {
  /** JSX to render at the end of the consumer's tree. */
  menu: React.ReactNode;
  /** Open at the given viewport coords with the given items. */
  open: (x: number, y: number, items: ContextMenuItem[]) => void;
  /** Manually close (also happens on outside click / Esc / re-open). */
  close: () => void;
  /** True while a menu is being displayed. */
  isOpen: boolean;
}

export function useContextMenu(): UseContextMenuAPI {
  const [state, setState] = useState<
    | { open: false }
    | { open: true; x: number; y: number; items: ContextMenuItem[] }
  >({ open: false });

  const open = useCallback(
    (x: number, y: number, items: ContextMenuItem[]) => {
      setState({ open: true, x, y, items });
    },
    [],
  );
  const close = useCallback(() => {
    setState({ open: false });
  }, []);

  const menu = useMemo(() => {
    if (!state.open) return null;
    return (
      <ContextMenu
        x={state.x}
        y={state.y}
        items={state.items}
        onClose={close}
      />
    );
  }, [state, close]);

  return { menu, open, close, isOpen: state.open };
}
