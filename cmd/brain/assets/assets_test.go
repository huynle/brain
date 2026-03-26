package assets

import (
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// Test that all 13 expected templates are accessible
func TestGetTemplate_AllTemplates(t *testing.T) {
	expectedTemplates := []string{
		"default.md",
		"task.md",
		"plan.md",
		"summary.md",
		"report.md",
		"walkthrough.md",
		"pattern.md",
		"learning.md",
		"idea.md",
		"scratch.md",
		"decision.md",
		"exploration.md",
		"execution.md",
	}

	for _, name := range expectedTemplates {
		t.Run(name, func(t *testing.T) {
			content, err := GetTemplate(name)
			if err != nil {
				t.Fatalf("GetTemplate(%q) failed: %v", name, err)
			}
			if len(content) == 0 {
				t.Errorf("GetTemplate(%q) returned empty content", name)
			}
		})
	}
}

// Test that templates have valid frontmatter structure
func TestGetTemplate_ValidFrontmatter(t *testing.T) {
	templates := []string{"default.md", "task.md", "plan.md"}

	for _, name := range templates {
		t.Run(name, func(t *testing.T) {
			content, err := GetTemplate(name)
			if err != nil {
				t.Fatalf("GetTemplate(%q) failed: %v", name, err)
			}

			text := string(content)
			// Check for YAML frontmatter delimiters
			if !strings.HasPrefix(text, "---\n") {
				t.Errorf("Template %q missing opening frontmatter delimiter", name)
			}
			// Closing delimiter may be preceded by template syntax like {{/if}}
			// so we check for either "\n---\n" or "}}---\n" (handlebars syntax)
			hasStandardClosing := strings.Contains(text, "\n---\n")
			hasHandlebarsClosing := strings.Contains(text, "}}---\n")
			if !hasStandardClosing && !hasHandlebarsClosing {
				t.Errorf("Template %q missing closing frontmatter delimiter", name)
			}
		})
	}
}

// Test that config.toml is accessible and valid TOML
func TestGetReferenceConfig(t *testing.T) {
	content, err := GetReferenceConfig()
	if err != nil {
		t.Fatalf("GetReferenceConfig() failed: %v", err)
	}

	if len(content) == 0 {
		t.Fatal("GetReferenceConfig() returned empty content")
	}

	// Verify it's valid TOML
	var config map[string]interface{}
	if err := toml.Unmarshal(content, &config); err != nil {
		t.Fatalf("GetReferenceConfig() returned invalid TOML: %v", err)
	}

	// Check expected sections exist
	if _, ok := config["note"]; !ok {
		t.Error("Config missing [note] section")
	}
	if _, ok := config["format"]; !ok {
		t.Error("Config missing [format] section")
	}
}

// Test that non-existent template returns error
func TestGetTemplate_NotFound(t *testing.T) {
	_, err := GetTemplate("nonexistent.md")
	if err == nil {
		t.Error("GetTemplate(nonexistent.md) should return error, got nil")
	}
}

// Test ListTemplates returns exactly 13 templates
func TestListTemplates(t *testing.T) {
	templates := ListTemplates()

	if len(templates) != 13 {
		t.Errorf("ListTemplates() returned %d templates, expected 13", len(templates))
	}

	// Check that list contains expected names
	expectedNames := map[string]bool{
		"default.md":     true,
		"task.md":        true,
		"plan.md":        true,
		"summary.md":     true,
		"report.md":      true,
		"walkthrough.md": true,
		"pattern.md":     true,
		"learning.md":    true,
		"idea.md":        true,
		"scratch.md":     true,
		"decision.md":    true,
		"exploration.md": true,
		"execution.md":   true,
	}

	for _, name := range templates {
		if !expectedNames[name] {
			t.Errorf("ListTemplates() returned unexpected template: %q", name)
		}
		delete(expectedNames, name)
	}

	// Check for missing templates
	for name := range expectedNames {
		t.Errorf("ListTemplates() missing expected template: %q", name)
	}
}

// Test GetTemplatesFS returns a valid filesystem
func TestGetTemplatesFS(t *testing.T) {
	fs := GetTemplatesFS()
	if fs == nil {
		t.Fatal("GetTemplatesFS() returned nil")
	}

	// Try reading a template directly from FS
	file, err := fs.Open("templates/default.md")
	if err != nil {
		t.Fatalf("Failed to open templates/default.md from FS: %v", err)
	}
	defer file.Close()

	// Verify we can read content
	buf := make([]byte, 100)
	n, err := file.Read(buf)
	if err != nil && err.Error() != "EOF" {
		t.Fatalf("Failed to read from file: %v", err)
	}
	if n == 0 {
		t.Error("Read 0 bytes from templates/default.md")
	}
}
