import { useQuery } from "@tanstack/react-query";
import { Modal } from "../components/common/Modal";
import { Pill } from "../components/common/Badge";
import { getHealth } from "../lib/api";
import { useAuth } from "../lib/auth";
import { streams } from "../lib/sse";
import { useUI } from "../store/ui";

export function SettingsSheet({ onClose }: { onClose: () => void }) {
  const mode = useAuth((s) => s.mode);
  const logout = useAuth((s) => s.logout);
  const toast = useUI((s) => s.toast);
  const healthQ = useQuery({ queryKey: ["health"], queryFn: getHealth });

  const embedding = healthQ.data?.embedding as
    | { enabled?: boolean; ready?: boolean }
    | undefined;

  return (
    <Modal title="Settings" onClose={onClose}>
      <div className="field">
        <label>Server</label>
        <div className="row" style={{ gap: "0.4rem" }}>
          <Pill
            color={healthQ.data ? "var(--green)" : "var(--red)"}
          >
            {healthQ.isLoading
              ? "checking…"
              : healthQ.data
                ? "connected"
                : "unreachable"}
          </Pill>
          <span className="mono faint" style={{ fontSize: 12 }}>
            {window.location.host}
          </span>
        </div>
      </div>

      {embedding && (
        <div className="field">
          <label>Semantic search</label>
          <Pill
            color={
              !embedding.enabled
                ? "var(--fg-faint)"
                : embedding.ready
                  ? "var(--green)"
                  : "var(--yellow)"
            }
          >
            {!embedding.enabled
              ? "disabled"
              : embedding.ready
                ? "ready"
                : "not ready"}
          </Pill>
        </div>
      )}

      <div className="field">
        <label>Authentication</label>
        <Pill color="var(--blue)">
          {mode === "manual"
            ? "API token"
            : mode === "oauth"
              ? "OAuth (PIN)"
              : "anonymous"}
        </Pill>
      </div>

      <div className="divider" />

      <div className="col">
        <button
          className="btn"
          onClick={() => {
            streams.restartAll();
            toast("Reconnecting live streams…");
          }}
        >
          ⟲ Reconnect live streams
        </button>
        <button
          className="btn"
          onClick={() => {
            window.location.reload();
          }}
        >
          ↻ Reload app
        </button>
        {mode && (
          <button
            className="btn danger"
            onClick={() => {
              logout();
              onClose();
            }}
          >
            Sign out
          </button>
        )}
      </div>

      <div className="faint" style={{ fontSize: 11.5, marginTop: "1rem", textAlign: "center" }}>
        Brain PWA · install via your browser’s “Add to Home Screen”.
      </div>
    </Modal>
  );
}
