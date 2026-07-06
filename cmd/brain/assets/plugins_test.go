package assets

import (
	"strings"
	"testing"
)

// Test GetPluginFile can read a top-level opencode plugin asset.
// Brain tools were previously shipped as a brain.ts plugin; they are now
// exposed through the brain MCP stdio server, so the only top-level asset
// in the opencode plugin tree is README.md.
func TestGetPluginFile_OpenCodeReadme(t *testing.T) {
	content, err := GetPluginFile("opencode", "README.md")
	if err != nil {
		t.Fatalf("GetPluginFile(opencode, README.md) failed: %v", err)
	}
	if len(content) == 0 {
		t.Error("GetPluginFile(opencode, README.md) returned empty content")
	}

	// Check for expected content markers describing the new MCP-based flow.
	text := string(content)
	if !strings.Contains(text, "OpenCode Brain Assets") {
		t.Error("README.md missing expected heading")
	}
	if !strings.Contains(text, "MCP") {
		t.Error("README.md should reference the MCP-based replacement for the legacy plugin")
	}
}

// Test GetPluginFile returns error for missing file
func TestGetPluginFile_NotFound(t *testing.T) {
	_, err := GetPluginFile("opencode", "nonexistent.ts")
	if err == nil {
		t.Error("GetPluginFile(opencode, nonexistent.ts) should return error, got nil")
	}
}

// Test GetPluginFile returns error for missing target
func TestGetPluginFile_InvalidTarget(t *testing.T) {
	_, err := GetPluginFile("invalid-target", "README.md")
	if err == nil {
		t.Error("GetPluginFile(invalid-target, README.md) should return error, got nil")
	}
}

// Test ListPluginFiles returns all top-level opencode files.
// After the brain.ts → MCP migration, only README.md ships at the top level;
// agents, skills, and commands live in subdirectories and are covered by
// ListPluginFilesRecursive (exercised by installer tests in internal/plugins).
func TestListPluginFiles_OpenCode(t *testing.T) {
	files, err := ListPluginFiles("opencode")
	if err != nil {
		t.Fatalf("ListPluginFiles(opencode) failed: %v", err)
	}

	expectedFiles := map[string]bool{
		"README.md": true,
	}

	if len(files) != len(expectedFiles) {
		t.Errorf("ListPluginFiles(opencode) returned %d files, expected %d (%v)", len(files), len(expectedFiles), files)
	}

	for _, file := range files {
		if !expectedFiles[file] {
			t.Errorf("ListPluginFiles(opencode) returned unexpected file: %q", file)
		}
		delete(expectedFiles, file)
	}

	for file := range expectedFiles {
		t.Errorf("ListPluginFiles(opencode) missing expected file: %q", file)
	}
}

// Test ListPluginFiles returns error for invalid target
func TestListPluginFiles_InvalidTarget(t *testing.T) {
	_, err := ListPluginFiles("invalid-target")
	if err == nil {
		t.Error("ListPluginFiles(invalid-target) should return error, got nil")
	}
}

// Test GetPluginsFS returns a valid filesystem with readable opencode assets.
func TestGetPluginsFS(t *testing.T) {
	fs := GetPluginsFS()
	if fs == nil {
		t.Fatal("GetPluginsFS() returned nil")
	}

	// Try reading a plugin file directly from FS
	file, err := fs.Open("plugins/opencode/README.md")
	if err != nil {
		t.Fatalf("Failed to open plugins/opencode/README.md from FS: %v", err)
	}
	defer file.Close()

	// Verify we can read content
	buf := make([]byte, 100)
	n, err := file.Read(buf)
	if err != nil && err.Error() != "EOF" {
		t.Fatalf("Failed to read from file: %v", err)
	}
	if n == 0 {
		t.Error("Read 0 bytes from plugins/opencode/README.md")
	}
}
