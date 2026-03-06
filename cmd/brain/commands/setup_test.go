package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCommand_Type(t *testing.T) {
	cmd := &InitCommand{}
	if got := cmd.Type(); got != "init" {
		t.Errorf("Type() = %q, want %q", got, "init")
	}
}

func TestInitCommand_Execute_CreatesDirectories(t *testing.T) {
	// Setup: Create temp directory
	tmpDir := t.TempDir()

	cfg := &UnifiedConfig{}
	cfg.Server.BrainDir = tmpDir

	flags := &InitFlags{
		Force:  false,
		DryRun: false,
	}

	var out bytes.Buffer
	cmd := &InitCommand{
		Config: cfg,
		Flags:  flags,
		Out:    &out,
	}

	// Execute
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// Verify directories were created
	expectedDirs := []string{
		tmpDir,
		filepath.Join(tmpDir, ".zk"),
		filepath.Join(tmpDir, ".zk", "templates"),
		filepath.Join(tmpDir, "global"),
		filepath.Join(tmpDir, "projects"),
	}

	for _, dir := range expectedDirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Errorf("Expected directory not created: %s", dir)
		}
	}
}

func TestInitCommand_Execute_CopiesTemplates(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &UnifiedConfig{}
	cfg.Server.BrainDir = tmpDir

	flags := &InitFlags{
		Force:  false,
		DryRun: false,
	}

	var out bytes.Buffer
	cmd := &InitCommand{
		Config: cfg,
		Flags:  flags,
		Out:    &out,
	}

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// Verify at least some templates were copied
	templatesDir := filepath.Join(tmpDir, ".zk", "templates")
	entries, err := os.ReadDir(templatesDir)
	if err != nil {
		t.Fatalf("Failed to read templates dir: %v", err)
	}

	if len(entries) < 13 {
		t.Errorf("Expected at least 13 templates, got %d", len(entries))
	}

	// Check for specific template
	taskTemplate := filepath.Join(templatesDir, "task.md")
	if _, err := os.Stat(taskTemplate); os.IsNotExist(err) {
		t.Errorf("Expected task.md template not found")
	}
}

func TestInitCommand_Execute_CopiesConfig(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &UnifiedConfig{}
	cfg.Server.BrainDir = tmpDir

	flags := &InitFlags{
		Force:  false,
		DryRun: false,
	}

	var out bytes.Buffer
	cmd := &InitCommand{
		Config: cfg,
		Flags:  flags,
		Out:    &out,
	}

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// Verify config was copied
	configPath := filepath.Join(tmpDir, ".zk", "config.toml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Errorf("Expected config.toml not found")
	}

	// Verify content is not empty
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}
	if len(content) == 0 {
		t.Errorf("Config file is empty")
	}
}

func TestInitCommand_Execute_DryRun(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &UnifiedConfig{}
	cfg.Server.BrainDir = tmpDir

	flags := &InitFlags{
		Force:  false,
		DryRun: true,
	}

	var out bytes.Buffer
	cmd := &InitCommand{
		Config: cfg,
		Flags:  flags,
		Out:    &out,
	}

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// Verify directories were NOT created
	zkDir := filepath.Join(tmpDir, ".zk")
	if _, err := os.Stat(zkDir); !os.IsNotExist(err) {
		t.Errorf("Expected .zk directory not to be created in dry-run mode")
	}

	// Verify output mentions dry-run
	output := out.String()
	if !strings.Contains(output, "DRY RUN") && !strings.Contains(output, "dry-run") {
		t.Errorf("Expected dry-run message in output, got: %s", output)
	}
}

func TestInitCommand_Execute_Force(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &UnifiedConfig{}
	cfg.Server.BrainDir = tmpDir

	// First run to create files
	flags1 := &InitFlags{Force: false, DryRun: false}
	cmd1 := &InitCommand{Config: cfg, Flags: flags1, Out: &bytes.Buffer{}}
	if err := cmd1.Execute(); err != nil {
		t.Fatalf("First Execute() error = %v", err)
	}

	// Modify a template to test overwriting
	taskTemplate := filepath.Join(tmpDir, ".zk", "templates", "task.md")
	originalContent, _ := os.ReadFile(taskTemplate)
	modifiedContent := []byte("# MODIFIED")
	if err := os.WriteFile(taskTemplate, modifiedContent, 0644); err != nil {
		t.Fatalf("Failed to modify template: %v", err)
	}

	// Second run with --force
	flags2 := &InitFlags{Force: true, DryRun: false}
	var out bytes.Buffer
	cmd2 := &InitCommand{Config: cfg, Flags: flags2, Out: &out}
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("Second Execute() error = %v", err)
	}

	// Verify file was overwritten
	newContent, err := os.ReadFile(taskTemplate)
	if err != nil {
		t.Fatalf("Failed to read template: %v", err)
	}

	if bytes.Equal(newContent, modifiedContent) {
		t.Errorf("Expected file to be overwritten, but it wasn't")
	}

	if !bytes.Equal(newContent, originalContent) {
		t.Logf("Original: %d bytes, New: %d bytes", len(originalContent), len(newContent))
		// Content should be restored to original
	}
}

