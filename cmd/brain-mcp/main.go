// Package main is the entry point for the Brain MCP (Model Context Protocol) server.
//
// This server exposes Brain API tools to AI editors like Claude Code via the
// MCP protocol (JSON-RPC 2.0 over stdin/stdout with Content-Length framing).
//
// Usage:
//
//	Add to your Claude Code MCP config:
//	{
//	  "mcpServers": {
//	    "brain": {
//	      "command": "brain-mcp",
//	      "env": { "BRAIN_API_URL": "http://localhost:3333" }
//	    }
//	  }
//	}
//
// Environment variables:
//   - BRAIN_API_URL: Base URL for the Brain API (default: http://localhost:3333)
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/huynle/brain-api/internal/mcp"
)

func main() {
	// Create MCP server
	server := mcp.NewServer()

	// Create API client (reads BRAIN_API_URL from env, defaults to localhost:3333)
	client := mcp.NewAPIClient(mcp.DefaultBaseURL())

	// Register all tool groups
	mcp.RegisterBrainTools(server, client)
	mcp.RegisterTaskTools(server, client)
	mcp.RegisterPlanningTools(server, client)

	// Set up graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Run the server (reads from stdin, writes to stdout)
	if err := server.Serve(ctx, os.Stdin, os.Stdout); err != nil {
		// io.EOF is normal termination (stdin closed)
		if err.Error() == "EOF" {
			return
		}
		log.Fatalf("MCP server error: %v", err)
	}
}
