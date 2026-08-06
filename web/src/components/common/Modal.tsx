/**
 * Modal — wireframe-parity port.
 *
 * DOM (matches wireframe panes-v2.css):
 *   .modal-scrim
 *   .modal
 *     .modal-head (title + close)
 *     .modal-tabs (optional)
 *     .modal-body
 *     .modal-foot (optional)
 *
 * Portaled to document.body. Escape closes; scrim-click closes;
 * body scroll is locked while open; focus is trapped loosely.
 */
import React, { useEffect, useRef } from "react";
import { createPortal } from "react-dom";

export interface ModalTab {
  id: string;
  label: React.ReactNode;
  disabled?: boolean;
}

export interface ModalProps {
  title: React.ReactNode;
  onClose: () => void;
  children?: React.ReactNode;
  footer?: React.ReactNode;
  tabs?: ModalTab[];
  activeTab?: string;
  onTabChange?: (id: string) => void;
  className?: string;
  closeOnScrimClick?: boolean;
  closeOnEscape?: boolean;
  /** Change this value to force the modal to re-run firstFocusable and
   *  refocus the primary action. Useful for multi-view modals
   *  (menu → confirmForce) where the primary button changes across views;
   *  without this, focus stays on whatever was focused when the modal
   *  first mounted. Optional — modals with a single view can omit it. */
  refocusKey?: string;
}

export function handleModalKeyDown(
  e: KeyboardEvent | React.KeyboardEvent,
  onClose: () => void,
  closeOnEscape = true,
): void {
  if (!closeOnEscape) return;
  if (e.key === "Escape") {
    e.preventDefault();
    onClose();
  }
}

const FOCUSABLE =
  'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]),' +
  ' textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

function firstFocusable(root: HTMLElement | null): HTMLElement | null {
  if (!root) return null;
  // Modal children can opt out of the natural DOM-order fallback by tagging
  // a primary action with data-autofocus="true". This is how TaskActions
  // Modal + FeatureActionsModal keep keyboard-users from landing on the ×
  // close button (which lives in .modal-head, rendered before the primary
  // action). Preferred over the DOM-order default; fall back if unset.
  const preferred = root.querySelector<HTMLElement>('[data-autofocus="true"]');
  if (preferred && !preferred.hasAttribute("disabled")) return preferred;
  return root.querySelector<HTMLElement>(FOCUSABLE);
}

export function Modal({
  title,
  onClose,
  children,
  footer,
  tabs,
  activeTab,
  onTabChange,
  className,
  closeOnScrimClick = true,
  closeOnEscape = true,
  refocusKey,
}: ModalProps): JSX.Element | null {
  const scrimRef = useRef<HTMLDivElement | null>(null);
  const dialogRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    const opener =
      typeof document !== "undefined" &&
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
    const target = firstFocusable(dialogRef.current) ?? dialogRef.current;
    target?.focus({ preventScroll: true });
    return () => {
      if (opener && opener.isConnected) opener.focus({ preventScroll: true });
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [refocusKey]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const modals =
        typeof document !== "undefined"
          ? document.querySelectorAll(".modal-scrim")
          : [];
      if (modals.length && modals[modals.length - 1] !== scrimRef.current)
        return;
      handleModalKeyDown(e, onClose, closeOnEscape);
    };
    window.addEventListener("keydown", onKey);
    const prev =
      typeof document !== "undefined" ? document.body.style.overflow : "";
    if (typeof document !== "undefined")
      document.body.style.overflow = "hidden";
    return () => {
      window.removeEventListener("keydown", onKey);
      if (typeof document !== "undefined")
        document.body.style.overflow = prev;
    };
  }, [onClose, closeOnEscape]);

  const modalCls = ["modal", className].filter(Boolean).join(" ");

  const contents = (
    <>
      <div
        ref={scrimRef}
        className="modal-scrim"
        onMouseDown={(e) => {
          if (closeOnScrimClick && e.target === e.currentTarget) onClose();
        }}
      />
      <div
        ref={dialogRef}
        className={modalCls}
        role="dialog"
        aria-modal="true"
        aria-label={typeof title === "string" ? title : undefined}
        tabIndex={-1}
      >
        <div className="modal-head">
          <div className="modal-title">{title}</div>
          <button
            type="button"
            className="modal-close"
            onClick={onClose}
            aria-label="Close"
            title="Close (Esc)"
          >
            ×
          </button>
        </div>

        {tabs && tabs.length > 0 && (
          <div className="modal-tabs" role="tablist">
            {tabs.map((t) => (
              <button
                type="button"
                key={t.id}
                className={
                  "modal-tab " + (t.id === activeTab ? "active" : "")
                }
                role="tab"
                aria-selected={t.id === activeTab}
                disabled={t.disabled}
                onClick={() => {
                  if (!t.disabled && onTabChange) onTabChange(t.id);
                }}
                style={{ border: 0, background: "transparent" }}
              >
                {t.label}
              </button>
            ))}
          </div>
        )}

        <div className="modal-body">{children}</div>

        {footer && <div className="modal-foot">{footer}</div>}
      </div>
    </>
  );

  if (typeof document === "undefined") return contents;
  return createPortal(contents, document.body);
}
