import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useLive } from "../lib/sse";
import { useUI } from "../store/ui";
import {
  getRunnerStatus,
  getRunners,
  pauseAll,
  resumeAll,
  pauseAutomations,
  resumeAutomations,
  shutdownRunner,
} from "../lib/api";
import { Pill } from "../components/common/Badge";
import { ConfirmDialog } from "../components/common/Modal";
import { EmptyState, ErrorState, Loading } from "../components/common/states";
import { relativeTime } from "../lib/format";
import type { RunnerInfo } from "../lib/types";

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

  const [confirmKill, setConfirmKill] = useState<RunnerInfo | null>(null);
  const [busy, setBusy] = useState(false);

  // Prefer the live SSE list when present; fall back to the polled query.
  const runners = liveRunners.length ? liveRunners : runnersQ.data ?? [];
  const status = statusQ.data;

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
    <div className="section-pad">
      {status && (
        <div className="card section-pad" style={{ marginBottom: "0.8rem" }}>
          <div className="row" style={{ marginBottom: "0.6rem" }}>
            <strong>Runner pool</strong>
            <div className="spacer" style={{ flex: 1 }} />
            <Pill color={status.paused ? "var(--red)" : "var(--green)"}>
              {status.paused ? "paused" : "running"}
            </Pill>
            <Pill
              color={status.automationsPaused ? "var(--red)" : "var(--green)"}
            >
              automations {status.automationsPaused ? "off" : "on"}
            </Pill>
          </div>
          <div className="btn-row">
            {status.paused ? (
              <button
                className="btn sm primary"
                disabled={busy}
                onClick={() => void act("Resumed", resumeAll)}
              >
                ▶ Resume all
              </button>
            ) : (
              <button
                className="btn sm"
                disabled={busy}
                onClick={() => void act("Paused", pauseAll)}
              >
                ⏸ Pause all
              </button>
            )}
            {status.automationsPaused ? (
              <button
                className="btn sm"
                disabled={busy}
                onClick={() => void act("Automations resumed", resumeAutomations)}
              >
                Resume automations
              </button>
            ) : (
              <button
                className="btn sm"
                disabled={busy}
                onClick={() => void act("Automations paused", pauseAutomations)}
              >
                Pause automations
              </button>
            )}
          </div>
          {status.pausedProjects?.length > 0 && (
            <div className="muted" style={{ marginTop: "0.5rem", fontSize: 13 }}>
              Paused projects: {status.pausedProjects.join(", ")}
            </div>
          )}
        </div>
      )}

      {runners.length === 0 ? (
        <EmptyState
          glyph="⚙"
          title="No runners online"
          hint="Start one with: brain run start <project> --headless"
        />
      ) : (
        runners.map((r) => (
          <RunnerCard
            key={r.runner_id}
            runner={r}
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

function RunnerCard({
  runner,
  onKill,
}: {
  runner: RunnerInfo;
  onKill: () => void;
}) {
  const statusColor =
    runner.status === "online"
      ? "var(--green)"
      : runner.status === "stale"
        ? "var(--yellow)"
        : "var(--red)";
  return (
    <div className="card section-pad" style={{ marginBottom: "0.6rem" }}>
      <div className="row">
        <span className="conn-dot" style={{ background: statusColor }} />
        <strong className="mono" style={{ fontSize: 13 }}>
          {runner.runner_id}
        </strong>
        <div style={{ flex: 1 }} />
        <Pill color={statusColor}>{runner.status}</Pill>
      </div>
      <div className="row wrap" style={{ gap: "0.35rem", marginTop: "0.5rem" }}>
        <Pill>{runner.hostname}</Pill>
        <Pill color="var(--blue)">
          {runner.active_tasks ?? 0}/{runner.max_parallel} active
        </Pill>
        {runner.executors?.map((e) => (
          <Pill key={e} color="var(--teal)">
            {e}
          </Pill>
        ))}
        {runner.version && <Pill className="faint">v{runner.version}</Pill>}
      </div>
      {runner.feature_assignments && runner.feature_assignments.length > 0 && (
        <div className="muted" style={{ fontSize: 12.5, marginTop: "0.4rem" }}>
          features:{" "}
          {runner.feature_assignments.map((f) => f.feature_id).join(", ")}
        </div>
      )}
      <div
        className="row"
        style={{ marginTop: "0.5rem", justifyContent: "space-between" }}
      >
        <span className="faint" style={{ fontSize: 12 }}>
          heartbeat {relativeTime(runner.last_heartbeat)}
        </span>
        <button className="btn danger sm" onClick={onKill}>
          Shut down
        </button>
      </div>
    </div>
  );
}
