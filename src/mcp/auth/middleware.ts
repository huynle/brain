/**
 * OAuth 2.1 Bearer Token Middleware for MCP
 *
 * Validates Bearer tokens on protected endpoints.
 */

import { createMiddleware } from "hono/factory";
import type { Context, Next } from "hono";
import { accessTokenStore } from "./stores";
import type { AccessToken } from "./types";

// Extend Hono context to include OAuth token info
declare module "hono" {
  interface ContextVariableMap {
    oauthToken?: AccessToken;
    oauthClientId?: string;
    oauthScope?: string;
    oauthUserId?: string;
  }
}

/**
 * Extract Bearer token from Authorization header
 */
function extractBearerToken(authHeader: string | undefined): string | null {
  if (!authHeader) return null;

  // Support "Bearer <token>" format
  if (authHeader.startsWith("Bearer ")) {
    return authHeader.slice(7);
  }

  // Also support just the token for convenience
  if (authHeader.startsWith("bearer ")) {
    return authHeader.slice(7);
  }

  return null;
}

/**
 * Bearer token authentication middleware
 *
 * Validates the Bearer token and adds token info to context.
 * Returns 401 if token is missing or invalid.
 */
export const bearerAuth = createMiddleware(async (c: Context, next: Next) => {
  const authHeader = c.req.header("authorization");
  const token = extractBearerToken(authHeader);

  if (!token) {
    return c.json(
      {
        error: "unauthorized",
        error_description: "Missing or invalid Authorization header. Use: Bearer <token>",
      },
      401,
      {
        "WWW-Authenticate": 'Bearer realm="mcp"',
      }
    );
  }

  // Validate token
  const accessToken = accessTokenStore.get(token);

  if (!accessToken) {
    return c.json(
      {
        error: "invalid_token",
        error_description: "Token is invalid or expired",
      },
      401,
      {
        "WWW-Authenticate": 'Bearer realm="mcp", error="invalid_token"',
      }
    );
  }

  // Add token info to context
  c.set("oauthToken", accessToken);
  c.set("oauthClientId", accessToken.client_id);
  c.set("oauthScope", accessToken.scope);
  c.set("oauthUserId", accessToken.user_id);

  await next();
});

/**
 * Optional bearer auth - validates token if present, but doesn't require it
 */
export const optionalBearerAuth = createMiddleware(async (c: Context, next: Next) => {
  const authHeader = c.req.header("authorization");
  const token = extractBearerToken(authHeader);

  if (token) {
    const accessToken = accessTokenStore.get(token);
    if (accessToken) {
      c.set("oauthToken", accessToken);
      c.set("oauthClientId", accessToken.client_id);
      c.set("oauthScope", accessToken.scope);
      c.set("oauthUserId", accessToken.user_id);
    }
  }

  await next();
});

/**
 * Scope validation middleware factory
 *
 * Use after bearerAuth to check if the token has required scope(s).
 */
export function requireScope(...requiredScopes: string[]) {
  return createMiddleware(async (c: Context, next: Next) => {
    const tokenScope = c.get("oauthScope");

    if (!tokenScope) {
      return c.json(
        {
          error: "insufficient_scope",
          error_description: "Token scope is missing",
        },
        403
      );
    }

    const grantedScopes = tokenScope.split(" ");

    // Check if any required scope is granted
    // Also check for parent scopes (mcp includes mcp:read and mcp:write)
    const hasScope = requiredScopes.some((required) => {
      if (grantedScopes.includes(required)) return true;
      // mcp scope grants all sub-scopes
      if (grantedScopes.includes("mcp") && required.startsWith("mcp:")) return true;
      return false;
    });

    if (!hasScope) {
      return c.json(
        {
          error: "insufficient_scope",
          error_description: `Required scope: ${requiredScopes.join(" or ")}`,
        },
        403,
        {
          "WWW-Authenticate": `Bearer realm="mcp", scope="${requiredScopes.join(" ")}"`,
        }
      );
    }

    await next();
  });
}

/**
 * Create conditional auth middleware that checks config
 */
export function conditionalAuth(enabled: boolean) {
  return createMiddleware(async (c: Context, next: Next) => {
    if (!enabled) {
      // Auth is disabled, allow all requests
      await next();
      return;
    }

    // Auth is enabled, require bearer token
    const authHeader = c.req.header("authorization");
    const token = extractBearerToken(authHeader);

    if (!token) {
      return c.json(
        {
          error: "unauthorized",
          error_description: "Authentication required. Use: Bearer <token>",
        },
        401,
        {
          "WWW-Authenticate": 'Bearer realm="mcp"',
        }
      );
    }

    const accessToken = accessTokenStore.get(token);

    if (!accessToken) {
      return c.json(
        {
          error: "invalid_token",
          error_description: "Token is invalid or expired",
        },
        401,
        {
          "WWW-Authenticate": 'Bearer realm="mcp", error="invalid_token"',
        }
      );
    }

    c.set("oauthToken", accessToken);
    c.set("oauthClientId", accessToken.client_id);
    c.set("oauthScope", accessToken.scope);
    c.set("oauthUserId", accessToken.user_id);

    await next();
  });
}
