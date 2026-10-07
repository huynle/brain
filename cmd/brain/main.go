// Package main is the entry point for the Brain CLI tool.
// The CLI provides commands for managing brain entries, searching, and more.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/huynle/brain-api/cmd/brain/commands"
)

// runCLI routes and executes one invocation and returns its exit status:
// 0 on success, 2 for a usage mistake (printed verbatim), 1 for any other error.
func runCLI(args []string, stderr io.Writer) int {
	cmd, err := route(args)
	if err == nil {
		err = cmd.Execute()
	}
	if err == nil {
		return 0
	}
	var usage *commands.UsageError
	if errors.As(err, &usage) {
		fmt.Fprintln(stderr, usage.Message)
		return 2
	}
	fmt.Fprintf(stderr, "Error: %v\n", err)
	return 1
}

func main() {
	// Detect invocation method via argv[0] for backward compatibility
	invoked := filepath.Base(os.Args[0])

	// Redirect legacy binary names to unified commands
	args := redirectLegacyInvocation(invoked, os.Args[1:])

	os.Exit(runCLI(args, os.Stderr))
}

// redirectLegacyInvocation redirects legacy binary names to unified commands.
//
// Supports backward compatibility via symlinks:
//   - brain-api [flags] → brain api [flags]
//   - brain [...] → brain [...] (no change)
func redirectLegacyInvocation(invoked string, args []string) []string {
	switch invoked {
	case "brain-api":
		// brain-api [flags] → brain api [flags]
		return append([]string{"api"}, args...)

	case "brain":
		// Normal invocation, no redirect
		return args

	default:
		// Unknown binary name, proceed normally
		return args
	}
}
