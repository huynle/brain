import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { useLive } from "../lib/sse";
import { ALL_PROJECTS, useUI } from "../store/ui";
import { useNav } from "../store/nav";
import { listNavHandlers } from "../lib/keymap/listNav";
import { useActions } from "../lib/keymap/useActions";
import { RUNNERS_SPECS } from "./control/keymap";
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
import type { OpencodeInstance, RunnerInfo, Task } from "../lib/types";
import { Chat, type ChatHandle } from "./control/Chat";
import { HistoryPane } from "./control/HistoryPane";
import { latestInstanceSessionId, sortSessionsByExecutedTime } from "./control/sessionUtils";

type RunnerRow = { kind: "runner"; runner: RunnerInfo; instances: OpencodeInstance[] };
type InstanceRow = { kind: "instance"; runner: RunnerInfo; instance: OpencodeInstance };
type Row = RunnerRow | InstanceRow;

/** Pending dispatch info: a task whose lease has been pushed/acked to a runner
 * but the runner hasn't reported the executor instance yet. Surfaced via the
 * PENDING chip + modal so users can see *which* tasks are queued and why.
 *
 * `healthy = true` means the last placement decision agrees with the lease
 * (accepted / no reason recorded yet — genuine in-flight dispatch). `healthy
 * = false` means the placement decision contradicts the lease (no_candidate,
 * rejected) — the lease row is stale and the server-side cleanup hasn't
 * caught up yet. We surface both, but only healthy ones count toward the
 * "pending" total that fronts the runner card.
 */
