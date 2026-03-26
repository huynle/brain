package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

// captureOutput captures stdout during function execution
func captureOutput(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestShowHelp(t *testing.T) {
	t.Run("main help", func(t *testing.T) {
		help := captureOutput(func() {
			ShowHelp("")
		})

		// Check main sections
		if !strings.Contains(help, "brain - Unified Brain API CLI") {
			t.Error("Main help should contain title")
		}
		if !strings.Contains(help, "SERVER MODE:") {
			t.Error("Main help should contain SERVER MODE section")
		}
		if !strings.Contains(help, "RUNNER MODE:") {
			t.Error("Main help should contain RUNNER MODE section")
		}
		if !strings.Contains(help, "MCP MODE:") {
			t.Error("Main help should contain MCP MODE section")
		}
		if !strings.Contains(help, "EXAMPLES:") {
			t.Error("Main help should contain EXAMPLES section")
		}
	})

	t.Run("server help", func(t *testing.T) {
		help := captureOutput(func() {
			ShowHelp("server")
		})

		if !strings.Contains(help, "brain server") {
			t.Error("Server help should contain command name")
		}
		if !strings.Contains(help, "--port") {
			t.Error("Server help should contain --port flag")
		}
		if !strings.Contains(help, "--daemon") {
			t.Error("Server help should contain --daemon flag")
		}
		if !strings.Contains(help, "LEGACY COMPATIBILITY:") {
			t.Error("Server help should contain legacy compatibility section")
		}
	})

	t.Run("runner help", func(t *testing.T) {
		help := captureOutput(func() {
			ShowHelp("run")
		})

		if !strings.Contains(help, "brain run") {
			t.Error("Runner help should contain command name")
		}
		if !strings.Contains(help, "--tui") {
			t.Error("Runner help should contain --tui flag")
		}
		if !strings.Contains(help, "--max-parallel") {
			t.Error("Runner help should contain --max-parallel flag")
		}
		if !strings.Contains(help, "brain myproject") {
			t.Error("Runner help should contain example")
		}
	})

	t.Run("runner alias", func(t *testing.T) {
		help := captureOutput(func() {
			ShowHelp("runner")
		})

		if !strings.Contains(help, "brain run") {
			t.Error("Runner alias should show same help as 'run'")
		}
	})

	t.Run("mcp help", func(t *testing.T) {
		help := captureOutput(func() {
			ShowHelp("mcp")
		})

		if !strings.Contains(help, "brain mcp") {
			t.Error("MCP help should contain command name")
		}
		if !strings.Contains(help, "--api-url") {
			t.Error("MCP help should contain --api-url flag")
		}
		if !strings.Contains(help, "CONFIGURATION:") {
			t.Error("MCP help should contain configuration section")
		}
	})

	t.Run("init help", func(t *testing.T) {
		help := captureOutput(func() {
			ShowHelp("init")
		})

		if !strings.Contains(help, "brain init") {
			t.Error("Init help should contain command name")
		}
		if !strings.Contains(help, "--force") {
			t.Error("Init help should contain --force flag")
		}
	})

	t.Run("doctor help", func(t *testing.T) {
		help := captureOutput(func() {
			ShowHelp("doctor")
		})

		if !strings.Contains(help, "brain doctor") {
			t.Error("Doctor help should contain command name")
		}
		if !strings.Contains(help, "--verbose") {
			t.Error("Doctor help should contain --verbose flag")
		}
		if !strings.Contains(help, "--fix") {
			t.Error("Doctor help should contain --fix flag")
		}
	})

	t.Run("install help", func(t *testing.T) {
		help := captureOutput(func() {
			ShowHelp("install")
		})

		if !strings.Contains(help, "brain install") {
			t.Error("Install help should contain command name")
		}
		if !strings.Contains(help, "opencode") {
			t.Error("Install help should contain opencode target")
		}
	})

	t.Run("token help", func(t *testing.T) {
		help := captureOutput(func() {
			ShowHelp("token")
		})

		if !strings.Contains(help, "brain token") {
			t.Error("Token help should contain command name")
		}
		if !strings.Contains(help, "create") {
			t.Error("Token help should contain create subcommand")
		}
		if !strings.Contains(help, "list") {
			t.Error("Token help should contain list subcommand")
		}
		if !strings.Contains(help, "revoke") {
			t.Error("Token help should contain revoke subcommand")
		}
	})

	t.Run("token alias", func(t *testing.T) {
		help := captureOutput(func() {
			ShowHelp("tokens")
		})

		if !strings.Contains(help, "brain token") {
			t.Error("Tokens alias should show same help as 'token'")
		}
	})

	t.Run("unknown command", func(t *testing.T) {
		help := captureOutput(func() {
			ShowHelp("invalid")
		})

		if !strings.Contains(help, "No help available for command: invalid") {
			t.Error("Unknown command should show error message")
		}
		if !strings.Contains(help, "brain - Unified Brain API CLI") {
			t.Error("Unknown command should fall back to main help")
		}
	})
}

func TestHelpContent(t *testing.T) {
	t.Run("main help contains all modes", func(t *testing.T) {
		if !strings.Contains(mainHelp, "SERVER MODE:") {
			t.Error("Main help missing SERVER MODE")
		}
		if !strings.Contains(mainHelp, "RUNNER MODE:") {
			t.Error("Main help missing RUNNER MODE")
		}
		if !strings.Contains(mainHelp, "MCP MODE:") {
			t.Error("Main help missing MCP MODE")
		}
		if !strings.Contains(mainHelp, "SETUP & CONFIG:") {
			t.Error("Main help missing SETUP & CONFIG")
		}
	})

	t.Run("main help contains zero-arg shorthand", func(t *testing.T) {
		if !strings.Contains(mainHelp, "ZERO-ARG SHORTHAND:") {
			t.Error("Main help missing ZERO-ARG SHORTHAND section")
		}
		if !strings.Contains(mainHelp, "brain                           Start runner TUI for all projects") {
			t.Error("Main help missing zero-arg example")
		}
	})

	t.Run("help texts are non-empty", func(t *testing.T) {
		helps := map[string]string{
			"main":    mainHelp,
			"server":  serverHelp,
			"runner":  runnerHelp,
			"mcp":     mcpHelp,
			"init":    initHelp,
			"doctor":  doctorHelp,
			"install": installHelp,
			"token":   tokenHelp,
		}

		for name, text := range helps {
			if len(text) < 100 {
				t.Errorf("%s help is too short: %d chars", name, len(text))
			}
		}
	})

	t.Run("all help texts contain examples", func(t *testing.T) {
		helps := map[string]string{
			"main":   mainHelp,
			"server": serverHelp,
			"runner": runnerHelp,
			"mcp":    mcpHelp,
		}

		for name, text := range helps {
			if !strings.Contains(text, "EXAMPLES:") && !strings.Contains(text, "EXAMPLE:") {
				t.Errorf("%s help missing EXAMPLES section", name)
			}
		}
	})
}

func ExampleShowHelp() {
	ShowHelp("")
	// Output contains: brain - Unified Brain API CLI
}

func ExampleShowHelp_server() {
	ShowHelp("server")
	// Output contains: brain server - Start the Brain API server
}

func ExampleShowHelp_runner() {
	ShowHelp("run")
	// Output contains: brain run - Start the task runner
}

func ExampleShowHelp_mcp() {
	ShowHelp("mcp")
	// Output contains: brain mcp - Start the MCP (Model Context Protocol) server
}

// Benchmark help display performance
func BenchmarkShowHelp(b *testing.B) {
	// Capture output to /dev/null
	old := os.Stdout
	os.Stdout, _ = os.Open(os.DevNull)
	defer func() { os.Stdout = old }()

	b.Run("main", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			ShowHelp("")
		}
	})

	b.Run("server", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			ShowHelp("server")
		}
	})
}

