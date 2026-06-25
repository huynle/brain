import { create } from "zustand";

export type View = "tasks" | "brain" | "automations" | "runners" | "logs";

export const ALL_PROJECTS = "__all__";

export interface Toast {
  id: number;
  kind: "info" | "success" | "error";
  message: string;
  // Optional inline action. When set, the toast renders a button that
  // triggers `action.onClick`. Used to surface recovery flows (e.g. "Force"
  // when a task is blocked by an existing dispatch lease) without forcing
  // the user into a separate modal.
  action?: {
    label: string;
    onClick: () => void | Promise<void>;
  };
}

// Content-tab order for H/L cycling — matches the TUI tab bar order
// (global tabs first, then project-scoped).
export const VIEW_ORDER: View[] = [
  "runners",
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
  assistantOpen: boolean; // overlay: mobile bottom sheet / narrow-viewport drawer
  assistantSidebar: boolean; // desktop persistent right sidebar (when wide enough)
  assistantWidth: number; // sidebar width in px (drag-to-resize)
  assistantFocusSeq: number; // increments when the prompt should receive focus
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
  updateApply: (() => Promise<void> | void) | null;
  setUpdateApply: (fn: (() => Promise<void> | void) | null) => void;
  setView: (v: View) => void;
  cycleView: (dir: 1 | -1) => void;
  setActiveProject: (p: string) => void;
  showLogsFor: (taskId: string) => void;
  setLogFilter: (f: string) => void;
  openInControl: (target: ControlTarget) => void;
  consumeControlTarget: () => ControlTarget | null;
  setSettingsOpen: (open: boolean) => void;
  setAssistantOpen: (open: boolean) => void;
  setAssistantSidebar: (visible: boolean) => void;
  setAssistantWidth: (px: number) => void;
  focusAssistantPrompt: () => void;
  toggleWrap: () => void;
  toggleDetail: () => void;
  toggleLogs: () => void;
  cycleFocus: () => void;
  setFocus: (p: Panel) => void;
  setProjectSheetOpen: (open: boolean) => void;
  openInspect: (t: InspectTarget) => void;
  closeInspect: () => void;
  toast: (
    message: string,
    kind?: Toast["kind"],
    options?: { action?: Toast["action"]; durationMs?: number },
  ) => void;
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

// Persist the assistant sidebar's visibility and width so a reload returns to
// the same layout. Defaults: visible at 380px on first load.
const ASSISTANT_SIDEBAR_KEY = "brain.assistant_sidebar"; // "1" | "0"
const ASSISTANT_WIDTH_KEY = "brain.assistant_width"; // integer string, px
const ASSISTANT_WIDTH_DEFAULT = 380;
const ASSISTANT_WIDTH_MIN = 280;
const ASSISTANT_WIDTH_MAX = 720;
function loadAssistantSidebar(): boolean {
  try {
    const v = localStorage.getItem(ASSISTANT_SIDEBAR_KEY);
    return v === null ? true : v === "1";
  } catch {
    return true;
  }
}
function loadAssistantWidth(): number {
  try {
    const raw = localStorage.getItem(ASSISTANT_WIDTH_KEY);
    if (!raw) return ASSISTANT_WIDTH_DEFAULT;
    const n = Number.parseInt(raw, 10);
    if (!Number.isFinite(n)) return ASSISTANT_WIDTH_DEFAULT;
    return Math.min(ASSISTANT_WIDTH_MAX, Math.max(ASSISTANT_WIDTH_MIN, n));
  } catch {
    return ASSISTANT_WIDTH_DEFAULT;
  }
}
export const ASSISTANT_WIDTH_BOUNDS = {
  min: ASSISTANT_WIDTH_MIN,
  max: ASSISTANT_WIDTH_MAX,
  default: ASSISTANT_WIDTH_DEFAULT,
};

export const useUI = create<UIState>((set, get) => ({
  view: "tasks",
  activeProject: loadActiveProject(),
  logFilter: "",
  controlTarget: null,
  settingsOpen: false,
  assistantOpen: false,
  assistantSidebar: loadAssistantSidebar(),
  assistantWidth: loadAssistantWidth(),
  assistantFocusSeq: 0,
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
  openInControl: (target) => set({ controlTarget: target, view: "runners" }),
  consumeControlTarget: () => {
    const t = get().controlTarget;
    if (t) set({ controlTarget: null });
    return t;
  },
  setSettingsOpen: (open) => set({ settingsOpen: open }),
  setAssistantOpen: (open) => set({ assistantOpen: open }),
  setAssistantSidebar: (visible) => {
    try {
      localStorage.setItem(ASSISTANT_SIDEBAR_KEY, visible ? "1" : "0");
    } catch {
      /* ignore storage errors (private mode, quota) */
    }
    set({ assistantSidebar: visible });
  },
  setAssistantWidth: (px) => {
    const clamped = Math.min(ASSISTANT_WIDTH_MAX, Math.max(ASSISTANT_WIDTH_MIN, Math.round(px)));
    try {
      localStorage.setItem(ASSISTANT_WIDTH_KEY, String(clamped));
    } catch {
      /* ignore */
    }
    set({ assistantWidth: clamped });
  },
  focusAssistantPrompt: () => set((s) => ({ assistantFocusSeq: s.assistantFocusSeq + 1 })),
  setProjectSheetOpen: (open) => set({ projectSheetOpen: open }),
  openInspect: (t) => set({ inspect: t }),
  closeInspect: () => set({ inspect: null }),
  toggleWrap: () => set((s) => ({ wrap: !s.wrap })),
  updateApply: null,
  setUpdateApply: (fn) => set({ updateApply: fn }),
  toast: (message, kind = "info", options) => {
    const id = get()._tid + 1;
    set((s) => ({ _tid: id, toasts: [...s.toasts, { id, kind, message, action: options?.action }] }));
    // Toasts with an action stay around longer so the user has time to
    // notice and click them. Errors also linger by default.
    const defaultDuration = options?.action ? 8000 : kind === "error" ? 6000 : 3500;
    window.setTimeout(() => get().dismissToast(id), options?.durationMs ?? defaultDuration);
  },
  dismissToast: (id) =>
    set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
}));
