import { create } from "zustand";
import { ForceDeclinedError } from "../lib/actions/forceRetry";

export interface BackgroundOperation {
  id: number;
  label: string;
  state: "running" | "finished" | "error" | "stopped";
  detail: string;
  issue?: boolean;
  completed?: number;
  total?: number;
}
interface State {
  operations: BackgroundOperation[];
  dismiss: (id: number) => void;
}
export const useBackgroundOperations = create<State>((set) => ({
  operations: [],
  dismiss: (id) => set((s) => ({ operations: s.operations.filter((o) => o.id !== id || o.state === "running") })),
}));
export type ReportProgress = (patch: Partial<Pick<BackgroundOperation, "detail" | "completed" | "total">>) => void;
let nextId = 0;

/** A single mutation at a time avoids overlapping deletes/status changes and force dialogs. */
export function startBackgroundOperation(label: string, run: (report: ReportProgress) => Promise<void>): Promise<void> {
  if (useBackgroundOperations.getState().operations.some((o) => o.state === "running")) {
    throw new Error("Another background operation is running. Wait for it to finish.");
  }
  const id = ++nextId;
  const patch = (update: Partial<BackgroundOperation>) => useBackgroundOperations.setState((s) => ({
    operations: s.operations.map((o) => o.id === id ? { ...o, ...update } : o),
  }));
  useBackgroundOperations.setState((s) => ({ operations: [...s.operations,
    { id, label, state: "running", detail: "Working…" },
  ] }));
  // Start on the next microtask so callers can close their confirmation first.
  return Promise.resolve().then(() => run(patch)).then(() => {
    patch({ state: "finished" });
  }, (err: unknown) => {
    patch({ state: err instanceof ForceDeclinedError ? "stopped" : "error",
      detail: `${err instanceof Error ? err.message : String(err)}. Earlier changes may already have been applied.` });
  });
}

/** Effects reached through the serial action runner can publish their server-reported result. */
export function reportBackgroundResult(detail: string, issue = false, completed?: number, total?: number) {
  useBackgroundOperations.setState((s) => ({ operations: s.operations.map((o) =>
    o.state === "running" ? { ...o, detail, issue, completed, total } : o),
  }));
}
