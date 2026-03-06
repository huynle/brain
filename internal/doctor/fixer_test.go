package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFixBrainDirectory(t *testing.T) {
	t.Run("create missing directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		brainDir := filepath.Join(tmpDir, "brain")

		err := fixBrainDirectory(brainDir, false)

		if err != nil {
			t.Errorf("fixBrainDirectory() error = %v, want nil", err)
		}

		// Verify directory was created
		info, statErr := os.Stat(brainDir)
		if statErr != nil {
			t.Fatalf("directory not created: %v", statErr)
		}
		if !info.IsDir() {
			t.Error("created path is not a directory")
		}
	})

	t.Run("dry run does not create directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		brainDir := filepath.Join(tmpDir, "brain")

		err := fixBrainDirectory(brainDir, true)

		if err != nil {
			t.Errorf("fixBrainDirectory() error = %v, want nil", err)
		}

		// Verify directory was NOT created
		if _, statErr := os.Stat(brainDir); !os.IsNotExist(statErr) {
			t.Error("directory should not be created in dry run mode")
		}
	})

	t.Run("creates nested directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		brainDir := filepath.Join(tmpDir, "brain")

		err := fixBrainDirectory(brainDir, false)
		if err != nil {
			t.Fatalf("fixBrainDirectory() error = %v", err)
		}

		// Verify expected subdirectories
		subdirs := []string{
			".zk",
			filepath.Join(".zk", "templates"),
			"global",
			"projects",
		}

		for _, subdir := range subdirs {
			path := filepath.Join(brainDir, subdir)
			info, statErr := os.Stat(path)
			if statErr != nil {
				t.Errorf("subdirectory %s not created: %v", subdir, statErr)
			} else if !info.IsDir() {
				t.Errorf("path %s is not a directory", subdir)
			}
		}
	})
}

func TestFixTemplates(t *testing.T) {
	t.Run("restore missing templates", func(t *testing.T) {
		tmpDir := t.TempDir()
		templatesDir := filepath.Join(tmpDir, ".zk", "templates")
		os.MkdirAll(templatesDir, 0755)

		err := fixTemplates(tmpDir, false, false)

		if err != nil {
			t.Errorf("fixTemplates() error = %v, want nil", err)
		}

		// Verify all templates were created
		expectedTemplates := []string{
			"decision.md", "execution.md", "exploration.md",
			"idea.md", "learning.md", "pattern.md",
			"plan.md", "report.md", "scratch.md",
			"summary.md", "task.md", "walkthrough.md",
			"default.md",
		}

		for _, tmpl := range expectedTemplates {
			path := filepath.Join(templatesDir, tmpl)
			if _, statErr := os.Stat(path); statErr != nil {
				t.Errorf("template %s not created: %v", tmpl, statErr)
			}
		}
	})

	t.Run("skip existing templates without force", func(t *testing.T) {
		tmpDir := t.TempDir()
		templatesDir := filepath.Join(tmpDir, ".zk", "templates")
		os.MkdirAll(templatesDir, 0755)

		// Create one template with custom content
		customContent := "custom content"
		customPath := filepath.Join(templatesDir, "task.md")
		os.WriteFile(customPath, []byte(customContent), 0644)

		err := fixTemplates(tmpDir, false, false)

		if err != nil {
			t.Errorf("fixTemplates() error = %v, want nil", err)
		}

		// Verify custom content preserved
		content, readErr := os.ReadFile(customPath)
		if readErr != nil {
			t.Fatalf("cannot read template: %v", readErr)
		}
		if string(content) != customContent {
			t.Error("existing template was overwritten without force flag")
		}
	})

	t.Run("overwrite existing templates with force", func(t *testing.T) {
		tmpDir := t.TempDir()
		templatesDir := filepath.Join(tmpDir, ".zk", "templates")
		os.MkdirAll(templatesDir, 0755)

		// Create one template with custom content
		customContent := "custom content"
		customPath := filepath.Join(templatesDir, "task.md")
		os.WriteFile(customPath, []byte(customContent), 0644)

		err := fixTemplates(tmpDir, false, true)

		if err != nil {
			t.Errorf("fixTemplates() error = %v, want nil", err)
		}

		// Verify custom content was overwritten
		content, readErr := os.ReadFile(customPath)
		if readErr != nil {
			t.Fatalf("cannot read template: %v", readErr)
		}
		if string(content) == customContent {
			t.Error("existing template should be overwritten with force flag")
		}
	})

	t.Run("dry run does not create files", func(t *testing.T) {
		tmpDir := t.TempDir()
		templatesDir := filepath.Join(tmpDir, ".zk", "templates")
		os.MkdirAll(templatesDir, 0755)

		err := fixTemplates(tmpDir, true, false)

		if err != nil {
			t.Errorf("fixTemplates() error = %v, want nil", err)
		}

		// Verify no templates were created
		entries, _ := os.ReadDir(templatesDir)
		if len(entries) > 0 {
			t.Error("files should not be created in dry run mode")
		}
	})
}

