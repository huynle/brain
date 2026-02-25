/**
 * Brain API Service - Entry Point
 *
 * A REST API service for AI agent memory and knowledge management.
 * Extracted from the OpenCode brain plugin.
 */

import { serve, type TLSOptions } from "bun";
import { getConfig } from "./config";
import { TASK_SSE_SAFE_IDLE_TIMEOUT_SECONDS } from "./api/sse-config";
import { createApp } from "./server";

function buildTlsOptions(config: ReturnType<typeof getConfig>): TLSOptions | undefined {
  const { tls } = config.server;
  if (!tls.enabled) return undefined;

  if (!tls.keyPath || !tls.certPath) {
    console.error("TLS enabled but TLS_KEY and TLS_CERT environment variables not set");
    process.exit(1);
  }

  return {
    key: Bun.file(tls.keyPath),
    cert: Bun.file(tls.certPath),
  };
}

const config = getConfig();

console.log(`
╔══════════════════════════════════════════════════════════════╗
║                     Brain API Service                         ║
╠══════════════════════════════════════════════════════════════╣
║  A REST API for AI agent memory and knowledge management      ║
╚══════════════════════════════════════════════════════════════╝
`);

console.log(`Configuration:`);
console.log(`  Brain Dir:    ${config.brain.brainDir}`);
console.log(`  Database:     ${config.brain.dbPath}`);
console.log(`  Port:         ${config.server.port}`);
console.log(`  Host:         ${config.server.host}`);
console.log(`  Log Level:    ${config.server.logLevel}`);
console.log(`  Auth:         ${config.server.enableAuth ? "enabled" : "disabled"}`);
console.log(`  Tenants:      ${config.server.enableTenants ? "enabled" : "disabled"}`);
console.log(`  TLS:          ${config.server.tls.enabled ? "enabled" : "disabled"}`);
console.log();

const app = createApp(config);
const tlsOptions = buildTlsOptions(config);

const server = serve({
  fetch: app.fetch,
  port: config.server.port,
  hostname: config.server.host,
  tls: tlsOptions,
  idleTimeout: TASK_SSE_SAFE_IDLE_TIMEOUT_SECONDS,
});

const protocol = config.server.tls.enabled ? "https" : "http";
console.log(`🧠 Brain API listening on ${protocol}://${config.server.host}:${config.server.port}`);
console.log();
console.log(`Endpoints:`);
console.log(`  GET  /api/v1/health       - Health check`);
console.log(`  GET  /api/v1/stats        - Brain statistics`);
console.log(`  POST /api/v1/entries      - Create entry`);
console.log(`  GET  /api/v1/entries/:id  - Get entry`);
console.log(`  GET  /api/v1/entries      - List entries`);
console.log(`  POST /api/v1/search       - Search entries`);
console.log(`  ...and more`);
console.log(`  POST /mcp                 - MCP Streamable HTTP transport`);
console.log();
