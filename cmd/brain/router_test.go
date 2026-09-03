package main

import (
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/huynle/brain-api/cmd/brain/commands"
)

func TestRoute_APIStartEmbeddedRunnerFlags(t *testing.T) {
	cmd, err := route([]string{
		"api", "start",
		"--runner",
		"--runner-project", "personal-productivity",
		"--port", "4444",
		"--host", "0.0.0.0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	start, ok := cmd.(*commands.StartCommand)
	if !ok {
		t.Fatalf("expected *commands.StartCommand, got %T", cmd)
	}
	if !start.Flags.Runner {
		t.Fatal("expected embedded runner flag to be true")
	}
	if start.Flags.RunnerProject != "personal-productivity" {
		t.Fatalf("RunnerProject = %q, want personal-productivity", start.Flags.RunnerProject)
	}
	if start.Flags.Port != 4444 || start.Flags.Host != "0.0.0.0" {
		t.Fatalf("Port/Host = %d/%q, want 4444/0.0.0.0", start.Flags.Port, start.Flags.Host)
	}
	if start.Config.Server.Port != 4444 || start.Config.Server.Host != "0.0.0.0" {
		t.Fatalf("Config Port/Host = %d/%q, want 4444/0.0.0.0", start.Config.Server.Port, start.Config.Server.Host)
	}
}

func TestRoute_APIDaemonEmbeddedRunnerFlags(t *testing.T) {
	cmd, err := route([]string{
		"api",
		"--daemon",
		"--runner",
		"--runner-project", "all",
		"--max-parallel", "3",
		"--include", "prod-*",
		"--exclude", "test-*",
		"--executor", "pi",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	api, ok := cmd.(*commands.APICommand)
	if !ok {
		t.Fatalf("expected *commands.APICommand, got %T", cmd)
	}
	if !api.Flags.Runner {
		t.Fatal("expected daemon child embedded runner flag to be true")
	}
	if api.Flags.RunnerProject != "all" {
		t.Fatalf("RunnerProject = %q, want all", api.Flags.RunnerProject)
	}
	if api.Flags.MaxParallel != 3 || api.Flags.Executor != "pi" {
		t.Fatalf("MaxParallel/Executor = %d/%q, want 3/pi", api.Flags.MaxParallel, api.Flags.Executor)
	}
	if len(api.Flags.Include) != 1 || api.Flags.Include[0] != "prod-*" {
		t.Fatalf("Include = %v, want [prod-*]", api.Flags.Include)
	}
	if len(api.Flags.Exclude) != 1 || api.Flags.Exclude[0] != "test-*" {
		t.Fatalf("Exclude = %v, want [test-*]", api.Flags.Exclude)
	}
}

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
		"api", "mcp", "run", "runner", "stop", "attachments",
		"dev", "init", "doctor",
		"config", "install", "uninstall", "plugin-status", "token", "dream", "help",
	}

	// Commands that return a different Type() than their name
	aliasExpected := map[string]string{
		"runner": "help", // "brain runner" alone → help; "brain runner start" → runner daemon
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

func TestRoute_AttachmentsCommandParsesSubcommands(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		subcommand string
		path       string
		entry      string
		attachment string
		project    string
		role       string
		output     string
		dryRun     bool
		force      bool
		skipReady  bool
		batchSize  int
		rateLimit  int
	}{
		{name: "upload", args: []string{"attachments", "upload", "file.txt", "--project", "brain-api", "--description", "fixture"}, subcommand: "upload", path: "file.txt", project: "brain-api"},
		{name: "attach", args: []string{"attachments", "attach", "entry-123", "att_123", "--project", "brain-api", "--role", "source"}, subcommand: "attach", entry: "entry-123", attachment: "att_123", project: "brain-api", role: "source"},
		{name: "list entry", args: []string{"attachments", "list", "--entry", "entry-123", "--project", "brain-api"}, subcommand: "list", entry: "entry-123", project: "brain-api"},
		{name: "download", args: []string{"attachments", "download", "att_123", "--project", "brain-api", "--output", "out.bin"}, subcommand: "download", attachment: "att_123", project: "brain-api", output: "out.bin"},
		{name: "extract", args: []string{"attachments", "extract", "att_123", "--project", "brain-api"}, subcommand: "extract", attachment: "att_123", project: "brain-api"},
		{name: "backfill", args: []string{"attachments", "backfill", "--project", "brain-api", "--dry-run", "--force", "--batch-size", "10", "--rate-limit-ms", "25"}, subcommand: "backfill", project: "brain-api", dryRun: true, force: true, batchSize: 10, rateLimit: 25},
		{name: "backfill skip ready", args: []string{"attachments", "backfill", "--project", "brain-api", "--force", "--skip-ready"}, subcommand: "backfill", project: "brain-api", skipReady: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := route(tt.args)
			if err != nil {
				t.Fatalf("route() error = %v", err)
			}
			attachmentCmd, ok := cmd.(*commands.AttachmentCommand)
			if !ok {
				t.Fatalf("expected *commands.AttachmentCommand, got %T", cmd)
			}
			if attachmentCmd.Subcommand != tt.subcommand || attachmentCmd.Path != tt.path || attachmentCmd.Entry != tt.entry || attachmentCmd.AttachmentID != tt.attachment {
				t.Fatalf("command = %#v", attachmentCmd)
			}
			if attachmentCmd.Flags.Project != tt.project || attachmentCmd.Flags.Role != tt.role || attachmentCmd.Flags.Output != tt.output {
				t.Fatalf("flags = %#v", attachmentCmd.Flags)
			}
			if boolAttachmentFlagForTest(t, attachmentCmd.Flags, "DryRun") != tt.dryRun || boolAttachmentFlagForTest(t, attachmentCmd.Flags, "Force") != tt.force || boolAttachmentFlagForTest(t, attachmentCmd.Flags, "SkipReady") != tt.skipReady || intAttachmentFlagForTest(t, attachmentCmd.Flags, "BatchSize") != tt.batchSize || intAttachmentFlagForTest(t, attachmentCmd.Flags, "RateLimitDelayMs") != tt.rateLimit {
				t.Fatalf("backfill flags = %#v", attachmentCmd.Flags)
			}
		})
	}
}

func boolAttachmentFlagForTest(t *testing.T, flags *commands.AttachmentFlags, name string) bool {
	t.Helper()
	field := reflect.ValueOf(flags).Elem().FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("AttachmentFlags missing field %s", name)
	}
	return field.Bool()
}

func intAttachmentFlagForTest(t *testing.T, flags *commands.AttachmentFlags, name string) int {
	t.Helper()
	field := reflect.ValueOf(flags).Elem().FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("AttachmentFlags missing field %s", name)
	}
	return int(field.Int())
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

func TestRoute_ConfigCommand_ParsesConfigSubcommands(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		subcommand string
		wantPrint  bool
		wantForce  bool
	}{
		{name: "defaults", args: []string{"config", "defaults"}, subcommand: "defaults"},
		{name: "init", args: []string{"config", "init"}, subcommand: "init"},
		{name: "init print", args: []string{"config", "init", "--print"}, subcommand: "init", wantPrint: true},
		{name: "init force", args: []string{"config", "init", "--force"}, subcommand: "init", wantForce: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := route(tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			configCmd, ok := cmd.(*commands.ConfigCommand)
			if !ok {
				t.Fatalf("expected *commands.ConfigCommand, got %T", cmd)
			}
			if configCmd.Subcommand != tt.subcommand {
				t.Fatalf("Subcommand = %q, want %q", configCmd.Subcommand, tt.subcommand)
			}
			if configCmd.Flags == nil {
				t.Fatal("expected config flags")
			}
			if configCmd.Flags.Print != tt.wantPrint {
				t.Fatalf("Print = %v, want %v", configCmd.Flags.Print, tt.wantPrint)
			}
			if configCmd.Flags.Force != tt.wantForce {
				t.Fatalf("Force = %v, want %v", configCmd.Flags.Force, tt.wantForce)
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
// `brain start` was the TUI dashboard entry point and is gone. It must now
// behave like any other unrecognized word — help, not a silently different
// runner mode — so a stale script or muscle-memory invocation fails loudly
// instead of starting a headless runner nobody asked for.
func TestRoute_StartNoLongerRoutesToARunner(t *testing.T) {
	for _, args := range [][]string{
		{"start"},
		{"start", "ft857"},
		{"start", "all"},
		{"start", "my-project"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd, err := route(args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := cmd.Type(); got != "help" {
				t.Errorf("route(%v) Type() = %q, want help", args, got)
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
		{"start", false},
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

// ---------------------------------------------------------------------------
// Test: "brain run start <project> --headless" arg order (BUG-2)
// ---------------------------------------------------------------------------

func TestRoute_RunStart_ArgOrder(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantProject  string
		wantHeadless bool
	}{
		{
			name:         "flag after project",
			args:         []string{"run", "start", "horizontal-scaling-test", "--headless"},
			wantProject:  "horizontal-scaling-test",
			wantHeadless: true,
		},
		{
			name:         "flag before project",
			args:         []string{"run", "start", "--headless", "horizontal-scaling-test"},
			wantProject:  "horizontal-scaling-test",
			wantHeadless: true,
		},
		{
			name:         "project only no flag",
			args:         []string{"run", "start", "horizontal-scaling-test"},
			wantProject:  "horizontal-scaling-test",
			wantHeadless: false,
		},
		{
			name:         "no project defaults to all",
			args:         []string{"run", "start"},
			wantProject:  "all",
			wantHeadless: false,
		},
		{
			name:         "no project with headless flag",
			args:         []string{"run", "start", "--headless"},
			wantProject:  "all",
			wantHeadless: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := route(tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cmd.Type() != "run_start" {
				t.Errorf("Type() = %q, want %q", cmd.Type(), "run_start")
			}
			rc, ok := cmd.(*commands.RunCommand)
			if !ok {
				t.Fatalf("expected *commands.RunCommand, got %T", cmd)
			}
			if rc.Project != tt.wantProject {
				t.Errorf("Project = %q, want %q", rc.Project, tt.wantProject)
			}
			if rc.Flags.Headless != tt.wantHeadless {
				t.Errorf("Flags.Headless = %v, want %v", rc.Flags.Headless, tt.wantHeadless)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test: automation goal subtree routing
// ---------------------------------------------------------------------------

func TestRoute_AutomationGoal_List(t *testing.T) {
	cmd, err := route([]string{"automation", "goal", "list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gc, ok := cmd.(*commands.AutomationGoalCommand)
	if !ok {
		t.Fatalf("expected *commands.AutomationGoalCommand, got %T", cmd)
	}
	if gc.Subcommand != "list" {
		t.Errorf("Subcommand = %q, want %q", gc.Subcommand, "list")
	}
	if gc.Type() != "automation goal" {
		t.Errorf("Type() = %q, want %q", gc.Type(), "automation goal")
	}
}

func TestRoute_AutomationGoal_Positionals(t *testing.T) {
	cmd, err := route([]string{"automation", "goal", "show", "my-project", "goal-123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gc, ok := cmd.(*commands.AutomationGoalCommand)
	if !ok {
		t.Fatalf("expected *commands.AutomationGoalCommand, got %T", cmd)
	}
	if gc.Subcommand != "show" {
		t.Errorf("Subcommand = %q, want %q", gc.Subcommand, "show")
	}
	if gc.Project != "my-project" {
		t.Errorf("Project = %q, want %q", gc.Project, "my-project")
	}
	if gc.GoalID != "goal-123" {
		t.Errorf("GoalID = %q, want %q", gc.GoalID, "goal-123")
	}
}

func TestRoute_AutomationGoal_SetObjective(t *testing.T) {
	cmd, err := route([]string{"automation", "goal", "set", "proj", "Ship dark mode", "--agent", "tdd-dev"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gc, ok := cmd.(*commands.AutomationGoalCommand)
	if !ok {
		t.Fatalf("expected *commands.AutomationGoalCommand, got %T", cmd)
	}
	if gc.Subcommand != "set" {
		t.Errorf("Subcommand = %q, want %q", gc.Subcommand, "set")
	}
	if gc.Project != "proj" {
		t.Errorf("Project = %q, want %q", gc.Project, "proj")
	}
	// For `set`, the second positional is the objective text stored in GoalID.
	if gc.GoalID != "Ship dark mode" {
		t.Errorf("GoalID (objective) = %q, want %q", gc.GoalID, "Ship dark mode")
	}
	if gc.Flags.Agent != "tdd-dev" {
		t.Errorf("Flags.Agent = %q, want %q", gc.Flags.Agent, "tdd-dev")
	}
}

func TestRoute_AutomationGoal_Help(t *testing.T) {
	cmd, err := route([]string{"automation", "goal", "help"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Type() != "help" {
		t.Errorf("Type() = %q, want %q", cmd.Type(), "help")
	}
}

// ---------------------------------------------------------------------------
// Test: deprecation alias "brain goal" -> "brain automation goal"
// ---------------------------------------------------------------------------

func TestRoute_GoalAlias_DelegatesToAutomationGoal(t *testing.T) {
	cmd, err := route([]string{"goal", "list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	alias, ok := cmd.(*deprecatedAliasCommand)
	if !ok {
		t.Fatalf("expected *deprecatedAliasCommand, got %T", cmd)
	}
	if alias.notice == "" {
		t.Error("expected a non-empty deprecation notice")
	}

	gc, ok := alias.inner.(*commands.AutomationGoalCommand)
	if !ok {
		t.Fatalf("expected inner *commands.AutomationGoalCommand, got %T", alias.inner)
	}
	if gc.Subcommand != "list" {
		t.Errorf("Subcommand = %q, want %q", gc.Subcommand, "list")
	}
	// Type() delegates to the inner command for transparency.
	if alias.Type() != "automation goal" {
		t.Errorf("Type() = %q, want %q", alias.Type(), "automation goal")
	}
}

func TestRoute_GoalAlias_PreservesPositionals(t *testing.T) {
	cmd, err := route([]string{"goal", "show", "my-project", "goal-123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	alias, ok := cmd.(*deprecatedAliasCommand)
	if !ok {
		t.Fatalf("expected *deprecatedAliasCommand, got %T", cmd)
	}
	gc, ok := alias.inner.(*commands.AutomationGoalCommand)
	if !ok {
		t.Fatalf("expected inner *commands.AutomationGoalCommand, got %T", alias.inner)
	}
	if gc.Subcommand != "show" {
		t.Errorf("Subcommand = %q, want %q", gc.Subcommand, "show")
	}
	if gc.Project != "my-project" {
		t.Errorf("Project = %q, want %q", gc.Project, "my-project")
	}
	if gc.GoalID != "goal-123" {
		t.Errorf("GoalID = %q, want %q", gc.GoalID, "goal-123")
	}
}

func TestRoute_GoalAlias_SetPassesFlags(t *testing.T) {
	cmd, err := route([]string{"goal", "set", "proj", "Ship dark mode", "--agent", "tdd-dev"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	alias, ok := cmd.(*deprecatedAliasCommand)
	if !ok {
		t.Fatalf("expected *deprecatedAliasCommand, got %T", cmd)
	}
	gc, ok := alias.inner.(*commands.AutomationGoalCommand)
	if !ok {
		t.Fatalf("expected inner *commands.AutomationGoalCommand, got %T", alias.inner)
	}
	if gc.Subcommand != "set" {
		t.Errorf("Subcommand = %q, want %q", gc.Subcommand, "set")
	}
	if gc.Project != "proj" {
		t.Errorf("Project = %q, want %q", gc.Project, "proj")
	}
	if gc.GoalID != "Ship dark mode" {
		t.Errorf("GoalID (objective) = %q, want %q", gc.GoalID, "Ship dark mode")
	}
	if gc.Flags.Agent != "tdd-dev" {
		t.Errorf("Flags.Agent = %q, want %q", gc.Flags.Agent, "tdd-dev")
	}
}

func TestRoute_GoalAlias_HelpPassesThrough(t *testing.T) {
	cmd, err := route([]string{"goal", "help"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Help should pass through directly (no alias wrapper) so help output
	// stays clean.
	if _, ok := cmd.(*deprecatedAliasCommand); ok {
		t.Fatalf("expected help to pass through, got *deprecatedAliasCommand")
	}
	if cmd.Type() != "help" {
		t.Errorf("Type() = %q, want %q", cmd.Type(), "help")
	}
}

func TestDeprecatedAliasCommand_Execute_PrintsNotice(t *testing.T) {
	// Capture stderr to verify the deprecation notice is printed.
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stderr = w

	executed := false
	alias := &deprecatedAliasCommand{
		inner:  &stubExecCommand{onExec: func() { executed = true }},
		notice: "Warning: deprecated",
	}
	execErr := alias.Execute()

	w.Close()
	os.Stderr = oldStderr

	out, _ := io.ReadAll(r)

	if execErr != nil {
		t.Fatalf("unexpected error: %v", execErr)
	}
	if !executed {
		t.Error("expected inner command to be executed")
	}
	if !strings.Contains(string(out), "deprecated") {
		t.Errorf("expected deprecation notice on stderr, got %q", string(out))
	}
}

// stubExecCommand is a minimal Command for testing the alias wrapper.
type stubExecCommand struct {
	onExec func()
}

func (c *stubExecCommand) Execute() error {
	if c.onExec != nil {
		c.onExec()
	}
	return nil
}

func (c *stubExecCommand) Type() string { return "stub-exec" }

// TestDefaultConfig_IndexWatchEnvOverride covers the Docker path: the amos
// deployment configures the server entirely through env vars, so the index
// watcher toggle has to be reachable without a config file.
func TestDefaultConfig_IndexWatchEnvOverride(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if cfg := defaultConfig(); cfg.Server.IndexWatch.Enabled {
		t.Fatal("index watch enabled with no config and no env var, want disabled")
	}

	t.Setenv("BRAIN_INDEX_WATCH", "true")
	if cfg := defaultConfig(); !cfg.Server.IndexWatch.Enabled {
		t.Fatal("BRAIN_INDEX_WATCH=true did not enable the index watcher")
	}
}

func TestSplitRunnerProjectArg(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantProject  string
		wantFlagArgs []string
	}{
		{
			name:        "no args defaults to all",
			args:        nil,
			wantProject: "all",
		},
		{
			name:         "project then flags",
			args:         []string{"my-project", "--headless"},
			wantProject:  "my-project",
			wantFlagArgs: []string{"--headless"},
		},
		{
			// The daemon re-execs exactly this shape. Reading "worker-a" as a
			// second positional would leave --name without a value and the
			// runner would fail to start at all.
			name:         "value after a flag is not the project",
			args:         []string{"all", "--headless", "--name", "worker-a"},
			wantProject:  "all",
			wantFlagArgs: []string{"--headless", "--name", "worker-a"},
		},
		{
			name:         "value flags with an explicit project",
			args:         []string{"my-project", "--model", "sonnet", "--max-parallel", "2"},
			wantProject:  "my-project",
			wantFlagArgs: []string{"--model", "sonnet", "--max-parallel", "2"},
		},
		{
			name:         "flag=value form needs no lookahead",
			args:         []string{"--name=worker-a", "my-project"},
			wantProject:  "my-project",
			wantFlagArgs: []string{"--name=worker-a"},
		},
		{
			name:         "bool flag does not swallow the project",
			args:         []string{"--headless", "my-project"},
			wantProject:  "my-project",
			wantFlagArgs: []string{"--headless"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project, flagArgs := splitRunnerProjectArg(tt.args)
			if project != tt.wantProject {
				t.Errorf("project = %q, want %q", project, tt.wantProject)
			}
			if len(flagArgs) != len(tt.wantFlagArgs) {
				t.Fatalf("flagArgs = %v, want %v", flagArgs, tt.wantFlagArgs)
			}
			for i := range flagArgs {
				if flagArgs[i] != tt.wantFlagArgs[i] {
					t.Fatalf("flagArgs = %v, want %v", flagArgs, tt.wantFlagArgs)
				}
			}
		})
	}
}

func TestParseRunCommand_NamedRunner(t *testing.T) {
	cmd, err := parseRunCommand([]string{"start", "all", "--headless", "--name", "worker-a"})
	if err != nil {
		t.Fatalf("parseRunCommand: %v", err)
	}
	rc, ok := cmd.(*commands.RunCommand)
	if !ok {
		t.Fatalf("got %T, want *commands.RunCommand", cmd)
	}
	if rc.Project != "all" {
		t.Errorf("Project = %q, want all", rc.Project)
	}
	if rc.Flags.Name != "worker-a" {
		t.Errorf("Flags.Name = %q, want worker-a", rc.Flags.Name)
	}
	if !rc.Flags.Headless {
		t.Error("Flags.Headless = false, want true")
	}
}
