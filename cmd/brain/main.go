// Package main is the entry point for the Brain CLI tool.
// The CLI provides commands for managing brain entries, searching, and more.
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	// Detect invocation method via argv[0] for backward compatibility
	invoked := filepath.Base(os.Args[0])

	// Redirect legacy binary names to unified commands
	args := redirectLegacyInvocation(invoked, os.Args[1:])

	// Route command
	cmd, err := route(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Execute command
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// redirectLegacyInvocation redirects legacy binary names to unified commands.
//
// Supports backward compatibility via symlinks:
//   - brain-api [flags] → brain api [flags]
//   - brain-mcp [flags] → brain mcp [flags]
//   - brain [...] → brain [...] (no change)
func redirectLegacyInvocation(invoked string, args []string) []string {
	switch invoked {
	case "brain-api":
		// brain-api [flags] → brain api [flags]
		return append([]string{"api"}, args...)

	case "brain-mcp":
		// brain-mcp [flags] → brain mcp [flags]
		return append([]string{"mcp"}, args...)

	case "brain":
		// Normal invocation, no redirect
		return args

	default:
		// Unknown binary name, proceed normally
		return args
	}
}
