package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDoctorService_Diagnose(t *testing.T) {
	t.Run("all checks pass", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Setup complete brain installation
		setupCompleteBrain(t, tmpDir)

		opts := DoctorOptions{
			BrainDir: tmpDir,
			Verbose:  true,
		}

		service := NewDoctorService()
		result, err := service.Diagnose(opts)

		if err != nil {
			t.Errorf("Diagnose() error = %v, want nil", err)
		}

		// Check that all checks passed
		for _, check := range result.Checks {
			if check.Status == CheckStatusFail {
				t.Errorf("check %q failed: %s", check.Name, check.Message)
			}
		}
	})

	t.Run("detects missing directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		nonExistent := filepath.Join(tmpDir, "nonexistent")

		opts := DoctorOptions{
			BrainDir: nonExistent,
		}

		service := NewDoctorService()
		result, err := service.Diagnose(opts)

		if err != nil {
			t.Errorf("Diagnose() error = %v, want nil", err)
		}

		// Check that brain-directory check failed
		var dirCheck *Check
		for i := range result.Checks {
			if result.Checks[i].Name == "brain-directory" {
				dirCheck = &result.Checks[i]
				break
			}
		}

		if dirCheck == nil {
			t.Fatal("brain-directory check not found")
		}
		if dirCheck.Status != CheckStatusFail {
			t.Errorf("brain-directory status = %v, want %v", dirCheck.Status, CheckStatusFail)
		}
	})

	t.Run("detects missing templates", func(t *testing.T) {
		tmpDir := t.TempDir()
		os.MkdirAll(filepath.Join(tmpDir, ".zk", "templates"), 0755)

		// Create only one template
		os.WriteFile(filepath.Join(tmpDir, ".zk", "templates", "task.md"), []byte("test"), 0644)

		opts := DoctorOptions{
			BrainDir: tmpDir,
		}

		service := NewDoctorService()
		result, err := service.Diagnose(opts)

		if err != nil {
			t.Errorf("Diagnose() error = %v, want nil", err)
		}

		// Check that templates check failed
		var templatesCheck *Check
		for i := range result.Checks {
			if result.Checks[i].Name == "templates" {
				templatesCheck = &result.Checks[i]
				break
			}
		}

		if templatesCheck == nil {
			t.Fatal("templates check not found")
		}
		if templatesCheck.Status != CheckStatusFail {
			t.Errorf("templates status = %v, want %v", templatesCheck.Status, CheckStatusFail)
		}
	})
}

func TestDoctorService_Fix(t *testing.T) {
	t.Run("fixes all issues", func(t *testing.T) {
		tmpDir := t.TempDir()
		nonExistent := filepath.Join(tmpDir, "brain")

		opts := DoctorOptions{
			BrainDir: nonExistent,
			Fix:      true,
		}

		service := NewDoctorService()
		result, err := service.Fix(opts)

		if err != nil {
			t.Errorf("Fix() error = %v, want nil", err)
		}

		// Verify directory was created
		if _, statErr := os.Stat(nonExistent); statErr != nil {
			t.Error("brain directory should be created")
		}

		// Verify templates were created
		templatesDir := filepath.Join(nonExistent, ".zk", "templates")
		entries, _ := os.ReadDir(templatesDir)
		if len(entries) == 0 {
			t.Error("templates should be created")
		}

		// Verify config was created
		configPath := filepath.Join(nonExistent, ".zk", "config.toml")
		if _, statErr := os.Stat(configPath); statErr != nil {
			t.Error("config should be created")
		}

		// Check result
		hasFailures := false
		for _, check := range result.Checks {
			if check.Status == CheckStatusFail {
				hasFailures = true
			}
		}
		if hasFailures {
			t.Error("Fix should resolve all failures")
		}
	})

	t.Run("dry run does not modify files", func(t *testing.T) {
		tmpDir := t.TempDir()
		nonExistent := filepath.Join(tmpDir, "brain")

		opts := DoctorOptions{
			BrainDir: nonExistent,
			Fix:      true,
			DryRun:   true,
		}

		service := NewDoctorService()
		_, err := service.Fix(opts)

		if err != nil {
			t.Errorf("Fix() error = %v, want nil", err)
		}

		// Verify directory was NOT created
		if _, statErr := os.Stat(nonExistent); !os.IsNotExist(statErr) {
			t.Error("brain directory should not be created in dry run")
		}
	})

	t.Run("force flag with partial brain", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create directory structure but with missing templates
		os.MkdirAll(filepath.Join(tmpDir, ".zk", "templates"), 0755)

		// Create only one template with custom content
		customContent := "custom content"
		taskPath := filepath.Join(tmpDir, ".zk", "templates", "task.md")
		os.WriteFile(taskPath, []byte(customContent), 0644)

		opts := DoctorOptions{
			BrainDir: tmpDir,
			Fix:      true,
			Force:    true,
		}

		service := NewDoctorService()
		_, err := service.Fix(opts)

		if err != nil {
			t.Errorf("Fix() error = %v, want nil", err)
		}

		// Verify template was overwritten (force=true means overwrite existing)
		content, readErr := os.ReadFile(taskPath)
		if readErr != nil {
			t.Fatalf("cannot read template: %v", readErr)
		}
		if string(content) == customContent {
			t.Error("template should be overwritten with force flag")
		}

		// Verify all other templates were created
		templatesDir := filepath.Join(tmpDir, ".zk", "templates")
		entries, _ := os.ReadDir(templatesDir)
		if len(entries) < 10 {
			t.Errorf("expected at least 10 templates, got %d", len(entries))
		}
	})
}

func TestNewDoctorService(t *testing.T) {
	service := NewDoctorService()

	if service == nil {
		t.Error("NewDoctorService() should return non-nil service")
	}
}

// Helper function to setup a complete brain installation for testing
func setupCompleteBrain(t *testing.T, brainDir string) {
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
