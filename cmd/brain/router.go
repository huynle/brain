// Package main provides command routing for the unified brain binary.
package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/huynle/brain-api/cmd/brain/commands"
	uconfig "github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/pkg/pathutil"
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
// Help Command
// =============================================================================

// HelpCommand displays help information.
type HelpCommand struct {
	command string // specific command to show help for (empty = main help)
}

func (c *HelpCommand) Execute() error {
	ShowHelp(c.command)
	return nil
}

func (c *HelpCommand) Type() string {
	return "help"
}

// =============================================================================
// Built-in Commands Registry
// =============================================================================

// builtinCommands is the set of recognized built-in commands.
// These commands take precedence over project names.
var builtinCommands = map[string]bool{
	"api":           true,
	"mcp":           true,
	"run":           true,
	"runner":        true, // alias for "run" (backwards compat with old Node.js CLI)
	"start":         true, // start runner TUI for a project
	"stop":          true, // stop runner for a project
	"dev":           true,
	"init":          true,
	"doctor":        true,
	"config":        true,
	"install":       true,
	"uninstall":     true,
	"plugin-status": true,
	"token":         true,
	"dream":         true,
	"save":          true,
	"get":           true,
	"cat":           true, // alias for "get"
	"update":        true,
	"edit":          true,
	"search":        true,
	"list":          true,
	"help":          true,
}

// =============================================================================
// Main Routing Logic
// =============================================================================

// route determines which command to execute based on CLI arguments.
//
// Routing priority:
//  1. Zero args → help
//  2. Built-in commands (api, start, mcp, etc.)
//  3. Unknown/invalid input → help
//
// Use "brain start <project>" or "brain start all" to launch the runner TUI.
func route(args []string) (Command, error) {
	// Zero args → help
	if len(args) == 0 {
		return newHelpCommand(), nil
	}

	firstArg := args[0]

	// Built-in commands
	if isBuiltinCommand(firstArg) {
		return parseBuiltinCommand(args)
	}

	// Flags without a command → help
	if len(firstArg) > 0 && firstArg[0] == '-' {
		return newHelpCommand(), nil
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
	return &HelpCommand{command: ""}
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
	case "api":
		return parseAPICommand(cmdArgs)
	case "start":
		if wantsHelp(cmdArgs) {
			return &HelpCommand{command: "start"}, nil
		}
		// "brain start <project>" → runner TUI for project
		// "brain start all" → runner TUI for all projects
		// "brain start" (no args) → runner TUI for all projects
		if len(cmdArgs) == 0 {
			return newRunnerTUICommand("all", []string{})
		}
		return newRunnerTUICommand(cmdArgs[0], cmdArgs[1:])
	case "stop":
		if wantsHelp(cmdArgs) {
			return &HelpCommand{command: "stop"}, nil
		}
		// "brain stop <project>" → stop runner for project (stub for now)
		return parseStopCommand(cmdArgs)
	case "init":
		if wantsHelp(cmdArgs) {
			return &HelpCommand{command: "init"}, nil
		}
		return parseInitCommand(cmdArgs)
	case "doctor":
		if wantsHelp(cmdArgs) {
			return &HelpCommand{command: "doctor"}, nil
		}
		return parseDoctorCommand(cmdArgs)
	case "config":
		if wantsHelp(cmdArgs) {
			return &HelpCommand{command: "config"}, nil
		}
		return parseConfigCommand(cmdArgs)
	case "mcp":
		if wantsHelp(cmdArgs) {
			return &HelpCommand{command: "mcp"}, nil
		}
		return parseMCPCommand(cmdArgs)
	case "token":
		return parseTokenCommand(cmdArgs)
	case "dream":
		if wantsHelp(cmdArgs) {
			return &HelpCommand{command: "dream"}, nil
		}
		return parseDreamCommand(cmdArgs)
	case "save":
		if wantsHelp(cmdArgs) {
			return &HelpCommand{command: "save"}, nil
		}
		return parseSaveCommand(cmdArgs)
	case "get", "cat":
		if wantsHelp(cmdArgs) {
			return &HelpCommand{command: "get"}, nil
		}
		return parseGetCommand(cmdArgs)
	case "update":
		if wantsHelp(cmdArgs) {
			return &HelpCommand{command: "update"}, nil
		}
		return parseUpdateCommand(cmdArgs)
	case "edit":
		if wantsHelp(cmdArgs) {
			return &HelpCommand{command: "edit"}, nil
		}
		return parseEditCommand(cmdArgs)
	case "search":
		if wantsHelp(cmdArgs) {
			return &HelpCommand{command: "search"}, nil
		}
		return parseSearchCommand(cmdArgs)
	case "list":
		if wantsHelp(cmdArgs) {
			return &HelpCommand{command: "list"}, nil
		}
		return parseListCommand(cmdArgs)
	case "install":
		if wantsHelp(cmdArgs) {
			return &HelpCommand{command: "install"}, nil
		}
		return parseInstallCommand(cmdArgs)
	case "uninstall":
		if wantsHelp(cmdArgs) {
			return &HelpCommand{command: "uninstall"}, nil
		}
		return parseUninstallCommand(cmdArgs)
	case "plugin-status":
		if wantsHelp(cmdArgs) {
			return &HelpCommand{command: "plugin-status"}, nil
		}
		return parsePluginStatusCommand(cmdArgs)
	case "run", "runner":
		if len(cmdArgs) == 0 {
			return &stubCommand{cmdType: "run"}, nil
		}
		if isHelpArg(cmdArgs[0]) {
			return &HelpCommand{command: "run"}, nil
		}
		// Handle "brain run <subcommand>" and "brain runner <subcommand>" patterns
		// "runner" is a backwards-compat alias for "run" (from old Node.js CLI)
		return parseRunCommand(cmdArgs)
	case "help":
		// "brain help server" / "brain help server start" → show contextual help
		topic := strings.TrimSpace(strings.Join(cmdArgs, " "))
		return &HelpCommand{command: topic}, nil
	default:
		// For other built-in commands, return stub for now
		return &stubCommand{cmdType: cmdName}, nil
	}
}

