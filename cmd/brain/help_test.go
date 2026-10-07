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

func TestShowHelp_UnknownTopicPrintsNothing(t *testing.T) {
	for _, topic := range []string{"mcp", "goal", "definitely-not-a-command"} {
		var known bool
		out := captureOutput(func() { known = ShowHelp(topic) })
		if known || out != "" {
			t.Errorf("ShowHelp(%q) = %v, printed %q; want false and nothing", topic, known, out)
		}
	}
	main := captureOutput(func() { ShowHelp("") })
	if strings.Contains(main, "brain mcp") {
		t.Errorf("main help still advertises brain mcp:\n%s", main)
	}
}

func TestRunHelp_DescribesPauseFamily(t *testing.T) {
	out := captureOutput(func() { ShowHelp("run") })
	for _, want := range []string{"pause-all", "resume-all", "pause <project>", "resume <project>", "--yes"} {
		if !strings.Contains(out, want) {
			t.Errorf("run help missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Stop runner") {
		t.Errorf("run help still advertises stop as stopping the runner:\n%s", out)
	}
}

func TestRunHelp_NoPlaceholders(t *testing.T) {
	out := captureOutput(func() { ShowHelp("run") })
	if strings.Contains(out, "placeholder") {
		t.Errorf("run help still labels implemented subcommands as placeholders:\n%s", out)
	}
	for _, want := range []string{"features <project>", "logs <project> <taskId>", "config"} {
		if !strings.Contains(out, want) {
			t.Errorf("run help missing %q:\n%s", want, out)
		}
	}
}

func TestHelpExitCodes(t *testing.T) {
	cases := []struct {
		args       []string
		code       int
		wantStdout string
		wantStderr string
	}{
		{[]string{"help", "bogus"}, 2, "", `brain: unknown command "bogus"`},
		{[]string{"help", "mcp"}, 2, "", `brain: unknown command "mcp"`},
		{[]string{"help", "run", "bogus"}, 2, "", `brain: unknown command "run bogus"`},
		{[]string{"help", "run"}, 0, "brain run", ""},
		{[]string{"help", "run", "pause"}, 0, "brain run pause <project>", ""},
		// Flag-style help for a sub-topic without its own page falls back
		// to the nearest parent page instead of the main help.
		{[]string{"automation", "bogus", "--help"}, 0, "brain automation", ""},
	}
	for _, tc := range cases {
		var stderr bytes.Buffer
		var code int
		out := captureOutput(func() { code = runCLI(tc.args, &stderr) })
		if code != tc.code {
			t.Errorf("brain %v exit = %d, want %d (stderr %q)", tc.args, code, tc.code, stderr.String())
		}
		if !strings.Contains(out, tc.wantStdout) || !strings.Contains(stderr.String(), tc.wantStderr) {
			t.Errorf("brain %v stdout %q / stderr %q", tc.args, firstLine(out), stderr.String())
		}
		if tc.code == 2 && out != "" {
			t.Errorf("brain %v printed to stdout on error: %q", tc.args, firstLine(out))
		}
	}
}

func TestRunSubcommandHelp(t *testing.T) {
	for _, sub := range []string{"start", "status", "list", "ready", "features", "logs", "config", "pause", "resume", "pause-all", "resume-all", "stop"} {
		for _, group := range []string{"run", "runner"} {
			if group == "runner" && (sub == "start" || sub == "stop" || sub == "status") {
				continue // runner's own daemon commands share the runner page
			}
			var stderr bytes.Buffer
			var code int
			out := captureOutput(func() { code = runCLI([]string{group, sub, "--help"}, &stderr) })
			if code != 0 || stderr.Len() != 0 {
				t.Errorf("brain %s %s --help: exit %d stderr %q", group, sub, code, stderr.String())
			}
			if !strings.HasPrefix(out, "brain run "+sub) {
				t.Errorf("brain %s %s --help should print its own page, got %q", group, sub, firstLine(out))
			}
		}
	}
}

func TestRunnerHelpDescribesBothRoles(t *testing.T) {
	out := captureOutput(func() { ShowHelp("runner") })
	for _, want := range []string{"brain runner start", "brain runner stop", "pause-all", "features"} {
		if !strings.Contains(out, want) {
			t.Errorf("runner help missing %q:\n%s", want, out)
		}
	}
	main := captureOutput(func() { ShowHelp("") })
	if strings.Contains(main, "Alias for run") {
		t.Errorf("main help still calls runner a plain alias:\n%s", main)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
