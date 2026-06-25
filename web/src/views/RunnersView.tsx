import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { useLive } from "../lib/sse";
import { ALL_PROJECTS, useUI } from "../store/ui";
import { useNav } from "../store/nav";
import { useViewKeyboard, handleListNavKey } from "../lib/keyboard";
import {
  controlAbortTask,
  controlKillInstance,
  controlListSessions,
  controlSpawnInstance,
  getRunnerStatus,
  getRunners,
  listInstances,
  pauseAll,
  pauseAutomations,
  resumeAll,
  resumeAutomations,
  shutdownRunner,
} from "../lib/api";
import { Modal, ConfirmDialog } from "../components/common/Modal";
import { EmptyState, ErrorState, Loading, Spinner } from "../components/common/states";
import { relativeTime } from "../lib/format";
import { sessionName } from "../lib/types";
import type { ControlTarget } from "../store/ui";
import type { OpencodeInstance, RunnerInfo } from "../lib/types";
import { Chat, type ChatHandle } from "./control/Chat";
import { HistoryPane } from "./control/HistoryPane";
import { latestInstanceSessionId, sortSessionsByExecutedTime } from "./control/sessionUtils";

type RunnerRow = { kind: "runner"; runner: RunnerInfo; instances: OpencodeInstance[] };
type InstanceRow = { kind: "instance"; runner: RunnerInfo; instance: OpencodeInstance };
type Row = RunnerRow | InstanceRow;

