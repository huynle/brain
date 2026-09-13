package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/huynle/brain-api/internal/service"
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Live context injector
//
// Production implementation of service.LiveInjector. It reuses the SAME
// in-process control plumbing the goal steerer uses (instance registry to
// locate the live task instance + runner bridge to deliver the prompt via the
// OpenCode /session/{id}/prompt_async proxy). This lets ResumeTaskWithContext
// inject supervisor context into a still-running session without a relaunch.
//
// Deliberately mirrors bridgeGoalSteerer (goal_steerer.go) rather than
// inventing a parallel discovery/delivery path.
// =============================================================================

// bridgeLiveInjector injects context into a task's live OpenCode session.
type bridgeLiveInjector struct {
	instances goalInstanceLister
	bridge    goalBridgeDoer
}

var _ service.LiveInjector = (*bridgeLiveInjector)(nil)

// newBridgeLiveInjector wires the injector from the instance registry and the
// runner bridge hub — the same two dependencies the goal steerer takes.
func newBridgeLiveInjector(instances goalInstanceLister, bridge goalBridgeDoer) *bridgeLiveInjector {
	return &bridgeLiveInjector{instances: instances, bridge: bridge}
}

// InjectContext locates the live instance serving the task and delivers the
// supervisor context into its most recent session via prompt_async.
//
// Returns (injected, sessionID, err):
//   - (false, "", nil) graceful skip: nil wiring, no live instance, no session
//     discovered yet, or a non-OpenCode executor (Pi has no prompt endpoint).
//     The caller falls through to the relaunch path.
//   - (true, sessionID, nil): delivered.
//   - (false, "", err): delivery attempted and failed.
func (i *bridgeLiveInjector) InjectContext(ctx context.Context, projectID, taskID, injectedContext string) (bool, string, error) {
	if i == nil || i.instances == nil || i.bridge == nil {
		return false, "", nil
	}

	inst := i.findTaskInstance(ctx, projectID, taskID)
	if inst == nil {
		return false, "", nil
	}
	// Only OpenCode instances expose prompt_async. Pi (or anything else) is a
	// graceful skip → relaunch.
	if inst.Executor != "" && inst.Executor != "opencode" {
		return false, "", nil
	}
	if len(inst.SessionIDs) == 0 {
		return false, "", nil
	}
	sessionID := inst.SessionIDs[len(inst.SessionIDs)-1]

	// Same upstream shape HandleControlPrompt / the goal steerer send.
	body, err := json.Marshal(map[string]interface{}{
		"parts": []map[string]interface{}{
			{"type": "text", "text": injectedContext},
		},
	})
	if err != nil {
		return false, "", fmt.Errorf("marshal injected context: %w", err)
	}

	path := fmt.Sprintf("/session/%s/prompt_async", url.PathEscape(sessionID))
	status, _, err := i.bridge.Do(ctx, inst.RunnerID, inst.InstanceID, http.MethodPost, path, body)
	if err != nil {
		return false, "", fmt.Errorf("bridge inject to %s/%s session %s: %w",
			inst.RunnerID, inst.InstanceID, sessionID, err)
	}
	if status >= http.StatusMultipleChoices && status != 0 {
		return false, "", fmt.Errorf("bridge inject to session %s: upstream status %d", sessionID, status)
	}
	return true, sessionID, nil
}

// findTaskInstance mirrors bridgeGoalSteerer.findTaskInstance: the live
// task-kind instance serving taskID, project-matched when both sides carry
// it, exited instances ignored.
func (i *bridgeLiveInjector) findTaskInstance(ctx context.Context, projectID, taskID string) *types.OpencodeInstance {
	resp, err := i.instances.ListAllInstances(ctx)
	if err != nil || resp == nil {
		return nil
	}
	for idx := range resp.Instances {
		inst := &resp.Instances[idx]
		if inst.Kind != types.InstanceKindTask || inst.TaskID != taskID {
			continue
		}
		if projectID != "" && inst.ProjectID != "" && inst.ProjectID != projectID {
			continue
		}
		if inst.Status == "exited" {
			continue
		}
		return inst
	}
	return nil
}