// apiSubcommands maps api subcommand names to their parse functions.
var apiSubcommands = map[string]func([]string) (Command, error){
	"start":   parseStartCommand,
	"stop":    parseStopCommand,
	"restart": parseRestartCommand,
	"status":  parseStatusCommand,
	"logs":    parseLogsCommand,
	"health":  parseHealthCommand,
}

// parseAPICommand creates an APICommand from args, or delegates to an
// api subcommand (start/stop/restart/status/logs/health).
func parseAPICommand(args []string) (Command, error) {
	if len(args) > 0 && isHelpArg(args[0]) {
		return &HelpCommand{command: "api"}, nil
	}

	// Check if the first arg is a known subcommand
	if len(args) > 0 {
		if parseFn, ok := apiSubcommands[args[0]]; ok {
			if wantsHelp(args[1:]) {
				return &HelpCommand{command: "api " + args[0]}, nil
			}
			return parseFn(args[1:])
		}
	}

	if wantsHelp(args) {
		return &HelpCommand{command: "api"}, nil
	}

	// Default: start API server in foreground
	cfg := defaultConfig()
	flags, err := ParseAPIFlags(args)
	if err != nil {
		return nil, err
	}

	return &commands.APICommand{
		Config: convertToCommandsConfig(cfg),
		Flags:  convertToCommandsAPIFlags(flags),
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
		return &commands.TokenCommand{
			Subcommand: "",
			Config:     convertToCommandsConfig(defaultConfig()),
			Flags:      &commands.TokenFlags{},
		}, nil
	}

	subcommand := args[0]
	if isHelpArg(subcommand) {
		return &HelpCommand{command: "token"}, nil
	}
	if len(args) > 1 && wantsHelp(args[1:]) {
		return &HelpCommand{command: "token " + subcommand}, nil
	}
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
		return &HelpCommand{command: "run"}, nil
	}

	subcommand := args[0]
	if isHelpArg(subcommand) {
		return &HelpCommand{command: "run"}, nil
	}
	if len(args) > 1 && wantsHelp(args[1:]) {
		return &HelpCommand{command: "run " + subcommand}, nil
	}
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

func isHelpArg(s string) bool {
	return s == "--help" || s == "-h" || s == "help"
}

func wantsHelp(args []string) bool {
	for _, arg := range args {
		if isHelpArg(arg) {
			return true
		}
	}
	return false
}

// =============================================================================
// Project Name Validation
// =============================================================================

// =============================================================================
// Config and Conversion Helpers
// =============================================================================

