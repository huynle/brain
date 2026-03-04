/**
 * Brain Token CLI - Manage API tokens
 *
 * Extracted as a testable module from brain.ts CLI.
 * Returns structured results instead of printing directly.
 */

import {
  createApiToken,
  listApiTokens,
  revokeApiToken,
} from "../auth";

// =============================================================================
// Types
// =============================================================================

export interface TokenCommandResult {
  exitCode: number;
  output: string;
}

// =============================================================================
// Token Help
// =============================================================================

function tokenHelp(): string {
  return `Usage: brain token <command> [options]

Commands:
  token create --name <name>   Create a new API token
  token list                   List all API tokens
  token revoke <name>          Revoke an API token`;
}

// =============================================================================
// Token Create
// =============================================================================

function tokenCreate(args: string[]): TokenCommandResult {
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

  try {
    const result = createApiToken(name);

    const output = `API Token created:

  Name:  ${result.name}
  Token: ${result.token}

Save this token — it cannot be displayed again.

Usage:
  export BRAIN_API_TOKEN=${result.token}
  curl -H "Authorization: Bearer ${result.token}" https://brain.huynle.com/api/v1/health`;

    return { exitCode: 0, output };
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : String(err);
    return { exitCode: 1, output: `Error: Failed to create token — ${message}` };
  }
}

// =============================================================================
// Token List
// =============================================================================

function tokenList(): TokenCommandResult {
  const tokens = listApiTokens();

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

function tokenRevoke(args: string[]): TokenCommandResult {
  const name = args[0];

  if (!name) {
    return { exitCode: 1, output: "Error: name is required\n\n" + tokenHelp() };
  }

  const revoked = revokeApiToken(name);

  if (revoked) {
    return { exitCode: 0, output: `Revoked token: ${name}` };
  } else {
    return { exitCode: 1, output: `Token not found: ${name}` };
  }
}

// =============================================================================
// Main Entry Point
// =============================================================================

export function execTokenCommand(args: string[]): TokenCommandResult {
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
