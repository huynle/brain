package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
	_ "time/tzdata" // Embedded IANA timezone database as safety net

	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/cron"
)

// featureSchedState tracks per-feature scheduling state to prevent re-triggering.
type featureSchedState struct {
	runOnceFired bool // true after a feature_run_once_at has been triggered
}

// timeWindowResult represents the result of checking a task's time window.
type timeWindowResult int

const (
	windowOpen    timeWindowResult = iota // Window is open (or no window set)
	windowNotYet                          // Before starts_at
	windowExpired                         // After expires_at
)

// checkTimeWindow evaluates starts_at/expires_at time windows for a scheduled task.
// Returns windowOpen if the task is within its time window (or no window is set),
// windowNotYet if now is before starts_at, or windowExpired if now is after expires_at.
// starts_at/expires_at are RFC3339 strings (with embedded timezone info).
// Invalid timestamps are ignored (treated as unset).
// Checks starts_at first: if now < starts_at, returns windowNotYet immediately.
// Then checks expires_at: if now > expires_at, returns windowExpired.
func checkTimeWindow(startsAt, expiresAt string, now time.Time, timezone string) timeWindowResult {
	nowUTC := now.UTC()

	// Check starts_at: if set and now is before it, window hasn't opened yet
	if startsAt != "" {
		t, err := time.Parse(time.RFC3339, startsAt)
		if err == nil {
			if nowUTC.Before(t.UTC()) {
				return windowNotYet
			}
		}
		// Invalid starts_at: ignore (treat as unset)
	}

	// Check expires_at: if set and now is after it, window has expired
	if expiresAt != "" {
		t, err := time.Parse(time.RFC3339, expiresAt)
		if err == nil {
			if nowUTC.After(t.UTC()) {
				return windowExpired
			}
		}
		// Invalid expires_at: ignore (treat as unset)
	}

	return windowOpen
}

// shouldTriggerRunOnce checks if a run_once_at task should fire now.
// Returns true if runOnceAt is a valid RFC3339 timestamp and now >= runOnceAt.
// The timezone parameter is accepted for consistency but runOnceAt is stored as RFC3339.
func shouldTriggerRunOnce(runOnceAt string, now time.Time, timezone string) bool {
	if runOnceAt == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, runOnceAt)
	if err != nil {
		return false
	}
	return !now.UTC().Before(t.UTC())
}

// Eligible statuses for cron triggering.
// Tasks in these statuses can be reset to "pending" by the scheduler.
var cronEligibleStatuses = map[string]bool{
	"active":    true,
	"completed": true,
	"blocked":   true,
}

