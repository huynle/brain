// Package bridge implements the WebSocket tunnel between brain-api and
// runners that carries proxied OpenCode requests and event streams.
//
// The runner dials out to the API (no inbound ports on runner machines) and
// multiplexes concurrent requests and per-instance event streams over a
// single connection using JSON frames with correlation IDs. Instances are
// addressed by opaque instance IDs — only the runner knows the localhost
// port behind an ID, so the tunnel can never be used to reach arbitrary
// host:port targets.
package bridge

import (
	"encoding/json"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// ProtoVersion is the bridge protocol version carried in hello frames.
const ProtoVersion = 1

// Frame types.
const (
	FrameHello         = "hello"          // runner → api: identify + current instances
	FrameReq           = "req"            // api → runner: proxied HTTP request
	FrameRes           = "res"            // runner → api: response to req/spawn/kill
	FrameStreamOpen    = "stream_open"    // api → runner: start full event forwarding
	FrameStreamClose   = "stream_close"   // api → runner: stop full event forwarding
	FrameStreamEvent   = "stream_event"   // runner → api: full-stream opencode event
	FrameStreamClosed  = "stream_closed"  // runner → api: upstream stream ended
	FrameInstanceEvent = "instance_event" // runner → api: always-on control event
	FrameSpawn         = "spawn"          // api → runner: spawn ad-hoc instance
	FrameKill          = "kill"           // api → runner: kill ad-hoc instance
	FrameAbortTask     = "abort_task"     // api → runner: abort task instance and reset pending
	FrameHistory       = "history"        // api → runner: fetch a session transcript (no live instance needed)

	// Runner shell. exec_start is correlated (answered by a res frame that
	// acks the spawn); output afterwards arrives as uncorrelated exec_data
	// pushes tagged with the same ExecID, terminated by exactly one
	// exec_exit.
	FrameExecStart  = "exec_start"  // api → runner: start a shell command
	FrameExecSignal = "exec_signal" // api → runner: signal a running command
	FrameExecData   = "exec_data"   // runner → api: output chunk
	FrameExecExit   = "exec_exit"   // runner → api: command finished
)

// Exec stream names carried on Frame.Stream for exec_data frames.
const (
	ExecStreamStdout = "stdout"
	ExecStreamStderr = "stderr"
)

// Limits enforced on both sides of the tunnel.
const (
	// MaxBodyBytes caps proxied request/response bodies. Generous enough to
	// carry a pasted image as a base64 data URL in a prompt.
	MaxBodyBytes = 24 << 20 // 24 MB
	// MaxFrameBytes caps a whole frame on the wire (body + envelope overhead).
	MaxFrameBytes = 28 << 20 // 28 MB
	// MaxInFlight caps concurrent proxied requests per runner connection.
	MaxInFlight = 64
	// DefaultTimeoutMs is the default per-request timeout when none is given.
	DefaultTimeoutMs = 30_000

	// ExecDefaultTimeoutMs bounds how long a shell command may run before
	// the runner kills it. Shell commands are interactive, so this is much
	// longer than a proxied request timeout.
	ExecDefaultTimeoutMs = 15 * 60 * 1000 // 15 min
	// ExecMaxTimeoutMs is the ceiling a caller may request.
	ExecMaxTimeoutMs = 60 * 60 * 1000 // 1 hour
	// ExecMaxCommandBytes caps the command line accepted over the wire.
	ExecMaxCommandBytes = 64 << 10 // 64 KB
	// ExecChunkBytes is the read buffer for command output; each read
	// becomes one exec_data frame, so this also bounds frame size.
	ExecChunkBytes = 16 << 10 // 16 KB
)

// Frame is the single wire format for all bridge messages.
type Frame struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`

	// hello
	RunnerID  string                   `json:"runner_id,omitempty"`
	Proto     int                      `json:"proto,omitempty"`
	Instances []types.OpencodeInstance `json:"instances,omitempty"`

	// history
	SessionID string `json:"session_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`

	// req / res / streams / kill
	InstanceID string          `json:"instance_id,omitempty"`
	Method     string          `json:"method,omitempty"`
	Path       string          `json:"path,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
	Status     int             `json:"status,omitempty"`
	Error      string          `json:"error,omitempty"`
	TimeoutMs  int             `json:"timeout_ms,omitempty"`
	Reason     string          `json:"reason,omitempty"`

	// stream_event / instance_event
	Event json.RawMessage `json:"event,omitempty"`

	// spawn
	Spec *types.SpawnInstanceSpec `json:"spec,omitempty"`

	// exec_start / exec_signal / exec_data / exec_exit
	ExecID  string `json:"exec_id,omitempty"`
	Command string `json:"command,omitempty"`
	Workdir string `json:"workdir,omitempty"`
	// ExecTimeoutMs is the command's own budget, distinct from TimeoutMs
	// (which bounds how long the API waits for the spawn ack).
	ExecTimeoutMs int    `json:"exec_timeout_ms,omitempty"`
	Stream        string `json:"stream,omitempty"`    // ExecStreamStdout | ExecStreamStderr
	Chunk         string `json:"chunk,omitempty"`     // raw output bytes for this frame
	ExitCode      int    `json:"exit_code,omitempty"` // exec_exit; absent means 0
	Signal        string `json:"signal,omitempty"`    // exec_signal: "int" | "term" | "kill"
}

// allowedRoute is one entry of the proxied-endpoint allowlist. Pattern
// segments equal to "*" match exactly one non-empty path segment.
type allowedRoute struct {
	method  string
	pattern string
}

// allowedRoutes is the explicit allowlist of OpenCode endpoints the bridge
// will proxy. Enforced on BOTH sides (api and runner) for defense in depth.
// /event is intentionally absent — event streaming uses stream frames.
var allowedRoutes = []allowedRoute{
	{"GET", "/session"},
	{"POST", "/session"},
	{"GET", "/session/status"},
	{"GET", "/session/*/message"},
	{"POST", "/session/*/prompt_async"},
	{"POST", "/session/*/permissions/*"},
	{"POST", "/session/*/abort"},
	{"GET", "/agent"},
	{"GET", "/config/providers"},
	{"GET", "/global/health"},
}

// AllowedRequest reports whether a proxied method+path is on the allowlist.
// Query strings are permitted and ignored for matching.
func AllowedRequest(method, path string) bool {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	segs := splitPath(path)
	for _, route := range allowedRoutes {
		if route.method != method {
			continue
		}
		if matchSegments(splitPath(route.pattern), segs) {
			return true
		}
	}
	return false
}

func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func matchSegments(pattern, segs []string) bool {
	if len(pattern) != len(segs) {
		return false
	}
	for i, p := range pattern {
		if p == "*" {
			if segs[i] == "" {
				return false
			}
			continue
		}
		if p != segs[i] {
			return false
		}
	}
	return true
}
