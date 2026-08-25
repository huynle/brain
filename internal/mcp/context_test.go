package mcp

import (
	"testing"
)

func TestResolveProjectName(t *testing.T) {
	tests := []struct {
		input string
		repo  bool
		want  string
	}{
		// Home-relative paths resolve to their last segment.
		{"projects/brain-api", false, "brain-api"},
		{"brain-api", false, "brain-api"},
		{"projects/foo/bar", false, "bar"},
		{"single", false, "single"},
		{"", false, ""},

		// An absolute path means makeHomeRelative found no home prefix to
		// strip, so this is not under the user's home. Without git vouching
		// for it, the project is unknown and must not be guessed.
		//
		// "/app" is the live case: the MCP server runs in a container with
		// that working directory, and the old unconditional basename
		// fallback confidently reported the project as "app". Seven
		// Hindsight entries were written to projects/app/ because of it.
		{"/app", false, ""},
		{"/", false, ""},
		{"/opt/someworkdir", false, ""},

		// Git recognising the directory is what makes the basename
		// meaningful, so a repo outside home still resolves.
		{"/opt/checkouts/myrepo", true, "myrepo"},
		{"/app", true, "app"},
	}

	for _, tt := range tests {
		got := resolveProjectName(tt.input, tt.repo)
		if got != tt.want {
			t.Errorf("resolveProjectName(%q, insideRepo=%v) = %q, want %q", tt.input, tt.repo, got, tt.want)
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

// TestMakeHomeRelative_RootHomeIsNotAPrefix reproduces the live container
// exactly: HOME=/ and WORKDIR=/app, with no git repository present.
//
// A home of "/" is a prefix of every absolute path, so treating it as one made
// "/app" look home-relative ("app"). resolveProjectName's guard then found no
// leading slash, concluded the path WAS under home, and let the basename
// fallback answer "app" — the same invented project name 25c02d5 removed,
// reached by a different route. Confirmed against production: context_get
// reported "Project: app" while sitting in a non-repository directory, and
// seven Hindsight entries had already been misfiled to projects/app/.
func TestMakeHomeRelative_RootHomeIsNotAPrefix(t *testing.T) {
	if got := makeHomeRelative("/app", "/"); got != "/app" {
		t.Errorf("makeHomeRelative(%q, %q) = %q, want it left absolute", "/app", "/", got)
	}
	if got := makeHomeRelative("/var/lib/thing", "/"); got != "/var/lib/thing" {
		t.Errorf("a root home must never make a path look home-relative, got %q", got)
	}
	// A real home still works.
	if got := makeHomeRelative("/Users/huy/projects/brain-api", "/Users/huy"); got != "projects/brain-api" {
		t.Errorf("makeHomeRelative with a real home = %q, want %q", got, "projects/brain-api")
	}
}

// TestResolveProjectName_ContainerWorkdirYieldsNoProject is the end-to-end
// version: the container's environment must produce "" — "ask the user" — not a
// confident wrong name. "" is what callers treat as unknown; any non-empty
// answer here silently files entries into a project nobody chose.
func TestResolveProjectName_ContainerWorkdirYieldsNoProject(t *testing.T) {
	// Exactly what the deployed MCP server sees.
	rel := makeHomeRelative("/app", "/")
	if got := resolveProjectName(rel, false); got != "" {
		t.Errorf("resolveProjectName for the container workdir = %q, want \"\" (unknown)", got)
	}

	// A git repo outside home still resolves — the fix must not make the
	// legitimate case unknown too.
	if got := resolveProjectName(makeHomeRelative("/srv/checkouts/orion-ai", "/"), true); got != "orion-ai" {
		t.Errorf("a repo outside home should still resolve, got %q", got)
	}
}
