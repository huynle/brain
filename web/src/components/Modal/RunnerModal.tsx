/**
 * RunnerModal — wireframe-parity.
 *
 * Three tabs: Overview / Shell (mock, backend blocked on 4mymbjen) / Logs.
 */
import { useMemo } from "react";
import { Modal, type ModalTab } from "../common/Modal";
import { useModal } from "../../store/modal";
import { useLive } from "../../lib/sse";
import { useRunners } from "../../hooks/useRunners";
import type { RunnerInfo } from "../../lib/types";
import { MockShell } from "../MockShell";

const TABS: ModalTab[] = [
  { id: "overview", label: "Overview" },
  { id: "shell", label: "Shell" },
  { id: "logs", label: "Logs" },
];

function statusDot(s: RunnerInfo["status"]): "on" | "err" | "busy" {
  if (s === "online") return "on";
  if (s === "stale") return "busy";
  return "err";
}

export function RunnerModal(): JSX.Element {
  const target = useModal((s) => s.target);
  const tab = useModal((s) => s.tab) ?? "overview";
  const switchTab = useModal((s) => s.switchTab);
  const close = useModal((s) => s.close);

  const runnerId =
    (target?.runnerId as string | undefined) ??
    (target?.id as string | undefined) ??
    "";

  const { runners } = useRunners();
  const runner = runners.find((r) => r.runner_id === runnerId);

  const logs = useLive((s) => s.logs);
  const runnerLogs = useMemo(
    () =>
      logs.filter((r) => r.runnerId === runnerId).slice(-80).reverse(),
    [logs, runnerId],
  );

  if (!runner) {
    return (
      <Modal
        title={runnerId ? `Runner not found: ${runnerId}` : "Runner"}
        onClose={close}
      >
        <div style={{ color: "#9098a1" }}>
          Runner is not currently registered.
        </div>
      </Modal>
    );
  }

  return (
    <Modal
      title={
        <>
          <span
            className={`dot ${statusDot(runner.status)}`}
            style={{
              display: "inline-block",
              width: 8,
              height: 8,
              borderRadius: "50%",
              marginRight: 6,
              verticalAlign: "middle",
              background:
                runner.status === "online"
                  ? "#6fca7d"
                  : runner.status === "stale"
                    ? "#f4b23a"
                    : "#d96060",
            }}
          />
          {runner.runner_id}
        </>
      }
      onClose={close}
      tabs={TABS}
      activeTab={tab}
      onTabChange={switchTab}
    >
      {tab === "overview" && (
        <div className="kv-grid">
          <div className="k">Status</div>
          <div className="v">{runner.status}</div>
          <div className="k">Hostname</div>
          <div className="v">
            <code>{runner.hostname}</code>
          </div>
          <div className="k">Capacity</div>
          <div className="v">
            {runner.active_tasks ?? 0}/{runner.max_parallel ?? 0}
          </div>
          {runner.executors && runner.executors.length > 0 && (
            <>
              <div className="k">Executors</div>
              <div className="v">
                {runner.executors.map((e) => (
                  <span key={e} className="chip mini" style={{ marginRight: 4 }}>
                    {e}
                  </span>
                ))}
              </div>
            </>
          )}
          {runner.projects && runner.projects.length > 0 && (
            <>
              <div className="k">Projects</div>
              <div className="v">
                {runner.projects.slice(0, 6).map((p) => (
                  <span key={p} className="chip mini" style={{ marginRight: 4 }}>
                    {p}
                  </span>
                ))}
                {runner.projects.length > 6 && (
                  <span style={{ color: "#6b757e", fontSize: 10 }}>
                    +{runner.projects.length - 6} more
                  </span>
                )}
              </div>
            </>
          )}
          {runner.labels &&
            Object.entries(runner.labels).map(([k, v]) => (
              <>
                <div key={"k-" + k} className="k">
                  {k}
                </div>
                <div key={"v-" + k} className="v">
                  {v}
                </div>
              </>
            ))}
          {runner.registered_at && (
            <>
              <div className="k">Registered</div>
              <div className="v">{runner.registered_at}</div>
            </>
          )}
          {runner.last_heartbeat && (
            <>
              <div className="k">Last heartbeat</div>
              <div className="v">{runner.last_heartbeat}</div>
            </>
          )}
          {runner.feature_assignments &&
            runner.feature_assignments.length > 0 && (
              <>
                <div className="k">Feature assignments</div>
                <div className="v">
                  {runner.feature_assignments.map((a) => (
                    <span
                      key={a.feature_id}
                      className="chip mini"
                      style={{ marginRight: 4 }}
                    >
                      {a.project_id ? `${a.project_id}/` : ""}
                      {a.feature_id}
                    </span>
                  ))}
                </div>
              </>
            )}
        </div>
      )}

      {tab === "shell" && (
        <div>
          <div
            style={{
              padding: "6px 8px",
              background: "#f4b23a22",
              border: "1px solid #f4b23a",
              borderRadius: 4,
              color: "#f4b23a",
              fontSize: 11,
              marginBottom: 8,
            }}
          >
            ⚠ Mock shell. Real ad-hoc shell backend is blocked on RBAC
            (Brain task <code>4mymbjen</code>). Commands here run
            client-side against synthetic data.
          </div>
          <MockShell runner={runner} />
        </div>
      )}

      {tab === "logs" && (
        <div className="log-mini" style={{ height: 340 }}>
          <div className="head">
            <span className="live-dot" />
            <span className="title">Runner logs · {runner.runner_id}</span>
          </div>
          <div className="body">
            {runnerLogs.length === 0 && (
              <div style={{ color: "#4b545c", padding: 4, fontSize: 10.5 }}>
                No log lines yet.
              </div>
            )}
            {runnerLogs.map((r) => (
              <div key={r.seq} className="l">
                <span className="ts">
                  {r.line.timestamp
                    ? new Date(r.line.timestamp).toLocaleTimeString()
                    : ""}
                </span>
                <span className="lvl">{r.line.level || "INFO"}</span>
                <span className="msg">{r.line.content}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </Modal>
  );
}
