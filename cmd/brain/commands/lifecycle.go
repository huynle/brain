package commands

import (
	"encoding/json"
	"io"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/huynle/brain-api/internal/lifecycle"
)

// LifecycleFlags holds common flags for lifecycle commands.
type LifecycleFlags struct {
	PIDFile string
	LogFile string
	Timeout int  // Timeout in seconds for stop operations
	Force   bool // Force kill if graceful shutdown fails
	DryRun  bool // Dry-run mode (don't actually execute)
}

// StartCommand starts the server in daemon mode.
type StartCommand struct {
	Config *UnifiedConfig
	Flags  *LifecycleFlags
}

func (c *StartCommand) Type() string {
	return "start"
}

func (c *StartCommand) Execute() error {
	// Determine PID file path
	pidFile := c.Flags.PIDFile
	if pidFile == "" {
		pidFile = c.Config.Server.PIDFile
	}
	if pidFile == "" {
		homeDir, _ := os.UserHomeDir()
		pidFile = filepath.Join(homeDir, ".local", "state", "brain-api", "brain-api.pid")
	}

	// Determine log file path
	logFile := c.Flags.LogFile
	if logFile == "" {
		logFile = c.Config.Server.LogFile
	}
	if logFile == "" {
		homeDir, _ := os.UserHomeDir()
		logFile = filepath.Join(homeDir, ".local", "state", "brain-api", "brain-api.log")
	}

	// Check if server is already running
	if pid, err := lifecycle.ReadPID(pidFile); err == nil {
		if lifecycle.IsProcessRunning(pid) {
			return fmt.Errorf("server already running (PID %d)", pid)
		}
		// Stale PID file - clean it up
		fmt.Printf("Cleaning up stale PID file (process %d not running)\n", pid)
		if err := lifecycle.ClearPID(pidFile); err != nil {
			return fmt.Errorf("failed to clean stale PID file: %w", err)
		}
	}

	if c.Flags.DryRun {
		fmt.Printf("[DRY-RUN] Would start server (pid_file=%s, log_file=%s)\n", pidFile, logFile)
		return nil
	}

	// Get path to brain binary
	brainBinary, err := exec.LookPath("brain")
	if err != nil {
		return fmt.Errorf("brain binary not found in PATH: %w", err)
	}

	// Build daemon arguments
	args := []string{"server", "--daemon", "--log-file", logFile}
	if c.Config.Server.Port != 0 {
		args = append(args, "--port", fmt.Sprintf("%d", c.Config.Server.Port))
	}
	if c.Config.Server.Host != "" {
		args = append(args, "--host", c.Config.Server.Host)
	}

	// Daemonize
	opts := lifecycle.DaemonOptions{
		PIDFile: pidFile,
		LogFile: logFile,
	}

	pid, err := lifecycle.Daemonize(brainBinary, args, opts)
	if err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	fmt.Printf("Server started (PID %d)\n", pid)
	fmt.Printf("Logs: %s\n", logFile)
	return nil
}

// StopCommand stops a running server.
type StopCommand struct {
	Config *UnifiedConfig
	Flags  *LifecycleFlags
}

func (c *StopCommand) Type() string {
	return "stop"
}