export function RunnersView() {
  const toast = useUI((s) => s.toast);
  const consumeControlTarget = useUI((s) => s.consumeControlTarget);
  const activeProject = useUI((s) => s.activeProject);
  const qc = useQueryClient();
  const liveRunners = useLive((s) => s.runners);
  const listRef = useRef<HTMLDivElement | null>(null);

  const runnersQ = useQuery({
    queryKey: ["runners"],
    queryFn: getRunners,
    refetchInterval: 10_000,
    staleTime: 10_000,
  });
  const statusQ = useQuery({
    queryKey: ["runner-status"],
    queryFn: getRunnerStatus,
    refetchInterval: 8_000,
    staleTime: 8_000,
  });
  const instancesQ = useQuery({
    queryKey: ["instances"],
    queryFn: listInstances,
    refetchInterval: 3_000,
  });

  const [spawnOpen, setSpawnOpen] = useState(false);
  const [confirmRunnerKill, setConfirmRunnerKill] = useState<RunnerInfo | null>(null);
  const [confirmInstanceKill, setConfirmInstanceKill] = useState<OpencodeInstance | null>(null);
  const [sessionTarget, setSessionTarget] = useState<OpencodeInstance | null>(null);
  const [historyTarget, setHistoryTarget] = useState<ControlTarget | null>(null);
  const [busy, setBusy] = useState(false);

  const runners = liveRunners.length ? liveRunners : runnersQ.data ?? [];
  const instances = instancesQ.data ?? [];

  // Aggregate active dispatch leases across every project the user is
  // currently subscribed to. A lease in state "pushed" means Brain sent a
  // dispatch command to the runner but the runner hasn't ack'd yet — so the
  // runner's SLOTS/TASKS counters still read zero even though work is queued
  // for it. Surface that as a separate chip so users aren't surprised by an
  // "idle" runner that's actually about to spin up. We include "acked" too
  // for the brief window between ack and the executor instance becoming
  // visible.
  const liveProjects = useLive((s) => s.projects);
  const pendingByRunner = useMemo(() => {
    const counts = new Map<string, number>();
    const now = Date.now();
    for (const project of Object.values(liveProjects)) {
      for (const task of project.tasks) {
        const lease = task.dispatch_lease;
        if (!lease) continue;
        if (lease.state !== "pushed" && lease.state !== "acked") continue;
        if (lease.expires_at && lease.expires_at < now) continue;
        counts.set(lease.assigned_runner_id, (counts.get(lease.assigned_runner_id) ?? 0) + 1);
      }
    }
    return counts;
  }, [liveProjects]);

  const rows = useMemo<Row[]>(() => {
    const out: Row[] = [];
    for (const runner of runners) {
      const runnerInstances = instances
        .filter((inst) => inst.runner_id === runner.runner_id)
        .sort((a, b) => (b.started_at ?? b.last_seen ?? 0) - (a.started_at ?? a.last_seen ?? 0));
      out.push({ kind: "runner", runner, instances: runnerInstances });
      for (const instance of runnerInstances) out.push({ kind: "instance", runner, instance });
    }
    return out;
  }, [runners, instances]);

  const rowIndexByKey = useMemo(() => {
    const indexes = new Map<string, number>();
    rows.forEach((row, i) => {
      indexes.set(row.kind === "runner" ? `runner:${row.runner.runner_id}` : `instance:${row.instance.instance_id}`, i);
    });
    return indexes;
  }, [rows]);

  const scope = "runners";
  const cursor = useNav((s) => Math.min(s.cursor[scope] ?? 0, Math.max(0, rows.length - 1)));
  const status = statusQ.data;
  const taskPaused = !!status?.paused;
  const automationPaused = !!status?.automationsPaused;
  const allProjectsSelected = activeProject === ALL_PROJECTS;
  const allPaused = taskPaused && automationPaused;
  const activeInstances = instances.filter((inst) => inst.kind === "task" || inst.status === "busy" || inst.status === "starting");
  const onlineRunners = runners.filter((runner) => runner.status === "online").length;

  useEffect(() => {
    const target = consumeControlTarget();
    if (!target) return;
    if (target.mode === "history") {
      setHistoryTarget(target);
      setSessionTarget(null);
      return;
    }
    const instance = instances.find((inst) => inst.instance_id === target.instanceId);
    if (instance) {
      setSessionTarget(instance);
      setHistoryTarget(null);
      return;
    }
    if (target.instanceId) toast("Instance is not online yet", "info");
  }, [consumeControlTarget, instances, toast]);

  useEffect(() => {
    listRef.current?.querySelector<HTMLElement>('[data-cursor="1"]')?.scrollIntoView({ block: "nearest" });
  }, [cursor]);

  function openRow(row: Row | undefined) {
    if (!row) return;
    if (row.kind === "instance") {
      setSessionTarget(row.instance);
      setHistoryTarget(null);
      return;
    }
    const first = row.instances[0];
    if (!first) {
      toast("Runner has no active tasks or instances", "info");
      return;
    }
    const idx = rowIndexByKey.get(`instance:${first.instance_id}`);
    if (idx !== undefined) useNav.getState().setCursor(scope, idx);
    setSessionTarget(first);
    setHistoryTarget(null);
  }

  useViewKeyboard(
    (e) => {
      if (handleListNavKey(e, scope, rows.length)) return true;
      const cur = rows[cursor];
      switch (e.key) {
        case "Enter":
        case "o":
          openRow(cur);
          return true;
        case "n":
        case "+":
          setSpawnOpen(true);
          return true;
        case "s":
          if (cur?.kind === "runner") setConfirmRunnerKill(cur.runner);
          return true;
        case "K":
          if (cur?.kind === "instance") setConfirmInstanceKill(cur.instance);
          return true;
        case "p":
        case "P":
          allProjectsSelected ? toggleAllPause() : toggleTaskPause();
          return true;
        case "a":
        case "A":
          toggleAutomationPause();
          return true;
        default:
          return false;
      }
    },
    [rows, cursor, taskPaused, automationPaused, allProjectsSelected, allPaused, rowIndexByKey],
  );

  function toggleTaskPause() {
    void (taskPaused ? act("Runner pool resumed", resumeAll) : act("Runner pool paused", pauseAll));
  }

  function toggleAutomationPause() {
    void (automationPaused
      ? act("Automations resumed", () => resumeAutomations())
      : act("Automations paused", () => pauseAutomations()));
  }

  function toggleAllPause() {
    void (allPaused
      ? act("All tasks and automations resumed", async () => {
          await resumeAll();
          await resumeAutomations();
        })
      : act("All tasks and automations paused", async () => {
          await pauseAll();
          await pauseAutomations();
        }));
  }

  async function act(label: string, fn: () => Promise<unknown>) {
    setBusy(true);
    try {
      await fn();
      toast(label, "success");
      void qc.invalidateQueries({ queryKey: ["runner-status"] });
      void qc.invalidateQueries({ queryKey: ["instances"] });
      void qc.invalidateQueries({ queryKey: ["runners"] });
    } catch (e) {
      toast(e instanceof Error ? e.message : "Action failed", "error");
    } finally {
      setBusy(false);
    }
  }

  async function killInstance(inst: OpencodeInstance) {
    if (inst.kind === "task") {
      if (!inst.project_id || !inst.task_id) {
        toast("Task instance is missing project or task id", "error");
        setConfirmInstanceKill(null);
        return;
      }
      await act("Task aborted; task reset to pending", () => controlAbortTask(inst.runner_id, inst.task_id!));
    } else {
      await act("Instance killed", () => controlKillInstance(inst.runner_id, inst.instance_id));
    }
    if (sessionTarget?.instance_id === inst.instance_id) setSessionTarget(null);
    setConfirmInstanceKill(null);
  }

  async function resumeHistory(runnerId: string) {
    const target = historyTarget;
    if (!target || !target.workdir) return;
    try {
      const res = await controlSpawnInstance(runnerId, { workdir: target.workdir, title: target.taskTitle });
      setHistoryTarget(null);
      setSessionTarget(res.instance);
      void qc.invalidateQueries({ queryKey: ["instances"] });
      toast("Session resumed", "success");
    } catch (e) {
      toast(e instanceof Error ? e.message : "Resume failed", "error");
    }
  }

  if (runnersQ.isLoading && !runners.length) return <Loading label="Loading runners…" />;
  if (runnersQ.error && !runners.length)
    return <ErrorState error={runnersQ.error} onRetry={() => void runnersQ.refetch()} />;

  return (
    <div className="runner-console">
      <div className="runner-toolbar">
        <div className="runner-summary">
          <span style={{ color: onlineRunners > 0 ? "var(--green)" : "var(--red)" }}>● {onlineRunners} online</span>
          <span>{runners.length} runners</span>
          <span>{activeInstances.length} executing</span>
          <span style={{ color: taskPaused ? "var(--red)" : "var(--green)" }}>tasks {taskPaused ? "paused" : "running"}</span>
          <span style={{ color: automationPaused ? "var(--red)" : "var(--green)" }}>autos {automationPaused ? "off" : "on"}</span>
        </div>
        <div className="runner-actions">
          {allProjectsSelected && (
            <button className="btn sm primary" disabled={busy} onClick={toggleAllPause} title="Shortcut: p">
              {allPaused ? "resume all" : "pause all"}
            </button>
          )}
          <button className="btn sm ghost" disabled={busy} onClick={toggleTaskPause} title="Shortcut: p">
            {taskPaused ? "resume tasks" : "pause tasks"}
          </button>
          <button className="btn sm ghost" disabled={busy} onClick={toggleAutomationPause} title="Shortcut: a">
            {automationPaused ? "resume autos" : "pause autos"}
          </button>
          <button className="btn sm" onClick={() => setSpawnOpen(true)} title="Shortcut: n">
            + instance
          </button>
        </div>
      </div>

      <div className="runner-list" ref={listRef}>
        {runners.length === 0 ? (
          <EmptyState glyph="⚙" title="No runners online" hint="Start one with: brain start <project>" />
        ) : (
          rows.map((row, i) => row.kind === "runner" ? (
            <RunnerCard
              key={`r:${row.runner.runner_id}`}
              runner={row.runner}
              instances={row.instances}
              pendingDispatches={pendingByRunner.get(row.runner.runner_id) ?? 0}
              cursored={i === cursor}
              onCursor={() => useNav.getState().setCursor(scope, i)}
              onKill={() => setConfirmRunnerKill(row.runner)}
            />
          ) : (
            <ExecutingTaskCard
              key={`i:${row.instance.instance_id}`}
              instance={row.instance}
              runner={row.runner}
              cursored={i === cursor}
              onOpen={() => {
                useNav.getState().setCursor(scope, i);
                setSessionTarget(row.instance);
                setHistoryTarget(null);
              }}
              onKill={() => setConfirmInstanceKill(row.instance)}
            />
          ))
        )}
      </div>

      {sessionTarget && (
        <SessionModal
          instance={sessionTarget}
          onClose={() => setSessionTarget(null)}
        />
      )}

      {historyTarget && (
        <Modal title={historyTarget.taskTitle || "Session history"} className="sheet-wide session-sheet" onClose={() => setHistoryTarget(null)}>
          <HistoryPane
            target={historyTarget}
            runners={runners}
            onBack={() => setHistoryTarget(null)}
            onResume={resumeHistory}
          />
        </Modal>
      )}

      {spawnOpen && (
        <SpawnModal
          runners={runners}
          onClose={() => setSpawnOpen(false)}
          onSpawned={(inst) => {
            setSpawnOpen(false);
            void qc.invalidateQueries({ queryKey: ["instances"] });
            setSessionTarget(inst);
            toast("Instance spawned", "success");
          }}
        />
      )}

      {confirmRunnerKill && (
        <ConfirmDialog
          title="Shut down runner?"
          danger
          confirmLabel="Shut down"
          busy={busy}
          message={
            <>
              Request graceful shutdown of <strong className="mono">{confirmRunnerKill.runner_id}</strong> on {confirmRunnerKill.hostname}.
            </>
          }
          onClose={() => setConfirmRunnerKill(null)}
          onConfirm={() =>
            void act("Shutdown requested", () => shutdownRunner(confirmRunnerKill.runner_id)).then(() => setConfirmRunnerKill(null))
          }
        />
      )}

      {confirmInstanceKill && (
        <ConfirmDialog
          title="Kill instance?"
          danger
          confirmLabel={confirmInstanceKill.kind === "task" ? "Abort task" : "Kill"}
          busy={busy}
          message={
            <>
              {confirmInstanceKill.kind === "task" ? "Abort task and reset to pending" : "Terminate ad-hoc instance"}{" "}
              <strong className="mono">{confirmInstanceKill.task_id || confirmInstanceKill.instance_id}</strong>
              {confirmInstanceKill.workdir ? <> in {confirmInstanceKill.workdir}</> : null}?
            </>
          }
          onClose={() => setConfirmInstanceKill(null)}
          onConfirm={() => void killInstance(confirmInstanceKill)}
        />
      )}
    </div>
  );
}