// defaultConfig returns a UnifiedConfig populated from config file, env vars, and built-in defaults.
// Config loading priority: CLI flags > env vars > config file > built-in defaults.
func defaultConfig() *UnifiedConfig {
	cfg := &UnifiedConfig{}

	// Server defaults
	cfg.Server.Port = 3333
	cfg.Server.Host = "localhost"
	cfg.Server.BrainDir = pathutil.ExpandTilde("~/brain")
	cfg.Server.LogLevel = "info"

	homeDir, _ := os.UserHomeDir()
	cfg.Server.PIDFile = filepath.Join(homeDir, ".local", "state", "brain-api", "brain-api.pid")
	cfg.Server.LogFile = filepath.Join(homeDir, ".local", "state", "brain-api", "brain-api.log")

	// Load unified config for server settings (enable_auth, cors_origin, etc.)
	ucfg, err := uconfig.LoadConfig()
	if err != nil {
		slog.Warn("failed to load unified config, using defaults", "error", err)
	} else {
		// Apply server settings from unified config (non-zero values override defaults)
		if ucfg.Server.Port != 0 {
			cfg.Server.Port = ucfg.Server.Port
		}
		if ucfg.Server.Host != "" {
			cfg.Server.Host = ucfg.Server.Host
		}
		if ucfg.Server.BrainDir != "" {
			cfg.Server.BrainDir = ucfg.Server.BrainDir
		}
		if ucfg.Server.LogLevel != "" {
			cfg.Server.LogLevel = ucfg.Server.LogLevel
		}
		if ucfg.Server.PIDFile != "" {
			cfg.Server.PIDFile = ucfg.Server.PIDFile
		}
		if ucfg.Server.LogFile != "" {
			cfg.Server.LogFile = ucfg.Server.LogFile
		}
		// Bool fields: always apply from config (can't distinguish zero from "not set")
		cfg.Server.EnableAuth = ucfg.Server.EnableAuth
		// CORS and OAuth
		if ucfg.Server.CORSOrigin != "" {
			cfg.Server.CORSOrigin = ucfg.Server.CORSOrigin
		}
		if ucfg.Server.OAuthPIN != "" {
			cfg.Server.OAuthPIN = ucfg.Server.OAuthPIN
		}

		// TUI keybindings
		if len(ucfg.TUI.KeyBindings) > 0 {
			cfg.TUI.KeyBindings = ucfg.TUI.KeyBindings
		}
	}

	// Layer 3: Environment variable overrides (highest priority, for Docker deployments)
	if v := os.Getenv("BRAIN_DIR"); v != "" {
		cfg.Server.BrainDir = v
	}
	if v := os.Getenv("PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Server.Port = n
		}
	}
	if v := os.Getenv("HOST"); v != "" {
		cfg.Server.Host = v
	}
	if v := os.Getenv("ENABLE_AUTH"); v != "" {
		lower := strings.ToLower(v)
		cfg.Server.EnableAuth = lower == "true" || lower == "1"
	}
	if v := os.Getenv("CORS_ORIGIN"); v != "" {
		cfg.Server.CORSOrigin = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.Server.LogLevel = v
	}
	if v := os.Getenv("OAUTH_PIN"); v != "" {
		cfg.Server.OAuthPIN = v
	}

	// Load runner config from config file + env vars
	runnerCfg, err := runner.LoadConfig()
	if err != nil {
		slog.Warn("failed to load config file, using defaults", "error", err)
		// Fall back to basic defaults
		cfg.Runner.MaxParallel = 3
		cfg.Runner.PollInterval = 10
		cfg.MCP.APIURL = "http://localhost:3333"
		return cfg
	}

	// Store the FULL runner config — no lossy field-by-field copying
	cfg.Runner = runnerCfg

	// MCP defaults (use the same API URL as runner)
	cfg.MCP.APIURL = runnerCfg.BrainAPIURL

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
	cmdCfg.Server.LogLevel = cfg.Server.LogLevel
	cmdCfg.Server.CORSOrigin = cfg.Server.CORSOrigin
	cmdCfg.Server.OAuthPIN = cfg.Server.OAuthPIN
	cmdCfg.Server.PIDFile = cfg.Server.PIDFile
	cmdCfg.Server.LogFile = cfg.Server.LogFile
	cmdCfg.Server.TLS.Enabled = cfg.Server.TLS.Enabled
	cmdCfg.Server.TLS.CertPath = cfg.Server.TLS.CertPath
	cmdCfg.Server.TLS.KeyPath = cfg.Server.TLS.KeyPath
	// Runner — assign the full config directly, no lossy field-by-field copying
	cmdCfg.Runner = cfg.Runner

	// MCP
	cmdCfg.MCP.APIURL = cfg.MCP.APIURL

	// TUI
	cmdCfg.TUI.KeyBindings = cfg.TUI.KeyBindings

	return cmdCfg
}

