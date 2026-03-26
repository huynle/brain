package assets

import (
	"strings"
	"testing"
)

// Test GetPluginFile can read opencode/brain.ts
func TestGetPluginFile_OpenCodeBrain(t *testing.T) {
	content, err := GetPluginFile("opencode", "brain.ts")
	if err != nil {
		t.Fatalf("GetPluginFile(opencode, brain.ts) failed: %v", err)
	}
	if len(content) == 0 {
		t.Error("GetPluginFile(opencode, brain.ts) returned empty content")
	}

	// Check for expected content markers
	text := string(content)
	if !strings.Contains(text, "BrainPlugin") {
		t.Error("brain.ts missing expected BrainPlugin export")
	}
	if !strings.Contains(text, "BRAIN_API_URL") {
		t.Error("brain.ts missing expected BRAIN_API_URL constant")
	}
}

// Test GetPluginFile can read opencode/brain-planning.ts
func TestGetPluginFile_OpenCodeBrainPlanning(t *testing.T) {
	content, err := GetPluginFile("opencode", "brain-planning.ts")
	if err != nil {
		t.Fatalf("GetPluginFile(opencode, brain-planning.ts) failed: %v", err)
	}
	if len(content) == 0 {
		t.Error("GetPluginFile(opencode, brain-planning.ts) returned empty content")
	}

	// Check for expected content markers
	text := string(content)
	if !strings.Contains(text, "BrainPlanningPlugin") {
		t.Error("brain-planning.ts missing expected BrainPlanningPlugin export")
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
	_, err := GetPluginFile("invalid-target", "brain.ts")
	if err == nil {
		t.Error("GetPluginFile(invalid-target, brain.ts) should return error, got nil")
	}
}

// Test ListPluginFiles returns all opencode files
func TestListPluginFiles_OpenCode(t *testing.T) {
	files, err := ListPluginFiles("opencode")
	if err != nil {
		t.Fatalf("ListPluginFiles(opencode) failed: %v", err)
	}

	// Expected files
	expectedFiles := map[string]bool{
		"brain.ts":          true,
		"brain-planning.ts": true,
		"README.md":         true,
	}

	if len(files) != len(expectedFiles) {
		t.Errorf("ListPluginFiles(opencode) returned %d files, expected %d", len(files), len(expectedFiles))
	}

	for _, file := range files {
		if !expectedFiles[file] {
			t.Errorf("ListPluginFiles(opencode) returned unexpected file: %q", file)
		}
		delete(expectedFiles, file)
	}

	// Check for missing files
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

// Test GetPluginsFS returns a valid filesystem
func TestGetPluginsFS(t *testing.T) {
	fs := GetPluginsFS()
	if fs == nil {
		t.Fatal("GetPluginsFS() returned nil")
	}

	// Try reading a plugin file directly from FS
	file, err := fs.Open("plugins/opencode/brain.ts")
	if err != nil {
		t.Fatalf("Failed to open plugins/opencode/brain.ts from FS: %v", err)
	}
	defer file.Close()

	// Verify we can read content
	buf := make([]byte, 100)
	n, err := file.Read(buf)
	if err != nil && err.Error() != "EOF" {
		t.Fatalf("Failed to read from file: %v", err)
	}
	if n == 0 {
		t.Error("Read 0 bytes from plugins/opencode/brain.ts")
	}
}