function RunnerCard({
  runner,
  instances,
  pendingDispatches,
  cursored,
  onCursor,
  onKill,
}: {
  runner: RunnerInfo;
  instances: OpencodeInstance[];
  pendingDispatches: number;
  cursored: boolean;
  onCursor: () => void;
  onKill: () => void;
}) {
  const color = runner.status === "online" ? "var(--green)" : runner.status === "stale" ? "var(--yellow)" : "var(--red)";
  return (
    <div className={`runner-card ${cursored ? "cursor" : ""}`} data-cursor={cursored ? "1" : undefined} onClick={onCursor}>
      <div className="runner-card-main">
        <span className="glyph" style={{ color }}>●</span>
        <div className="runner-title">
          <strong className="mono truncate">{runner.runner_id}</strong>
          <span className="faint truncate">{runner.hostname}</span>
        </div>
      </div>
      <div className="runner-chipline">
        <span className="ctl-meta-chip"><span className="ctl-meta-label">slots</span>{runner.active_tasks ?? 0}/{runner.max_parallel}</span>
        {runner.executors?.length ? <span className="ctl-meta-chip"><span className="ctl-meta-label">exec</span>{runner.executors.join(",")}</span> : null}
        <span className="ctl-meta-chip"><span className="ctl-meta-label">tasks</span>{instances.length}</span>
        {pendingDispatches > 0 && (
          <span
            className="ctl-meta-chip"
            style={{ color: "var(--yellow)" }}
            title={`${pendingDispatches} task(s) dispatched to this runner but not yet executing (lease pushed/acked, instance not yet visible)`}
          >
            <span className="ctl-meta-label">pending</span>{pendingDispatches}
          </span>
        )}
        <span className="ctl-meta-chip"><span className="ctl-meta-label">hb</span>{relativeTime(runner.last_heartbeat)}</span>
      </div>
      <button className="icon-btn runner-power" title="Shut down runner (s)" onClick={(e) => { e.stopPropagation(); onKill(); }}>⏻</button>
    </div>
  );
}

