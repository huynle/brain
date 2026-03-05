/**
 * Brain Token CLI - Manage API tokens
 *
 * Routes token operations through the running server's API when available.
 * This avoids SQLite WAL visibility issues that occur when a CLI process
 * writes tokens via a separate database connection — the long-running
 * server may not see the new rows without a restart.
 *
 * Fallback: If the server is not running, falls back to direct DB writes.
 * This is necessary for initial bootstrap (creating the first token before
 * any server is running).
 */

import {
  createApiToken,
  listApiTokens,
  revokeApiToken,
} from "../auth";
import type { ApiTokenInfo } from "../auth";

// =============================================================================
// Types
// =============================================================================

export interface TokenCommandResult {
  exitCode: number;
  output: string;
}

interface ApiTokenResponse {
  token: string;
  name: string;
  createdAt: number;
}

interface ApiTokenListResponse {
  tokens: ApiTokenInfo[];
  count: number;
  active: number;
  revoked: number;
}

// =============================================================================
// Server URL Resolution
// =============================================================================

const DEFAULT_PORT = process.env.BRAIN_PORT || process.env.PORT || "3333";

function getServerUrl(): string {
  return `http://localhost:${DEFAULT_PORT}`;
}

/**
 * Check if the brain API server is running and reachable.
 * Uses the unauthenticated health endpoint.
 *
 * Set BRAIN_TOKEN_DIRECT_DB=1 to force direct DB mode (used in tests).
 */
async function isServerRunning(): Promise<boolean> {
  if (process.env.BRAIN_TOKEN_DIRECT_DB === "1") {
    return false;
  }
  try {
    const response = await fetch(`${getServerUrl()}/api/v1/health`, {
      signal: AbortSignal.timeout(2000),
    });
    return response.ok;
  } catch {
    return false;
  }
}

// =============================================================================
// API-based Operations (preferred — uses server's DB connection)
// =============================================================================

async function apiCreateToken(
  name: string,
  apiToken?: string
): Promise<ApiTokenResponse> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (apiToken) {
    headers["Authorization"] = `Bearer ${apiToken}`;
  }

  const response = await fetch(`${getServerUrl()}/api/v1/tokens`, {
    method: "POST",
    headers,
    body: JSON.stringify({ name }),
    signal: AbortSignal.timeout(5000),
  });

  if (!response.ok) {
    const body = await response.json().catch(() => ({ message: response.statusText }));
    throw new Error((body as { message?: string }).message || `HTTP ${response.status}`);
  }

  return (await response.json()) as ApiTokenResponse;
}

async function apiListTokens(apiToken?: string): Promise<ApiTokenListResponse> {
  const headers: Record<string, string> = {};
  if (apiToken) {
    headers["Authorization"] = `Bearer ${apiToken}`;
  }

  const response = await fetch(`${getServerUrl()}/api/v1/tokens`, {
    headers,
    signal: AbortSignal.timeout(5000),
  });

  if (!response.ok) {
    const body = await response.json().catch(() => ({ message: response.statusText }));
    throw new Error((body as { message?: string }).message || `HTTP ${response.status}`);
  }

  return (await response.json()) as ApiTokenListResponse;
}

async function apiRevokeToken(
  name: string,
  apiToken?: string
): Promise<boolean> {
  const headers: Record<string, string> = {};
  if (apiToken) {
    headers["Authorization"] = `Bearer ${apiToken}`;
  }

  const response = await fetch(
    `${getServerUrl()}/api/v1/tokens/${encodeURIComponent(name)}`,
    {
      method: "DELETE",
      headers,
      signal: AbortSignal.timeout(5000),
    }
  );

  if (response.status === 404) return false;

  if (!response.ok) {
    const body = await response.json().catch(() => ({ message: response.statusText }));
    throw new Error((body as { message?: string }).message || `HTTP ${response.status}`);
  }

  return true;
}

// =============================================================================
// Token Help
// =============================================================================

function tokenHelp(): string {
  return `Usage: brain token <command> [options]

Commands:
  token create --name <name>   Create a new API token
  token list                   List all API tokens
  token revoke <name>          Revoke an API token

When the Brain API server is running, token operations go through the API
to ensure immediate visibility. Falls back to direct DB access otherwise.`;
}

// =============================================================================
// Token Create
// =============================================================================

