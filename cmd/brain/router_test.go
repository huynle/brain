package main

import (
	"strings"
	"testing"

	"github.com/huynle/brain-api/cmd/brain/commands"
)

// ---------------------------------------------------------------------------
// Test: Zero args routes to help
// ---------------------------------------------------------------------------

func TestRoute_ZeroArgs_RoutesToHelp(t *testing.T) {
	args := []string{}
	cmd, err := route(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Type() != "help" {
		t.Errorf("Type() = %q, want %q", cmd.Type(), "help")
	}
}

// ---------------------------------------------------------------------------
// Test: Unknown args route to help (not runner TUI)
// ---------------------------------------------------------------------------

func TestRoute_UnknownArg_RoutesToHelp(t *testing.T) {
	// "all" without "start" prefix should be help, not runner TUI
	for _, arg := range []string{"all", "my-project", "ft857"} {
		t.Run(arg, func(t *testing.T) {
			cmd, err := route([]string{arg})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cmd.Type() != "help" {
				t.Errorf("route(%q) Type() = %q, want %q", arg, cmd.Type(), "help")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test: Built-in commands take precedence
// ---------------------------------------------------------------------------

func TestRoute_BuiltinCommands_TakePrecedence(t *testing.T) {
	builtins := []string{
		"api", "mcp", "run", "runner", "start", "stop",
		"dev", "init", "doctor",
		"config", "install", "uninstall", "plugin-status", "token", "dream", "help",
	}

	// Commands that return a different Type() than their name
	aliasExpected := map[string]string{
		"runner": "run",
		"start":  "runner_tui", // "brain start" → runner TUI for all projects
	}

	for _, builtin := range builtins {
		t.Run(builtin, func(t *testing.T) {
			args := []string{builtin}
			cmd, err := route(args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			cmdType := cmd.Type()
			expected := builtin
			if alias, ok := aliasExpected[builtin]; ok {
				expected = alias
			}
			if cmdType != expected {
				t.Errorf("Type() = %q, want %q", cmdType, expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test: Config command routes to ConfigCommand
// ---------------------------------------------------------------------------

func TestRoute_ConfigCommand_ReturnsConfigCommand(t *testing.T) {
	args := []string{"config"}
	cmd, err := route(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Type() != "config" {
		t.Errorf("Type() = %q, want %q", cmd.Type(), "config")
	}

	// Verify it's actually a ConfigCommand, not a stub
	_, ok := cmd.(*commands.ConfigCommand)
	if !ok {
		t.Errorf("expected *commands.ConfigCommand, got %T", cmd)
	}
}

// ---------------------------------------------------------------------------
// Test: Unknown command routes to help
// ---------------------------------------------------------------------------

func TestRoute_UnknownCommand_RoutesToHelp(t *testing.T) {
	args := []string{"unknown-command-12345"}
	cmd, err := route(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Type() != "help" {
		t.Errorf("Type() = %q, want %q", cmd.Type(), "help")
	}
}

// ---------------------------------------------------------------------------
// Test: isBuiltinCommand helper
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Test: "brain server <subcommand>" routes to the correct lifecycle command
// ---------------------------------------------------------------------------

func TestRoute_APISubcommands(t *testing.T) {
	tests := []struct {
		args     []string
		wantType string
	}{
		{[]string{"api", "start"}, "start"},
		{[]string{"api", "stop"}, "stop"},
		{[]string{"api", "restart"}, "restart"},
		{[]string{"api", "status"}, "status"},
		{[]string{"api", "logs"}, "logs"},
		{[]string{"api", "health"}, "health"},
	}

	for _, tt := range tests {
		name := tt.args[0] + " " + tt.args[1]
		t.Run(name, func(t *testing.T) {
			cmd, err := route(tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cmd.Type() != tt.wantType {
				t.Errorf("Type() = %q, want %q", cmd.Type(), tt.wantType)
			}
		})
	}
}

// Test: "brain start <project>" routes to runner TUI
func TestRoute_StartProject(t *testing.T) {
	tests := []struct {
		args        []string
		wantType    string
		wantProject string
	}{
		{[]string{"start"}, "runner_tui", "all"},
		{[]string{"start", "ft857"}, "runner_tui", "ft857"},
		{[]string{"start", "all"}, "runner_tui", "all"},
		{[]string{"start", "my-project"}, "runner_tui", "my-project"},
	}

	for _, tt := range tests {
		name := strings.Join(tt.args, " ")
		t.Run(name, func(t *testing.T) {
			cmd, err := route(tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cmd.Type() != tt.wantType {
				t.Errorf("Type() = %q, want %q", cmd.Type(), tt.wantType)
			}
			// Verify the project is correct
			if tuiCmd, ok := cmd.(*commands.RunnerTUICommand); ok {
				if tuiCmd.Project != tt.wantProject {
					t.Errorf("Project = %q, want %q", tuiCmd.Project, tt.wantProject)
				}
			}
		})
	}
}

// Test: "brain api" with no subcommand still starts the API server
func TestRoute_APINoSubcommand_StartsAPI(t *testing.T) {
	cmd, err := route([]string{"api"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Type() != "api" {
		t.Errorf("Type() = %q, want %q", cmd.Type(), "api")
	}
}

func TestRoute_HelpFlagsRouteToHelpCommand(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		helpTopic string
	}{
		{name: "api help", args: []string{"api", "--help"}, helpTopic: "api"},
		{name: "api logs help", args: []string{"api", "logs", "--help"}, helpTopic: "api logs"},
		{name: "api health short help", args: []string{"api", "health", "-h"}, helpTopic: "api health"},
		{name: "start help", args: []string{"start", "--help"}, helpTopic: "start"},
		{name: "run start help", args: []string{"run", "start", "--help"}, helpTopic: "run start"},
		{name: "token create help", args: []string{"token", "create", "--help"}, helpTopic: "token create"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := route(tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			h, ok := cmd.(*HelpCommand)
			if !ok {
				t.Fatalf("expected *HelpCommand, got %T", cmd)
			}
			if h.command != tt.helpTopic {
				t.Fatalf("help topic = %q, want %q", h.command, tt.helpTopic)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test: isBuiltinCommand helper
// ---------------------------------------------------------------------------

func TestIsBuiltinCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"api", true},
		{"mcp", true},
		{"help", true},
		{"run", true},
		{"start", true},
		{"stop", true},
		{"dream", true},
		{"my-project", false},
		{"all", false},
		{"unknown", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			got := isBuiltinCommand(tt.cmd)
			if got != tt.want {
				t.Errorf("isBuiltinCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test: "brain dream" routes to DreamCommand
// ---------------------------------------------------------------------------

func TestRoute_DreamCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantType    string
		wantProject string
	}{
		{"dream bare", []string{"dream"}, "dream", ""},
		{"dream project", []string{"dream", "brain-api"}, "dream", "brain-api"},
		{"dream enable", []string{"dream", "brain-api", "--enable"}, "dream", "brain-api"},
		{"dream disable", []string{"dream", "brain-api", "--disable"}, "dream", "brain-api"},
		{"dream now", []string{"dream", "brain-api", "--now"}, "dream", "brain-api"},
		{"dream enable now", []string{"dream", "brain-api", "--enable", "--now"}, "dream", "brain-api"},
		{"dream help", []string{"dream", "--help"}, "help", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := route(tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cmd.Type() != tt.wantType {
				t.Errorf("Type() = %q, want %q", cmd.Type(), tt.wantType)
			}

			// Verify project on DreamCommand
			if dc, ok := cmd.(*commands.DreamCommand); ok {
				if dc.Project != tt.wantProject {
					t.Errorf("Project = %q, want %q", dc.Project, tt.wantProject)
				}
			}

			// Verify --now flag on DreamCommand
			if dc, ok := cmd.(*commands.DreamCommand); ok && tt.name == "dream now" {
				if !dc.Flags.Now {
					t.Errorf("Flags.Now = false, want true")
				}
			}
			if dc, ok := cmd.(*commands.DreamCommand); ok && tt.name == "dream enable now" {
				if !dc.Flags.Now || !dc.Flags.Enable {
					t.Errorf("Flags = {Now:%v, Enable:%v}, want {Now:true, Enable:true}", dc.Flags.Now, dc.Flags.Enable)
				}
			}

			// Verify help command for --help
			if tt.wantType == "help" {
				h, ok := cmd.(*HelpCommand)
				if !ok {
					t.Fatalf("expected *HelpCommand, got %T", cmd)
				}
				if h.command != "dream" {
					t.Errorf("help topic = %q, want %q", h.command, "dream")
				}
			}
		})
	}
}
