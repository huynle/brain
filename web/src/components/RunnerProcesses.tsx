/**
 * RunnerProcesses — the "Processes" tab body of the RunnerModal.
 *
 * Top half: every executor process the runner has reported to the
 * instance registry (opencode / pi / script task processes plus
 * ad-hoc control sessions), with the task each one is running.
 *
 * Bottom half: a two-mode detail pane for the selected process.
 *
 *   CHAT — the real session transcript, composed out of the pieces the
 *          Session views already use: liveSessionRef → useSessionTranscript
 *          → PermissionBanner + Transcript + Composer. The composer's
 *          prompt is delivered to the RUNNING agent (prompt_async), so a
 *          user can steer a process mid-flight and watch it react. This
 *          is the default wherever a session is addressable.
 *   RAW LOG — the historical task-log REST buffer merged with the live
 *          SSE tail. Automatic for pi/script processes (no session), and
 *          always reachable from chat by toggle, because debugging
 *          genuinely wants stdout sometimes.
 *
 * Both panes treat their payloads as terminal output rather than as
 * strings — carriage returns collapse to their final frame and SGR
 * escapes render as colour, instead of a literal "[0m" on one endless
 * unwrappable line. That is common/TerminalText, shared with the chat
 * transcript, whose tool output is the same pty capture.
 *
 * Data sources:
 *   • GET /runners/{id}/instances  (poll 5s — registry rows carry
 *     task_id, title, executor, agent, model, pid, port, started_at,
 *     session_ids)
 *   • GET /tasks/{project}/{task}/logs  (poll 5s for the selection)
 *   • useLive().logs — live runner_log SSE records (taskId-tagged)
 *   • control SSE / messages, via useSessionTranscript
 */
import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { getTaskLogs, listRunnerInstances } from "../lib/api";
import { useLive } from "../lib/sse";
import { useModal } from "../store/modal";
import { useSessionTranscript } from "../hooks/useSessionTranscript";
import { Transcript } from "./Session/Transcript";
import { Composer } from "./Session/Composer";
import { PermissionBanner } from "./Session/PermissionBanner";
import { Loading } from "./common/Loading";
import { ErrorState } from "./common/ErrorState";
import { TerminalText } from "./common/TerminalText";
import { clockTime } from "../lib/format";
import {
  chatCapability,
  formatUptime,
  hasTaskLog,
  instanceDot,
  isLogStreaming,
  logLevelClass,
  mergeTaskLogs,
  resolveProcessView,
  sortProcesses,
  type ProcessView,
} from "../lib/processes";
import { instanceTranscriptRef } from "../lib/sessionRef";
import type { LogLine, OpencodeInstance, Task } from "../lib/types";

const EMPTY_TASKS: readonly Task[] = Object.freeze([]);

/** How close to the bottom still counts as "following the tail" (px). */
const PIN_THRESHOLD = 48;

export interface RunnerProcessesProps {
  runnerId: string;
  /** Preselect this instance's detail pane (e.g. from a leaf drill-down). */
  initialInstanceId?: string;
}

function procTitle(inst: OpencodeInstance): string {
  if (inst.kind === "adhoc") {
    return inst.title || `ad-hoc session ${inst.instance_id}`;
  }
  return inst.title || inst.task_id || inst.instance_id;
}

function procMeta(inst: OpencodeInstance, now: number): string[] {
  const parts: string[] = [];
  parts.push(inst.status);
  if (inst.executor) parts.push(inst.executor);
  if (inst.agent) parts.push(inst.agent);
  if (inst.model) parts.push(inst.model.split("/").pop() ?? inst.model);
  if (inst.pid) parts.push(`pid ${inst.pid}`);
  if (inst.port) parts.push(`:${inst.port}`);
  const up = inst.started_at ? formatUptime(inst.started_at, now) : "";
  if (up) parts.push(`up ${up}`);
  return parts;
}

