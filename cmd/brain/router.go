// Package main provides command routing for the unified brain binary.
package main

import (
	"fmt"
	"github.com/huynle/brain-api/cmd/brain/commands"
	"os"
	"path/filepath"
)

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
		return newRunnerTUICommand("all", []string{})
	}

	firstArg := args[0]

	// Built-in commands take precedence
	if isBuiltinCommand(firstArg) {
		return parseBuiltinCommand(args)
	}

	// "all" keyword → runner TUI for all projects
	if firstArg == "all" {
		return newRunnerTUICommand("all", args[1:])
	}

	// Flags without a command → help
	if len(firstArg) > 0 && firstArg[0] == '-' {
		return newHelpCommand(), nil
	}

	// Check if it looks like a valid project name
	if looksLikeProjectName(firstArg) {
		return newRunnerTUICommand(firstArg, args[1:])
	}

	// Unknown → help
	return newHelpCommand(), nil
}

// =============================================================================
// Command Constructors
// =============================================================================

func newRunnerTUICommand(project string, args []string) (Command, error) {
	cfg := defaultConfig()
	flags, err := ParseRunnerFlags(args)
	if err != nil {
		return nil, err
	}

	return &commands.RunnerTUICommand{
		Project: project,
		Config:  convertToCommandsConfig(cfg),
		Flags:   convertToCommandsRunnerFlags(flags),
	}, nil
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
func parseBuiltinCommand(args []string) (Command, error) {
	if len(args) == 0 {
		return newHelpCommand(), nil
	}

	cmdName := args[0]
	cmdArgs := args[1:]

	switch cmdName {
	case "server":
		return parseServerCommand(cmdArgs)
	case "start":
		return parseStartCommand(cmdArgs)
	case "stop":
		return parseStopCommand(cmdArgs)
	case "restart":
		return parseRestartCommand(cmdArgs)
	case "status":
		return parseStatusCommand(cmdArgs)
	case "health":
		return parseHealthCommand(cmdArgs)
	case "logs":
		return parseLogsCommand(cmdArgs)
		return parseRestartCommand(cmdArgs)
	case "mcp":
		return parseMCPCommand(cmdArgs)
	case "token":
		return parseTokenCommand(cmdArgs)
	case "run":
		// Handle "brain run <subcommand>" pattern
		if len(cmdArgs) > 0 {
			return parseRunCommand(cmdArgs)
		}
		// "brain run" without subcommand returns stub
		return &stubCommand{cmdType: "run"}, nil
	case "help":
		return newHelpCommand(), nil
	default:
		// For other built-in commands, return stub for now
		return &stubCommand{cmdType: cmdName}, nil
	}
}

// parseServerCommand creates a ServerCommand from args.
func parseServerCommand(args []string) (Command, error) {
	cfg := defaultConfig()
	flags, err := ParseServerFlags(args)
	if err != nil {
		return nil, err
	}

	return &commands.ServerCommand{
		Config: convertToCommandsConfig(cfg),
		Flags:  convertToCommandsServerFlags(flags),
	}, nil
}

// parseMCPCommand creates an MCPCommand from args.
func parseMCPCommand(args []string) (Command, error) {
	cfg := defaultConfig()
	flags, err := ParseMCPFlags(args)
	if err != nil {
		return nil, err
	}

	return &commands.MCPCommand{
		Config: convertToCommandsConfig(cfg),
		Flags:  convertToCommandsMCPFlags(flags),
	}, nil
}

// parseTokenCommand creates a TokenCommand from args.
func parseTokenCommand(args []string) (Command, error) {
	if len(args) == 0 {
		return newHelpCommand(), nil
	}

	subcommand := args[0]
	subArgs := args[1:]

	cfg := defaultConfig()
	flags, err := ParseTokenFlags(subArgs)
	if err != nil {
		return nil, err
	}

	// Get name from remaining args if not from flags
	name := flags.Name
	if name == "" && len(subArgs) > 0 && !isFlag(subArgs[0]) {
		name = subArgs[0]
	}

	return &commands.TokenCommand{
		Subcommand: subcommand,
		Name:       name,
		Config:     convertToCommandsConfig(cfg),
		Flags:      convertToCommandsTokenFlags(flags),
	}, nil
}

// parseRunCommand creates a RunCommand from args.
func parseRunCommand(args []string) (Command, error) {
	if len(args) == 0 {
		return newHelpCommand(), nil
	}

	subcommand := args[0]
	subArgs := args[1:]

	cfg := defaultConfig()
	flags, err := ParseRunnerFlags(subArgs)
	if err != nil {
		return nil, err
	}

	// Determine project from subArgs or default to "all"
	project := "all"
	if len(subArgs) > 0 && !isFlag(subArgs[0]) {
		project = subArgs[0]
	}

	return &commands.RunCommand{
		Subcommand: subcommand,
		Project:    project,
		Config:     convertToCommandsConfig(cfg),
		Flags:      convertToCommandsRunnerFlags(flags),
	}, nil
}

// isFlag checks if a string looks like a flag.
func isFlag(s string) bool {
	return len(s) > 0 && s[0] == '-'
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

// =============================================================================
// Config and Conversion Helpers
// =============================================================================

// defaultConfig returns a default UnifiedConfig.
// In future phases, this will load from config files.
func defaultConfig() *UnifiedConfig {
	cfg := &UnifiedConfig{}

	// Server defaults
	cfg.Server.Port = 3333
	cfg.Server.Host = "localhost"
	cfg.Server.BrainDir = "~/brain" // Will be expanded
	cfg.Server.LogLevel = "info"

	homeDir, _ := os.UserHomeDir()
	cfg.Server.PIDFile = filepath.Join(homeDir, ".local", "state", "brain-api", "brain-api.pid")
	cfg.Server.LogFile = filepath.Join(homeDir, ".local", "state", "brain-api", "brain-api.log")

	// Runner defaults
	cfg.Runner.MaxParallel = 3
	cfg.Runner.PollInterval = 10
	cfg.Runner.WorkDir = ""

	// MCP defaults
	cfg.MCP.APIURL = "http://localhost:3333"

	return cfg
}

// convertToCommandsConfig converts main.UnifiedConfig to commands.UnifiedConfig.
func convertToCommandsConfig(cfg *UnifiedConfig) *commands.UnifiedConfig {
	cmdCfg := &commands.UnifiedConfig{}

	// Server
	// Server
	cmdCfg.Server.Port = cfg.Server.Port
	cmdCfg.Server.Host = cfg.Server.Host
	cmdCfg.Server.BrainDir = cfg.Server.BrainDir
	cmdCfg.Server.EnableAuth = cfg.Server.EnableAuth
	cmdCfg.Server.APIKey = cfg.Server.APIKey
	cmdCfg.Server.LogLevel = cfg.Server.LogLevel
	cmdCfg.Server.PIDFile = cfg.Server.PIDFile
	cmdCfg.Server.LogFile = cfg.Server.LogFile
	cmdCfg.Server.TLS.Enabled = cfg.Server.TLS.Enabled
	cmdCfg.Server.TLS.CertPath = cfg.Server.TLS.CertPath
	cmdCfg.Server.TLS.KeyPath = cfg.Server.TLS.KeyPath
	// Runner
	cmdCfg.Runner.MaxParallel = cfg.Runner.MaxParallel
	cmdCfg.Runner.PollInterval = cfg.Runner.PollInterval
	cmdCfg.Runner.WorkDir = cfg.Runner.WorkDir
	cmdCfg.Runner.StateDir = cfg.Runner.StateDir
	cmdCfg.Runner.LogDir = cfg.Runner.LogDir
	cmdCfg.Runner.ExcludeProjects = cfg.Runner.ExcludeProjects
	cmdCfg.Runner.OpenCode.Agent = cfg.Runner.OpenCode.Agent
	cmdCfg.Runner.OpenCode.Model = cfg.Runner.OpenCode.Model

	// MCP
	cmdCfg.MCP.APIURL = cfg.MCP.APIURL

	return cmdCfg
}

// convertToCommandsServerFlags converts main.ServerFlags to commands.ServerFlags.
func convertToCommandsServerFlags(flags *ServerFlags) *commands.ServerFlags {
	return &commands.ServerFlags{
		Port:    flags.Port,
		Host:    flags.Host,
		Daemon:  flags.Daemon,
		LogFile: flags.LogFile,
		TLS:     flags.TLS,
		TLSCert: flags.TLSCert,
		TLSKey:  flags.TLSKey,
	}
}

// convertToCommandsRunnerFlags converts main.RunnerFlags to commands.RunnerFlags.
func convertToCommandsRunnerFlags(flags *RunnerFlags) *commands.RunnerFlags {
	return &commands.RunnerFlags{
		TUI:          flags.TUI,
		Foreground:   flags.Foreground,
		Background:   flags.Background,
		Dashboard:    flags.Dashboard,
		MaxParallel:  flags.MaxParallel,
		PollInterval: flags.PollInterval,
		Workdir:      flags.Workdir,
		Agent:        flags.Agent,
		Model:        flags.Model,
		Include:      flags.Include,
		Exclude:      flags.Exclude,
		Follow:       flags.Follow,
	}
}

// convertToCommandsMCPFlags converts main.MCPFlags to commands.MCPFlags.
func convertToCommandsMCPFlags(flags *MCPFlags) *commands.MCPFlags {
	return &commands.MCPFlags{
		APIURL: flags.APIURL,
	}
}

func convertToCommandsTokenFlags(flags *TokenFlags) *commands.TokenFlags {
	return &commands.TokenFlags{
		Name: flags.Name,
	}
}

// parseStartCommand creates a StartCommand from args.
func parseStartCommand(args []string) (Command, error) {
	cfg := defaultConfig()
	flags, err := ParseLifecycleFlags(args)
	if err != nil {
		return nil, err
	}

	return &commands.StartCommand{
		Config: convertToCommandsConfig(cfg),
		Flags:  convertToCommandsLifecycleFlags(flags),
	}, nil
}

// parseStopCommand creates a StopCommand from args.
func parseStopCommand(args []string) (Command, error) {
	cfg := defaultConfig()
	flags, err := ParseLifecycleFlags(args)
	if err != nil {
		return nil, err
	}

	return &commands.StopCommand{
		Config: convertToCommandsConfig(cfg),
		Flags:  convertToCommandsLifecycleFlags(flags),
	}, nil
}

// parseRestartCommand creates a RestartCommand from args.
func parseRestartCommand(args []string) (Command, error) {
	cfg := defaultConfig()
	flags, err := ParseLifecycleFlags(args)
	if err != nil {
		return nil, err
	}

	return &commands.RestartCommand{
		Config: convertToCommandsConfig(cfg),
		Flags:  convertToCommandsLifecycleFlags(flags),
	}, nil
}

// parseStatusCommand creates a StatusCommand from args.
func parseStatusCommand(args []string) (Command, error) {
	cfg := defaultConfig()
	
	// Parse --json flag
	jsonFlag := false
	for _, arg := range args {
		if arg == "--json" {
			jsonFlag = true
		}
	}

	return &commands.StatusCommand{
		Config: convertToCommandsConfig(cfg),
		Flags:  &commands.StatusFlags{JSON: jsonFlag},
		Out:    nil, // Will use os.Stdout in Execute if nil
	}, nil
}

// parseHealthCommand creates a HealthCommand from args.
func parseHealthCommand(args []string) (Command, error) {
	cfg := defaultConfig()
	
	// Parse flags
	waitFlag := false
	timeout := 30
	for i, arg := range args {
		if arg == "--wait" {
			waitFlag = true
		}
		if arg == "--timeout" && i+1 < len(args) {
			// Parse timeout (simplified)
			fmt.Sscanf(args[i+1], "%d", &timeout)
		}
	}

	return &commands.HealthCommand{
		Config: convertToCommandsConfig(cfg),
		Flags:  &commands.HealthFlags{Wait: waitFlag, Timeout: timeout},
		Out:    nil, // Will use os.Stdout in Execute if nil
	}, nil
}

// parseLogsCommand creates a LogsCommand from args.
func parseLogsCommand(args []string) (Command, error) {
	cfg := defaultConfig()
	
	// Parse flags
	followFlag := false
	lines := 100
	for i, arg := range args {
		if arg == "-f" || arg == "--follow" {
			followFlag = true
		}
		if (arg == "-n" || arg == "--lines") && i+1 < len(args) {
			fmt.Sscanf(args[i+1], "%d", &lines)
		}
	}

	return &commands.LogsCommand{
		Config: convertToCommandsConfig(cfg),
		Flags:  &commands.LogsFlags{Follow: followFlag, Lines: lines},
		Out:    nil, // Will use os.Stdout in Execute if nil
	}, nil
}
