import { useEffect } from "react";
import { Route, Routes } from "react-router-dom";
import { useAuth } from "./lib/auth";
import { Loading } from "./components/common/states";
import { Toasts } from "./components/common/Toasts";
import { Login } from "./pages/Login";
import { AuthCallback } from "./pages/AuthCallback";
import { Dashboard } from "./pages/Dashboard";

export function App() {
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
    </>
  );
}

function Gate({ status }: { status: ReturnType<typeof useAuth.getState>["status"] }) {
  if (status === "loading") return <Loading label="Connecting to Brain…" />;
  if (status === "needs-login") return <Login />;
  return <Dashboard />;
}
