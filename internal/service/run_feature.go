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
//   - "feature_in_progress"      — every ready task was already leased
//   - "no_online_runner"         — no runners are registered
//   - "no_eligible_runner"       — runners exist but none can take these
//     tasks; Detail names the runner and why (project not allowed, executor
//     unsupported, at capacity, …)
//
// The last two are the per-task skip reasons promoted to the response: when
// nothing dispatched, the whole feature's outcome is the dominant per-task
// cause, so a caller that only reads Reason/Detail still learns why.
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
				// RunFeatureNow is user-initiated, so always tell the
				// runner this is a manual override — bypassing its pause
				// gate. See scheduler.go RunTaskNow for the full rationale.
				"force": true,
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
	if !resp.Dispatched && resp.SkippedCount > 0 {
		// Nothing dispatched: promote the dominant per-task skip reason to
		// the response so callers get a cause, not silence. Leaving Reason
		// empty here was the bug behind "Run feature now does nothing" —
		// every task skipped for `no_eligible_runner`, the whole feature
		// queued, and the PWA had nothing to render but "nothing to
		// dispatch" while `results[i].detail` held the real answer
		// ("runner-a: project not allowed").
		reason, detail := dominantSkipReason(resp.Results)
		switch reason {
		case "", "already_leased":
			// Leases are held by runners actively working the feature, so
			// this stays the pre-existing in-flight reason (which the
			// cascade and the PWA both already understand).
			resp.Reason = "feature_in_progress"
			resp.Detail = "every ready task was already in flight or otherwise unrunnable"
		default:
			resp.Reason = reason
			resp.Detail = detail
		}
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

// dominantSkipReason picks the per-task skip reason that explains the most
// tasks in a run, plus a representative Detail for it. Ties break on the
// reason string so the response is deterministic for a given input.
//
// It exists so a caller reading only Reason/Detail — a toast, a log line, an
// MCP tool result — gets the same answer as one that walks every entry in
// Results. Dispatched entries and entries without a reason are ignored.
func dominantSkipReason(results []types.RunTaskResponse) (string, string) {
	counts := make(map[string]int, len(results))
	for _, one := range results {
		if one.Dispatched || one.Reason == "" {
			continue
		}
		counts[one.Reason]++
	}
	if len(counts) == 0 {
		return "", ""
	}

	reason := ""
	for candidate, n := range counts {
		if n > counts[reason] || (n == counts[reason] && candidate < reason) {
			reason = candidate
		}
	}

	// Representative detail: the first one seen for the winning reason, plus
	// a count when the tasks disagree (two runners, two different excuses).
	detail := ""
	distinct := 0
	seen := make(map[string]bool, len(results))
	for _, one := range results {
		if one.Dispatched || one.Reason != reason || one.Detail == "" || seen[one.Detail] {
			continue
		}
		seen[one.Detail] = true
		distinct++
		if detail == "" {
			detail = one.Detail
		}
	}
	if distinct > 1 {
		detail = fmt.Sprintf("%s (+%d other reason(s))", detail, distinct-1)
	}
	return reason, detail
}
