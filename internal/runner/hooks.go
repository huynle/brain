package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Error Types
// =============================================================================

// HookAbortError is returned when a pre-hook exits with code 1 (abort action).
type HookAbortError struct {
	Hook string
}

func (e *HookAbortError) Error() string {
	return fmt.Sprintf("hook %q aborted action (exit 1)", e.Hook)
}

// IsHookAbort returns true if err is a HookAbortError.
func IsHookAbort(err error) bool {
	var he *HookAbortError
	return errors.As(err, &he)
}

// HookBlockError is returned when a pre-hook exits with code 2 (block task).
type HookBlockError struct {
	Hook   string
	Reason string
}

func (e *HookBlockError) Error() string {
	return fmt.Sprintf("hook %q blocked task (exit 2): %s", e.Hook, e.Reason)
}

// AsHookBlockError extracts a HookBlockError from err, returning it and true
// if found, or nil and false otherwise.
func AsHookBlockError(err error) (*HookBlockError, bool) {
	var he *HookBlockError
	if errors.As(err, &he) {
		return he, true
	}
	return nil, false
}

// =============================================================================
// HookDispatcher
// =============================================================================

// HookDispatcher discovers hook scripts from a directory and executes them
// when events occur. Pre-hooks (blocking) run synchronously with timeout;
// post-hooks fire-and-forget in goroutines.
type HookDispatcher struct {
	// hooksDir is the directory containing hook scripts.
	hooksDir string
	// timeout is the maximum duration for pre-hook execution.
	timeout time.Duration
	// hooks maps filenames to their full paths (only executable files).
	hooks map[string]string
}

// NewHookDispatcher creates a HookDispatcher that discovers executable scripts
// from hooksDir. If hooksDir does not exist, the dispatcher is created with
// no hooks (not an error). The timeout applies to pre-hook execution.
func NewHookDispatcher(hooksDir string, timeout time.Duration) (*HookDispatcher, error) {
	hd := &HookDispatcher{
		hooksDir: hooksDir,
		timeout:  timeout,
		hooks:    make(map[string]string),
	}

	if err := hd.scan(); err != nil {
		return nil, err
	}
	return hd, nil
}

// scan discovers executable scripts in the hooks directory.
func (hd *HookDispatcher) scan() error {
	entries, err := os.ReadDir(hd.hooksDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			slog.Debug("hooks directory does not exist, no hooks loaded", "dir", hd.hooksDir)
			return nil
		}
		return fmt.Errorf("read hooks directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		path := filepath.Join(hd.hooksDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			slog.Warn("cannot stat hook file", "path", path, "error", err)
			continue
		}

		// Check if file is executable (any execute bit set).
		if info.Mode()&0111 == 0 {
			slog.Warn("skipping non-executable file in hooks directory", "path", path)
			continue
		}

		hd.hooks[entry.Name()] = path
		slog.Debug("discovered hook", "name", entry.Name(), "path", path)
	}

	slog.Info("hook dispatcher initialized", "hooks_dir", hd.hooksDir, "hooks_count", len(hd.hooks))
	return nil
}

// ListHooks returns the names of all discovered hooks.
func (hd *HookDispatcher) ListHooks() []string {
	names := make([]string, 0, len(hd.hooks))
	for name := range hd.hooks {
		names = append(names, name)
	}
	return names
}

// =============================================================================
// Event-to-Filename Mapping
// =============================================================================

// eventToHookFilenames maps a namespaced event type (e.g., "task.started")
// to the expected pre- and post-hook filenames.
//
// Convention:
//
//	"task.started"   → "pre-task-start",   "post-task-start"
//	"task.completed" → "pre-task-complete", "post-task-complete"
//
// The mapping strips common verb suffixes (-ed, -d) from the last segment
// and replaces the dot separator with a dash.
func eventToHookFilenames(eventType string) (pre, post string) {
	parts := strings.SplitN(eventType, ".", 2)
	if len(parts) != 2 {
		// Fallback for non-namespaced events.
		base := stripVerbSuffix(eventType)
		return "pre-" + base, "post-" + base
	}

	namespace := parts[0]
	action := stripVerbSuffix(parts[1])
	base := namespace + "-" + action
	return "pre-" + base, "post-" + base
}

// verbStemMap maps past-tense verbs used in event types to their stems.
// This explicit mapping avoids fragile suffix-stripping heuristics.
var verbStemMap = map[string]string{
	"started":    "start",
	"stopped":    "stop",
	"completed":  "complete",
	"failed":     "fail",
	"cancelled":  "cancel",
	"claimed":    "claim",
	"rejected":   "reject",
	"released":   "release",
	"changed":    "change",
	"detected":   "detect",
	"enabled":    "enable",
	"disabled":   "disable",
	"paused":     "pause",
	"resumed":    "resume",
	"blocked":    "block",
	"created":    "create",
	"updated":    "update",
	"deleted":    "delete",
	"discovered": "discover",
	"saved":      "save",
}

