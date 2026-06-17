package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/huynle/brain-api/internal/apiserver"
	"github.com/huynle/brain-api/internal/lifecycle"
	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/runnercli"
)

var runAPIServer = apiserver.RunServer
var runEmbeddedTaskRunner = runnercli.RunTaskRunner
var waitForEmbeddedRunnerAPI = waitForAPIHealth
var resolveEmbeddedRunnerProjects = resolveProjectList

func runServerWithOptionalRunner(ctx context.Context, cfg *UnifiedConfig, opts apiserver.ServerOptions, flags LifecycleFlags) error {
	if !flags.Runner {
		return runAPIServer(ctx, opts)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- runAPIServer(ctx, opts)
	}()

	project, runnerCfg := embeddedRunnerConfig(cfg, opts, embeddedRunnerFlags{
		RunnerProject: flags.RunnerProject,
		MaxParallel:   flags.MaxParallel,
		Include:       flags.Include,
		Exclude:       flags.Exclude,
		Executor:      flags.Executor,
	})

	if err := waitForEmbeddedRunnerAPI(ctx, runnerCfg.BrainAPIURL); err != nil {
		cancel()
		return err
	}

	projects, err := resolveEmbeddedRunnerProjects(project, runnerCfg)
	if err != nil {
		cancel()
		return err
	}

	runnerErrCh := make(chan error, 1)
	go func() {
		runnerErrCh <- runEmbeddedTaskRunner(ctx, runnercli.RunnerOptions{
			Projects:    projects,
			Config:      runnerCfg,
			Mode:        "headless",
			StartPaused: false,
			KeyBindings: cfg.TUI.KeyBindings,
		})
	}()

	select {
	case err := <-runnerErrCh:
		cancel()
		if serverErr := <-serverErrCh; serverErr != nil && err == nil {
			return serverErr
		}
		return err
	case err := <-serverErrCh:
		cancel()
		if err != nil {
			return err
		}
		return <-runnerErrCh
	case <-ctx.Done():
		cancel()
		return ctx.Err()
	}
}

func waitForAPIHealth(ctx context.Context, apiURL string) error {
	healthURL := strings.TrimRight(apiURL, "/") + "/health"
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 500 {
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for Brain API at %s", healthURL)
		case <-ticker.C:
		}
	}
}

func lifecycleFlagsFromAPIFlags(flags *APIFlags) LifecycleFlags {
	if flags == nil {
		return LifecycleFlags{}
	}
	return LifecycleFlags{
		Port:          flags.Port,
		Host:          flags.Host,
		Daemon:        flags.Daemon,
		Runner:        flags.Runner,
		RunnerProject: flags.RunnerProject,
		MaxParallel:   flags.MaxParallel,
		Include:       flags.Include,
		Exclude:       flags.Exclude,
		Executor:      flags.Executor,
	}
}

type embeddedRunnerFlags struct {
	RunnerProject string
	MaxParallel   int
	Include       []string
	Exclude       []string
	Executor      string
}

func embeddedRunnerConfig(cfg *UnifiedConfig, opts apiserver.ServerOptions, flags embeddedRunnerFlags) (string, runner.RunnerConfig) {
	project := flags.RunnerProject
	if project == "" {
		project = "all"
	}
	runnerCfg := cfg.Runner
	runnerCfg.BrainAPIURL = embeddedRunnerAPIURL(cfg, opts)
	if flags.MaxParallel != 0 {
		runnerCfg.MaxParallel = flags.MaxParallel
	}
	if flags.Executor != "" {
		runnerCfg.DefaultExecutor = flags.Executor
	}
	if len(flags.Include) > 0 {
		runnerCfg.IncludeProjects = append(runnerCfg.IncludeProjects, flags.Include...)
	}
	if len(flags.Exclude) > 0 {
		runnerCfg.ExcludeProjects = append(runnerCfg.ExcludeProjects, flags.Exclude...)
	}
	return project, runnerCfg
}

func embeddedRunnerAPIURL(cfg *UnifiedConfig, opts apiserver.ServerOptions) string {
	if cfg != nil && cfg.Runner.BrainAPIURL != "" && !isDefaultLocalAPIURL(cfg.Runner.BrainAPIURL) {
		return cfg.Runner.BrainAPIURL
	}

	host := opts.Host
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "localhost"
	}

	scheme := "http"
	if cfg != nil && cfg.Server.TLS.Enabled {
		scheme = "https"
	}

	return fmt.Sprintf("%s://%s:%d", scheme, host, opts.Port)
}

func isDefaultLocalAPIURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "http" && u.Hostname() == "localhost" && u.Port() == "3333"
}

// LifecycleFlags holds common flags for lifecycle commands.
type LifecycleFlags struct {
	PIDFile       string
	LogFile       string
	Timeout       int      // Timeout in seconds for stop operations
	Force         bool     // Force kill if graceful shutdown fails
	DryRun        bool     // Dry-run mode (don't actually execute)
	Daemon        bool     // Run as background daemon
	Port          int      // Server port override
	Host          string   // Server host override
	Runner        bool     // Run embedded task runner with API server
	RunnerProject string   // Embedded runner project scope
	MaxParallel   int      // Embedded runner max parallel tasks
	Include       []string // Embedded runner include project patterns
	Exclude       []string // Embedded runner exclude project patterns
	Executor      string   // Embedded runner executor override
}

