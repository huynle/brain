package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// UpdateCommand Unit Tests
// =============================================================================

func TestUpdateCommand_Type(t *testing.T) {
	cmd := &UpdateCommand{}
	if cmd.Type() != "update" {
		t.Errorf("Type() = %q, want %q", cmd.Type(), "update")
	}
}

func TestUpdateCommand_Execute_NoIDOrPath(t *testing.T) {
	cmd := &UpdateCommand{
		IDOrPath: "",
		Config:   testConfig(),
		Flags:    &EntryUpdateFlags{Status: "completed"},
	}
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing ID")
	}
	if !strings.Contains(err.Error(), "ID or path is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestUpdateCommand_Execute_NoFlags(t *testing.T) {
	cmd := &UpdateCommand{
		IDOrPath: "abc12def",
		Config:   testConfig(),
		Flags:    &EntryUpdateFlags{},
	}
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for no flags")
	}
	if !strings.Contains(err.Error(), "at least one update flag") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestUpdateCommand_Execute_ContentAndAppendMutuallyExclusive(t *testing.T) {
	cmd := &UpdateCommand{
		IDOrPath: "abc12def",
		Config:   testConfig(),
		Flags: &EntryUpdateFlags{
			Content: "new content",
			Append:  "appended text",
		},
	}
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for mutually exclusive flags")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestUpdateCommand_Execute_StatusUpdate(t *testing.T) {
	var receivedPath string
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" && strings.Contains(r.URL.Path, "/api/v1/entries/") {
			receivedPath = r.URL.Path
			json.NewDecoder(r.Body).Decode(&receivedBody)
			resp := types.BrainEntry{
				ID:     "abc12def",
				Path:   "projects/test/task/abc12def.md",
				Title:  "Test Task",
				Status: "completed",
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestUpdateCommand(server.URL, "abc12def", &EntryUpdateFlags{Status: "completed"}, &out)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	// Verify API was called with correct path
	if !strings.Contains(receivedPath, "abc12def") {
		t.Errorf("API path = %q, expected to contain 'abc12def'", receivedPath)
	}

	// Verify status was sent
	if receivedBody["status"] != "completed" {
		t.Errorf("status = %v, want 'completed'", receivedBody["status"])
	}

	// Verify output
	output := out.String()
	if !strings.Contains(output, "Updated: abc12def") {
		t.Errorf("output = %q, expected 'Updated: abc12def'", output)
	}
	if !strings.Contains(output, "status: completed") {
		t.Errorf("output = %q, expected 'status: completed'", output)
	}
}

func TestUpdateCommand_Execute_MultipleFlags(t *testing.T) {
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			json.NewDecoder(r.Body).Decode(&receivedBody)
			resp := types.BrainEntry{ID: "abc12def", Path: "projects/test/task/abc12def.md"}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestUpdateCommand(server.URL, "abc12def", &EntryUpdateFlags{
		Status:   "completed",
		Title:    "New Title",
		Note:     "All tests passing",
		Priority: "high",
	}, &out)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if receivedBody["status"] != "completed" {
		t.Errorf("status = %v, want 'completed'", receivedBody["status"])
	}
	if receivedBody["title"] != "New Title" {
		t.Errorf("title = %v, want 'New Title'", receivedBody["title"])
	}
	if receivedBody["note"] != "All tests passing" {
		t.Errorf("note = %v, want 'All tests passing'", receivedBody["note"])
	}
	if receivedBody["priority"] != "high" {
		t.Errorf("priority = %v, want 'high'", receivedBody["priority"])
	}
}

func TestUpdateCommand_Execute_TagsCommaSeparated(t *testing.T) {
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			json.NewDecoder(r.Body).Decode(&receivedBody)
			resp := types.BrainEntry{ID: "abc12def", Path: "projects/test/task/abc12def.md"}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestUpdateCommand(server.URL, "abc12def", &EntryUpdateFlags{
		Tags: "api, auth, v2",
	}, &out)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	tags, ok := receivedBody["tags"].([]interface{})
	if !ok {
		t.Fatalf("tags is not []interface{}: %T", receivedBody["tags"])
	}
	if len(tags) != 3 {
		t.Fatalf("len(tags) = %d, want 3", len(tags))
	}
	if tags[0] != "api" || tags[1] != "auth" || tags[2] != "v2" {
		t.Errorf("tags = %v, want [api auth v2]", tags)
	}
}

func TestUpdateCommand_Execute_DependsOnCommaSeparated(t *testing.T) {
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			json.NewDecoder(r.Body).Decode(&receivedBody)
			resp := types.BrainEntry{ID: "abc12def", Path: "projects/test/task/abc12def.md"}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestUpdateCommand(server.URL, "abc12def", &EntryUpdateFlags{
		DependsOn: "task1,task2",
	}, &out)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	deps, ok := receivedBody["depends_on"].([]interface{})
	if !ok {
		t.Fatalf("depends_on is not []interface{}: %T", receivedBody["depends_on"])
	}
	if len(deps) != 2 {
		t.Fatalf("len(deps) = %d, want 2", len(deps))
	}
	if deps[0] != "task1" || deps[1] != "task2" {
		t.Errorf("depends_on = %v, want [task1 task2]", deps)
	}
}

func TestUpdateCommand_Execute_ContentInline(t *testing.T) {
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			json.NewDecoder(r.Body).Decode(&receivedBody)
			resp := types.BrainEntry{ID: "abc12def", Path: "projects/test/task/abc12def.md"}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestUpdateCommand(server.URL, "abc12def", &EntryUpdateFlags{
		Content: "new replacement content",
	}, &out)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if receivedBody["content"] != "new replacement content" {
		t.Errorf("content = %v, want 'new replacement content'", receivedBody["content"])
	}
}

func TestUpdateCommand_Execute_ContentFromStdin(t *testing.T) {
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			json.NewDecoder(r.Body).Decode(&receivedBody)
			resp := types.BrainEntry{ID: "abc12def", Path: "projects/test/task/abc12def.md"}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestUpdateCommand(server.URL, "abc12def", &EntryUpdateFlags{
		Content: "-",
	}, &out)
	// Inject stdin
	cmd.Stdin = strings.NewReader("piped content from stdin")

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if receivedBody["content"] != "piped content from stdin" {
		t.Errorf("content = %v, want 'piped content from stdin'", receivedBody["content"])
	}
}

func TestUpdateCommand_Execute_ContentFromFile(t *testing.T) {
	var receivedBody map[string]interface{}

	// Create a temp file
	tmpFile, err := os.CreateTemp("", "brain-update-test-*.md")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString("content from file")
	tmpFile.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			json.NewDecoder(r.Body).Decode(&receivedBody)
			resp := types.BrainEntry{ID: "abc12def", Path: "projects/test/task/abc12def.md"}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestUpdateCommand(server.URL, "abc12def", &EntryUpdateFlags{
		Content: "@" + tmpFile.Name(),
	}, &out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if receivedBody["content"] != "content from file" {
		t.Errorf("content = %v, want 'content from file'", receivedBody["content"])
	}
}

func TestUpdateCommand_Execute_ContentFromFile_NotFound(t *testing.T) {
	cmd := &UpdateCommand{
		IDOrPath: "abc12def",
		Config:   testConfig(),
		Flags: &EntryUpdateFlags{
			Content: "@/nonexistent/path/file.md",
		},
	}
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if !strings.Contains(err.Error(), "read file") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestUpdateCommand_Execute_AppendText(t *testing.T) {
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			json.NewDecoder(r.Body).Decode(&receivedBody)
			resp := types.BrainEntry{ID: "abc12def", Path: "projects/test/task/abc12def.md"}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestUpdateCommand(server.URL, "abc12def", &EntryUpdateFlags{
		Append: "## Progress\n- Done with auth module",
	}, &out)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if receivedBody["append"] != "## Progress\n- Done with auth module" {
		t.Errorf("append = %v, want expected text", receivedBody["append"])
	}
}

func TestUpdateCommand_Execute_FeatureID(t *testing.T) {
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			json.NewDecoder(r.Body).Decode(&receivedBody)
			resp := types.BrainEntry{ID: "abc12def", Path: "projects/test/task/abc12def.md"}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestUpdateCommand(server.URL, "abc12def", &EntryUpdateFlags{
		FeatureID: "my-feature",
	}, &out)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if receivedBody["feature_id"] != "my-feature" {
		t.Errorf("feature_id = %v, want 'my-feature'", receivedBody["feature_id"])
	}
}

func TestUpdateCommand_Execute_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "entry not found"}`))
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestUpdateCommand(server.URL, "nonexistent", &EntryUpdateFlags{
		Status: "completed",
	}, &out)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for API error")
	}
	if !strings.Contains(err.Error(), "update entry") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestUpdateCommand_Execute_FullPath(t *testing.T) {
	var receivedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			receivedPath = r.URL.Path
			resp := types.BrainEntry{ID: "abc12def", Path: "projects/test/task/abc12def.md"}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestUpdateCommand(server.URL, "projects/test/task/abc12def.md", &EntryUpdateFlags{
		Status: "completed",
	}, &out)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	// Verify path was passed through
	if !strings.Contains(receivedPath, "projects/test/task/abc12def.md") {
		t.Errorf("API path = %q, expected to contain full path", receivedPath)
	}
}

// =============================================================================
// splitCSV Unit Tests
// =============================================================================

func TestSplitCSV(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"a,b,c", []string{"a", "b", "c"}},
		{"a, b , c", []string{"a", "b", "c"}},
		{"single", []string{"single"}},
		{",,,", nil},
		{"a,,b", []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := splitCSV(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("splitCSV(%q) = %v (len %d), want %v (len %d)",
					tt.input, result, len(result), tt.expected, len(tt.expected))
				return
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("splitCSV(%q)[%d] = %q, want %q",
						tt.input, i, result[i], tt.expected[i])
				}
			}
		})
	}
}

// =============================================================================
// hasAnyFlag Unit Tests
// =============================================================================

func TestUpdateCommand_HasAnyFlag(t *testing.T) {
	tests := []struct {
		name     string
		flags    *EntryUpdateFlags
		expected bool
	}{
		{"empty", &EntryUpdateFlags{}, false},
		{"status only", &EntryUpdateFlags{Status: "completed"}, true},
		{"title only", &EntryUpdateFlags{Title: "New"}, true},
		{"content only", &EntryUpdateFlags{Content: "text"}, true},
		{"append only", &EntryUpdateFlags{Append: "text"}, true},
		{"note only", &EntryUpdateFlags{Note: "note"}, true},
		{"tags only", &EntryUpdateFlags{Tags: "a,b"}, true},
		{"priority only", &EntryUpdateFlags{Priority: "high"}, true},
		{"depends-on only", &EntryUpdateFlags{DependsOn: "t1"}, true},
		{"feature-id only", &EntryUpdateFlags{FeatureID: "f1"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &UpdateCommand{Flags: tt.flags}
			if got := cmd.hasAnyFlag(); got != tt.expected {
				t.Errorf("hasAnyFlag() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// =============================================================================
// resolveContentSource Unit Tests
// =============================================================================

func TestUpdateCommand_ResolveContentSource_Literal(t *testing.T) {
	cmd := &UpdateCommand{}
	content, err := cmd.resolveContentSource("hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "hello world" {
		t.Errorf("content = %q, want 'hello world'", content)
	}
}

func TestUpdateCommand_ResolveContentSource_Stdin(t *testing.T) {
	cmd := &UpdateCommand{
		Stdin: strings.NewReader("stdin content"),
	}
	content, err := cmd.resolveContentSource("-")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "stdin content" {
		t.Errorf("content = %q, want 'stdin content'", content)
	}
}

func TestUpdateCommand_ResolveContentSource_File(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.md")
	os.WriteFile(tmpFile, []byte("file content"), 0644)

	cmd := &UpdateCommand{}
	content, err := cmd.resolveContentSource("@" + tmpFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "file content" {
		t.Errorf("content = %q, want 'file content'", content)
	}
}

func TestUpdateCommand_ResolveContentSource_FileNotFound(t *testing.T) {
	cmd := &UpdateCommand{}
	_, err := cmd.resolveContentSource("@/nonexistent/file.md")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

// =============================================================================
// Test Helpers
// =============================================================================

func testConfig() *UnifiedConfig {
	return &UnifiedConfig{}
}

func newTestUpdateCommand(apiURL, idOrPath string, flags *EntryUpdateFlags, out *bytes.Buffer) *UpdateCommand {
	cfg := &UnifiedConfig{}
	cfg.Runner.BrainAPIURL = apiURL
	cfg.Runner.APITimeout = 5000

	return &UpdateCommand{
		IDOrPath:  idOrPath,
		Config:    cfg,
		Flags:     flags,
		Out:       out,
		apiClient: runner.NewAPIClient(cfg.Runner),
	}
}

// =============================================================================
// Flag Parsing Tests (in main package, tested via integration)
// Note: ParseEntryUpdateFlags is in the main package, not commands.
// These are pure unit tests within the commands package.
// =============================================================================

// Verify the Update output format matches expected confirmation style
func TestUpdateCommand_OutputFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := types.BrainEntry{
			ID:     "abc12def",
			Path:   "projects/test/task/abc12def.md",
			Status: "completed",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestUpdateCommand(server.URL, "abc12def", &EntryUpdateFlags{
		Status: "completed",
		Note:   "All tests passing",
	}, &out)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	output := out.String()
	// Expected format:
	// Updated: abc12def
	//   status: completed
	//   note: All tests passing
	if !strings.Contains(output, "Updated: abc12def") {
		t.Errorf("missing 'Updated: abc12def' in output: %s", output)
	}
	if !strings.Contains(output, "status: completed") {
		t.Errorf("missing 'status: completed' in output: %s", output)
	}
	if !strings.Contains(output, "note: All tests passing") {
		t.Errorf("missing 'note: All tests passing' in output: %s", output)
	}
}

// Verify long content is truncated in output
func TestUpdateCommand_OutputFormat_LongContentTruncated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := types.BrainEntry{ID: "abc12def", Path: "projects/test/task/abc12def.md"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	longContent := strings.Repeat("x", 100)
	var out bytes.Buffer
	cmd := newTestUpdateCommand(server.URL, "abc12def", &EntryUpdateFlags{
		Content: longContent,
	}, &out)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "...") {
		t.Errorf("expected truncated content with '...' in output: %s", output)
	}
}

// Suppress "unused" lint for context import
var _ = context.Background
var _ = fmt.Sprintf
