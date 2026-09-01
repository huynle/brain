package mcp

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/huynle/brain-api/internal/identity"
)

// ExecutionContext holds the detected project context for MCP tool calls.
//
// Project fields (ProjectID/Workdir/GitRemote/GitBranch) describe where the
// MCP server was launched. Identity fields (ClientID/HostID/Hostname/OS/
// Arch/Username/HomeDir) describe who/where is calling, and are used by
// Phase 2 (task stamping) and Phase 3 (affinity routing) to align
// MCP-created tasks with the runner that will execute them. HostID is
// intentionally derived from the same machine-id file the runner uses, so
// affinity matching across processes works.
type ExecutionContext struct {
	// Project context (where the MCP server was launched).
	ProjectID string // Short project name (last path segment)
	Workdir   string // Home-relative path to main repo
	GitRemote string // Git remote URL (origin)
	GitBranch string // Current git branch

	// AbsPath is the caller's ACTUAL working directory, absolute and
	// un-normalized — the linked worktree, not the main repo. Workdir is
	// home-relative and gets re-resolved against whatever host runs the
	// task; AbsPath is only meaningful together with HostID, and is stamped
	// on tasks as origin_path so a runner on the same machine can use the
	// directory the author was really in.
	AbsPath string

	// Identity context (who/where is calling). Populated once per process.
	ClientID string // MCP per-install client id (e.g. mcp-<uuid>)
	HostID   string // Stable machine id shared with the runner
	Hostname string // os.Hostname()
	OS       string // runtime.GOOS
	Arch     string // runtime.GOARCH
	Username string // current user's username (best-effort)
	HomeDir  string // current user's home directory (best-effort)
}

// GetExecutionContext detects the project context from the given directory
// and resolves the calling process's identity (client id, host id, host
// metadata). Identity resolution is best-effort: any failure degrades to a
// safe default rather than blocking startup.
func GetExecutionContext(directory string) ExecutionContext {
	home, _ := os.UserHomeDir()
	mainRepoPath := directory
	var gitRemote, gitBranch string
	// insideRepo records whether git recognised this directory at all. It is
	// what separates a real project name from the basename of whatever
	// directory the process happens to be running in.
	insideRepo := false

	// Try to get the main worktree path
	if out, err := gitCommand(directory, "worktree", "list", "--porcelain"); err == nil {
		insideRepo = true
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "worktree ") {
				mainRepoPath = strings.TrimPrefix(line, "worktree ")
				break
			}
		}
	}

	// Get git remote
	if out, err := gitCommand(directory, "remote", "get-url", "origin"); err == nil {
		gitRemote = strings.TrimSpace(out)
	}

	// Get current branch
	if out, err := gitCommand(directory, "branch", "--show-current"); err == nil {
		gitBranch = strings.TrimSpace(out)
	}

	workdir := makeHomeRelative(mainRepoPath, home)

	hostname, _ := os.Hostname()

	var username, homeDir string
	if u, err := user.Current(); err == nil && u != nil {
		username = u.Username
		homeDir = u.HomeDir
	}
	if homeDir == "" {
		// user.Current can fail in static builds / minimal containers; fall
		// back to os.UserHomeDir which honors $HOME.
		if h, err := os.UserHomeDir(); err == nil {
			homeDir = h
		}
	}

	// Only claim an absolute origin path when git vouched for the directory
	// being a repository. Outside a repo there is nothing for a runner to do
	// with the path, and stamping one would invite it to open an unrelated
	// directory that happens to exist at the same location.
	absPath := ""
	if insideRepo && filepath.IsAbs(directory) {
		absPath = directory
	}

	return ExecutionContext{
		ProjectID: resolveProjectName(workdir, insideRepo),
		Workdir:   workdir,
		AbsPath:   absPath,
		GitRemote: gitRemote,
		GitBranch: gitBranch,

		ClientID: LoadOrCreateMCPClientID(),
		HostID:   identity.ResolveMachineID(),
		Hostname: hostname,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Username: username,
		HomeDir:  homeDir,
	}
}

// resolveProjectName extracts a short project name from a home-relative path.
// e.g., "projects/brain-api" → "brain-api", "brain-api" → "brain-api"
//
// insideRepo says whether git recognised the directory. It is the difference
// between "the last path segment is this project's name" and "the last path
// segment is whatever directory this process happens to be sitting in".
//
// Without it, an unconditional basename fallback answers confidently from
// anywhere. The MCP server runs in a container whose working directory is
// /app, which never matches the home prefix, so makeHomeRelative returned it
// unchanged and this function called the project "app" — with no failure
// signal. Seven substantive Hindsight entries were written to projects/app/
// as a result, invisible to anyone searching the project they belong to, and
// "app" now appears in scheduler_status as a live project.
//
// Returns "" when the project cannot be determined. Callers must treat that
// as "ask the user", never as a name.
func resolveProjectName(homeRelativePath string, insideRepo bool) string {
	// Still absolute means makeHomeRelative found no home prefix to strip,
	// so this path is not under the user's home. Unless git vouched for it
	// being a repository, we do not know what project it is.
	if strings.HasPrefix(homeRelativePath, "/") && !insideRepo {
		return ""
	}

	segments := strings.Split(homeRelativePath, "/")
	var filtered []string
	for _, s := range segments {
		if s != "" {
			filtered = append(filtered, s)
		}
	}
	if len(filtered) == 0 {
		return ""
	}
	return filtered[len(filtered)-1]
}

