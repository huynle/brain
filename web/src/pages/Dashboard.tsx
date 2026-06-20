import { useEffect, useRef, useState } from "react";
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
import { commandSuggestions, resolveCommand } from "../lib/commands";
import { useGlobalKeyboard } from "../lib/keyboard";
import { useIsMobile } from "../hooks/useIsMobile";
import { useSwipe } from "../hooks/useSwipe";
import { StatusBar } from "../components/layout/StatusBar";
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

export function Dashboard() {
  const view = useUI((s) => s.view);
  const cycleView = useUI((s) => s.cycleView);
  const isMobile = useIsMobile();
  const activeProject = useUI((s) => s.activeProject);
  const settingsOpen = useUI((s) => s.settingsOpen);
  const [assistantOpen, setAssistantOpen] = useState(false);
  const setSettingsOpen = useUI((s) => s.setSettingsOpen);
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

  async function runPause(label: string, fn: () => Promise<unknown>) {
    try {
      await fn();
      toast(label, "success");
      void qc.invalidateQueries({ queryKey: ["runner-status"] });
    } catch (e) {
      toast(e instanceof Error ? e.message : "Action failed", "error");
    }
  }

  // Swipe-to-cycle-tabs handlers (used on mobile). Declared unconditionally
  // (hook) — applied conditionally in the render.
  const swipe = useSwipe({ onLeft: () => cycleView(1), onRight: () => cycleView(-1) });

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
  });

  if (isLoading) return <Loading label="Loading projects…" />;
  if (error) return <ErrorState error={error} onRetry={() => void refetch()} />;

  // On touch devices, swipe left/right cycles content tabs.
  const swipeProps = isMobile ? swipe : {};

  return (
    <div className={`tui ${isMobile ? "mobile" : ""}`}>
      <StatusBar onAssistant={() => setAssistantOpen(true)} />
      {!isMobile && <ContentTabs />}
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
      {isMobile ? <MobileNav onAssistant={() => setAssistantOpen(true)} /> : <HelpBar />}
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
      <AssistantDrawer open={assistantOpen} onClose={() => setAssistantOpen(false)} />
    </div>
  );
}

function CommandBar({ onClose }: { onClose: () => void }) {
  const [value, setValue] = useState(":");
  const inputRef = useRef<HTMLInputElement>(null);
  const setView = useUI((s) => s.setView);
  const setProjectSheetOpen = useUI((s) => s.setProjectSheetOpen);
  const toast = useUI((s) => s.toast);
  const suggestions = commandSuggestions(value).slice(0, 6);

  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.setSelectionRange(value.length, value.length);
  }, [value.length]);

  function submit() {
    const result = resolveCommand(value);
    if (result.type === "view") {
      setView(result.view);
      onClose();
      return;
    }
    if (result.type === "projectPicker") {
      setProjectSheetOpen(true);
      onClose();
      return;
    }
    toast(result.suggestions.length ? `Try :${result.suggestions[0]}` : `Unknown command ${value}`, "error");
  }

  return (
    <div className="command-bar" role="dialog" aria-label="Command">
      <span className="command-prompt">:</span>
      <input
        ref={inputRef}
        value={value.replace(/^:/, "")}
        onChange={(e) => setValue(`:${e.currentTarget.value}`)}
        onKeyDown={(e) => {
          if (e.key === "Escape") onClose();
          if (e.key === "Enter") submit();
        }}
        placeholder="tasks, brain, automations, runners, logs, projects"
      />
      {suggestions.length > 0 && (
        <div className="command-suggestions">
          {suggestions.map((suggestion) => (
            <button key={suggestion} type="button" onClick={() => setValue(`:${suggestion}`)}>
              :{suggestion}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
