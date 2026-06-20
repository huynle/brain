import { useUI } from "../store/ui";
import { getEntry, listInstances } from "../lib/api";
import type { SessionInfo } from "../lib/types";

// pickLatestSession returns the most recently recorded session pointer on a task
// entry (by timestamp), or null if none were recorded.
function pickLatestSession(
  sessions: Record<string, SessionInfo> | undefined,
): { sessionId: string; info: SessionInfo } | null {
  if (!sessions) return null;
  const entries = Object.entries(sessions);
  if (entries.length === 0) return null;
  entries.sort((a, b) => (b[1]?.timestamp ?? "").localeCompare(a[1]?.timestamp ?? ""));
  return { sessionId: entries[0][0], info: entries[0][1] };
}

// useOpenInControl returns a function that opens a task's OpenCode session in the
// Control tab — a live instance if one exists, otherwise the most recent recorded
// session (read-only review + resume). Surfaces a toast when nothing is available.
// Shared by the desktop Automations flow and the mobile inspect sheet.
export function useOpenInControl() {
  const openInControl = useUI((s) => s.openInControl);
  const toast = useUI((s) => s.toast);

  return async function open(target: { taskId?: string; path: string; title?: string }) {
    if (!target.taskId) {
      toast("No session for this entry", "info");
      return;
    }
    try {
      // Prefer a live instance for this task.
      const instances = await listInstances();
      const inst = instances.find((i) => i.task_id === target.taskId);
      if (inst) {
        openInControl({
          mode: "live",
          runnerId: inst.runner_id,
          instanceId: inst.instance_id,
          sessionId: inst.session_ids?.[0],
          taskTitle: target.title,
        });
        return;
      }
      // No live instance — fall back to the recorded session pointer. The list
      // endpoint may omit metadata, so fetch the full entry.
      const full = await getEntry(target.path);
      const latest = pickLatestSession(full.sessions);
      if (!latest) {
        toast("No session recorded for this task", "info");
        return;
      }
      openInControl({
        mode: "history",
        runnerId: latest.info.runner_id || "",
        sessionId: latest.sessionId,
        machineId: latest.info.machine_id,
        hostname: latest.info.hostname,
        workdir: latest.info.workdir,
        taskTitle: target.title,
      });
    } catch (e) {
      toast(e instanceof Error ? e.message : "Could not open session", "error");
    }
  };
}
