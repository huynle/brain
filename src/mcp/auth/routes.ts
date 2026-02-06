/**
 * OAuth 2.1 Routes for MCP Server Authentication
 *
 * Implements:
 * - RFC 8414: OAuth 2.0 Authorization Server Metadata
 * - RFC 8707: Resource Indicators (Protected Resource Metadata)
 * - RFC 7591: Dynamic Client Registration
 * - OAuth 2.1 Authorization Code Grant with PKCE
 */

import { Hono, type Context } from "hono";
import type { ContentfulStatusCode } from "hono/utils/http-status";
import { getConfig } from "../../config";
import {
  clientStore,
  authCodeStore,
  accessTokenStore,
  refreshTokenStore,
} from "./stores";
import { verifyPkceChallenge, isValidCodeChallenge } from "./pkce";
import {
  OAuthError,
  type OAuthServerMetadata,
  type ProtectedResourceMetadata,
  type ClientRegistrationRequest,
  type ClientRegistrationResponse,
  type TokenResponse,
} from "./types";

/**
 * Get the base URL from request or config
 */
function getIssuer(requestUrl: string): string {
  const config = getConfig();
  // Use request origin for issuer
  const url = new URL(requestUrl);
  return `${url.protocol}//${url.host}`;
}

/**
 * Create OAuth routes
 */
