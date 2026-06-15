import { useState } from "react";
import { useUI } from "../../store/ui";

// Shown when a new PWA build is waiting (service worker "needs refresh"). Clicking
// Reload applies the waiting service worker and reloads, so the open tab picks up
// the new UI without a manual hard-refresh.
export function UpdateBanner() {
  const updateApply = useUI((s) => s.updateApply);
  const setUpdateApply = useUI((s) => s.setUpdateApply);
  const [applying, setApplying] = useState(false);

  if (!updateApply) return null;

  return (
    <div className="update-banner" role="alert">
      <span className="update-banner-text">A new version of Brain is available.</span>
      <button
        className="btn sm primary"
        disabled={applying}
        onClick={() => {
          setApplying(true);
          updateApply();
        }}
      >
        {applying ? "Updating…" : "Reload"}
      </button>
      <button
        className="btn sm ghost"
        onClick={() => setUpdateApply(null)}
        aria-label="Dismiss"
        title="Dismiss"
      >
        ✕
      </button>
    </div>
  );
}
