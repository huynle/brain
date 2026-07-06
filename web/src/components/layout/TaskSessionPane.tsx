// Read-only rich session view for a highlighted task.
//
// Replaces the flat timestamped EntryLogsPane with the structured turns +
// collapsible tool cards used by the Control session modal. Two modes:
//
//   - live    — task has an OpenCode instance with a session. Subscribe to
//                the chatStore event stream and render incoming messages
//                as they arrive.
//   - history — task has a recorded session pointer (most recent wins).
//                Fetch the transcript via controlSessionHistory and render
//                read-only.
//
// In both modes we use the same MessageRow renderer so the output looks
// identical to the Control modal. No composer, no permissions UI, no
// agent/model selector — this pane is for auditing what already ran.
//
// Selection changes are cheap: the chatStore is reference-counted, so
// attaching/detaching is O(1) on already-live instances. History queries
// are cached by react-query keyed on (runnerId, sessionId).

import { useEffect, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  controlSessionHistory,
  getEntry,
  getRunners,
  listInstances,
} from "../../lib/api";
import { MessageRow } from "../../views/control/Chat";
import { resolveServingRunner } from "../../views/control/HistoryPane";
import { chatKey, useChat } from "../../views/control/chatStore";
import type {
  OcMessage,
  OpencodeInstance,
  RunnerInfo,
  SessionInfo,
  Task,
} from "../../lib/types";

// pickLatestSession returns the most recently recorded session pointer on a
// task entry. Matches the helper in useOpenInControl but kept local so this
// pane stays self-contained.
function pickLatestSession(
  sessions: Record<string, SessionInfo> | undefined,
): { sessionId: string; info: SessionInfo } | null {
  if (!sessions) return null;
  const entries = Object.entries(sessions);
  if (entries.length === 0) return null;
  entries.sort((a, b) => (b[1]?.timestamp ?? "").localeCompare(a[1]?.timestamp ?? ""));
  return { sessionId: entries[0][0], info: entries[0][1] };
}

// Resolved target the pane needs to render. Discriminated by `mode`.
type ResolvedTarget =
  | {
      mode: "live";
      runnerId: string;
      instanceId: string;
      sessionId: string;
      instance: OpencodeInstance;
    }
  | {
      mode: "history";
      runnerId?: string;
      machineId?: string;
      hostname?: string;
      workdir?: string;
      sessionId: string;
    }
  | { mode: "none"; reason: "no_task" | "no_session" };

// useResolveTaskTarget returns the resolved live/history pointer for a task.
// Runs two queries in parallel:
//   1) listInstances — find a running OpenCode session for this task
//   2) getEntry      — read recorded session pointers from the brain entry
//
// Live wins when both are available; history is the fallback for completed
// tasks.
function useResolveTaskTarget(
  taskId?: string,
  projectId?: string,
  taskPath?: string,
): { target: ResolvedTarget; loading: boolean; error?: string } {
  const enabled = !!taskId && !!projectId;
  const instancesQ = useQuery({
    queryKey: ["live-instances-for-task", projectId, taskId],
    queryFn: () => listInstances(),
    enabled,
    refetchInterval: 5_000,
  });
  const entryQ = useQuery({
    queryKey: ["task-entry-sessions", taskPath],
    queryFn: () => getEntry(taskPath as string),
    enabled: enabled && !!taskPath,
    // Sessions don't change often after completion; cache aggressively but
    // still refresh in case a new run appends a session pointer.
    staleTime: 30_000,
    refetchInterval: 30_000,
  });

  const target: ResolvedTarget = useMemo(() => {
    if (!taskId) return { mode: "none", reason: "no_task" };
    const inst = (instancesQ.data ?? []).find(
      (i) => i.task_id === taskId && (!projectId || i.project_id === projectId),
    );
    if (inst && inst.session_ids?.[0]) {
      return {
        mode: "live",
        runnerId: inst.runner_id,
        instanceId: inst.instance_id,
        sessionId: inst.session_ids[0],
        instance: inst,
      };
    }
    const sessions = (entryQ.data as Task | undefined)?.sessions;
    const latest = pickLatestSession(sessions);
    if (latest) {
      return {
        mode: "history",
        runnerId: latest.info.runner_id,
        machineId: latest.info.machine_id,
        hostname: latest.info.hostname,
        workdir: latest.info.workdir,
        sessionId: latest.sessionId,
      };
    }
    return { mode: "none", reason: "no_session" };
  }, [taskId, projectId, instancesQ.data, entryQ.data]);

  const loading = enabled && (instancesQ.isLoading || entryQ.isLoading);
  const errSrc = instancesQ.error || entryQ.error;
  const error = errSrc instanceof Error ? errSrc.message : undefined;
  return { target, loading, error };
}

