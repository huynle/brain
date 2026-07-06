import { useQuery } from "@tanstack/react-query";
import { useLive } from "../../lib/sse";
import { useNav } from "../../store/nav";
import { ALL_PROJECTS, useUI } from "../../store/ui";
import { useLiveTasks, deriveCounts } from "../../hooks/useLiveTasks";
import { getHealth, getRunnerStatus } from "../../lib/api";
import type { RunnerStatusResponse } from "../../lib/types";

function shortName(id: string): string {
  if (id === ALL_PROJECTS) return "all";
  return id.split(/[/\\]/).pop() || id;
}

// deriveTaskPaused returns the per-project (or global) task-pause state
// the StatusChip wants to render. Returns `undefined` only when the
// runner-status snapshot itself hasn't loaded yet — empty/null per-
// project lists must collapse to `false`, otherwise the chip renders a
// faint "…" even though autos/tasks are clearly running for the project.
//
// The Go API serializes nil slices as JSON `null` (not `[]`), so we
// have to defend against both null AND undefined on the list field.
export function deriveTaskPaused(
  data: RunnerStatusResponse | undefined,
  activeProject: string,
): boolean | undefined {
  if (!data) return undefined;
  if (activeProject === ALL_PROJECTS) return data.paused;
  const list = data.pausedProjects ?? [];
  return list.includes(activeProject);
}

// deriveAutomationsPaused mirrors deriveTaskPaused for automation pause
// state. See that function for the empty-vs-unknown rationale.
export function deriveAutomationsPaused(
  data: RunnerStatusResponse | undefined,
  activeProject: string,
): boolean | undefined {
  if (!data) return undefined;
  if (activeProject === ALL_PROJECTS) return data.automationsPaused;
  const list = data.automationPausedProjects ?? [];
  return list.includes(activeProject);
}

export function StatusBar({ onAssistant }: { onAssistant?: () => void }) {
  const activeProject = useUI((s) => s.activeProject);
  const setProjectSheetOpen = useUI((s) => s.setProjectSheetOpen);
  const runners = useLive((s) => s.runners);
  const selected = useNav((s) => Object.keys(s.selected).length);
  const { tasks, stats, connected } = useLiveTasks(activeProject);
  const { active, completed } = deriveCounts(tasks);

  const activeFeatures = new Set(
    tasks
      .filter((t) => t.status === "in_progress" || t.status === "active")
      .map((t) => t.feature_id)
      .filter(Boolean),
  ).size;

  const statusQ = useQuery({
    queryKey: ["runner-status"],
    queryFn: getRunnerStatus,
    staleTime: 8_000,
  });
  const taskPaused = deriveTaskPaused(statusQ.data, activeProject);
  const automationsPaused = deriveAutomationsPaused(statusQ.data, activeProject);

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
        <button
          className="sb-project sb-project-btn"
          onClick={() => setProjectSheetOpen(true)}
          title="Switch project (⌘; / Ctrl+;, or H/L)"
        >
          {shortName(activeProject)} ▾
        </button>
        {activeFeatures > 0 ? (
          <span style={{ color: "var(--purple)", fontWeight: 700, marginRight: 12 }}>
            ▶{activeFeatures}
          </span>
        ) : null}
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
        <StatusChip
          label="tasks"
          paused={taskPaused}
          runningText="run"
          pausedText="paused"
          title={
            activeProject === ALL_PROJECTS
              ? "task runner pool status"
              : `task runner status for ${activeProject}`
          }
        />
        <StatusChip
          label="autos"
          paused={automationsPaused}
          runningText="on"
          pausedText="off"
          title={
            activeProject === ALL_PROJECTS
              ? "automation runner status"
              : `automation status for ${activeProject}`
          }
        />
        <button className="sb-assistant" onClick={onAssistant} title="Open Brain Assistant">assistant</button>
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

function StatusChip({
  label,
  paused,
  runningText,
  pausedText,
  title,
}: {
  label: string;
  paused: boolean | undefined;
  runningText: string;
  pausedText: string;
  title: string;
}) {
  const known = paused !== undefined;
  const color = !known ? "var(--fg-faint)" : paused ? "var(--red)" : "var(--green)";
  const state = !known ? "…" : paused ? pausedText : runningText;
  return (
    <span className={`sb-state ${paused ? "paused" : "running"}`} title={title}>
      <span style={{ color }}>●</span> {label}:{" "}
      <b style={{ color }}>{state}</b>
    </span>
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
