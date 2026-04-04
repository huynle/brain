package runner

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// ExecuteTask manually executes a task from the TUI ("x" key).
// For in_progress tasks (orphaned from a previous session), it skips
// claiming and status update and directly spawns a resume session.
// For other tasks, it delegates to claimAndSpawn for the full pipeline.
func (tr *TaskRunner) ExecuteTask(ctx context.Context, task *types.ResolvedTask, projectID string) error {
	// Check capacity
	maxParallel := tr.getMaxParallel()
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
	// Resolve executor for this task
	taskExecutor, err := tr.resolveExecutor(task)
	if err != nil {
		return fmt.Errorf("resolve executor: %w", err)
	}

	// Resolve workdir (may create git worktree)
	workdir, err := taskExecutor.ResolveWorkdir(task)
	if err != nil {
		return fmt.Errorf("resolve workdir: %w", err)
	}

	spawnOpts := SpawnOptions{
		Mode:     tr.mode,
		Workdir:  workdir,
		IsResume: true,
	}

	spawnResult, err := taskExecutor.Spawn(ctx, task, projectID, spawnOpts)
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
		RunID:          latestInProgressRunID(task.Runs),
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

// ExecuteFeature batch-executes all ready tasks from a feature group.
// Filters to ready tasks, sorts by priority, and executes up to available
// capacity. Returns the number of tasks successfully started.
func (tr *TaskRunner) ExecuteFeature(ctx context.Context, tasks []types.ResolvedTask, projectID string) (int, error) {
	// 1. Filter to only ready tasks
	var readyTasks []types.ResolvedTask
	for _, task := range tasks {
		if task.Classification == "ready" {
			readyTasks = append(readyTasks, task)
		}
	}

	if len(readyTasks) == 0 {
		return 0, nil
	}

	// 2. Sort by priority (high > medium > low), then title for stability
	sort.Slice(readyTasks, func(i, j int) bool {
		pi := priorityOrder(readyTasks[i].Priority)
		pj := priorityOrder(readyTasks[j].Priority)
		if pi != pj {
			return pi < pj // lower order = higher priority
		}
		return readyTasks[i].Title < readyTasks[j].Title
	})

	// 3. Check available capacity
	maxParallel := tr.getMaxParallel()
	running := tr.processMgr.RunningCount()
	available := maxParallel - running
	if available <= 0 {
		return 0, fmt.Errorf("at capacity: %d/%d slots in use", running, maxParallel)
	}

	// 4. Execute up to available slots
	started := 0
	var firstErr error
	for _, task := range readyTasks {
		if started >= available {
			break
		}
		if ctx.Err() != nil {
			break
		}

		taskCopy := task // copy for closure safety
		if err := tr.claimAndSpawn(ctx, &taskCopy, projectID); err != nil {
			tr.logger.Printf("feature execute: claim and spawn failed for %s/%s: %v", projectID, task.ID, err)
			if firstErr == nil {
				firstErr = err
			}
			continue // try next task, don't fail the whole batch
		}
		started++
	}

	return started, firstErr
}

// priorityOrder maps priority strings to sort order (lower = higher priority).
func priorityOrder(priority string) int {
	switch priority {
	case "high":
		return 0
	case "medium":
		return 1
	case "low":
		return 2
	default:
		return 1 // default to medium
	}
}
