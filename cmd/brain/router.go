// Package main provides command routing for the unified brain binary.
package main

// =============================================================================
// Command Interface
// =============================================================================

// Command represents an executable command with a type identifier.
type Command interface {
	Execute() error
	Type() string
}

// =============================================================================
// Stub Command (temporary, will be replaced with actual implementations)
// =============================================================================

// stubCommand is a placeholder command for testing.
// In later phases, this will be replaced with actual command implementations.
type stubCommand struct {
	cmdType string
}

func (c *stubCommand) Execute() error {
	return nil
}

func (c *stubCommand) Type() string {
	return c.cmdType
}

// =============================================================================
// Built-in Commands Registry
// =============================================================================

// builtinCommands is the set of recognized built-in commands.
// These commands take precedence over project names.
var builtinCommands = map[string]bool{
	"server":        true,
	"mcp":           true,
	"run":           true,
	"start":         true,
	"stop":          true,
	"restart":       true,
	"status":        true,
	"health":        true,
	"logs":          true,
	"dev":           true,
	"init":          true,
	"doctor":        true,
	"config":        true,
	"install":       true,
	"uninstall":     true,
	"plugin-status": true,
	"token":         true,
	"help":          true,
}

// =============================================================================
// Main Routing Logic
// =============================================================================

// route determines which command to execute based on CLI arguments.
//
// Routing priority:
//  1. Zero args → runner TUI for all projects
//  2. Built-in commands (server, mcp, etc.)
//  3. "all" keyword → runner TUI for all projects
//  4. Valid project name → runner TUI for that project
//  5. Unknown/invalid input → help
func route(args []string) (Command, error) {
	// Zero args → runner TUI for all projects
	if len(args) == 0 {
		return newRunnerTUICommand(), nil
	}

	firstArg := args[0]

	// Built-in commands take precedence
	if isBuiltinCommand(firstArg) {
		return parseBuiltinCommand(args)
	}

	// "all" keyword → runner TUI for all projects
	if firstArg == "all" {
		return newRunnerTUICommand(), nil
	}

	// Flags without a command → help
	if len(firstArg) > 0 && firstArg[0] == '-' {
		return newHelpCommand(), nil
	}

	// Check if it looks like a valid project name
	if looksLikeProjectName(firstArg) {
		return newRunnerTUICommand(), nil
	}

	// Unknown → help
	return newHelpCommand(), nil
}

// =============================================================================
// Command Constructors
// =============================================================================

func newRunnerTUICommand() Command {
	return &stubCommand{cmdType: "runner_tui"}
}

func newHelpCommand() Command {
	return &stubCommand{cmdType: "help"}
}

// =============================================================================
// Built-in Command Handling
// =============================================================================

// isBuiltinCommand checks if a command string is a built-in command.
func isBuiltinCommand(cmd string) bool {
	return builtinCommands[cmd]
}

// parseBuiltinCommand parses and creates a built-in command.
// Returns a stub command for now; will be replaced with actual implementations.
func parseBuiltinCommand(args []string) (Command, error) {
	if len(args) == 0 {
		return newHelpCommand(), nil
	}
	// Return a stub command with the same type as the command name
	return &stubCommand{cmdType: args[0]}, nil
}

// =============================================================================
// Project Name Validation
// =============================================================================

// looksLikeProjectName checks if a string matches typical project name patterns.
//
// Valid project names are lowercase alphanumeric with hyphens/underscores.
// We exclude strings ending with 5+ consecutive digits, as those are more likely
// to be test strings or invalid identifiers (e.g., "unknown-command-12345").
func looksLikeProjectName(s string) bool {
	if s == "" {
		return false
	}

	// Check if string ends with 5+ consecutive digits (likely a test string)
	digitCount := 0
	for i := len(s) - 1; i >= 0 && digitCount < 5; i-- {
		if s[i] >= '0' && s[i] <= '9' {
			digitCount++
		} else {
			break
		}
	}
	if digitCount >= 5 {
		return false
	}

	// Check if all characters are valid project name characters
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}

	return true
}
