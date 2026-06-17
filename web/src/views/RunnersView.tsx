import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { useLive } from "../lib/sse";
import { ALL_PROJECTS, useUI } from "../store/ui";
import { useNav } from "../store/nav";
import { useViewKeyboard, handleListNavKey } from "../lib/keyboard";
import {
  getRunnerStatus,
  getRunners,
  listInstances,
  pauseAll,
  resumeAll,
  pauseAutomations,
  resumeAutomations,
  shutdownRunner,
} from "../lib/api";
import { ConfirmDialog } from "../components/common/Modal";
import { EmptyState, ErrorState, Loading } from "../components/common/states";
import { relativeTime } from "../lib/format";
import type { OpencodeInstance, RunnerInfo } from "../lib/types";

export function RunnersView() {
  const toast = useUI((s) => s.toast);
  const openInControl = useUI((s) => s.openInControl);
  const activeProject = useUI((s) => s.activeProject);
  const qc = useQueryClient();
  const liveRunners = useLive((s) => s.runners);

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
    refetchInterval: 10_000,
    staleTime: 10_000,
  });

  const [confirmKill, setConfirmKill] = useState<RunnerInfo | null>(null);
  const [busy, setBusy] = useState(false);

  // Prefer the live SSE list when present; fall back to the polled query.
  const runners = liveRunners.length ? liveRunners : runnersQ.data ?? [];
  const instances = instancesQ.data ?? [];
  const rows = useMemo(() => {
    const out: Array<
      | { kind: "runner"; runner: RunnerInfo; instances: OpencodeInstance[] }
      | { kind: "instance"; runner: RunnerInfo; instance: OpencodeInstance }
    > = [];
    for (const runner of runners) {
      const runnerInstances = instances.filter((inst) => inst.runner_id === runner.runner_id);
      out.push({ kind: "runner", runner, instances: runnerInstances });
      for (const instance of runnerInstances) out.push({ kind: "instance", runner, instance });
    }
    return out;
  }, [runners, instances]);
  const rowIndexByKey = useMemo(() => {
    const indexes = new Map<string, number>();
    rows.forEach((row, i) => {
      indexes.set(
        row.kind === "runner" ? `runner:${row.runner.runner_id}` : `instance:${row.instance.instance_id}`,
        i,
      );
    });
    return indexes;
  }, [rows]);
  const status = statusQ.data;
  const taskPaused = !!status?.paused;
  const automationPaused = !!status?.automationsPaused;
  const allProjectsSelected = activeProject === ALL_PROJECTS;
  const allPaused = taskPaused && automationPaused;

  const scope = "runners";
  const cursor = useNav((s) => Math.min(s.cursor[scope] ?? 0, Math.max(0, rows.length - 1)));

  function openInstance(inst: OpencodeInstance) {
    openInControl({
      mode: "live",
      runnerId: inst.runner_id,
      instanceId: inst.instance_id,
      sessionId: inst.session_ids?.[0],
      taskTitle: inst.title || inst.task_id,
    });
  }

  function openRow(row: (typeof rows)[number] | undefined) {
    if (!row) return;
    if (row.kind === "instance") {
      openInstance(row.instance);
      return;
    }
    const first = row.instances[0];
    if (!first) {
      toast("Runner has no instances", "info");
      return;
    }
    const idx = rowIndexByKey.get(`instance:${first.instance_id}`);
    if (idx !== undefined) useNav.getState().setCursor(scope, idx);
    openInstance(first);
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
        case "s":
          if (cur?.kind === "runner") setConfirmKill(cur.runner);
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

  useEffect(() => {
    const el = document.querySelector<HTMLElement>('[data-cursor="1"]');
    el?.scrollIntoView({ block: "nearest" });
  }, [cursor]);

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
    } catch (e) {
      toast(e instanceof Error ? e.message : "Action failed", "error");
    } finally {
      setBusy(false);
    }
  }

  if (runnersQ.isLoading && !runners.length) return <Loading label="Loading runners…" />;
  if (runnersQ.error && !runners.length)
    return <ErrorState error={runnersQ.error} onRetry={() => void runnersQ.refetch()} />;

  return (
    <div>
      {status && (
        <div
          className="row wrap"
          style={{
            gap: 8,
            padding: "2px 2px 6px",
            marginBottom: 4,
            borderBottom: "1px solid var(--border)",
            fontSize: 12.5,
          }}
        >
          <span style={{ color: taskPaused ? "var(--red)" : "var(--green)" }}>
            ● runner pool {taskPaused ? "paused" : "running"}
          </span>
          <span style={{ color: automationPaused ? "var(--red)" : "var(--green)" }}>
            ● automations {automationPaused ? "off" : "on"}
          </span>
          <div style={{ flex: 1 }} />
          {allProjectsSelected && (
            <button className="btn sm primary" disabled={busy} onClick={toggleAllPause} title="Shortcut: p">
              {allPaused ? "▶ resume all" : "⏸ pause all"}
            </button>
          )}
          <button className="btn sm ghost" disabled={busy} onClick={toggleTaskPause} title="Shortcut: p">
            {taskPaused ? "resume tasks" : "pause tasks"}
          </button>
          <button className="btn sm ghost" disabled={busy} onClick={toggleAutomationPause} title="Shortcut: a">
            {automationPaused ? "resume autos" : "pause autos"}
          </button>
        </div>
      )}

      {runners.length === 0 ? (
        <EmptyState
          glyph="⚙"
          title="No runners online"
          hint="Start one with: brain run start <project> --headless"
        />
      ) : (
        runners.map((r, i) => (
          <RunnerRow
            key={r.runner_id}
            runner={r}
            instances={instances.filter((inst) => inst.runner_id === r.runner_id)}
            cursored={rowIndexByKey.get(`runner:${r.runner_id}`) === cursor}
            last={i === runners.length - 1}
            cursor={cursor}
            rowIndexByKey={rowIndexByKey}
            onCursor={(idx) => useNav.getState().setCursor(scope, idx)}
            onOpen={openInstance}
            onKill={() => setConfirmKill(r)}
          />
        ))
      )}

      {confirmKill && (
        <ConfirmDialog
          title="Shut down runner?"
          danger
          confirmLabel="Shut down"
          busy={busy}
          message={
            <>
              Request graceful shutdown of{" "}
              <strong className="mono">{confirmKill.runner_id}</strong> on{" "}
              {confirmKill.hostname}.
            </>
          }
          onClose={() => setConfirmKill(null)}
          onConfirm={() =>
            void act("Shutdown requested", () =>
              shutdownRunner(confirmKill.runner_id),
            ).then(() => setConfirmKill(null))
          }
        />
      )}
    </div>
  );
}

