import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useProjects } from "../hooks/useProjects";
import {
  getRunnerStatus,
  pauseAll,
  pauseProject,
  resumeAll,
  resumeProject,
} from "../lib/api";
import { streams } from "../lib/sse";
import { useAuth } from "../lib/auth";
import { useUI, ALL_PROJECTS } from "../store/ui";
import { useNav } from "../store/nav";
import { useGlobalKeyboard } from "../lib/keyboard";
import { StatusBar } from "../components/layout/StatusBar";
import { ContentTabs } from "../components/layout/ContentTabs";
import { HelpBar } from "../components/layout/HelpBar";
import { Panel } from "../components/layout/Panel";
import { HelpModal } from "../components/common/HelpModal";
import { ErrorState, Loading } from "../components/common/states";
import { TasksView } from "../views/TasksView";
import { BrainView } from "../views/BrainView";
import { AutomationsView } from "../views/AutomationsView";
import { RunnersView } from "../views/RunnersView";
import { ControlView } from "../views/control/ControlView";
import { LogsView } from "../views/LogsView";
import { SettingsSheet } from "../views/SettingsSheet";

export function Dashboard() {
  const view = useUI((s) => s.view);
  const activeProject = useUI((s) => s.activeProject);
  const settingsOpen = useUI((s) => s.settingsOpen);
  const setSettingsOpen = useUI((s) => s.setSettingsOpen);
  const token = useAuth((s) => s.token);
  const helpOpen = useNav((s) => s.helpOpen);
  const setHelpOpen = useNav((s) => s.setHelpOpen);
  const toast = useUI((s) => s.toast);
  const qc = useQueryClient();
  const { data: projects, isLoading, error, refetch } = useProjects();
  const statusQ = useQuery({
    queryKey: ["runner-status"],
    queryFn: getRunnerStatus,
    refetchInterval: 12_000,
  });

  useEffect(() => {
    if (!projects) return;
    streams.sync(projects);
    return () => streams.stopAll();
  }, [projects]);

  useEffect(() => {
    if (token) streams.restartAll();
  }, [token]);

  async function runPause(label: string, fn: () => Promise<unknown>) {
    try {
      await fn();
      toast(label, "success");
      void qc.invalidateQueries({ queryKey: ["runner-status"] });
    } catch (e) {
      toast(e instanceof Error ? e.message : "Action failed", "error");
    }
  }

  useGlobalKeyboard({
    projects: projects ?? [],
    allLabel: ALL_PROJECTS,
    onRefresh: () => {
      streams.restartAll();
      void qc.invalidateQueries();
      toast("Reconnecting…");
    },
    onPauseToggle: () => {
      const st = statusQ.data;
      if (activeProject === ALL_PROJECTS) {
        void runPause(st?.paused ? "Resumed all" : "Paused all", st?.paused ? resumeAll : pauseAll);
        return;
      }
      const paused = st?.pausedProjects?.includes(activeProject);
      void runPause(
        paused ? `Resumed ${activeProject}` : `Paused ${activeProject}`,
        () => (paused ? resumeProject(activeProject) : pauseProject(activeProject)),
      );
    },
    onPauseAll: () => {
      void runPause(statusQ.data?.paused ? "Resumed all" : "Paused all", statusQ.data?.paused ? resumeAll : pauseAll);
    },
  });

  if (isLoading) return <Loading label="Loading projects…" />;
  if (error) return <ErrorState error={error} onRetry={() => void refetch()} />;

  return (
    <div className="tui">
      <StatusBar />
      <ContentTabs />
      <div className="tui-main">
        {/* Top-level views render without a panel title — the active content
            tab already names them, so a titled border just duplicated it. */}
        {view === "tasks" && <TasksView />}
        {/* Brain & Automations manage their own list+detail layout (parity
            with Tasks), so they render without an outer panel. */}
        {view === "brain" && <BrainView />}
        {view === "automations" && <AutomationsView />}
        {view === "runners" && (
          <Panel focused style={{ flex: 1 }}>
            <RunnersView />
          </Panel>
        )}
        {view === "control" && (
          <Panel focused style={{ flex: 1 }}>
            <ControlView />
          </Panel>
        )}
        {view === "logs" && (
          <Panel focused style={{ flex: 1 }}>
            <LogsView />
          </Panel>
        )}
      </div>
      <HelpBar />
      {settingsOpen && <SettingsSheet onClose={() => setSettingsOpen(false)} />}
      {helpOpen && <HelpModal onClose={() => setHelpOpen(false)} />}
    </div>
  );
}