function ExecutingTaskCard({
  instance,
  runner,
  cursored,
  onOpen,
  onKill,
}: {
  instance: OpencodeInstance;
  runner: RunnerInfo;
  cursored: boolean;
  onOpen: () => void;
  onKill: () => void;
}) {
  const sessions = instance.session_ids?.length ?? 0;
  return (
    <div
      className={`runner-task-card ${cursored ? "cursor" : ""}`}
      data-cursor={cursored ? "1" : undefined}
      onClick={onOpen}
    >
      <div className="runner-task-head">
        <span style={{ color: instanceStatusColor(instance.status) }}>▣</span>
        <span className="ctl-kind" style={{ color: instance.kind === "adhoc" ? "var(--purple)" : "var(--blue)" }}>{instance.kind}</span>
        <strong className="truncate">{instance.title || instance.task_id || instance.instance_id}</strong>
        <span className="faint">{instance.status}</span>
        <button className="btn sm ghost" onClick={(e) => { e.stopPropagation(); onOpen(); }}>session</button>
        <button className="btn sm danger" onClick={(e) => { e.stopPropagation(); onKill(); }}>{instance.kind === "task" ? "abort" : "kill"}</button>
      </div>
      <div className="runner-chipline">
        <Chip label="project" value={instance.project_id} />
        <Chip label="feature" value={instance.feature_id} />
        <Chip label="task" value={instance.task_id} />
        <Chip label="runner" value={runner.runner_id} />
        <Chip label="agent" value={instance.agent} />
        <Chip label="model" value={compactModelName(instance.model)} title={instance.model} />
        <Chip label="exec" value={instance.executor} />
        <Chip label="workdir" value={compactPath(instance.workdir)} title={instance.workdir} />
        {sessions > 0 && <Chip label="sessions" value={sessions} />}
        {(instance.pending_permissions ?? 0) > 0 && <Chip label="perm" value={`${instance.pending_permissions} pending`} tone="danger" />}
      </div>
    </div>
  );
}

