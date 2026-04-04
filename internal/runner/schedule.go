package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
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

		// Check max_runs: count completed/failed runs and stop if exhausted
		if task.MaxRuns != nil && *task.MaxRuns > 0 {
			runCount := countRuns(task.Runs)
			if runCount >= *task.MaxRuns {
				tr.disableSchedule(ctx, task, fmt.Sprintf("max_runs reached (%d/%d)", runCount, *task.MaxRuns))
				continue
			}
		}

		// Check if the schedule triggers now
		if !shouldTrigger(task.Schedule, task.NextRun, now) {
			continue
		}

		// Trigger the task
		tr.processScheduledTask(ctx, task, now)
	}
}

// latestInProgressRunID returns the RunID of the most recent run with status "in_progress".
// Returns empty string if no in-progress run is found.
func latestInProgressRunID(runs []types.CronRun) string {
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].Status == "in_progress" {
			return runs[i].RunID
		}
	}
	return ""
}

// countRuns counts the number of completed, failed, and skipped runs.
func countRuns(runs []types.CronRun) int {
	count := 0
	for _, r := range runs {
		switch r.Status {
		case "completed", "failed", "skipped", "in_progress":
			count++
		}
	}
	return count
}

// generateRunID creates a unique run ID for a scheduled execution.
func generateRunID(t time.Time) string {
	suffix := make([]byte, 3)
	rand.Read(suffix)
	return fmt.Sprintf("%s-%s", t.UTC().Format("20060102-1504"), hex.EncodeToString(suffix))
}

// disableSchedule sets schedule_enabled=false and logs why.
func (tr *TaskRunner) disableSchedule(ctx context.Context, task *types.ResolvedTask, reason string) {
	tr.logger.Printf("cron: disabling schedule for %s: %s", task.ID, reason)

	schedDisabled := false
	if err := tr.client.UpdateMetadata(ctx, task.Path, map[string]interface{}{
		"schedule_enabled": schedDisabled,
	}); err != nil {
		tr.logger.Printf("cron: failed to disable schedule for %s: %v", task.ID, err)
	}

	note := fmt.Sprintf("\n\n## Schedule Disabled\n- Reason: %s\n- Disabled at: %s\n", reason, time.Now().UTC().Format(time.RFC3339))
	if err := tr.client.AppendToTask(ctx, task.Path, note); err != nil {
		tr.logger.Printf("cron: failed to append disable note for %s: %v", task.ID, err)
	}

	// Emit event for TUI
	tr.emitEvent(RunnerEvent{
		Type:   EventTaskCancelled,
		TaskID: task.ID,
		Reason: "schedule disabled: " + reason,
	})
}

// processScheduledTask handles a triggered scheduled task.
// For feature_schedule gate tasks, it directly completes the task (no agent spawn).
// For regular tasks, it resets status to pending for agent pickup.
// It records the run in the task's runs[] array for tracking and max_runs counting.
func (tr *TaskRunner) processScheduledTask(ctx context.Context, task *types.ResolvedTask, now time.Time) {
	isFeatureGate := task.GeneratedKind == "feature_schedule"

	if isFeatureGate {
		tr.processFeatureScheduleGate(ctx, task, now)
	} else {
		tr.processRegularScheduledTask(ctx, task, now)
	}
}

// processFeatureScheduleGate directly completes a feature_schedule gate task.
// This immediately unblocks all downstream feature tasks with zero overhead.
func (tr *TaskRunner) processFeatureScheduleGate(ctx context.Context, task *types.ResolvedTask, now time.Time) {
	runID := generateRunID(now)
	nowStr := now.UTC().Format(time.RFC3339)

	// Record the run as completed directly (no in_progress intermediate)
	newRun := map[string]interface{}{
		"run_id":    runID,
		"status":    "completed",
		"started":   nowStr,
		"completed": nowStr,
		"tasks":     1,
	}
	runs := buildRunsArray(task.Runs, newRun)

	// Compute next_run for metadata
	nextRun, err := getNextRun(task.Schedule, now)
	if err != nil {
		tr.logger.Printf("cron: failed to compute next_run for %s: %v", task.ID, err)
		return
	}

	// Update metadata: runs, next_run, and disable schedule
	if err := tr.client.UpdateMetadata(ctx, task.Path, map[string]interface{}{
		"runs":             runs,
		"next_run":         nextRun.Format(time.RFC3339),
		"schedule_enabled": false,
	}); err != nil {
		tr.logger.Printf("cron: failed to update metadata for gate %s: %v", task.ID, err)
	}

	// Set status to completed directly
	if err := tr.client.UpdateTaskStatus(ctx, task.Path, "completed"); err != nil {
		tr.logger.Printf("cron: failed to complete gate %s: %v", task.ID, err)
		return
	}

	tr.logger.Printf("cron: feature gate completed %s run=%s (feature_id: %s), next_run: %s",
		task.ID, runID, task.FeatureID, nextRun.Format(time.RFC3339))

	// Emit completed event for TUI
	tr.emitEvent(RunnerEvent{
		Type:   EventTaskCompleted,
		TaskID: task.ID,
	})
}

// processRegularScheduledTask resets a scheduled task to pending and advances next_run.
func (tr *TaskRunner) processRegularScheduledTask(ctx context.Context, task *types.ResolvedTask, now time.Time) {
	runID := generateRunID(now)

	// Record the run as in_progress
	newRun := map[string]interface{}{
		"run_id":  runID,
		"status":  "in_progress",
		"started": now.UTC().Format(time.RFC3339),
		"tasks":   1,
	}
	runs := buildRunsArray(task.Runs, newRun)

	// Update metadata: runs array + next_run
	nextRun, err := getNextRun(task.Schedule, now)
	if err != nil {
		tr.logger.Printf("cron: failed to compute next_run for %s: %v", task.ID, err)
		return
	}

	if err := tr.client.UpdateMetadata(ctx, task.Path, map[string]interface{}{
		"runs":     runs,
		"next_run": nextRun.Format(time.RFC3339),
	}); err != nil {
		tr.logger.Printf("cron: failed to update runs/next_run for %s: %v", task.ID, err)
	}

	// Reset status to pending
	if err := tr.client.UpdateTaskStatus(ctx, task.Path, "pending"); err != nil {
		tr.logger.Printf("cron: failed to reset status for %s: %v", task.ID, err)
		return
	}

	tr.logger.Printf("cron: triggered %s run=%s (schedule: %s), next_run: %s, runs=%d",
		task.ID, runID, task.Schedule, nextRun.Format(time.RFC3339), len(runs))
}

// buildRunsArray constructs the runs metadata array from existing runs plus a new run.
func buildRunsArray(existingRuns []types.CronRun, newRun map[string]interface{}) []interface{} {
	runs := make([]interface{}, 0, len(existingRuns)+1)
	for _, r := range existingRuns {
		runs = append(runs, map[string]interface{}{
			"run_id":      r.RunID,
			"status":      r.Status,
			"started":     r.Started,
			"completed":   r.Completed,
			"skip_reason": r.SkipReason,
		})
	}
	runs = append(runs, newRun)
	return runs
}
