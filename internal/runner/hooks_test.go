package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Test Helpers
// =============================================================================

// makeExecutable creates a script file in dir with the given name and content.
func makeExecutable(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatalf("write executable %s: %v", name, err)
	}
	return path
}

// makeNonExecutable creates a non-executable file in dir.
func makeNonExecutable(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write non-executable %s: %v", name, err)
	}
	return path
}

// testEvent creates a types.Event for testing.
func testEvent(eventType string) types.Event {
	return types.Event{
		ID:        "evt_test123",
		Type:      eventType,
		Source:    types.EventSourceRunner,
		Timestamp: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		RunnerID:  "runner-1",
		ProjectID: "my-project",
		TaskID:    "abc12345",
		TaskPath:  "projects/my-project/task/abc12345.md",
		TaskTitle: "Fix the widget",
		FeatureID: "feature-alpha",
	}
}

// =============================================================================
// HookDispatcher Construction
// =============================================================================

func TestNewHookDispatcher_MissingDir(t *testing.T) {
	// Missing hooks directory should not be an error; just no hooks.
	hd, err := NewHookDispatcher("/nonexistent/path/hooks", 30*time.Second)
	if err != nil {
		t.Fatalf("expected no error for missing dir, got: %v", err)
	}
	if hd == nil {
		t.Fatal("expected non-nil HookDispatcher")
	}
	hooks := hd.ListHooks()
	if len(hooks) != 0 {
		t.Errorf("expected 0 hooks, got %d", len(hooks))
	}
}

func TestNewHookDispatcher_DiscoversExecutableScripts(t *testing.T) {
	dir := t.TempDir()
	makeExecutable(t, dir, "pre-task-start", "#!/bin/sh\nexit 0\n")
	makeExecutable(t, dir, "post-task-completed", "#!/bin/sh\nexit 0\n")
	makeNonExecutable(t, dir, "notes.txt", "not a hook")

	hd, err := NewHookDispatcher(dir, 30*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hooks := hd.ListHooks()
	if len(hooks) != 2 {
		t.Errorf("expected 2 hooks, got %d: %v", len(hooks), hooks)
	}
}

func TestNewHookDispatcher_SkipsSubdirectories(t *testing.T) {
	dir := t.TempDir()
	makeExecutable(t, dir, "pre-task-start", "#!/bin/sh\nexit 0\n")
	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatal(err)
	}

	hd, err := NewHookDispatcher(dir, 30*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hooks := hd.ListHooks()
	if len(hooks) != 1 {
		t.Errorf("expected 1 hook, got %d", len(hooks))
	}
}

// =============================================================================
// Event-to-Hook Filename Mapping
// =============================================================================

func TestEventToHookFilenames(t *testing.T) {
	tests := []struct {
		eventType string
		wantPre   string
		wantPost  string
	}{
		{"task.started", "pre-task-start", "post-task-start"},
		{"task.completed", "pre-task-complete", "post-task-complete"},
		{"task.failed", "pre-task-fail", "post-task-fail"},
		{"task.cancelled", "pre-task-cancel", "post-task-cancel"},
		{"task.blocked", "pre-task-block", "post-task-block"},
		{"runner.started", "pre-runner-start", "post-runner-start"},
		{"feature.enabled", "pre-feature-enable", "post-feature-enable"},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			pre, post := eventToHookFilenames(tt.eventType)
			if pre != tt.wantPre {
				t.Errorf("pre: got %q, want %q", pre, tt.wantPre)
			}
			if post != tt.wantPost {
				t.Errorf("post: got %q, want %q", post, tt.wantPost)
			}
		})
	}
}

// =============================================================================
// Environment Variables
// =============================================================================

func TestBuildHookEnv(t *testing.T) {
	evt := testEvent(types.EventTaskStarted)
	evt.FromStatus = "pending"
	evt.ToStatus = "in_progress"

	env := buildHookEnv(evt)
	envMap := envToMap(env)

	expected := map[string]string{
		"BRAIN_EVENT_TYPE":  "task.started",
		"BRAIN_PROJECT_ID":  "my-project",
		"BRAIN_RUNNER_ID":   "runner-1",
		"BRAIN_TASK_ID":     "abc12345",
		"BRAIN_TASK_TITLE":  "Fix the widget",
		"BRAIN_TASK_PATH":   "projects/my-project/task/abc12345.md",
		"BRAIN_FEATURE_ID":  "feature-alpha",
		"BRAIN_FROM_STATUS": "pending",
		"BRAIN_TO_STATUS":   "in_progress",
	}

	for k, want := range expected {
		got, ok := envMap[k]
		if !ok {
			t.Errorf("missing env var %s", k)
			continue
		}
		if got != want {
			t.Errorf("env var %s: got %q, want %q", k, got, want)
		}
	}
}

