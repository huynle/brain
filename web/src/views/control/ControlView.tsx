// Remote-control command center: runners → instances → sessions → chat.
// Desktop: rail + chat side by side. Mobile: single column, the rail
// collapses behind a back button once an instance is selected.

import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  controlCreateSession,
  controlKillInstance,
  controlListSessions,
  controlSpawnInstance,
  getRunners,
  listInstances,
} from "../../lib/api";
import { Modal, ConfirmDialog } from "../../components/common/Modal";
import { EmptyState, ErrorState, Loading, Spinner } from "../../components/common/states";
import { useUI } from "../../store/ui";
import type { OcSession, OpencodeInstance, RunnerInfo } from "../../lib/types";
import { Chat } from "./Chat";

interface Selection {
  runnerId: string;
  instanceId: string;
}

export function ControlView() {
  const toast = useUI((s) => s.toast);
  const qc = useQueryClient();
  const [selected, setSelected] = useState<Selection | null>(null);
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [spawnOpen, setSpawnOpen] = useState(false);
  const [confirmKill, setConfirmKill] = useState<OpencodeInstance | null>(null);

  const runnersQ = useQuery({ queryKey: ["runners"], queryFn: getRunners, refetchInterval: 15_000 });
  const instancesQ = useQuery({
    queryKey: ["instances"],
    queryFn: listInstances,
    refetchInterval: 8_000,
  });

  const runners = runnersQ.data ?? [];
  const instances = instancesQ.data ?? [];
  const selectedInstance = useMemo(
    () => instances.find((i) => i.instance_id === selected?.instanceId) ?? null,
    [instances, selected],
  );

  function pick(inst: OpencodeInstance) {
    setSelected({ runnerId: inst.runner_id, instanceId: inst.instance_id });
    setSessionId(inst.session_ids?.[0] ?? null);
  }

  async function kill(inst: OpencodeInstance) {
    try {
      await controlKillInstance(inst.runner_id, inst.instance_id);
      toast("Instance killed", "success");
      if (selected?.instanceId === inst.instance_id) {
        setSelected(null);
        setSessionId(null);
      }
      void qc.invalidateQueries({ queryKey: ["instances"] });
    } catch (e) {
      toast(e instanceof Error ? e.message : "Kill failed", "error");
    }
  }

  if (runnersQ.isLoading && !runners.length) return <Loading label="Loading runners…" />;
  if (runnersQ.error && !runners.length)
    return <ErrorState error={runnersQ.error} onRetry={() => void runnersQ.refetch()} />;

  const chatOpen = !!selected;

  return (
    <div className={`ctl-layout ${chatOpen ? "chat-open" : ""}`}>
      <div className="ctl-rail">
        <div className="row" style={{ padding: "4px 6px", gap: 6 }}>
          <strong style={{ fontSize: 12.5 }}>Runners</strong>
          <span style={{ flex: 1 }} />
          <button className="btn sm" onClick={() => setSpawnOpen(true)}>
            + new instance
          </button>
        </div>

        {runners.length === 0 && (
          <EmptyState
            glyph="⚙"
            title="No runners online"
            hint="Start one with: brain start <project>"
          />
        )}

        {runners.map((r) => (
          <RailRunner
            key={r.runner_id}
            runner={r}
            instances={instances.filter((i) => i.runner_id === r.runner_id)}
            selected={selected?.instanceId ?? null}
            onPick={pick}
            onKill={(inst) => setConfirmKill(inst)}
          />
        ))}
      </div>

      <div className="ctl-main">
        {!selected && (
          <EmptyState
            glyph="⌁"
            title="No instance attached"
            hint="Pick an instance on the left, or spawn a new one."
          />
        )}
        {selected && !selectedInstance && (
          <EmptyState glyph="⌁" title="Instance gone" hint="It may have exited." />
        )}
        {selected && selectedInstance && (
          <InstancePane
            instance={selectedInstance}
            sessionId={sessionId}
            onBack={() => {
              setSelected(null);
              setSessionId(null);
            }}
            onSession={setSessionId}
          />
        )}
      </div>

      {spawnOpen && (
        <SpawnModal
          runners={runners}
          onClose={() => setSpawnOpen(false)}
          onSpawned={(inst) => {
            setSpawnOpen(false);
            void qc.invalidateQueries({ queryKey: ["instances"] });
            pick(inst);
            toast("Instance spawned", "success");
          }}
        />
      )}

      {confirmKill && (
        <ConfirmDialog
          title="Kill instance?"
          danger
          confirmLabel="Kill"
          message={
            <>
              Terminate ad-hoc instance{" "}
              <strong className="mono">{confirmKill.instance_id}</strong> in{" "}
              <span className="mono">{confirmKill.workdir}</span>?
            </>
          }
          onClose={() => setConfirmKill(null)}
          onConfirm={() => {
            void kill(confirmKill).then(() => setConfirmKill(null));
          }}
        />
      )}
    </div>
  );
}