func TestFixConfig(t *testing.T) {
	t.Run("restore missing config", func(t *testing.T) {
		tmpDir := t.TempDir()
		zkDir := filepath.Join(tmpDir, ".zk")
		os.MkdirAll(zkDir, 0755)

		err := fixConfig(tmpDir, false, false)

		if err != nil {
			t.Errorf("fixConfig() error = %v, want nil", err)
		}

		// Verify config was created
		configPath := filepath.Join(zkDir, "config.toml")
		if _, statErr := os.Stat(configPath); statErr != nil {
			t.Errorf("config not created: %v", statErr)
		}
	})

	t.Run("skip existing config without force", func(t *testing.T) {
		tmpDir := t.TempDir()
		zkDir := filepath.Join(tmpDir, ".zk")
		os.MkdirAll(zkDir, 0755)

		// Create config with custom content
		customContent := "# custom config"
		configPath := filepath.Join(zkDir, "config.toml")
		os.WriteFile(configPath, []byte(customContent), 0644)

		err := fixConfig(tmpDir, false, false)

		if err != nil {
			t.Errorf("fixConfig() error = %v, want nil", err)
		}

		// Verify custom content preserved
		content, readErr := os.ReadFile(configPath)
		if readErr != nil {
			t.Fatalf("cannot read config: %v", readErr)
		}
		if string(content) != customContent {
			t.Error("existing config was overwritten without force flag")
		}
	})

	t.Run("overwrite existing config with force", func(t *testing.T) {
		tmpDir := t.TempDir()
		zkDir := filepath.Join(tmpDir, ".zk")
		os.MkdirAll(zkDir, 0755)

		// Create config with custom content
		customContent := "# custom config"
		configPath := filepath.Join(zkDir, "config.toml")
		os.WriteFile(configPath, []byte(customContent), 0644)

		err := fixConfig(tmpDir, false, true)

		if err != nil {
			t.Errorf("fixConfig() error = %v, want nil", err)
		}

		// Verify custom content was overwritten
		content, readErr := os.ReadFile(configPath)
		if readErr != nil {
			t.Fatalf("cannot read config: %v", readErr)
		}
		if string(content) == customContent {
			t.Error("existing config should be overwritten with force flag")
		}
	})

	t.Run("dry run does not create file", func(t *testing.T) {
		tmpDir := t.TempDir()
		zkDir := filepath.Join(tmpDir, ".zk")
		os.MkdirAll(zkDir, 0755)

		err := fixConfig(tmpDir, true, false)

		if err != nil {
			t.Errorf("fixConfig() error = %v, want nil", err)
		}

		// Verify config was NOT created
		configPath := filepath.Join(zkDir, "config.toml")
		if _, statErr := os.Stat(configPath); !os.IsNotExist(statErr) {
			t.Error("config should not be created in dry run mode")
		}
	})
}
