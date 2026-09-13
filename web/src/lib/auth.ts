// Auth client + zustand store for the brain-api server. Three login modes:
//
//   - password: POST /api/v1/auth/login with username/password → access+refresh
//     tokens; refreshed via /api/v1/auth/refresh.
//   - oauth: authorization-code + PKCE. "login" is a full-page redirect to the
//     server's /authorize consent page (which now asks for username/password),
//     then back to /auth/callback. Used by browser + connectors.
//   - manual: paste a long-lived API token.
//
// We always register a fresh OAuth client on each oauth login (registerClient)
// so a stale cached client_id can't wedge the flow with "unknown client_id".

import { create } from "zustand";
import { OAUTH, redirectUri } from "./config";
import { randomString, s256Challenge } from "./pkce";

const LS = {
  accessToken: "brain.access_token",
  refreshToken: "brain.refresh_token",
  expiresAt: "brain.expires_at",
  clientId: "brain.client_id",
  clientSecret: "brain.client_secret",
  mode: "brain.auth_mode", // "oauth" | "manual" | "password"
};

type AuthMode = "oauth" | "manual" | "password";
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
  mode: AuthMode | null;
  error: string | null;
  init: (lightweight?: boolean) => Promise<void>;
  beginLogin: () => Promise<void>;
  loginPassword: (username: string, password: string) => Promise<void>;
  handleCallback: (code: string, state: string) => Promise<string>;
  setManualToken: (token: string) => void;
  logout: () => void;
  /** Mark that the server rejected our token; try refresh, else needs-login. */
  onUnauthorized: (rejectedToken?: string | null) => Promise<boolean>;
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

function saveTokens(
  t: {
    access_token: string;
    refresh_token?: string;
    expires_in: number;
  },
  mode: AuthMode = "oauth",
) {
  localStorage.setItem(LS.accessToken, t.access_token);
  if (t.refresh_token) localStorage.setItem(LS.refreshToken, t.refresh_token);
  localStorage.setItem(LS.expiresAt, String(now() + (t.expires_in || 3600)));
  localStorage.setItem(LS.mode, mode);
}

// Refresh a password-mode session via the dedicated /api/v1/auth/refresh endpoint.
async function exchangePasswordRefresh(
  isCurrent: () => boolean,
): Promise<boolean> {
  const refresh = localStorage.getItem(LS.refreshToken);
  if (!refresh) return false;
  const res = await fetch("/api/v1/auth/refresh", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ refresh_token: refresh }),
  });
  if (!res.ok) return false;
  const data = await res.json();
  if (!isCurrent()) return false;
  saveTokens(data, "password");
  return true;
}

