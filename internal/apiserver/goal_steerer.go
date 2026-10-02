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
// Goal session steerer
//
// Production implementation of service.SessionSteerer. It reuses the same
// in-process control plumbing the /control handlers use: the runner instance
// registry locates the live instance serving a task (the way abort-task
// resolution works), and the runner bridge delivers the prompt through the
// OpenCode /session/{id}/prompt proxy path. No HTTP self-calls.
// =============================================================================

// goalInstanceLister is the slice of the runner registry the steerer needs.
type goalInstanceLister interface {
	ListAllInstances(ctx context.Context) (*types.InstanceListResponse, error)
}

// goalBridgeDoer is the slice of the bridge hub the steerer needs.
type goalBridgeDoer interface {
	Do(ctx context.Context, runnerID, instanceID, method, path string, body []byte) (int, []byte, error)
}

// bridgeGoalSteerer steers a task's live OpenCode session via the runner
// bridge. Compile-time checked against the service contract.
type bridgeGoalSteerer struct {
	instances goalInstanceLister
	bridge    goalBridgeDoer
}

var _ service.SessionSteerer = (*bridgeGoalSteerer)(nil)

// newBridgeGoalSteerer wires the steerer from the instance registry and the
// runner bridge hub.
func newBridgeGoalSteerer(instances goalInstanceLister, bridge goalBridgeDoer) *bridgeGoalSteerer {
	return &bridgeGoalSteerer{instances: instances, bridge: bridge}
}

// SteerTask locates the live instance serving the task and injects the prompt
// into its most recent session via the v2 /session/{id}/prompt endpoint.
//
// Graceful skips (SteerResult with Steered=false, nil error): no live task
// instance, no discovered session yet, or a non-OpenCode executor (e.g. Pi
// RPC processes expose no prompt endpoint — Unsupported=true). Delivery
// failures return an error.
func (s *bridgeGoalSteerer) SteerTask(ctx context.Context, projectID, taskID, prompt string) (service.SteerResult, error) {
	if s == nil || s.instances == nil || s.bridge == nil {
		return service.SteerResult{Reason: "steerer not wired"}, nil
	}

	inst := s.findTaskInstance(ctx, projectID, taskID)
	if inst == nil {
		return service.SteerResult{Reason: "no live instance for task"}, nil
	}
	// Only OpenCode instances expose the prompt endpoint. Anything else
	// (e.g. "pi") is skipped as unsupported rather than errored.
	if inst.Executor != "" && inst.Executor != "opencode" {
		return service.SteerResult{
			Unsupported: true,
			Reason:      fmt.Sprintf("executor %q does not support prompt injection", inst.Executor),
		}, nil
	}
	if len(inst.SessionIDs) == 0 {
		return service.SteerResult{Reason: "no session discovered for instance yet"}, nil
	}
	sessionID := inst.SessionIDs[len(inst.SessionIDs)-1]

	// v2 prompt body: a single "text" field (the v1 {parts:[…]} shape is gone;
	// POST /api/session/{id}/prompt takes {"text":"…"}).
	body, err := json.Marshal(map[string]interface{}{
		"text": prompt,
	})
	if err != nil {
		return service.SteerResult{}, fmt.Errorf("marshal steering prompt: %w", err)
	}

	// v2 endpoint is /session/{id}/prompt (NOT /prompt_async). The bridge
	// proxy /api-prefixes it to /api/session/{id}/prompt.
	path := fmt.Sprintf("/session/%s/prompt", url.PathEscape(sessionID))
	status, _, err := s.bridge.Do(ctx, inst.RunnerID, inst.InstanceID, http.MethodPost, path, body)
	if err != nil {
		return service.SteerResult{}, fmt.Errorf("bridge prompt to %s/%s session %s: %w",
			inst.RunnerID, inst.InstanceID, sessionID, err)
	}
	if status >= http.StatusMultipleChoices && status != 0 {
		return service.SteerResult{}, fmt.Errorf("bridge prompt to session %s: upstream status %d", sessionID, status)
	}
	return service.SteerResult{Steered: true}, nil
}

// findTaskInstance returns the live task-kind instance serving the given
// task, or nil. Project is matched when both sides carry it; exited
// instances are ignored.
func (s *bridgeGoalSteerer) findTaskInstance(ctx context.Context, projectID, taskID string) *types.OpencodeInstance {
	resp, err := s.instances.ListAllInstances(ctx)
	if err != nil || resp == nil {
		return nil
	}
	for i := range resp.Instances {
		inst := &resp.Instances[i]
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
