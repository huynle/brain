package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// RunFeatureNow is the user-explicit "run this entire feature now" entry point.
//
// It loads every ready task in the feature, sorts by priority then by ID for
// stable ordering, and dispatches as many as runner capacity allows in a
// single batch — tracking reservedSlots locally so two tasks in this call
// can't both target the same single-slot runner. Tasks that can't dispatch
// because runners are at capacity (or the task is already leased) are
// recorded in Queued; FeatureCascadeService then drains them as in-flight
// tasks complete, even while the project is paused.
//
// Pause is unconditionally bypassed: this method does not consult
// shouldSkipTask. The behaviour matches RunTaskNow's documented contract
// — pause is meant to halt automatic scheduling, not block manual overrides.
//
// Returned reasons (when Dispatched=false):
//   - "scheduler_not_configured" — required dependencies missing
//   - "feature_not_found"        — featureID is empty
//   - "no_ready_tasks"           — feature has no ready tasks right now
//   - "feature_in_progress"      — every ready task was already in flight
//
// Errors are returned only for unexpected infrastructure problems.
func (s *SchedulerService) RunFeatureNow(ctx context.Context, projectID, featureID string, force bool) (*types.RunFeatureResponse, error) {
	resp := &types.RunFeatureResponse{ProjectID: projectID, FeatureID: featureID}

	if s.runners == nil || s.placement == nil || s.leases == nil {
		resp.Reason = "scheduler_not_configured"
		resp.Detail = "scheduler is missing dependencies required for dispatch"
		return resp, nil
	}
	if strings.TrimSpace(featureID) == "" {
		resp.Reason = "feature_not_found"
		resp.Detail = "featureID is required"
		return resp, nil
	}

	tasks, err := s.tasks.GetReady(ctx, projectID, &api.TaskFilterOptions{FeatureIDs: []string{featureID}})
	if err != nil {
		return nil, fmt.Errorf("load ready tasks for feature %q: %w", featureID, err)
	}
	if len(tasks) == 0 {
		resp.Reason = "no_ready_tasks"
		resp.Detail = "no ready tasks in this feature; check dependencies or in-progress state"
		return resp, nil
	}

	runners, err := s.runners.ListRunners(ctx)
	if err != nil {
		return nil, fmt.Errorf("list runners: %w", err)
	}
	placement, err := s.placement.Get(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get project placement: %w", err)
	}
	if placement == nil {
		placement = &types.ProjectPlacement{ProjectID: projectID, Affinity: types.PlacementAffinitySoft}
	}

	// Stable iteration: priority desc, then task ID asc.
	sort.SliceStable(tasks, func(i, j int) bool {
		pi := priorityRank(tasks[i].Priority)
		pj := priorityRank(tasks[j].Priority)
		if pi != pj {
			return pi > pj
		}
		return tasks[i].ID < tasks[j].ID
	})

	resp.Results = make([]types.RunTaskResponse, 0, len(tasks))
	reservedSlots := make(map[string]int)

	for i := range tasks {
		task := &tasks[i]
		one := types.RunTaskResponse{ProjectID: projectID, TaskID: task.ID}

		// Empty runner list: surface a single clear reason per task; do
		// not queue because the cascade can't fix "no runners online".
		if len(runners) == 0 {
			one.Reason = "no_online_runner"
			one.Detail = "no runners are registered"
			resp.Results = append(resp.Results, one)
			resp.SkippedCount++
			continue
		}

		candidate, reasons := s.selectCandidate(*task, projectID, runners, placement, reservedSlots)
		if candidate == nil {
			one.Reason = "no_eligible_runner"
			if len(reasons) > 0 {
				one.Detail = strings.Join(reasons, "; ")
			}
			resp.Results = append(resp.Results, one)
			resp.SkippedCount++
			// Queue: capacity-driven skips resolve as in-flight tasks
			// complete. Other no_eligible_runner causes (executor
			// mismatch, missing workspace root, labels) won't fix
			// themselves but cascading is cheap and self-limiting — a
			// cascade tick that hits the same skip just no-ops.
			resp.Queued = append(resp.Queued, task.ID)
			continue
		}

		now := s.nowUnixMS()
		lease, created, leaseErr := s.leases.CreateDispatchLease(ctx, storage.DispatchLeaseCreate{
			ProjectID:         projectID,
			TaskID:            task.ID,
			AssignedRunnerID:  candidate.RunnerID,
			AssignedMachineID: candidate.MachineID,
			PushedAt:          now,
			ExpiresAt:         now + s.leaseTTL.Milliseconds(),
		})
		if leaseErr != nil {
			return nil, fmt.Errorf("create dispatch lease for %s: %w", task.ID, leaseErr)
		}
		if !created {
			// Task is already leased to another runner. Surface the
			// existing owner so the user can see what's happening, and
			// queue so the cascade re-evaluates when the lease releases.
			existing, lookupErr := s.leases.GetDispatchLeaseRow(ctx, projectID, task.ID)
			if lookupErr != nil {
				return nil, fmt.Errorf("lookup existing dispatch lease: %w", lookupErr)
			}
			one.Reason = "already_leased"
			one.Detail = "task already has an active dispatch lease"
			if existing != nil {
				one.RunnerID = existing.AssignedRunnerID
				one.LeaseID = existing.LeaseID
				one.LeaseState = existing.State
				if existing.ExpiresAt > 0 {
					one.ExpiresAt = time.UnixMilli(existing.ExpiresAt).UTC().Format(time.RFC3339)
				}
			}
			resp.Results = append(resp.Results, one)
			resp.SkippedCount++
			resp.Queued = append(resp.Queued, task.ID)
			continue
		}

		if s.publisher != nil {
			payload := map[string]any{
				"taskId":    task.ID,
				"projectId": projectID,
				"lease":     lease,
				"expiresAt": lease.ExpiresAt,
				// Inline the resolved task so the runner can process
				// this dispatch without an HTTP round-trip back to
				// GetReadyTasks.
				"task": task,
			}
			if force {
				payload["force"] = true
			}
			s.publisher.PublishRunnerCommand(candidate.RunnerID, "dispatch", payload)
		}

		reservedSlots[candidate.RunnerID]++
		one.Dispatched = true
		one.RunnerID = candidate.RunnerID
		one.LeaseID = lease.LeaseID
		if lease.ExpiresAt > 0 {
			one.ExpiresAt = time.UnixMilli(lease.ExpiresAt).UTC().Format(time.RFC3339)
		}
		resp.Results = append(resp.Results, one)
		resp.DispatchedCount++
	}

	resp.Dispatched = resp.DispatchedCount > 0
	if !resp.Dispatched && len(resp.Queued) == 0 && resp.SkippedCount > 0 {
		resp.Reason = "feature_in_progress"
		resp.Detail = "every ready task was already in flight or otherwise unrunnable"
	}

	// Register the cascade when there's leftover work that will resolve
	// as in-flight tasks complete, OR when we dispatched at least one
	// task in this feature (so subsequent completions inside the feature
	// continue to feed any tasks that become ready due to dependency
	// satisfaction). The cascade self-unregisters once the feature has
	// no ready tasks left.
	if s.cascade != nil && (len(resp.Queued) > 0 || resp.Dispatched) {
		s.cascade.Register(projectID, featureID)
		resp.CascadeActive = true
	} else if s.cascade != nil {
		// Caller's view should reflect existing cascade state even when
		// this call dispatched nothing new.
		resp.CascadeActive = s.cascade.IsActive(projectID, featureID)
	}

	return resp, nil
}