function instanceDot(status: OpencodeInstance["status"]): string {
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

function RailRunner({
  runner,
  instances,
  selected,
  onPick,
  onKill,
}: {
  runner: RunnerInfo;
  instances: OpencodeInstance[];
  selected: string | null;
  onPick: (inst: OpencodeInstance) => void;
  onKill: (inst: OpencodeInstance) => void;
}) {
  const online = runner.status === "online";
  return (
    <div className="ctl-runner">
      <div className="row" style={{ gap: 6, padding: "4px 6px" }}>
        <span style={{ color: online ? "var(--green)" : "var(--red)" }}>●</span>
        <span className="mono truncate" style={{ fontSize: 12.5 }}>
          {runner.runner_id}
        </span>
        <span className="faint" style={{ fontSize: 11.5 }}>
          {runner.hostname}
        </span>
      </div>
      {instances.length === 0 && (
        <div className="faint" style={{ padding: "0 6px 6px 22px", fontSize: 12 }}>
          no instances
        </div>
      )}
      {instances.map((inst) => (
        <div
          key={inst.instance_id}
          className={`ctl-inst ${selected === inst.instance_id ? "on" : ""}`}
          onClick={() => onPick(inst)}
        >
          <span style={{ color: instanceDot(inst.status) }} title={inst.status}>
            ▣
          </span>
          <span
            className="ctl-kind"
            style={{ color: inst.kind === "adhoc" ? "var(--teal)" : "var(--blue)" }}
          >
            {inst.kind}
          </span>
          <span className="truncate" style={{ flex: 1 }}>
            {inst.title || inst.task_id || inst.instance_id}
          </span>
          {(inst.pending_permissions ?? 0) > 0 && (
            <span style={{ color: "var(--red)", fontSize: 11.5 }}>
              {inst.pending_permissions}⚠
            </span>
          )}
          {inst.kind === "adhoc" && (
            <span
              title="Kill instance"
              style={{ color: "var(--red)", cursor: "pointer" }}
              onClick={(e) => {
                e.stopPropagation();
                onKill(inst);
              }}
            >
              ✕
            </span>
          )}
        </div>
      ))}
    </div>
  );
}

function InstancePane({
  instance,
  sessionId,
  onBack,
  onSession,
}: {
  instance: OpencodeInstance;
  sessionId: string | null;
  onBack: () => void;
  onSession: (id: string) => void;
}) {
  const toast = useUI((s) => s.toast);
  const qc = useQueryClient();
  const rid = instance.runner_id;
  const iid = instance.instance_id;

  const sessionsQ = useQuery({
    queryKey: ["control-sessions", rid, iid],
    queryFn: () => controlListSessions(rid, iid),
    refetchInterval: sessionId ? false : 10_000,
    retry: 1,
  });

  const sessions = useMemo(() => {
    const list = sessionsQ.data ?? [];
    return [...list].sort((a, b) => (b.time?.updated ?? 0) - (a.time?.updated ?? 0));
  }, [sessionsQ.data]);

  async function newSession() {
    try {
      const ses = await controlCreateSession(rid, iid);
      void qc.invalidateQueries({ queryKey: ["control-sessions", rid, iid] });
      if (ses?.id) onSession(ses.id);
    } catch (e) {
      toast(e instanceof Error ? e.message : "Create session failed", "error");
    }
  }

  return (
    <div className="ctl-pane">
      <div className="row ctl-pane-head" style={{ gap: 8 }}>
        <button className="btn sm ghost ctl-back" onClick={onBack}>
          ← back
        </button>
        <span style={{ color: instanceDot(instance.status) }}>▣</span>
        <strong className="truncate">{instance.title || instance.instance_id}</strong>
        <span className="faint truncate" style={{ fontSize: 11.5 }}>
          {instance.workdir}
        </span>
        <span style={{ flex: 1 }} />
        <select
          value={sessionId ?? ""}
          onChange={(e) => e.target.value && onSession(e.target.value)}
          title="Session"
        >
          <option value="">
            {sessionsQ.isLoading ? "loading sessions…" : "select session…"}
          </option>
          {sessions.map((s: OcSession) => (
            <option key={s.id} value={s.id}>
              {(s.title || s.id).slice(0, 60)}
            </option>
          ))}
        </select>
        <button className="btn sm ghost" onClick={() => void newSession()}>
          + session
        </button>
      </div>

      {sessionsQ.error && !sessions.length && (
        <div className="faint" style={{ padding: 12, color: "var(--red)" }}>
          Cannot reach instance: {String((sessionsQ.error as Error).message)}
        </div>
      )}

      {sessionId ? (
        <Chat runnerId={rid} instanceId={iid} sessionId={sessionId} />
      ) : (
        <EmptyState
          glyph="◌"
          title="No session selected"
          hint="Pick a session above or create a new one."
        />
      )}
    </div>
  );
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
      const res = await controlSpawnInstance(runnerId, {
        workdir,
        title: title || undefined,
      });
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
          <button className="btn ghost" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button className="btn" onClick={() => void spawn()} disabled={busy || !runnerId || !workdir}>
            {busy ? <Spinner /> : "Spawn"}
          </button>
        </>
      }
    >
      <div className="form-col" style={{ display: "flex", flexDirection: "column", gap: 10 }}>
        <label>
          <div className="faint" style={{ fontSize: 12 }}>Runner</div>
          <select value={runnerId} onChange={(e) => setRunnerId(e.target.value)} style={{ width: "100%" }}>
            {online.length === 0 && <option value="">no online runners</option>}
            {online.map((r) => (
              <option key={r.runner_id} value={r.runner_id}>
                {r.runner_id} ({r.hostname})
              </option>
            ))}
          </select>
        </label>
        <label>
          <div className="faint" style={{ fontSize: 12 }}>Working directory (absolute path on the runner)</div>
          <input
            type="text"
            value={workdir}
            placeholder="/home/user/projects/my-repo"
            onChange={(e) => setWorkdir(e.target.value)}
            style={{ width: "100%" }}
          />
        </label>
        <label>
          <div className="faint" style={{ fontSize: 12 }}>Title (optional)</div>
          <input
            type="text"
            value={title}
            placeholder="quick fix session"
            onChange={(e) => setTitle(e.target.value)}
            style={{ width: "100%" }}
          />
        </label>
        {error && <div style={{ color: "var(--red)", fontSize: 12.5 }}>{error}</div>}
      </div>
    </Modal>
  );
}
