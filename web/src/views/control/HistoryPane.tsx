// Read-only review of a completed session, plus a resume affordance.
//
// A finished task's OpenCode instance is gone, but its session transcript lives
// on disk on the machine that ran it. This pane resolves a connected runner on
// that machine, fetches the transcript via the history endpoint (the backend
// serves it from a live instance or reads it from storage), and renders it
// read-only. "Resume" spawns a fresh server in the recorded workdir and hands
// off to the live chat.

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { controlSessionHistory } from "../../lib/api";
import { EmptyState, Loading } from "../../components/common/states";
import type { ControlTarget } from "../../store/ui";
import type { OcMessage, RunnerInfo } from "../../lib/types";
import { MessageRow } from "./Chat";

// resolveServingRunner picks a bridge-connected runner that can serve the
// session: the recorded runner if it's back online, else any connected runner
// on the same machine (they share OpenCode's on-disk storage).
export function resolveServingRunner(
  runners: RunnerInfo[],
  target: ControlTarget,
): RunnerInfo | null {
  const exact = runners.find((r) => r.runner_id === target.runnerId);
  if (exact?.bridge_connected) return exact;
  if (target.machineId) {
    const sameMachine = runners.find(
      (r) => r.machine_id === target.machineId && r.bridge_connected,
    );
    if (sameMachine) return sameMachine;
  }
  return null;
}

export function HistoryPane({
  target,
  runners,
  onBack,
  onResume,
}: {
  target: ControlTarget;
  runners: RunnerInfo[];
  onBack: () => void;
  onResume: (runnerId: string) => void;
}) {
  const serving = useMemo(() => resolveServingRunner(runners, target), [runners, target]);
  const sessionId = target.sessionId ?? "";

  const historyQ = useQuery({
    queryKey: ["control-history", serving?.runner_id, sessionId],
    queryFn: () => controlSessionHistory(serving!.runner_id, sessionId),
    enabled: !!serving && !!sessionId,
    retry: 1,
  });

  const messages = useMemo<OcMessage[]>(
    () => (historyQ.data ?? []).filter((m) => !!m?.info),
    [historyQ.data],
  );

  const host = target.hostname || serving?.hostname || "another machine";

  return (
    <div className="ctl-pane">
      <div className="row ctl-pane-head" style={{ gap: 8 }}>
        <button className="btn sm ghost ctl-back" onClick={onBack}>
          ← back
        </button>
        <span style={{ color: "var(--blue)" }}>⊙</span>
        <strong className="truncate">{target.taskTitle || "Session review"}</strong>
        <span className="mono faint truncate" style={{ fontSize: 11.5 }}>
          {sessionId}
        </span>
        <span style={{ flex: 1 }} />
        {serving && (
          <span className="faint" style={{ fontSize: 11.5 }}>
            read-only · {serving.hostname}
          </span>
        )}
        {serving && target.workdir && (
          <button
            className="btn sm"
            title={`Resume this session on ${serving.hostname} (${target.workdir})`}
            onClick={() => onResume(serving.runner_id)}
          >
            ▶ resume
          </button>
        )}
      </div>

      {!serving ? (
        <div className="ctl-history-notice">
          <div style={{ fontSize: 28, marginBottom: 8 }}>⊘</div>
          <div style={{ fontWeight: 600, marginBottom: 4 }}>
            Session ran on {host}
          </div>
          <div className="faint" style={{ maxWidth: 420, lineHeight: 1.5 }}>
            That runner isn't connected, so its session storage can't be reached.
            Start a runner on {host} (<span className="mono">brain start &lt;project&gt;</span>)
            to review or resume this session.
          </div>
        </div>
      ) : !sessionId ? (
        <EmptyState glyph="◌" title="No session id recorded" hint="" />
      ) : historyQ.isLoading ? (
        <Loading label="Loading session transcript…" />
      ) : historyQ.error ? (
        <div className="ctl-history-notice">
          <div style={{ fontWeight: 600, marginBottom: 4 }}>Couldn't load this session</div>
          <div className="faint" style={{ maxWidth: 420 }}>
            {String((historyQ.error as Error).message)}
          </div>
        </div>
      ) : (
        <div className="ctl-chat">
          <div className="ctl-chat-scroll">
            {messages.length === 0 ? (
              <div className="faint" style={{ padding: 12 }}>
                No messages recorded for this session.
              </div>
            ) : (
              messages.map((m) => <MessageRow key={m.info.id} message={m} />)
            )}
          </div>
        </div>
      )}
    </div>
  );
}
