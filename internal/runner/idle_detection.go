package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// opcodeStatusClient is the HTTP client used for OpenCode status checks.
// Short timeout to avoid blocking the poll loop.
var opcodeStatusClient = &http.Client{Timeout: 5 * time.Second}

// checkOpencodeStatus queries the OpenCode HTTP API to check if it's idle or busy.
// The /session/status endpoint returns a map of session IDs to statuses.
// An empty map {} means all sessions are idle. Sessions that are busy appear in the map.
// Returns "idle", "busy", or "unavailable".
func checkOpencodeStatus(port int) string {
	url := fmt.Sprintf("http://localhost:%d/session/status", port)
	resp, err := opcodeStatusClient.Get(url)
	if err != nil {
		return "unavailable"
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "unavailable"
	}

	// Response is a map of sessionID -> status object.
	// Empty map = all idle, any entries = at least one busy.
	var statusMap map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&statusMap); err != nil {
		return "unavailable"
	}

	if len(statusMap) == 0 {
		return "idle"
	}
	return "busy"
}

// idleDetectionThreshold returns the configured idle detection threshold,
// defaulting to 30 seconds if not set.
func (tr *TaskRunner) idleDetectionThreshold() time.Duration {
	if tr.config.IdleDetectionThreshold > 0 {
		return time.Duration(tr.config.IdleDetectionThreshold) * time.Millisecond
	}
	return 30 * time.Second
}

// resolveCompleteOnIdle determines whether a task should be auto-completed on idle.
// If CompleteOnIdle is explicitly set, use that value.
// If DirectPrompt is set and CompleteOnIdle is not explicitly set, default to true.
func resolveCompleteOnIdle(completeOnIdle *bool, directPrompt string) bool {
	if completeOnIdle != nil {
		return *completeOnIdle
	}
	// Default to true when direct_prompt is set
	return directPrompt != ""
}

// checkIdleStatus iterates running tasks, checks their OpenCode status,
// and handles idle detection for tasks with CompleteOnIdle or direct_prompt.
func (tr *TaskRunner) checkIdleStatus(ctx context.Context) {
	allProcesses := tr.processMgr.GetAllRunning()
	threshold := tr.idleDetectionThreshold()

	for _, info := range allProcesses {
		if ctx.Err() != nil {
			return
		}

		task := info.Task
		port := task.OpencodePort

		// Skip tasks without a discovered port
		if port == 0 {
			continue
		}

		status := checkOpencodeStatus(port)

		switch status {
		case "idle":
			if task.IdleSince == "" {
				// First idle detection — record the timestamp
				now := time.Now().UTC().Format(time.RFC3339)
				tr.processMgr.UpdateIdleSince(task.ID, now)
				tr.logger.Printf("idle detection: task %s first idle at %s", task.ID, now)
			} else {
				// Already idle — check if threshold exceeded
				idleSince, err := time.Parse(time.RFC3339, task.IdleSince)
				if err != nil {
					tr.logger.Printf("idle detection: failed to parse IdleSince for %s: %v", task.ID, err)
					continue
				}

				idleDuration := time.Since(idleSince)
				if idleDuration >= threshold {
					tr.handleIdleThresholdExceeded(ctx, task)
				}
			}

		case "busy":
			// Agent is working — clear idle timestamp
			if task.IdleSince != "" {
				tr.processMgr.UpdateIdleSince(task.ID, "")
				tr.logger.Printf("idle detection: task %s back to busy, clearing idle timer", task.ID)
			}

		case "unavailable":
			// Skip — might be temporary (process starting up, network blip)
		}
	}
}

// isTerminalStatus returns true if the given status is a terminal state
// that should not be overwritten by the idle detection logic.
func isTerminalStatus(status string) bool {
	switch status {
	case "completed", "validated", "blocked", "cancelled", "archived", "superseded":
		return true
	}
	return false
}

