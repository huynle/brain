/**
 * Brain MCP Transport
 *
 * Hono route handler that serves MCP over Streamable HTTP at /mcp.
 * Uses stateless mode (no session tracking) with a fresh McpServer per request.
 * Supports optional OAuth 2.1 bearer token authentication.
 */

import { Hono } from "hono";
import { WebStandardStreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/webStandardStreamableHttp.js";
import { createMcpServer } from "./index";
import { conditionalAuth } from "./auth";
import type { Config } from "../core/types";

export function createMcpRoutes(config: Config): Hono {
  const mcp = new Hono();

  // Apply OAuth authentication when ENABLE_AUTH=true
  mcp.use("/mcp", conditionalAuth(config.server.enableAuth));

  mcp.post("/mcp", async (c) => {
    const server = createMcpServer();
    const transport = new WebStandardStreamableHTTPServerTransport({
      sessionIdGenerator: undefined, // stateless mode
      enableJsonResponse: true, // return JSON instead of SSE for simpler stateless usage
    });

    await server.connect(transport);

    // Pre-parse body since Hono may have consumed the stream
    let parsedBody: unknown;
    try {
      parsedBody = await c.req.json();
    } catch {
      return c.text("Invalid JSON", 400);
    }

    try {
      const response = await transport.handleRequest(c.req.raw, { parsedBody });
      return response;
    } finally {
      await transport.close();
      await server.close();
    }
  });

  // GET /mcp - not supported in stateless mode
  mcp.get("/mcp", (c) => {
    return c.json(
      {
        jsonrpc: "2.0",
        error: { code: -32000, message: "Method not allowed. Use POST." },
        id: null,
      },
      405
    );
  });

  // DELETE /mcp - not supported in stateless mode
  mcp.delete("/mcp", (c) => {
    return c.json(
      {
        jsonrpc: "2.0",
        error: { code: -32000, message: "Method not allowed." },
        id: null,
      },
      405
    );
  });

  return mcp;
}
