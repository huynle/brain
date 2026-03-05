package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/huynle/brain-api/cmd/brain/assets"
)

// InitFlags holds flags for the init command.
type InitFlags struct {
	Force  bool
	DryRun bool
}

// InitCommand implements the init command.
type InitCommand struct {
	Config *UnifiedConfig
	Flags  *InitFlags
	Out    io.Writer
}

// Type returns the command type.
func (c *InitCommand) Type() string {
	return "init"
}

// Execute runs the init command.
func (c *InitCommand) Execute() error {
	// Get writer
	out := c.Out
	if out == nil {
		out = os.Stdout
	}

	// Expand tilde in brain directory path
	brainDir := expandPath(c.Config.Server.BrainDir)

	// Track counts for summary
	createdCount := 0
	skippedCount := 0

	// Define directories to create
	dirs := []string{
		brainDir,
		filepath.Join(brainDir, ".zk"),
		filepath.Join(brainDir, ".zk", "templates"),
		filepath.Join(brainDir, "global"),
		filepath.Join(brainDir, "projects"),
	}

	// Create directories
	if c.Flags.DryRun {
		fmt.Fprintf(out, "DRY RUN: Would create directories in %s\n", brainDir)
		for _, dir := range dirs {
			fmt.Fprintf(out, "  - %s\n", dir)
		}
	} else {
		for _, dir := range dirs {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dir, err)
			}
		}
	}

	// Copy templates from embedded assets
	templates := assets.ListTemplates()
	templatesDir := filepath.Join(brainDir, ".zk", "templates")

	for _, templateName := range templates {
		destPath := filepath.Join(templatesDir, templateName)

		// Check if file exists
		exists := fileExists(destPath)

		if exists && !c.Flags.Force {
			skippedCount++
			if !c.Flags.DryRun {
				fmt.Fprintf(out, "⏭  Skipped %s (already exists)\n", templateName)
			}
			continue
		}

		if c.Flags.DryRun {
			if exists {
				fmt.Fprintf(out, "DRY RUN: Would overwrite %s\n", templateName)
			} else {
				fmt.Fprintf(out, "DRY RUN: Would create %s\n", templateName)
			}
			createdCount++
			continue
		}

		// Get template content
		content, err := assets.GetTemplate(templateName)
		if err != nil {
			fmt.Fprintf(out, "⚠️  Failed to load template %s: %v\n", templateName, err)
			continue
		}

		// Write template
		if err := os.WriteFile(destPath, content, 0644); err != nil {
			fmt.Fprintf(out, "⚠️  Failed to write template %s: %v\n", templateName, err)
			continue
		}

		createdCount++
		fmt.Fprintf(out, "✅ Created %s\n", templateName)
	}

	// Copy config.toml
	configPath := filepath.Join(brainDir, ".zk", "config.toml")
	configExists := fileExists(configPath)

	if configExists && !c.Flags.Force {
		skippedCount++
		if !c.Flags.DryRun {
			fmt.Fprintf(out, "⏭  Skipped config.toml (already exists)\n")
		}
	} else {
		if c.Flags.DryRun {
			if configExists {
				fmt.Fprintf(out, "DRY RUN: Would overwrite config.toml\n")
			} else {
				fmt.Fprintf(out, "DRY RUN: Would create config.toml\n")
			}
		} else {
			configContent, err := assets.GetReferenceConfig()
			if err != nil {
				return fmt.Errorf("failed to load reference config: %w", err)
			}

			if err := os.WriteFile(configPath, configContent, 0644); err != nil {
				return fmt.Errorf("failed to write config.toml: %w", err)
			}

			fmt.Fprintf(out, "✅ Created config.toml\n")
		}
		createdCount++
	}

	// Print summary
	if c.Flags.DryRun {
		fmt.Fprintf(out, "\nDRY RUN Summary:\n")
		fmt.Fprintf(out, "  Would create/update: %d files\n", createdCount)
	} else {
		fmt.Fprintf(out, "\n✅ Initialization complete!\n")
		fmt.Fprintf(out, "  Created: %d files\n", createdCount)
		if skippedCount > 0 {
			fmt.Fprintf(out, "  Skipped: %d files (use --force to overwrite)\n", skippedCount)
		}
	}

	return nil
}

// fileExists checks if a file or directory exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
