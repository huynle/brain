import { create } from "zustand";

export type View = "tasks" | "brain" | "automations" | "runners" | "control" | "logs";

export const ALL_PROJECTS = "__all__";

export interface Toast {
  id: number;
  kind: "info" | "success" | "error";
  message: string;
}

// Content-tab order for H/L cycling — matches the TUI tab bar order
// (global tabs first, then project-scoped).
export const VIEW_ORDER: View[] = [
  "runners",
  "control",
  "logs",
  "brain",
  "tasks",
  "automations",
];

export type Panel = "tasks" | "detail" | "logs";

// A mobile "inspect" request: open the Detail/Logs bottom sheet for one entry.
// Carries enough to drive EntryDetailPane (path) and EntryLogsPane (task id +
// project). Set by a row tap on a touch device; consumed by MobileInspectSheet.
export interface InspectTarget {
  path: string;
  title?: string;
  taskId?: string;
  projectId?: string;
}

// A request to open a specific instance/session in the Control tab — set by
// other views (e.g. Automations "o") and consumed by ControlView on mount.
//
// "live" targets a running instance directly. "history" targets a session that
// may no longer have a live instance: ControlView resolves a connected runner
// on the recorded machine, shows the read-only transcript, and offers resume.
export interface ControlTarget {
  mode: "live" | "history";
  runnerId: string; // recorded/owning runner (may be offline)
  instanceId?: string; // present for live targets
  sessionId?: string;
  machineId?: string; // recorded machine, for same-host fallback resolution
  hostname?: string;
  workdir?: string; // recorded workdir, enables resume
  taskTitle?: string;
}

interface UIState {
  view: View;
  activeProject: string; // project id or ALL_PROJECTS
  logFilter: string; // task id filter applied when opening the logs view
  controlTarget: ControlTarget | null; // pending Control attach request
  settingsOpen: boolean;
  wrap: boolean; // text-wrap toggle (w)
  detailVisible: boolean; // T
  logsVisible: boolean; // z
  focus: Panel; // focused panel within the Tasks view
  projectSheetOpen: boolean; // mobile: project picker bottom sheet
  inspect: InspectTarget | null; // mobile: Detail/Logs bottom sheet target
  toasts: Toast[];
  _tid: number;
  // Set when a new service-worker build is waiting; calling it applies the
  // update and reloads. Null when no update is pending.
  updateApply: (() => void) | null;
  setUpdateApply: (fn: (() => void) | null) => void;
  setView: (v: View) => void;
  cycleView: (dir: 1 | -1) => void;
  setActiveProject: (p: string) => void;
  showLogsFor: (taskId: string) => void;
  setLogFilter: (f: string) => void;
  openInControl: (target: ControlTarget) => void;
  consumeControlTarget: () => ControlTarget | null;
  setSettingsOpen: (open: boolean) => void;
  toggleWrap: () => void;
  toggleDetail: () => void;
  toggleLogs: () => void;
  cycleFocus: () => void;
  setFocus: (p: Panel) => void;
  setProjectSheetOpen: (open: boolean) => void;
  openInspect: (t: InspectTarget) => void;
  closeInspect: () => void;
  toast: (message: string, kind?: Toast["kind"]) => void;
  dismissToast: (id: number) => void;
}

// Persist the selected project across reloads so a refresh returns to what you
// were working on instead of resetting to "all projects".
const ACTIVE_PROJECT_KEY = "brain.active_project";
function loadActiveProject(): string {
  try {
    return localStorage.getItem(ACTIVE_PROJECT_KEY) || ALL_PROJECTS;
  } catch {
    return ALL_PROJECTS;
  }
}

export const useUI = create<UIState>((set, get) => ({
  view: "tasks",
  activeProject: loadActiveProject(),
  logFilter: "",
  controlTarget: null,
  settingsOpen: false,
  wrap: false,
  detailVisible: true,
  logsVisible: true,
  focus: "tasks",
  projectSheetOpen: false,
  inspect: null,
  toasts: [],
  _tid: 0,
  setView: (v) => set({ view: v }),
  cycleView: (dir) =>
    set((s) => {
      const i = VIEW_ORDER.indexOf(s.view);
      const next = (i + dir + VIEW_ORDER.length) % VIEW_ORDER.length;
      return { view: VIEW_ORDER[next] };
    }),
  toggleDetail: () => set((s) => ({ detailVisible: !s.detailVisible })),
  toggleLogs: () => set((s) => ({ logsVisible: !s.logsVisible })),
  setFocus: (p) => set({ focus: p }),
  cycleFocus: () =>
    set((s) => {
      const panels: Panel[] = ["tasks"];
      if (s.detailVisible) panels.push("detail");
      if (s.logsVisible) panels.push("logs");
      const i = panels.indexOf(s.focus);
      return { focus: panels[(i + 1) % panels.length] };
    }),
  setActiveProject: (p) => {
    try {
      localStorage.setItem(ACTIVE_PROJECT_KEY, p);
    } catch {
      /* ignore storage errors (private mode, quota) */
    }
    set({ activeProject: p });
  },
  // The Logs tab is the global server-request log now; a task's own output
  // lives in the Logs pane under Tasks, so drill-downs open that instead.
  showLogsFor: (taskId) => set({ view: "tasks", logsVisible: true, logFilter: taskId }),
  setLogFilter: (f) => set({ logFilter: f }),
  openInControl: (target) => set({ controlTarget: target, view: "control" }),
  consumeControlTarget: () => {
    const t = get().controlTarget;
    if (t) set({ controlTarget: null });
    return t;
  },
  setSettingsOpen: (open) => set({ settingsOpen: open }),
  setProjectSheetOpen: (open) => set({ projectSheetOpen: open }),
  openInspect: (t) => set({ inspect: t }),
  closeInspect: () => set({ inspect: null }),
  toggleWrap: () => set((s) => ({ wrap: !s.wrap })),
  updateApply: null,
  setUpdateApply: (fn) => set({ updateApply: fn }),
  toast: (message, kind = "info") => {
    const id = get()._tid + 1;
    set((s) => ({ _tid: id, toasts: [...s.toasts, { id, kind, message }] }));
    window.setTimeout(() => get().dismissToast(id), kind === "error" ? 6000 : 3500);
  },
  dismissToast: (id) =>
    set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
}));
