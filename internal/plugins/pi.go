package plugins

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/huynle/brain-api/cmd/brain/assets"
)

// PiTarget implements Target for Pi coding agent installation
type PiTarget struct {
	configPath string // defaults to ~/.pi
}

// NewPiTarget creates a new PiTarget with default config path
func NewPiTarget() *PiTarget {
	home, err := os.UserHomeDir()
	if err != nil {
		return &PiTarget{configPath: "~/.pi"}
	}
	return &PiTarget{
		configPath: filepath.Join(home, ".pi"),
	}
}

// ID returns the unique identifier for this target
func (t *PiTarget) ID() string {
	return "pi"
}

// Name returns the human-readable name
func (t *PiTarget) Name() string {
	return "Pi Coding Agent"
}

// Description returns a description of what this target is
func (t *PiTarget) Description() string {
	return "Pi AI coding agent (pi.dev)"
}

// Exists checks if the target config directory exists
func (t *PiTarget) Exists() bool {
	_, err := os.Stat(t.configPath)
	return !os.IsNotExist(err)
}

// piComponentDir maps embedded asset prefixes to their Pi config subdirectories.
var piComponentDir = map[string]string{
	"brain-agents": "brain-agents",
	"extensions":   "extensions",
}

// Install performs the installation of all brain components for Pi.
func (t *PiTarget) Install(opts InstallOptions) error {
	// List all embedded files recursively
	files, err := assets.ListPluginFilesRecursive("pi")
	if err != nil {
		return fmt.Errorf("failed to list plugin files: %w", err)
	}

	installed := 0
	updated := 0
	identical := 0
	for _, relPath := range files {
		// Skip README.md files
		if filepath.Base(relPath) == "README.md" {
			continue
		}

		// Determine destination path
		destPath := t.resolveDestPath(relPath)
		if destPath == "" {
			continue
		}

		// Read from assets
		content, err := assets.GetPluginFile("pi", relPath)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", relPath, err)
		}

		// Add auto-generated header for code files (.ts, .js)
		if isCodeFile(relPath) {
			header := generatePiHeader(relPath)
			content = append([]byte(header), content...)
		} else if isMarkdownFile(relPath) {
			header := generatePiMarkdownHeader(relPath)
			content = append([]byte(header), content...)
		}

		existingContent, readErr := os.ReadFile(destPath)
		exists := readErr == nil
		if readErr != nil && !os.IsNotExist(readErr) {
			return fmt.Errorf("failed to read existing %s: %w", relPath, readErr)
		}
		isIdentical := exists && bytes.Equal(existingContent, content)

		// Check for existing file if not Force mode
		if !opts.Force && exists {
			if isIdentical {
				identical++
				fmt.Printf("  Identical: %s (left untouched)\n", relPath)
			} else if opts.DryRun {
				fmt.Printf("  [DRY RUN] Would skip (exists): %s\n", relPath)
			}
			continue // skip silently
		}

		if isIdentical {
			identical++
			fmt.Printf("  Identical: %s (left untouched)\n", relPath)
			continue
		}

		// DryRun mode just prints
		if opts.DryRun {
			if exists {
				fmt.Printf("  [DRY RUN] Would update: %s -> %s\n", relPath, destPath)
				updated++
			} else {
				fmt.Printf("  [DRY RUN] Would install: %s -> %s\n", relPath, destPath)
				installed++
			}
			continue
		}

		// Ensure parent directory exists
		if err := ensureDir(filepath.Dir(destPath)); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", relPath, err)
		}

		// Actually write
		if err := os.WriteFile(destPath, content, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", relPath, err)
		}

		if exists {
			updated++
			fmt.Printf("  Updated: %s\n", relPath)
		} else {
			installed++
			fmt.Printf("  Installed: %s\n", relPath)
		}
	}

	if opts.DryRun {
		fmt.Printf("\n  [DRY RUN] Would install %d files, update %d files, leave %d identical files untouched\n", installed, updated, identical)
	} else {
		fmt.Printf("\n  %d files installed, %d updated, %d identical left untouched in %s\n", installed, updated, identical, t.configPath)
	}

	return nil
}

// resolveDestPath maps an embedded asset path to its destination in the Pi config.
// Files are mapped directly by their directory prefix (brain-agents/, extensions/).
func (t *PiTarget) resolveDestPath(relPath string) string {
	parts := strings.SplitN(relPath, string(os.PathSeparator), 2)

	// Top-level files — skip (Pi doesn't use top-level plugin files)
	if len(parts) == 1 {
		return ""
	}

	// Subdirectory files (e.g., brain-agents/tdd-dev/config.json -> ~/.pi/brain-agents/tdd-dev/config.json)
	prefix := parts[0]
	if _, ok := piComponentDir[prefix]; ok {
		return filepath.Join(t.configPath, relPath)
	}

	// Unknown prefix — skip
	return ""
}

// Uninstall removes all installed brain components.
func (t *PiTarget) Uninstall() error {
	files, err := assets.ListPluginFilesRecursive("pi")
	if err != nil {
		return fmt.Errorf("failed to list plugin files: %w", err)
	}

	for _, relPath := range files {
		if filepath.Base(relPath) == "README.md" {
			continue
		}
		destPath := t.resolveDestPath(relPath)
		if destPath == "" {
			continue
		}
		if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove %s: %w", relPath, err)
		}
	}

	return nil
}

// Validate checks if the installation is valid and complete.
func (t *PiTarget) Validate() error {
	// Check pi binary is on PATH
	if _, err := exec.LookPath("pi"); err != nil {
		return fmt.Errorf("pi binary not found on PATH: install from https://pi.dev")
	}

	// Check all expected files exist
	files, err := assets.ListPluginFilesRecursive("pi")
	if err != nil {
		return fmt.Errorf("failed to list plugin files: %w", err)
	}

	var missing []string
	for _, relPath := range files {
		if filepath.Base(relPath) == "README.md" {
			continue
		}
		destPath := t.resolveDestPath(relPath)
		if destPath == "" {
			continue
		}
		if _, err := os.Stat(destPath); os.IsNotExist(err) {
			missing = append(missing, relPath)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing files: %s", strings.Join(missing, ", "))
	}

	return nil
}

// generatePiHeader creates auto-generated header for Pi code files
func generatePiHeader(filename string) string {
	return fmt.Sprintf(`/**
 * AUTO-GENERATED FILE - DO NOT EDIT DIRECTLY
 * 
 * This file was installed by: brain install pi
 * To update: brain install pi --force
 * To check status: brain plugin-status
 * Source: https://github.com/huynle/brain-api
 * Generated: %s
 */

`, time.Now().Format(time.RFC3339))
}

// generatePiMarkdownHeader creates auto-generated header for Pi markdown files
func generatePiMarkdownHeader(filename string) string {
	return fmt.Sprintf(`<!--
AUTO-GENERATED FILE - DO NOT EDIT DIRECTLY

This file was installed by: brain install pi
To update: brain install pi
To check status: brain doctor
Source: https://github.com/huynle/brain-api
Generated: %s
-->

`, time.Now().Format(time.RFC3339))
}
