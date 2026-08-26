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
// force reclaims a dispatch lease that was pushed to a runner and never
// acknowledged. It does NOT touch an acknowledged lease — see
// reclaimableLease for why that distinction is the whole safety argument.
//
// Returned reasons (when Dispatched=false):
//   - "scheduler_not_configured" — required dependencies missing
//   - "feature_not_found"        — featureID is empty
//   - "no_ready_tasks"           — feature has no ready tasks right now;
//     Detail and WaitingOnFeatures/BlockedByFeatures name the features it is
//     gated behind when that is why
//   - "feature_in_progress"      — a runner acknowledged every ready task
//     and is working it
//   - "feature_dispatch_pending" — every ready task is held by a lease that
//     was pushed and never acknowledged: nothing is running, and the holds
//     clear on their own when the leases expire
//   - "no_online_runner"         — no runners are registered
//   - "no_eligible_runner"       — runners exist but none can take these
//     tasks; Detail names the runner and why (project not allowed, executor
//     unsupported, at capacity, …)
//   - "runner_unreachable"       — placement picked a runner but its command
//     stream is not connected, so the dispatch was not delivered
//
// The last three are the per-task skip reasons promoted to the response: when
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
	// Two different "forces" meet here, and conflating them is a trap.
	//
	// The dispatch PAYLOAD always carries force=true, which is what makes
	// the runner bypass the project pause dial — the whole point of running
	// one feature by hand.
	//
	// opts.Force is narrower and separate: it reclaims an UNACKNOWLEDGED
	// dispatch lease (see reclaimableLease below), never an acknowledged
	// one. It exists so a lease left behind by a command that never reached
	// its runner does not block an explicit user request.
	force := opts.Force
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

	// The snapshot is taken BEFORE the early return below, because the
	// early return is exactly the case the cascade misread: "no ready
	// tasks" is not "feature finished". Outstanding is nil when no
	// feature-task lister is wired, so callers can tell "nothing left"
	// from "cannot tell" instead of reading a default 0 as drained.
	snap := s.featureTaskSnapshot(ctx, projectID, featureID)
	resp.Outstanding = snap.Outstanding
	resp.WaitingOnFeatures = snap.WaitingOn
	resp.BlockedByFeatures = snap.BlockedBy

	tasks, err := s.tasks.GetReady(ctx, projectID, &api.TaskFilterOptions{FeatureIDs: []string{featureID}})
	if err != nil {
		return nil, fmt.Errorf("load ready tasks for feature %q: %w", featureID, err)
	}
	if len(tasks) == 0 {
		resp.Reason = "no_ready_tasks"
		resp.Detail = noReadyTasksDetail(snap)
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
			// Task is already leased. Look the holder up so the user can
			// see who has it and in what state — "already leased" alone
			// cannot distinguish a runner working the task from a
			// dispatch that never got an answer.
			existing, lookupErr := s.leases.GetDispatchLeaseRow(ctx, projectID, task.ID)
			if lookupErr != nil {
				return nil, fmt.Errorf("lookup existing dispatch lease: %w", lookupErr)
			}
			if force && reclaimableLease(existing) {
				lease, created, leaseErr = s.reclaimLease(ctx, projectID, task.ID, existing, candidate, now)
				if leaseErr != nil {
					return nil, leaseErr
				}
			}
			if !created {
				one.Reason = "already_leased"
				one.Detail = describeActiveLease(existing)
				if force && existing != nil && !reclaimableLease(existing) {
					one.Detail += "; force does not reclaim an acknowledged lease"
				}
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
				// Queue so the cascade re-evaluates when the lease
				// releases.
				resp.Queued = append(resp.Queued, task.ID)
				continue
			}
			// Reaching here means created flipped true, which only the
			// reclaim above can do — so existing is non-nil.
			slog.Info("reclaimed unacknowledged dispatch lease on forced feature run",
				"project_id", projectID, "task_id", task.ID,
				"previous_runner_id", existing.AssignedRunnerID,
				"previous_lease_id", existing.LeaseID,
				"runner_id", candidate.RunnerID,
			)
		}

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
		if !s.publishDispatch(candidate.RunnerID, payload) {
			// The lease is ours and the command is lost. Undo the lease
			// so this task is not reported — or refused — as in flight
			// for the rest of its TTL, and say which runner went quiet.
			s.undoUndeliveredLease(ctx, projectID, task.ID)
			one.Reason = reasonRunnerUnreachable
			one.Detail = unreachableDetail(candidate.RunnerID)
			one.RunnerID = candidate.RunnerID
			resp.Results = append(resp.Results, one)
			resp.SkippedCount++
			// Queue it: a runner whose stream is down is usually
			// reconnecting, and the cascade re-attempt costs a publish.
			resp.Queued = append(resp.Queued, task.ID)
			continue
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
			// "Held by a lease" is two different situations and they used
			// to flatten into one sentence. A lease a runner acknowledged
			// means the work is genuinely in flight. A lease that was
			// pushed and never acknowledged means nothing is running: the
			// dispatch went out and no answer came back, and the hold
			// clears itself when the lease expires. Reporting the second
			// as "every ready task is already in flight" sent users
			// looking for a process that does not exist.
			if pending, until := allLeasesAwaitingAck(resp.Results); pending {
				resp.Reason = "feature_dispatch_pending"
				resp.Detail = "no task is running: every ready task is held by a dispatch that was never acknowledged"
				if until != "" {
					resp.Detail += fmt.Sprintf("; the holds clear by %s and the feature can run again then", until)
				}
			} else {
				resp.Reason = "feature_in_progress"
				resp.Detail = "every ready task was already in flight or otherwise unrunnable"
			}
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

// reclaimableLease decides whether an explicit, user-initiated run may take a
// dispatch lease away from the runner that holds it.
//
// Only an unacknowledged ("pushed") lease qualifies. This is deliberately
// narrower than RunTaskNow's force, which releases any lease including an
// acknowledged one, and it is narrower than it first looks like it needs to
// be — so the reasoning is worth writing down:
//
//   - An ACKED lease means the runner told the server it took the work and,
//     immediately after, claims and spawns it. Reclaiming that races a live
//     process, and there is prior art for refusing exactly this: the resume
//     path will not release a claim held by an online runner even under
//     force (see the Abandonment + Resume section in CLAUDE.md). A user who
//     really wants to displace running work has task-level force and the
//     runner-shell for that; the feature-level batch should not do it to a
//     whole feature at once.
//
//   - A PUSHED lease is NOT proof that the runner never got the dispatch.
//     The runner acks late — after resolving its executor and its workdir,
//     and workdir resolution can create a git worktree or clone a remote,
//     which takes real seconds (see CommonResolveWorkdir). "Pushed"
//     therefore covers both "the command evaporated" and "the runner is
//     mid-setup". That ambiguity is why reclaiming a pushed lease is gated
//     behind an explicit force rather than done automatically on every
//     RunFeatureNow: automatic reclaim would occasionally double-dispatch a
//     task whose runner was simply slow to prepare.
//
// The ordinary case this used to matter for no longer reaches here at all:
// a dispatch that is not delivered now clears its own lease at publish time
// (see publishDispatch), so the phantom lease that made "Run feature now"
// look dead is not created in the first place. force is the backstop for
// the residue — a command accepted by a stream that then died before the
// runner read it.
func reclaimableLease(row *storage.DispatchLeaseRow) bool {
	return row != nil && row.State == storage.DispatchLeaseStatePushed
}

// reclaimLease releases an unacknowledged lease and immediately re-creates it
// for the chosen candidate. Returns the new lease and whether the re-create
// won; a false means someone else got there first and the caller must fall
// back to reporting the task as already leased.
func (s *SchedulerService) reclaimLease(
	ctx context.Context,
	projectID, taskID string,
	existing *storage.DispatchLeaseRow,
	candidate *types.RunnerInfo,
	now int64,
) (*storage.DispatchLeaseRow, bool, error) {
	if _, err := s.leases.ReleaseDispatchLease(ctx, projectID, taskID, existing.AssignedRunnerID); err != nil {
		return nil, false, fmt.Errorf("release unacknowledged dispatch lease for %s: %w", taskID, err)
	}
	lease, created, err := s.leases.CreateDispatchLease(ctx, storage.DispatchLeaseCreate{
		ProjectID:         projectID,
		TaskID:            taskID,
		AssignedRunnerID:  candidate.RunnerID,
		AssignedMachineID: candidate.MachineID,
		PushedAt:          now,
		ExpiresAt:         now + s.leaseTTL.Milliseconds(),
	})
	if err != nil {
		return nil, false, fmt.Errorf("recreate dispatch lease after reclaim for %s: %w", taskID, err)
	}
	return lease, created, nil
}

// allLeasesAwaitingAck reports whether every lease blocking this run was
// pushed and never acknowledged — i.e. nothing is actually running — and the
// latest expiry among them, so the caller can say when the holds clear.
//
// "Every" is the bar on purpose: one acknowledged lease means a runner really
// is working inside this feature, and "in progress" is then the honest
// summary even if a sibling task is stuck in limbo.
func allLeasesAwaitingAck(results []types.RunTaskResponse) (bool, string) {
	pending := 0
	latest := ""
	for _, one := range results {
		if one.Dispatched || one.Reason != "already_leased" {
			continue
		}
		if one.LeaseState != storage.DispatchLeaseStatePushed {
			return false, ""
		}
		pending++
		if one.ExpiresAt > latest {
			latest = one.ExpiresAt
		}
	}
	return pending > 0, latest
}

// noReadyTasksDetail turns "no ready tasks" into a sentence that answers the
// question it raises. A feature gated behind another feature used to be told
// only to "check dependencies", sending the user to go find out something the
// server already knew: the tasks carry the blocking feature IDs
// (waiting_on_features / blocked_by_features, see applyFeatureGating).
func noReadyTasksDetail(snap featureSnapshot) string {
	switch {
	case len(snap.BlockedBy) > 0:
		return fmt.Sprintf("no ready tasks in this feature; it is blocked by feature(s) %s",
			strings.Join(snap.BlockedBy, ", "))
	case len(snap.WaitingOn) > 0:
		return fmt.Sprintf("no ready tasks in this feature; it is waiting on feature(s) %s to complete",
			strings.Join(snap.WaitingOn, ", "))
	default:
		return "no ready tasks in this feature; check dependencies or in-progress state"
	}
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

// featureSnapshot is what one GetTasksByFeature call can tell RunFeatureNow
// about a feature beyond its ready set.
type featureSnapshot struct {
	// Outstanding counts tasks that can still produce work without a
	// human: status pending or in_progress. nil means "could not
	// measure" — see RunFeatureResponse.Outstanding for why that must
	// not collapse to 0.
	Outstanding *int

	// WaitingOn / BlockedBy name the features this one is gated behind,
	// folded from the tasks' feature-level dependency state
	// (applyFeatureGating). They answer the question "no ready tasks"
	// raises and refuses to answer: ready for what, waiting on whom.
	WaitingOn []string
	BlockedBy []string
}

// featureTaskSnapshot reads every task in the feature — not just the ready
// ones — and folds them into the facts RunFeatureNow reports.
//
// Terminal statuses (completed, validated, archived, cancelled) and blocked
// do not count toward Outstanding: blocked is where the retry cap parks a
// task after max_attempts, and continuing to wait on it would keep a cascade
// alive forever.
func (s *SchedulerService) featureTaskSnapshot(ctx context.Context, projectID, featureID string) featureSnapshot {
	if s.featTasks == nil {
		return featureSnapshot{}
	}
	tasks, err := s.featTasks.GetTasksByFeature(ctx, projectID, featureID)
	if err != nil {
		slog.Warn("feature task snapshot failed",
			"project_id", projectID, "feature_id", featureID, "error", err)
		return featureSnapshot{}
	}
	snap := featureSnapshot{}
	n := 0
	waiting := newStringSet()
	blocked := newStringSet()
	for i := range tasks {
		switch tasks[i].Status {
		case "pending", "in_progress":
			n++
		}
		waiting.addAll(tasks[i].WaitingOnFeatures)
		blocked.addAll(tasks[i].BlockedByFeatures)
	}
	snap.Outstanding = &n
	snap.WaitingOn = waiting.sorted()
	snap.BlockedBy = blocked.sorted()
	return snap
}

// stringSet is a tiny insertion-deduping set with sorted output, so the
// feature IDs a response carries are stable across calls (map iteration
// order would make the same hold read differently every refresh).
type stringSet struct {
	seen map[string]bool
}

func newStringSet() *stringSet { return &stringSet{seen: map[string]bool{}} }

func (s *stringSet) addAll(values []string) {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			s.seen[v] = true
		}
	}
}

func (s *stringSet) sorted() []string {
	if len(s.seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.seen))
	for v := range s.seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
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
