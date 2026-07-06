// Remote-control command center: runners → instances → sessions → chat.
// Desktop: rail + chat side by side. Mobile: single column, the rail
// collapses behind a back button once an instance is selected.

import { useEffect, useMemo, useRef, useState } from "react";
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
import { useNav } from "../../store/nav";
import { listNavHandlers } from "../../lib/keymap/listNav";
import { useActions } from "../../lib/keymap/useActions";
import { CONTROL_SPECS } from "./keymap";
import type { ControlTarget } from "../../store/ui";
import { useIsMobile } from "../../hooks/useIsMobile";
import { useSwipe } from "../../hooks/useSwipe";
import { sessionName } from "../../lib/types";
import type { OcSession, OpencodeInstance, RunnerInfo } from "../../lib/types";
import { Chat } from "./Chat";
import { HistoryPane } from "./HistoryPane";

interface Selection {
  runnerId: string;
  instanceId: string;
}

type ControlRow =
  | { kind: "runner"; runner: RunnerInfo; instances: OpencodeInstance[] }
  | { kind: "instance"; runner: RunnerInfo; instance: OpencodeInstance };

export function ControlView() {
  const toast = useUI((s) => s.toast);
  const consumeControlTarget = useUI((s) => s.consumeControlTarget);
  const qc = useQueryClient();
  const [selected, setSelected] = useState<Selection | null>(null);
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [historyTarget, setHistoryTarget] = useState<ControlTarget | null>(null);
  const [spawnOpen, setSpawnOpen] = useState(false);
  const [confirmKill, setConfirmKill] = useState<OpencodeInstance | null>(null);
  const railRef = useRef<HTMLDivElement | null>(null);

  const runnersQ = useQuery({ queryKey: ["runners"], queryFn: getRunners, refetchInterval: 15_000 });
  const instancesQ = useQuery({
    queryKey: ["instances"],
    queryFn: listInstances,
    // Poll briskly so a freshly-triggered task surfaces here within a few
    // seconds without a manual refresh.
    refetchInterval: 3_000,
  });

  const runners = runnersQ.data ?? [];
  const instances = instancesQ.data ?? [];
  const rows = useMemo<ControlRow[]>(() => {
    const out: ControlRow[] = [];
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
  const scope = "control";
  const cursor = useNav((s) => Math.min(s.cursor[scope] ?? 0, Math.max(0, rows.length - 1)));

  // Honor an "open in Control" request from another view (e.g. Automations "o"):
  // select the requested instance and session once it's known to the registry.
  useEffect(() => {
    const target = consumeControlTarget();
    if (!target) return;
    if (target.mode === "history") {
      // Completed/historical session: show the read-only review pane.
      setHistoryTarget(target);
      setSelected(null);
      setSessionId(target.sessionId ?? null);
      return;
    }
    setHistoryTarget(null);
    if (target.instanceId) setSelected({ runnerId: target.runnerId, instanceId: target.instanceId });
    if (target.sessionId) setSessionId(target.sessionId);
  }, [consumeControlTarget]);

  // Resume a reviewed session: spawn a server in the recorded workdir on a
  // connected runner; the session reloads from disk and becomes live.
  async function resumeHistory(runnerId: string) {
    const t = historyTarget;
    if (!t || !t.workdir) return;
    try {
      const res = await controlSpawnInstance(runnerId, {
        workdir: t.workdir,
        title: t.taskTitle,
      });
      void qc.invalidateQueries({ queryKey: ["instances"] });
      setHistoryTarget(null);
      pick(res.instance);
      if (t.sessionId) setSessionId(t.sessionId);
      toast("Session resumed", "success");
    } catch (e) {
      toast(e instanceof Error ? e.message : "Resume failed", "error");
    }
  }
  const selectedInstance = useMemo(
    () => instances.find((i) => i.instance_id === selected?.instanceId) ?? null,
    [instances, selected],
  );
  const chatOpen = !!selected || !!historyTarget;

  function pick(inst: OpencodeInstance) {
    setSelected({ runnerId: inst.runner_id, instanceId: inst.instance_id });
    setSessionId(inst.session_ids?.[0] ?? null);
  }

  // Mobile: an edge swipe (from the left, dragging right) returns from a
  // session/chat back to the instance list — the gesture equivalent of ← back.
  const isMobile = useIsMobile();
  function goBack() {
    setHistoryTarget(null);
    setSelected(null);
    setSessionId(null);
  }
  const backSwipe = useSwipe({ onRight: goBack, edgeOnly: 44 });

  function openRow(row: ControlRow | undefined) {
    if (!row) return;
    if (row.kind === "instance") {
      pick(row.instance);
      return;
    }
    const first = row.instances[0];
    if (!first) {
      toast("Runner has no instances", "info");
      return;
    }
    const idx = rowIndexByKey.get(`instance:${first.instance_id}`);
    if (idx !== undefined) useNav.getState().setCursor(scope, idx);
    pick(first);
  }

  useActions(
    "view:control",
    "view",
    CONTROL_SPECS,
    {
      ...listNavHandlers("control", { scope: () => scope, count: () => rows.length }),
      "control.open": () => openRow(rows[cursor]),
      "control.spawn": () => setSpawnOpen(true),
      "control.kill": () => {
        const cur = rows[cursor];
        if (cur?.kind !== "instance") return;
        if (cur.instance.kind !== "adhoc") {
          toast("Only ad-hoc instances can be killed", "info");
          return;
        }
        setConfirmKill(cur.instance);
      },
      "control.back": () => {
        if (!chatOpen) return false; // fall through to the global Esc chain
        goBack();
      },
    },
    [rows, cursor, chatOpen, rowIndexByKey],
  );

  useEffect(() => {
    const el = railRef.current?.querySelector<HTMLElement>('[data-cursor="1"]');
    el?.scrollIntoView({ block: "nearest" });
  }, [cursor]);

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

  return (
    <div className={`ctl-layout ${chatOpen ? "chat-open" : ""}`}>
      <div className="ctl-rail" ref={railRef}>
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
            cursor={cursor}
            rowIndexByKey={rowIndexByKey}
            onCursor={(idx) => useNav.getState().setCursor(scope, idx)}
            onPick={pick}
            onKill={(inst) => setConfirmKill(inst)}
          />
        ))}
      </div>

      <div className="ctl-main" {...(isMobile && chatOpen ? backSwipe : {})}>
        {historyTarget ? (
          <HistoryPane
            target={historyTarget}
            runners={runners}
            onBack={() => {
              setHistoryTarget(null);
              setSessionId(null);
            }}
            onResume={resumeHistory}
          />
        ) : !selected ? (
          <EmptyState
            glyph="⌁"
            title="No instance attached"
            hint="Pick an instance on the left, or spawn a new one."
          />
        ) : !selectedInstance ? (
          <EmptyState glyph="⌁" title="Instance gone" hint="It may have exited." />
        ) : (
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
  cursor,
  rowIndexByKey,
  onCursor,
  onPick,
  onKill,
}: {
  runner: RunnerInfo;
  instances: OpencodeInstance[];
  selected: string | null;
  cursor: number;
  rowIndexByKey: Map<string, number>;
  onCursor: (idx: number) => void;
  onPick: (inst: OpencodeInstance) => void;
  onKill: (inst: OpencodeInstance) => void;
}) {
  const online = runner.status === "online";
  const runnerIndex = rowIndexByKey.get(`runner:${runner.runner_id}`);
  const runnerCursored = runnerIndex === cursor;
  return (
    <div className="ctl-runner">
      <div
        className={`row ctl-runner-head ${runnerCursored ? "cursor" : ""}`}
        data-cursor={runnerCursored ? "1" : undefined}
        style={{ gap: 6, padding: "4px 6px" }}
        onClick={() => {
          if (runnerIndex !== undefined) onCursor(runnerIndex);
        }}
      >
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
      {instances.map((inst) => {
        const index = rowIndexByKey.get(`instance:${inst.instance_id}`);
        return (
          <InstanceRailItem
            key={inst.instance_id}
            instance={inst}
            selected={selected === inst.instance_id}
            cursored={index === cursor}
            onCursor={() => {
              if (index !== undefined) onCursor(index);
            }}
            onPick={onPick}
            onKill={onKill}
          />
        );
      })}
    </div>
  );
}

function InstanceRailItem({
  instance,
  selected,
  cursored,
  onCursor,
  onPick,
  onKill,
}: {
  instance: OpencodeInstance;
  selected: boolean;
  cursored?: boolean;
  onCursor: () => void;
  onPick: (inst: OpencodeInstance) => void;
  onKill: (inst: OpencodeInstance) => void;
}) {
  const meta = compactInstanceMetadata(instance);
  return (
    <div
      className={`ctl-inst ${selected ? "on" : ""} ${cursored ? "cursor" : ""}`}
      data-cursor={cursored ? "1" : undefined}
      onClick={() => {
        onCursor();
        onPick(instance);
      }}
    >
      <div className="ctl-inst-top">
        <span style={{ color: instanceDot(instance.status) }} title={instance.status}>
          ▣
        </span>
        <span
          className="ctl-kind"
          style={{ color: instance.kind === "adhoc" ? "var(--teal)" : "var(--blue)" }}
        >
          {instance.kind}
        </span>
        <span className="truncate" style={{ flex: 1 }}>
          {instance.title || instance.task_id || instance.instance_id}
        </span>
        {(instance.pending_permissions ?? 0) > 0 && (
          <span style={{ color: "var(--red)", fontSize: 11.5 }}>
            {instance.pending_permissions}⚠
          </span>
        )}
        {instance.kind === "adhoc" && (
          <span
            title="Kill instance"
            style={{ color: "var(--red)", cursor: "pointer" }}
            onClick={(e) => {
              e.stopPropagation();
              onKill(instance);
            }}
          >
            ✕
          </span>
        )}
      </div>
      {meta.length > 0 && (
        <div className="ctl-inst-meta">
          {meta.map((m) => (
            <span key={m.label} className="ctl-meta-chip" title={m.title ?? `${m.label}: ${m.value}`}>
              <span className="ctl-meta-label">{m.label}</span>
              <span className="truncate">{m.value}</span>
            </span>
          ))}
        </div>
      )}
    </div>
  );
}

function compactInstanceMetadata(instance: OpencodeInstance) {
  const items: Array<{ label: string; value: string; title?: string }> = [];
  const add = (label: string, value?: string | number | null, title?: string) => {
    if (value === undefined || value === null || value === "") return;
    items.push({ label, value: String(value), title });
  };

  add("project", instance.project_id);
  add("feature", instance.feature_id);
  add("task", instance.task_id);
  add("priority", instance.priority);
  add("exec", instance.executor);
  add("agent", instance.agent);
  add("model", compactModelName(instance.model), instance.model);
  add("workdir", compactPath(instance.workdir), instance.workdir);
  if ((instance.session_ids?.length ?? 0) > 0) add("sessions", instance.session_ids!.length);
  return items;
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
    // Keep polling until a session is selected — a just-started task's
    // session appears a beat after the instance does.
    refetchInterval: sessionId ? false : 3_000,
    retry: 1,
  });

  const sessions = useMemo(() => {
    const list = sessionsQ.data ?? [];
    return [...list].sort((a, b) => (b.time?.updated ?? 0) - (a.time?.updated ?? 0));
  }, [sessionsQ.data]);

  // Auto-attach to the most-recently-active session so opening a running task
  // drops you straight into its live conversation.
  useEffect(() => {
    if (!sessionId && sessions.length > 0) {
      onSession(sessions[0].id);
    }
  }, [sessionId, sessions, onSession]);

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
        <div className="ctl-pane-meta">
          {compactInstanceMetadata(instance)
            .slice(0, 7)
            .map((m) => (
              <span key={m.label} className="ctl-meta-chip" title={m.title ?? `${m.label}: ${m.value}`}>
                <span className="ctl-meta-label">{m.label}</span>
                <span className="truncate">{m.value}</span>
              </span>
            ))}
        </div>
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
              {sessionName(s).slice(0, 60)}
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
        <Chat
          runnerId={rid}
          instanceId={iid}
          sessionId={sessionId}
          defaultAgent={instance.agent}
          defaultModel={instance.model}
          sessionLabel={
            sessions.find((s) => s.id === sessionId)
              ? sessionName(sessions.find((s) => s.id === sessionId)!)
              : undefined
          }
        />
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
