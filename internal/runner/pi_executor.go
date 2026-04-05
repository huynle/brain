package runner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// PiExecutor
// =============================================================================

// PiExecutor implements the TaskExecutor interface for pi.dev.
// It spawns a Pi subprocess via PiRPCProcess, sends prompts over JSONL stdin,
// and reads events from JSONL stdout.
//
// Pi only supports headless mode — there is no TUI or dashboard mode.
type PiExecutor struct {
	config         RunnerConfig
	CommandFactory CommandFactory
}

// NewPiExecutor creates a new PiExecutor with the given configuration.
func NewPiExecutor(cfg RunnerConfig) *PiExecutor {
	return &PiExecutor{
		config: cfg,
		CommandFactory: func(name string, args ...string) *exec.Cmd {
			return exec.Command(name, args...)
		},
	}
}

// =============================================================================
// Prompt Building
// =============================================================================

// BuildPrompt builds the prompt string for a task executed by pi.dev.
// If the task has a direct_prompt, it is used verbatim.
// Otherwise, a standard prompt referencing the brain task is generated.
func (pe *PiExecutor) BuildPrompt(task *types.ResolvedTask, isResume bool) string {
	// Direct prompt bypass
	if task.DirectPrompt != "" {
		return task.DirectPrompt
	}

	if isResume {
		return fmt.Sprintf(`RESUME the interrupted task at brain path: %s

IMPORTANT: This task was previously in_progress but was interrupted.

Use brain_recall to read the task details, then:
1. Check the task file for any progress notes or partial work
2. Assess what work (if any) was already completed
3. If work was partially done, continue from where it left off
4. If unclear what was done, restart the task from the beginning
5. Create atomic git commit
6. Capture commit hash and mark as completed with summary

Start now.`, task.Path)
	}

	return fmt.Sprintf(`Process the task at brain path: %s

Use brain_recall to read the task details, then:
1. Mark the task as in_progress
2. Execute the task requirements
3. Run tests if applicable
4. Create atomic git commit
5. Capture commit hash and mark as completed with summary

Start now.`, task.Path)
}

// =============================================================================
// Workdir Resolution
// =============================================================================

// ResolveWorkdir resolves the working directory for a task.
// Reuses the same worktree/fallback logic as the OpenCode executor.
func (pe *PiExecutor) ResolveWorkdir(task *types.ResolvedTask) (string, error) {
	// Delegate to the shared worktree logic via a temporary Executor
	// to avoid duplicating the complex worktree resolution code.
	sharedExecutor := &Executor{
		config:         pe.config,
		CommandFactory: pe.CommandFactory,
	}
	return sharedExecutor.ResolveWorkdir(task)
}

// =============================================================================
// Spawning
// =============================================================================

// Spawn creates a PiRPCProcess, sends the prompt via JSONL, and returns a SpawnResult.
// Pi only supports headless mode; Mode in SpawnOptions is ignored.
func (pe *PiExecutor) Spawn(ctx context.Context, task *types.ResolvedTask, projectID string, opts SpawnOptions) (*SpawnResult, error) {
	// Ensure state directory exists
	if err := os.MkdirAll(pe.config.StateDir, 0o755); err != nil {
		return nil, fmt.Errorf("ensure state dir: %w", err)
	}

	// Build and save prompt
	prompt := pe.BuildPrompt(task, opts.IsResume)
	promptFile := filepath.Join(pe.config.StateDir, fmt.Sprintf("prompt_%s_%s.txt", projectID, task.ID))
	if err := os.WriteFile(promptFile, []byte(prompt), 0o644); err != nil {
		return nil, fmt.Errorf("write prompt file: %w", err)
	}

	// Resolve workdir
	workdir := opts.Workdir
	if workdir == "" {
		var err error
		workdir, err = pe.ResolveWorkdir(task)
		if err != nil {
			return nil, fmt.Errorf("resolve workdir: %w", err)
		}
	}

	// Build command args for pi
	args := pe.buildArgs(task, opts.RuntimeDefaultModel)

	// Create the pi command
	cmd := pe.CommandFactory(pe.config.Pi.Bin, args...)
	cmd.Dir = workdir

	// Create output log file for capturing stderr
	outputFile := filepath.Join(pe.config.StateDir, fmt.Sprintf("output_%s_%s.log", projectID, task.ID))
	logFile, err := os.Create(outputFile)
	if err != nil {
		return nil, fmt.Errorf("create output log: %w", err)
	}

	// Pi's stderr goes to log file (stdout is JSONL protocol)
	cmd.Stderr = logFile

	// Create PiRPCProcess which manages stdin/stdout JSONL communication
	proc, err := NewPiRPCProcess(cmd)
	if err != nil {
		logFile.Close()
		return nil, fmt.Errorf("start pi process: %w", err)
	}

	// Close log file when the process exits
	go func() {
		<-proc.Done()
		logFile.Close()
	}()

	// Send the prompt to the pi process
	if err := proc.SendPrompt(prompt); err != nil {
		slog.Warn("pi_executor: failed to send prompt, killing process",
			"error", err,
			"taskId", task.ID,
		)
		_ = proc.Kill(nil)
		return nil, fmt.Errorf("send prompt to pi: %w", err)
	}

	return &SpawnResult{
		PID:        proc.PID(),
		Proc:       proc,
		PromptFile: promptFile,
		Workdir:    workdir,
	}, nil
}

// buildArgs constructs the command-line arguments for the pi binary.
func (pe *PiExecutor) buildArgs(task *types.ResolvedTask, runtimeDefaultModel string) []string {
	var args []string

	// Model resolution: task > runtime default > config
	model := pe.GetEffectiveModel(task, runtimeDefaultModel)
	if model != "" {
		args = append(args, "--model", model)
	}

	return args
}

// GetEffectiveModel returns the effective model for a task.
// Precedence: task.Model > runtimeDefaultModel > config.Pi.Model
func (pe *PiExecutor) GetEffectiveModel(task *types.ResolvedTask, runtimeDefaultModel string) string {
	if task.Model != "" {
		return task.Model
	}
	if runtimeDefaultModel != "" {
		return runtimeDefaultModel
	}
	return pe.config.Pi.Model
}

// =============================================================================
// Cleanup
// =============================================================================

// Cleanup removes temporary files for a task.
func (pe *PiExecutor) Cleanup(taskID, projectID string) error {
	files := []string{
		filepath.Join(pe.config.StateDir, fmt.Sprintf("prompt_%s_%s.txt", projectID, taskID)),
		filepath.Join(pe.config.StateDir, fmt.Sprintf("output_%s_%s.log", projectID, taskID)),
	}

	for _, f := range files {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", f, err)
		}
	}

	return nil
}