/** Chat / Raw-log switch, shown in both panes' headers. */
function ViewToggle({
  view,
  onView,
  chatEnabled,
}: {
  view: ProcessView;
  onView: (v: ProcessView) => void;
  chatEnabled: boolean;
}): JSX.Element {
  return (
    <div className="proc-view-toggle">
      <button
        type="button"
        className={view === "chat" ? "active" : ""}
        disabled={!chatEnabled}
        aria-pressed={view === "chat"}
        title={
          chatEnabled
            ? "Session transcript — read the conversation and steer the agent"
            : "This executor does not expose a session transcript"
        }
        onClick={() => onView("chat")}
      >
        Chat
      </button>
      <button
        type="button"
        className={view === "log" ? "active" : ""}
        aria-pressed={view === "log"}
        title="Raw process stdout"
        onClick={() => onView("log")}
      >
        Raw log
      </button>
    </div>
  );
}

/** Small live/polling/ended indicator, mirroring SessionFull's header. */
function DeliveryPill({
  delivery,
}: {
  delivery: "streaming" | "polling" | "ended" | "none";
}): JSX.Element | null {
  if (delivery === "none") return null;
  if (delivery === "streaming") {
    return (
      <span className="proc-chat-delivery live">
        <span className="live-dot" /> streaming
      </span>
    );
  }
  return (
    <span className="proc-chat-delivery">
      {delivery === "polling" ? "updating · 10s" : "session ended"}
    </span>
  );
}

/**
 * Chat pane — the session transcript for the selected process, plus a
 * composer that injects a prompt into the agent while it runs.
 */
function ProcessChat({
  inst,
  toggle,
}: {
  inst: OpencodeInstance;
  toggle: JSX.Element;
}): JSX.Element {
  // The registry row is a fresh object on every 5s poll, so memoise on
  // the identity fields rather than the row itself.
  const sessionKey = (inst.session_ids ?? []).join(",");
  const sref = useMemo(
    () => instanceTranscriptRef(inst),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [
      inst.runner_id,
      inst.instance_id,
      inst.status,
      inst.task_id,
      inst.project_id,
      inst.workdir,
      sessionKey,
    ],
  );

  const transcript = useSessionTranscript(sref);

  // The linked task, for the composer's check-in preset seed — same
  // source SessionFull uses.
  const projectTasks =
    useLive((s) =>
      inst.project_id ? s.projects[inst.project_id]?.tasks : undefined,
    ) ?? EMPTY_TASKS;
  const linkedTask = useMemo(
    () => (inst.task_id ? projectTasks.find((t) => t.id === inst.task_id) : undefined),
    [projectTasks, inst.task_id],
  );

  const live = sref?.mode === "live";
  const sessionId = sref?.session_id;
  const canSteer = live && !!sessionId && inst.status !== "exited";

  return (
    <div className="session-view proc-chat" style={{ gridTemplateColumns: "1fr" }}>
      <div className="hdr">
        <span className="proc-chat-mode">
          {live ? (inst.status === "busy" ? "working" : "live") : "transcript"}
        </span>
        {live && transcript.starting ? (
          <span className="proc-chat-delivery">session starting…</span>
        ) : (
          <DeliveryPill delivery={transcript.delivery} />
        )}
        <code className="proc-chat-sid" title={sessionId ?? undefined}>
          {sessionId ?? "discovering…"}
        </code>
        <span className="spacer" style={{ flex: 1 }} />
        {toggle}
      </div>

      <div className="stream proc-chat-stream">
        {live && sref && (
          <PermissionBanner
            runnerId={sref.runner_id}
            instanceId={sref.instance_id}
            sessionId={sessionId}
          />
        )}
        {transcript.error ? (
          <div className="proc-log-empty">
            Transcript unavailable — the runner hosting this session may be
            offline.
            <div style={{ marginTop: 4 }}>
              {String((transcript.error as Error)?.message ?? transcript.error)}
            </div>
          </div>
        ) : (
          <Transcript
            style={{ flex: 1, overflowY: "auto", padding: "8px 10px" }}
            messages={transcript.messages}
            emptyText={
              transcript.starting
                ? "Session starting — the runner is still discovering it."
                : transcript.isLoading
                  ? "Loading transcript…"
                  : "No messages yet."
            }
          />
        )}
      </div>

      {canSteer && sref ? (
        <Composer
          target={{
            runner_id: sref.runner_id,
            instance_id: sref.mode === "live" ? sref.instance_id : "",
            session_id: sessionId as string,
          }}
          checkinSeed={
            linkedTask
              ? { title: linkedTask.title || linkedTask.id, request: linkedTask.content }
              : inst.title
                ? { title: inst.title }
                : undefined
          }
        />
      ) : (
        <div className="composer proc-chat-note">
          {inst.status === "exited"
            ? "This process has exited — the transcript is read-only."
            : "Waiting for the session id before prompts can be delivered."}
        </div>
      )}
    </div>
  );
}