function SessionModal({ instance, onClose }: { instance: OpencodeInstance; onClose: () => void }) {
  const [sessionId, setSessionId] = useState<string | null>(null);
  const chatRef = useRef<ChatHandle | null>(null);
  const sessionsQ = useQuery({
    queryKey: ["control-sessions", instance.runner_id, instance.instance_id],
    queryFn: () => controlListSessions(instance.runner_id, instance.instance_id),
    refetchInterval: sessionId ? false : 3_000,
    retry: 1,
  });
  const sessions = useMemo(() => sortSessionsByExecutedTime(sessionsQ.data ?? []), [sessionsQ.data]);

  useEffect(() => {
    if (!sessionId) setSessionId(latestInstanceSessionId(instance, sessions));
  }, [instance, sessions, sessionId]);

  const selected = sessions.find((session) => session.id === sessionId);

  function moveSession(delta: 1 | -1) {
    if (sessions.length === 0) return;
    const current = Math.max(0, sessions.findIndex((session) => session.id === sessionId));
    const next = (current + delta + sessions.length) % sessions.length;
    setSessionId(sessions[next].id);
  }

  function handleModalKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    const target = e.target as HTMLElement | null;
    const tag = target?.tagName.toLowerCase();
    const isTextInput = tag === "input" || tag === "textarea" || target?.isContentEditable;
    if (isTextInput) return;
    if (e.key === "j" || e.key === "ArrowDown") {
      e.preventDefault();
      moveSession(1);
      return;
    }
    if (e.key === "k" || e.key === "ArrowUp") {
      e.preventDefault();
      moveSession(-1);
      return;
    }
    if (e.key === "i") {
      e.preventDefault();
      chatRef.current?.focusPrompt();
    }
  }

  return (
    <Modal
      title={instance.title || instance.task_id || instance.instance_id}
      className="sheet-wide session-sheet"
      onClose={onClose}
    >
      <div onKeyDown={handleModalKeyDown} tabIndex={-1}>
      <div className="session-modal-head">
        <span style={{ color: instanceStatusColor(instance.status) }}>▣ {instance.status}</span>
        <Chip label="project" value={instance.project_id} />
        <Chip label="feature" value={instance.feature_id} />
        <Chip label="task" value={instance.task_id} />
        <Chip label="agent" value={instance.agent} />
        <Chip label="model" value={compactModelName(instance.model)} title={instance.model} />
        <Chip label="workdir" value={compactPath(instance.workdir)} title={instance.workdir} />
      </div>
      {sessions.length > 1 && (
        <div className="session-picker">
          <span className="faint">sessions sorted by executed time</span>
          <select value={sessionId ?? ""} onChange={(e) => setSessionId(e.target.value || null)}>
            <option value="">select session…</option>
            {sessions.map((session) => (
              <option key={session.id} value={session.id}>
                {sessionName(session)} · {session.time?.updated ? new Date(session.time.updated).toLocaleString() : "unknown"}
              </option>
            ))}
          </select>
        </div>
      )}
      {sessionsQ.error && !sessions.length && (
        <div className="faint" style={{ padding: 12, color: "var(--red)" }}>
          Cannot reach instance: {String((sessionsQ.error as Error).message)}
        </div>
      )}
      {sessionId ? (
        <Chat
          ref={chatRef}
          runnerId={instance.runner_id}
          instanceId={instance.instance_id}
          sessionId={sessionId}
          defaultAgent={instance.agent}
          defaultModel={instance.model}
          sessionLabel={selected ? sessionName(selected) : undefined}
        />
      ) : (
        <EmptyState glyph="◌" title="No session selected" hint={sessionsQ.isLoading ? "Loading sessions…" : "No session is recorded for this instance yet."} />
      )}
      </div>
    </Modal>
  );
}