type PendingDispatch = {
  task: Task;
  projectId: string;
  leaseState: string;
  pushedAt: number;
  ackedAt?: number;
  expiresAt: number;
  lastError?: string;
  healthy: boolean;
};

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
  const [pendingTarget, setPendingTarget] = useState<{ runner: RunnerInfo; pending: PendingDispatch[] } | null>(null);
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
    const byRunner = new Map<string, PendingDispatch[]>();
    const now = Date.now();
    for (const project of Object.values(liveProjects)) {
      for (const task of project.tasks) {
        const lease = task.dispatch_lease;
        if (!lease) continue;
        if (lease.state !== "pushed" && lease.state !== "acked") continue;
        if (lease.expires_at && lease.expires_at < now) continue;
        // A lease is "healthy" (a real in-flight dispatch) only when the most
        // recent placement decision agrees with it. If the scheduler later
        // decided the task has no candidate, or the runner rejected it, the
        // lease row is stale — server cleanup should be dropping it, but until
        // it does we surface it under a "stuck" bucket instead of counting it
        // as pending against the runner.
        const decision = task.last_placement_reason?.decision;
        const healthy = !decision || decision === "accepted";
        const list = byRunner.get(lease.assigned_runner_id) ?? [];
        list.push({
          task,
          projectId: lease.project_id,
          leaseState: lease.state,
          pushedAt: lease.pushed_at,
          ackedAt: lease.acked_at,
          expiresAt: lease.expires_at,
          lastError: lease.last_error,
          healthy,
        });
        byRunner.set(lease.assigned_runner_id, list);
      }
    }
    // Stable ordering: healthy first, then by oldest pushed. Users care more
    // about active dispatches than about stale rows.
    for (const list of byRunner.values()) {
      list.sort((a, b) => {
        if (a.healthy !== b.healthy) return a.healthy ? -1 : 1;
        return a.pushedAt - b.pushedAt;
      });
    }
    return byRunner;
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

  // Keep the pending-dispatch modal in sync with the live lease map. If every
  // pending lease for the target runner resolves (instance shows up, lease
  // expires, or it gets rejected), close the modal so the user isn't staring
  // at stale data.
  useEffect(() => {
    if (!pendingTarget) return;
    const fresh = pendingByRunner.get(pendingTarget.runner.runner_id) ?? [];
    if (fresh.length === 0) {
      setPendingTarget(null);
      return;
    }
    // Update if the contents actually changed (by task id + state + pushed_at).
    const key = (list: PendingDispatch[]) => list.map((p) => `${p.task.id}:${p.leaseState}:${p.pushedAt}`).join("|");
    if (key(fresh) !== key(pendingTarget.pending)) {
      setPendingTarget({ runner: pendingTarget.runner, pending: fresh });
    }
  }, [pendingByRunner, pendingTarget]);

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

  useActions(
    "view:runners",
    "view",
    RUNNERS_SPECS,
    {
      ...listNavHandlers("runners", { scope: () => scope, count: () => rows.length }),
      "runners.open": () => openRow(rows[cursor]),
      "runners.spawn": () => setSpawnOpen(true),
      "runners.shutdown": () => {
        const cur = rows[cursor];
        if (cur?.kind === "runner") setConfirmRunnerKill(cur.runner);
      },
      "runners.killInstance": () => {
        const cur = rows[cursor];
        if (cur?.kind === "instance") setConfirmInstanceKill(cur.instance);
      },
      "runners.pause": () => (allProjectsSelected ? toggleAllPause() : toggleTaskPause()),
      "runners.pauseAutos": () => toggleAutomationPause(),
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
              pendingDispatches={pendingByRunner.get(row.runner.runner_id) ?? []}
              cursored={i === cursor}
              onCursor={() => useNav.getState().setCursor(scope, i)}
              onKill={() => setConfirmRunnerKill(row.runner)}
              onShowPending={(pending) => setPendingTarget({ runner: row.runner, pending })}
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

      {pendingTarget && (
        <PendingDispatchModal
          runner={pendingTarget.runner}
          pending={pendingTarget.pending}
          onClose={() => setPendingTarget(null)}
        />
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
  onShowPending,
}: {
  runner: RunnerInfo;
  instances: OpencodeInstance[];
  pendingDispatches: PendingDispatch[];
  cursored: boolean;
  onCursor: () => void;
  onKill: () => void;
  onShowPending: (pending: PendingDispatch[]) => void;
}) {
  const color = runner.status === "online" ? "var(--green)" : runner.status === "stale" ? "var(--yellow)" : "var(--red)";
  const healthyPending = pendingDispatches.filter((p) => p.healthy);
  const stuckPending = pendingDispatches.filter((p) => !p.healthy);
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
        {instances.length > 0 ? (
          <span
            className="ctl-meta-chip"
            style={{ color: "var(--green)" }}
            title={`${instances.length} task(s) currently executing on this runner — expand to see details below`}
          >
            <span className="ctl-meta-label">running</span>{instances.length}
          </span>
        ) : (
          <span className="ctl-meta-chip"><span className="ctl-meta-label">tasks</span>0</span>
        )}
        {healthyPending.length > 0 && (
          <button
            type="button"
            className="ctl-meta-chip"
            style={{ color: "var(--yellow)", cursor: "pointer", border: "none", background: "transparent", padding: 0, font: "inherit" }}
            title={`Click to see the ${healthyPending.length} task(s) dispatched but not yet executing on this runner`}
            onClick={(e) => { e.stopPropagation(); onShowPending(pendingDispatches); }}
          >
            <span className="ctl-meta-label">pending</span>{healthyPending.length}
          </button>
        )}
        {stuckPending.length > 0 && (
          <button
            type="button"
            className="ctl-meta-chip"
            style={{ color: "var(--red)", cursor: "pointer", border: "none", background: "transparent", padding: 0, font: "inherit" }}
            title={`Click to see ${stuckPending.length} stale lease(s) against this runner. The scheduler no longer considers this runner a candidate for these tasks, but the lease row hasn't been cleaned up.`}
            onClick={(e) => { e.stopPropagation(); onShowPending(pendingDispatches); }}
          >
            <span className="ctl-meta-label">stuck</span>{stuckPending.length}
          </button>
        )}
        <span className="ctl-meta-chip"><span className="ctl-meta-label">hb</span>{relativeTime(runner.last_heartbeat)}</span>
      </div>
      <button className="icon-btn runner-power" title="Shut down runner (s)" onClick={(e) => { e.stopPropagation(); onKill(); }}>⏻</button>
    </div>
  );
}

/** Pending dispatch transparency: shows each task whose lease has been pushed
 * to this runner but hasn't started executing yet. Renders task id, project,
 * lease state (pushed/acked), how long it's been waiting, time until expiry,
 * the most recent placement decision, and any reported error. */
function PendingDispatchModal({
  runner,
  pending,
  onClose,
}: {
  runner: RunnerInfo;
  pending: PendingDispatch[];
  onClose: () => void;
}) {
  const now = Date.now();
  const healthy = pending.filter((p) => p.healthy);
  const stale = pending.filter((p) => !p.healthy);
  return (
    <Modal title={`Pending dispatches — ${runner.runner_id}`} onClose={onClose} className="sheet-wide">
      <div style={{ padding: "0.5rem 0 1rem", color: "var(--text-dim)", fontSize: "0.9em" }}>
        Tasks with a live dispatch lease against <span className="mono">{runner.runner_id}</span> ({runner.hostname}).
        A lease is <em>pushed</em> when Brain hands the task off and <em>acked</em> once the runner confirms receipt.
        The runner's SLOTS / TASKS counters won't reflect them until the executor instance comes online.
        {stale.length > 0 && (
          <span>
            {" "}
            <strong style={{ color: "var(--red)" }}>{stale.length} row(s) are stale</strong>
            {" "}— the scheduler's most recent placement decision contradicts the lease
            (typically <span className="mono">no_candidate</span> or <span className="mono">rejected</span>).
            These should clear on the next scheduler pass. If they persist, the runner's project subscription
            may not match the task's project.
          </span>
        )}
      </div>
      {healthy.length > 0 && <PendingTable rows={healthy} label="In-flight" tone="healthy" now={now} />}
      {stale.length > 0 && <PendingTable rows={stale} label="Stale — placement contradicts lease" tone="stale" now={now} />}
    </Modal>
  );
}

function PendingTable({
  rows,
  label,
  tone,
  now,
}: {
  rows: PendingDispatch[];
  label: string;
  tone: "healthy" | "stale";
  now: number;
}) {
  return (
    <div style={{ marginBottom: "1rem" }}>
      <div style={{
        fontSize: "0.8em",
        textTransform: "uppercase",
        letterSpacing: "0.05em",
        color: tone === "stale" ? "var(--red)" : "var(--text-dim)",
        padding: "0.3rem 0",
        borderBottom: "1px solid var(--border)",
      }}>
        {label} · {rows.length}
      </div>
      <div style={{ overflow: "auto" }}>
        <table className="data-table" style={{ width: "100%", borderCollapse: "collapse", fontSize: "0.9em" }}>
          <thead>
            <tr style={{ textAlign: "left", borderBottom: "1px solid var(--border)" }}>
              <th style={{ padding: "0.4rem 0.6rem" }}>Task</th>
              <th style={{ padding: "0.4rem 0.6rem" }}>Project</th>
              <th style={{ padding: "0.4rem 0.6rem" }}>State</th>
              <th style={{ padding: "0.4rem 0.6rem" }}>Waiting</th>
              <th style={{ padding: "0.4rem 0.6rem" }}>Expires</th>
              <th style={{ padding: "0.4rem 0.6rem" }}>Latest placement decision</th>
              <th style={{ padding: "0.4rem 0.6rem" }}>Error</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((p) => {
              const reason = p.task.last_placement_reason;
              const stateColor =
                p.leaseState === "acked" ? "var(--green)" :
                p.leaseState === "pushed" ? "var(--yellow)" :
                "var(--text-dim)";
              const expiresIn = p.expiresAt - now;
              const expiresColor = expiresIn < 30_000 ? "var(--red)" : expiresIn < 120_000 ? "var(--yellow)" : "var(--text-dim)";
              const decisionColor =
                reason?.decision === "accepted" ? "var(--green)" :
                reason?.decision === "no_candidate" || reason?.decision === "rejected" ? "var(--red)" :
                "var(--yellow)";
              return (
                <tr key={`${p.projectId}:${p.task.id}`} style={{ borderBottom: "1px solid var(--border-faint, var(--border))" }}>
                  <td style={{ padding: "0.4rem 0.6rem" }}>
                    <div className="mono" style={{ fontSize: "0.85em" }}>{p.task.id}</div>
                    <div className="faint truncate" style={{ maxWidth: "32ch" }}>{p.task.title}</div>
                  </td>
                  <td style={{ padding: "0.4rem 0.6rem" }} className="mono">{p.projectId}</td>
                  <td style={{ padding: "0.4rem 0.6rem", color: stateColor }}>{p.leaseState}</td>
                  <td style={{ padding: "0.4rem 0.6rem" }} title={new Date(p.pushedAt).toISOString()}>
                    {formatDuration(now - p.pushedAt)}
                  </td>
                  <td style={{ padding: "0.4rem 0.6rem", color: expiresColor }} title={new Date(p.expiresAt).toISOString()}>
                    {formatDuration(expiresIn)}
                  </td>
                  <td style={{ padding: "0.4rem 0.6rem" }}>
                    {reason ? (
                      <div>
                        <div style={{ color: decisionColor }}>{reason.decision ?? "—"}</div>
                        <div className="faint" style={{ fontSize: "0.85em" }}>{reason.reason ?? ""}</div>
                      </div>
                    ) : <span className="faint">—</span>}
                  </td>
                  <td style={{ padding: "0.4rem 0.6rem", color: p.lastError ? "var(--red)" : undefined }}>
                    {p.lastError ?? <span className="faint">—</span>}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}

/** Format a millisecond duration as a short relative string. Used for the
 * "expires in" column where negative values mean the lease should have
 * already expired (and will be cleaned up on the next scheduler tick). */
function formatDuration(ms: number): string {
  if (!Number.isFinite(ms)) return "—";
  const abs = Math.abs(ms);
  const sign = ms < 0 ? "-" : "";
  if (abs < 1000) return `${sign}${abs}ms`;
  const sec = Math.floor(abs / 1000);
  if (sec < 60) return `${sign}${sec}s`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${sign}${min}m${sec % 60 ? ` ${sec % 60}s` : ""}`;
  const hr = Math.floor(min / 60);
  return `${sign}${hr}h${min % 60 ? ` ${min % 60}m` : ""}`;
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
