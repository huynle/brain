package mcpserver

import (
	"context"
	"fmt"
	"io"

	"github.com/huynle/brain-api/internal/mcp"
)

// MCPOptions holds configuration for the MCP server.
type MCPOptions struct {
	APIURL string
}

// RunMCPServer starts the MCP server and blocks until context is cancelled or stdin closes.
func RunMCPServer(ctx context.Context, opts MCPOptions, stdin io.Reader, stdout io.Writer) error {
	if opts.APIURL == "" {
		return fmt.Errorf("BRAIN_API_URL is required")
	}

	// Create MCP server. This transport is a child process of the client, so
	// tool arguments naming local paths refer to files this process can open.
	server := mcp.NewServer(mcp.WithLocalFilesystem())

	// Create API client
	client := mcp.NewAPIClient(opts.APIURL)

	// Register all tool groups
	mcp.RegisterBrainTools(server, client)
	mcp.RegisterTaskTools(server, client)
	mcp.RegisterFeatureTools(server, client)
	mcp.RegisterRunnerTools(server, client)
	mcp.RegisterObservabilityTools(server, client)
	mcp.RegisterControlTools(server, client)
	mcp.RegisterProjectTools(server, client)
	mcp.RegisterPlanningTools(server, client)
	mcp.RegisterWebhookTools(server, client)
	mcp.RegisterGoalTools(server, client)
	mcp.RegisterReminderTools(server, client)

	// Run the server (reads from stdin, writes to stdout)
	if err := server.Serve(ctx, stdin, stdout); err != nil {
		// io.EOF is normal termination (stdin closed)
		if err == io.EOF || err.Error() == "EOF" {
			return nil
		}
		return fmt.Errorf("MCP server error: %w", err)
	}

	return nil
}
