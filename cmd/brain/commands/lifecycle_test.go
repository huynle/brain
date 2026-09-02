package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/huynle/brain-api/internal/apiserver"
	"github.com/huynle/brain-api/internal/lifecycle"
	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/runnercli"
)

func TestEmbeddedRunnerAPIURLDerivation(t *testing.T) {
	tests := []struct {
		name        string
		host        string
		port        int
		tlsEnabled  bool
		existingURL string
		want        string
	}{
		{name: "wildcard IPv4 maps to localhost", host: "0.0.0.0", port: 3333, want: "http://localhost:3333"},
		{name: "empty host maps to localhost", host: "", port: 4444, want: "http://localhost:4444"},
		{name: "wildcard IPv6 maps to localhost", host: "::", port: 3333, want: "http://localhost:3333"},
		{name: "bracketed wildcard IPv6 maps to localhost", host: "[::]", port: 3333, want: "http://localhost:3333"},
		{name: "custom port derives URL", host: "127.0.0.1", port: 4444, want: "http://127.0.0.1:4444"},
		{name: "TLS uses https", host: "localhost", port: 3333, tlsEnabled: true, want: "https://localhost:3333"},
		{name: "explicit public URL is preserved", host: "0.0.0.0", port: 3333, existingURL: "https://brain.example.com", want: "https://brain.example.com"},
		{name: "default localhost URL is overridden for effective port", host: "0.0.0.0", port: 4444, existingURL: "http://localhost:3333", want: "http://localhost:4444"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &UnifiedConfig{}
			cfg.Server.TLS.Enabled = tt.tlsEnabled
			cfg.Runner.BrainAPIURL = tt.existingURL

			got := embeddedRunnerAPIURL(cfg, apiserver.ServerOptions{Host: tt.host, Port: tt.port})
			if got != tt.want {
				t.Fatalf("embeddedRunnerAPIURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStartCommandDryRunMentionsEmbeddedRunner(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &UnifiedConfig{}
	cfg.Server.PIDFile = filepath.Join(tmpDir, "brain-api.pid")
	cfg.Server.LogFile = filepath.Join(tmpDir, "brain-api.log")
	cfg.Server.Port = 4444
	cfg.Server.Host = "0.0.0.0"

	cmd := &StartCommand{
		Config: cfg,
		Flags: &LifecycleFlags{
			DryRun:        true,
			Runner:        true,
			RunnerProject: "personal-productivity",
		},
	}

	output := captureStdout(t, func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !contains(output, "embedded runner") {
		t.Fatalf("expected dry-run output to mention embedded runner, got %q", output)
	}
	if !contains(output, "personal-productivity") {
		t.Fatalf("expected dry-run output to mention runner project, got %q", output)
	}
	if !contains(output, "http://localhost:4444") {
		t.Fatalf("expected dry-run output to include derived runner API URL, got %q", output)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("failed to read stdout: %v", err)
	}
	return buf.String()
}

func TestStartCommandDaemonArgsIncludeEmbeddedRunnerFlags(t *testing.T) {
	cfg := &UnifiedConfig{}
	cfg.Server.Port = 4444
	cfg.Server.Host = "0.0.0.0"

	cmd := &StartCommand{
		Config: cfg,
		Flags: &LifecycleFlags{
			Runner:        true,
			RunnerProject: "personal-productivity",
			MaxParallel:   4,
			Include:       []string{"prod-*", "brain-*"},
			Exclude:       []string{"test-*"},
			Executor:      "pi",
		},
	}

	args := cmd.daemonArgs("/tmp/brain.log")
	wantContains := []string{
		"api", "--daemon", "--log-file", "/tmp/brain.log",
		"--port", "4444", "--host", "0.0.0.0",
		"--runner", "--runner-project", "personal-productivity",
		"--max-parallel", "4", "--include", "prod-*", "--include", "brain-*",
		"--exclude", "test-*", "--executor", "pi",
	}

	for _, want := range wantContains {
		if !stringSliceContains(args, want) {
			t.Fatalf("daemon args %v do not contain %q", args, want)
		}
	}
}

func TestEmbeddedRunnerConfigAppliesRunnerFlagsAndDerivedURL(t *testing.T) {
	cfg := &UnifiedConfig{}
	cfg.Runner.BrainAPIURL = "http://localhost:3333"
	cfg.Runner.MaxParallel = 2
	cfg.Runner.IncludeProjects = []string{"existing-*"}
	cfg.Server.Port = 4444
	cfg.Server.Host = "0.0.0.0"

	flags := embeddedRunnerFlags{
		RunnerProject: "personal-productivity",
		MaxParallel:   5,
		Include:       []string{"prod-*"},
		Exclude:       []string{"test-*"},
		Executor:      "pi",
	}

	project, runnerCfg := embeddedRunnerConfig(cfg, apiserver.ServerOptions{Host: "0.0.0.0", Port: 4444}, flags)

	if project != "personal-productivity" {
		t.Fatalf("project = %q, want personal-productivity", project)
	}
	if runnerCfg.BrainAPIURL != "http://localhost:4444" {
		t.Fatalf("BrainAPIURL = %q, want http://localhost:4444", runnerCfg.BrainAPIURL)
	}
	if runnerCfg.MaxParallel != 5 {
		t.Fatalf("MaxParallel = %d, want 5", runnerCfg.MaxParallel)
	}
	if runnerCfg.DefaultExecutor != "pi" {
		t.Fatalf("DefaultExecutor = %q, want pi", runnerCfg.DefaultExecutor)
	}
	if !stringSliceContains(runnerCfg.IncludeProjects, "existing-*") || !stringSliceContains(runnerCfg.IncludeProjects, "prod-*") {
		t.Fatalf("IncludeProjects = %v, want existing-* and prod-*", runnerCfg.IncludeProjects)
	}
	if !stringSliceContains(runnerCfg.ExcludeProjects, "test-*") {
		t.Fatalf("ExcludeProjects = %v, want test-*", runnerCfg.ExcludeProjects)
	}
}

func stringSliceContains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestRunServerWithOptionalRunnerDisabledUsesAPIServerOnly(t *testing.T) {
	oldServer := runAPIServer
	oldRunner := runEmbeddedTaskRunner
	oldWait := waitForEmbeddedRunnerAPI
	t.Cleanup(func() {
		runAPIServer = oldServer
		runEmbeddedTaskRunner = oldRunner
		waitForEmbeddedRunnerAPI = oldWait
	})

	serverCalls := 0
	runnerCalls := 0
	waitCalls := 0
	runAPIServer = func(ctx context.Context, opts apiserver.ServerOptions) error {
		serverCalls++
		return nil
	}
	runEmbeddedTaskRunner = func(ctx context.Context, opts runnercli.RunnerOptions) error {
		runnerCalls++
		return nil
	}
	waitForEmbeddedRunnerAPI = func(ctx context.Context, apiURL string) error {
		waitCalls++
		return nil
	}

	err := runServerWithOptionalRunner(context.Background(), &UnifiedConfig{}, apiserver.ServerOptions{Host: "localhost", Port: 3333}, LifecycleFlags{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if serverCalls != 1 || runnerCalls != 0 || waitCalls != 0 {
		t.Fatalf("calls server/runner/wait = %d/%d/%d, want 1/0/0", serverCalls, runnerCalls, waitCalls)
	}
}

func TestRunServerWithOptionalRunnerResolvesAllProjectBeforeStartingRunner(t *testing.T) {
	oldServer := runAPIServer
	oldRunner := runEmbeddedTaskRunner
	oldWait := waitForEmbeddedRunnerAPI
	oldResolve := resolveEmbeddedRunnerProjects
	t.Cleanup(func() {
		runAPIServer = oldServer
		runEmbeddedTaskRunner = oldRunner
		waitForEmbeddedRunnerAPI = oldWait
		resolveEmbeddedRunnerProjects = oldResolve
	})

	runAPIServer = func(ctx context.Context, opts apiserver.ServerOptions) error {
		<-ctx.Done()
		return nil
	}
	waitForEmbeddedRunnerAPI = func(ctx context.Context, apiURL string) error { return nil }
	resolveEmbeddedRunnerProjects = func(project string, cfg runner.RunnerConfig) ([]string, error) {
		if project != "all" {
			t.Fatalf("project = %q, want all", project)
		}
		return []string{"brain", "notes"}, nil
	}
	runEmbeddedTaskRunner = func(ctx context.Context, opts runnercli.RunnerOptions) error {
		if len(opts.Projects) != 2 || opts.Projects[0] != "brain" || opts.Projects[1] != "notes" {
			t.Fatalf("runner projects = %v, want [brain notes]", opts.Projects)
		}
		return nil
	}

	err := runServerWithOptionalRunner(context.Background(), &UnifiedConfig{}, apiserver.ServerOptions{Host: "localhost", Port: 3333}, LifecycleFlags{Runner: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunServerWithOptionalRunnerStartsRunnerWithDerivedConfig(t *testing.T) {
	oldServer := runAPIServer
	oldRunner := runEmbeddedTaskRunner
	oldWait := waitForEmbeddedRunnerAPI
	t.Cleanup(func() {
		runAPIServer = oldServer
		runEmbeddedTaskRunner = oldRunner
		waitForEmbeddedRunnerAPI = oldWait
	})

	serverStarted := make(chan struct{})
	runnerStarted := make(chan runnercli.RunnerOptions, 1)
	runAPIServer = func(ctx context.Context, opts apiserver.ServerOptions) error {
		close(serverStarted)
		<-ctx.Done()
		return nil
	}
	waitForEmbeddedRunnerAPI = func(ctx context.Context, apiURL string) error {
		if apiURL != "http://localhost:4444" {
			t.Fatalf("wait apiURL = %q, want http://localhost:4444", apiURL)
		}
		return nil
	}
	runEmbeddedTaskRunner = func(ctx context.Context, opts runnercli.RunnerOptions) error {
		runnerStarted <- opts
		return nil
	}

	cfg := &UnifiedConfig{}
	cfg.Runner.BrainAPIURL = "http://localhost:3333"

	err := runServerWithOptionalRunner(context.Background(), cfg, apiserver.ServerOptions{Host: "0.0.0.0", Port: 4444}, LifecycleFlags{
		Runner:        true,
		RunnerProject: "personal-productivity",
		MaxParallel:   7,
		Executor:      "pi",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	<-serverStarted
	got := <-runnerStarted
	if len(got.Projects) != 1 || got.Projects[0] != "personal-productivity" {
		t.Fatalf("runner projects = %v, want [personal-productivity]", got.Projects)
	}
	if got.Mode != "headless" {
		t.Fatalf("runner mode = %q, want headless", got.Mode)
	}
	if got.Config.BrainAPIURL != "http://localhost:4444" {
		t.Fatalf("runner API URL = %q, want http://localhost:4444", got.Config.BrainAPIURL)
	}
	if got.Config.MaxParallel != 7 {
		t.Fatalf("runner max parallel = %d, want 7", got.Config.MaxParallel)
	}
	if got.Config.DefaultExecutor != "pi" {
		t.Fatalf("runner executor = %q, want pi", got.Config.DefaultExecutor)
	}
}

func TestStartCommandForegroundWithRunnerUsesCombinedLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &UnifiedConfig{}
	cfg.Server.PIDFile = filepath.Join(tmpDir, "brain-api.pid")
	cfg.Server.Port = 4444
	cfg.Server.Host = "0.0.0.0"
	cfg.Runner.BrainAPIURL = "http://localhost:3333"

	oldServer := runAPIServer
	oldRunner := runEmbeddedTaskRunner
	oldWait := waitForEmbeddedRunnerAPI
	oldResolve := resolveEmbeddedRunnerProjects
	t.Cleanup(func() {
		runAPIServer = oldServer
		runEmbeddedTaskRunner = oldRunner
		waitForEmbeddedRunnerAPI = oldWait
		resolveEmbeddedRunnerProjects = oldResolve
	})

	runAPIServer = func(ctx context.Context, opts apiserver.ServerOptions) error {
		<-ctx.Done()
		return nil
	}
	waitForEmbeddedRunnerAPI = func(ctx context.Context, apiURL string) error { return nil }
	resolveEmbeddedRunnerProjects = func(project string, cfg runner.RunnerConfig) ([]string, error) {
		return []string{project}, nil
	}
	runnerCalled := false
	runEmbeddedTaskRunner = func(ctx context.Context, opts runnercli.RunnerOptions) error {
		runnerCalled = true
		if opts.Config.BrainAPIURL != "http://localhost:4444" {
			t.Fatalf("runner URL = %q, want http://localhost:4444", opts.Config.BrainAPIURL)
		}
		return nil
	}

	cmd := &StartCommand{
		Config: cfg,
		Flags:  &LifecycleFlags{Runner: true, RunnerProject: "brain"},
	}
	if err := cmd.startForeground(cfg.Server.PIDFile, filepath.Join(tmpDir, "brain-api.log")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !runnerCalled {
		t.Fatal("expected embedded runner to be started")
	}
}

func TestStartCommand_AlreadyRunning(t *testing.T) {
	// Setup: Create a PID file with running process
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	// Write our own PID (we know it's running)
	if err := lifecycle.WritePID(pidFile, os.Getpid()); err != nil {
		t.Fatalf("Failed to write PID file: %v", err)
	}

	cfg := &UnifiedConfig{}
	cfg.Server.PIDFile = pidFile
	cfg.Server.Port = 3333

	cmd := &StartCommand{
		Config: cfg,
		Flags:  &LifecycleFlags{},
	}

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Expected error when server already running, got nil")
	}

	expectedMsg := "already running"
	if !contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error to contain %q, got %q", expectedMsg, err.Error())
	}
}

// TestStopCommand_NotRunning tests that stop handles gracefully when no server is running.
func TestStopCommand_NotRunning(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	cfg := &UnifiedConfig{}
	cfg.Server.PIDFile = pidFile

	cmd := &StopCommand{
		Config: cfg,
		Flags:  &LifecycleFlags{},
	}

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Expected error when no server running, got nil")
	}

	expectedMsg := "not running"
	if !contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error to contain %q, got %q", expectedMsg, err.Error())
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestStatusCommand_Stopped tests status command when server is stopped.
func TestStatusCommand_Stopped(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	cfg := &UnifiedConfig{}
	cfg.Server.PIDFile = pidFile
	cfg.Server.Port = 3333

	var buf bytes.Buffer
	cmd := &StatusCommand{
		Config: cfg,
		Flags:  &StatusFlags{},
		Out:    &buf,
	}

	err := cmd.Execute()
	// Exit code 1 means not running - command should return error
	if err == nil {
		t.Fatal("Expected error exit code when server stopped")
	}

	output := buf.String()
	if !contains(output, "stopped") {
		t.Errorf("Expected output to contain 'stopped', got: %s", output)
	}
}

// TestStatusCommand_Running tests status command when server is running.
func TestStatusCommand_Running(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	// Write current PID
	if err := lifecycle.WritePID(pidFile, os.Getpid()); err != nil {
		t.Fatalf("Failed to write PID: %v", err)
	}

	cfg := &UnifiedConfig{}
	cfg.Server.PIDFile = pidFile
	cfg.Server.Port = 3333

	var buf bytes.Buffer
	cmd := &StatusCommand{
		Config: cfg,
		Flags:  &StatusFlags{},
		Out:    &buf,
	}

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Expected no error when server running, got: %v", err)
	}

	output := buf.String()
	if !contains(output, "running") {
		t.Errorf("Expected output to contain 'running', got: %s", output)
	}
	if !contains(output, "PID") {
		t.Errorf("Expected output to contain 'PID', got: %s", output)
	}
}

// TestStatusCommand_JSON tests JSON output format.
func TestStatusCommand_JSON(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	// Write current PID
	if err := lifecycle.WritePID(pidFile, os.Getpid()); err != nil {
		t.Fatalf("Failed to write PID: %v", err)
	}

	cfg := &UnifiedConfig{}
	cfg.Server.PIDFile = pidFile
	cfg.Server.Port = 3333

	var buf bytes.Buffer
	cmd := &StatusCommand{
		Config: cfg,
		Flags:  &StatusFlags{JSON: true},
		Out:    &buf,
	}

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Parse JSON output
	var result map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	if result["status"] != "running" {
		t.Errorf("Expected status 'running', got: %v", result["status"])
	}
}

// TestStatusCommand_Crashed tests status when PID file exists but process is dead.
func TestStatusCommand_Crashed(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	// Write a non-existent PID
	if err := os.WriteFile(pidFile, []byte("999999\n"), 0644); err != nil {
		t.Fatalf("Failed to write PID: %v", err)
	}

	cfg := &UnifiedConfig{}
	cfg.Server.PIDFile = pidFile
	cfg.Server.Port = 3333

	var buf bytes.Buffer
	cmd := &StatusCommand{
		Config: cfg,
		Flags:  &StatusFlags{},
		Out:    &buf,
	}

	err := cmd.Execute()
	// Exit code 1 for crashed state
	if err == nil {
		t.Fatal("Expected error exit code when server crashed")
	}

	output := buf.String()
	if !contains(output, "crashed") {
		t.Errorf("Expected output to contain 'crashed', got: %s", output)
	}
}

func TestEmbeddedRunnerConfig_RunnerName(t *testing.T) {
	cfg := &UnifiedConfig{}
	opts := apiserver.ServerOptions{Port: 3333, Host: "localhost"}

	// Unnamed: the embedded runner keeps whatever the config said (empty means
	// the default runner).
	_, runnerCfg := embeddedRunnerConfig(cfg, opts, embeddedRunnerFlags{})
	if runnerCfg.Name != "" {
		t.Errorf("Name = %q, want empty", runnerCfg.Name)
	}

	// Named: needed so an embedded runner and a standalone `brain runner start`
	// on the same host don't share a state dir, and therefore a runner id.
	_, runnerCfg = embeddedRunnerConfig(cfg, opts, embeddedRunnerFlags{RunnerName: "embedded"})
	if runnerCfg.Name != "embedded" {
		t.Errorf("Name = %q, want embedded", runnerCfg.Name)
	}
}
