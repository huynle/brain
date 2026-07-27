import { useEffect, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useAuth } from "../lib/auth";
import { Loading } from "../components/common/Loading";

export function AuthCallback() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const handleCallback = useAuth((s) => s.handleCallback);
  const [error, setError] = useState<string | null>(null);
  const ran = useRef(false);

  useEffect(() => {
    if (ran.current) return;
    ran.current = true;

    const err = params.get("error");
    if (err) {
      setError(params.get("error_description") || err);
      return;
    }
    const code = params.get("code");
    const state = params.get("state");
    if (!code || !state) {
      setError("Missing authorization code");
      return;
    }
    handleCallback(code, state)
      .then((returnTo) => navigate(returnTo, { replace: true }))
      .catch((e) => setError(e instanceof Error ? e.message : String(e)));
  }, [params, handleCallback, navigate]);

  if (error) {
    return (
      <div className="app">
        <div className="center-state" style={{ flex: 1 }}>
          <div className="big" style={{ color: "var(--red)" }}>
            ⚠
          </div>
          <div style={{ fontWeight: 600 }}>Sign-in failed</div>
          <div className="muted" style={{ maxWidth: 340 }}>
            {error}
          </div>
          <button className="btn primary" onClick={() => navigate("/", { replace: true })}>
            Back to sign in
          </button>
        </div>
      </div>
    );
  }
  return <Loading label="Completing sign-in…" />;
}