export function createOAuthRoutes(): Hono {
  const oauth = new Hono();

  // ==========================================================================
  // Discovery Endpoints (RFC 8414 & RFC 8707)
  // ==========================================================================

  /**
   * OAuth Authorization Server Metadata
   * GET /.well-known/oauth-authorization-server
   */
  oauth.get("/.well-known/oauth-authorization-server", (c) => {
    const issuer = getIssuer(c.req.url);

    const metadata: OAuthServerMetadata = {
      issuer,
      authorization_endpoint: `${issuer}/authorize`,
      token_endpoint: `${issuer}/token`,
      registration_endpoint: `${issuer}/register`,
      scopes_supported: ["mcp", "mcp:read", "mcp:write"],
      response_types_supported: ["code"],
      grant_types_supported: ["authorization_code", "refresh_token"],
      token_endpoint_auth_methods_supported: [
        "client_secret_post",
        "client_secret_basic",
        "none",
      ],
      code_challenge_methods_supported: ["S256"],
      service_documentation: `${issuer}/api/v1/openapi.json`,
    };

    return c.json(metadata);
  });

  /**
   * Protected Resource Metadata (for MCP endpoint)
   * GET /.well-known/oauth-protected-resource/mcp
   */
  oauth.get("/.well-known/oauth-protected-resource/mcp", (c) => {
    const issuer = getIssuer(c.req.url);

    const metadata: ProtectedResourceMetadata = {
      resource: `${issuer}/mcp`,
      authorization_servers: [issuer],
      bearer_methods_supported: ["header"],
      scopes_supported: ["mcp", "mcp:read", "mcp:write"],
    };

    return c.json(metadata);
  });

  // ==========================================================================
  // Dynamic Client Registration (RFC 7591)
  // ==========================================================================

  /**
   * Register a new OAuth client
   * POST /register
   */
  oauth.post("/register", async (c) => {
    let body: ClientRegistrationRequest;

    try {
      body = await c.req.json();
    } catch {
      throw new OAuthError("invalid_request", "Invalid JSON body");
    }

    // Validate required fields
    if (!body.redirect_uris || !Array.isArray(body.redirect_uris)) {
      throw new OAuthError(
        "invalid_request",
        "redirect_uris is required and must be an array"
      );
    }

    if (body.redirect_uris.length === 0) {
      throw new OAuthError(
        "invalid_request",
        "At least one redirect_uri is required"
      );
    }

    // Validate redirect URIs
    for (const uri of body.redirect_uris) {
      try {
        new URL(uri);
      } catch {
        throw new OAuthError(
          "invalid_request",
          `Invalid redirect_uri: ${uri}`
        );
      }
    }

    // Create client
    const client = clientStore.create(body.redirect_uris, {
      clientName: body.client_name,
      clientUri: body.client_uri,
      logoUri: body.logo_uri,
      scope: body.scope,
      grantTypes: body.grant_types,
      responseTypes: body.response_types,
      tokenEndpointAuthMethod: body.token_endpoint_auth_method,
    });

    const response: ClientRegistrationResponse = {
      client_id: client.client_id,
      client_secret: client.client_secret,
      client_id_issued_at: Math.floor(client.created_at / 1000),
      client_secret_expires_at: 0, // Never expires
      redirect_uris: client.redirect_uris,
      client_name: client.client_name,
      grant_types: client.grant_types,
      response_types: client.response_types,
      token_endpoint_auth_method: client.token_endpoint_auth_method,
    };

    return c.json(response, 201);
  });

  // ==========================================================================
  // Authorization Endpoint
  // ==========================================================================

  /**
   * Authorization endpoint - displays consent page
   * GET /authorize
   */
  oauth.get("/authorize", async (c) => {
    const query = c.req.query();

    // Validate required parameters
    const responseType = query.response_type;
    const clientId = query.client_id;
    const redirectUri = query.redirect_uri;
    const codeChallenge = query.code_challenge;
    const codeChallengeMethod = query.code_challenge_method;
    const state = query.state;
    const scope = query.scope ?? "mcp";

    // Validate response_type
    if (responseType !== "code") {
      return c.html(renderError("Unsupported response_type. Only 'code' is supported."), 400);
    }

    // Validate client
    if (!clientId) {
      return c.html(renderError("Missing client_id"), 400);
    }

    const client = clientStore.get(clientId);
    if (!client) {
      return c.html(renderError("Invalid client_id"), 400);
    }

    // Validate redirect_uri
    if (!redirectUri) {
      return c.html(renderError("Missing redirect_uri"), 400);
    }

    if (!clientStore.validateRedirectUri(client, redirectUri)) {
      return c.html(renderError("Invalid redirect_uri"), 400);
    }

    // Validate PKCE (required in OAuth 2.1)
    if (!codeChallenge) {
      return redirectWithError(redirectUri, "invalid_request", "Missing code_challenge", state);
    }

    if (codeChallengeMethod !== "S256") {
      return redirectWithError(redirectUri, "invalid_request", "Only S256 code_challenge_method is supported", state);
    }

    if (!isValidCodeChallenge(codeChallenge)) {
      return redirectWithError(redirectUri, "invalid_request", "Invalid code_challenge format", state);
    }

    // Display consent page
    const issuer = getIssuer(c.req.url);
    return c.html(renderConsentPage({
      clientName: client.client_name ?? clientId,
      clientUri: client.client_uri,
      scope,
      state,
      clientId,
      redirectUri,
      codeChallenge,
      issuer,
    }));
  });

  /**
   * Authorization endpoint - handles consent form submission
   * POST /authorize
   */
  oauth.post("/authorize", async (c) => {
    const body = await c.req.parseBody();

    const action = body.action as string;
    const clientId = body.client_id as string;
    const redirectUri = body.redirect_uri as string;
    const codeChallenge = body.code_challenge as string;
    const scope = (body.scope as string) ?? "mcp";
    const state = body.state as string | undefined;

    // Validate client again
    const client = clientStore.get(clientId);
    if (!client || !clientStore.validateRedirectUri(client, redirectUri)) {
      return c.html(renderError("Invalid request"), 400);
    }

    // Handle deny
    if (action === "deny") {
      return redirectWithError(redirectUri, "access_denied", "User denied the request", state);
    }

    // Handle approve - create authorization code
    const authCode = authCodeStore.create(clientId, redirectUri, codeChallenge, {
      scope,
    });

    // Redirect with code
    const params = new URLSearchParams({ code: authCode.code });
    if (state) params.set("state", state);

    return c.redirect(`${redirectUri}?${params.toString()}`);
  });

  // ==========================================================================
  // Token Endpoint
  // ==========================================================================

  /**
   * Token endpoint
   * POST /token
   */
  oauth.post("/token", async (c) => {
    // Parse body (support both JSON and form-urlencoded)
    let body: Record<string, string>;
    const contentType = c.req.header("content-type") ?? "";

    if (contentType.includes("application/json")) {
      body = await c.req.json();
    } else {
      const formData = await c.req.parseBody();
      body = Object.fromEntries(
        Object.entries(formData).map(([k, v]) => [k, String(v)])
      );
    }

    const grantType = body.grant_type;

    // Check for client credentials in Authorization header
    let clientId = body.client_id;
    let clientSecret = body.client_secret;

    const authHeader = c.req.header("authorization");
    if (authHeader?.startsWith("Basic ")) {
      const decoded = atob(authHeader.slice(6));
      const [id, secret] = decoded.split(":");
      clientId = clientId ?? id;
      clientSecret = clientSecret ?? secret;
    }

    let response: TokenResponse;

    if (grantType === "authorization_code") {
      response = await handleAuthorizationCodeGrant(body, clientId, clientSecret);
    } else if (grantType === "refresh_token") {
      response = await handleRefreshTokenGrant(body, clientId, clientSecret);
    } else {
      throw new OAuthError("unsupported_grant_type", `Grant type '${grantType}' is not supported`);
    }

    return c.json(response);
  });

  // ==========================================================================
  // Error Handler
  // ==========================================================================

  oauth.onError((err, c) => {
    if (err instanceof OAuthError) {
      return c.json(err.toJSON(), err.statusCode as ContentfulStatusCode);
    }

    console.error("OAuth error:", err);
    return c.json(
      { error: "server_error", error_description: "Internal server error" },
      500
    );
  });

  return oauth;
}

