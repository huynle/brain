import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useLive } from "../lib/sse";
import { useUI } from "../store/ui";
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
  const qc = useQueryClient();
  const liveRunners = useLive((s) => s.runners);

  const runnersQ = useQuery({
    queryKey: ["runners"],
    queryFn: getRunners,
    refetchInterval: 10_000,
  });
  const statusQ = useQuery({
    queryKey: ["runner-status"],
    queryFn: getRunnerStatus,
    refetchInterval: 8_000,
  });
  const instancesQ = useQuery({
    queryKey: ["instances"],
    queryFn: listInstances,
    refetchInterval: 10_000,
  });

  const [confirmKill, setConfirmKill] = useState<RunnerInfo | null>(null);
  const [busy, setBusy] = useState(false);

  // Prefer the live SSE list when present; fall back to the polled query.
  const runners = liveRunners.length ? liveRunners : runnersQ.data ?? [];
  const status = statusQ.data;

  const scope = "runners";
  const cursor = useNav((s) => Math.min(s.cursor[scope] ?? 0, Math.max(0, runners.length - 1)));

  useViewKeyboard(
    (e) => {
      if (handleListNavKey(e, scope, runners.length)) return true;
      const cur = runners[cursor];
      switch (e.key) {
        case "s":
          if (cur) setConfirmKill(cur);
          return true;
        case "p":
          void (status?.paused ? act("Resumed", resumeAll) : act("Paused", pauseAll));
          return true;
        case "P":
          void act("Paused all", pauseAll);
          return true;
        default:
          return false;
      }
    },
    [runners, cursor, status?.paused],
  );

  useEffect(() => {
    const el = document.querySelector<HTMLElement>('[data-cursor="1"]');
    el?.scrollIntoView({ block: "nearest" });
  }, [cursor]);

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
          <span style={{ color: status.paused ? "var(--red)" : "var(--green)" }}>
            ● pool {status.paused ? "paused" : "running"}
          </span>
          <span style={{ color: status.automationsPaused ? "var(--red)" : "var(--green)" }}>
            ● automations {status.automationsPaused ? "off" : "on"}
          </span>
          <div style={{ flex: 1 }} />
          <button
            className="btn sm ghost"
            disabled={busy}
            onClick={() =>
              status.paused
                ? void act("Resumed", resumeAll)
                : void act("Paused", pauseAll)
            }
          >
            {status.paused ? "▶ resume all" : "⏸ pause all"}
          </button>
          <button
            className="btn sm ghost"
            disabled={busy}
            onClick={() =>
              status.automationsPaused
                ? void act("Automations resumed", resumeAutomations)
                : void act("Automations paused", pauseAutomations)
            }
          >
            {status.automationsPaused ? "resume autos" : "pause autos"}
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
            instances={(instancesQ.data ?? []).filter(
              (inst) => inst.runner_id === r.runner_id,
            )}
            cursored={i === cursor}
            last={i === runners.length - 1}
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
  onKill,
}: {
  runner: RunnerInfo;
  instances: OpencodeInstance[];
  cursored?: boolean;
  last?: boolean;
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
}: {
  instance: OpencodeInstance;
  parentLast: boolean;
  last: boolean;
}) {
  const sessions = instance.session_ids?.length ?? 0;
  return (
    <div className="tree-row" style={{ gap: 4 }}>
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