// handleIdleThresholdExceeded handles a task that has been idle longer than the threshold.
func (tr *TaskRunner) handleIdleThresholdExceeded(ctx context.Context, task RunningTask) {
	// Guard: re-fetch the task status from the Brain API before overwriting.
	// This prevents a race condition where the agent already marked the task
	// as completed (or another terminal status) via brain_update, but the
	// runner's idle detection fires and overwrites it.
	entry, err := tr.client.GetEntry(ctx, task.Path)
	if err != nil {
		// API error — log and proceed with existing behavior (graceful degradation)
		tr.logger.Printf("idle detection: failed to re-fetch task %s status: %v (proceeding with idle handling)", task.ID, err)
	} else if isTerminalStatus(entry.Status) {
		tr.logger.Printf("idle detection: task %s already in terminal status %q, skipping overwrite", task.ID, entry.Status)

		// Determine the appropriate event type based on the actual status
		eventType := EventTaskCompleted
		if entry.Status == "blocked" || entry.Status == "cancelled" {
			eventType = EventTaskFailed
		}

		// Create task result
		completedAt := time.Now()
		duration := completedAt.Sub(task.StartedAt).Milliseconds()
		exitCode := 0
		result := &TaskResult{
			TaskID:      task.ID,
			Status:      TaskResultCompleted,
			StartedAt:   task.StartedAt,
			CompletedAt: completedAt,
			Duration:    duration,
			ExitCode:    &exitCode,
		}
		if entry.Status == "blocked" || entry.Status == "cancelled" {
			result.Status = TaskResultBlocked
		}

		// Cleanup: remove from process manager, tmux, temp files
		tr.processMgr.Remove(task.ID)

		tr.mu.Lock()
		if entry.Status == "completed" || entry.Status == "validated" {
			tr.stats.Completed++
		} else {
			tr.stats.Failed++
		}
		tr.stats.TotalRuntime += duration
		if tr.processMgr.RunningCount() == 0 {
			tr.status = RunnerStatusPolling
		}
		tr.mu.Unlock()

		tr.cleanupTaskTmux(task)
		for _, exec := range tr.executors {
			exec.Cleanup(task.ID, task.ProjectID)
		}

		tr.emitEvent(RunnerEvent{
			Type:   eventType,
			Result: result,
			TaskID: task.ID,
		})
		return
	}

	if task.CompleteOnIdle {
		// Auto-complete the task
		tr.logger.Printf("idle detection: task %s idle threshold exceeded, marking completed", task.ID)

		// Append completion note
		note := "\n\n---\n*Auto-completed by runner: OpenCode agent went idle after completing work.*\n"
		if err := tr.client.AppendToTask(ctx, task.Path, note); err != nil {
			tr.logger.Printf("idle detection: failed to append note to %s: %v", task.ID, err)
		}

		// Update task status to completed
		if err := tr.client.UpdateTaskStatus(ctx, task.Path, "completed"); err != nil {
			tr.logger.Printf("idle detection: failed to mark %s completed: %v", task.ID, err)
			return
		}

		// Create task result
		completedAt := time.Now()
		duration := completedAt.Sub(task.StartedAt).Milliseconds()
		exitCode := 0
		result := &TaskResult{
			TaskID:      task.ID,
			Status:      TaskResultCompleted,
			StartedAt:   task.StartedAt,
			CompletedAt: completedAt,
			Duration:    duration,
			ExitCode:    &exitCode,
		}

		// Remove from process manager
		tr.processMgr.Remove(task.ID)

		// Update stats
		tr.mu.Lock()
		tr.stats.Completed++
		tr.stats.TotalRuntime += duration
		if tr.processMgr.RunningCount() == 0 {
			tr.status = RunnerStatusPolling
		}
		tr.mu.Unlock()

		// Clean up tmux window
		tr.cleanupTaskTmux(task)

		// Cleanup temp files
		for _, exec := range tr.executors {
			exec.Cleanup(task.ID, task.ProjectID)
		}

		// Emit completion event
		tr.emitEvent(RunnerEvent{
			Type:   EventTaskCompleted,
			Result: result,
			TaskID: task.ID,
		})
	} else {
		// Not auto-complete — mark as blocked (agent went idle without completing)
		tr.logger.Printf("idle detection: task %s idle threshold exceeded, marking blocked", task.ID)

		note := "\n\n---\n*Marked blocked by runner: OpenCode agent went idle without completing the task.*\n"
		if err := tr.client.AppendToTask(ctx, task.Path, note); err != nil {
			tr.logger.Printf("idle detection: failed to append note to %s: %v", task.ID, err)
		}

		if err := tr.client.UpdateTaskStatus(ctx, task.Path, "blocked"); err != nil {
			tr.logger.Printf("idle detection: failed to mark %s blocked: %v", task.ID, err)
			return
		}

		// Remove from process manager
		tr.processMgr.Remove(task.ID)

		// Update stats
		tr.mu.Lock()
		tr.stats.Failed++
		if tr.processMgr.RunningCount() == 0 {
			tr.status = RunnerStatusPolling
		}
		tr.mu.Unlock()

		// Clean up tmux window
		tr.cleanupTaskTmux(task)

		// Cleanup temp files
		for _, exec := range tr.executors {
			exec.Cleanup(task.ID, task.ProjectID)
		}

		// Emit event
		tr.emitEvent(RunnerEvent{
			Type:   EventTaskFailed,
			TaskID: task.ID,
		})
	}
}
