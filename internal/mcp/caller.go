package mcp

import (
	"net/http"
	"path"
	"strings"
	"unicode/utf8"
)

// Request headers a hosted MCP client sends to describe itself. They are
// routing hints only: they decide where a task prefers to run and which
// project a call defaults to, never what the caller is allowed to do —
// authentication stays with the bearer token.
const (
	// HeaderBrainHostID carries the client's machine id, the same value the
	// runner on that machine reads from ~/.config/brain/machine-id.
	HeaderBrainHostID = "X-Brain-Host-Id"
	// HeaderBrainClientID identifies the client install (e.g. one OpenCode).
	HeaderBrainClientID = "X-Brain-Client-Id"
	// HeaderBrainWorkdir is the absolute directory the client is working in.
	HeaderBrainWorkdir = "X-Brain-Workdir"
	// HeaderBrainHome is the client's home directory, used only to express
	// the workdir home-relatively for runners on other machines.
	HeaderBrainHome = "X-Brain-Home"
)

const (
	maxCallerIDLen   = 128
	maxCallerPathLen = 1024
)

// CallerContext is what a hosted MCP client said about itself in request
// headers, after validation. Invalid headers are dropped and named in
// Rejected so context_get can explain why stamping did not happen.
type CallerContext struct {
	HostID   string
	ClientID string
	Workdir  string
	Home     string
	Rejected []string
}

// ParseCallerHeaders validates the caller headers on a hosted MCP request.
// It returns nil when the client sent none of them, so callers can tell
// "no caller context" from "caller context with everything rejected".
func ParseCallerHeaders(h http.Header) *CallerContext {
	c := &CallerContext{}
	present := false
	take := func(name string, valid func(string) bool, dst *string) {
		v := strings.TrimSpace(h.Get(name))
		if v == "" {
			return
		}
		present = true
		if valid(v) {
			*dst = v
		} else {
			c.Rejected = append(c.Rejected, name)
		}
	}
	take(HeaderBrainHostID, validCallerID, &c.HostID)
	take(HeaderBrainClientID, validCallerID, &c.ClientID)
	take(HeaderBrainWorkdir, validCallerPath, &c.Workdir)
	take(HeaderBrainHome, validCallerPath, &c.Home)
	if !present {
		return nil
	}
	return c
}

func validCallerID(v string) bool {
	if len(v) > maxCallerIDLen {
		return false
	}
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_', r == '-', r == '.', r == ':':
		default:
			return false
		}
	}
	return true
}

// validCallerPath accepts a clean absolute slash path: no traversal, no
// duplicate or trailing separators, no control characters.
func validCallerPath(v string) bool {
	if len(v) > maxCallerPathLen || !utf8.ValidString(v) || !strings.HasPrefix(v, "/") {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return path.Clean(v) == v
}

// worktreeContainers are the directory names under which this project's
// tooling creates linked worktrees. The removed stdio server asked git for the
// main worktree; the hosted server cannot run git on the client's disk, so it
// recognizes the layouts instead.
var worktreeContainers = []string{"/.worktrees/", "/.claude/worktrees/"}

// ExecutionContext derives the per-call execution context from the caller's
// headers, keeping the removed stdio server's guard against misfiling.
//
// That server named a project only when git vouched for the directory, so a
// container sitting in /app never became project "app". The hosted server
// cannot run git on the client's disk, so the declared home stands in: a
// project is derived only for a workdir strictly under X-Brain-Home, with
// linked worktrees mapped back to their repo, and never for home itself.
// Anything outside home — /app, /tmp, /workspace — or a call without a usable
// home yields no project.
func (c *CallerContext) ExecutionContext() ExecutionContext {
	ec := ExecutionContext{HostID: c.HostID, ClientID: c.ClientID, AbsPath: c.Workdir}
	if c.Workdir == "" {
		return ec
	}
	main := c.Workdir
	for _, marker := range worktreeContainers {
		if i := strings.Index(main+"/", marker); i >= 0 {
			main = main[:i]
		}
	}
	if main == "" {
		main = "/"
	}

	ec.Workdir = main
	if c.Home == "" || c.Home == "/" {
		return ec
	}
	if main == c.Home {
		ec.Workdir = ""
		return ec
	}
	rel, underHome := strings.CutPrefix(main, c.Home+"/")
	if !underHome {
		return ec
	}
	ec.Workdir = rel
	ec.ProjectID = callerProjectName(path.Base(main))
	return ec
}

// callerProjectName accepts a directory name as a project id only when it is
// already a plausible one; a header must not be able to mint odd names.
func callerProjectName(name string) string {
	if name == "" || name == "." || len(name) > maxCallerIDLen {
		return ""
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case (r == '_' || r == '-' || r == '.') && i > 0:
		default:
			return ""
		}
	}
	return name
}
