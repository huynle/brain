import { create } from "zustand";

export type View = "tasks" | "brain" | "automations" | "runners" | "logs";

export const ALL_PROJECTS = "__all__";

export interface Toast {
  id: number;
  kind: "info" | "success" | "error";
  message: string;
}

// Content-tab order for H/L cycling — matches the bottom-nav order.
export const VIEW_ORDER: View[] = [
  "tasks",
  "automations",
  "brain",
  "logs",
  "runners",
];

interface UIState {
  view: View;
  activeProject: string; // project id or ALL_PROJECTS
  logFilter: string; // task id filter applied when opening the logs view
  settingsOpen: boolean;
  wrap: boolean; // text-wrap toggle (w)
  toasts: Toast[];
  _tid: number;
  setView: (v: View) => void;
  cycleView: (dir: 1 | -1) => void;
  setActiveProject: (p: string) => void;
  showLogsFor: (taskId: string) => void;
  setLogFilter: (f: string) => void;
  setSettingsOpen: (open: boolean) => void;
  toggleWrap: () => void;
  toast: (message: string, kind?: Toast["kind"]) => void;
  dismissToast: (id: number) => void;
}

export const useUI = create<UIState>((set, get) => ({
  view: "tasks",
  activeProject: ALL_PROJECTS,
  logFilter: "",
  settingsOpen: false,
  wrap: false,
  toasts: [],
  _tid: 0,
  setView: (v) => set({ view: v }),
  cycleView: (dir) =>
    set((s) => {
      const i = VIEW_ORDER.indexOf(s.view);
      const next = (i + dir + VIEW_ORDER.length) % VIEW_ORDER.length;
      return { view: VIEW_ORDER[next] };
    }),
  setActiveProject: (p) => set({ activeProject: p }),
  showLogsFor: (taskId) => set({ view: "logs", logFilter: taskId }),
  setLogFilter: (f) => set({ logFilter: f }),
  setSettingsOpen: (open) => set({ settingsOpen: open }),
  toggleWrap: () => set((s) => ({ wrap: !s.wrap })),
  toast: (message, kind = "info") => {
    const id = get()._tid + 1;
    set((s) => ({ _tid: id, toasts: [...s.toasts, { id, kind, message }] }));
    window.setTimeout(() => get().dismissToast(id), kind === "error" ? 6000 : 3500);
  },
  dismissToast: (id) =>
    set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
}));
