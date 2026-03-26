package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/huynle/brain-api/cmd/brain/assets"
)

// OpenCodeTarget implements Target for OpenCode installation
type OpenCodeTarget struct {
	configPath string // defaults to ~/.config/opencode
}

// NewOpenCodeTarget creates a new OpenCodeTarget with default config path
func NewOpenCodeTarget() *OpenCodeTarget {
	home, err := os.UserHomeDir()
	if err != nil {
		return &OpenCodeTarget{configPath: "~/.config/opencode"}
	}
	return &OpenCodeTarget{
		configPath: filepath.Join(home, ".config", "opencode"),
	}
}

// ID returns the unique identifier for this target
func (t *OpenCodeTarget) ID() string {
	return "opencode"
}

// Name returns the human-readable name
func (t *OpenCodeTarget) Name() string {
	return "OpenCode"
}

// Description returns a description of what this target is
func (t *OpenCodeTarget) Description() string {
	return "OpenCode AI coding assistant"
}

// Exists checks if the target is already installed
func (t *OpenCodeTarget) Exists() bool {
	_, err := os.Stat(t.configPath)
	return !os.IsNotExist(err)
}

// Install performs the installation
func (t *OpenCodeTarget) Install(opts InstallOptions) error {
	// 1. Ensure plugin directory exists
	pluginDir := filepath.Join(t.configPath, "plugin")
	if err := ensureDir(pluginDir); err != nil {
		return fmt.Errorf("failed to create plugin directory: %w", err)
	}

	// 2. List all plugin files to install
	files, err := assets.ListPluginFiles("opencode")
	if err != nil {
		return fmt.Errorf("failed to list plugin files: %w", err)
	}

	// 3. For each file:
	for _, filename := range files {
		// Skip README.md
		if filename == "README.md" {
			continue
		}

		// Read from assets
		content, err := assets.GetPluginFile("opencode", filename)
		if err != nil {
			return fmt.Errorf("failed to read plugin file %s: %w", filename, err)
		}

		// Add auto-generated header
		header := generateHeader(filename)
		fullContent := append([]byte(header), content...)

		// Write to target
		destPath := filepath.Join(pluginDir, filename)

		// Check for existing file if not Force mode
		if !opts.Force {
			if _, err := os.Stat(destPath); !os.IsNotExist(err) {
				return fmt.Errorf("file exists: %s (use --force to overwrite)", destPath)
			}
		}

		// DryRun mode just prints
		if opts.DryRun {
			fmt.Printf("  [DRY RUN] Would install: %s\n", filename)
			continue
		}

		// Actually write
		if err := os.WriteFile(destPath, fullContent, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", filename, err)
		}

		fmt.Printf("  ✅ %s\n", filename)
	}

	return nil
}

// Uninstall removes the installed plugin
func (t *OpenCodeTarget) Uninstall() error {
	pluginDir := filepath.Join(t.configPath, "plugin")

	// List all plugin files that should be removed
	files, err := assets.ListPluginFiles("opencode")
	if err != nil {
		return fmt.Errorf("failed to list plugin files: %w", err)
	}

	// Remove each file (skip README.md)
	for _, filename := range files {
		if filename == "README.md" {
			continue
		}

		filePath := filepath.Join(pluginDir, filename)
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove %s: %w", filename, err)
		}
	}

	return nil
}

// Validate checks if the installation is valid and complete
func (t *OpenCodeTarget) Validate() error {
	pluginDir := filepath.Join(t.configPath, "plugin")

	// Check if plugin directory exists
	if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
		return fmt.Errorf("plugin directory does not exist: %s", pluginDir)
	}

	// List expected plugin files
	files, err := assets.ListPluginFiles("opencode")
	if err != nil {
		return fmt.Errorf("failed to list plugin files: %w", err)
	}

	// Check each file exists (skip README.md)
	missingFiles := []string{}
	for _, filename := range files {
		if filename == "README.md" {
			continue
		}

		filePath := filepath.Join(pluginDir, filename)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			missingFiles = append(missingFiles, filename)
		}
	}

	if len(missingFiles) > 0 {
		return fmt.Errorf("missing plugin files: %s", strings.Join(missingFiles, ", "))
	}

	return nil
}

// generateHeader creates auto-generated header for plugin files
func generateHeader(filename string) string {
	return fmt.Sprintf(`/**
 * AUTO-GENERATED FILE - DO NOT EDIT DIRECTLY
 * 
 * This file was installed by: brain install opencode
 * To update: brain install opencode --force
 * To check status: brain plugin-status
 * Source: https://github.com/huynle/brain-api
 * Generated: %s
 */

`, time.Now().Format(time.RFC3339))
}
