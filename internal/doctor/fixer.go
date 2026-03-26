package doctor

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/huynle/brain-api/cmd/brain/assets"
)

// fixBrainDirectory creates the brain directory structure.
func fixBrainDirectory(brainDir string, dryRun bool) error {
	if dryRun {
		return nil
	}

	// Create main directories
	dirs := []string{
		brainDir,
		filepath.Join(brainDir, ".zk"),
		filepath.Join(brainDir, ".zk", "templates"),
		filepath.Join(brainDir, "global"),
		filepath.Join(brainDir, "projects"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// fixTemplates restores missing or corrupted templates.
func fixTemplates(brainDir string, dryRun bool, force bool) error {
	if dryRun {
		return nil
	}

	templatesDir := filepath.Join(brainDir, ".zk", "templates")

	// Ensure templates directory exists
	if err := os.MkdirAll(templatesDir, 0755); err != nil {
		return fmt.Errorf("failed to create templates directory: %w", err)
	}

	// Get expected templates from assets
	expectedTemplates := assets.ListTemplates()

	for _, tmpl := range expectedTemplates {
		destPath := filepath.Join(templatesDir, tmpl)

		// Check if file exists
		if _, err := os.Stat(destPath); err == nil && !force {
			// File exists and no force flag, skip
			continue
		}

		// Get template content from assets
		content, err := assets.GetTemplate(tmpl)
		if err != nil {
			return fmt.Errorf("failed to load template %s: %w", tmpl, err)
		}

		// Write template
		if err := os.WriteFile(destPath, content, 0644); err != nil {
			return fmt.Errorf("failed to write template %s: %w", tmpl, err)
		}
	}

	return nil
}

// fixConfig restores the reference config.toml.
func fixConfig(brainDir string, dryRun bool, force bool) error {
	if dryRun {
		return nil
	}

	zkDir := filepath.Join(brainDir, ".zk")
	configPath := filepath.Join(zkDir, "config.toml")

	// Check if file exists
	if _, err := os.Stat(configPath); err == nil && !force {
		// File exists and no force flag, skip
		return nil
	}

	// Ensure .zk directory exists
	if err := os.MkdirAll(zkDir, 0755); err != nil {
		return fmt.Errorf("failed to create .zk directory: %w", err)
	}

	// Get reference config from assets
	content, err := assets.GetReferenceConfig()
	if err != nil {
		return fmt.Errorf("failed to load reference config: %w", err)
	}

	// Write config
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}
