package doctor

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/huynle/brain-api/cmd/brain/assets"
)

// checkBrainDirectory checks if the brain directory exists and is writable.
func checkBrainDirectory(brainDir string) Check {
	check := Check{
		Name:    "brain-directory",
		Fixable: true,
	}

	// Check if directory exists
	info, err := os.Stat(brainDir)
	if err != nil {
		if os.IsNotExist(err) {
			check.Status = CheckStatusFail
			check.Message = fmt.Sprintf("Brain directory does not exist: %s", brainDir)
			return check
		}
		check.Status = CheckStatusFail
		check.Message = fmt.Sprintf("Cannot access brain directory: %v", err)
		return check
	}

	if !info.IsDir() {
		check.Status = CheckStatusFail
		check.Message = fmt.Sprintf("Path exists but is not a directory: %s", brainDir)
		return check
	}

	// Check if writable by attempting to create a temp file
	testFile := filepath.Join(brainDir, ".write_test")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		check.Status = CheckStatusFail
		check.Message = fmt.Sprintf("Brain directory is not writable: %v", err)
		return check
	}
	os.Remove(testFile)

	check.Status = CheckStatusPass
	check.Message = fmt.Sprintf("Brain directory exists and is writable: %s", brainDir)
	return check
}

// checkTemplates checks if all required templates are present.
func checkTemplates(brainDir string) Check {
	check := Check{
		Name:    "templates",
		Fixable: true,
	}

	templatesDir := filepath.Join(brainDir, ".zk", "templates")

	// Check if templates directory exists
	if _, err := os.Stat(templatesDir); os.IsNotExist(err) {
		check.Status = CheckStatusFail
		check.Message = "Templates directory does not exist"
		return check
	}

	// Get expected templates from assets
	expectedTemplates := assets.ListTemplates()
	if len(expectedTemplates) == 0 {
		check.Status = CheckStatusWarn
		check.Message = "Cannot verify templates: asset list empty"
		return check
	}

	// Check each template
	missing := []string{}
	for _, tmpl := range expectedTemplates {
		path := filepath.Join(templatesDir, tmpl)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			missing = append(missing, tmpl)
		}
	}

	if len(missing) > 0 {
		check.Status = CheckStatusFail
		check.Message = fmt.Sprintf("Missing %d templates: %v", len(missing), missing)
		return check
	}

	check.Status = CheckStatusPass
	check.Message = fmt.Sprintf("All %d templates present", len(expectedTemplates))
	return check
}

// checkConfig checks if config.toml exists and is valid.
func checkConfig(brainDir string) Check {
	check := Check{
		Name:    "config",
		Fixable: true,
	}

	configPath := filepath.Join(brainDir, ".zk", "config.toml")

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		check.Status = CheckStatusFail
		check.Message = "Config file does not exist: .zk/config.toml"
		return check
	}

	// Try to parse as TOML
	var config map[string]interface{}
	if _, err := toml.DecodeFile(configPath, &config); err != nil {
		check.Status = CheckStatusFail
		check.Message = fmt.Sprintf("Config file is not valid TOML: %v", err)
		return check
	}

	check.Status = CheckStatusPass
	check.Message = "Config file exists and is valid TOML"
	return check
}

// checkDatabase checks if the database file exists and is accessible.
func checkDatabase(brainDir string) Check {
	check := Check{
		Name:    "database",
		Fixable: false, // Database is created automatically on first use
	}

	dbPath := filepath.Join(brainDir, "brain.db")

	// Check if database file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		check.Status = CheckStatusWarn
		check.Message = "Database file does not exist (will be created on first use)"
		return check
	}

	check.Status = CheckStatusPass
	check.Message = "Database file exists"
	return check
}
