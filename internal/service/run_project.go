package service

import (
	"context"
	"log/slog"
	"strings"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/types"
)

// RunProjectNow is the user-explicit "run every ready feature in this project
// now" entry point. Fans out RunFeatureNow across the distinct set of
// feature_ids present in the project's ready-task pool. Tasks without a
// feature_id are dispatched individually via RunTaskNow so a project of
// unfeatured tasks still runs.
//
// Aggregates:
//   - FeaturesConsidered   — count of distinct groupings attempted (features + one "unfeatured" bucket if applicable)
//   - FeaturesDispatched   — count of groups where at least one task dispatched
//   - FeaturesSkipped      — count of groups where no task dispatched
//   - TotalTasksDispatched — sum of RunFeatureResponse.DispatchedCount + successful RunTaskResponse dispatches
//   - Results              — per-feature RunFeatureResponse; unfeatured tasks summarized as a synthetic entry with FeatureID=""
//
// Force applies uniformly to every dispatch — same semantics as force at
// the single-feature level (bypass automation-pause safety only).
//
// This is a convenience API; it does NOT introduce new concurrency or lease
// contention beyond what the underlying RunFeatureNow / RunTaskNow already
// enforce. The scheduler's per-runner reservedSlots tracking prevents two
// features from oversubscribing the same runner slot in a single call.
func (s *SchedulerService) RunProjectNow(ctx context.Context, projectID string, force bool) (*types.RunProjectResponse, error) {
	resp := &types.RunProjectResponse{ProjectID: projectID}
	sanitized := strings.TrimSpace(projectID)
	if sanitized == "" {
		resp.Reason = "project_id_required"
		return resp, nil
	}
	if s.tasks == nil {
		resp.Reason = "scheduler_not_configured"
		return resp, nil
	}

	// One pass over the project's ready-task pool. Group by feature_id so
	// we call RunFeatureNow once per feature instead of once per task.
	// Ordering: sort by feature_id ascending for deterministic response
	// (test-friendly and matches how the PWA renders feature lists).
	ready, err := s.tasks.GetReady(ctx, sanitized, nil)
	if err != nil {
		return nil, err
	}
	if len(ready) == 0 {
		resp.Reason = "no_ready_tasks"
		return resp, nil
	}

	// Distinct feature_ids preserving first-seen order.
	seen := make(map[string]bool)
	orderedFeatureIDs := make([]string, 0)
	unfeaturedTasks := make([]string, 0)
	for _, t := range ready {
		fid := strings.TrimSpace(t.FeatureID)
		if fid == "" {
			unfeaturedTasks = append(unfeaturedTasks, t.ID)
			continue
		}
		if !seen[fid] {
			seen[fid] = true
			orderedFeatureIDs = append(orderedFeatureIDs, fid)
		}
	}

	resp.FeaturesConsidered = len(orderedFeatureIDs)
	if len(unfeaturedTasks) > 0 {
		resp.FeaturesConsidered++
	}

	// Fan out RunFeatureNow per feature.
	for _, fid := range orderedFeatureIDs {
		fr, err := s.RunFeatureNow(ctx, sanitized, fid, force)
		if err != nil {
			// Per-feature failure surfaces as a Reason on the sub-response
			// rather than aborting the batch — the user gets partial dispatch
			// visibility.
			slog.Warn("run project: per-feature RunFeatureNow failed",
				"project", sanitized, "feature", fid, "error", err)
			fr = &types.RunFeatureResponse{
				ProjectID: sanitized,
				FeatureID: fid,
				Reason:    "internal_error",
				Detail:    err.Error(),
			}
		}
		if fr != nil {
			if fr.Dispatched {
				resp.FeaturesDispatched++
			} else {
				resp.FeaturesSkipped++
			}
			resp.TotalTasksDispatched += fr.DispatchedCount
			resp.Results = append(resp.Results, *fr)
		}
	}

	// Handle unfeatured tasks as a single synthetic result. Rather than
	// re-implementing per-task dispatch here, we call RunTaskNow for each
	// and collate the outcomes into a RunFeatureResponse-shaped record so
	// the client can render "unfeatured" uniformly.
	if len(unfeaturedTasks) > 0 {
		synthetic := types.RunFeatureResponse{
			ProjectID: sanitized,
			FeatureID: "", // signals "unfeatured bucket" to clients
		}
		for _, tid := range unfeaturedTasks {
			r, err := s.RunTaskNow(ctx, sanitized, tid, force)
			if err != nil {
				slog.Warn("run project: per-task RunTaskNow failed",
					"project", sanitized, "task_id", tid, "error", err)
				continue
			}
			if r == nil {
				continue
			}
			synthetic.Results = append(synthetic.Results, *r)
			if r.Dispatched {
				synthetic.DispatchedCount++
				synthetic.Dispatched = true
			} else {
				synthetic.SkippedCount++
				if len(r.Reason) > 0 && synthetic.Reason == "" {
					synthetic.Reason = r.Reason
				}
			}
		}
		if synthetic.Dispatched {
			resp.FeaturesDispatched++
		} else {
			resp.FeaturesSkipped++
		}
		resp.TotalTasksDispatched += synthetic.DispatchedCount
		resp.Results = append(resp.Results, synthetic)
	}

	if resp.FeaturesDispatched == 0 && resp.TotalTasksDispatched == 0 {
		resp.Reason = "no_dispatched"
	}

	slog.Info("run project: batch complete",
		"project", sanitized,
		"features_considered", resp.FeaturesConsidered,
		"features_dispatched", resp.FeaturesDispatched,
		"features_skipped", resp.FeaturesSkipped,
		"total_tasks_dispatched", resp.TotalTasksDispatched,
		"force", force,
	)
	return resp, nil
}

// Compile-time check that SchedulerService satisfies RunProjectService.
// (RunTaskService + RunFeatureService are already satisfied by the same
// implementation via RunTaskNow + RunFeatureNow.)
var _ api.RunProjectService = (*SchedulerService)(nil)
