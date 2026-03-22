package runner

import (
	"context"
	"time"

	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/cron"
)

// Eligible statuses for cron triggering.
// Tasks in these statuses can be reset to "pending" by the scheduler.
var cronEligibleStatuses = map[string]bool{
	"active":    true,
	"completed": true,
	"blocked":   true,
}

// shouldTrigger checks if a scheduled task should be triggered now.
// Checks next_run first (is now >= next_run?), falls back to live cron matching.
func shouldTrigger(schedule string, nextRun string, now time.Time) bool {
	if schedule == "" {
		return false
	}

	// Parse the cron expression (validates it)
	sched, err := cron.Parse(schedule)
	if err != nil {
		return false
	}

	// If next_run is set and valid, use it
	if nextRun != "" {
		nextRunTime, err := time.Parse(time.RFC3339, nextRun)
		if err == nil {
			// Valid next_run: trigger if now >= next_run
			return !now.Before(nextRunTime)
		}
		// Invalid next_run: fall through to cron matching
	}

	// Fallback: live cron matching against current time
	return sched.Matches(now.UTC())
}

// getNextRun computes the next trigger time from a cron expression.
func getNextRun(schedule string, after time.Time) (time.Time, error) {
	sched, err := cron.Parse(schedule)
	if err != nil {
		return time.Time{}, err
	}
	return sched.NextAfter(after.UTC()), nil
}

// checkScheduledTasks evaluates cron expressions on scheduled tasks
// and triggers them by resetting their status to pending.
// Rate-limited to run at most once per poll interval.
func (tr *TaskRunner) checkScheduledTasks(ctx context.Context, now time.Time) {
	// Rate limiting: skip if we checked too recently
	tr.mu.RLock()
	lastCheck := tr.lastCronCheckAt
	tr.mu.RUnlock()

	pollInterval := time.Duration(tr.config.PollInterval) * time.Second
	if pollInterval < time.Second {
		pollInterval = time.Second
	}
	if !lastCheck.IsZero() && now.Sub(lastCheck) < pollInterval {
		return
	}

	// Update last check time
	tr.mu.Lock()
	tr.lastCronCheckAt = now
	tr.mu.Unlock()

	for _, projectID := range tr.projects {
		if ctx.Err() != nil {
			return
		}

		tr.checkProjectScheduledTasks(ctx, projectID, now)
	}
}

// checkProjectScheduledTasks checks scheduled tasks for a single project.
func (tr *TaskRunner) checkProjectScheduledTasks(ctx context.Context, projectID string, now time.Time) {
	tasks, err := tr.client.GetAllTasks(ctx, projectID)
	if err != nil {
		tr.logger.Printf("cron: failed to get tasks for %s: %v", projectID, err)
		return
	}

	for i := range tasks {
		task := &tasks[i]

		// Skip tasks without a schedule
		if task.Schedule == "" {
			continue
		}

		// Skip disabled schedules
		if task.ScheduleEnabled != nil && !*task.ScheduleEnabled {
			continue
		}

		// Only trigger from eligible statuses
		if !cronEligibleStatuses[task.Status] {
			continue
		}

		// Overlap guard: skip if task is already tracked in process manager
		if tr.processMgr.Get(task.ID) != nil {
			continue
		}

		// Check if the schedule triggers now
		if !shouldTrigger(task.Schedule, task.NextRun, now) {
			continue
		}

		// Trigger the task
		tr.processScheduledTask(ctx, task, now)
	}
}

// processScheduledTask resets a scheduled task to pending and advances next_run.
func (tr *TaskRunner) processScheduledTask(ctx context.Context, task *types.ResolvedTask, now time.Time) {
	// Reset status to pending via the proper status update endpoint
	// (UpdateTaskStatus updates the DB status column, not just the metadata JSON)
	if err := tr.client.UpdateTaskStatus(ctx, task.Path, "pending"); err != nil {
		tr.logger.Printf("cron: failed to reset status for %s: %v", task.ID, err)
		return
	}

	// Advance next_run
	nextRun, err := getNextRun(task.Schedule, now)
	if err != nil {
		tr.logger.Printf("cron: failed to compute next_run for %s: %v", task.ID, err)
		return
	}

	if err := tr.client.UpdateMetadata(ctx, task.Path, map[string]interface{}{
		"next_run": nextRun.Format(time.RFC3339),
	}); err != nil {
		tr.logger.Printf("cron: failed to update next_run for %s: %v", task.ID, err)
	}

	tr.logger.Printf("cron: triggered %s (schedule: %s), next_run: %s",
		task.ID, task.Schedule, nextRun.Format(time.RFC3339))
}
