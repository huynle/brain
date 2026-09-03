package runner

import "log/slog"

// RegisterEventLogger registers an event handler that logs all runner events
// via slog. This is used in headless mode to ensure task lifecycle
// events are visible in the log output rather than silently discarded.
func RegisterEventLogger(tr *TaskRunner) {
	tr.OnEvent(func(event RunnerEvent) {
		switch event.Type {
		case EventRunnerStarted:
			slog.Info("runner started",
				"runner_id", event.RunnerID,
				"mode", event.Mode,
				"projects", event.Projects,
			)
		case EventTaskStarted:
			if event.Task != nil {
				slog.Info("task started",
					"runner_id", event.RunnerID,
					"task_id", event.Task.ID,
					"project", event.Task.ProjectID,
					"title", event.Task.Title,
					"pid", event.Task.PID,
				)
			}
		case EventTaskClaimed:
			slog.Info("task claimed",
				"runner_id", event.RunnerID,
				"task_id", event.TaskID,
				"project", event.ProjectID,
			)
		case EventTaskClaimRejected:
			slog.Warn("task claim rejected",
				"runner_id", event.RunnerID,
				"task_id", event.TaskID,
				"project", event.ProjectID,
				"claimed_by", event.ClaimedBy,
			)
		case EventTaskStatusChanged:
			slog.Info("task status changed",
				"runner_id", event.RunnerID,
				"task_id", event.TaskID,
				"project", event.ProjectID,
				"from", event.FromStatus,
				"to", event.ToStatus,
			)
		case EventTaskCompleted:
			if event.Result != nil {
				attrs := []any{
					"runner_id", event.RunnerID,
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
					"runner_id", event.RunnerID,
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
				"runner_id", event.RunnerID,
				"task_id", event.TaskID,
			)
		case EventTaskReleased:
			slog.Warn("task released",
				"runner_id", event.RunnerID,
				"task_id", event.TaskID,
				"project", event.ProjectID,
			)
		case EventPollComplete:
			slog.Debug("poll complete",
				"runner_id", event.RunnerID,
				"ready", event.ReadyCount,
				"running", event.RunningCount,
			)
		case EventSessionDiscovered:
			slog.Info("session discovered",
				"runner_id", event.RunnerID,
				"task_path", event.TaskPath,
				"session_id", event.SessionID,
			)
		case EventProjectPaused:
			slog.Info("project paused",
				"runner_id", event.RunnerID,
				"project", event.ProjectID,
			)
		case EventProjectResumed:
			slog.Info("project resumed",
				"runner_id", event.RunnerID,
				"project", event.ProjectID,
			)
		case EventAllPaused:
			slog.Info("all projects paused",
				"runner_id", event.RunnerID,
			)
		case EventAllResumed:
			slog.Info("all projects resumed",
				"runner_id", event.RunnerID,
			)
		case EventShutdown:
			slog.Info("runner shutdown",
				"runner_id", event.RunnerID,
				"reason", event.Reason,
			)
		}
	})
}
