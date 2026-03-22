package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// ExecuteTask manually executes a task from the TUI ("x" key).
// For in_progress tasks (orphaned from a previous session), it skips
// claiming and status update and directly spawns a resume session.
// For other tasks, it delegates to claimAndSpawn for the full pipeline.
func (tr *TaskRunner) ExecuteTask(ctx context.Context, task *types.ResolvedTask, projectID string) error {
	// Check capacity
	maxParallel := tr.config.MaxParallel
	if maxParallel <= 0 {
		maxParallel = 2 // default
	}
	running := tr.processMgr.RunningCount()
	if running >= maxParallel {
		return fmt.Errorf("at capacity: %d/%d slots in use", running, maxParallel)
	}

	// For in_progress tasks: skip claim and status update, just spawn directly.
	// This handles the "resume after restart" case where the task was left
	// in_progress from a previous runner session.
	if task.Status == "in_progress" {
		return tr.resumeTask(ctx, task, projectID)
	}

	// For other statuses: full claim-and-spawn pipeline
	return tr.claimAndSpawn(ctx, task, projectID)
}

// resumeTask spawns an executor for an already in_progress task without
// re-claiming or updating status. Used when resuming orphaned tasks.
func (tr *TaskRunner) resumeTask(ctx context.Context, task *types.ResolvedTask, projectID string) error {
	// Resolve workdir (may create git worktree)
	workdir, err := tr.executor.ResolveWorkdir(task)
	if err != nil {
		return fmt.Errorf("resolve workdir: %w", err)
	}

	spawnOpts := SpawnOptions{
		Mode:     tr.mode,
		Workdir:  workdir,
		IsResume: true,
	}

	spawnResult, err := tr.executor.Spawn(ctx, task, projectID, spawnOpts)
	if err != nil {
		return fmt.Errorf("spawn task: %w", err)
	}

	// Build running task record
	runningTask := RunningTask{
		ID:             task.ID,
		Path:           task.Path,
		Title:          task.Title,
		Priority:       task.Priority,
		ProjectID:      projectID,
		PID:            spawnResult.PID,
		PaneID:         spawnResult.PaneID,
		WindowName:     spawnResult.WindowName,
		StartedAt:      time.Now(),
		Workdir:        spawnResult.Workdir,
		CompleteOnIdle: resolveCompleteOnIdle(task.CompleteOnIdle, task.DirectPrompt),
	}

	// Track in process manager
	if spawnResult.Proc != nil {
		if err := tr.processMgr.Add(task.ID, runningTask, spawnResult.Proc); err != nil {
			return fmt.Errorf("track process: %w", err)
		}
	}

	// Emit event
	tr.emitEvent(RunnerEvent{
		Type: EventTaskStarted,
		Task: &runningTask,
	})

	// Discover opencode session ID in background and save to task entry
	go tr.discoverAndSaveSession(task.Path, spawnResult.PID)

	return nil
}
