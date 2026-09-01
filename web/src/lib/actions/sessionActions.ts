/**
 * lib/actions/sessionActions — the verb matrix for a live session /
 * executor process (one `OpencodeInstance` registry row).
 *
 * Three surfaces render these rows — the sidebar's Live sessions, the
 * per-project session bubbles, and the runner Processes tab — and
 * before this module each offered a different (mostly empty) subset:
 * abort lived nowhere, kill only inside SessionFull's hand-rolled
 * two-step button. One builder, one verb set, all three surfaces.
 *
 * Abort vs kill are deliberately two verbs with different blast radii:
 *
 *   abort session   interrupts the CURRENT TURN of one session; the
 *                   instance stays up and can be prompted again.
 *   abort task      routes through the runner's task-abort endpoint,
 *                   which also unwinds the runner-side claim
 *                   bookkeeping for the linked task.
 *   kill instance   terminates the executor process itself. The
 *                   transcript survives in storage, but nothing can be
 *                   delivered to it afterwards.
 *
 * Pure: takes the instance plus effect callbacks, returns descriptors.
 * Instance rows are poll-backed (no SSE), so every mutating effect in
 * the context invalidates the registry queries afterwards (see
 * hooks/useSessionActionContext).
 */
import { chatCapability } from "../processes";
import { instanceSessionRef } from "../sessionRef";
import type { OpencodeInstance } from "../types";
import type { ActionDescriptor } from "./types";

/**
 * Effects a session action can perform. The component supplies real
 * implementations; tests supply recorders.
 */
export interface SessionActionContext {
  /** Focus the session view — live instance or recorded transcript. */
  openSession: (inst: OpencodeInstance) => void;
  /** Open the runner modal on Processes, preselecting this instance. */
  openProcesses: (inst: OpencodeInstance) => void;
  /** Open the linked task's modal. */
  openTask: (inst: OpencodeInstance) => void;
  /** POST session abort — interrupts the session's current turn. */
  abortSession: (inst: OpencodeInstance, sessionId: string) => Promise<void>;
  /** POST task abort on the owning runner. */
  abortTask: (inst: OpencodeInstance) => Promise<void>;
  /** DELETE the instance — the process dies, the transcript survives. */
  killInstance: (inst: OpencodeInstance) => Promise<void>;
  /** Copy a value to the clipboard, toasting what was copied. */
  copyText: (label: string, value: string) => Promise<void>;
}

/** Display name, mirroring the sidebar/Processes title precedence. */
export function sessionName(inst: OpencodeInstance): string {
  if (inst.title && inst.title.trim()) return inst.title;
  if (inst.kind === "adhoc") return `ad-hoc session ${inst.instance_id}`;
  return inst.task_id || inst.instance_id;
}

/**
 * The session id an abort addresses. Newest-discovered-wins, delegated
 * to lib/sessionRef so session selection keeps exactly one
 * implementation across every surface.
 */
export function latestSessionId(inst: OpencodeInstance): string | undefined {
  return instanceSessionRef(inst).session_id;
}

/** Why the session view cannot be opened, or "" when it can. */
export function watchSessionBlockedReason(inst: OpencodeInstance): string {
  // chatCapability already encodes "will this row ever have a
  // transcript" (pi/script never do; exited-without-session never
  // will) — reuse it rather than restating the executor rules.
  if (chatCapability(inst) !== "none") return "";
  if (inst.status === "exited") {
    return "Process exited before a session was discovered — there is no transcript to view";
  }
  return `Executor is ${inst.executor || "unknown"} — it has no session transcript; read its raw log in runner processes`;
}

/** Why the session cannot be aborted right now, or "" when it can. */
export function abortSessionBlockedReason(inst: OpencodeInstance): string {
  if (inst.status === "exited") {
    return "Process has exited — there is no live session to abort";
  }
  if (chatCapability(inst) === "none") {
    return `Executor is ${inst.executor || "unknown"} — it has no addressable session; abort the task instead`;
  }
  if (!latestSessionId(inst)) {
    return "No session discovered yet — the runner is still starting it";
  }
  return "";
}

/** Why the linked task cannot be aborted, or "" when it can. */
export function abortTaskBlockedReason(inst: OpencodeInstance): string {
  if (!inst.task_id) {
    return inst.kind === "adhoc"
      ? "Ad-hoc session — there is no task to abort"
      : "Not linked to a task";
  }
  if (inst.status === "exited") {
    return "Process has exited — nothing is executing the task";
  }
  return "";
}

/** Why the instance cannot be killed, or "" when it can. */
export function killInstanceBlockedReason(inst: OpencodeInstance): string {
  if (inst.status === "exited") return "Process has already exited";
  return "";
}

