package main

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Test: Zero args routes to runner TUI
// ---------------------------------------------------------------------------

func TestRoute_ZeroArgs_RoutesToRunnerTUI(t *testing.T) {
	args := []string{}
	cmd, err := route(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Type() != "runner_tui" {
		t.Errorf("Type() = %q, want %q", cmd.Type(), "runner_tui")
	}
}

// ---------------------------------------------------------------------------
// Test: "all" keyword routes to runner TUI
// ---------------------------------------------------------------------------

func TestRoute_AllKeyword_RoutesToRunnerTUI(t *testing.T) {
	args := []string{"all"}
	cmd, err := route(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Type() != "runner_tui" {
		t.Errorf("Type() = %q, want %q", cmd.Type(), "runner_tui")
	}
}

// ---------------------------------------------------------------------------
// Test: Project name routes to runner TUI
// ---------------------------------------------------------------------------

func TestRoute_ProjectName_RoutesToRunnerTUI(t *testing.T) {
	args := []string{"my-project"}
	cmd, err := route(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Type() != "runner_tui" {
		t.Errorf("Type() = %q, want %q", cmd.Type(), "runner_tui")
	}
}

// ---------------------------------------------------------------------------
// Test: Built-in commands take precedence
// ---------------------------------------------------------------------------

func TestRoute_BuiltinCommands_TakePrecedence(t *testing.T) {
	builtins := []string{
		"server", "mcp", "run", "start", "stop", "restart",
		"status", "health", "logs", "dev", "init", "doctor",
		"config", "install", "uninstall", "plugin-status", "token", "help",
	}

	for _, builtin := range builtins {
		t.Run(builtin, func(t *testing.T) {
			args := []string{builtin}
			cmd, err := route(args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			cmdType := cmd.Type()
			// Built-in commands should not route to runner_tui
			if cmdType == "runner_tui" {
				t.Errorf("builtin %q should not route to runner_tui", builtin)
			}
			// Should route to the builtin command type
			if cmdType != builtin {
				t.Errorf("Type() = %q, want %q", cmdType, builtin)
			}
		})
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
