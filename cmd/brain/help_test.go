package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func captureOutput(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func TestShowHelp_BasicTopics(t *testing.T) {
	tests := []struct {
		name  string
		topic string
		wants []string
	}{
		{name: "main", topic: "", wants: []string{"brain - Unified Brain CLI", "CORE COMMANDS:", "RUNNER COMMANDS:"}},
		{name: "api", topic: "api", wants: []string{"brain api", "SUBCOMMANDS:", "brain help api logs"}},
		{name: "run", topic: "run", wants: []string{"brain run", "SUBCOMMANDS:", "run start"}},
		{name: "mcp", topic: "mcp", wants: []string{"brain mcp", "--api-url"}},
		{name: "init", topic: "init", wants: []string{"brain init", "--dry-run"}},
		{name: "doctor", topic: "doctor", wants: []string{"brain doctor", "--skip-version-check"}},
		{name: "install", topic: "install", wants: []string{"brain install", "opencode", "--api-url"}},
		{name: "token", topic: "token", wants: []string{"brain token", "create", "revoke"}},
		{name: "attachments", topic: "attachments", wants: []string{"brain attachments", "upload <path>", "download <attachment-id>", "extract <attachment-id>", "delete <attachment-id>"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureOutput(func() {
				ShowHelp(tt.topic)
			})

			for _, want := range tt.wants {
				if !strings.Contains(output, want) {
					t.Errorf("help for %q missing %q", tt.topic, want)
				}
			}
		})
	}
}

func TestShowHelp_AutomationGoalTopics(t *testing.T) {
	wants := []string{
		"brain automation goal",
		"set <project>",
		"--trigger-source",
		"--session-mode",
		"--criteria",
		"reconcile",
	}
	topics := []string{
		"automation goal",
		"automation goal set",
		"automation goal list",
		"automation goal run",
		"automation goal reconcile",
		"automation goal validate",
	}
	for _, topic := range topics {
		t.Run(topic, func(t *testing.T) {
			output := captureOutput(func() {
				ShowHelp(topic)
			})
			for _, want := range wants {
				if !strings.Contains(output, want) {
					t.Errorf("help for %q missing %q", topic, want)
				}
			}
		})
	}
}

func TestShowHelp_SubTopicsAndAliases(t *testing.T) {
	tests := []struct {
		name  string
		topic string
		want  string
	}{
		{name: "api logs", topic: "api logs", want: "--since <duration>"},
		{name: "api health", topic: "api health", want: "--wait"},
		{name: "token create", topic: "token create", want: "--name <name>"},
		{name: "run start", topic: "run start", want: "brain run start"},
		{name: "runner alias", topic: "runner", want: "brain run"},
		{name: "tokens alias", topic: "tokens", want: "brain token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureOutput(func() {
				ShowHelp(tt.topic)
			})

			if !strings.Contains(output, tt.want) {
				t.Errorf("help for %q missing %q", tt.topic, tt.want)
			}
		})
	}
}

func TestShowHelp_AutomationSurfacesMentionSupportedTriggersAndGuards(t *testing.T) {
	tests := []struct {
		name  string
		topic string
		wants []string
	}{
		{
			name:  "automation overview",
			topic: "automation",
			wants: []string{
				"event",
				"cron",
				"webhook",
				"session",
				"cooldown",
				"max_concurrent",
			},
		},
		{
			name:  "automation create wizard",
			topic: "automation create",
			wants: []string{
				"Trigger type (event, cron, webhook, session)",
				"runner.session_discovered",
				"cooldown",
				"max_concurrent",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureOutput(func() {
				ShowHelp(tt.topic)
			})

			for _, want := range tt.wants {
				if !strings.Contains(output, want) {
					t.Errorf("help for %q missing %q", tt.topic, want)
				}
			}
		})
	}
}

func TestShowHelp_UnknownTopicFallsBackToMain(t *testing.T) {
	output := captureOutput(func() {
		ShowHelp("definitely-not-a-command")
	})

	if !strings.Contains(output, "No help available for command: definitely-not-a-command") {
		t.Fatal("expected unknown-command message")
	}
	if !strings.Contains(output, "brain - Unified Brain CLI") {
		t.Fatal("expected main help fallback")
	}
}