// stripVerbSuffix converts a past-tense action to its base form using
// an explicit mapping table.
//
//	"started"   → "start"
//	"completed" → "complete"
//	"failed"    → "fail"
//	"cancelled" → "cancel"
func stripVerbSuffix(s string) string {
	if stem, ok := verbStemMap[s]; ok {
		return stem
	}
	// Fallback: return as-is for unknown verbs.
	return s
}

// =============================================================================
// Environment Variables
// =============================================================================

// buildHookEnv constructs the environment variables for hook execution.
// Variables are prefixed with BRAIN_ to avoid collisions.
func buildHookEnv(evt types.Event) []string {
	env := os.Environ()
	env = append(env,
		"BRAIN_EVENT_TYPE="+evt.Type,
		"BRAIN_PROJECT_ID="+evt.ProjectID,
		"BRAIN_RUNNER_ID="+evt.RunnerID,
		"BRAIN_TASK_ID="+evt.TaskID,
		"BRAIN_TASK_TITLE="+evt.TaskTitle,
		"BRAIN_TASK_PATH="+evt.TaskPath,
		"BRAIN_FEATURE_ID="+evt.FeatureID,
		"BRAIN_FROM_STATUS="+evt.FromStatus,
		"BRAIN_TO_STATUS="+evt.ToStatus,
	)
	return env
}

// =============================================================================
// Dispatch Methods
// =============================================================================

// DispatchPre executes all matching pre-hooks synchronously with timeout.
// Returns nil if no hooks match or all hooks exit 0.
// Returns HookAbortError if a hook exits with code 1.
// Returns HookBlockError if a hook exits with code 2 (stderr captured as reason).
// Returns an error if a hook times out or encounters an unexpected error.
func (hd *HookDispatcher) DispatchPre(evt types.Event) error {
	preName, _ := eventToHookFilenames(evt.Type)
	hookPath, ok := hd.hooks[preName]
	if !ok {
		return nil
	}

	slog.Info("executing pre-hook", "hook", preName, "event", evt.Type)
	return hd.executePreHook(hookPath, preName, evt)
}

// DispatchPost executes all matching post-hooks asynchronously (fire-and-forget).
// Errors are logged but not returned.
func (hd *HookDispatcher) DispatchPost(evt types.Event) {
	_, postName := eventToHookFilenames(evt.Type)
	hookPath, ok := hd.hooks[postName]
	if !ok {
		return
	}

	slog.Info("dispatching post-hook", "hook", postName, "event", evt.Type)
	go func() {
		if err := hd.executePostHook(hookPath, postName, evt); err != nil {
			slog.Error("post-hook failed", "hook", postName, "event", evt.Type, "error", err)
		}
	}()
}

// =============================================================================
// Hook Execution
// =============================================================================

// executePreHook runs a pre-hook script with timeout and interprets exit codes.
func (hd *HookDispatcher) executePreHook(hookPath, hookName string, evt types.Event) error {
	ctx, cancel := context.WithTimeout(context.Background(), hd.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, hookPath)
	cmd.Env = buildHookEnv(evt)
	// Kill the entire process group so child processes (e.g., sleep) are also killed.
	cmd.Cancel = func() error {
		return cmd.Process.Kill()
	}
	cmd.WaitDelay = 500 * time.Millisecond

	// Pipe event JSON to stdin.
	eventJSON, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshal event for hook stdin: %w", err)
	}
	cmd.Stdin = bytes.NewReader(eventJSON)

	// Capture stderr for block reason.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		// Check for timeout.
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("pre-hook %q timed out after %v", hookName, hd.timeout)
		}

		// Check exit code.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code := exitErr.ExitCode()
			switch code {
			case 1:
				return &HookAbortError{Hook: hookName}
			case 2:
				reason := strings.TrimSpace(stderr.String())
				if reason == "" {
					reason = "hook exited with code 2 (no stderr)"
				}
				return &HookBlockError{Hook: hookName, Reason: reason}
			default:
				return fmt.Errorf("pre-hook %q exited with code %d", hookName, code)
			}
		}
		return fmt.Errorf("pre-hook %q execution error: %w", hookName, err)
	}

	slog.Info("pre-hook succeeded", "hook", hookName)
	return nil
}

// executePostHook runs a post-hook script (no exit code interpretation).
func (hd *HookDispatcher) executePostHook(hookPath, hookName string, evt types.Event) error {
	ctx, cancel := context.WithTimeout(context.Background(), hd.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, hookPath)
	cmd.Env = buildHookEnv(evt)
	cmd.Cancel = func() error {
		return cmd.Process.Kill()
	}
	cmd.WaitDelay = 500 * time.Millisecond

	// Pipe event JSON to stdin.
	eventJSON, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshal event for hook stdin: %w", err)
	}
	cmd.Stdin = bytes.NewReader(eventJSON)

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("post-hook %q timed out after %v", hookName, hd.timeout)
		}
		return fmt.Errorf("post-hook %q failed: %w", hookName, err)
	}

	slog.Info("post-hook succeeded", "hook", hookName)
	return nil
}
