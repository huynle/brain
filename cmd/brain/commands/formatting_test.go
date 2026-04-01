package commands

import (
	"encoding/json"
	"flag"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestOutputConfig_RegisterFlags(t *testing.T) {
	o := &OutputConfig{}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	o.RegisterFlags(fs)

	expectedFlags := []string{"format", "0", "q", "quiet", "no-color"}
	for _, name := range expectedFlags {
		if fs.Lookup(name) == nil {
			t.Errorf("expected flag %q to be registered", name)
		}
	}
}

func TestOutputConfig_RegisterFlags_Defaults(t *testing.T) {
	o := &OutputConfig{}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	o.RegisterFlags(fs)

	if err := fs.Parse([]string{}); err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	if o.Format != "" {
		t.Errorf("expected empty default format, got %q", o.Format)
	}
	if o.Delimiter != "\n" {
		t.Errorf("expected newline default delimiter, got %q", o.Delimiter)
	}
	if o.Quiet {
		t.Error("expected quiet to be false by default")
	}
	if o.NoColor {
		t.Error("expected no-color to be false by default")
	}
}

func TestOutputConfig_RegisterFlags_NulDelimiter(t *testing.T) {
	o := &OutputConfig{}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	o.RegisterFlags(fs)

	if err := fs.Parse([]string{"-0"}); err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	if o.Delimiter != "\x00" {
		t.Errorf("expected NUL delimiter with -0, got %q", o.Delimiter)
	}
}

func TestOutputConfig_RegisterFlags_QuietMode(t *testing.T) {
	// Test -q short flag
	o := &OutputConfig{}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	o.RegisterFlags(fs)

	if err := fs.Parse([]string{"-q"}); err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	if !o.Quiet {
		t.Error("expected quiet=true with -q flag")
	}
}

func TestOutputConfig_IsCustomTemplate(t *testing.T) {
	tests := []struct {
		format string
		want   bool
	}{
		{"path", false},
		{"id", false},
		{"short", false},
		{"full", false},
		{"json", false},
		{"jsonl", false},
		{"{{.Title}}", true},
		{"{{.Title}} [{{.Status}}]", true},
		{"", false},
	}

	for _, tt := range tests {
		o := &OutputConfig{Format: tt.format}
		got := o.IsCustomTemplate()
		if got != tt.want {
			t.Errorf("IsCustomTemplate(%q) = %v, want %v", tt.format, got, tt.want)
		}
	}
}

func sampleEntry() types.BrainEntry {
	return types.BrainEntry{
		ID:       "abc12def",
		Path:     "projects/test/task/abc12def.md",
		Title:    "Test Task",
		Type:     "task",
		Status:   "pending",
		Priority: "high",
		Tags:     []string{"api", "auth"},
		Content:  "# Test Task\n\nThis is the body.",
	}
}

func TestOutputConfig_FormatEntry_Path(t *testing.T) {
	o := &OutputConfig{Format: "path"}
	entry := sampleEntry()

	got := o.FormatEntry(entry)
	if got != entry.Path {
		t.Errorf("FormatEntry(path) = %q, want %q", got, entry.Path)
	}
}

func TestOutputConfig_FormatEntry_ID(t *testing.T) {
	o := &OutputConfig{Format: "id"}
	entry := sampleEntry()

	got := o.FormatEntry(entry)
	if got != entry.ID {
		t.Errorf("FormatEntry(id) = %q, want %q", got, entry.ID)
	}
}

func TestOutputConfig_FormatEntry_Short(t *testing.T) {
	o := &OutputConfig{Format: "short"}
	entry := sampleEntry()

	got := o.FormatEntry(entry)

	// Short format should contain title, path, status, and priority
	if !strings.Contains(got, entry.Title) {
		t.Errorf("short format missing title, got: %s", got)
	}
	if !strings.Contains(got, entry.Path) {
		t.Errorf("short format missing path, got: %s", got)
	}
	if !strings.Contains(got, entry.Status) {
		t.Errorf("short format missing status, got: %s", got)
	}
	if !strings.Contains(got, entry.Priority) {
		t.Errorf("short format missing priority, got: %s", got)
	}
}

func TestOutputConfig_FormatEntry_JSON(t *testing.T) {
	o := &OutputConfig{Format: "json"}
	entry := sampleEntry()

	got := o.FormatEntry(entry)

	// Should be valid JSON
	var parsed types.BrainEntry
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("FormatEntry(json) produced invalid JSON: %v\nOutput: %s", err, got)
	}
	if parsed.ID != entry.ID {
		t.Errorf("parsed ID = %q, want %q", parsed.ID, entry.ID)
	}
}

func TestOutputConfig_FormatEntry_JSONL(t *testing.T) {
	o := &OutputConfig{Format: "jsonl"}
	entry := sampleEntry()

	got := o.FormatEntry(entry)

	// JSONL single entry = same as JSON single entry (one line)
	if strings.Contains(got, "\n") {
		t.Error("JSONL single entry should not contain newlines")
	}

	var parsed types.BrainEntry
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("FormatEntry(jsonl) produced invalid JSON: %v", err)
	}
}

func TestOutputConfig_FormatEntry_CustomTemplate(t *testing.T) {
	o := &OutputConfig{Format: "{{.Title}} [{{.Status}}]"}
	entry := sampleEntry()

	got := o.FormatEntry(entry)
	want := "Test Task [pending]"
	if got != want {
		t.Errorf("FormatEntry(custom) = %q, want %q", got, want)
	}
}

func TestOutputConfig_FormatEntry_CustomTemplate_Path(t *testing.T) {
	o := &OutputConfig{Format: "{{.Path}}"}
	entry := sampleEntry()

	got := o.FormatEntry(entry)
	if got != entry.Path {
		t.Errorf("FormatEntry(custom path) = %q, want %q", got, entry.Path)
	}
}

