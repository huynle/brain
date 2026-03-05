// Package assets provides access to embedded template files and reference configuration.
// Templates are markdown files with YAML frontmatter used for creating different types
// of brain entries (task, plan, summary, etc.). The reference config.toml contains
// default configuration settings for note generation.
package assets

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

//go:embed templates/*.md
//go:embed config.toml
var embeddedFS embed.FS

// GetTemplate returns the content of a template by name
func GetTemplate(name string) ([]byte, error) {
	path := filepath.Join("templates", name)
	content, err := embeddedFS.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("template %q not found: %w", name, err)
	}
	return content, nil
}

// GetReferenceConfig returns the reference config.toml content
func GetReferenceConfig() ([]byte, error) {
	content, err := embeddedFS.ReadFile("config.toml")
	if err != nil {
		return nil, fmt.Errorf("config.toml not found: %w", err)
	}
	return content, nil
}

// ListTemplates returns all available template names
func ListTemplates() []string {
	entries, err := fs.ReadDir(embeddedFS, "templates")
	if err != nil {
		return nil
	}

	templates := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			templates = append(templates, entry.Name())
		}
	}
	return templates
}

// GetTemplatesFS returns the embedded filesystem for direct access
func GetTemplatesFS() fs.FS {
	return embeddedFS
}
