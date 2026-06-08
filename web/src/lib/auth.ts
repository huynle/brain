// OAuth 2.1 (authorization code + PKCE) client for the brain-api server, plus a
// zustand store holding auth state. The server renders its own consent page
// with a PIN field, so "login" is a full-page redirect to /authorize; the user
// enters the OAUTH_PIN there and is redirected back to /auth/callback.
//
// The server's OAuth client/refresh-token store is in-memory, so after a server
// restart a stored refresh token (or client registration) may be rejected. We
// handle that by clearing state and re-running the flow.
//
// Alternatively, a user can paste a long-lived API token (manual mode).

import { create } from "zustand";
import { OAUTH, redirectUri } from "./config";
import { randomString, s256Challenge } from "./pkce";

const LS = {
  accessToken: "brain.access_token",
  refreshToken: "brain.refresh_token",
  expiresAt: "brain.expires_at",
  clientId: "brain.client_id",
  clientSecret: "brain.client_secret",
  mode: "brain.auth_mode", // "oauth" | "manual"
};
const SS = {
  verifier: "brain.pkce_verifier",
  state: "brain.oauth_state",
  returnTo: "brain.return_to",
};

export type AuthStatus =
  | "loading" // checking stored creds / probing server
  | "anonymous" // server has auth disabled; no token needed
  | "authenticated" // we hold a valid token
  | "needs-login"; // server requires auth and we have no valid token

interface AuthState {
  status: AuthStatus;
  token: string | null;
  mode: "oauth" | "manual" | null;
  error: string | null;
  init: () => Promise<void>;
  beginLogin: () => Promise<void>;
  handleCallback: (code: string, state: string) => Promise<string>;
  setManualToken: (token: string) => void;
  logout: () => void;
  /** Mark that the server rejected our token; try refresh, else needs-login. */
  onUnauthorized: () => Promise<boolean>;
  authHeader: () => Record<string, string>;
}

function now(): number {
  return Math.floor(Date.now() / 1000);
}

function storedToken(): { token: string | null; expiresAt: number } {
  return {
    token: localStorage.getItem(LS.accessToken),
    expiresAt: Number(localStorage.getItem(LS.expiresAt) || 0),
  };
}

function saveTokens(t: {
  access_token: string;
  refresh_token?: string;
  expires_in: number;
}) {
  localStorage.setItem(LS.accessToken, t.access_token);
  if (t.refresh_token) localStorage.setItem(LS.refreshToken, t.refresh_token);
  localStorage.setItem(LS.expiresAt, String(now() + (t.expires_in || 3600)));
  localStorage.setItem(LS.mode, "oauth");
}

function clearTokens() {
  localStorage.removeItem(LS.accessToken);
  localStorage.removeItem(LS.refreshToken);
  localStorage.removeItem(LS.expiresAt);
  localStorage.removeItem(LS.mode);
}

async function registerClient(): Promise<{ id: string; secret: string }> {
  const existingId = localStorage.getItem(LS.clientId);
  const existingSecret = localStorage.getItem(LS.clientSecret);
  if (existingId) {
    return { id: existingId, secret: existingSecret || "" };
  }
  const res = await fetch(OAUTH.registerEndpoint, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      client_name: OAUTH.clientName,
      redirect_uris: [redirectUri()],
      grant_types: ["authorization_code", "refresh_token"],
      response_types: ["code"],
      scope: OAUTH.scope,
    }),
  });
  if (!res.ok) {
    throw new Error(`client registration failed (${res.status})`);
  }
  const data = await res.json();
  localStorage.setItem(LS.clientId, data.client_id);
  if (data.client_secret) localStorage.setItem(LS.clientSecret, data.client_secret);
  return { id: data.client_id, secret: data.client_secret || "" };
}

function clearClient() {
  localStorage.removeItem(LS.clientId);
  localStorage.removeItem(LS.clientSecret);
}

async function exchangeRefresh(): Promise<boolean> {
  const refresh = localStorage.getItem(LS.refreshToken);
  const clientId = localStorage.getItem(LS.clientId);
  if (!refresh || !clientId) return false;
  const body = new URLSearchParams({
    grant_type: "refresh_token",
    refresh_token: refresh,
    client_id: clientId,
  });
  const res = await fetch(OAUTH.tokenEndpoint, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body,
  });
  if (!res.ok) return false;
  const data = await res.json();
  saveTokens(data);
  return true;
}

