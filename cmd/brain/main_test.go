package main

import (
	"reflect"
	"testing"
)

func TestRedirectLegacyInvocation(t *testing.T) {
	tests := []struct {
		name     string
		argv0    string
		args     []string
		expected []string
	}{
		{
			name:     "brain-api redirect",
			argv0:    "brain-api",
			args:     []string{"--port", "3000"},
			expected: []string{"server", "--port", "3000"},
		},
		{
			name:     "brain-api no args",
			argv0:    "brain-api",
			args:     []string{},
			expected: []string{"server"},
		},
		{
			name:     "brain-runner project shorthand",
			argv0:    "brain-runner",
			args:     []string{"myproject"},
			expected: []string{"myproject"},
		},
		{
			name:     "brain-runner project with flags",
			argv0:    "brain-runner",
			args:     []string{"myproject", "--tui"},
			expected: []string{"myproject", "--tui"},
		},
		{
			name:     "brain-runner explicit start command",
			argv0:    "brain-runner",
			args:     []string{"start", "myproject"},
			expected: []string{"run", "start", "myproject"},
		},
		{
			name:     "brain-runner explicit list command",
			argv0:    "brain-runner",
			args:     []string{"list", "myproject"},
			expected: []string{"run", "list", "myproject"},
		},
		{
			name:     "brain-runner status command",
			argv0:    "brain-runner",
			args:     []string{"status"},
			expected: []string{"run", "status"},
		},
		{
			name:     "brain-runner no args",
			argv0:    "brain-runner",
			args:     []string{},
			expected: []string{"run"},
		},
		{
			name:     "brain-mcp redirect",
			argv0:    "brain-mcp",
			args:     []string{},
			expected: []string{"mcp"},
		},
		{
			name:     "brain-mcp with flags",
			argv0:    "brain-mcp",
			args:     []string{"--port", "8080"},
			expected: []string{"mcp", "--port", "8080"},
		},
		{
			name:     "brain normal invocation",
			argv0:    "brain",
			args:     []string{"server"},
			expected: []string{"server"},
		},
		{
			name:     "brain with project",
			argv0:    "brain",
			args:     []string{"myproject"},
			expected: []string{"myproject"},
		},
		{
			name:     "unknown binary name",
			argv0:    "unknown-binary",
			args:     []string{"arg1"},
			expected: []string{"arg1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := redirectLegacyInvocation(tt.argv0, tt.args)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("redirectLegacyInvocation(%q, %v) = %v, want %v",
					tt.argv0, tt.args, result, tt.expected)
			}
		})
	}
}

func TestIsRunnerSubcommand(t *testing.T) {
	tests := []struct {
		name     string
		arg      string
		expected bool
	}{
		{name: "start is subcommand", arg: "start", expected: true},
		{name: "stop is subcommand", arg: "stop", expected: true},
		{name: "status is subcommand", arg: "status", expected: true},
		{name: "list is subcommand", arg: "list", expected: true},
		{name: "ready is subcommand", arg: "ready", expected: true},
		{name: "waiting is subcommand", arg: "waiting", expected: true},
		{name: "blocked is subcommand", arg: "blocked", expected: true},
		{name: "features is subcommand", arg: "features", expected: true},
		{name: "logs is subcommand", arg: "logs", expected: true},
		{name: "config is subcommand", arg: "config", expected: true},
		{name: "project name is not subcommand", arg: "myproject", expected: false},
		{name: "server is not runner subcommand", arg: "server", expected: false},
		{name: "help is not runner subcommand", arg: "help", expected: false},
		{name: "random string is not subcommand", arg: "foobar", expected: false},
		{name: "empty string is not subcommand", arg: "", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRunnerSubcommand(tt.arg)
			if result != tt.expected {
				t.Errorf("isRunnerSubcommand(%q) = %v, want %v",
					tt.arg, result, tt.expected)
			}
		})
	}
}
