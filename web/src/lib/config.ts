// Runtime configuration. The PWA is served from the same origin as the API in
// production (embedded in the Go binary), so all paths are relative. In dev,
// Vite proxies these prefixes to the brain-api server.

export const API_BASE = ""; // same-origin
export const API_V1 = "/api/v1";

export const OAUTH = {
  authorizeEndpoint: "/authorize",
  tokenEndpoint: "/token",
  registerEndpoint: "/register",
  scope: "mcp",
  clientName: "Brain PWA",
  // The redirect target is a client-side route handled by the SPA.
  redirectPath: "/auth/callback",
};

export function redirectUri(): string {
  return window.location.origin + OAUTH.redirectPath;
}
