/**
 * Brain API - Token Management Endpoints
 *
 * REST API endpoints for managing API bearer tokens.
 * These endpoints go through the running server's DB connection,
 * avoiding WAL visibility issues that occur when CLI processes
 * write tokens via a separate SQLite connection.
 *
 * All token endpoints require authentication (when ENABLE_AUTH=true).
 * The exception is POST /tokens/bootstrap, which is only available
 * when no tokens exist yet (first-run bootstrap).
 */

import { OpenAPIHono, createRoute, z } from "@hono/zod-openapi";
import {
  createApiToken,
  listApiTokens,
  revokeApiToken,
  getApiTokenByName,
} from "../auth";
import { ErrorResponseSchema, NotFoundResponseSchema } from "./schemas";

// =============================================================================
// Schemas
// =============================================================================

const CreateTokenRequestSchema = z
  .object({
    name: z.string().min(1).max(64).openapi({
      description: "Human-readable name for the token (must be unique)",
      example: "runner",
    }),
  })
  .openapi("CreateTokenRequest");

const TokenResponseSchema = z
  .object({
    token: z.string().openapi({
      description:
        "The full API token. Save this — it cannot be displayed again.",
      example: "a1b2c3d4...",
    }),
    name: z.string(),
    createdAt: z.number(),
  })
  .openapi("TokenResponse");

const TokenInfoSchema = z
  .object({
    name: z.string(),
    createdAt: z.number(),
    lastUsedAt: z.number().nullable(),
    revokedAt: z.number().nullable(),
    tokenPrefix: z.string().openapi({
      description: "First 8 characters of the token for identification",
    }),
  })
  .openapi("TokenInfo");

const TokenListResponseSchema = z
  .object({
    tokens: z.array(TokenInfoSchema),
    count: z.number(),
    active: z.number(),
    revoked: z.number(),
  })
  .openapi("TokenListResponse");

const TokenRevokeResponseSchema = z
  .object({
    revoked: z.boolean(),
    name: z.string(),
  })
  .openapi("TokenRevokeResponse");

// =============================================================================
// Route Definitions
// =============================================================================

const createTokenRoute = createRoute({
  method: "post",
  path: "/",
  tags: ["Tokens"],
  summary: "Create a new API token",
  description:
    "Creates a new API bearer token with the given name. Names must be unique. The full token is returned only once — save it immediately.",
  request: {
    body: {
      content: {
        "application/json": {
          schema: CreateTokenRequestSchema,
        },
      },
    },
  },
  responses: {
    201: {
      content: { "application/json": { schema: TokenResponseSchema } },
      description: "Token created successfully",
    },
    400: {
      content: { "application/json": { schema: ErrorResponseSchema } },
      description: "Invalid request (missing or duplicate name)",
    },
    409: {
      content: { "application/json": { schema: ErrorResponseSchema } },
      description: "Token with this name already exists",
    },
  },
});

const listTokensRoute = createRoute({
  method: "get",
  path: "/",
  tags: ["Tokens"],
  summary: "List all API tokens",
  description:
    "Returns metadata for all API tokens. Full token values are never returned — only the first 8 characters for identification.",
  responses: {
    200: {
      content: { "application/json": { schema: TokenListResponseSchema } },
      description: "Token list",
    },
  },
});

const revokeTokenRoute = createRoute({
  method: "delete",
  path: "/{name}",
  tags: ["Tokens"],
  summary: "Revoke an API token",
  description:
    "Revokes an API token by name (soft delete). The token will immediately stop working for authentication.",
  request: {
    params: z.object({
      name: z.string().min(1).openapi({
        description: "Token name to revoke",
        example: "runner",
      }),
    }),
  },
  responses: {
    200: {
      content: { "application/json": { schema: TokenRevokeResponseSchema } },
      description: "Token revoked",
    },
    404: {
      content: { "application/json": { schema: NotFoundResponseSchema } },
      description: "Token not found or already revoked",
    },
  },
});

// =============================================================================
// Route Factory
// =============================================================================

export function createTokenRoutes(): OpenAPIHono {
  const app = new OpenAPIHono();

  // POST /tokens - Create a new token
  app.openapi(createTokenRoute, async (c) => {
    const { name } = c.req.valid("json");

    // Check if token with this name already exists and is active
    const existing = getApiTokenByName(name);
    if (existing && !existing.revokedAt) {
      return c.json(
        {
          error: "Conflict",
          message: `Token with name "${name}" already exists`,
        },
        409
      );
    }

    try {
      const result = createApiToken(name);
      return c.json(
        {
          token: result.token,
          name: result.name,
          createdAt: result.createdAt,
        },
        201
      );
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : String(err);
      return c.json(
        {
          error: "Bad Request",
          message: `Failed to create token: ${message}`,
        },
        400
      );
    }
  });

  // GET /tokens - List all tokens
  app.openapi(listTokensRoute, async (c) => {
    const tokens = listApiTokens();
    const active = tokens.filter((t) => !t.revokedAt).length;
    const revoked = tokens.filter((t) => t.revokedAt).length;

    return c.json({
      tokens,
      count: tokens.length,
      active,
      revoked,
    });
  });

  // DELETE /tokens/:name - Revoke a token
  app.openapi(revokeTokenRoute, async (c) => {
    const { name } = c.req.valid("param");

    const revoked = revokeApiToken(name);

    if (!revoked) {
      return c.json(
        {
          error: "Not Found" as const,
          message: `Token "${name}" not found or already revoked`,
        },
        404
      );
    }

    return c.json({ revoked: true as const, name }, 200);
  });

  return app;
}
