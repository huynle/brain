/**
 * RunnerProcesses — the "Processes" tab body of the RunnerModal.
 *
 * Top half: every executor process the runner has reported to the
 * instance registry (opencode / pi / script task processes plus
 * ad-hoc control sessions), with the task each one is running.
 * Bottom half: the log stream for the selected process — historical
 * lines from the task-log REST buffer merged with the live SSE tail.
 *
 * Data sources:
 *   • GET /runners/{id}/instances  (poll 5s — registry rows carry
 *     task_id, title, executor, agent, model, pid, port, started_at)
 *   • GET /tasks/{project}/{task}/logs  (poll 5s for the selection)
 *   • useLive().logs — live runner_log SSE records (taskId-tagged)
 */
import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { getTaskLogs, listRunnerInstances } from "../lib/api";
import { useLive } from "../lib/sse";
import { useModal } from "../store/modal";
import { Loading } from "./common/Loading";
import { ErrorState } from "./common/ErrorState";
import {
  formatUptime,
  instanceDot,
  logLevelClass,
  mergeTaskLogs,
  sortProcesses,
} from "../lib/processes";
import type { LogLine, OpencodeInstance } from "../lib/types";

export interface RunnerProcessesProps {
  runnerId: string;
  /** Preselect this instance's log panel (e.g. from a leaf drill-down). */
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

/** Log panel for one selected process. */
function ProcessLog({ inst }: { inst: OpencodeInstance }): JSX.Element {
  const projectId = inst.project_id ?? "";
  const taskId = inst.task_id ?? "";
  const hasTaskLog = inst.kind === "task" && projectId !== "" && taskId !== "";

  const q = useQuery({
    queryKey: ["v2", "task-logs", projectId, taskId],
    queryFn: () => getTaskLogs(projectId, taskId, { limit: 300 }),
    refetchInterval: 5_000,
    staleTime: 4_000,
    enabled: hasTaskLog,
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

  // Pin the viewport to the newest line as the stream grows.
  const bodyRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    const el = bodyRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [lines.length, inst.instance_id]);

  if (!hasTaskLog) {
    return (
      <div className="proc-log-empty">
        Ad-hoc session — no task log stream.
        {inst.session_ids && inst.session_ids.length > 0 && (
          <>
            {" "}
            Sessions:{" "}
            {inst.session_ids.map((s) => (
              <code key={s} style={{ marginRight: 4 }}>
                {s}
              </code>
            ))}
          </>
        )}
      </div>
    );
  }

  return (
    <div className="log-mini proc-log">
      <div className="head">
        <span className="live-dot" />
        <span className="title">
          {taskId} · {projectId}
        </span>
      </div>
      <div className="body" ref={bodyRef}>
        {q.isLoading && lines.length === 0 && (
          <div className="proc-log-empty">Loading logs…</div>
        )}
        {!q.isLoading && lines.length === 0 && (
          <div className="proc-log-empty">
            No log lines for this process yet.
          </div>
        )}
        {lines.map((l, i) => {
          const cls = logLevelClass(l);
          return (
            <div key={`${l.timestamp}-${i}`} className={`l ${cls}`}>
              <span className="ts">
                {l.timestamp
                  ? new Date(l.timestamp).toLocaleTimeString()
                  : ""}
              </span>
              <span className="lvl">{l.level || "INFO"}</span>
              <span className="msg">{l.content}</span>
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
                {p.kind === "adhoc" && (
                  <span className="chip mini">ad-hoc</span>
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
      {sel && <ProcessLog inst={sel} />}
    </div>
  );
}
