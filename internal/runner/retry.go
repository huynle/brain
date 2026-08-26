package runner

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// DefaultMaxTaskAttempts caps how many times a task may run before a failure
// parks it in "blocked" rather than resetting it to "pending".
//
// A failed/crashed/timed-out task is reset to "pending" so it can be retried.
// Nothing counted those retries, so a task that fails deterministically — a
// guard that exits non-zero, a missing input, a bad command — was re-dispatched
// on every poll (5s in practice), forever, occupying a runner slot on each
// pass. Three attempts is enough to ride out a transient failure and few
// enough that a deterministic one stops quickly.
const DefaultMaxTaskAttempts = 3

// resolveMaxAttempts returns the attempt cap for a task.
//
// Precedence: task retry.max_attempts > runner config > DefaultMaxTaskAttempts.
// The value is a count of TOTAL attempts, not retries-after-the-first: 1 means
// "run once, never retry". A negative config value disables the cap entirely,
// restoring the old unbounded-retry behaviour.
func resolveMaxAttempts(task *types.ResolvedTask, cfg RunnerConfig) int {
	if task != nil && task.Retry != nil && task.Retry.MaxAttempts > 0 {
		return task.Retry.MaxAttempts
	}
	if cfg.MaxTaskAttempts < 0 {
		return 0 // uncapped
	}
	if cfg.MaxTaskAttempts > 0 {
		return cfg.MaxTaskAttempts
	}
	return DefaultMaxTaskAttempts
}

// exhaustedAttempts reports whether a task that has just failed has used up
// its allowance. maxAttempts <= 0 means uncapped.
func exhaustedAttempts(attemptCount, maxAttempts int) bool {
	if maxAttempts <= 0 {
		return false
	}
	return attemptCount >= maxAttempts
}

// recordTaskFailure persists the attempt counter after a failed run and reports
// the status the task should be moved to: "blocked" once the cap is reached,
// "pending" while retries remain.
//
// Metadata write failures are logged, not fatal — losing the counter costs one
// extra retry, whereas failing here would leave the task stuck in_progress.
func (tr *TaskRunner) recordTaskFailure(ctx context.Context, task RunningTask) (status string, attempt int) {
	attempt = task.AttemptCount + 1
	exhausted := exhaustedAttempts(attempt, task.MaxAttempts)

	fields := map[string]interface{}{
		"attempt_count":  attempt,
		"last_failed_at": time.Now().UTC().Format(time.RFC3339),
	}
	if err := tr.client.UpdateMetadata(ctx, task.Path, fields); err != nil {
		slog.Warn("retry: failed to persist attempt counter (continuing)",
			"project", task.ProjectID, "task_id", task.ID, "attempt", attempt, "error", err)
	}

	if !exhausted {
		return "pending", attempt
	}

	slog.Warn("retry: attempt cap reached, parking task in blocked",
		"project", task.ProjectID, "task_id", task.ID,
		"attempts", attempt, "max_attempts", task.MaxAttempts)

	// Leave a trail on the task itself: "blocked" with no explanation is the
	// same dead end as the silent crash-loop, just quieter.
	note := fmt.Sprintf(
		"\n\n---\n**Retry cap reached** — failed %d/%d attempts, most recently at %s. "+
			"Automatic retries stopped. Fix the cause, then resume or re-trigger the task "+
			"(resuming clears the counter on the next successful run).\n",
		attempt, task.MaxAttempts, time.Now().UTC().Format(time.RFC3339))
	if err := tr.client.AppendToTask(ctx, task.Path, note); err != nil {
		slog.Warn("retry: failed to append cap note to task (continuing)",
			"project", task.ProjectID, "task_id", task.ID, "error", err)
	}

	return "blocked", attempt
}

// clearTaskFailures resets the attempt counter after a successful run so a
// later failure starts from zero rather than inheriting a stale count from a
// previous incarnation of the same task. No-op when the counter is already 0.
func (tr *TaskRunner) clearTaskFailures(ctx context.Context, task RunningTask) {
	if task.AttemptCount == 0 {
		return
	}
	if err := tr.client.UpdateMetadata(ctx, task.Path, map[string]interface{}{
		"attempt_count": 0,
	}); err != nil {
		slog.Warn("retry: failed to clear attempt counter after success (continuing)",
			"project", task.ProjectID, "task_id", task.ID, "error", err)
	}
}