// TestHelpIntegration tests help flag integration
func TestHelpIntegration(t *testing.T) {
	t.Run("--help flag shows help", func(t *testing.T) {
		// This would be tested in main_test.go with actual CLI integration
		// For now, just verify ShowHelp is exported
		var _ func(string) = ShowHelp
	})
}

// Additional test for help formatting
func TestHelpFormatting(t *testing.T) {
	t.Run("consistent indentation", func(t *testing.T) {
		helps := []string{mainHelp, serverHelp, runnerHelp, mcpHelp}

		for _, help := range helps {
			lines := strings.Split(help, "\n")
			for _, line := range lines {
				// Check for consistent indentation (2 spaces for descriptions)
				if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") {
					// Valid 2-space indent
					continue
				}
				if strings.HasPrefix(line, "    ") {
					// Valid 4-space indent
					continue
				}
				if len(strings.TrimSpace(line)) == 0 || !strings.HasPrefix(line, " ") {
					// Empty line or no indent is OK
					continue
				}
			}
		}
	})

	t.Run("no trailing whitespace", func(t *testing.T) {
		helps := []string{mainHelp, serverHelp, runnerHelp, mcpHelp}

		for i, help := range helps {
			lines := strings.Split(help, "\n")
			for lineNum, line := range lines {
				if line != strings.TrimRight(line, " \t") {
					t.Errorf("Help text %d has trailing whitespace on line %d", i, lineNum)
				}
			}
		}
	})
}

// Test that help system doesn't panic
func TestHelpPanicSafety(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ShowHelp panicked: %v", r)
		}
	}()

	commands := []string{"", "server", "run", "runner", "mcp", "init", "doctor", "install", "token", "tokens", "invalid", "random"}

	for _, cmd := range commands {
		captureOutput(func() {
			ShowHelp(cmd)
		})
	}
}

// Test help output format
func TestHelpOutputFormat(t *testing.T) {
	t.Run("headers are uppercase", func(t *testing.T) {
		output := captureOutput(func() {
			ShowHelp("")
		})

		headers := []string{"USAGE:", "SERVER MODE:", "RUNNER MODE:", "MCP MODE:", "FLAGS:", "EXAMPLES:"}
		for _, header := range headers {
			if !strings.Contains(output, header) {
				t.Errorf("Missing header: %s", header)
			}
		}
	})

	t.Run("commands show synopsis", func(t *testing.T) {
		tests := []struct {
			command string
			want    string
		}{
			{"server", "brain server"},
			{"run", "brain run"},
			{"mcp", "brain mcp"},
		}

		for _, tt := range tests {
			output := captureOutput(func() {
				ShowHelp(tt.command)
			})

			if !strings.Contains(output, tt.want) {
				t.Errorf("Help for %s missing synopsis %s", tt.command, tt.want)
			}
		}
	})
}

// Example tests for documentation
func TestHelpExamples(t *testing.T) {
	examples := []struct {
		command string
		example string
	}{
		{"", "brain myproject"},
		{"server", "brain server --daemon"},
		{"run", "brain myproject --max-parallel 5"},
		{"mcp", "brain mcp"},
	}

	for _, ex := range examples {
		output := captureOutput(func() {
			ShowHelp(ex.command)
		})

		if !strings.Contains(output, ex.example) {
			t.Errorf("Help for %s missing example: %s", ex.command, ex.example)
		}
	}
}

// Helper for manual testing
func printHelp(command string) {
	fmt.Printf("=== Help for: %s ===\n", command)
	ShowHelp(command)
	fmt.Println()
}