// =============================================================================
// Grant Type Handlers
// =============================================================================

async function handleAuthorizationCodeGrant(
  body: Record<string, string>,
  clientId: string | undefined,
  clientSecret: string | undefined
): Promise<TokenResponse> {
  const code = body.code;
  const redirectUri = body.redirect_uri;
  const codeVerifier = body.code_verifier;

  // Validate required fields
  if (!code) {
    throw new OAuthError("invalid_request", "Missing code");
  }

  if (!redirectUri) {
    throw new OAuthError("invalid_request", "Missing redirect_uri");
  }

  if (!codeVerifier) {
    throw new OAuthError("invalid_request", "Missing code_verifier");
  }

  // Consume auth code (single use)
  const authCode = authCodeStore.consume(code);
  if (!authCode) {
    throw new OAuthError("invalid_grant", "Invalid or expired authorization code");
  }

  // Validate client
  if (clientId && authCode.client_id !== clientId) {
    throw new OAuthError("invalid_grant", "Client ID mismatch");
  }

  // Validate redirect_uri matches
  if (authCode.redirect_uri !== redirectUri) {
    throw new OAuthError("invalid_grant", "Redirect URI mismatch");
  }

  // Verify PKCE
  const pkceValid = await verifyPkceChallenge(codeVerifier, authCode.code_challenge);
  if (!pkceValid) {
    throw new OAuthError("invalid_grant", "Invalid code_verifier");
  }

  // Validate client credentials if provided
  if (clientSecret) {
    const client = clientStore.validate(authCode.client_id, clientSecret);
    if (!client) {
      throw new OAuthError("invalid_client", "Invalid client credentials", 401);
    }
  }

  // Issue tokens
  const accessToken = accessTokenStore.create(authCode.client_id, {
    scope: authCode.scope,
    userId: authCode.user_id,
    ttlSeconds: 3600, // 1 hour
  });

  const refreshToken = refreshTokenStore.create(authCode.client_id, {
    scope: authCode.scope,
    userId: authCode.user_id,
    ttlSeconds: 604800, // 7 days
  });

  return {
    access_token: accessToken.token,
    token_type: "Bearer",
    expires_in: 3600,
    refresh_token: refreshToken.token,
    scope: accessToken.scope,
  };
}

async function handleRefreshTokenGrant(
  body: Record<string, string>,
  clientId: string | undefined,
  clientSecret: string | undefined
): Promise<TokenResponse> {
  const refreshToken = body.refresh_token;

  if (!refreshToken) {
    throw new OAuthError("invalid_request", "Missing refresh_token");
  }

  // Rotate tokens
  const tokens = refreshTokenStore.rotate(refreshToken, {
    accessTtl: 3600,
    refreshTtl: 604800,
  });

  if (!tokens) {
    throw new OAuthError("invalid_grant", "Invalid or expired refresh token");
  }

  // Validate client if credentials provided
  if (clientId && tokens.accessToken.client_id !== clientId) {
    throw new OAuthError("invalid_grant", "Client ID mismatch");
  }

  if (clientSecret) {
    const client = clientStore.validate(tokens.accessToken.client_id, clientSecret);
    if (!client) {
      throw new OAuthError("invalid_client", "Invalid client credentials", 401);
    }
  }

  return {
    access_token: tokens.accessToken.token,
    token_type: "Bearer",
    expires_in: 3600,
    refresh_token: tokens.refreshToken.token,
    scope: tokens.accessToken.scope,
  };
}

// =============================================================================
// HTML Templates
// =============================================================================

function redirectWithError(
  redirectUri: string,
  error: string,
  description: string,
  state?: string
): Response {
  const params = new URLSearchParams({
    error,
    error_description: description,
  });
  if (state) params.set("state", state);

  return Response.redirect(`${redirectUri}?${params.toString()}`, 302);
}

