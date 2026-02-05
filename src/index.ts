/**
 * Brain API Service - Entry Point
 *
 * A REST API service for AI agent memory and knowledge management.
 * Extracted from the OpenCode brain plugin.
 */

import { serve } from "bun";
import { getConfig } from "./config";
import { createApp } from "./server";

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
console.log();

const app = createApp(config);

const server = serve({
  fetch: app.fetch,
  port: config.server.port,
  hostname: config.server.host,
});

console.log(`🧠 Brain API listening on http://${config.server.host}:${config.server.port}`);
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
