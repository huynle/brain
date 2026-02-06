/**
 * OAuth 2.1 Stores - Database Layer
 *
 * SQLite-backed storage for OAuth clients, tokens, and authorization codes.
 */

import { getDb } from "../../core/db";
import type {
  OAuthClient,
  AuthorizationCode,
  AccessToken,
  RefreshToken,
} from "./types";

// =============================================================================
// Utility Functions
// =============================================================================

/**
 * Generate a cryptographically secure random string
 */
export function generateSecureToken(length: number = 32): string {
  const array = new Uint8Array(length);
  crypto.getRandomValues(array);
  return Array.from(array)
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

/**
 * Generate client ID (shorter, URL-safe)
 */
export function generateClientId(): string {
  return `brain_${generateSecureToken(16)}`;
}

/**
 * Generate client secret (longer for security)
 */
export function generateClientSecret(): string {
  return generateSecureToken(32);
}

// =============================================================================
// Client Store
// =============================================================================

export const clientStore = {
  /**
   * Create a new OAuth client (Dynamic Client Registration)
   */
  create(
    redirectUris: string[],
    options: {
      clientName?: string;
      clientUri?: string;
      logoUri?: string;
      scope?: string;
      grantTypes?: string[];
      responseTypes?: string[];
      tokenEndpointAuthMethod?: string;
    } = {}
  ): OAuthClient {
    const db = getDb();
    const now = Date.now();

    const client: OAuthClient = {
      client_id: generateClientId(),
      client_secret: generateClientSecret(),
      redirect_uris: redirectUris,
      client_name: options.clientName,
      client_uri: options.clientUri,
      logo_uri: options.logoUri,
      scope: options.scope ?? "mcp",
      grant_types: options.grantTypes ?? ["authorization_code", "refresh_token"],
      response_types: options.responseTypes ?? ["code"],
      token_endpoint_auth_method:
        (options.tokenEndpointAuthMethod as OAuthClient["token_endpoint_auth_method"]) ??
        "client_secret_post",
      created_at: now,
    };

    db.run(
      `INSERT INTO oauth_clients 
       (client_id, client_secret, redirect_uris, client_name, client_uri, logo_uri, 
        scope, grant_types, response_types, token_endpoint_auth_method, created_at)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
      [
        client.client_id,
        client.client_secret,
        JSON.stringify(client.redirect_uris),
        client.client_name ?? null,
        client.client_uri ?? null,
        client.logo_uri ?? null,
        client.scope ?? null,
        JSON.stringify(client.grant_types),
        JSON.stringify(client.response_types),
        client.token_endpoint_auth_method,
        client.created_at,
      ]
    );

    return client;
  },

  /**
   * Get a client by ID
   */
  get(clientId: string): OAuthClient | null {
    const db = getDb();
    const row = db
      .prepare("SELECT * FROM oauth_clients WHERE client_id = ?")
      .get(clientId) as {
      client_id: string;
      client_secret: string;
      redirect_uris: string;
      client_name: string | null;
      client_uri: string | null;
      logo_uri: string | null;
      scope: string | null;
      grant_types: string;
      response_types: string;
      token_endpoint_auth_method: string;
      created_at: number;
    } | undefined;

    if (!row) return null;

    return {
      client_id: row.client_id,
      client_secret: row.client_secret,
      redirect_uris: JSON.parse(row.redirect_uris),
      client_name: row.client_name ?? undefined,
      client_uri: row.client_uri ?? undefined,
      logo_uri: row.logo_uri ?? undefined,
      scope: row.scope ?? undefined,
      grant_types: JSON.parse(row.grant_types),
      response_types: JSON.parse(row.response_types),
      token_endpoint_auth_method: row.token_endpoint_auth_method as OAuthClient["token_endpoint_auth_method"],
      created_at: row.created_at,
    };
  },

  /**
   * Validate client credentials
   */
  validate(clientId: string, clientSecret: string): OAuthClient | null {
    const client = this.get(clientId);
    if (!client) return null;
    if (client.client_secret !== clientSecret) return null;
    return client;
  },

  /**
   * Check if redirect URI is valid for client
   */
  validateRedirectUri(client: OAuthClient, redirectUri: string): boolean {
    return client.redirect_uris.includes(redirectUri);
  },

  /**
   * Delete a client and all associated tokens
   */
  delete(clientId: string): boolean {
    const db = getDb();

    // Delete associated tokens first
    db.run("DELETE FROM oauth_access_tokens WHERE client_id = ?", [clientId]);
    db.run("DELETE FROM oauth_refresh_tokens WHERE client_id = ?", [clientId]);
    db.run("DELETE FROM oauth_auth_codes WHERE client_id = ?", [clientId]);

    const result = db.run("DELETE FROM oauth_clients WHERE client_id = ?", [
      clientId,
    ]);
    return result.changes > 0;
  },
};

// =============================================================================
// Authorization Code Store
// =============================================================================

export const authCodeStore = {
  /**
   * Create a new authorization code
   */
  create(
    clientId: string,
    redirectUri: string,
    codeChallenge: string,
    options: {
      scope?: string;
      userId?: string;
      ttlSeconds?: number;
    } = {}
  ): AuthorizationCode {
    const db = getDb();
    const now = Date.now();
    const ttl = (options.ttlSeconds ?? 600) * 1000; // Default 10 minutes

    const authCode: AuthorizationCode = {
      code: generateSecureToken(32),
      client_id: clientId,
      redirect_uri: redirectUri,
      scope: options.scope ?? "mcp",
      code_challenge: codeChallenge,
      code_challenge_method: "S256",
      user_id: options.userId,
      expires_at: now + ttl,
      created_at: now,
    };

    db.run(
      `INSERT INTO oauth_auth_codes 
       (code, client_id, redirect_uri, scope, code_challenge, code_challenge_method, 
        user_id, expires_at, created_at)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
      [
        authCode.code,
        authCode.client_id,
        authCode.redirect_uri,
        authCode.scope,
        authCode.code_challenge,
        authCode.code_challenge_method,
        authCode.user_id ?? null,
        authCode.expires_at,
        authCode.created_at,
      ]
    );

    return authCode;
  },

  /**
   * Get and consume an authorization code (single use)
   */
  consume(code: string): AuthorizationCode | null {
    const db = getDb();
    const now = Date.now();

    const row = db
      .prepare(
        "SELECT * FROM oauth_auth_codes WHERE code = ? AND expires_at > ?"
      )
      .get(code, now) as {
      code: string;
      client_id: string;
      redirect_uri: string;
      scope: string | null;
      code_challenge: string;
      code_challenge_method: string;
      user_id: string | null;
      expires_at: number;
      created_at: number;
    } | undefined;

    if (!row) return null;

    // Delete immediately (single use)
    db.run("DELETE FROM oauth_auth_codes WHERE code = ?", [code]);

    return {
      code: row.code,
      client_id: row.client_id,
      redirect_uri: row.redirect_uri,
      scope: row.scope ?? "",
      code_challenge: row.code_challenge,
      code_challenge_method: "S256",
      user_id: row.user_id ?? undefined,
      expires_at: row.expires_at,
      created_at: row.created_at,
    };
  },

  /**
   * Clean up expired codes
   */
  cleanup(): number {
    const db = getDb();
    const result = db.run(
      "DELETE FROM oauth_auth_codes WHERE expires_at < ?",
      [Date.now()]
    );
    return result.changes;
  },
};

// =============================================================================
// Access Token Store
// =============================================================================

export const accessTokenStore = {
  /**
   * Create a new access token
   */
  create(
    clientId: string,
    options: {
      scope?: string;
      userId?: string;
      ttlSeconds?: number;
    } = {}
  ): AccessToken {
    const db = getDb();
    const now = Date.now();
    const ttl = (options.ttlSeconds ?? 3600) * 1000; // Default 1 hour

    const token: AccessToken = {
      token: generateSecureToken(32),
      token_type: "Bearer",
      client_id: clientId,
      scope: options.scope ?? "mcp",
      user_id: options.userId,
      expires_at: now + ttl,
      created_at: now,
    };

    db.run(
      `INSERT INTO oauth_access_tokens 
       (token, client_id, scope, user_id, expires_at, created_at)
       VALUES (?, ?, ?, ?, ?, ?)`,
      [
        token.token,
        token.client_id,
        token.scope,
        token.user_id ?? null,
        token.expires_at,
        token.created_at,
      ]
    );

    return token;
  },

  /**
   * Get an access token (validates expiry)
   */
  get(tokenValue: string): AccessToken | null {
    const db = getDb();
    const now = Date.now();

    const row = db
      .prepare(
        "SELECT * FROM oauth_access_tokens WHERE token = ? AND expires_at > ?"
      )
      .get(tokenValue, now) as {
      token: string;
      client_id: string;
      scope: string | null;
      user_id: string | null;
      expires_at: number;
      created_at: number;
    } | undefined;

    if (!row) return null;

    return {
      token: row.token,
      token_type: "Bearer",
      client_id: row.client_id,
      scope: row.scope ?? "",
      user_id: row.user_id ?? undefined,
      expires_at: row.expires_at,
      created_at: row.created_at,
    };
  },

  /**
   * Revoke an access token
   */
  revoke(tokenValue: string): boolean {
    const db = getDb();
    const result = db.run("DELETE FROM oauth_access_tokens WHERE token = ?", [
      tokenValue,
    ]);
    return result.changes > 0;
  },

  /**
   * Revoke all tokens for a client
   */
  revokeByClient(clientId: string): number {
    const db = getDb();
    const result = db.run(
      "DELETE FROM oauth_access_tokens WHERE client_id = ?",
      [clientId]
    );
    return result.changes;
  },

  /**
   * Clean up expired tokens
   */
  cleanup(): number {
    const db = getDb();
    const result = db.run(
      "DELETE FROM oauth_access_tokens WHERE expires_at < ?",
      [Date.now()]
    );
    return result.changes;
  },
};

// =============================================================================
// Refresh Token Store
// =============================================================================

export const refreshTokenStore = {
  /**
   * Create a new refresh token
   */
  create(
    clientId: string,
    options: {
      scope?: string;
      userId?: string;
      ttlSeconds?: number;
    } = {}
  ): RefreshToken {
    const db = getDb();
    const now = Date.now();
    const ttl = (options.ttlSeconds ?? 604800) * 1000; // Default 7 days

    const token: RefreshToken = {
      token: generateSecureToken(32),
      client_id: clientId,
      scope: options.scope ?? "mcp",
      user_id: options.userId,
      expires_at: now + ttl,
      created_at: now,
    };

    db.run(
      `INSERT INTO oauth_refresh_tokens 
       (token, client_id, scope, user_id, expires_at, created_at)
       VALUES (?, ?, ?, ?, ?, ?)`,
      [
        token.token,
        token.client_id,
        token.scope,
        token.user_id ?? null,
        token.expires_at,
        token.created_at,
      ]
    );

    return token;
  },

  /**
   * Get and rotate a refresh token (returns new tokens)
   */
  rotate(
    tokenValue: string,
    options: {
      accessTtl?: number;
      refreshTtl?: number;
    } = {}
  ): { accessToken: AccessToken; refreshToken: RefreshToken } | null {
    const db = getDb();
    const now = Date.now();

    const row = db
      .prepare(
        "SELECT * FROM oauth_refresh_tokens WHERE token = ? AND expires_at > ?"
      )
      .get(tokenValue, now) as {
      token: string;
      client_id: string;
      scope: string | null;
      user_id: string | null;
      expires_at: number;
      created_at: number;
    } | undefined;

    if (!row) return null;

    // Delete old refresh token (rotation)
    db.run("DELETE FROM oauth_refresh_tokens WHERE token = ?", [tokenValue]);

    // Create new tokens
    const accessToken = accessTokenStore.create(row.client_id, {
      scope: row.scope ?? undefined,
      userId: row.user_id ?? undefined,
      ttlSeconds: options.accessTtl,
    });

    const refreshToken = this.create(row.client_id, {
      scope: row.scope ?? undefined,
      userId: row.user_id ?? undefined,
      ttlSeconds: options.refreshTtl,
    });

    return { accessToken, refreshToken };
  },

  /**
   * Revoke a refresh token
   */
  revoke(tokenValue: string): boolean {
    const db = getDb();
    const result = db.run("DELETE FROM oauth_refresh_tokens WHERE token = ?", [
      tokenValue,
    ]);
    return result.changes > 0;
  },

  /**
   * Clean up expired tokens
   */
  cleanup(): number {
    const db = getDb();
    const result = db.run(
      "DELETE FROM oauth_refresh_tokens WHERE expires_at < ?",
      [Date.now()]
    );
    return result.changes;
  },
};

// =============================================================================
// Cleanup All Expired
// =============================================================================

/**
 * Clean up all expired OAuth artifacts
 */
export function cleanupExpired(): {
  codes: number;
  accessTokens: number;
  refreshTokens: number;
} {
  return {
    codes: authCodeStore.cleanup(),
    accessTokens: accessTokenStore.cleanup(),
    refreshTokens: refreshTokenStore.cleanup(),
  };
}