function Chip({ label, value, title, tone }: { label: string; value?: string | number | null; title?: string; tone?: "danger" }) {
  if (value === undefined || value === null || value === "") return null;
  return (
    <span className="ctl-meta-chip" title={title ?? `${label}: ${value}`} style={tone === "danger" ? { borderColor: "var(--red)", color: "var(--red)" } : undefined}>
      <span className="ctl-meta-label">{label}</span>
      <span className="truncate">{value}</span>
    </span>
  );
}

function instanceStatusColor(status: OpencodeInstance["status"]): string {
  switch (status) {
    case "busy":
      return "var(--yellow)";
    case "idle":
      return "var(--green)";
    case "starting":
      return "var(--blue)";
    default:
      return "var(--red)";
  }
}

function compactModelName(model?: string) {
  if (!model) return "";
  const name = model.split("/").pop() ?? model;
  return name.replace(/^claude-/, "");
}

function compactPath(path?: string) {
  if (!path) return "";
  const parts = path.split("/").filter(Boolean);
  if (parts.length <= 2) return path;
  return `…/${parts.slice(-2).join("/")}`;
}

function SpawnModal({
  runners,
  onClose,
  onSpawned,
}: {
  runners: RunnerInfo[];
  onClose: () => void;
  onSpawned: (inst: OpencodeInstance) => void;
}) {
  const online = runners.filter((r) => r.status === "online");
  const [runnerId, setRunnerId] = useState(online[0]?.runner_id ?? "");
  const [workdir, setWorkdir] = useState("");
  const [title, setTitle] = useState("");
  const [busy, setBusyState] = useState(false);
  const [error, setError] = useState("");

  async function spawn() {
    if (!runnerId || !workdir) return;
    setBusyState(true);
    setError("");
    try {
      const res = await controlSpawnInstance(runnerId, { workdir, title: title || undefined });
      onSpawned(res.instance);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Spawn failed");
      setBusyState(false);
    }
  }

  return (
    <Modal
      title="New OpenCode instance"
      onClose={onClose}
      footer={
        <>
          <button className="btn ghost" onClick={onClose} disabled={busy}>Cancel</button>
          <button className="btn" onClick={() => void spawn()} disabled={busy || !runnerId || !workdir}>{busy ? <Spinner /> : "Spawn"}</button>
        </>
      }
    >
      <div className="form-col" style={{ display: "flex", flexDirection: "column", gap: 10 }}>
        <label>
          <div className="faint" style={{ fontSize: 12 }}>Runner</div>
          <select value={runnerId} onChange={(e) => setRunnerId(e.target.value)} style={{ width: "100%" }}>
            {online.length === 0 && <option value="">no online runners</option>}
            {online.map((r) => <option key={r.runner_id} value={r.runner_id}>{r.runner_id} ({r.hostname})</option>)}
          </select>
        </label>
        <label>
          <div className="faint" style={{ fontSize: 12 }}>Working directory (absolute path on the runner)</div>
          <input type="text" value={workdir} placeholder="/home/user/projects/my-repo" onChange={(e) => setWorkdir(e.target.value)} style={{ width: "100%" }} />
        </label>
        <label>
          <div className="faint" style={{ fontSize: 12 }}>Title (optional)</div>
          <input type="text" value={title} placeholder="quick fix session" onChange={(e) => setTitle(e.target.value)} style={{ width: "100%" }} />
        </label>
        {error && <div style={{ color: "var(--red)", fontSize: 12.5 }}>{error}</div>}
      </div>
    </Modal>
  );
}