// makeHomeRelative converts an absolute path to a home-relative path.
// makeHomeRelative strips a home-directory prefix, so callers can tell
// "somewhere under the user's home" from "somewhere else entirely".
//
// A home of "/" is ignored, because it is a prefix of EVERY absolute path and
// would make every path look home-relative — destroying exactly the signal
// resolveProjectName depends on. That is not hypothetical: the MCP server runs
// in a container with HOME=/ and WORKDIR=/app, so "/app" became "app", the
// leading-slash guard in resolveProjectName never fired, and the basename
// fallback confidently answered "app".
//
// That is the same wrong answer 25c02d5 set out to eliminate, arrived at by a
// different route — verified against the live container, which reported
// Project: app while sitting in a directory that is not a git repository.
func makeHomeRelative(path, home string) string {
	if home == "" || home == "/" {
		return path
	}
	if strings.HasPrefix(path, home) {
		rel := strings.TrimPrefix(path, home)
		rel = strings.TrimPrefix(rel, "/")
		return rel
	}
	return path
}

// gitCommand runs a git command in the given directory and returns stdout.
func gitCommand(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// DefaultBaseURL returns the Brain API base URL from environment or default.
func DefaultBaseURL() string {
	if u := os.Getenv("BRAIN_API_URL"); u != "" {
		return u
	}
	return "http://localhost:3333"
}

// CachedContext holds the lazily-initialized execution context.
var cachedContext *ExecutionContext

// ContextDir is the directory used for execution context detection.
var ContextDir = func() string {
	dir, _ := os.Getwd()
	return dir
}

// GetCachedContext returns the execution context, computing it once.
func GetCachedContext() ExecutionContext {
	if cachedContext == nil {
		ctx := GetExecutionContext(ContextDir())
		cachedContext = &ctx
	}
	return *cachedContext
}

// ResolveProject returns the project ID from args or falls back to cached context.
func ResolveProject(args map[string]any) string {
	if p, ok := args["project"].(string); ok && p != "" {
		return p
	}
	return GetCachedContext().ProjectID
}

// ResolveProjectArg returns the project ID from args, preferring the canonical
// "project" key, accepting legacy "project_id"/"projectId" spellings, and
// falling back to the ambient launch-directory context.
func ResolveProjectArg(args map[string]any) string {
	if p := StringArgAlias(args, "", "project", "project_id", "projectId"); p != "" {
		return p
	}
	return GetCachedContext().ProjectID
}

// argAlias returns the first non-nil raw value among keys.
// Use for values passed through to API request bodies unchanged.
func argAlias(args map[string]any, keys ...string) any {
	for _, key := range keys {
		if v, ok := args[key]; ok && v != nil {
			return v
		}
	}
	return nil
}

// StringArgAlias returns the first non-empty string value among keys.
// Use to accept a canonical snake_case parameter name alongside legacy
// camelCase spellings.
func StringArgAlias(args map[string]any, defaultVal string, keys ...string) string {
	for _, key := range keys {
		if v, ok := args[key].(string); ok && v != "" {
			return v
		}
	}
	return defaultVal
}

// BoolArgAlias returns the first present boolean value among keys.
func BoolArgAlias(args map[string]any, defaultVal bool, keys ...string) bool {
	for _, key := range keys {
		if v, ok := args[key].(bool); ok {
			return v
		}
	}
	return defaultVal
}

// IntArgAlias returns the first present numeric value among keys.
// JSON numbers are decoded as float64 by default.
func IntArgAlias(args map[string]any, defaultVal int, keys ...string) int {
	for _, key := range keys {
		if v, ok := args[key].(float64); ok {
			return int(v)
		}
	}
	return defaultVal
}

// StringSliceArgAlias returns the first present string-array value among keys.
func StringSliceArgAlias(args map[string]any, keys ...string) []string {
	for _, key := range keys {
		if v := StringSliceArg(args, key); v != nil {
			return v
		}
	}
	return nil
}

// PathFromArgs extracts a path from args, trying keys in order.
func PathFromArgs(args map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := args[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// StringArg extracts a string argument with a default value.
func StringArg(args map[string]any, key, defaultVal string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return defaultVal
}

// IntArg extracts a numeric argument with a default value.
// JSON numbers are decoded as float64 by default.
func IntArg(args map[string]any, key string, defaultVal int) int {
	if v, ok := args[key].(float64); ok {
		return int(v)
	}
	return defaultVal
}

// BoolArg extracts a boolean argument with a default value.
func BoolArg(args map[string]any, key string, defaultVal bool) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return defaultVal
}

// StringSliceArg extracts a string array argument.
func StringSliceArg(args map[string]any, key string) []string {
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	if arr, ok := v.([]any); ok {
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}
