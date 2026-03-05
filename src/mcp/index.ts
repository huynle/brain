/**
 * Brain MCP Server Factory
 *
 * Creates an McpServer instance with all brain tools registered.
 */

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { registerBrainTools } from "./tools";

export function createMcpServer(): McpServer {
  const server = new McpServer({
    name: "brain-mcp",
    version: "1.0.0",
  });
  registerBrainTools(server);
  return server;
}
