import { useState } from "react";
import { useAuth } from "../lib/auth";

export function Login() {
  const beginLogin = useAuth((s) => s.beginLogin);
  const setManualToken = useAuth((s) => s.setManualToken);
  const error = useAuth((s) => s.error);
  const [manual, setManual] = useState(false);
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);

  return (
    <div className="app">
      <div
        className="center-state"
        style={{ flex: 1, justifyContent: "center" }}
      >
        <img
          src="/icons/pwa-192x192.png"
          width={84}
          height={84}
          alt="Brain"
          style={{ borderRadius: 20 }}
        />
        <h1 style={{ margin: "0.3rem 0 0", color: "var(--fg)" }}>Brain</h1>
        <p className="muted" style={{ maxWidth: 320 }}>
          Sign in to view tasks, automations, runners, and your knowledge base.
        </p>

        {error && (
          <div
            className="toast error"
            style={{ position: "static", maxWidth: 360 }}
          >
            {error}
          </div>
        )}

        {!manual ? (
          <div className="col" style={{ width: "min(92%, 340px)" }}>
            <button
              className="btn primary"
              onClick={() => {
                setBusy(true);
                void beginLogin().finally(() => setBusy(false));
              }}
              disabled={busy}
            >
              {busy ? "Redirecting…" : "Sign in with PIN"}
            </button>
            <button className="btn ghost" onClick={() => setManual(true)}>
              Use an API token instead
            </button>
          </div>
        ) : (
          <form
            className="col"
            style={{ width: "min(92%, 340px)" }}
            onSubmit={(e) => {
              e.preventDefault();
              if (token.trim()) setManualToken(token.trim());
            }}
          >
            <input
              type="password"
              placeholder="Paste API token"
              value={token}
              autoFocus
              onChange={(e) => setToken(e.target.value)}
            />
            <button className="btn primary" type="submit" disabled={!token.trim()}>
              Save token
            </button>
            <button
              className="btn ghost"
              type="button"
              onClick={() => setManual(false)}
            >
              Back
            </button>
          </form>
        )}
      </div>
    </div>
  );
}
