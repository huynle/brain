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
		"server", "mcp", "run", "runner", "start", "stop",
		"dev", "init", "doctor",
		"config", "install", "uninstall", "plugin-status", "token", "help",
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

func TestRoute_ServerSubcommands(t *testing.T) {
	tests := []struct {
		args     []string
		wantType string
	}{
		{[]string{"server", "start"}, "start"},
		{[]string{"server", "stop"}, "stop"},
		{[]string{"server", "restart"}, "restart"},
		{[]string{"server", "status"}, "status"},
		{[]string{"server", "logs"}, "logs"},
		{[]string{"server", "health"}, "health"},
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

// Test: "brain server" with no subcommand still starts the server
func TestRoute_ServerNoSubcommand_StartsServer(t *testing.T) {
	cmd, err := route([]string{"server"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Type() != "server" {
		t.Errorf("Type() = %q, want %q", cmd.Type(), "server")
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
		{"server", true},
		{"mcp", true},
		{"help", true},
		{"run", true},
		{"start", true},
		{"stop", true},
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
