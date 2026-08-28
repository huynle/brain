/**
 * Brain PWA dashboard page — wireframe-parity port.
 *
 * The shell follows `web/public/wireframes/panes-v2.html` exactly:
 *
 *   <div id="app">
 *     <div class="topbar">...</div>
 *     <div class="sidebar">...</div>
 *     <div class="workspace">...</div>
 *     <div class="statusbar">...</div>
 *
 * The wireframe's CSS is imported verbatim from `styles/global.css`
 * (which IS the wireframe's `panes-v2.css`). Flat classnames — no
 * `.p2-*` scoping. `body.mobile` and `body.sidebar-collapsed` classes
 * drive mobile / sidebar-collapsed layouts.
 */
import { useEffect } from "react";
import { Topbar } from "../components/Topbar";
import { Statusbar } from "../components/Statusbar";
import { Sidebar } from "../components/Sidebar/Sidebar";
import { Workspace } from "../components/Workspace/Workspace";
import { ModalHost } from "../components/Modal/ModalHost";
import { CommandPalette } from "../components/CommandPalette";
import { SidebarDock } from "../components/SidebarDock";
import { AssistantPanel } from "../components/AssistantPanel";
import { MobileNav } from "../components/MobileNav";
import { useWorkspace } from "../store/workspace";
import { useProjects } from "../hooks/useProjects";
import { streams, useLive } from "../lib/sse";
import { useAuth } from "../lib/auth";
import { useIsMobile } from "../hooks/useIsMobile";
import { useGlobalKeyboard } from "../hooks/useGlobalKeyboard";
import { usePauseSync } from "../hooks/usePauseSync";
import { Loading } from "../components/common/Loading";
import { ErrorState } from "../components/common/ErrorState";

export function Dashboard(): JSX.Element {
  const sidebarCollapsed = useWorkspace((s) => s.sidebarCollapsed);
  const setStreaming = useWorkspace((s) => s.setStreaming);
  const theme = useWorkspace((s) => s.theme);
  const sidebarDockOpen = useWorkspace((s) => s.sidebarDockOpen);
  const drawerWidth = useWorkspace((s) => s.drawerWidth);
  const sidebarWidth = useWorkspace((s) => s.sidebarWidth);
  const isMobile = useIsMobile();

  const { data: projects, isLoading, error, refetch } = useProjects();
  const token = useAuth((s) => s.token);

  const anyConnected = useLive((s) =>
    Object.values(s.projects).some((p) => p.connected),
  );

  // Apply theme to body (dark/light/system).
  useEffect(() => {
    const resolve = () => {
      if (theme === "system") {
        const dark =
          typeof window !== "undefined" &&
          window.matchMedia("(prefers-color-scheme: dark)").matches;
        document.body.setAttribute("data-theme", dark ? "dark" : "light");
      } else {
        document.body.setAttribute("data-theme", theme);
      }
    };
    resolve();
    if (theme !== "system") return;
    if (typeof window === "undefined") return;
    const mql = window.matchMedia("(prefers-color-scheme: dark)");
    mql.addEventListener("change", resolve);
    return () => mql.removeEventListener("change", resolve);
  }, [theme]);

  // Reflect mobile + sidebar-collapsed states on body for CSS to hook.
  useEffect(() => {
    document.body.classList.toggle("mobile", isMobile);
    return () => {
      document.body.classList.remove("mobile");
    };
  }, [isMobile]);

  useEffect(() => {
    document.body.classList.toggle("sidebar-collapsed", sidebarCollapsed);
    return () => {
      document.body.classList.remove("sidebar-collapsed");
    };
  }, [sidebarCollapsed]);

  // Drives the desktop drawer-as-grid-column layout (see `body.drawer-open
  // #app` in global.css). Scoped to desktop by CSS (`:not(.mobile)`) —
  // mobile keeps its fixed-overlay portal regardless of this class.
  // `sidebarDockOpen` is the panel's own visibility gate (see
  // SidebarDock.tsx / store/workspace.ts) — it flips true automatically
  // when something is opened into the sidebar dock, and the user can
  // pin it open/closed manually via the topbar toggle.
  useEffect(() => {
    document.body.classList.toggle("drawer-open", sidebarDockOpen);
    return () => {
      document.body.classList.remove("drawer-open");
    };
  }, [sidebarDockOpen]);

  // Global keyboard shortcuts (⌘K palette, ⌘/ help, etc.)
  useGlobalKeyboard();

  // Single owner of the pause / scheduler polling. Every pause indicator in
  // the tree reads the same cache entries without adding a timer — see the
  // per-observer note in usePauseState.
  usePauseSync();

  useEffect(() => {
    if (!projects) return;
    streams.sync(projects);
  }, [projects]);
  useEffect(() => () => streams.stopAll(), []);
  useEffect(() => {
    if (token) streams.restartAll();
  }, [token]);
  useEffect(() => {
    setStreaming(anyConnected);
  }, [anyConnected, setStreaming]);

  if (isLoading) {
    return <Loading label="Loading projects…" />;
  }
  if (error) {
    return <ErrorState error={error} onRetry={() => void refetch()} />;
  }

  // `--sidebar-w` / `--drawer-w` drive #app's grid-template-columns
  // (see global.css). Set here, on #app itself, so both the grid
  // columns AND the drawer's own width (inherited by the <aside> that
  // Dashboard mounts as a grid child below) read the same values. On
  // mobile the drawer instead portals to document.body and carries its
  // own copy of `--drawer-w` inline (see SidebarDock.tsx) — these
  // vars are harmless there since #app's mobile grid never references
  // them.
  const appStyle = {
    ["--sidebar-w" as never]: `${sidebarWidth}px`,
    ["--drawer-w" as never]: `${drawerWidth}px`,
  } as React.CSSProperties;

  return (
    <>
      <div id="app" style={appStyle}>
        <Topbar />
        <Sidebar />
        {isMobile && <MobileNav />}
        <Workspace />
        <Statusbar />
        {/* Direct child of #app so `grid-area: drawer` applies on
         * desktop. On mobile SidebarDock portals itself to
         * document.body instead — see SidebarDock.tsx. */}
        <SidebarDock />
      </div>
      <ModalHost />
      <CommandPalette />
      <AssistantPanel />
    </>
  );
}
