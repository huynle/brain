import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useProjects } from "../hooks/useProjects";
import { getRunnerStatus } from "../lib/api";
import { applyPause } from "../lib/pauseActions";
import { streams } from "../lib/sse";
import { useAuth } from "../lib/auth";
import { useUI, ALL_PROJECTS } from "../store/ui";
import { useNav } from "../store/nav";
import { useGlobalKeyboard } from "../lib/keyboard";
import { CommandBar } from "../components/layout/CommandBar";
import { useIsMobile } from "../hooks/useIsMobile";
import { useViewport } from "../hooks/useViewport";
import { useSwipe } from "../hooks/useSwipe";
import { StatusBar, deriveTaskPaused } from "../components/layout/StatusBar";
import { ContentTabs } from "../components/layout/ContentTabs";
import { HelpBar } from "../components/layout/HelpBar";
import { MobileNav } from "../components/layout/MobileNav";
import { ProjectSheet } from "../components/layout/ProjectSheet";
import { MobileInspectSheet } from "../components/layout/MobileInspectSheet";
import { Panel } from "../components/layout/Panel";
import { HelpModal } from "../components/common/HelpModal";
import { ErrorState, Loading } from "../components/common/states";
import { TasksView } from "../views/TasksView";
import { BrainView } from "../views/BrainView";
import { AutomationsView } from "../views/AutomationsView";
import { RunnersView } from "../views/RunnersView";
import { LogsView } from "../views/LogsView";
import { SettingsSheet } from "../views/SettingsSheet";
import { AssistantDrawer } from "../views/AssistantDrawer";
import { AssistantSidebar } from "../views/AssistantSidebar";
import { AssistantFAB } from "../components/layout/AssistantFAB";

export function Dashboard() {
  const view = useUI((s) => s.view);
  const cycleView = useUI((s) => s.cycleView);
  const isMobile = useIsMobile();
  const activeProject = useUI((s) => s.activeProject);
  const settingsOpen = useUI((s) => s.settingsOpen);
  const setAssistantOpen = useUI((s) => s.setAssistantOpen);
  const sidebarVisible = useUI((s) => s.assistantSidebar);
  const setSidebarVisible = useUI((s) => s.setAssistantSidebar);
  const setSettingsOpen = useUI((s) => s.setSettingsOpen);
  const focusAssistantPrompt = useUI((s) => s.focusAssistantPrompt);
  const token = useAuth((s) => s.token);
  const helpOpen = useNav((s) => s.helpOpen);
  const commandOpen = useNav((s) => s.commandOpen);
  const setHelpOpen = useNav((s) => s.setHelpOpen);
  const setCommandOpen = useNav((s) => s.setCommandOpen);
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
    // Keep per-project views lightweight. Opening one EventSource per project
    // can saturate the browser's per-origin connection pool and starve normal
    // fetches (for example the Automations tab) until streams close.
    streams.sync(activeProject === ALL_PROJECTS ? projects : [activeProject]);
  }, [projects, activeProject]);

  // Tear down all streams only on unmount.
  useEffect(() => () => streams.stopAll(), []);

  useEffect(() => {
    if (token) streams.restartAll();
  }, [token]);



  // Swipe-to-cycle-tabs handlers (used on mobile). Declared unconditionally
  // (hook) — applied conditionally in the render.
  const swipe = useSwipe({ onLeft: () => cycleView(1), onRight: () => cycleView(-1) });

  // Smart assistant toggle: on wide desktop the assistant lives as a
  // persistent right sidebar (toggle visibility), on narrow desktop / mobile
  // it's an overlay (toggle the open flag).
  const tier = useViewport();
  function toggleAssistant() {
    if (tier === "wide") {
      setSidebarVisible(!sidebarVisible);
      if (!sidebarVisible) window.setTimeout(focusAssistantPrompt, 0);
      return;
    }
    setAssistantOpen(true);
    window.setTimeout(focusAssistantPrompt, 0);
  }

  useGlobalKeyboard({
    projects: projects ?? [],
    allLabel: ALL_PROJECTS,
    onRefresh: () => {
      streams.restartAll();
      void qc.invalidateQueries();
      toast("Reconnecting…");
    },
    // p — tasks pause for the active project (global when on the All tab);
    // P — tasks pause for ALL projects (this used to duplicate p — fixed);
    // b/B — the automations equivalents. All optimistic via pauseActions.
    onPauseToggle: () => {
      const project = activeProject === ALL_PROJECTS ? undefined : activeProject;
      const paused = project ? deriveTaskPaused(statusQ.data, activeProject) : statusQ.data?.paused;
      if (paused === undefined) return;
      void applyPause(qc, { kind: "tasks", project, pause: !paused }, toast);
    },
    onPauseAll: () => {
      const paused = statusQ.data?.paused;
      if (paused === undefined) return;
      void applyPause(qc, { kind: "tasks", pause: !paused }, toast);
    },
    onPauseAutosToggle: () => {
      const project = activeProject === ALL_PROJECTS ? undefined : activeProject;
      const paused = project
        ? (statusQ.data?.automationPausedProjects ?? []).includes(activeProject)
        : statusQ.data?.automationsPaused;
      if (paused === undefined) return;
      void applyPause(qc, { kind: "autos", project, pause: !paused }, toast);
    },
    onPauseAutosAll: () => {
      const paused = statusQ.data?.automationsPaused;
      if (paused === undefined) return;
      void applyPause(qc, { kind: "autos", pause: !paused }, toast);
    },
  });

  if (isLoading) return <Loading label="Loading projects…" />;
  if (error) return <ErrorState error={error} onRetry={() => void refetch()} />;

  // On touch devices, swipe left/right cycles content tabs.
  const swipeProps = isMobile ? swipe : {};

  return (
    <div className={`tui ${isMobile ? "mobile" : ""}`}>
      <StatusBar onAssistant={toggleAssistant} />
      {!isMobile && <ContentTabs />}
      <div className="tui-body">
        <div className="tui-main" {...swipeProps}>
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
          {view === "logs" && (
            <Panel focused style={{ flex: 1 }}>
              <LogsView />
            </Panel>
          )}
        </div>
        {/* Persistent right-side assistant pane on wide desktop viewports.
            Returns null on mobile / narrow desktops, where the AssistantDrawer
            overlay is used instead. */}
        <AssistantSidebar />
      </div>
      {isMobile ? <MobileNav /> : <HelpBar />}
      {commandOpen && (
        <div className="command-layer" onMouseDown={(e) => { if (e.target === e.currentTarget) setCommandOpen(false); }}>
          <CommandBar onClose={() => setCommandOpen(false)} />
        </div>
      )}
      {/* Searchable project picker — available on desktop and mobile. */}
      <ProjectSheet />
      {isMobile && <MobileInspectSheet />}
      {settingsOpen && <SettingsSheet onClose={() => setSettingsOpen(false)} />}
      {helpOpen && <HelpModal onClose={() => setHelpOpen(false)} />}
      <AssistantDrawer />
      <AssistantFAB />
    </div>
  );
}

