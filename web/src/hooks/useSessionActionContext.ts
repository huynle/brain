/**
 * useSessionActionContext — binds the pure session/process action
 * builders to real effects (control-plane API calls, navigation,
 * clipboard, toasts).
 *
 * Instance rows do not ride SSE — they come from two poll-backed
 * queries (["v2","sessions"] at 8s, ["v2","runner-instances",id] at
 * 5s), so every mutating effect invalidates both; otherwise a killed
 * process would sit in the sidebar for most of a poll interval after
 * the user just killed it.
 *
 * Instance-scoped rather than project-scoped: an OpencodeInstance
 * already carries its own runner/project/task addressing, so unlike
 * the task/automation contexts there is no per-project factory.
 */
import { useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { useModal } from "../store/modal";
import { useWorkspace } from "../store/workspace";
import { useUI } from "../store/ui";
import {
  controlAbort,
  controlAbortTask,
  controlKillInstance,
} from "../lib/api";
import {
  sessionName,
  type SessionActionContext,
} from "../lib/actions/sessionActions";
import { instanceTranscriptRef } from "../lib/sessionRef";
import type { OpencodeInstance } from "../lib/types";

export function useSessionActionContext(): SessionActionContext {
  const openModal = useModal((s) => s.open);
  const closeModal = useModal((s) => s.close);
  const setFocusSession = useWorkspace((s) => s.setFocusSession);
  const openSessionRef = useWorkspace((s) => s.openSessionRef);
  const toast = useUI((s) => s.toast);
  const queryClient = useQueryClient();

  return useMemo(() => {
    const invalidate = () => {
      void queryClient.invalidateQueries({ queryKey: ["v2", "sessions"] });
      // Prefix-matches every runner's Processes query.
      void queryClient.invalidateQueries({
        queryKey: ["v2", "runner-instances"],
      });
    };

    return {
      openSession: (inst: OpencodeInstance) => {
        // The runner Processes rows live inside the runner modal —
        // leaving it open would stack it over the session view.
        closeModal();
        if (inst.status !== "exited") {
          // The instance-id fast path the sidebar rows already use.
          setFocusSession(inst.instance_id);
          return;
        }
        // Exited ⇒ the transcript outlives the process; address it as
        // history. The builder disables watch when no session was ever
        // discovered, so a missing ref here means a stale menu — noop.
        const ref = instanceTranscriptRef(inst);
        if (ref) openSessionRef(ref);
      },

      openProcesses: (inst: OpencodeInstance) =>
        openModal(
          "runner",
          { id: inst.runner_id, instanceId: inst.instance_id },
          "processes",
        ),

      openTask: (inst: OpencodeInstance) =>
        openModal("task", {
          projectId: inst.project_id,
          taskId: inst.task_id,
        }),

      abortSession: async (inst: OpencodeInstance, sessionId: string) => {
        await controlAbort(inst.runner_id, inst.instance_id, sessionId);
        invalidate();
        toast(`Abort sent to ${sessionName(inst)}`, "success");
      },

      abortTask: async (inst: OpencodeInstance) => {
        // The builder gates on task_id; this guard covers the stale-row
        // race and gives the runner-catch a real message either way.
        if (!inst.task_id) {
          throw new Error("Instance is not linked to a task");
        }
        await controlAbortTask(inst.runner_id, inst.task_id);
        invalidate();
        toast(
          `Abort signal sent to ${inst.runner_id} for ${inst.task_id}`,
          "success",
        );
      },

      killInstance: async (inst: OpencodeInstance) => {
        await controlKillInstance(inst.runner_id, inst.instance_id);
        invalidate();
        toast(`Killed ${sessionName(inst)}`, "success");
      },

      copyText: async (label: string, value: string) => {
        // clipboard is undefined off secure origins — surface that as
        // the action's error toast instead of a TypeError.
        if (!navigator.clipboard?.writeText) {
          throw new Error("Clipboard unavailable — requires a secure origin");
        }
        await navigator.clipboard.writeText(value);
        toast(`${label} copied`, "success");
      },
    };
  }, [
    openModal,
    closeModal,
    setFocusSession,
    openSessionRef,
    toast,
    queryClient,
  ]);
}
