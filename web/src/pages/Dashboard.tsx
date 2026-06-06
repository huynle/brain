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
import { Header } from "../components/layout/Header";
import { BottomNav } from "../components/layout/BottomNav";
import { HelpModal } from "../components/common/HelpModal";
import { ErrorState, Loading } from "../components/common/states";
import { TasksView } from "../views/TasksView";
import { BrainView } from "../views/BrainView";
import { AutomationsView } from "../views/AutomationsView";
import { RunnersView } from "../views/RunnersView";
import { LogsView } from "../views/LogsView";
import { SettingsSheet } from "../views/SettingsSheet";

export function Dashboard() {
  const view = useUI((s) => s.view);
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

  // Open/refresh SSE streams whenever the project set or token changes.
  useEffect(() => {
    if (!projects) return;
    streams.sync(projects);
    return () => streams.stopAll();
  }, [projects]);

  useEffect(() => {
    if (token) streams.restartAll();
  }, [token]);

  const activeProject = useUI((s) => s.activeProject);
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
    <div className="app">
      <Header
        projects={projects ?? []}
        onOpenSettings={() => setSettingsOpen(true)}
      />
      <main className="app-main">
        {view === "tasks" && <TasksView />}
        {view === "brain" && <BrainView />}
        {view === "automations" && <AutomationsView />}
        {view === "runners" && <RunnersView />}
        {view === "logs" && <LogsView />}
      </main>
      <BottomNav />
      {settingsOpen && <SettingsSheet onClose={() => setSettingsOpen(false)} />}
      {helpOpen && <HelpModal onClose={() => setHelpOpen(false)} />}
    </div>
  );
}