func envToMap(env []string) map[string]string {
	m := make(map[string]string)
	for _, entry := range env {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	return m
}

// =============================================================================
// Pre-Hook Execution (Blocking)
// =============================================================================

func TestDispatchPre_NoHooksIsNoop(t *testing.T) {
	hd, err := NewHookDispatcher(t.TempDir(), 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	evt := testEvent(types.EventTaskStarted)
	if err := hd.DispatchPre(evt); err != nil {
		t.Fatalf("expected no error for no matching hooks, got: %v", err)
	}
}

func TestDispatchPre_ExitZero_Proceeds(t *testing.T) {
	dir := t.TempDir()
	makeExecutable(t, dir, "pre-task-start", "#!/bin/sh\nexit 0\n")

	hd, err := NewHookDispatcher(dir, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	evt := testEvent(types.EventTaskStarted)
	if err := hd.DispatchPre(evt); err != nil {
		t.Fatalf("expected exit 0 to proceed, got error: %v", err)
	}
}

func TestDispatchPre_ExitOne_Aborts(t *testing.T) {
	dir := t.TempDir()
	makeExecutable(t, dir, "pre-task-start", "#!/bin/sh\nexit 1\n")

	hd, err := NewHookDispatcher(dir, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	evt := testEvent(types.EventTaskStarted)
	err = hd.DispatchPre(evt)
	if err == nil {
		t.Fatal("expected error for exit 1, got nil")
	}
	if !IsHookAbort(err) {
		t.Errorf("expected HookAbortError, got: %T %v", err, err)
	}
}

func TestDispatchPre_ExitTwo_BlocksWithReason(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
echo "dependency not ready" >&2
exit 2
`
	makeExecutable(t, dir, "pre-task-start", script)

	hd, err := NewHookDispatcher(dir, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	evt := testEvent(types.EventTaskStarted)
	err = hd.DispatchPre(evt)
	if err == nil {
		t.Fatal("expected error for exit 2, got nil")
	}
	hookErr, ok := AsHookBlockError(err)
	if !ok {
		t.Fatalf("expected HookBlockError, got: %T %v", err, err)
	}
	if !strings.Contains(hookErr.Reason, "dependency not ready") {
		t.Errorf("expected stderr in reason, got: %q", hookErr.Reason)
	}
}

func TestDispatchPre_ReceivesJSONOnStdin(t *testing.T) {
	dir := t.TempDir()
	// Script that reads stdin and writes it to a file for verification.
	outFile := filepath.Join(dir, "stdin_capture.json")
	script := `#!/bin/sh
cat > ` + outFile + `
exit 0
`
	makeExecutable(t, dir, "pre-task-start", script)

	hd, err := NewHookDispatcher(dir, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	evt := testEvent(types.EventTaskStarted)
	if err := hd.DispatchPre(evt); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read captured stdin: %v", err)
	}

	var captured types.Event
	if err := json.Unmarshal(data, &captured); err != nil {
		t.Fatalf("unmarshal captured event: %v", err)
	}

	if captured.Type != types.EventTaskStarted {
		t.Errorf("captured event type: got %q, want %q", captured.Type, types.EventTaskStarted)
	}
	if captured.TaskID != "abc12345" {
		t.Errorf("captured task ID: got %q, want %q", captured.TaskID, "abc12345")
	}
}

func TestDispatchPre_Timeout(t *testing.T) {
	dir := t.TempDir()
	// Use sleep with a long duration; timeout should fire quickly.
	makeExecutable(t, dir, "pre-task-start", "#!/bin/sh\nsleep 60\nexit 0\n")

	hd, err := NewHookDispatcher(dir, 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	evt := testEvent(types.EventTaskStarted)
	err = hd.DispatchPre(evt)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected timeout message, got: %v", err)
	}
}

// =============================================================================
// Post-Hook Execution (Fire-and-Forget)
// =============================================================================

func TestDispatchPost_NoHooksIsNoop(t *testing.T) {
	hd, err := NewHookDispatcher(t.TempDir(), 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	evt := testEvent(types.EventTaskCompleted)
	// Should not panic or block.
	hd.DispatchPost(evt)
}

func TestDispatchPost_ExecutesHookAsync(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "executed.txt")
	script := `#!/bin/sh
echo "done" > ` + marker + `
`
	makeExecutable(t, dir, "post-task-complete", script)

	hd, err := NewHookDispatcher(dir, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	evt := testEvent(types.EventTaskCompleted)
	hd.DispatchPost(evt)

	// Wait for async execution.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return // Success: the hook wrote the marker file.
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("post-hook did not execute within 5 seconds")
}

func TestDispatchPost_ErrorDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	makeExecutable(t, dir, "post-task-complete", "#!/bin/sh\nexit 1\n")

	hd, err := NewHookDispatcher(dir, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	evt := testEvent(types.EventTaskCompleted)
	// Should return immediately; error is logged, not returned.
	done := make(chan struct{})
	go func() {
		hd.DispatchPost(evt)
		close(done)
	}()

	select {
	case <-done:
		// Good: returned quickly.
	case <-time.After(2 * time.Second):
		t.Fatal("DispatchPost blocked for too long")
	}
}

// =============================================================================
// Non-Executable Files
// =============================================================================

func TestNonExecutableFilesSkipped(t *testing.T) {
	dir := t.TempDir()
	makeNonExecutable(t, dir, "pre-task-start", "#!/bin/sh\nexit 1\n")

	hd, err := NewHookDispatcher(dir, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// The non-executable hook should NOT be dispatched.
	evt := testEvent(types.EventTaskStarted)
	if err := hd.DispatchPre(evt); err != nil {
		t.Fatalf("expected non-executable to be skipped, got error: %v", err)
	}
}

// =============================================================================
// Integration: Multiple Hooks for Same Event
// =============================================================================

func TestDispatchPre_MultipleHooks_AllMustPass(t *testing.T) {
	// This tests that if there were multiple pre-hooks for the same event,
	// they'd all need to pass. But our naming convention means only one
	// pre- and one post- per event type. Still, test that the single match works.
	dir := t.TempDir()
	makeExecutable(t, dir, "pre-task-start", "#!/bin/sh\nexit 0\n")

	hd, err := NewHookDispatcher(dir, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	evt := testEvent(types.EventTaskStarted)
	if err := hd.DispatchPre(evt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// =============================================================================
// NewHookDispatcherWithConfig
// =============================================================================

func TestNewHookDispatcherWithConfig_InlineHooks(t *testing.T) {
	cfg := HooksConfig{
		HooksDir: t.TempDir(), // empty dir
		Hooks: map[string]InlineHookConfig{
			"pre-task-start": {
				Command: "echo hello",
			},
			"post-task-complete": {
				Command: "echo done",
			},
		},
	}

	hd, err := NewHookDispatcherWithConfig(cfg, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	hooks := hd.ListHooks()
	if len(hooks) != 2 {
		t.Errorf("expected 2 hooks, got %d: %v", len(hooks), hooks)
	}
}

func TestNewHookDispatcherWithConfig_DisabledHooksSkipped(t *testing.T) {
	disabled := false
	cfg := HooksConfig{
		HooksDir: t.TempDir(),
		Hooks: map[string]InlineHookConfig{
			"pre-task-start": {
				Command: "echo hello",
				Enabled: &disabled,
			},
		},
	}

	hd, err := NewHookDispatcherWithConfig(cfg, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	hooks := hd.ListHooks()
	if len(hooks) != 0 {
		t.Errorf("expected 0 hooks (disabled), got %d: %v", len(hooks), hooks)
	}
}

func TestNewHookDispatcherWithConfig_InlineOverridesDirectory(t *testing.T) {
	dir := t.TempDir()
	// Create a directory hook that exits 1 (would abort).
	makeExecutable(t, dir, "pre-task-start", "#!/bin/sh\nexit 1\n")

	// Inline hook with same name exits 0 (should succeed).
	cfg := HooksConfig{
		HooksDir: dir,
		Hooks: map[string]InlineHookConfig{
			"pre-task-start": {
				Command: "exit 0",
			},
		},
	}

	hd, err := NewHookDispatcherWithConfig(cfg, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Should use inline hook (exit 0), not directory hook (exit 1).
	evt := testEvent(types.EventTaskStarted)
	if err := hd.DispatchPre(evt); err != nil {
		t.Fatalf("expected inline hook to succeed, got error: %v", err)
	}
}

// =============================================================================
// Inline Hook Pre-Dispatch
// =============================================================================

func TestDispatchPre_InlineCommand_ExitZero(t *testing.T) {
	cfg := HooksConfig{
		HooksDir: t.TempDir(),
		Hooks: map[string]InlineHookConfig{
			"pre-task-start": {
				Command: "exit 0",
			},
		},
	}

	hd, err := NewHookDispatcherWithConfig(cfg, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	evt := testEvent(types.EventTaskStarted)
	if err := hd.DispatchPre(evt); err != nil {
		t.Fatalf("expected inline command to succeed, got: %v", err)
	}
}

func TestDispatchPre_InlineCommand_ExitOne_Aborts(t *testing.T) {
	cfg := HooksConfig{
		HooksDir: t.TempDir(),
		Hooks: map[string]InlineHookConfig{
			"pre-task-start": {
				Command: "exit 1",
			},
		},
	}

	hd, err := NewHookDispatcherWithConfig(cfg, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	evt := testEvent(types.EventTaskStarted)
	err = hd.DispatchPre(evt)
	if err == nil {
		t.Fatal("expected error for inline exit 1")
	}
	if !IsHookAbort(err) {
		t.Errorf("expected HookAbortError, got: %T %v", err, err)
	}
}

func TestDispatchPre_InlineScript_Executes(t *testing.T) {
	dir := t.TempDir()
	scriptPath := makeExecutable(t, dir, "my-check.sh", "#!/bin/sh\nexit 0\n")

	cfg := HooksConfig{
		HooksDir: t.TempDir(), // separate from script
		Hooks: map[string]InlineHookConfig{
			"pre-task-start": {
				Script: scriptPath,
			},
		},
	}

	hd, err := NewHookDispatcherWithConfig(cfg, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	evt := testEvent(types.EventTaskStarted)
	if err := hd.DispatchPre(evt); err != nil {
		t.Fatalf("expected inline script to succeed, got: %v", err)
	}
}

func TestDispatchPre_InlineCommand_Timeout(t *testing.T) {
	cfg := HooksConfig{
		HooksDir: t.TempDir(),
		Hooks: map[string]InlineHookConfig{
			"pre-task-start": {
				Command: "sleep 60",
				Timeout: Duration{500 * time.Millisecond},
			},
		},
	}

	hd, err := NewHookDispatcherWithConfig(cfg, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	evt := testEvent(types.EventTaskStarted)
	err = hd.DispatchPre(evt)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected timeout message, got: %v", err)
	}
}

// =============================================================================
// Inline Hook Post-Dispatch
// =============================================================================

func TestDispatchPost_InlineCommand_Executes(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "inline_post_ran.txt")
	cfg := HooksConfig{
		HooksDir: t.TempDir(),
		Hooks: map[string]InlineHookConfig{
			"post-task-complete": {
				Command: "echo done > " + marker,
			},
		},
	}

	hd, err := NewHookDispatcherWithConfig(cfg, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	evt := testEvent(types.EventTaskCompleted)
	hd.DispatchPost(evt)

	// Wait for async execution.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("inline post-hook did not execute within 5 seconds")
}

// =============================================================================
// InlineHookConfig Helpers
// =============================================================================

func TestInlineHookConfig_IsEnabled(t *testing.T) {
	// Default (nil) → true.
	h := InlineHookConfig{Command: "echo"}
	if !h.IsEnabled() {
		t.Error("expected nil Enabled to default to true")
	}

	// Explicit true.
	enabled := true
	h.Enabled = &enabled
	if !h.IsEnabled() {
		t.Error("expected explicit true")
	}

	// Explicit false.
	disabled := false
	h.Enabled = &disabled
	if h.IsEnabled() {
		t.Error("expected explicit false")
	}
}

func TestInlineHookConfig_IsBlocking(t *testing.T) {
	// Default (nil) → true.
	h := InlineHookConfig{Command: "echo"}
	if !h.IsBlocking() {
		t.Error("expected nil Blocking to default to true")
	}

	// Explicit false.
	blocking := false
	h.Blocking = &blocking
	if h.IsBlocking() {
		t.Error("expected explicit false")
	}
}

func TestInlineHookConfig_GetTimeout(t *testing.T) {
	// No timeout set → use default.
	h := InlineHookConfig{Command: "echo"}
	got := h.GetTimeout(30 * time.Second)
	if got != 30*time.Second {
		t.Errorf("expected 30s default, got %v", got)
	}

	// Timeout set → use configured.
	h.Timeout = Duration{10 * time.Second}
	got = h.GetTimeout(30 * time.Second)
	if got != 10*time.Second {
		t.Errorf("expected 10s configured, got %v", got)
	}
}
