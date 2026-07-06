package api

import (
	"context"
	"log/slog"

	"github.com/huynle/brain-api/internal/types"
)

// pausePublisher is the narrow interface publishPauseCommand needs from
// realtime.Hub. Kept small so unit tests can mock it easily.
type pausePublisher interface {
	PublishRunnerCommand(runnerID string, command string, payload interface{})
}

// pauseRunnerLister is the narrow interface publishPauseCommand needs
// from the runner registry. Matches RunnerRegistryService.ListRunners.
type pauseRunnerLister interface {
	ListRunners(ctx context.Context) (*types.RunnerListResponse, error)
}

// publishPauseCommand fans out an SSE pause/resume command to every
// online runner subscribed to the affected project. This is what keeps
// runner-local pauseCache in sync with the API's DB state after the
// PWA (or any client) toggles a pause dial.
//
// Filtering rules:
//   - Only online runners receive the command. Offline/stale runners
//     will re-sync when they reconnect (via existing registration/poll
//     paths).
//   - If projectID is empty (global pause/resume), every online runner
//     receives the command.
//   - If projectID is non-empty, only runners whose Projects list
//     includes that project receive it. Runners with an empty Projects
//     list receive it too — an empty list conventionally means "all
//     projects" in this codebase.
//
// Payload shape:
//
//	{"projectId": "orion-ai", "scope": "tasks"}
//
// The runner-side handler in internal/runner/runner.go interprets
// projectId and scope to update the correct pause dial. See
// applyPauseCommand.
func publishPauseCommand(
	ctx context.Context,
	hub pausePublisher,
	registry pauseRunnerLister,
	projectID string,
	scope string,
	pause bool,
) {
	if hub == nil || registry == nil {
		return
	}
	resp, err := registry.ListRunners(ctx)
	if err != nil || resp == nil {
		slog.Warn("publishPauseCommand: list runners failed",
			"error", err, "project_id", projectID)
		return
	}
	command := "resume"
	if pause {
		command = "pause"
	}
	payload := map[string]any{
		"projectId": projectID,
		"scope":     scope,
	}
	for _, r := range resp.Runners {
		if r.Status != types.RunnerStatusOnline {
			continue
		}
		if projectID != "" && !runnerSubscribedToProject(r, projectID) {
			continue
		}
		hub.PublishRunnerCommand(r.RunnerID, command, payload)
	}
}

// runnerSubscribedToProject reports whether the runner accepts work
// for a given project. An empty Projects slice conventionally means
// "all projects" (matching the runner-side supportsProject logic).
func runnerSubscribedToProject(r types.RunnerInfo, projectID string) bool {
	if len(r.Projects) == 0 {
		return true
	}
	for _, p := range r.Projects {
		if p == projectID || p == "all" {
			return true
		}
	}
	return false
}
