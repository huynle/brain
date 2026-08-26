package service

import (
	"context"
	"fmt"
	"log/slog"
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
	return s.RunFeatureWithOptions(ctx, projectID, featureID, RunFeatureOptions{Force: force})
}

// RunFeatureOptions carries the manual-run switches.
type RunFeatureOptions struct {
	Force bool
	// IncludeDependents enrols the transitive dependents of this feature as
	// a standing request, so each runs as its gate opens.
	//
	// The cascade itself must NEVER set this. Propagation re-derives the
	// chain from the stored root on every sweep; a cascade tick that
	// re-enrolled would create a second root per member and fan out
	// combinatorially.
	IncludeDependents bool
}

// RunFeatureWithOptions is RunFeatureNow plus the dependent-chain option.
func (s *SchedulerService) RunFeatureWithOptions(ctx context.Context, projectID, featureID string, opts RunFeatureOptions) (*types.RunFeatureResponse, error) {
	// opts.Force is accepted for symmetry with RunTaskNow but is not read:
	// a manual feature run ALWAYS dispatches with force=true (see the
	// payload built below), because bypassing the project pause dial is the
	// entire point of running one feature by hand. Noted rather than
	// removed so the next reader does not go looking for the branch.
	resp := &types.RunFeatureResponse{ProjectID: projectID, FeatureID: featureID}

	// Enrolment happens BEFORE any early return below.
	//
	// The natural moment to say "and queue the chain" is when the root is
	// already running — every task in flight, nothing ready. That path
	// returns early with "no_ready_tasks", so enrolling afterwards would
	// make the option a silent no-op in one of its most common uses.
	if opts.IncludeDependents && strings.TrimSpace(featureID) != "" {
		if q, err := s.enrolDependents(ctx, projectID, featureID); err != nil {
			slog.Warn("enrol dependent chain failed",
				"project_id", projectID, "feature_id", featureID, "error", err)
		} else {
			resp.Dependents = q
		}
	}

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

	// Outstanding is computed BEFORE the early return below, because the
	// early return is exactly the case the cascade misread: "no ready
	// tasks" is not "feature finished". Unknown (-1) when no feature-task
	// lister is wired, so callers can tell "nothing left" from "cannot
	// tell" instead of reading a default 0 as drained.
	resp.Outstanding = s.featureOutstanding(ctx, projectID, featureID)

	tasks, err := s.tasks.GetReady(ctx, projectID, &api.TaskFilterOptions{FeatureIDs: []string{featureID}})
	if err != nil {
		return nil, fmt.Errorf("load ready tasks for feature %q: %w", featureID, err)
	}
	if len(tasks) == 0 {
		resp.Reason = "no_ready_tasks"
		resp.Detail = "no ready tasks in this feature; check dependencies or in-progress state"
		if s.cascade != nil {
			resp.CascadeActive = s.cascade.IsActive(projectID, featureID)
		}
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

// featureOutstanding counts tasks in the feature that can still produce work
// without a human: status pending or in_progress.
//
// Returns nil when the lister is not wired or the query failed, which
// callers must treat as "unknown" rather than "drained". Terminal statuses (completed, validated,
// archived, cancelled) and blocked do not count: blocked is where the retry
// cap parks a task after max_attempts, and continuing to wait on it would
// keep a cascade alive forever.
func (s *SchedulerService) featureOutstanding(ctx context.Context, projectID, featureID string) *int {
	if s.featTasks == nil {
		return nil
	}
	tasks, err := s.featTasks.GetTasksByFeature(ctx, projectID, featureID)
	if err != nil {
		slog.Warn("feature outstanding count failed",
			"project_id", projectID, "feature_id", featureID, "error", err)
		return nil
	}
	n := 0
	for i := range tasks {
		switch tasks[i].Status {
		case "pending", "in_progress":
			n++
		}
	}
	return &n
}

// =============================================================================
// Dependent chains
// =============================================================================

// projectFeatures resolves the current feature graph for a project.
func (s *SchedulerService) projectFeatures(ctx context.Context, projectID string) ([]*ComputedFeature, error) {
	if s.projTasks == nil {
		return nil, fmt.Errorf("project task lister not wired")
	}
	result, err := s.projTasks.GetTasks(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("load tasks for %q: %w", projectID, err)
	}
	return ResolveFeatureDependencies(ComputeFeatures(result.Tasks)), nil
}

// enrolDependents records the standing request and returns the chain it
// covers, derived from the current graph.
func (s *SchedulerService) enrolDependents(ctx context.Context, projectID, rootFeatureID string) (*types.DependentQueue, error) {
	features, err := s.projectFeatures(ctx, projectID)
	if err != nil {
		return nil, err
	}
	closure := TransitiveDependents(features, rootFeatureID)

	// Persist the ROOT, capturing whether the project's task dial was
	// already off. Propagation force-dispatches past a pause the user was
	// already working under; a pause applied LATER stops the chain
	// spreading into features that have not started. Those two cases are
	// indistinguishable without this snapshot.
	if s.roots != nil {
		pausedNow := s.pauses != nil && s.pauses.IsPaused(projectID)
		if err := s.roots.UpsertFeatureCascadeRoot(ctx, projectID, rootFeatureID, pausedNow); err != nil {
			return nil, fmt.Errorf("persist cascade root: %w", err)
		}
	}

	q := &types.DependentQueue{
		Queued:          closure.Members,
		WaitsOnExternal: closure.External,
		Truncated:       closure.Truncated,
	}
	if len(closure.Skipped) > 0 {
		q.Skipped = closure.Skipped
	}
	return q, nil
}

// CancelDependentChain drops a standing run-with-dependents request.
//
// Reports whether anything was actually cancelled, so a caller can tell
// "stopped a running chain" from "there was nothing queued" — the latter
// silently reported as success is how a user concludes cancel is broken.
func (s *SchedulerService) CancelDependentChain(ctx context.Context, projectID, rootFeatureID string) (bool, error) {
	if s.roots == nil {
		return false, fmt.Errorf("cascade root store not wired")
	}
	return s.roots.DeleteFeatureCascadeRoot(ctx, projectID, rootFeatureID)
}

// ListDependentChains returns the standing requests for a project.
func (s *SchedulerService) ListDependentChains(ctx context.Context, projectID string) ([]types.DependentChain, error) {
	if s.roots == nil {
		return nil, nil
	}
	rows, err := s.roots.ListFeatureCascadeRoots(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	features, ferr := s.projectFeatures(ctx, projectID)
	out := make([]types.DependentChain, 0, len(rows))
	for _, r := range rows {
		c := types.DependentChain{
			ProjectID:       r.ProjectID,
			RootFeatureID:   r.RootFeatureID,
			RequestedAt:     r.RequestedAt,
			PausedAtRequest: r.PausedAtRequest,
		}
		// The chain is re-derived, never read back from storage, so it
		// reflects edits to feature_depends_on and features whose tasks
		// only appeared after the request.
		if ferr == nil {
			closure := TransitiveDependents(features, r.RootFeatureID)
			c.Queued = closure.Members
			c.WaitsOnExternal = closure.External
			c.Truncated = closure.Truncated
			if len(closure.Skipped) > 0 {
				c.Skipped = closure.Skipped
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// SweepDependentChains advances every standing chain in a project: for each
// stored root, re-derive the chain and dispatch any member that has become
// ready.
//
// Re-deriving rather than replaying a stored member list is what lets a chain
// pick up features whose tasks were generated after the request (feature
// checkout follow-ups, goal work) and drop ones whose dependencies were
// edited away.
//
// Returns the number of features dispatched.
func (s *SchedulerService) SweepDependentChains(ctx context.Context, projectID string) int {
	if s.roots == nil {
		return 0
	}
	rows, err := s.roots.ListFeatureCascadeRoots(ctx, projectID)
	if err != nil {
		slog.Warn("sweep dependent chains: list roots failed",
			"project_id", projectID, "error", err)
		return 0
	}
	if len(rows) == 0 {
		return 0
	}

	// Group by the row's OWN project, never the parameter.
	//
	// The ticker sweeps with an empty projectID meaning "every project",
	// which the storage layer honours for the LIST — but every call after
	// that (resolve the graph, check the pause dial, dispatch, retire) is
	// project-scoped. Using the parameter there passed "" downstream and the
	// retire failed with "project id and root feature id are required". The
	// event path masked it because it always carries a real project.
	byProject := map[string][]storage.FeatureCascadeRootRow{}
	for _, r := range rows {
		byProject[r.ProjectID] = append(byProject[r.ProjectID], r)
	}

	dispatched := 0
	for proj, projRows := range byProject {
		dispatched += s.sweepProjectChains(ctx, proj, projRows)
	}
	return dispatched
}

// sweepProjectChains advances every standing chain in ONE project. Resolving
// the graph once per project rather than once per chain keeps a project with
// several chains to a single task load.
func (s *SchedulerService) sweepProjectChains(ctx context.Context, projectID string, rows []storage.FeatureCascadeRootRow) int {
	features, err := s.projectFeatures(ctx, projectID)
	if err != nil {
		slog.Warn("sweep dependent chains: resolve features failed",
			"project_id", projectID, "error", err)
		return 0
	}
	byID := make(map[string]*ComputedFeature, len(features))
	for _, f := range features {
		byID[f.ID] = f
	}

	// The pause rule, evaluated once per project per sweep.
	//
	// A pause the user was ALREADY working under is the isolate workflow —
	// propagation must cross it, or the option does nothing in the only
	// situation where it matters. A pause applied AFTER the request is a
	// deliberate brake: stop spreading into features that have not started.
	// Work already dispatched is unaffected either way; this gates only NEW
	// features joining.
	pausedNow := s.pauses != nil && s.pauses.IsPaused(projectID)

	dispatched := 0
	for _, r := range rows {
		if pausedNow && !r.PausedAtRequest {
			slog.Info("dependent chain held: project paused after the request",
				"project_id", projectID, "root_feature_id", r.RootFeatureID)
			continue
		}

		closure := TransitiveDependents(features, r.RootFeatureID)

		if s.chainSettled(byID, r.RootFeatureID, closure.Members) {
			if _, derr := s.roots.DeleteFeatureCascadeRoot(ctx, projectID, r.RootFeatureID); derr != nil {
				slog.Warn("retire dependent chain failed",
					"project_id", projectID, "root_feature_id", r.RootFeatureID, "error", derr)
			} else {
				slog.Info("dependent chain complete",
					"project_id", projectID, "root_feature_id", r.RootFeatureID)
			}
			continue
		}

		// The ROOT is swept alongside its members.
		//
		// TransitiveDependents deliberately excludes the root — it is
		// dispatched inline by the call that created the chain. But that
		// only covers the first pass. After a server restart, or when the
		// root was not yet ready at click time (its own feature_depends_on
		// unmet, or every task already in flight), nothing would ever
		// re-dispatch it and the whole chain would sit stranded on its own
		// root. Verified live: a chain persisted across an API restart
		// never resumed until the root was included here.
		for _, id := range append([]string{r.RootFeatureID}, closure.Members...) {
			f := byID[id]
			if f == nil {
				continue
			}
			// Pending count FIRST, and it is not redundant with the
			// classification check below.
			//
			// classifyFeature returns "ready" for a feature whose status is
			// completed or archived — "already settled, no classification
			// needed". So Classification alone does NOT mean "has work to
			// dispatch": a finished feature reads ready forever, and this
			// loop re-dispatched it on every sweep. Observed live as
			// data-pipeline running a second time after it had completed.
			if f.TaskStats.Pending == 0 {
				continue
			}
			if f.Classification != "ready" {
				// Waiting or blocked is not an error: the member is
				// waiting its turn. Calling RunFeatureNow anyway would
				// return no_ready_tasks and cost a full project resolve.
				continue
			}
			resp, rerr := s.RunFeatureNow(ctx, projectID, id, true)
			if rerr != nil {
				slog.Warn("dependent chain dispatch failed",
					"project_id", projectID, "root_feature_id", r.RootFeatureID,
					"feature_id", id, "error", rerr)
				continue
			}
			if resp != nil && resp.Dispatched {
				dispatched += resp.DispatchedCount
				slog.Info("dependent chain advanced",
					"project_id", projectID, "root_feature_id", r.RootFeatureID,
					"feature_id", id, "dispatched", resp.DispatchedCount)
			}
		}
	}
	return dispatched
}

// chainSettled reports whether the root and every member has finished, so the
// standing request can be retired.
//
// A member that is blocked counts as settled: that is where the retry cap
// parks a task after max_attempts, and waiting on it would keep the chain
// alive forever on work that cannot proceed without a human.
func (s *SchedulerService) chainSettled(byID map[string]*ComputedFeature, root string, members []string) bool {
	for _, id := range append([]string{root}, members...) {
		f, ok := byID[id]
		if !ok {
			continue
		}
		if f.TaskStats.Pending > 0 || f.TaskStats.InProgress > 0 {
			return false
		}
	}
	return true
}

// RunFeatureWithDependents adapts RunFeatureWithOptions to a primitive
// signature so internal/api can depend on it without closing the import cycle
// (internal/service already imports internal/api).
func (s *SchedulerService) RunFeatureWithDependents(ctx context.Context, projectID, featureID string, force, includeDependents bool) (*types.RunFeatureResponse, error) {
	return s.RunFeatureWithOptions(ctx, projectID, featureID, RunFeatureOptions{
		Force:             force,
		IncludeDependents: includeDependents,
	})
}