func (c *StopCommand) Execute() error {
	// Determine PID file path
	pidFile := c.Flags.PIDFile
	if pidFile == "" {
		pidFile = c.Config.Server.PIDFile
	}
	if pidFile == "" {
		homeDir, _ := os.UserHomeDir()
		pidFile = filepath.Join(homeDir, ".local", "state", "brain-api", "brain-api.pid")
	}

	// Read PID
	pid, err := lifecycle.ReadPID(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("server not running (no PID file)")
		}
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	// Check if process is running
	if !lifecycle.IsProcessRunning(pid) {
		// Clean up stale PID file
		lifecycle.ClearPID(pidFile)
		return fmt.Errorf("server not running (stale PID %d)", pid)
	}

	if c.Flags.DryRun {
		fmt.Printf("[DRY-RUN] Would stop server (PID %d)\n", pid)
		return nil
	}

	// Send SIGTERM for graceful shutdown
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	fmt.Printf("Stopping server (PID %d)...\n", pid)
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM: %w", err)
	}

	// Wait for process to exit with timeout
	timeout := c.Flags.Timeout
	if timeout == 0 {
		timeout = 10 // Default 10 seconds
	}

	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	for time.Now().Before(deadline) {
		if !lifecycle.IsProcessRunning(pid) {
			// Process stopped - clean up PID file
			lifecycle.ClearPID(pidFile)
			fmt.Println("Server stopped")
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Timeout - force kill if requested
	if c.Flags.Force {
		fmt.Println("Graceful shutdown timeout, sending SIGKILL")
		if err := proc.Signal(syscall.SIGKILL); err != nil {
			return fmt.Errorf("failed to send SIGKILL: %w", err)
		}
		time.Sleep(500 * time.Millisecond)
		lifecycle.ClearPID(pidFile)
		fmt.Println("Server killed")
		return nil
	}

	return fmt.Errorf("server did not stop within %d seconds", timeout)
}

// RestartCommand restarts the server.
type RestartCommand struct {
	Config *UnifiedConfig
	Flags  *LifecycleFlags
}

func (c *RestartCommand) Type() string {
	return "restart"
}

func (c *RestartCommand) Execute() error {
	// Determine PID file path
	pidFile := c.Flags.PIDFile
	if pidFile == "" {
		pidFile = c.Config.Server.PIDFile
	}
	if pidFile == "" {
		homeDir, _ := os.UserHomeDir()
		pidFile = filepath.Join(homeDir, ".local", "state", "brain-api", "brain-api.pid")
	}

	// Check if server is running
	isRunning := false
	if pid, err := lifecycle.ReadPID(pidFile); err == nil {
		if lifecycle.IsProcessRunning(pid) {
			isRunning = true
		}
	}

	// Stop if running
	if isRunning {
		stopCmd := &StopCommand{
			Config: c.Config,
			Flags:  c.Flags,
		}
		if err := stopCmd.Execute(); err != nil {
			return fmt.Errorf("failed to stop server: %w", err)
		}
	}

	// Start
	startCmd := &StartCommand{
		Config: c.Config,
		Flags:  c.Flags,
	}
	return startCmd.Execute()
}

// StatusFlags holds flags for the status command.
type StatusFlags struct {
	JSON bool
}

// StatusCommand checks the server status.
type StatusCommand struct {
	Config *UnifiedConfig
	Flags  *StatusFlags
	Out    io.Writer
}

func (c *StatusCommand) Type() string {
	return "status"
}

func (c *StatusCommand) Execute() error {
	c.Out = getWriter(c.Out)
	// Determine PID file path
	pidFile := c.Config.Server.PIDFile
	if pidFile == "" {
		homeDir, _ := os.UserHomeDir()
		pidFile = filepath.Join(homeDir, ".local", "state", "brain-api", "brain-api.pid")
	}

	// Get port
	port := c.Config.Server.Port
	if port == 0 {
		port = 3333 // Default port
	}

	// Get server status
	state, err := lifecycle.GetServerStatus(pidFile, port)
	if err != nil {
		return fmt.Errorf("failed to get server status: %w", err)
	}

	// Format output
	if c.Flags.JSON {
		return c.writeJSON(state)
	}
	return c.writeText(state)
}

func (c *StatusCommand) writeJSON(state lifecycle.ServerState) error {
	data := map[string]interface{}{
		"status": string(state.Status),
		"pid":    state.PID,
		"port":   state.Port,
	}
	if state.Status == lifecycle.ServerStatusRunning {
		data["uptime"] = state.Uptime.String()
		data["started_at"] = state.StartedAt.Format(time.RFC3339)
	}

	enc := json.NewEncoder(c.Out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}

	// Exit code based on status
	if state.Status != lifecycle.ServerStatusRunning {
		return fmt.Errorf("server not running")
	}
	return nil
}

func (c *StatusCommand) writeText(state lifecycle.ServerState) error {
	switch state.Status {
	case lifecycle.ServerStatusRunning:
		fmt.Fprintf(c.Out, "Status: running\n")
		fmt.Fprintf(c.Out, "PID: %d\n", state.PID)
		fmt.Fprintf(c.Out, "Port: %d\n", state.Port)
		if state.Uptime > 0 {
			fmt.Fprintf(c.Out, "Uptime: %s\n", formatUptime(state.Uptime))
		}
		return nil

	case lifecycle.ServerStatusStopped:
		fmt.Fprintf(c.Out, "Status: stopped\n")
		return fmt.Errorf("server not running")

	case lifecycle.ServerStatusCrashed:
		fmt.Fprintf(c.Out, "Status: crashed (PID %d no longer exists)\n", state.PID)
		return fmt.Errorf("server crashed")

	default:
		fmt.Fprintf(c.Out, "Status: unknown\n")
		return fmt.Errorf("unknown status")
	}
}

// formatUptime formats a duration in human-readable form.
func formatUptime(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	return fmt.Sprintf("%dd %dh", days, hours)
}

// getWriter returns the writer to use, defaulting to os.Stdout if nil.
func getWriter(w io.Writer) io.Writer {
	if w == nil {
		return os.Stdout
	}
	return w
}

// DevCommand runs the server in foreground with debug logging.
type DevCommand struct {
	Config *UnifiedConfig
}

func (c *DevCommand) Type() string {
	return "dev"
}

func (c *DevCommand) Execute() error {
	// Set debug log level
	if c.Config.Server.LogLevel == "" {
		c.Config.Server.LogLevel = "debug"
	}

	// Create a server command with foreground mode (no daemon)
	serverCmd := &ServerCommand{
		Config: c.Config,
		Flags: &ServerFlags{
			Port:   c.Config.Server.Port,
			Host:   c.Config.Server.Host,
			Daemon: false, // Always foreground
		},
	}

	fmt.Println("Starting server in development mode (debug logging, foreground)")
	fmt.Println("Press Ctrl+C to stop")
	fmt.Println()

	return serverCmd.Execute()
}
