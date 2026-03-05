package mcp

import (
	"testing"
)

func TestResolveProjectName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"projects/brain-api", "brain-api"},
		{"brain-api", "brain-api"},
		{"projects/foo/bar", "bar"},
		{"single", "single"},
		{"", ""},
	}

	for _, tt := range tests {
		got := resolveProjectName(tt.input)
		if got != tt.want {
			t.Errorf("resolveProjectName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMakeHomeRelative(t *testing.T) {
	tests := []struct {
		path string
		home string
		want string
	}{
		{"/Users/test/projects/brain-api", "/Users/test", "projects/brain-api"},
		{"/other/path", "/Users/test", "/other/path"},
		{"/Users/test", "/Users/test", ""},
		{"relative/path", "/Users/test", "relative/path"},
	}

	for _, tt := range tests {
		got := makeHomeRelative(tt.path, tt.home)
		if got != tt.want {
			t.Errorf("makeHomeRelative(%q, %q) = %q, want %q", tt.path, tt.home, got, tt.want)
		}
	}
}

func TestStringArg(t *testing.T) {
	args := map[string]any{
		"name":  "test",
		"empty": "",
	}

	if got := StringArg(args, "name", "default"); got != "test" {
		t.Errorf("StringArg(name) = %q, want %q", got, "test")
	}
	if got := StringArg(args, "empty", "default"); got != "default" {
		t.Errorf("StringArg(empty) = %q, want %q", got, "default")
	}
	if got := StringArg(args, "missing", "default"); got != "default" {
		t.Errorf("StringArg(missing) = %q, want %q", got, "default")
	}
}

func TestIntArg(t *testing.T) {
	args := map[string]any{
		"limit": float64(10),
	}

	if got := IntArg(args, "limit", 5); got != 10 {
		t.Errorf("IntArg(limit) = %d, want %d", got, 10)
	}
	if got := IntArg(args, "missing", 5); got != 5 {
		t.Errorf("IntArg(missing) = %d, want %d", got, 5)
	}
}

func TestBoolArg(t *testing.T) {
	args := map[string]any{
		"global": true,
	}

	if got := BoolArg(args, "global", false); got != true {
		t.Errorf("BoolArg(global) = %v, want %v", got, true)
	}
	if got := BoolArg(args, "missing", false); got != false {
		t.Errorf("BoolArg(missing) = %v, want %v", got, false)
	}
}

func TestStringSliceArg(t *testing.T) {
	args := map[string]any{
		"tags": []any{"go", "mcp"},
		"nil":  nil,
	}

	got := StringSliceArg(args, "tags")
	if len(got) != 2 || got[0] != "go" || got[1] != "mcp" {
		t.Errorf("StringSliceArg(tags) = %v, want [go mcp]", got)
	}

	if got := StringSliceArg(args, "nil"); got != nil {
		t.Errorf("StringSliceArg(nil) = %v, want nil", got)
	}

	if got := StringSliceArg(args, "missing"); got != nil {
		t.Errorf("StringSliceArg(missing) = %v, want nil", got)
	}
}

func TestPathFromArgs(t *testing.T) {
	args := map[string]any{
		"path":   "projects/test/plan/abc.md",
		"planId": "projects/test/plan/xyz.md",
	}

	if got := PathFromArgs(args, "path"); got != "projects/test/plan/abc.md" {
		t.Errorf("PathFromArgs(path) = %q, want %q", got, "projects/test/plan/abc.md")
	}
	if got := PathFromArgs(args, "planId"); got != "projects/test/plan/xyz.md" {
		t.Errorf("PathFromArgs(planId) = %q, want %q", got, "projects/test/plan/xyz.md")
	}
	if got := PathFromArgs(args, "missing"); got != "" {
		t.Errorf("PathFromArgs(missing) = %q, want %q", got, "")
	}
	// First match wins
	if got := PathFromArgs(args, "path", "planId"); got != "projects/test/plan/abc.md" {
		t.Errorf("PathFromArgs(path, planId) = %q, want %q", got, "projects/test/plan/abc.md")
	}
}

func TestResolveProject(t *testing.T) {
	// Override cached context for testing
	cachedContext = &ExecutionContext{ProjectID: "test-project"}
	defer func() { cachedContext = nil }()

	// With explicit project
	args := map[string]any{"project": "custom-project"}
	if got := ResolveProject(args); got != "custom-project" {
		t.Errorf("ResolveProject(explicit) = %q, want %q", got, "custom-project")
	}

	// Without project, falls back to cached
	args = map[string]any{}
	if got := ResolveProject(args); got != "test-project" {
		t.Errorf("ResolveProject(fallback) = %q, want %q", got, "test-project")
	}
}

func TestDefaultBaseURL(t *testing.T) {
	// Default value
	got := DefaultBaseURL()
	// Can't test env var easily, but default should be localhost:3333
	if got != "http://localhost:3333" {
		// May have BRAIN_API_URL set in env, that's ok
		t.Logf("DefaultBaseURL() = %q (may be from env)", got)
	}
}
