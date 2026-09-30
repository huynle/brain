import { useMobileViewport } from "./hooks/useMobileViewport";
import { useIsMobile } from "./hooks/useIsMobile";
import { OfflineSync } from "./components/common/OfflineSync";
import { BulkJobs } from "./components/common/BulkJobs";
import { useEffect } from "react";
import { Route, Routes } from "react-router-dom";
import { useAuth } from "./lib/auth";
import { Loading } from "./components/common/Loading";
import { BackgroundOperations } from "./components/common/BackgroundOperations";
import { ReminderStatusPopups } from "./components/common/ReminderStatusPopups";
import { Toasts } from "./components/common/Toasts";
import { UpdateBanner } from "./components/common/UpdateBanner";
import { Login } from "./pages/Login";
import { AuthCallback } from "./pages/AuthCallback";
import { Dashboard } from "./pages/Dashboard";
import { useWorkspace } from "./store/workspace";

export function App() {
  const mobile = useIsMobile();
  useMobileViewport(mobile);
  useEffect(() => {
    document.body.classList.toggle("mobile", mobile);
    return () => document.body.classList.remove("mobile");
  }, [mobile]);
  const status = useAuth((s) => s.status);
  const init = useAuth((s) => s.init);

  useEffect(() => {
    void init();
  }, [init]);

  return (
    <>
      <Routes>
        <Route path="/auth/callback" element={<AuthCallback />} />
        <Route path="*" element={<Gate status={status} />} />
      </Routes>
      <Toasts />
      {mobile ? (
        <details className="mobile-activity">
          <summary>Activity</summary>
          <div className="background-operation-tray">
            <ReminderStatusPopups />
            <BackgroundOperations />
            <BulkJobs />
          </div>
        </details>
      ) : (
        <div className="background-operation-tray">
          <ReminderStatusPopups />
          <BackgroundOperations />
          <BulkJobs />
        </div>
      )}
      <UpdateBanner />
      <OfflineSync />
    </>
  );
}

function Gate({
  status,
}: {
  status: ReturnType<typeof useAuth.getState>["status"];
}) {
  useEffect(() => {
    if (status === "loading" || status === "needs-login") return;
    const open = (target: string | null) => {
      if (target === "assistant") useWorkspace.getState().setAssistantOpen(true);
      if (target === "reminders") useWorkspace.getState().openInFocus("reminders", {}, "Reminders");
    };
    const url = new URL(location.href);
    const target = url.searchParams.get("notification");
    if (target) {open(target); url.searchParams.delete("notification"); history.replaceState(null, "", url);}
    const receive = (event: MessageEvent) => {if (event.data?.type === "brain-notification") open(event.data.target);};
    navigator.serviceWorker?.addEventListener("message", receive);
    return () => navigator.serviceWorker?.removeEventListener("message", receive);
  }, [status]);
  if (status === "loading") return <Loading label="Connecting to Brain…" />;
  if (status === "needs-login") return <Login />;
  // Authenticated. Panes-v2 is the default and only dashboard as of Phase 9.
  // Any path renders the Dashboard — deep-links from earlier builds still work.
  return <Dashboard />;
}