// defaultPIDFile returns the default PID file path, respecting XDG_STATE_HOME.
func defaultPIDFile() string {
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		homeDir, _ := os.UserHomeDir()
		stateHome = filepath.Join(homeDir, ".local", "state")
	}
	return filepath.Join(stateHome, "brain-api", "brain-api.pid")
}

// defaultLogFile returns the default log file path, respecting XDG_STATE_HOME.
func defaultLogFile() string {
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		homeDir, _ := os.UserHomeDir()
		stateHome = filepath.Join(homeDir, ".local", "state")
	}
	return filepath.Join(stateHome, "brain-api", "brain-api.log")
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
		pidFile = defaultPIDFile()
	}

	// Determine log file path
	logFile := c.Flags.LogFile
	if logFile == "" {
		logFile = c.Config.Server.LogFile
	}
	if logFile == "" {
		logFile = defaultLogFile()
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
		mode := "foreground"
		if c.Flags.Daemon {
			mode = "daemon"
		}
		fmt.Printf("[DRY-RUN] Would start server in %s mode (pid_file=%s, log_file=%s)\n", mode, pidFile, logFile)
		if c.Flags.Runner {
			project := c.Flags.RunnerProject
			if project == "" {
				project = "all"
			}
			opts := apiserver.ServerOptions{Port: c.Config.Server.Port, Host: c.Config.Server.Host}
			fmt.Printf("[DRY-RUN] Would start embedded runner (project=%s, api_url=%s)\n", project, embeddedRunnerAPIURL(c.Config, opts))
		}
		return nil
	}

	// Daemon mode: fork a detached child process
	if c.Flags.Daemon {
		return c.startDaemon(pidFile, logFile)
	}

	// Default: run in foreground
	return c.startForeground(pidFile)
}

func (c *StartCommand) daemonArgs(logFile string) []string {
	args := []string{"api", "--daemon", "--log-file", logFile}
	if c.Config.Server.Port != 0 {
		args = append(args, "--port", fmt.Sprintf("%d", c.Config.Server.Port))
	}
	if c.Config.Server.Host != "" {
		args = append(args, "--host", c.Config.Server.Host)
	}
	if c.Flags.Runner {
		args = append(args, "--runner")
		if c.Flags.RunnerProject != "" {
			args = append(args, "--runner-project", c.Flags.RunnerProject)
		}
		if c.Flags.MaxParallel != 0 {
			args = append(args, "--max-parallel", fmt.Sprintf("%d", c.Flags.MaxParallel))
		}
		for _, include := range c.Flags.Include {
			args = append(args, "--include", include)
		}
		for _, exclude := range c.Flags.Exclude {
			args = append(args, "--exclude", exclude)
		}
		if c.Flags.Executor != "" {
			args = append(args, "--executor", c.Flags.Executor)
		}
	}
	return args
}

// startDaemon forks a detached child process running the API server.
func (c *StartCommand) startDaemon(pidFile, logFile string) error {
	// Get path to brain binary
	brainBinary, err := exec.LookPath("brain")
	if err != nil {
		return fmt.Errorf("brain binary not found in PATH: %w", err)
	}

	// Build daemon arguments
	args := c.daemonArgs(logFile)

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

// startForeground runs the API server in the current process.
func (c *StartCommand) startForeground(pidFile string) error {
	opts := apiserver.ServerOptions{
		Port:         c.Config.Server.Port,
		Host:         c.Config.Server.Host,
		BrainDir:     c.Config.Server.BrainDir,
		EnableAuth:   c.Config.Server.EnableAuth,
		LogLevel:     c.Config.Server.LogLevel,
		CORSOrigin:   c.Config.Server.CORSOrigin,
		OAuthPIN:     c.Config.Server.OAuthPIN,
		TaskDefaults: c.Config.Server.TaskDefaults,
		Embedding:    c.Config.Server.Embedding,
		Attachments:  c.Config.Server.Attachments,

		AttachmentExtraction: c.Config.Server.AttachmentExtraction,
	}

	// Create context with signal handling for graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Write PID file so `brain api stop` works against foreground processes too
	if err := lifecycle.WritePID(pidFile, os.Getpid()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write PID file: %v\n", err)
	} else {
		defer lifecycle.ClearPID(pidFile)
	}

	fmt.Printf("Starting Brain API server on %s:%d\n", opts.Host, opts.Port)
	if c.Flags.Runner {
		project, runnerCfg := embeddedRunnerConfig(c.Config, opts, embeddedRunnerFlags{
			RunnerProject: c.Flags.RunnerProject,
			MaxParallel:   c.Flags.MaxParallel,
			Include:       c.Flags.Include,
			Exclude:       c.Flags.Exclude,
			Executor:      c.Flags.Executor,
		})
		fmt.Printf("Starting embedded runner for project %s using %s\n", project, runnerCfg.BrainAPIURL)
	}
	fmt.Println("Press Ctrl+C to stop")
	return runServerWithOptionalRunner(ctx, c.Config, opts, *c.Flags)
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
		pidFile = defaultPIDFile()
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
		pidFile = defaultPIDFile()
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
		pidFile = defaultPIDFile()
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

	// Create an API command with foreground mode (no daemon)
	apiCmd := &APICommand{
		Config: c.Config,
		Flags: &APIFlags{
			Port:   c.Config.Server.Port,
			Host:   c.Config.Server.Host,
			Daemon: false, // Always foreground
		},
	}

	fmt.Println("Starting server in development mode (debug logging, foreground)")
	fmt.Println("Press Ctrl+C to stop")
	fmt.Println()

	return apiCmd.Execute()
}
