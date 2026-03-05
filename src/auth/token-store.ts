/**
 * API Token Store - Database Layer
 *
 * SQLite-backed storage for API bearer tokens.
 * Tokens are long-lived, revocable, and identified by a human-readable name.
 */

import { getDb } from "../core/db";
import { generateSecureToken } from "./crypto";

// =============================================================================
// Types
// =============================================================================

export interface ApiToken {
  token: string;
  name: string;
  createdAt: number;
}

export interface ApiTokenInfo {
  name: string;
  createdAt: number;
  lastUsedAt: number | null;
  revokedAt: number | null;
  tokenPrefix: string;
}

export interface ApiTokenValidation {
  valid: boolean;
  name?: string;
}

// =============================================================================
// Token Operations
// =============================================================================

/**
 * Create a new API token with the given name.
 * Names must be unique. Returns the full token (only time it's visible).
 */
export function createApiToken(name: string): ApiToken {
  const db = getDb();
  const token = generateSecureToken(32);
  const now = Date.now();

  db.run(
    `INSERT INTO api_tokens (token, name, created_at) VALUES (?, ?, ?)`,
    [token, name, now]
  );

  return { token, name, createdAt: now };
}

/**
 * Validate an API token. Returns validity and associated name.
 * Updates last_used_at on successful validation.
 */
export function validateApiToken(token: string): ApiTokenValidation {
  const db = getDb();

  const row = db
    .prepare(
      "SELECT name FROM api_tokens WHERE token = ? AND revoked_at IS NULL"
    )
    .get(token) as { name: string } | undefined;

  if (!row) {
    return { valid: false };
  }

  // Update last_used_at
  db.run("UPDATE api_tokens SET last_used_at = ? WHERE token = ?", [
    Date.now(),
    token,
  ]);

  return { valid: true, name: row.name };
}

/**
 * List all API tokens with metadata (token prefix only, not full token).
 */
export function listApiTokens(): ApiTokenInfo[] {
  const db = getDb();

  const rows = db
    .prepare(
      "SELECT token, name, created_at, last_used_at, revoked_at FROM api_tokens ORDER BY created_at DESC"
    )
    .all() as Array<{
    token: string;
    name: string;
    created_at: number;
    last_used_at: number | null;
    revoked_at: number | null;
  }>;

  return rows.map((row) => ({
    name: row.name,
    createdAt: row.created_at,
    lastUsedAt: row.last_used_at,
    revokedAt: row.revoked_at,
    tokenPrefix: row.token.slice(0, 8),
  }));
}

/**
 * Revoke an API token by name or token prefix.
 * Sets revoked_at timestamp (soft delete). Returns true if a token was revoked.
 */
export function revokeApiToken(nameOrPrefix: string): boolean {
  const db = getDb();
  const now = Date.now();

  // Try by name first
  const byName = db.run(
    "UPDATE api_tokens SET revoked_at = ? WHERE name = ? AND revoked_at IS NULL",
    [now, nameOrPrefix]
  );

  if (byName.changes > 0) {
    return true;
  }

  // Try by token prefix
  const byPrefix = db.run(
    "UPDATE api_tokens SET revoked_at = ? WHERE token LIKE ? AND revoked_at IS NULL",
    [now, `${nameOrPrefix}%`]
  );

  return byPrefix.changes > 0;
}

/**
 * Get API token metadata by name.
 */
export function getApiTokenByName(name: string): ApiTokenInfo | null {
  const db = getDb();

  const row = db
    .prepare(
      "SELECT token, name, created_at, last_used_at, revoked_at FROM api_tokens WHERE name = ?"
    )
    .get(name) as
    | {
        token: string;
        name: string;
        created_at: number;
        last_used_at: number | null;
        revoked_at: number | null;
      }
    | undefined;

  if (!row) return null;

  return {
    name: row.name,
    createdAt: row.created_at,
    lastUsedAt: row.last_used_at,
    revokedAt: row.revoked_at,
    tokenPrefix: row.token.slice(0, 8),
  };
}