async function tokenCreate(args: string[]): Promise<TokenCommandResult> {
  // Parse name from --name flag or positional arg
  const nameIdx = args.indexOf("--name");
  let name: string | undefined;

  if (nameIdx !== -1 && args[nameIdx + 1]) {
    name = args[nameIdx + 1];
  } else if (args[0] && !args[0].startsWith("-")) {
    name = args[0];
  }

  if (!name) {
    return { exitCode: 1, output: "Error: name is required\n\n" + tokenHelp() };
  }

  // Read existing token from env for authenticated API calls
  const existingToken = process.env.BRAIN_API_TOKEN;

  try {
    let result: { token: string; name: string };
    let via: string;

    const serverUp = await isServerRunning();

    if (serverUp) {
      result = await apiCreateToken(name, existingToken);
      via = "API";
    } else {
      // Fallback to direct DB (bootstrap scenario)
      result = createApiToken(name);
      via = "database (server not running)";
    }

    const output = `API Token created (via ${via}):

  Name:  ${result.name}
  Token: ${result.token}

Save this token — it cannot be displayed again.

Usage:
  export BRAIN_API_TOKEN=${result.token}
  curl -H "Authorization: Bearer ${result.token}" http://localhost:${DEFAULT_PORT}/api/v1/health`;

    return { exitCode: 0, output };
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : String(err);
    return { exitCode: 1, output: `Error: Failed to create token — ${message}` };
  }
}

// =============================================================================
// Token List
// =============================================================================

async function tokenList(): Promise<TokenCommandResult> {
  const existingToken = process.env.BRAIN_API_TOKEN;

  let tokens: ApiTokenInfo[];

  try {
    const serverUp = await isServerRunning();

    if (serverUp) {
      const response = await apiListTokens(existingToken);
      tokens = response.tokens;
    } else {
      tokens = listApiTokens();
    }
  } catch {
    // If API fails, fall back to direct DB
    tokens = listApiTokens();
  }

  if (tokens.length === 0) {
    return { exitCode: 0, output: "No API tokens found." };
  }

  // Build table
  const lines: string[] = [];

  // Header
  const header = padRow("NAME", "CREATED", "LAST USED", "STATUS", "TOKEN");
  lines.push(header);
  lines.push("-".repeat(header.length));

  for (const t of tokens) {
    const created = new Date(t.createdAt).toLocaleString();
    const lastUsed = t.lastUsedAt ? new Date(t.lastUsedAt).toLocaleString() : "never";
    const status = t.revokedAt ? "revoked" : "active";
    const tokenCol = `${t.tokenPrefix}...`;

    lines.push(padRow(t.name, created, lastUsed, status, tokenCol));
  }

  // Summary
  const active = tokens.filter((t) => !t.revokedAt).length;
  const revoked = tokens.filter((t) => t.revokedAt).length;
  lines.push("");
  lines.push(`${tokens.length} tokens (${active} active, ${revoked} revoked)`);

  return { exitCode: 0, output: lines.join("\n") };
}

function padRow(name: string, created: string, lastUsed: string, status: string, token: string): string {
  return `${name.padEnd(20)} ${created.padEnd(24)} ${lastUsed.padEnd(24)} ${status.padEnd(10)} ${token}`;
}

// =============================================================================
// Token Revoke
// =============================================================================

async function tokenRevoke(args: string[]): Promise<TokenCommandResult> {
  const name = args[0];

  if (!name) {
    return { exitCode: 1, output: "Error: name is required\n\n" + tokenHelp() };
  }

  const existingToken = process.env.BRAIN_API_TOKEN;

  try {
    let revoked: boolean;

    const serverUp = await isServerRunning();

    if (serverUp) {
      revoked = await apiRevokeToken(name, existingToken);
    } else {
      revoked = revokeApiToken(name);
    }

    if (revoked) {
      return { exitCode: 0, output: `Revoked token: ${name}` };
    } else {
      return { exitCode: 1, output: `Token not found: ${name}` };
    }
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : String(err);
    return { exitCode: 1, output: `Error: Failed to revoke token — ${message}` };
  }
}

// =============================================================================
// Main Entry Point
// =============================================================================

export async function execTokenCommand(args: string[]): Promise<TokenCommandResult> {
  const subcommand = args[0];

  switch (subcommand) {
    case "create":
      return tokenCreate(args.slice(1));
    case "list":
    case "ls":
      return tokenList();
    case "revoke":
      return tokenRevoke(args.slice(1));
    default:
      return { exitCode: 1, output: tokenHelp() };
  }
}
