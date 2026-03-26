package commands

import (
	"context"
	"os"

	"github.com/huynle/brain-api/internal/mcpserver"
)

// MCPFlags holds MCP command flags.
type MCPFlags struct {
	APIURL string
}

// MCPCommand implements the Command interface for the MCP command.
type MCPCommand struct {
	Config *UnifiedConfig
	Flags  *MCPFlags
}

// Type returns the command type identifier.
func (c *MCPCommand) Type() string {
	return "mcp"
}

// Execute starts the MCP server.
func (c *MCPCommand) Execute() error {
	// Build options from config + flags
	opts := mcpserver.MCPOptions{
		APIURL: c.Config.MCP.APIURL,
	}

	// Flags override config
	if c.Flags != nil && c.Flags.APIURL != "" {
		opts.APIURL = c.Flags.APIURL
	}

	// Create context
	ctx := context.Background()

	// Run MCP server on stdin/stdout
	return mcpserver.RunMCPServer(ctx, opts, os.Stdin, os.Stdout)
}
