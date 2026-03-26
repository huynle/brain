package lifecycle

import "time"

// ServerStatus represents the current state of the server.
type ServerStatus string

const (
	// ServerStatusRunning indicates the server is actively running.
	ServerStatusRunning ServerStatus = "running"

	// ServerStatusStopped indicates the server is not running.
	ServerStatusStopped ServerStatus = "stopped"

	// ServerStatusCrashed indicates the server exited unexpectedly.
	ServerStatusCrashed ServerStatus = "crashed"

	// ServerStatusUnknown indicates the server state cannot be determined.
	ServerStatusUnknown ServerStatus = "unknown"
)

// ServerState holds the current state of the server process.
type ServerState struct {
	// Status is the current status of the server.
	Status ServerStatus

	// PID is the process ID of the running server, or 0 if not running.
	PID int

	// Uptime is how long the server has been running, or 0 if not running.
	Uptime time.Duration

	// Port is the HTTP port the server is listening on.
	Port int

	// StartedAt is when the server was started, or zero if not running.
	StartedAt time.Time
}

// DaemonOptions configures how a process is daemonized.
type DaemonOptions struct {
	// PIDFile is the path where the daemon PID will be written.
	PIDFile string

	// LogFile is where stdout/stderr will be redirected.
	LogFile string

	// ErrorLogFile is where stderr will be redirected (if different from LogFile).
	// If empty, stderr goes to LogFile.
	ErrorLogFile string

	// WorkDir is the working directory for the daemon.
	// If empty, uses current directory.
	WorkDir string
}
