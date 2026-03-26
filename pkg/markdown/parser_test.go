package markdown

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ===========================================================================
// ParsedFile type
// ===========================================================================

func TestParsedFile_HasExpectedFields(t *testing.T) {
	// Verify the ParsedFile struct has all expected fields by constructing one
	pf := ParsedFile{
		Path:       "projects/test/task/abc12def.md",
		ShortID:    "abc12def",
		Title:      "Test Entry",
		Lead:       "First paragraph",
		Body:       "# Test\n\nFirst paragraph",
		RawContent: "---\ntitle: Test Entry\n---\n# Test\n\nFirst paragraph",
		WordCount:  3,
		Checksum:   "abc123",
		Type:       strPtr("task"),
		Status:     strPtr("active"),
		Priority:   strPtr("high"),
		ProjectID:  strPtr("test"),
		FeatureID:  strPtr("feat-1"),
		Tags:       []string{"task"},
		Created:    "2024-01-01T00:00:00Z",
		Modified:   "2024-01-02T00:00:00Z",
		Links:      []ExtractedLink{},
	}
	if pf.Path != "projects/test/task/abc12def.md" {
		t.Errorf("Path = %q", pf.Path)
	}
	if pf.ShortID != "abc12def" {
		t.Errorf("ShortID = %q", pf.ShortID)
	}
}

// ===========================================================================
// ParseFile
// ===========================================================================

func TestParseFile_BasicFile(t *testing.T) {
	dir := t.TempDir()
	relPath := "projects/test/task/abc12def.md"
	fullPath := filepath.Join(dir, relPath)

	// Create directory structure
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}

	content := "---\ntitle: My Task\ntype: task\nstatus: active\ntags:\n  - task\n  - important\npriority: high\nprojectId: test\nfeature_id: feat-1\ncreated: 2024-01-01T00:00:00Z\n---\n# My Task\n\nThis is the body with a [link](abc12def) to another note."
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	pf, err := ParseFile(relPath, dir)
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}

	if pf.Path != relPath {
		t.Errorf("Path = %q, want %q", pf.Path, relPath)
	}
	if pf.ShortID != "abc12def" {
		t.Errorf("ShortID = %q, want %q", pf.ShortID, "abc12def")
	}
	if pf.Title != "My Task" {
		t.Errorf("Title = %q, want %q", pf.Title, "My Task")
	}
	if pf.RawContent != content {
		t.Errorf("RawContent mismatch")
	}
	if pf.Checksum == "" {
		t.Error("Checksum should not be empty")
	}
	if pf.WordCount == 0 {
		t.Error("WordCount should not be 0")
	}
	if pf.Lead == "" {
		t.Error("Lead should not be empty")
	}

	// Check frontmatter fields
	if pf.Type == nil || *pf.Type != "task" {
		t.Errorf("Type = %v, want 'task'", pf.Type)
	}
	if pf.Status == nil || *pf.Status != "active" {
		t.Errorf("Status = %v, want 'active'", pf.Status)
	}
	if pf.Priority == nil || *pf.Priority != "high" {
		t.Errorf("Priority = %v, want 'high'", pf.Priority)
	}
	if pf.ProjectID == nil || *pf.ProjectID != "test" {
		t.Errorf("ProjectID = %v, want 'test'", pf.ProjectID)
	}
	if pf.FeatureID == nil || *pf.FeatureID != "feat-1" {
		t.Errorf("FeatureID = %v, want 'feat-1'", pf.FeatureID)
	}
	if len(pf.Tags) != 2 || pf.Tags[0] != "task" || pf.Tags[1] != "important" {
		t.Errorf("Tags = %v, want [task important]", pf.Tags)
	}
	if pf.Created != "2024-01-01T00:00:00Z" {
		t.Errorf("Created = %q, want %q", pf.Created, "2024-01-01T00:00:00Z")
	}
	if pf.Modified == "" {
		t.Error("Modified should not be empty")
	}

	// Check links
	if len(pf.Links) != 1 {
		t.Fatalf("Links count = %d, want 1", len(pf.Links))
	}
	if pf.Links[0].Href != "abc12def" {
		t.Errorf("Links[0].Href = %q, want %q", pf.Links[0].Href, "abc12def")
	}
}

func TestParseFile_NoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	relPath := "notes/plain.md"
	fullPath := filepath.Join(dir, relPath)

	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}

	content := "Just plain markdown without frontmatter.\n\nSecond paragraph."
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	pf, err := ParseFile(relPath, dir)
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}

	if pf.ShortID != "plain" {
		t.Errorf("ShortID = %q, want %q", pf.ShortID, "plain")
	}
	// Title should fall back to shortID when no frontmatter title
	if pf.Title != "plain" {
		t.Errorf("Title = %q, want %q", pf.Title, "plain")
	}
	if pf.Body != content {
		t.Errorf("Body = %q, want %q", pf.Body, content)
	}
}

func TestParseFile_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := ParseFile("nonexistent.md", dir)
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestParseFile_EmptyFrontmatter(t *testing.T) {
	dir := t.TempDir()
	relPath := "test.md"
	fullPath := filepath.Join(dir, relPath)

	content := "---\n---\nBody content here."
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	pf, err := ParseFile(relPath, dir)
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}

	if pf.Title != "test" {
		t.Errorf("Title = %q, want %q (fallback to shortID)", pf.Title, "test")
	}
	if !strings.Contains(pf.Body, "Body content here") {
		t.Errorf("Body = %q, should contain 'Body content here'", pf.Body)
	}
}

func TestParseFile_MetadataPointer(t *testing.T) {
	dir := t.TempDir()
	relPath := "test.md"
	fullPath := filepath.Join(dir, relPath)

	content := "---\ntitle: Test\ntype: plan\nstatus: draft\n---\nBody."
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	pf, err := ParseFile(relPath, dir)
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}

	if pf.Metadata == nil {
		t.Fatal("Metadata should not be nil")
	}
	if pf.Metadata.Title != "Test" {
		t.Errorf("Metadata.Title = %q, want %q", pf.Metadata.Title, "Test")
	}
}

// helper
func strPtr(s string) *string { return &s }