export const useAuth = create<AuthState>((set, get) => ({
  status: "loading",
  token: null,
  mode: null,
  error: null,

  authHeader() {
    const t = get().token;
    const h: Record<string, string> = {};
    if (t) h.Authorization = `Bearer ${t}`;
    return h;
  },

  async init() {
    const mode = localStorage.getItem(LS.mode) as "oauth" | "manual" | null;
    const { token, expiresAt } = storedToken();

    if (token) {
      // Manual tokens never expire from our side.
      if (mode === "manual" || expiresAt > now() + 30) {
        set({ status: "authenticated", token, mode });
        return;
      }
      // OAuth access token expired/expiring — try silent refresh.
      if (await exchangeRefresh()) {
        set({
          status: "authenticated",
          token: localStorage.getItem(LS.accessToken),
          mode: "oauth",
        });
        return;
      }
      clearTokens();
    }

    // No usable token. Probe whether the server even requires auth.
    try {
      const res = await fetch("/api/v1/tasks", { headers: {} });
      if (res.status === 401) {
        set({ status: "needs-login", token: null, mode: null });
      } else {
        set({ status: "anonymous", token: null, mode: null });
      }
    } catch {
      // Network error — assume login is needed; the UI surfaces the error.
      set({ status: "needs-login", token: null, mode: null });
    }
  },

  async beginLogin() {
    set({ error: null });
    try {
      const { id } = await registerClient();
      const verifier = randomString(64);
      const challenge = await s256Challenge(verifier);
      const state = randomString(24);
      sessionStorage.setItem(SS.verifier, verifier);
      sessionStorage.setItem(SS.state, state);
      sessionStorage.setItem(
        SS.returnTo,
        window.location.pathname + window.location.search,
      );

      const params = new URLSearchParams({
        response_type: "code",
        client_id: id,
        redirect_uri: redirectUri(),
        scope: OAUTH.scope,
        state,
        code_challenge: challenge,
        code_challenge_method: "S256",
      });
      window.location.href = `${OAUTH.authorizeEndpoint}?${params.toString()}`;
    } catch (e) {
      // A stale in-memory client registration is the usual culprit; reset it.
      clearClient();
      set({ error: e instanceof Error ? e.message : String(e) });
    }
  },

  async handleCallback(code, state) {
    const expectedState = sessionStorage.getItem(SS.state);
    const verifier = sessionStorage.getItem(SS.verifier);
    if (!expectedState || state !== expectedState) {
      throw new Error("state mismatch — possible CSRF, please retry login");
    }
    if (!verifier) throw new Error("missing PKCE verifier — please retry login");
    const clientId = localStorage.getItem(LS.clientId);
    if (!clientId) throw new Error("missing client registration — please retry");

    const body = new URLSearchParams({
      grant_type: "authorization_code",
      code,
      redirect_uri: redirectUri(),
      client_id: clientId,
      code_verifier: verifier,
    });
    const res = await fetch(OAUTH.tokenEndpoint, {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body,
    });
    if (!res.ok) {
      const txt = await res.text().catch(() => "");
      throw new Error(`token exchange failed (${res.status}): ${txt}`);
    }
    const data = await res.json();
    saveTokens(data);
    sessionStorage.removeItem(SS.verifier);
    sessionStorage.removeItem(SS.state);
    const returnTo = sessionStorage.getItem(SS.returnTo) || "/";
    sessionStorage.removeItem(SS.returnTo);
    set({
      status: "authenticated",
      token: data.access_token,
      mode: "oauth",
      error: null,
    });
    return returnTo === OAUTH.redirectPath ? "/" : returnTo;
  },

  setManualToken(token) {
    localStorage.setItem(LS.accessToken, token);
    localStorage.setItem(LS.mode, "manual");
    localStorage.removeItem(LS.expiresAt);
    set({ status: "authenticated", token, mode: "manual", error: null });
  },

  logout() {
    clearTokens();
    clearClient();
    set({ status: "needs-login", token: null, mode: null });
  },

  async onUnauthorized() {
    const mode = get().mode;
    if (mode === "oauth" && (await exchangeRefresh())) {
      set({ token: localStorage.getItem(LS.accessToken) });
      return true;
    }
    clearTokens();
    // Keep the client registration; it may still be valid for a fresh login.
    set({ status: "needs-login", token: null, mode: null });
    return false;
  },
}));