/** Raw stdout pane for one selected process. */
function ProcessRawLog({
  inst,
  toggle,
}: {
  inst: OpencodeInstance;
  toggle: JSX.Element;
}): JSX.Element {
  const projectId = inst.project_id ?? "";
  const taskId = inst.task_id ?? "";
  const streams = hasTaskLog(inst);

  const q = useQuery({
    queryKey: ["v2", "task-logs", projectId, taskId],
    queryFn: () => getTaskLogs(projectId, taskId, { limit: 300 }),
    refetchInterval: 5_000,
    staleTime: 4_000,
    enabled: streams,
  });

  const liveRecords = useLive((s) => s.logs);
  const liveLines = useMemo<LogLine[]>(
    () =>
      liveRecords
        .filter((r) => r.taskId === taskId && r.projectId === projectId)
        .map((r) => r.line),
    [liveRecords, taskId, projectId],
  );

  const lines = useMemo(
    () => mergeTaskLogs(q.data?.lines ?? [], liveLines).slice(-400),
    [q.data, liveLines],
  );

  /*
   * Follow the tail, but let the user read history.
   *
   * The old effect keyed on `lines.length`, which is capped at 400 by the
   * slice above — so it stopped firing exactly when output was heaviest,
   * and it force-scrolled with no detach check, yanking the viewport back
   * down the moment anyone scrolled up. Same pin-with-detach rule the
   * session Transcript uses: pinned while at (or near) the bottom,
   * scrolling away detaches, scrolling back re-pins.
   */
  const bodyRef = useRef<HTMLDivElement | null>(null);
  const pinnedRef = useRef(true);

  const onScroll = () => {
    const el = bodyRef.current;
    if (!el) return;
    pinnedRef.current =
      el.scrollHeight - el.scrollTop - el.clientHeight < PIN_THRESHOLD;
  };

  // A new selection starts pinned again.
  useEffect(() => {
    pinnedRef.current = true;
  }, [inst.instance_id]);

  useEffect(() => {
    const el = bodyRef.current;
    if (el && pinnedRef.current) el.scrollTop = el.scrollHeight;
  }, [lines]);

  const sessions = inst.session_ids ?? [];
  // The pulsing dot means "output is still arriving", so it must follow
  // liveness, not the mere existence of a buffer: an exited process's
  // stdout is frozen, and an animated "live" marker over it is a lie.
  const streaming = isLogStreaming(inst);

  return (
    <div className="log-mini proc-log">
      <div className="head">
        {streaming && <span className="live-dot" />}
        <span className="title">
          {streams ? `${taskId} · ${projectId}` : procTitle(inst)} · stdout
          {streams && !streaming && " · ended"}
        </span>
        {toggle}
      </div>
      <div className="body" ref={bodyRef} onScroll={onScroll}>
        {!streams && (
          <div className="proc-log-empty">
            {inst.kind === "adhoc"
              ? "Ad-hoc control session — it has no task-log stream."
              : "This process is not linked to a task, so there is no log stream."}
            {sessions.length > 0 && (
              <div style={{ marginTop: 4 }}>
                Its conversation is in the Chat view
                {sessions.length > 1 ? ` (${sessions.length} sessions).` : "."}
              </div>
            )}
          </div>
        )}
        {streams && q.error != null && (
          <div className="proc-log-empty">
            Log stream unavailable — {String((q.error as Error)?.message ?? q.error)}
          </div>
        )}
        {streams && q.isLoading && lines.length === 0 && (
          <div className="proc-log-empty">Loading logs…</div>
        )}
        {streams && !q.isLoading && q.error == null && lines.length === 0 && (
          <div className="proc-log-empty">No log lines for this process yet.</div>
        )}
        {lines.map((l, i) => {
          const cls = logLevelClass(l);
          return (
            <div key={`${l.timestamp}-${i}`} className={`l ${cls}`}>
              <span className="ts">{clockTime(l.timestamp)}</span>
              <span className="lvl">{l.level || "INFO"}</span>
              <span className="msg">
                <TerminalText text={l.content} />
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

export function RunnerProcesses({
  runnerId,
  initialInstanceId,
}: RunnerProcessesProps): JSX.Element {
  const openModal = useModal((s) => s.open);
  const q = useQuery({
    queryKey: ["v2", "runner-instances", runnerId],
    queryFn: () => listRunnerInstances(runnerId),
    refetchInterval: 5_000,
    staleTime: 4_000,
  });

  const processes = useMemo(() => sortProcesses(q.data ?? []), [q.data]);

  const [selected, setSelected] = useState<string | null>(
    initialInstanceId ?? null,
  );
  /*
   * Explicit Chat/Raw-log choices, keyed by instance id. Held here (not
   * inside the pane) so a 5s registry poll — which replaces every row
   * object — cannot reset the user's choice, and so switching away from a
   * process and back remembers how they were reading it.
   */
  const [viewOverrides, setViewOverrides] = useState<
    Record<string, ProcessView>
  >({});

  // Default-select the first (most active) process once data lands,
  // and recover when the selected process disappears from the registry.
  useEffect(() => {
    if (processes.length === 0) return;
    if (selected && processes.some((p) => p.instance_id === selected)) return;
    setSelected(processes[0].instance_id);
  }, [processes, selected]);

  if (q.isLoading) return <Loading size="sm" label="Loading processes…" />;
  if (q.error) {
    return <ErrorState error={q.error} onRetry={() => void q.refetch()} />;
  }

  if (processes.length === 0) {
    return (
      <div className="proc-log-empty">
        No processes reported by this runner. Task processes appear here
        while the runner is executing work.
      </div>
    );
  }

  const now = Date.now();
  const sel = processes.find((p) => p.instance_id === selected) ?? null;
  const view = sel ? resolveProcessView(sel, viewOverrides[sel.instance_id]) : "log";
  const chatEnabled = sel ? chatCapability(sel) !== "none" : false;

  const toggle = sel ? (
    <ViewToggle
      view={view}
      chatEnabled={chatEnabled}
      onView={(v) =>
        setViewOverrides((prev) => ({ ...prev, [sel.instance_id]: v }))
      }
    />
  ) : null;

  return (
    <div className="proc-split">
      <div className="proc-list">
        {processes.map((p) => (
          <div
            key={p.instance_id}
            className={`proc-row${p.instance_id === selected ? " sel" : ""}`}
            onClick={() => setSelected(p.instance_id)}
          >
            <span className={`dot ${instanceDot(p.status)}`} />
            <div className="proc-body">
              <div className="proc-title">
                {procTitle(p)}
                {p.kind === "task" && p.task_id && (
                  <span
                    className="chip mini"
                    title="Open task"
                    onClick={(e) => {
                      e.stopPropagation();
                      openModal("task", {
                        projectId: p.project_id,
                        taskId: p.task_id,
                      });
                    }}
                  >
                    {p.task_id}
                  </span>
                )}
                {p.kind === "adhoc" && <span className="chip mini">ad-hoc</span>}
                {chatCapability(p) !== "none" && (
                  <span className="chip mini" title="Has a session transcript">
                    chat
                  </span>
                )}
              </div>
              <div className="proc-meta">
                {procMeta(p, now).map((m, i) => (
                  <span key={i}>{m}</span>
                ))}
              </div>
              {p.project_id && (
                <div className="proc-meta">
                  <span>{p.project_id}</span>
                  {p.feature_id && <span>{p.feature_id}</span>}
                  {p.workdir && (
                    <span title={p.workdir}>
                      {p.workdir.length > 44
                        ? "…" + p.workdir.slice(-43)
                        : p.workdir}
                    </span>
                  )}
                </div>
              )}
            </div>
          </div>
        ))}
      </div>
      {sel && toggle && (
        <div className="proc-detail" key={sel.instance_id}>
          {view === "chat" ? (
            <ProcessChat inst={sel} toggle={toggle} />
          ) : (
            <ProcessRawLog inst={sel} toggle={toggle} />
          )}
        </div>
      )}
    </div>
  );
}
