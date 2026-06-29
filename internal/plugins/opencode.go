package plugins

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// componentDir maps embedded asset prefixes to their opencode config subdirectories.
var componentDir = map[string]string{
	"plugin":  "plugin",
	"skill":   "skill",
	"command": "command",
	"agent":   "agent",
	"tool":    "tool",
}

var retiredOpenCodeFiles = []string{
	"plugin/brain.ts",
	"plugin/brain-planning.ts",
	"skill/brain-dream-context/SKILL.md",
	"skill/brain-planning/SKILL.md",
	"skill/project-planning/SKILL.md",
	"skill/writing-plans/SKILL.md",
	"command/checkout-plan.md",
	"command/execute-plan.md",
	"command/validate-plan.md",
	"tool/plan-checkout/index.ts",
}

// Install performs the installation of all brain components for OpenCode.
func (t *OpenCodeTarget) Install(opts InstallOptions) error {
	// List all embedded files recursively
	files, err := assets.ListPluginFilesRecursive("opencode")
	if err != nil {
		return fmt.Errorf("failed to list plugin files: %w", err)
	}

	removed := 0
	if opts.Force {
		var err error
		removed, err = t.removeRetiredFiles(opts.DryRun)
		if err != nil {
			return err
		}
	}

	installed := 0
	updated := 0
	identical := 0
	for _, relPath := range files {
		// Skip README.md files
		if filepath.Base(relPath) == "README.md" {
			continue
		}

		// Determine destination directory based on file type
		destPath := t.resolveDestPath(relPath)
		if destPath == "" {
			continue
		}

		// Read from assets
		content, err := assets.GetPluginFile("opencode", relPath)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", relPath, err)
		}

		// Add auto-generated header for code files (.ts, .js)
		if isCodeFile(relPath) {
			header := generateHeader(relPath)
			content = append([]byte(header), content...)
		} else if isMarkdownFile(relPath) {
			content = addMarkdownHeader(content, relPath)
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
			continue // skip silently instead of erroring — install all other files
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
		fmt.Printf("\n  [DRY RUN] Would install %d files, update %d files, remove %d retired files, leave %d identical files untouched\n", installed, updated, removed, identical)
	} else {
		fmt.Printf("\n  %d files installed, %d updated, %d retired removed, %d identical left untouched in %s\n", installed, updated, removed, identical, t.configPath)
	}

	return nil
}

func (t *OpenCodeTarget) removeRetiredFiles(dryRun bool) (int, error) {
	removed := 0
	for _, relPath := range retiredOpenCodeFiles {
		destPath := t.resolveDestPath(relPath)
		if destPath == "" {
			continue
		}

		content, err := os.ReadFile(destPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return removed, fmt.Errorf("failed to read retired %s: %w", relPath, err)
		}
		if !bytes.Contains(content, []byte("AUTO-GENERATED FILE - DO NOT EDIT DIRECTLY")) || !bytes.Contains(content, []byte("brain install opencode")) {
			continue
		}

		if dryRun {
			fmt.Printf("  [DRY RUN] Would remove retired: %s\n", relPath)
			removed++
			continue
		}
		if err := os.Remove(destPath); err != nil {
			return removed, fmt.Errorf("failed to remove retired %s: %w", relPath, err)
		}
		_ = os.Remove(filepath.Dir(destPath))
		fmt.Printf("  Removed retired: %s\n", relPath)
		removed++
	}
	return removed, nil
}

// resolveDestPath maps an embedded asset path to its destination in the opencode config.
// Top-level .ts/.js files go to plugin/, subdirectories map by name.
func (t *OpenCodeTarget) resolveDestPath(relPath string) string {
	parts := strings.SplitN(relPath, string(os.PathSeparator), 2)

	// Top-level files (e.g., a future *.ts plugin) -> plugin/
	if len(parts) == 1 {
		return filepath.Join(t.configPath, "plugin", relPath)
	}

	// Subdirectory files (e.g., skill/brain-memory/SKILL.md -> skill/brain-memory/SKILL.md)
	prefix := parts[0]
	if _, ok := componentDir[prefix]; ok {
		return filepath.Join(t.configPath, relPath)
	}

	// Unknown prefix — skip
	return ""
}

func isCodeFile(path string) bool {
	ext := filepath.Ext(path)
	return ext == ".ts" || ext == ".js"
}

func isMarkdownFile(path string) bool {
	return filepath.Ext(path) == ".md"
}

// addMarkdownHeader inserts the generated header without hiding YAML frontmatter.
// OpenCode requires component markdown files such as SKILL.md to start with
// frontmatter, so generated comments must be placed after the closing delimiter
// when frontmatter is present.
func addMarkdownHeader(content []byte, filename string) []byte {
	header := []byte(generateMarkdownHeader(filename))
	frontmatterEnd := yamlFrontmatterEnd(content)
	if frontmatterEnd == 0 {
		return append(header, content...)
	}

	result := make([]byte, 0, len(content)+len(header))
	result = append(result, content[:frontmatterEnd]...)
	result = append(result, header...)
	result = append(result, content[frontmatterEnd:]...)
	return result
}

// yamlFrontmatterEnd returns the byte offset immediately after the closing
// frontmatter delimiter line, or 0 when the file does not start with YAML
// frontmatter.
func yamlFrontmatterEnd(content []byte) int {
	if !bytes.HasPrefix(content, []byte("---\n")) && !bytes.HasPrefix(content, []byte("---\r\n")) {
		return 0
	}

	lineStart := 0
	for lineStart < len(content) {
		lineEndRel := bytes.IndexByte(content[lineStart:], '\n')
		lineEnd := len(content)
		nextLineStart := len(content)
		if lineEndRel >= 0 {
			lineEnd = lineStart + lineEndRel
			nextLineStart = lineEnd + 1
		}

		line := bytes.TrimRight(content[lineStart:lineEnd], "\r")
		if lineStart != 0 && bytes.Equal(line, []byte("---")) {
			return nextLineStart
		}

		lineStart = nextLineStart
	}

	return 0
}

// generateMarkdownHeader creates auto-generated header for markdown files
func generateMarkdownHeader(filename string) string {
	return fmt.Sprintf(`<!--
AUTO-GENERATED FILE - DO NOT EDIT DIRECTLY

This file was installed by: brain install opencode
To update: brain install opencode
To check status: brain doctor
Source: https://github.com/huynle/brain-api
-->

`)
}

// Uninstall removes all installed brain components.
func (t *OpenCodeTarget) Uninstall() error {
	files, err := assets.ListPluginFilesRecursive("opencode")
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
func (t *OpenCodeTarget) Validate() error {
	files, err := assets.ListPluginFilesRecursive("opencode")
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

// generateHeader creates auto-generated header for plugin files
func generateHeader(filename string) string {
	return fmt.Sprintf(`/**
 * AUTO-GENERATED FILE - DO NOT EDIT DIRECTLY
 * 
 * This file was installed by: brain install opencode
 * To update: brain install opencode --force
 * To check status: brain plugin-status
 * Source: https://github.com/huynle/brain-api
 */

`)
}