function renderError(message: string): string {
  return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Authorization Error - Brain MCP</title>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body { 
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
      background: #0a0a0a; color: #f5f5f5;
      display: flex; align-items: center; justify-content: center;
      min-height: 100vh; padding: 1rem;
    }
    .container {
      background: #1a1a1a; border-radius: 12px; padding: 2rem;
      max-width: 400px; width: 100%; border: 1px solid #333;
    }
    h1 { color: #ff6b6b; font-size: 1.25rem; margin-bottom: 1rem; }
    p { color: #aaa; line-height: 1.6; }
  </style>
</head>
<body>
  <div class="container">
    <h1>Authorization Error</h1>
    <p>${escapeHtml(message)}</p>
  </div>
</body>
</html>`;
}

function renderConsentPage(params: {
  clientName: string;
  clientUri?: string;
  scope: string;
  state?: string;
  clientId: string;
  redirectUri: string;
  codeChallenge: string;
  issuer: string;
}): string {
  const scopes = params.scope.split(" ").filter(Boolean);

  return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Authorize - Brain MCP</title>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body { 
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
      background: #0a0a0a; color: #f5f5f5;
      display: flex; align-items: center; justify-content: center;
      min-height: 100vh; padding: 1rem;
    }
    .container {
      background: #1a1a1a; border-radius: 12px; padding: 2rem;
      max-width: 420px; width: 100%; border: 1px solid #333;
    }
    .header { text-align: center; margin-bottom: 1.5rem; }
    .logo { font-size: 2.5rem; margin-bottom: 0.5rem; }
    h1 { font-size: 1.25rem; font-weight: 600; margin-bottom: 0.25rem; }
    .subtitle { color: #888; font-size: 0.875rem; }
    .client-info {
      background: #222; border-radius: 8px; padding: 1rem;
      margin-bottom: 1.5rem; border: 1px solid #333;
    }
    .client-name { font-weight: 600; font-size: 1.1rem; }
    .client-uri { color: #888; font-size: 0.8rem; margin-top: 0.25rem; }
    .scope-section { margin-bottom: 1.5rem; }
    .scope-title { font-size: 0.875rem; color: #888; margin-bottom: 0.5rem; }
    .scope-list { list-style: none; }
    .scope-item {
      display: flex; align-items: center; padding: 0.5rem 0;
      border-bottom: 1px solid #333;
    }
    .scope-item:last-child { border-bottom: none; }
    .scope-icon { color: #10b981; margin-right: 0.75rem; }
    .scope-name { font-weight: 500; }
    .scope-desc { color: #888; font-size: 0.8rem; }
    .buttons { display: flex; gap: 0.75rem; }
    button {
      flex: 1; padding: 0.875rem 1.5rem; border-radius: 8px;
      font-size: 0.9rem; font-weight: 500; cursor: pointer;
      border: none; transition: all 0.2s;
    }
    .btn-deny { background: #333; color: #fff; }
    .btn-deny:hover { background: #444; }
    .btn-allow { background: #10b981; color: #fff; }
    .btn-allow:hover { background: #059669; }
    .footer { text-align: center; margin-top: 1.5rem; color: #666; font-size: 0.75rem; }
  </style>
</head>
<body>
  <div class="container">
    <div class="header">
      <div class="logo">&#129504;</div>
      <h1>Brain MCP</h1>
      <div class="subtitle">Authorization Request</div>
    </div>

    <div class="client-info">
      <div class="client-name">${escapeHtml(params.clientName)}</div>
      ${params.clientUri ? `<div class="client-uri">${escapeHtml(params.clientUri)}</div>` : ""}
      <div class="client-uri">wants to access your Brain</div>
    </div>

    <div class="scope-section">
      <div class="scope-title">This will allow the application to:</div>
      <ul class="scope-list">
        ${scopes.map((scope) => renderScopeItem(scope)).join("")}
      </ul>
    </div>

    <form method="POST" action="/authorize">
      <input type="hidden" name="client_id" value="${escapeHtml(params.clientId)}">
      <input type="hidden" name="redirect_uri" value="${escapeHtml(params.redirectUri)}">
      <input type="hidden" name="code_challenge" value="${escapeHtml(params.codeChallenge)}">
      <input type="hidden" name="scope" value="${escapeHtml(params.scope)}">
      ${params.state ? `<input type="hidden" name="state" value="${escapeHtml(params.state)}">` : ""}

      <div class="buttons">
        <button type="submit" name="action" value="deny" class="btn-deny">Deny</button>
        <button type="submit" name="action" value="allow" class="btn-allow">Allow</button>
      </div>
    </form>

    <div class="footer">
      By allowing access, you agree to share the requested data with this application.
    </div>
  </div>
</body>
</html>`;
}

function renderScopeItem(scope: string): string {
  const scopeInfo: Record<string, { name: string; desc: string }> = {
    mcp: { name: "Full MCP Access", desc: "Read and execute MCP tools" },
    "mcp:read": { name: "Read Access", desc: "Read brain entries and search" },
    "mcp:write": { name: "Write Access", desc: "Create and modify brain entries" },
  };

  const info = scopeInfo[scope] ?? { name: scope, desc: "Access to this scope" };

  return `<li class="scope-item">
    <span class="scope-icon">&#10003;</span>
    <div>
      <div class="scope-name">${escapeHtml(info.name)}</div>
      <div class="scope-desc">${escapeHtml(info.desc)}</div>
    </div>
  </li>`;
}

function escapeHtml(str: string): string {
  return str
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;");
}