// loadTimezone loads an IANA timezone location.
// Returns UTC if timezone is empty or invalid.
func loadTimezone(timezone string) *time.Location {
	if timezone == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// shouldTrigger checks if a scheduled task should be triggered now.
// Checks next_run first (is now >= next_run?), falls back to live cron matching.
// The timezone parameter specifies the IANA timezone for cron evaluation.
// Empty or invalid timezone falls back to UTC.
func shouldTrigger(schedule string, nextRun string, now time.Time, timezone string) bool {
	if schedule == "" {
		return false
	}

	// Parse the cron expression (validates it)
	sched, err := cron.Parse(schedule)
	if err != nil {
		return false
	}

	// If next_run is set and valid, use it (next_run is already stored as UTC)
	if nextRun != "" {
		nextRunTime, err := time.Parse(time.RFC3339, nextRun)
		if err == nil {
			// Valid next_run: trigger if now >= next_run
			return !now.Before(nextRunTime)
		}
		// Invalid next_run: fall through to cron matching
	}

	// Convert now to the task's timezone for cron matching
	loc := loadTimezone(timezone)
	return sched.Matches(now.In(loc))
}

// getNextRun computes the next trigger time from a cron expression.
// The timezone parameter specifies the IANA timezone for cron evaluation.
// Computation is done in the task's timezone, then the result is converted to UTC.
// Empty or invalid timezone falls back to UTC.
func getNextRun(schedule string, after time.Time, timezone string) (time.Time, error) {
	sched, err := cron.Parse(schedule)
	if err != nil {
		return time.Time{}, err
	}

	loc := loadTimezone(timezone)
	// Convert after to the task's timezone, find next match, convert back to UTC
	nextLocal := sched.NextAfter(after.In(loc))
	return nextLocal.UTC(), nil
}

// parseTimeInZone parses an RFC3339 time string and converts it to the specified timezone.
// Returns the time in the given timezone's location.
// Returns error if the time string is invalid or the timezone is unknown.
func parseTimeInZone(timeStr, timezone string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, timeStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid time %q: %w", timeStr, err)
	}

	if timezone == "" {
		return t.UTC(), nil
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone %q: %w", timezone, err)
	}

	return t.In(loc), nil
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

// isScheduledTask returns true if the task has a cron schedule or run_once_at set.
func isScheduledTask(task *types.ResolvedTask) bool {
	return task.Schedule != "" || task.RunOnceAt != ""
}

// featureScheduleInfo holds the extracted schedule fields for a feature group.
type featureScheduleInfo struct {
	featureID    string
	schedule     string
	startsAt     string
	expiresAt    string
	runOnceAt    string
	timezone     string
	priorityRank int // lower = higher priority (0=high,1=medium,2=low)
}

// priorityRank maps priority strings to numeric ranks (lower = higher priority).
func priorityRank(p string) int {
	switch p {
	case "high":
		return 0
	case "medium":
		return 1
	case "low":
		return 2
	default:
		return 3 // no priority
	}
}

// checkFeatureSchedules evaluates feature-level schedules and enables/disables
// features based on cron expressions, one-shot times, and time windows.
// Called each poll iteration after checkScheduledTasks.
func (tr *TaskRunner) checkFeatureSchedules(ctx context.Context, now time.Time) {
	for _, projectID := range tr.projects {
		if ctx.Err() != nil {
			return
		}

		tasks, err := tr.client.GetAllTasks(ctx, projectID)
		if err != nil {
			tr.logger.Printf("feature-sched: failed to get tasks for %s: %v", projectID, err)
			continue
		}

		// Group tasks by feature_id and extract schedule info from highest-priority task
		featureInfos := tr.groupFeatureSchedules(tasks)

		for featureID, info := range featureInfos {
			if ctx.Err() != nil {
				return
			}
			tr.evaluateFeatureSchedule(ctx, featureID, info, now)
		}
	}
}

// groupFeatureSchedules groups tasks by feature_id and extracts the feature schedule
// fields from the highest-priority task that has them set.
func (tr *TaskRunner) groupFeatureSchedules(tasks []types.ResolvedTask) map[string]featureScheduleInfo {
	infos := make(map[string]featureScheduleInfo)

	for i := range tasks {
		task := &tasks[i]
		if task.FeatureID == "" {
			continue
		}
		// Skip tasks with no feature-level schedule fields
		if task.FeatureSchedule == "" && task.FeatureRunOnceAt == "" {
			// Check if another task in this feature already has schedule info
			if _, exists := infos[task.FeatureID]; !exists {
				continue
			}
			// Already have info from another task, skip
			continue
		}

		rank := priorityRank(task.FeaturePriority)
		existing, exists := infos[task.FeatureID]

		if !exists || rank < existing.priorityRank {
			infos[task.FeatureID] = featureScheduleInfo{
				featureID:    task.FeatureID,
				schedule:     task.FeatureSchedule,
				startsAt:     task.FeatureStartsAt,
				expiresAt:    task.FeatureExpiresAt,
				runOnceAt:    task.FeatureRunOnceAt,
				timezone:     task.FeatureTimezone,
				priorityRank: rank,
			}
		}
	}

	return infos
}

// evaluateFeatureSchedule evaluates a single feature's schedule and enables/disables it.
func (tr *TaskRunner) evaluateFeatureSchedule(ctx context.Context, featureID string, info featureScheduleInfo, now time.Time) {
	// Check time window first
	switch checkTimeWindow(info.startsAt, info.expiresAt, now, info.timezone) {
	case windowNotYet:
		// Window hasn't opened yet — skip silently
		tr.logger.Printf("feature-sched: %s window not yet open (starts_at: %s)", featureID, info.startsAt)
		return
	case windowExpired:
		// Window has passed — disable the feature
		tr.logger.Printf("feature-sched: %s window expired (expires_at: %s), disabling", featureID, info.expiresAt)
		tr.DisableFeature(featureID)
		return
	}

	// Handle run_once_at (one-shot)
	if info.runOnceAt != "" && info.schedule == "" {
		tr.mu.RLock()
		state := tr.featureScheduleState[featureID]
		tr.mu.RUnlock()

		if state.runOnceFired {
			// Already fired — do not re-trigger
			return
		}

		if shouldTriggerRunOnce(info.runOnceAt, now, info.timezone) {
			tr.logger.Printf("feature-sched: triggering run_once_at for feature %s", featureID)
			tr.EnableFeature(featureID)

			// Mark as fired to prevent re-triggering
			tr.mu.Lock()
			tr.featureScheduleState[featureID] = featureSchedState{runOnceFired: true}
			tr.mu.Unlock()
		}
		return
	}

	// Handle cron schedule
	if info.schedule != "" {
		if shouldTrigger(info.schedule, "", now, info.timezone) {
			tr.logger.Printf("feature-sched: cron match for feature %s (schedule: %s)", featureID, info.schedule)
			tr.EnableFeature(featureID)
		}
	}
}

// checkProjectScheduledTasks checks scheduled tasks for a single project.
// Handles both recurring cron-based tasks and one-shot run_once_at tasks.
func (tr *TaskRunner) checkProjectScheduledTasks(ctx context.Context, projectID string, now time.Time) {
	tasks, err := tr.client.GetAllTasks(ctx, projectID)
	if err != nil {
		tr.logger.Printf("cron: failed to get tasks for %s: %v", projectID, err)
		return
	}

	for i := range tasks {
		task := &tasks[i]

		// Skip tasks without a schedule or run_once_at
		if !isScheduledTask(task) {
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

		// Check time window: starts_at / expires_at
		switch checkTimeWindow(task.StartsAt, task.ExpiresAt, now, task.Timezone) {
		case windowNotYet:
			// Window hasn't opened yet — skip silently
			continue
		case windowExpired:
			// Window has passed — auto-disable the schedule
			tr.disableSchedule(ctx, task, fmt.Sprintf("expires_at passed (%s)", task.ExpiresAt))
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

		// Branch: run_once_at (one-shot) vs cron (recurring)
		if task.RunOnceAt != "" && task.Schedule == "" {
			// One-shot task: check if time has arrived
			if !shouldTriggerRunOnce(task.RunOnceAt, now, task.Timezone) {
				continue
			}
			tr.processRunOnceTask(ctx, task, now)
		} else {
			// Recurring cron task
			if !shouldTrigger(task.Schedule, task.NextRun, now, task.Timezone) {
				continue
			}
			tr.processScheduledTask(ctx, task, now)
		}
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

// processScheduledTask resets a scheduled task to pending and advances next_run.
// It records the run in the task's runs[] array for tracking and max_runs counting.
func (tr *TaskRunner) processScheduledTask(ctx context.Context, task *types.ResolvedTask, now time.Time) {
	runID := generateRunID(now)

	// Record the run as in_progress
	newRun := map[string]interface{}{
		"run_id":  runID,
		"status":  "in_progress",
		"started": now.UTC().Format(time.RFC3339),
		"tasks":   1,
	}
	runs := make([]interface{}, 0, len(task.Runs)+1)
	for _, r := range task.Runs {
		runs = append(runs, map[string]interface{}{
			"run_id":      r.RunID,
			"status":      r.Status,
			"started":     r.Started,
			"completed":   r.Completed,
			"skip_reason": r.SkipReason,
		})
	}
	runs = append(runs, newRun)

	// Update metadata: runs array + next_run
	nextRun, err := getNextRun(task.Schedule, now, task.Timezone)
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

// processRunOnceTask fires a one-shot run_once_at task: records a run, resets status
// to pending, and auto-disables the schedule so it never fires again.
func (tr *TaskRunner) processRunOnceTask(ctx context.Context, task *types.ResolvedTask, now time.Time) {
	runID := generateRunID(now)

	// Record the run as in_progress
	newRun := map[string]interface{}{
		"run_id":  runID,
		"status":  "in_progress",
		"started": now.UTC().Format(time.RFC3339),
		"tasks":   1,
	}
	runs := make([]interface{}, 0, len(task.Runs)+1)
	for _, r := range task.Runs {
		runs = append(runs, map[string]interface{}{
			"run_id":      r.RunID,
			"status":      r.Status,
			"started":     r.Started,
			"completed":   r.Completed,
			"skip_reason": r.SkipReason,
		})
	}
	runs = append(runs, newRun)

	// Update metadata: runs array + auto-disable schedule
	if err := tr.client.UpdateMetadata(ctx, task.Path, map[string]interface{}{
		"runs":             runs,
		"schedule_enabled": false,
	}); err != nil {
		tr.logger.Printf("cron: failed to update runs/disable for %s: %v", task.ID, err)
	}

	// Reset status to pending
	if err := tr.client.UpdateTaskStatus(ctx, task.Path, "pending"); err != nil {
		tr.logger.Printf("cron: failed to reset status for %s: %v", task.ID, err)
		return
	}

	tr.logger.Printf("cron: triggered one-shot %s run=%s (run_once_at: %s), auto-disabled, runs=%d",
		task.ID, runID, task.RunOnceAt, len(runs))
}