async function bindOfflineScope(token: string, isCurrent = () => true) {
  const res = await fetch("/api/v1/sync/identity", {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!res.ok)
    throw new Error(
      "Could not verify the offline account. Try signing in again.",
    );
  const { scope } = await res.json();
  if (typeof scope !== "string" || !/^[a-f0-9]{64}$/.test(scope))
    throw new Error("Invalid offline account identity");
  if (!isCurrent()) return;
  localStorage.setItem("brain.offline.scope", scope);
  localStorage.removeItem("brain.offline.anonymous");
}

function clearTokens() {
  localStorage.removeItem("brain.offline.scope");
  localStorage.removeItem("brain.offline.anonymous");
  localStorage.removeItem(LS.accessToken);
  localStorage.removeItem(LS.refreshToken);
  localStorage.removeItem(LS.expiresAt);
  localStorage.removeItem(LS.mode);
}

// registerClient always performs a fresh dynamic client registration. We do NOT
// reuse a cached client_id: if the server lost it (e.g. a restart before flow
// state was persisted), reusing a stale id produces an "unknown client_id" error
// on the /authorize redirect that the SPA can't catch. Registering fresh on each
// login self-heals that. Registration is a cheap POST.
async function registerClient(): Promise<{ id: string; secret: string }> {
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
  if (data.client_secret)
    localStorage.setItem(LS.clientSecret, data.client_secret);
  return { id: data.client_id, secret: data.client_secret || "" };
}

function clearClient() {
  localStorage.removeItem(LS.clientId);
  localStorage.removeItem(LS.clientSecret);
}

async function exchangeRefresh(isCurrent: () => boolean): Promise<boolean> {
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
  if (!isCurrent()) return false;
  saveTokens(data);
  return true;
}

// Refresh tokens rotate on consumption. Share the exchange across requests and
// serialize it across browser tabs, re-reading storage after acquiring the lock.
let authGeneration = 0;
const refreshes = new Map<string, Promise<boolean>>();
function refreshCredentials(
  mode: AuthMode | null,
  token: string,
): Promise<boolean> {
  const existing = refreshes.get(token);
  if (existing) return existing;
  const exchange = async () => {
    const isCurrent = () => localStorage.getItem(LS.accessToken) === token;
    if (!isCurrent()) return !!localStorage.getItem(LS.accessToken);
    const ok =
      mode === "password"
        ? await exchangePasswordRefresh(isCurrent)
        : mode === "oauth"
          ? await exchangeRefresh(isCurrent)
          : false;
    return ok || (!isCurrent() && !!localStorage.getItem(LS.accessToken));
  };
  const promise = (async () => {
    if (typeof navigator !== "undefined" && navigator.locks)
      return await navigator.locks.request("brain-auth-refresh", exchange);
    return await exchange();
  })().finally(() => {
    refreshes.delete(token);
  });
  refreshes.set(token, promise);
  return promise;
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

  async init(_lightweight = false) {
    const generation = authGeneration;
    const current = () => generation === authGeneration;
    const finish = (patch: Partial<AuthState>) => {
      if (current()) set(patch);
    };
    const mode = localStorage.getItem(LS.mode) as AuthMode | null;
    const { token, expiresAt } = storedToken();

    if (token) {
      // Manual tokens never expire from our side.
      if (mode === "manual" || expiresAt > now() + 30) {
        if (!localStorage.getItem("brain.offline.scope")) {
          try {
            await bindOfflineScope(token, current);
          } catch (e) {
            finish({
              status: "needs-login",
              token: null,
              mode: null,
              error: String(e),
            });
            return;
          }
        }
        finish({ status: "authenticated", token, mode });
        return;
      }
      // Access token expired/expiring — try a silent refresh for the mode.
      let refreshed = false;
      try {
        refreshed = await refreshCredentials(mode, token);
      } catch {
        if (localStorage.getItem("brain.offline.scope")) {
          finish({ status: "authenticated", token, mode });
          return;
        }
      }
      if (!current()) return;
      if (refreshed) {
        finish({
          status: "authenticated",
          token: localStorage.getItem(LS.accessToken),
          mode: mode ?? "oauth",
        });
        return;
      }
      if (!navigator.onLine && localStorage.getItem("brain.offline.scope")) {
        finish({ status: "authenticated", token, mode });
        return;
      }
      clearTokens();
    }

    // No usable token. Probe whether the server even requires auth.
    try {
      const res = await fetch("/api/v1/sync/identity", { headers: {} });
      if (!current()) return;
      if (res.status === 401) {
        finish({ status: "needs-login", token: null, mode: null });
      } else {
        localStorage.setItem("brain.offline.scope", "anonymous");
        localStorage.setItem("brain.offline.anonymous", "true");
        finish({ status: "anonymous", token: null, mode: null });
      }
    } catch {
      // Only reopen an anonymous cache after this origin previously verified it.
      finish({
        status:
          localStorage.getItem("brain.offline.anonymous") === "true"
            ? "anonymous"
            : "needs-login",
        token: null,
        mode: null,
      });
    }
  },

  async beginLogin() {
    ++authGeneration;
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
        window.location.pathname +
          window.location.search +
          window.location.hash,
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

  async loginPassword(username, password) {
    const generation = ++authGeneration;
    const current = () => generation === authGeneration;
    set({ error: null });
    const res = await fetch("/api/v1/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, password }),
    });
    if (!res.ok) {
      let msg = "Login failed";
      if (res.status === 401) msg = "Invalid username or password";
      else if (res.status === 429) msg = "Too many attempts — try again later";
      else if (res.status === 404)
        msg = "Password login is not enabled on this server";
      else {
        const txt = await res.text().catch(() => "");
        if (txt) msg = txt.slice(0, 200);
      }
      set({ error: msg });
      throw new Error(msg);
    }
    const data = await res.json();
    if (!current()) return;
    localStorage.removeItem("brain.offline.scope");
    localStorage.removeItem("brain.offline.anonymous");
    await bindOfflineScope(data.access_token, current);
    if (!current()) return;
    saveTokens(data, "password");
    set({
      status: "authenticated",
      token: data.access_token,
      mode: "password",
      error: null,
    });
  },

  async handleCallback(code, state) {
    ++authGeneration;
    const expectedState = sessionStorage.getItem(SS.state);
    const verifier = sessionStorage.getItem(SS.verifier);
    if (!expectedState || state !== expectedState) {
      throw new Error("state mismatch — possible CSRF, please retry login");
    }
    if (!verifier)
      throw new Error("missing PKCE verifier — please retry login");
    const clientId = localStorage.getItem(LS.clientId);
    if (!clientId)
      throw new Error("missing client registration — please retry");

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
    await bindOfflineScope(data.access_token);
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
    const generation = ++authGeneration;
    const current = () => generation === authGeneration;
    set({ status: "loading", error: null });
    void bindOfflineScope(token, current)
      .then(() => {
        if (!current()) return;
        localStorage.setItem(LS.accessToken, token);
        localStorage.setItem(LS.mode, "manual");
        localStorage.removeItem(LS.expiresAt);
        set({ status: "authenticated", token, mode: "manual", error: null });
      })
      .catch(
        (e) =>
          current() &&
          set({
            status: "needs-login",
            token: null,
            mode: null,
            error: String(e),
          }),
      );
  },

  logout() {
    ++authGeneration;
    // Stop lock-screen delivery on shared devices when explicitly signing out.
    const pushToken = get().token;
    if ("serviceWorker" in navigator) {
      void navigator.serviceWorker.getRegistration().then(async reg => {
        const sub = await reg?.pushManager?.getSubscription();
        if (!sub) return;
        try {
          await fetch("/api/v1/push/unsubscribe", {method: "POST", headers: {"Content-Type":"application/json", ...(pushToken ? {Authorization:`Bearer ${pushToken}`} : {})}, body:JSON.stringify({endpoint:sub.endpoint}), keepalive:true});
        } finally { await sub.unsubscribe(); }
      }).catch(() => {});
    }
    // Best-effort revoke for password sessions; fire-and-forget.
    if (get().mode === "password") {
      const refresh = localStorage.getItem(LS.refreshToken);
      if (refresh) {
        void fetch("/api/v1/auth/logout", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ refresh_token: refresh }),
        }).catch(() => {});
      }
    }
    clearTokens();
    clearClient();
    set({ status: "needs-login", token: null, mode: null });
  },

  async onUnauthorized(rejectedToken = get().token) {
    // Late 401s must never refresh or clear a newer login.
    if (rejectedToken !== get().token) return !!get().token;
    if (!rejectedToken) {
      if (get().status === "anonymous") {
        ++authGeneration;
        clearTokens();
        set({ status: "needs-login", token: null, mode: null });
      }
      return false;
    }
    const mode = get().mode;
    const refreshed = await refreshCredentials(mode, rejectedToken);
    if (get().token !== rejectedToken) return !!get().token;
    if (refreshed) {
      const token = localStorage.getItem(LS.accessToken);
      if (!token) return false;
      set({ token, mode: localStorage.getItem(LS.mode) as AuthMode | null });
      return true;
    }
    if (localStorage.getItem(LS.accessToken) !== rejectedToken) return false;
    ++authGeneration;
    clearTokens();
    set({ status: "needs-login", token: null, mode: null });
    return false;
  },
}));

// Login/logout and token refresh in another tab must also update its in-memory
// credentials before that tab can read a newly selected offline namespace.
if (
  typeof window !== "undefined" &&
  typeof window.addEventListener === "function"
) {
  window.addEventListener("storage", (event) => {
    if (event.key === LS.accessToken) {
      ++authGeneration;
      void useAuth.getState().init();
    }
  });
}