func TestInitCommand_Execute_SkipsExisting(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &UnifiedConfig{}
	cfg.Server.BrainDir = tmpDir

	// First run
	flags1 := &InitFlags{Force: false, DryRun: false}
	var out1 bytes.Buffer
	cmd1 := &InitCommand{Config: cfg, Flags: flags1, Out: &out1}
	if err := cmd1.Execute(); err != nil {
		t.Fatalf("First Execute() error = %v", err)
	}

	// Second run without --force
	flags2 := &InitFlags{Force: false, DryRun: false}
	var out2 bytes.Buffer
	cmd2 := &InitCommand{Config: cfg, Flags: flags2, Out: &out2}
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("Second Execute() error = %v", err)
	}

	// Verify output mentions skipped files
	output := out2.String()
	if !strings.Contains(output, "skip") && !strings.Contains(output, "⏭") {
		t.Errorf("Expected skip message in output when files exist, got: %s", output)
	}
}

func TestInitCommand_Execute_ExpandsTilde(t *testing.T) {
	cfg := &UnifiedConfig{}
	cfg.Server.BrainDir = "~/test-brain" // Use tilde

	// We need to intercept the path expansion, but for now
	// we'll test that the command doesn't error with tilde paths
	flags := &InitFlags{DryRun: true, Force: false}
	var out bytes.Buffer
	cmd := &InitCommand{Config: cfg, Flags: flags, Out: &out}

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() with tilde path error = %v", err)
	}

	// In dry-run mode, should not error even with tilde path
	output := out.String()
	if len(output) == 0 {
		t.Errorf("Expected some output in dry-run mode")
	}
}

func TestInitCommand_Execute_DisplaysSummary(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &UnifiedConfig{}
	cfg.Server.BrainDir = tmpDir

	flags := &InitFlags{Force: false, DryRun: false}
	var out bytes.Buffer
	cmd := &InitCommand{Config: cfg, Flags: flags, Out: &out}

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := out.String()

	// Should show created count
	if !strings.Contains(output, "created") && !strings.Contains(output, "✅") {
		t.Errorf("Expected 'created' or ✅ in summary, got: %s", output)
	}

	// Should show template/config counts
	if !strings.Contains(output, "template") && !strings.Contains(output, "config") {
		t.Errorf("Expected mention of templates or config in summary, got: %s", output)
	}
}

// ConfigCommand tests
func TestConfigCommand_Type(t *testing.T) {
	cmd := &ConfigCommand{}
	if got := cmd.Type(); got != "config" {
		t.Errorf("Type() = %q, want %q", got, "config")
	}
}

func TestConfigCommand_Execute_DefaultConfig(t *testing.T) {
	cfg := &UnifiedConfig{}
	cfg.Server.Port = 3333
	cfg.Server.Host = "localhost"
	cfg.Server.BrainDir = "~/brain"
	cfg.Server.LogLevel = "info"
	cfg.Runner.MaxParallel = 3
	cfg.Runner.PollInterval = 5000
	cfg.MCP.APIURL = "http://localhost:3333"

	var out bytes.Buffer
	cmd := &ConfigCommand{
		Config: cfg,
		Out:    &out,
	}

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := out.String()

	// Check for server section
	if !strings.Contains(output, "Server") {
		t.Errorf("Expected 'Server' section in output")
	}
	if !strings.Contains(output, "3333") {
		t.Errorf("Expected port '3333' in output")
	}
	if !strings.Contains(output, "localhost") {
		t.Errorf("Expected host 'localhost' in output")
	}

	// Check for runner section
	if !strings.Contains(output, "Runner") {
		t.Errorf("Expected 'Runner' section in output")
	}
	if !strings.Contains(output, "3") || !strings.Contains(output, "MaxParallel") {
		t.Errorf("Expected 'MaxParallel: 3' in output")
	}

	// Check for MCP section
	if !strings.Contains(output, "MCP") {
		t.Errorf("Expected 'MCP' section in output")
	}
}

func TestConfigCommand_Execute_ShowsConfigPath(t *testing.T) {
	cfg := &UnifiedConfig{}
	cfg.Server.Port = 3333

	var out bytes.Buffer
	cmd := &ConfigCommand{
		Config: cfg,
		Out:    &out,
	}

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := out.String()

	// Should show config file location
	if !strings.Contains(output, "Config") && !strings.Contains(output, "config") {
		t.Errorf("Expected config file path mention in output")
	}
}

func TestConfigCommand_Execute_CustomValues(t *testing.T) {
	cfg := &UnifiedConfig{}
	cfg.Server.Port = 8080
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.BrainDir = "/custom/path"
	cfg.Server.EnableAuth = true
	cfg.Server.LogLevel = "debug"
	cfg.Runner.MaxParallel = 10
	cfg.Runner.PollInterval = 3000
	cfg.Runner.WorkDir = "/custom/work"
	cfg.Runner.ExcludeProjects = []string{"test-*", "temp-*"}

	var out bytes.Buffer
	cmd := &ConfigCommand{
		Config: cfg,
		Out:    &out,
	}

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := out.String()

	// Verify custom values appear
	if !strings.Contains(output, "8080") {
		t.Errorf("Expected custom port '8080' in output")
	}
	if !strings.Contains(output, "0.0.0.0") {
		t.Errorf("Expected custom host '0.0.0.0' in output")
	}
	if !strings.Contains(output, "/custom/path") {
		t.Errorf("Expected custom brain dir in output")
	}
	if !strings.Contains(output, "debug") {
		t.Errorf("Expected custom log level 'debug' in output")
	}
	if !strings.Contains(output, "10") || !strings.Contains(output, "MaxParallel") {
		t.Errorf("Expected custom MaxParallel '10' in output")
	}
}