function RunnerRow({
  runner,
  instances,
  cursored,
  last,
  cursor,
  rowIndexByKey,
  onCursor,
  onOpen,
  onKill,
}: {
  runner: RunnerInfo;
  instances: OpencodeInstance[];
  cursored?: boolean;
  last?: boolean;
  cursor: number;
  rowIndexByKey: Map<string, number>;
  onCursor: (idx: number) => void;
  onOpen: (inst: OpencodeInstance) => void;
  onKill: () => void;
}) {
  const statusColor =
    runner.status === "online"
      ? "var(--green)"
      : runner.status === "stale"
        ? "var(--yellow)"
        : "var(--red)";
  return (
    <>
      <div
        className={`tree-row ${cursored ? "cursor" : ""}`}
        data-cursor={cursored ? "1" : undefined}
        style={{ gap: 4 }}
        onClick={() => {
          const idx = rowIndexByKey.get(`runner:${runner.runner_id}`);
          if (idx !== undefined) onCursor(idx);
        }}
      >
        <span className="connector">{last ? "└─ " : "├─ "}</span>
        <span className="glyph" style={{ color: cursored ? undefined : statusColor }}>
          ●
        </span>
        <span className="title truncate">{runner.runner_id}</span>
        <span className="suffix faint">{runner.hostname}</span>
        <span className="suffix" style={{ color: "var(--blue)" }}>
          {runner.active_tasks ?? 0}/{runner.max_parallel}
        </span>
        {runner.executors && runner.executors.length > 0 && (
          <span className="suffix" style={{ color: "var(--teal)" }}>
            {runner.executors.join(",")}
          </span>
        )}
        <span className="suffix faint">hb {relativeTime(runner.last_heartbeat)}</span>
        <span
          className="suffix"
          style={{ cursor: "pointer", color: "var(--red)" }}
          title="Shut down (s)"
          onClick={(e) => { e.stopPropagation(); onKill(); }}
        >
          ⏻
        </span>
      </div>
      {instances.map((inst, j) => (
        <InstanceRow
          key={inst.instance_id}
          instance={inst}
          parentLast={!!last}
          last={j === instances.length - 1}
          cursored={rowIndexByKey.get(`instance:${inst.instance_id}`) === cursor}
          onCursor={() => {
            const idx = rowIndexByKey.get(`instance:${inst.instance_id}`);
            if (idx !== undefined) onCursor(idx);
          }}
          onOpen={() => onOpen(inst)}
        />
      ))}
    </>
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

function InstanceRow({
  instance,
  parentLast,
  last,
  cursored,
  onCursor,
  onOpen,
}: {
  instance: OpencodeInstance;
  parentLast: boolean;
  last: boolean;
  cursored?: boolean;
  onCursor: () => void;
  onOpen: () => void;
}) {
  const sessions = instance.session_ids?.length ?? 0;
  return (
    <div
      className={`tree-row ${cursored ? "cursor" : ""}`}
      data-cursor={cursored ? "1" : undefined}
      style={{ gap: 4 }}
      onClick={onCursor}
      onDoubleClick={onOpen}
    >
      <span className="connector">
        {parentLast ? "   " : "│  "}
        {last ? "└─ " : "├─ "}
      </span>
      <span
        className="glyph"
        style={{ color: instanceStatusColor(instance.status) }}
        title={instance.status}
      >
        ▣
      </span>
      <span
        className="suffix"
        style={{
          color: instance.kind === "adhoc" ? "var(--purple, var(--teal))" : "var(--teal)",
        }}
      >
        {instance.kind}
      </span>
      <span className="title truncate">
        {instance.title || instance.task_id || instance.instance_id}
      </span>
      {instance.workdir && (
        <span className="suffix faint truncate" title={instance.workdir}>
          {instance.workdir.replace(/^.*\//, "…/")}
        </span>
      )}
      <span className="suffix faint">{instance.status}</span>
      {instance.port ? <span className="suffix faint">:{instance.port}</span> : null}
      {sessions > 0 && (
        <span className="suffix" style={{ color: "var(--blue)" }}>
          {sessions} ses
        </span>
      )}
      {(instance.pending_permissions ?? 0) > 0 && (
        <span className="suffix" style={{ color: "var(--red)" }}>
          {instance.pending_permissions} ⚠ perm
        </span>
      )}
    </div>
  );
}
