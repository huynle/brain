import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { registerSW } from "virtual:pwa-register";
import { App } from "./App";
import { useUI } from "./store/ui";
import "./styles/global.css";

// Register the service worker and poll for new builds (every 30s) so a long-open
// tab notices a new release without a manual hard-refresh. registerType:"prompt"
// holds the new SW in "waiting" and fires onNeedRefresh; we surface an
// "Update available — Reload" banner that applies it on click (updateSW(true)
// skips waiting and reloads). This avoids serving a stale cached UI.
const updateSW = registerSW({
  immediate: true,
  onRegisteredSW(_swUrl, registration) {
    if (!registration) return;
    setInterval(() => {
      registration.update().catch(() => {});
    }, 30_000);
  },
  onNeedRefresh() {
    useUI.getState().setUpdateApply(async () => {
      const fallback = window.setTimeout(() => window.location.reload(), 1500);
      try {
        await updateSW(true);
      } finally {
        window.clearTimeout(fallback);
        window.location.reload();
      }
    });
  },
});

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 10_000,
      refetchOnWindowFocus: true,
      retry: 1,
    },
  },
});

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </QueryClientProvider>
  </React.StrictMode>,
);