/** Why the linked task modal cannot be opened, or "" when it can. */
export function openTaskBlockedReason(inst: OpencodeInstance): string {
  if (!inst.task_id) {
    return inst.kind === "adhoc"
      ? "Ad-hoc session — no linked task"
      : "Not linked to a task";
  }
  if (!inst.project_id) {
    return "Task's project is unknown — the task modal cannot address it";
  }
  return "";
}

/**
 * Build the full action list for a session/process row. Every action
 * is always present; unavailable ones carry a `disabledReason`. See
 * ./types for why.
 */
export function buildSessionActions(
  inst: OpencodeInstance,
  ctx: SessionActionContext,
): ActionDescriptor[] {
  const actions: ActionDescriptor[] = [];
  const name = sessionName(inst);
  const sessionId = latestSessionId(inst);

  // ─── state ──────────────────────────────────────────────────────
  actions.push({
    id: "abort-session",
    label: "Abort session",
    group: "state",
    key: "a",
    danger: true,
    disabledReason: abortSessionBlockedReason(inst),
    // Interrupting a turn discards in-flight work, so it earns a beat
    // of thought — but it is recoverable (prompt again), so no
    // type-to-confirm.
    confirm: {
      title: "Abort this session?",
      body:
        `${name}'s current turn is interrupted mid-flight. The instance ` +
        "stays up — send it another prompt to keep working.",
      confirmLabel: "Abort session",
    },
    run: () => ctx.abortSession(inst, sessionId as string),
  });

  actions.push({
    id: "abort-task",
    label: "Abort task execution",
    group: "state",
    danger: true,
    disabledReason: abortTaskBlockedReason(inst),
    // Same copy contract as the task menu's abort: say what does NOT
    // happen (the status) so nobody expects abort to also cancel.
    confirm: {
      title: "Abort the task's execution?",
      body:
        `The runner is told to abort ${inst.task_id ?? "the linked task"}'s ` +
        "execution. The task keeps its current status — cancel it, resume " +
        "it, or re-run it afterwards.",
      confirmLabel: "Abort execution",
    },
    run: () => ctx.abortTask(inst),
  });

  // ─── edit ───────────────────────────────────────────────────────
  // Clipboard verbs ride the edit group: there is no dedicated
  // clipboard group, and inventing one would make every renderer
  // learn it for two rows.
  actions.push({
    id: "copy-pid",
    label: "Copy PID",
    group: "edit",
    disabledReason: inst.pid ? "" : "No PID reported for this process",
    run: () => ctx.copyText("PID", String(inst.pid)),
  });

  actions.push({
    id: "copy-workdir",
    label: "Copy workdir",
    group: "edit",
    disabledReason: inst.workdir ? "" : "No workdir reported for this process",
    run: () => ctx.copyText("Workdir", inst.workdir as string),
  });

  // ─── navigate ───────────────────────────────────────────────────
  actions.push({
    id: "watch",
    // Same two labels the task menu's session verb uses, chosen the same
    // way: live sessions stream and steer, exited ones are recordings.
    // Honest about process state, like instanceTranscriptRef.
    label:
      inst.status === "exited" ? "Open session transcript" : "Open live session",
    group: "navigate",
    key: "w",
    disabledReason: watchSessionBlockedReason(inst),
    run: async () => ctx.openSession(inst),
  });

  actions.push({
    id: "processes",
    label: "Open in runner processes",
    group: "navigate",
    key: "p",
    run: async () => ctx.openProcesses(inst),
  });

  actions.push({
    id: "open-task",
    label: "Open linked task",
    group: "navigate",
    key: "t",
    disabledReason: openTaskBlockedReason(inst),
    run: async () => ctx.openTask(inst),
  });

  // ─── danger ─────────────────────────────────────────────────────
  actions.push({
    id: "kill",
    label: "Kill instance",
    group: "danger",
    key: "k",
    danger: true,
    disabledReason: killInstanceBlockedReason(inst),
    // Not a data delete — the transcript survives — so confirm without
    // type-to-confirm; the body spells out the task-side consequence
    // instead, which is the part that actually surprises people.
    confirm: {
      title: `Kill ${name}?`,
      body:
        "The executor process is terminated. Its transcript survives in " +
        "storage, " +
        (inst.task_id
          ? "but the linked task is left in_progress until abandonment " +
            "detection notices — resume it from the task menu afterwards."
          : "but nothing can be delivered to the session afterwards."),
      confirmLabel: "Kill instance",
    },
    run: () => ctx.killInstance(inst),
  });

  return actions;
}
