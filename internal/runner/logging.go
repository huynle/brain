package runner

import "log/slog"

// RegisterEventLogger registers an event handler that logs all runner events
// via slog. This is used in headless (non-TUI) mode to ensure task lifecycle
// events are visible in the log output rather than silently discarded.
func RegisterEventLogger(tr *TaskRunner) {
	tr.OnEvent(func(event RunnerEvent) {
		switch event.Type {
		case EventTaskStarted:
			if event.Task != nil {
				slog.Info("task started",
					"task_id", event.Task.ID,
					"project", event.Task.ProjectID,
					"title", event.Task.Title,
					"pid", event.Task.PID,
				)
			}
		case EventTaskCompleted:
			if event.Result != nil {
				attrs := []any{
					"task_id", event.Result.TaskID,
					"duration_ms", event.Result.Duration,
					"status", string(event.Result.Status),
				}
				if event.Result.ExitCode != nil {
					attrs = append(attrs, "exit_code", *event.Result.ExitCode)
				}
				slog.Info("task completed", attrs...)
			}
		case EventTaskFailed:
			if event.Result != nil {
				attrs := []any{
					"task_id", event.Result.TaskID,
					"duration_ms", event.Result.Duration,
					"status", string(event.Result.Status),
				}
				if event.Result.ExitCode != nil {
					attrs = append(attrs, "exit_code", *event.Result.ExitCode)
				}
				slog.Warn("task failed", attrs...)
			}
		case EventTaskCancelled:
			slog.Info("task cancelled",
				"task_id", event.TaskID,
			)
		case EventPollComplete:
			slog.Debug("poll complete",
				"ready", event.ReadyCount,
				"running", event.RunningCount,
			)
		case EventSessionDiscovered:
			slog.Info("session discovered",
				"task_path", event.TaskPath,
				"session_id", event.SessionID,
			)
		case EventProjectPaused:
			slog.Info("project paused", "project", event.ProjectID)
		case EventProjectResumed:
			slog.Info("project resumed", "project", event.ProjectID)
		case EventAllPaused:
			slog.Info("all projects paused")
		case EventAllResumed:
			slog.Info("all projects resumed")
		case EventShutdown:
			slog.Info("runner shutdown", "reason", event.Reason)
		}
	})
}
