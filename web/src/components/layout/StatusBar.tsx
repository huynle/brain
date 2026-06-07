import { useQuery } from "@tanstack/react-query";
import { useLive } from "../../lib/sse";
import { useNav } from "../../store/nav";
import { ALL_PROJECTS, useUI } from "../../store/ui";
import { useLiveTasks, deriveCounts } from "../../hooks/useLiveTasks";
import { getHealth } from "../../lib/api";

function shortName(id: string): string {
  if (id === ALL_PROJECTS) return "all";
  return id.split(/[/\\]/).pop() || id;
}

export function StatusBar() {
  const activeProject = useUI((s) => s.activeProject);
  const runners = useLive((s) => s.runners);
  const selected = useNav((s) => Object.keys(s.selected).length);
  const { tasks, stats, connected } = useLiveTasks(activeProject);
  const { active, completed } = deriveCounts(tasks);

  const healthQ = useQuery({ queryKey: ["health"], queryFn: getHealth, staleTime: 30_000 });
  const embedding = healthQ.data?.embedding as
    | { enabled?: boolean; status?: string }
    | undefined;

  const online = runners.filter((r) => r.status === "online").length;
  const stale = runners.filter((r) => r.status === "stale").length;

  const brainColor = connected ? "var(--green)" : "var(--red)";
  const embColor = !connected
    ? "var(--red)"
    : !embedding?.enabled
      ? "var(--fg-faint)"
      : embedding.status === "ready"
        ? "var(--green)"
        : "var(--yellow)";

  return (
    <div className="tui-statusbar">
      <div className="tui-statusrow">
        <span className="sb-project">{shortName(activeProject)}</span>
        <span className="sb-stats">
          <Seg glyph="●" color="var(--green)" n={stats.ready} label="ready" />
          <Seg glyph="○" color="var(--yellow)" n={stats.waiting} label="waiting" />
          <Seg glyph="▶" color="var(--blue)" n={active} label="active" />
          <Seg glyph="✓" color="var(--fg-faint)" n={completed} label="inactive" />
          {stats.blocked > 0 && (
            <Seg glyph="✗" color="var(--red)" n={stats.blocked} label="blocked" />
          )}
          {selected > 0 && (
            <span className="sb-seg" style={{ color: "var(--blue)" }}>
              • <b>{selected}</b> selected
            </span>
          )}
          {runners.length > 0 && (
            <span className="sb-seg faint">
              ⚙ <b>{runners.length}</b> runners
              {online > 0 && (
                <>
                  {" | "}
                  <span style={{ color: "var(--green)" }}>●</span> {online} online
                </>
              )}
              {stale > 0 && (
                <>
                  {" | "}
                  <span style={{ color: "var(--yellow)" }}>●</span> {stale} stale
                </>
              )}
            </span>
          )}
        </span>
        <span className="sb-spacer" />
        <span title={connected ? "brain connected" : "brain offline"}>
          <span style={{ color: brainColor }}>●</span> brain{" "}
        </span>
        <span title="embedding status">
          <span style={{ color: embColor }}>●</span> emb
        </span>
      </div>
    </div>
  );
}

function Seg({
  glyph,
  color,
  n,
  label,
}: {
  glyph: string;
  color: string;
  n: number;
  label: string;
}) {
  return (
    <span className="sb-seg">
      <span style={{ color }}>{glyph}</span> <b>{n}</b> {label}
    </span>
  );
}