func TestOutputConfig_FormatEntries_JSON(t *testing.T) {
	o := &OutputConfig{Format: "json", Delimiter: "\n"}
	entries := []types.BrainEntry{sampleEntry(), sampleEntry()}
	entries[1].ID = "xyz98765"
	entries[1].Title = "Second Task"

	got := o.FormatEntries(entries)

	// JSON format for multiple entries should be a JSON array
	var parsed []types.BrainEntry
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("FormatEntries(json) produced invalid JSON array: %v\nOutput: %s", err, got)
	}
	if len(parsed) != 2 {
		t.Errorf("expected 2 entries, got %d", len(parsed))
	}
}

func TestOutputConfig_FormatEntries_JSONL(t *testing.T) {
	o := &OutputConfig{Format: "jsonl", Delimiter: "\n"}
	entries := []types.BrainEntry{sampleEntry(), sampleEntry()}
	entries[1].ID = "xyz98765"

	got := o.FormatEntries(entries)

	// JSONL: one JSON object per line
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines in JSONL, got %d", len(lines))
	}

	for i, line := range lines {
		var parsed types.BrainEntry
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			t.Errorf("line %d is invalid JSON: %v", i, err)
		}
	}
}

func TestOutputConfig_FormatEntries_Path(t *testing.T) {
	o := &OutputConfig{Format: "path", Delimiter: "\n"}
	entries := []types.BrainEntry{sampleEntry(), sampleEntry()}
	entries[1].Path = "projects/test/task/xyz98765.md"

	got := o.FormatEntries(entries)
	lines := strings.Split(strings.TrimSpace(got), "\n")

	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), got)
	}
	if lines[0] != entries[0].Path {
		t.Errorf("line 0 = %q, want %q", lines[0], entries[0].Path)
	}
	if lines[1] != entries[1].Path {
		t.Errorf("line 1 = %q, want %q", lines[1], entries[1].Path)
	}
}

func TestOutputConfig_FormatEntries_NulDelimiter(t *testing.T) {
	o := &OutputConfig{Format: "id", Delimiter: "\x00"}
	entries := []types.BrainEntry{sampleEntry(), sampleEntry()}
	entries[1].ID = "xyz98765"

	got := o.FormatEntries(entries)

	parts := strings.Split(got, "\x00")
	// Trailing NUL produces an empty final element
	if len(parts) < 2 {
		t.Fatalf("expected at least 2 NUL-separated parts, got %d: %q", len(parts), got)
	}
	if parts[0] != "abc12def" {
		t.Errorf("part 0 = %q, want %q", parts[0], "abc12def")
	}
	if parts[1] != "xyz98765" {
		t.Errorf("part 1 = %q, want %q", parts[1], "xyz98765")
	}
}

func TestOutputConfig_FormatEntries_Empty(t *testing.T) {
	o := &OutputConfig{Format: "json", Delimiter: "\n"}
	got := o.FormatEntries(nil)

	// Empty JSON array
	if got != "[]" {
		t.Errorf("FormatEntries(nil) = %q, want %q", got, "[]")
	}
}

func TestOutputConfig_FormatEntries_EmptyNonJSON(t *testing.T) {
	o := &OutputConfig{Format: "path", Delimiter: "\n"}
	got := o.FormatEntries(nil)

	if got != "" {
		t.Errorf("FormatEntries(nil, path) = %q, want empty string", got)
	}
}

func TestOutputConfig_DetectDefaultFormat_TTY(t *testing.T) {
	o := &OutputConfig{}
	// When isTTY=true, default should be "short"
	got := o.DetectDefaultFormat(true)
	if got != "short" {
		t.Errorf("DetectDefaultFormat(tty=true) = %q, want %q", got, "short")
	}
}

func TestOutputConfig_DetectDefaultFormat_Pipe(t *testing.T) {
	o := &OutputConfig{}
	// When isTTY=false, default should be "path"
	got := o.DetectDefaultFormat(false)
	if got != "path" {
		t.Errorf("DetectDefaultFormat(tty=false) = %q, want %q", got, "path")
	}
}

func TestOutputConfig_DetectDefaultFormat_ExplicitFormat(t *testing.T) {
	o := &OutputConfig{Format: "json"}
	// If format is already set, should return it unchanged
	got := o.DetectDefaultFormat(true)
	if got != "json" {
		t.Errorf("DetectDefaultFormat with explicit format = %q, want %q", got, "json")
	}
}

func TestOutputConfig_FormatEntry_Full(t *testing.T) {
	o := &OutputConfig{Format: "full"}
	entry := sampleEntry()

	got := o.FormatEntry(entry)

	// Full format should include frontmatter-style metadata and content body
	if !strings.Contains(got, "title:") || !strings.Contains(got, entry.Title) {
		t.Errorf("full format missing title metadata, got: %s", got)
	}
	if !strings.Contains(got, "type:") {
		t.Errorf("full format missing type metadata, got: %s", got)
	}
	if !strings.Contains(got, "This is the body") {
		t.Errorf("full format missing content body, got: %s", got)
	}
}

func TestOutputConfig_FormatEntry_InvalidTemplate(t *testing.T) {
	o := &OutputConfig{Format: "{{.InvalidField"}
	entry := sampleEntry()

	got := o.FormatEntry(entry)

	// Should return an error message, not panic
	if !strings.Contains(got, "error") && !strings.Contains(got, "Error") && !strings.Contains(got, "ERROR") {
		t.Errorf("invalid template should produce error, got: %q", got)
	}
}