// LiveSessionView renders messages from the chatStore for a (runner, instance,
// session) triple. Attaches and detaches automatically; the chatStore is
// ref-counted so this is safe to mount alongside the Control modal.
function LiveSessionView({
  runnerId,
  instanceId,
  sessionId,
}: {
  runnerId: string;
  instanceId: string;
  sessionId: string;
}) {
  const key = chatKey(runnerId, instanceId);
  const chat = useChat((s) => s.chats[key]);
  const attach = useChat((s) => s.attach);
  const hydrate = useChat((s) => s.hydrate);

  useEffect(() => {
    const detach = attach(runnerId, instanceId, sessionId);
    void hydrate(runnerId, instanceId, sessionId);
    return detach;
  }, [runnerId, instanceId, sessionId, attach, hydrate]);

  const messages = useMemo<OcMessage[]>(() => {
    if (!chat) return [];
    return chat.order
      .map((id) => chat.messages[id])
      .filter((m): m is OcMessage => !!m && m.info.sessionID === sessionId);
  }, [chat, sessionId]);

  if (messages.length === 0) {
    return (
      <span className="faint">
        {chat?.hydrated ? "No messages yet — session just started." : "Loading session…"}
      </span>
    );
  }
  return (
    <div className="ctl-chat-scroll">
      {messages.map((m) => (
        <MessageRow key={m.info.id} message={m} />
      ))}
    </div>
  );
}

// HistorySessionView fetches a session transcript once and renders it with
// the same MessageRow renderer. Picks any bridge-connected runner on the
// recorded machine so the on-disk session storage is reachable.
function HistorySessionView({
  runnerId,
  machineId,
  hostname,
  sessionId,
}: {
  runnerId?: string;
  machineId?: string;
  hostname?: string;
  sessionId: string;
}) {
  const runnersQ = useQuery({
    queryKey: ["runners-for-history"],
    queryFn: () => getRunners(),
    staleTime: 10_000,
    refetchInterval: 15_000,
  });
  // getRunners returns RunnerInfo[] directly (no wrapper).
  const runners = useMemo<RunnerInfo[]>(() => runnersQ.data ?? [], [runnersQ.data]);
  const serving = useMemo(
    () =>
      resolveServingRunner(runners, {
        mode: "history",
        runnerId: runnerId || "",
        sessionId,
        machineId,
      }),
    [runners, runnerId, machineId, sessionId],
  );
  const historyQ = useQuery({
    queryKey: ["task-pane-history", serving?.runner_id, sessionId],
    queryFn: () => controlSessionHistory(serving!.runner_id, sessionId),
    enabled: !!serving && !!sessionId,
    retry: 1,
    staleTime: 30_000,
  });

  if (!serving) {
    const host = hostname || "the machine that ran it";
    return (
      <div className="ctl-history-notice">
        <div style={{ fontSize: 24, marginBottom: 6 }}>⊘</div>
        <div style={{ fontWeight: 600, marginBottom: 4 }}>Session ran on {host}</div>
        <div className="faint" style={{ maxWidth: 380, lineHeight: 1.5, fontSize: 12.5 }}>
          That runner isn't connected, so its session storage can't be reached.
          Start a runner on <span className="mono">{host}</span> to review or resume.
        </div>
      </div>
    );
  }
  if (historyQ.isLoading) return <span className="faint">Loading session transcript…</span>;
  if (historyQ.error) {
    return (
      <span className="faint" style={{ color: "var(--red)" }}>
        {String((historyQ.error as Error).message)}
      </span>
    );
  }
  const messages = (historyQ.data ?? []).filter((m) => !!m?.info);
  if (messages.length === 0) {
    return <span className="faint">No messages recorded for this session.</span>;
  }
  return (
    <div className="ctl-chat-scroll">
      {messages.map((m) => (
        <MessageRow key={m.info.id} message={m} />
      ))}
    </div>
  );
}

export function TaskSessionPane({
  taskId,
  projectId,
  taskPath,
}: {
  taskId?: string;
  projectId?: string;
  taskPath?: string;
}) {
  const { target, loading, error } = useResolveTaskTarget(taskId, projectId, taskPath);

  if (!taskId || !projectId) {
    return <span className="faint">No task highlighted — session view appears here when a task is selected.</span>;
  }
  if (loading && target.mode === "none") return <span className="faint">Loading session…</span>;
  if (error && target.mode === "none") {
    return <span className="faint" style={{ color: "var(--red)" }}>{error}</span>;
  }
  if (target.mode === "none") {
    return <span className="faint">No session recorded for this task yet.</span>;
  }
  if (target.mode === "live") {
    return (
      <LiveSessionView
        runnerId={target.runnerId}
        instanceId={target.instanceId}
        sessionId={target.sessionId}
      />
    );
  }
  return (
    <HistorySessionView
      runnerId={target.runnerId}
      machineId={target.machineId}
      hostname={target.hostname}
      sessionId={target.sessionId}
    />
  );
}
