import { useEffect, useState } from "react";
import { useProjects } from "../hooks/useProjects";
import { streams } from "../lib/sse";
import { useAuth } from "../lib/auth";
import { useUI } from "../store/ui";
import { Header } from "../components/layout/Header";
import { BottomNav } from "../components/layout/BottomNav";
import { ErrorState, Loading } from "../components/common/states";
import { TasksView } from "../views/TasksView";
import { BrainView } from "../views/BrainView";
import { AutomationsView } from "../views/AutomationsView";
import { RunnersView } from "../views/RunnersView";
import { LogsView } from "../views/LogsView";
import { SettingsSheet } from "../views/SettingsSheet";

export function Dashboard() {
  const view = useUI((s) => s.view);
  const token = useAuth((s) => s.token);
  const { data: projects, isLoading, error, refetch } = useProjects();
  const [settingsOpen, setSettingsOpen] = useState(false);

  // Open/refresh SSE streams whenever the project set or token changes.
  useEffect(() => {
    if (!projects) return;
    streams.sync(projects);
    return () => streams.stopAll();
  }, [projects]);

  useEffect(() => {
    // Token rotated (login / refresh) — reconnect with the new credential.
    if (token) streams.restartAll();
  }, [token]);

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
    </div>
  );
}
