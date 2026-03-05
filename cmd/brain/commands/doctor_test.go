package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorCommand_Type(t *testing.T) {
	cmd := &DoctorCommand{}

	if cmd.Type() != "doctor" {
		t.Errorf("Type() = %q, want %q", cmd.Type(), "doctor")
	}
}

func TestDoctorCommand_Execute(t *testing.T) {
	t.Run("diagnose healthy brain", func(t *testing.T) {
		tmpDir := t.TempDir()
		setupTestBrain(t, tmpDir)

		var buf bytes.Buffer
		cfg := &UnifiedConfig{}
		cfg.Server.BrainDir = tmpDir

		cmd := &DoctorCommand{
			Config: cfg,
			Flags:  &DoctorFlags{},
			Out:    &buf,
		}

		err := cmd.Execute()

		if err != nil {
			t.Errorf("Execute() error = %v, want nil", err)
		}

		output := buf.String()
		if !strings.Contains(output, "✅") {
			t.Error("Expected success symbols in output")
		}
	})

	t.Run("diagnose missing brain", func(t *testing.T) {
		tmpDir := t.TempDir()
		nonExistent := filepath.Join(tmpDir, "nonexistent")

		var buf bytes.Buffer
		cfg := &UnifiedConfig{}
		cfg.Server.BrainDir = nonExistent

		cmd := &DoctorCommand{
			Config: cfg,
			Flags:  &DoctorFlags{},
			Out:    &buf,
		}

		err := cmd.Execute()

		if err != nil {
			t.Errorf("Execute() error = %v, want nil", err)
		}

		output := buf.String()
		if !strings.Contains(output, "❌") {
			t.Error("Expected failure symbols in output")
		}
		if !strings.Contains(output, "--fix") {
			t.Error("Expected suggestion to use --fix flag")
		}
	})

	t.Run("fix missing brain", func(t *testing.T) {
		tmpDir := t.TempDir()
		brainDir := filepath.Join(tmpDir, "brain")

		var buf bytes.Buffer
		cfg := &UnifiedConfig{}
		cfg.Server.BrainDir = brainDir

		cmd := &DoctorCommand{
			Config: cfg,
			Flags:  &DoctorFlags{Fix: true},
			Out:    &buf,
		}

		err := cmd.Execute()

		if err != nil {
			t.Errorf("Execute() error = %v, want nil", err)
		}

		// Verify directory was created
		if _, statErr := os.Stat(brainDir); statErr != nil {
			t.Error("Brain directory should be created with --fix")
		}

		output := buf.String()
		if !strings.Contains(output, "✅") {
			t.Errorf("Expected success symbols in output, got: %s", output)
		}
	})

	t.Run("verbose shows passed checks", func(t *testing.T) {
		tmpDir := t.TempDir()
		setupTestBrain(t, tmpDir)

		var buf bytes.Buffer
		cfg := &UnifiedConfig{}
		cfg.Server.BrainDir = tmpDir

		cmd := &DoctorCommand{
			Config: cfg,
			Flags:  &DoctorFlags{Verbose: true},
			Out:    &buf,
		}

		err := cmd.Execute()

		if err != nil {
			t.Errorf("Execute() error = %v, want nil", err)
		}

		output := buf.String()
		// Should show check details in verbose mode
		if !strings.Contains(output, "Brain Directory") || !strings.Contains(output, "✅") {
			t.Errorf("Verbose output should show check names, got: %s", output)
		}
	})

	t.Run("dry run shows what would be done", func(t *testing.T) {
		tmpDir := t.TempDir()
		brainDir := filepath.Join(tmpDir, "brain")

		var buf bytes.Buffer
		cfg := &UnifiedConfig{}
		cfg.Server.BrainDir = brainDir

		cmd := &DoctorCommand{
			Config: cfg,
			Flags:  &DoctorFlags{Fix: true, DryRun: true},
			Out:    &buf,
		}

		err := cmd.Execute()

		if err != nil {
			t.Errorf("Execute() error = %v, want nil", err)
		}

		// Verify directory was NOT created
		if _, statErr := os.Stat(brainDir); !os.IsNotExist(statErr) {
			t.Error("Brain directory should not be created with --dry-run")
		}

		output := buf.String()
		if !strings.Contains(output, "DRY RUN") && !strings.Contains(output, "Would") {
			t.Error("Expected dry run indication in output")
		}
	})
}

// setupTestBrain creates a complete brain installation for testing
func setupTestBrain(t *testing.T, brainDir string) {
	t.Helper()

	// Create directory structure
	dirs := []string{
		brainDir,
		filepath.Join(brainDir, ".zk"),
		filepath.Join(brainDir, ".zk", "templates"),
		filepath.Join(brainDir, "global"),
		filepath.Join(brainDir, "projects"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("failed to create directory %s: %v", dir, err)
		}
	}

	// Create all templates
	templates := []string{
		"decision.md", "execution.md", "exploration.md",
		"idea.md", "learning.md", "pattern.md",
		"plan.md", "report.md", "scratch.md",
		"summary.md", "task.md", "walkthrough.md",
		"default.md",
	}

	for _, tmpl := range templates {
		path := filepath.Join(brainDir, ".zk", "templates", tmpl)
		if err := os.WriteFile(path, []byte("---\ntitle: test\n---\n"), 0644); err != nil {
			t.Fatalf("failed to create template %s: %v", tmpl, err)
		}
	}

	// Create config
	configPath := filepath.Join(brainDir, ".zk", "config.toml")
	configContent := `[note]
id_length = 8
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to create config: %v", err)
	}
}