// convertToCommandsAPIFlags converts main.APIFlags to commands.APIFlags.
func convertToCommandsAPIFlags(flags *APIFlags) *commands.APIFlags {
	return &commands.APIFlags{
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
		Headless:     flags.Headless,
		Dashboard:    flags.Dashboard,
		MaxParallel:  flags.MaxParallel,
		PollInterval: flags.PollInterval,
		Workdir:      flags.Workdir,
		Agent:        flags.Agent,
		Model:        flags.Model,
		Include:      flags.Include,
		Exclude:      flags.Exclude,
		FeatureIDs:   flags.FeatureIDs,
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

	// Apply port/host overrides from flags
	if flags.Port != 0 {
		cfg.Server.Port = flags.Port
	}
	if flags.Host != "" {
		cfg.Server.Host = flags.Host
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
	since := ""
	level := ""
	for i, arg := range args {
		if arg == "-f" || arg == "--follow" {
			followFlag = true
		}
		if (arg == "-n" || arg == "--lines") && i+1 < len(args) {
			fmt.Sscanf(args[i+1], "%d", &lines)
		}
		if arg == "--since" && i+1 < len(args) {
			since = args[i+1]
		}
		if arg == "--level" && i+1 < len(args) {
			level = args[i+1]
		}
	}

	return &commands.LogsCommand{
		Config: convertToCommandsConfig(cfg),
		Flags: &commands.LogsFlags{
			Follow: followFlag,
			Lines:  lines,
			Since:  since,
			Level:  level,
		},
		Out: nil, // Will use os.Stdout in Execute if nil
	}, nil
}

// parseInitCommand creates an InitCommand from args.
func parseInitCommand(args []string) (Command, error) {
	cfg := defaultConfig()
	flags, err := ParseInitFlags(args)
	if err != nil {
		return nil, err
	}

	return &commands.InitCommand{
		Config: convertToCommandsConfig(cfg),
		Flags:  convertToCommandsInitFlags(flags),
		Out:    nil, // Will use os.Stdout in Execute if nil
	}, nil
}

// parseDoctorCommand creates a DoctorCommand from args.
func parseDoctorCommand(args []string) (Command, error) {
	cfg := defaultConfig()
	flags, err := ParseDoctorFlags(args)
	if err != nil {
		return nil, err
	}

	return &commands.DoctorCommand{
		Config: convertToCommandsConfig(cfg),
		Flags:  convertToCommandsDoctorFlags(flags),
		Out:    nil, // Will use os.Stdout in Execute if nil
	}, nil
}

// parseConfigCommand creates a ConfigCommand from args.
func parseConfigCommand(args []string) (Command, error) {
	cfg := defaultConfig()

	return &commands.ConfigCommand{
		Config: convertToCommandsConfig(cfg),
		Out:    nil, // Will use os.Stdout in Execute if nil
	}, nil
}

// parseInstallCommand creates a PluginCommand for install subcommand.
func parseInstallCommand(args []string) (Command, error) {
	if len(args) == 0 {
		// Return stub for testing/help
		return &stubCommand{cmdType: "install"}, nil
	}

	target := args[0]
	flagArgs := args[1:]

	cfg := defaultConfig()
	flags, err := ParsePluginFlags(flagArgs)
	if err != nil {
		return nil, err
	}

	return &commands.PluginCommand{
		Subcommand: "install",
		Target:     target,
		Config:     convertToCommandsConfig(cfg),
		Flags:      convertToCommandsPluginFlags(flags),
	}, nil
}

// parseUninstallCommand creates a PluginCommand for uninstall subcommand.
func parseUninstallCommand(args []string) (Command, error) {
	if len(args) == 0 {
		// Return stub for testing/help
		return &stubCommand{cmdType: "uninstall"}, nil
	}

	target := args[0]
	flagArgs := args[1:]

	cfg := defaultConfig()
	flags, err := ParsePluginFlags(flagArgs)
	if err != nil {
		return nil, err
	}

	return &commands.PluginCommand{
		Subcommand: "uninstall",
		Target:     target,
		Config:     convertToCommandsConfig(cfg),
		Flags:      convertToCommandsPluginFlags(flags),
	}, nil
}

// parseGetCommand creates a GetCommand from args.
func parseGetCommand(args []string) (Command, error) {
	cfg := defaultConfig()
	flags, idOrPath, err := ParseEntryGetFlags(args)
	if err != nil {
		return nil, err
	}

	// Detect TTY via os.Stdout.Stat()
	isTTY := false
	if fi, err := os.Stdout.Stat(); err == nil {
		isTTY = (fi.Mode() & os.ModeCharDevice) != 0
	}

	return &commands.GetCommand{
		IDOrPath: idOrPath,
		Config:   convertToCommandsConfig(cfg),
		Flags:    convertToCommandsEntryGetFlags(flags),
		IsTTY:    isTTY,
	}, nil
}

// parseSaveCommand creates a SaveCommand from args.
func parseSaveCommand(args []string) (Command, error) {
	cfg := defaultConfig()
	flags, err := ParseEntrySaveFlags(args)
	if err != nil {
		return nil, err
	}

	return &commands.SaveCommand{
		Config: convertToCommandsConfig(cfg),
		Flags:  convertToCommandsEntrySaveFlags(flags),
	}, nil
}

// parseUpdateCommand creates an UpdateCommand from args.
func parseUpdateCommand(args []string) (Command, error) {
	cfg := defaultConfig()
	flags, idOrPath, err := ParseEntryUpdateFlags(args)
	if err != nil {
		return nil, err
	}

	return &commands.UpdateCommand{
		IDOrPath: idOrPath,
		Config:   convertToCommandsConfig(cfg),
		Flags:    convertToCommandsEntryUpdateFlags(flags),
	}, nil
}

// parsePluginStatusCommand creates a PluginCommand for status subcommand.
func parsePluginStatusCommand(args []string) (Command, error) {
	cfg := defaultConfig()
	flags, err := ParsePluginFlags(args)
	if err != nil {
		return nil, err
	}

	return &commands.PluginCommand{
		Subcommand: "status",
		Target:     "",
		Config:     convertToCommandsConfig(cfg),
		Flags:      convertToCommandsPluginFlags(flags),
	}, nil
}

// parseDreamCommand creates a DreamCommand from args.
// Usage: brain dream <project> [flags]
func parseDreamCommand(args []string) (Command, error) {
	// Extract project name (first non-flag argument)
	project := ""
	var flagArgs []string
	for _, arg := range args {
		if !isFlag(arg) && project == "" {
			project = arg
		} else {
			flagArgs = append(flagArgs, arg)
		}
	}

	cfg := defaultConfig()
	flags, err := ParseDreamFlags(flagArgs)
	if err != nil {
		return nil, err
	}

	return &commands.DreamCommand{
		Project: project,
		Config:  convertToCommandsConfig(cfg),
		Flags:   convertToCommandsDreamFlags(flags),
	}, nil
}

// parseEditCommand creates an EditCommand from args.
func parseEditCommand(args []string) (Command, error) {
	cfg := defaultConfig()
	flags, idOrPath, err := ParseEntryEditFlags(args)
	if err != nil {
		return nil, err
	}

	return &commands.EditCommand{
		IDOrPath: idOrPath,
		Config:   convertToCommandsConfig(cfg),
		Flags:    convertToCommandsEntryEditFlags(flags),
	}, nil
}

// parseSearchCommand creates a SearchCommand from args.
func parseSearchCommand(args []string) (Command, error) {
	cfg := defaultConfig()
	flags, err := ParseEntrySearchFlags(args)
	if err != nil {
		return nil, err
	}

	// Extract query from positional args (first non-flag arg)
	query := ""
	for _, arg := range args {
		if !isFlag(arg) {
			query = arg
			break
		}
	}

	return &commands.SearchCommand{
		Query:  query,
		Config: convertToCommandsConfig(cfg),
		Flags:  convertToCommandsEntrySearchFlags(flags),
	}, nil
}

// parseListCommand creates a ListCommand from args.
func parseListCommand(args []string) (Command, error) {
	cfg := defaultConfig()
	flags, err := ParseEntryListFlags(args)
	if err != nil {
		return nil, err
	}

	return &commands.ListCommand{
		Config: convertToCommandsConfig(cfg),
		Flags:  convertToCommandsEntryListFlags(flags),
	}, nil
}
