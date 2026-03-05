/**
 * API Auth Middleware
 *
 * Unified authentication for brain-api REST routes.
 * Validates tokens from Bearer header or ?token= query param
 * against both the API token store and the OAuth access token store.
 */

import { createMiddleware } from "hono/factory";
import type { Context, Next } from "hono";
import { validateApiToken } from "./token-store";
import { accessTokenStore } from "../mcp/auth/stores";

// Augment Hono's context variable types
declare module "hono" {
  interface ContextVariableMap {
    authType?: "api_token" | "oauth";
    authName?: string;
  }
}

/**
 * Extract Bearer token from Authorization header.
 * Supports both "Bearer" and "bearer" prefix (case-insensitive first 7 chars).
 */
function extractBearerToken(
  authHeader: string | undefined
): string | undefined {
  if (!authHeader) return undefined;
  if (authHeader.startsWith("Bearer ")) return authHeader.slice(7);
  if (authHeader.startsWith("bearer ")) return authHeader.slice(7);
  return undefined;
}

/**
 * API authentication middleware.
 *
 * When `enabled` is false, all requests pass through.
 * When `enabled` is true:
 *   1. Extracts token from Authorization: Bearer header or ?token= query param
 *   2. Validates against API token store first
 *   3. Falls back to OAuth access token store
 *   4. Returns 401 if no valid token found
 *
 * Paths in `skipPaths` bypass authentication even when enabled.
 */
export function apiAuth(enabled: boolean, skipPaths: string[] = []) {
  return createMiddleware(async (c: Context, next: Next) => {
    if (!enabled) {
      return next();
    }

    // Skip auth for specific paths
    const url = new URL(c.req.url);
    if (skipPaths.some((skip) => url.pathname === skip || url.pathname.startsWith(skip + "/"))) {
      return next();
    }

    // Extract token: header takes precedence over query param
    const token =
      extractBearerToken(c.req.header("authorization")) ??
      (url.searchParams.get("token") || undefined);

    if (!token) {
      return c.json(
        {
          error: "unauthorized",
          error_description: "Missing authentication token",
        },
        401,
        {
          "WWW-Authenticate": 'Bearer realm="brain-api"',
        }
      );
    }

    // Try API token store first
    const apiResult = validateApiToken(token);
    if (apiResult.valid) {
      c.set("authType", "api_token");
      c.set("authName", apiResult.name);
      return next();
    }

    // Fall back to OAuth access token store
    const oauthToken = accessTokenStore.get(token);
    if (oauthToken) {
      c.set("authType", "oauth");
      c.set("oauthClientId", oauthToken.client_id);
      c.set("oauthScope", oauthToken.scope);
      return next();
    }

    // Both stores rejected the token
    return c.json(
      {
        error: "invalid_token",
        error_description: "Token is invalid or revoked",
      },
      401,
      {
        "WWW-Authenticate": 'Bearer realm="brain-api", error="invalid_token"',
      }
    );
  });
}
