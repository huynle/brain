package plugins

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/huynle/brain-api/cmd/brain/assets"
)

// ClaudeTarget implements Target for Claude Code installation. It installs the
// shared brain skills (task graphs with depends_on, memory, automations, etc.)
// as personal Claude Code skills under ~/.claude/skills/<name>/SKILL.md.
type ClaudeTarget struct {
	configPath string // defaults to ~/.claude
}

// NewClaudeTarget creates a new ClaudeTarget with default config path
func NewClaudeTarget() *ClaudeTarget {
	home, err := os.UserHomeDir()
	if err != nil {
		return &ClaudeTarget{configPath: "~/.claude"}
	}
	return &ClaudeTarget{
		configPath: filepath.Join(home, ".claude"),
	}
}

// ID returns the unique identifier for this target
func (t *ClaudeTarget) ID() string {
	return "claude"
}

// Name returns the human-readable name
func (t *ClaudeTarget) Name() string {
	return "Claude Code"
}

// Description returns a description of what this target is
func (t *ClaudeTarget) Description() string {
	return "Claude Code AI coding agent (claude.ai/code)"
}

// Exists checks if the target config directory exists
func (t *ClaudeTarget) Exists() bool {
	_, err := os.Stat(t.configPath)
	return !os.IsNotExist(err)
}

// skillDestPath maps a shared skill path (e.g., "using-brain/SKILL.md") to its
// destination under ~/.claude/skills/.
func (t *ClaudeTarget) skillDestPath(relPath string) string {
	return filepath.Join(t.configPath, "skills", relPath)
}

// Install installs the shared brain skills into ~/.claude/skills/.
func (t *ClaudeTarget) Install(opts InstallOptions) error {
	files, err := assets.ListSharedSkillFiles()
	if err != nil {
		return fmt.Errorf("failed to list shared skills: %w", err)
	}

	installed := 0
	updated := 0
	identical := 0
	for _, relPath := range files {
		// Skip README.md files
		if filepath.Base(relPath) == "README.md" {
			continue
		}

		destPath := t.skillDestPath(relPath)
		displayPath := filepath.Join("skills", relPath)

		content, err := assets.GetSharedSkillFile(relPath)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", relPath, err)
		}

		if isMarkdownFile(relPath) {
			content = addMarkdownHeader(content, claudeMarkdownHeader())
		}

		existingContent, readErr := os.ReadFile(destPath)
		exists := readErr == nil
		if readErr != nil && !os.IsNotExist(readErr) {
			return fmt.Errorf("failed to read existing %s: %w", displayPath, readErr)
		}
		isIdentical := exists && bytes.Equal(existingContent, content)

		// Check for existing file if not Force mode
		if !opts.Force && exists {
			if isIdentical {
				identical++
				fmt.Printf("  Identical: %s (left untouched)\n", displayPath)
			} else if opts.DryRun {
				fmt.Printf("  [DRY RUN] Would skip (exists): %s\n", displayPath)
			}
			continue // skip silently instead of erroring — install all other files
		}

		if isIdentical {
			identical++
			fmt.Printf("  Identical: %s (left untouched)\n", displayPath)
			continue
		}

		// DryRun mode just prints
		if opts.DryRun {
			if exists {
				fmt.Printf("  [DRY RUN] Would update: %s -> %s\n", displayPath, destPath)
				updated++
			} else {
				fmt.Printf("  [DRY RUN] Would install: %s -> %s\n", displayPath, destPath)
				installed++
			}
			continue
		}

		// Ensure parent directory exists
		if err := ensureDir(filepath.Dir(destPath)); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", displayPath, err)
		}

		// Actually write
		if err := os.WriteFile(destPath, content, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", displayPath, err)
		}

		if exists {
			updated++
			fmt.Printf("  Updated: %s\n", displayPath)
		} else {
			installed++
			fmt.Printf("  Installed: %s\n", displayPath)
		}
	}

	if opts.DryRun {
		fmt.Printf("\n  [DRY RUN] Would install %d files, update %d files, leave %d identical files untouched\n", installed, updated, identical)
	} else {
		fmt.Printf("\n  %d files installed, %d updated, %d identical left untouched in %s\n", installed, updated, identical, filepath.Join(t.configPath, "skills"))
	}

	return nil
}

// Uninstall removes all installed brain skills.
func (t *ClaudeTarget) Uninstall() error {
	files, err := assets.ListSharedSkillFiles()
	if err != nil {
		return fmt.Errorf("failed to list shared skills: %w", err)
	}

	for _, relPath := range files {
		if filepath.Base(relPath) == "README.md" {
			continue
		}
		destPath := t.skillDestPath(relPath)
		if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove %s: %w", relPath, err)
		}
		// Best-effort removal of the now-empty skill directory
		_ = os.Remove(filepath.Dir(destPath))
	}

	return nil
}

// Validate checks if the installation is valid and complete.
func (t *ClaudeTarget) Validate() error {
	files, err := assets.ListSharedSkillFiles()
	if err != nil {
		return fmt.Errorf("failed to list shared skills: %w", err)
	}

	var missing []string
	for _, relPath := range files {
		if filepath.Base(relPath) == "README.md" {
			continue
		}
		if _, err := os.Stat(t.skillDestPath(relPath)); os.IsNotExist(err) {
			missing = append(missing, filepath.Join("skills", relPath))
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing files: %s", strings.Join(missing, ", "))
	}

	return nil
}

// claudeMarkdownHeader creates the auto-generated header for installed skills.
// It contains no timestamp so repeated installs stay byte-identical.
func claudeMarkdownHeader() string {
	return `<!--
AUTO-GENERATED FILE - DO NOT EDIT DIRECTLY

This file was installed by: brain install claude
To update: brain install claude --force
To check status: brain plugin-status
Source: https://github.com/huynle/brain-api
-->

`
}
