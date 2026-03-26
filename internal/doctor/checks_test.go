package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckBrainDirectory(t *testing.T) {
	t.Run("directory exists and writable", func(t *testing.T) {
		tmpDir := t.TempDir()

		check := checkBrainDirectory(tmpDir)

		if check.Status != CheckStatusPass {
			t.Errorf("checkBrainDirectory() status = %v, want %v", check.Status, CheckStatusPass)
		}
		if check.Name != "brain-directory" {
			t.Errorf("checkBrainDirectory() name = %q, want %q", check.Name, "brain-directory")
		}
		if !check.Fixable {
			t.Error("checkBrainDirectory() should be fixable")
		}
	})

	t.Run("directory does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		nonExistent := filepath.Join(tmpDir, "nonexistent")

		check := checkBrainDirectory(nonExistent)

		if check.Status != CheckStatusFail {
			t.Errorf("checkBrainDirectory() status = %v, want %v", check.Status, CheckStatusFail)
		}
		if !check.Fixable {
			t.Error("checkBrainDirectory() should be fixable when directory missing")
		}
	})

	t.Run("directory not writable", func(t *testing.T) {
		tmpDir := t.TempDir()
		readOnlyDir := filepath.Join(tmpDir, "readonly")
		os.Mkdir(readOnlyDir, 0444)
		defer os.Chmod(readOnlyDir, 0755) // cleanup

		check := checkBrainDirectory(readOnlyDir)

		if check.Status != CheckStatusFail {
			t.Errorf("checkBrainDirectory() status = %v, want %v", check.Status, CheckStatusFail)
		}
	})
}

func TestCheckTemplates(t *testing.T) {
	t.Run("all templates present", func(t *testing.T) {
		tmpDir := t.TempDir()
		templatesDir := filepath.Join(tmpDir, ".zk", "templates")
		os.MkdirAll(templatesDir, 0755)

		// Create all expected templates
		expectedTemplates := []string{
			"decision.md", "execution.md", "exploration.md",
			"idea.md", "learning.md", "pattern.md",
			"plan.md", "report.md", "scratch.md",
			"summary.md", "task.md", "walkthrough.md",
			"default.md",
		}
		for _, tmpl := range expectedTemplates {
			os.WriteFile(filepath.Join(templatesDir, tmpl), []byte("test"), 0644)
		}

		check := checkTemplates(tmpDir)

		if check.Status != CheckStatusPass {
			t.Errorf("checkTemplates() status = %v, want %v", check.Status, CheckStatusPass)
		}
		if check.Name != "templates" {
			t.Errorf("checkTemplates() name = %q, want %q", check.Name, "templates")
		}
	})

	t.Run("missing templates", func(t *testing.T) {
		tmpDir := t.TempDir()
		templatesDir := filepath.Join(tmpDir, ".zk", "templates")
		os.MkdirAll(templatesDir, 0755)

		// Create only some templates
		os.WriteFile(filepath.Join(templatesDir, "task.md"), []byte("test"), 0644)

		check := checkTemplates(tmpDir)

		if check.Status != CheckStatusFail {
			t.Errorf("checkTemplates() status = %v, want %v", check.Status, CheckStatusFail)
		}
		if !check.Fixable {
			t.Error("checkTemplates() should be fixable when templates missing")
		}
	})

	t.Run("templates directory missing", func(t *testing.T) {
		tmpDir := t.TempDir()

		check := checkTemplates(tmpDir)

		if check.Status != CheckStatusFail {
			t.Errorf("checkTemplates() status = %v, want %v", check.Status, CheckStatusFail)
		}
		if !check.Fixable {
			t.Error("checkTemplates() should be fixable when directory missing")
		}
	})
}

func TestCheckConfig(t *testing.T) {
	t.Run("config exists and valid", func(t *testing.T) {
		tmpDir := t.TempDir()
		zkDir := filepath.Join(tmpDir, ".zk")
		os.MkdirAll(zkDir, 0755)

		configPath := filepath.Join(zkDir, "config.toml")
		validConfig := `[note]
id_length = 8
`
		os.WriteFile(configPath, []byte(validConfig), 0644)

		check := checkConfig(tmpDir)

		if check.Status != CheckStatusPass {
			t.Errorf("checkConfig() status = %v, want %v", check.Status, CheckStatusPass)
		}
		if check.Name != "config" {
			t.Errorf("checkConfig() name = %q, want %q", check.Name, "config")
		}
	})

	t.Run("config missing", func(t *testing.T) {
		tmpDir := t.TempDir()

		check := checkConfig(tmpDir)

		if check.Status != CheckStatusFail {
			t.Errorf("checkConfig() status = %v, want %v", check.Status, CheckStatusFail)
		}
		if !check.Fixable {
			t.Error("checkConfig() should be fixable when config missing")
		}
	})

	t.Run("config invalid TOML", func(t *testing.T) {
		tmpDir := t.TempDir()
		zkDir := filepath.Join(tmpDir, ".zk")
		os.MkdirAll(zkDir, 0755)

		configPath := filepath.Join(zkDir, "config.toml")
		invalidConfig := `[note
this is not valid TOML
`
		os.WriteFile(configPath, []byte(invalidConfig), 0644)

		check := checkConfig(tmpDir)

		if check.Status != CheckStatusFail {
			t.Errorf("checkConfig() status = %v, want %v", check.Status, CheckStatusFail)
		}
		if !check.Fixable {
			t.Error("checkConfig() should be fixable when config invalid")
		}
	})
}

func TestCheckDatabase(t *testing.T) {
	t.Run("database exists and accessible", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "brain.db")

		// Create empty file to simulate database
		os.WriteFile(dbPath, []byte{}, 0644)

		check := checkDatabase(tmpDir)

		if check.Status != CheckStatusPass {
			t.Errorf("checkDatabase() status = %v, want %v", check.Status, CheckStatusPass)
		}
		if check.Name != "database" {
			t.Errorf("checkDatabase() name = %q, want %q", check.Name, "database")
		}
	})

	t.Run("database missing", func(t *testing.T) {
		tmpDir := t.TempDir()

		check := checkDatabase(tmpDir)

		if check.Status != CheckStatusWarn {
			t.Errorf("checkDatabase() status = %v, want %v", check.Status, CheckStatusWarn)
		}
		if check.Fixable {
			t.Error("checkDatabase() should not be fixable - database created on first use")
		}
	})
}
