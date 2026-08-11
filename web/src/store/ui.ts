/**
 * Minimal UI store — preserved for the PWA update flow and toast host.
 *
 * As of Phase 9 the old dashboard is deleted and the panes-v2 shell owns
 * all view/nav/panel/scope state via `store/workspace` and `store/modal`.
 * The one thing that lives above that layer is the PWA update prompt:
 * `main.tsx` registers the service worker and, on `onNeedRefresh`, calls
 * `useUI.getState().setUpdateApply(...)`. The `<UpdateBanner />` component
 * reads that and offers a Reload button. `<Toasts />` renders transient
 * user notifications (used by v2 modals to signal outcomes of API calls).
 *
 * Kept here (not folded into `store/workspace`) so main.tsx can call
 * `setUpdateApply` before any React tree mounts, without pulling in the
 * heavier v2 workspace store. If the toast host is later reimplemented
 * on top of a v2 primitive, this store can shrink to just `updateApply`.
 */
import { create } from "zustand";

export interface Toast {
  id: number;
  // "warning" covers the partial outcome a bulk fan-out can produce —
  // "7 of 9 updated" is neither a success nor an error, and rendering it
  // as either misleads.
  kind: "info" | "success" | "error" | "warning";
  message: string;
  action?: {
    label: string;
    onClick: () => void | Promise<void>;
  };
}

interface UIState {
  /** Ordered list of visible toasts. Rendered by `<Toasts />`. */
  toasts: Toast[];
  /** Internal monotonic id counter for new toasts. */
  _tid: number;
  /** Show a toast. Auto-dismisses after `duration` ms (default 4000). */
  toast: (
    message: string,
    kind?: Toast["kind"],
    options?: { duration?: number; action?: Toast["action"] },
  ) => number;
  /** Remove a toast by id. */
  dismissToast: (id: number) => void;

  /** Set by `main.tsx` when a new PWA build is ready. When truthy, the
   *  `<UpdateBanner />` renders and offers a Reload button that calls this. */
  updateApply: (() => Promise<void> | void) | null;
  setUpdateApply: (fn: (() => Promise<void> | void) | null) => void;
}

export const useUI = create<UIState>((set, get) => ({
  toasts: [],
  _tid: 0,
  toast: (message, kind = "info", options) => {
    const id = get()._tid + 1;
    set((s) => ({ _tid: id, toasts: [...s.toasts, { id, kind, message, action: options?.action }] }));
    const duration = options?.duration ?? 4000;
    if (duration > 0) {
      window.setTimeout(() => get().dismissToast(id), duration);
    }
    return id;
  },
  dismissToast: (id) =>
    set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),

  updateApply: null,
  setUpdateApply: (fn) => set({ updateApply: fn }),
}));
