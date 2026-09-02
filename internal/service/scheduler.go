package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

const defaultDispatchLeaseTTL = 60 * time.Second

// DefaultSchedulerInterval is the default interval for the background scheduler loop.
const DefaultSchedulerInterval = 5 * time.Second

type schedulerTaskService interface {
	GetReady(ctx context.Context, projectID string, opts *api.TaskFilterOptions) ([]types.ResolvedTask, error)
}

type schedulerProjectLister interface {
	ListProjects(ctx context.Context) ([]string, error)
}

// schedulerFeatureTaskLister exposes every task in a feature, not just the
// ready ones. RunFeatureNow needs it to answer "is this feature finished?",
// which GetReady structurally cannot: it filters to Classification=="ready",
// so a feature whose remaining tasks are waiting on an in-flight sibling and
// a feature that has genuinely drained both come back empty.
//
// Optional, asserted from the task service the same way schedulerProjectLister
// is. *TaskServiceImpl satisfies it; fakes that do not simply leave
// Outstanding unknown (-1) and the cascade falls back to its old behaviour.
// schedulerProjectTaskLister exposes every task in a project, which is what
// the feature graph is derived from. GetReady cannot serve here: it filters to
// Classification=="ready", and a dependent chain is made almost entirely of
// features that are NOT ready yet.
type schedulerProjectTaskLister interface {
	GetTasks(ctx context.Context, projectID string) (*types.TaskListResponse, error)
}

// schedulerCascadeRootStore persists standing run-with-dependents requests.
// Optional: without it the option still dispatches the root and whatever is
// already ready, but nothing survives a restart and there is no chain to
// cancel.
type schedulerCascadeRootStore interface {
	UpsertFeatureCascadeRoot(ctx context.Context, projectID, rootFeatureID string, pausedAtRequest bool) error
	DeleteFeatureCascadeRoot(ctx context.Context, projectID, rootFeatureID string) (bool, error)
	ListFeatureCascadeRoots(ctx context.Context, projectID string) ([]storage.FeatureCascadeRootRow, error)
}

type schedulerFeatureTaskLister interface {
	GetTasksByFeature(ctx context.Context, projectID, featureID string) ([]types.ResolvedTask, error)
}

type schedulerRunnerRegistry interface {
	ListRunners(ctx context.Context) ([]types.RunnerInfo, error)
}

type schedulerPlacementService interface {
	Get(ctx context.Context, projectID string) (*types.ProjectPlacement, error)
}

type schedulerLeaseStore interface {
	CreateDispatchLease(ctx context.Context, in storage.DispatchLeaseCreate) (*storage.DispatchLeaseRow, bool, error)
	GetDispatchLeaseRow(ctx context.Context, projectID, taskID string) (*storage.DispatchLeaseRow, error)
	ReleaseDispatchLease(ctx context.Context, projectID, taskID, runnerID string) (bool, error)
	ClearDispatchLease(ctx context.Context, projectID, taskID string) (bool, error)
	RecordPlacementReason(ctx context.Context, row *storage.PlacementReasonRow) error
}

type schedulerLeaseExpirer interface {
	ExpireDispatchLeases(ctx context.Context, now int64) (int64, error)
}

type schedulerCommandPublisher interface {
	PublishRunnerCommand(runnerID string, command string, payload interface{})
}

// schedulerCommandDeliverer is the optional delivery-reporting half of the
// publisher. *realtime.Hub satisfies it; a publisher that does not is used
// exactly as before and every dispatch counts as delivered, because "cannot
// measure" must never be read as "did not arrive".
type schedulerCommandDeliverer interface {
	PublishRunnerCommandTracked(runnerID string, command string, payload interface{}) (delivered, dropped int)
}

type schedulerPauseChecker interface {
	IsPaused(projectID string) bool
	IsAutomationsPaused() bool
}

type schedulerProjectAutomationPauseChecker interface {
	IsAutomationsPausedForProject(projectID string) bool
}

// schedulerFeaturePauseChecker is the FEATURE-scoped dial. Optional, like
// the per-project automations one: a pause implementation that predates it
// simply never holds a feature.
type schedulerFeaturePauseChecker interface {
	IsFeaturePaused(projectID, featureID string) bool
}

// SchedulerService owns Brain push dispatch placement decisions.
type SchedulerService struct {
	tasks     schedulerTaskService
	projects  schedulerProjectLister
	featTasks schedulerFeatureTaskLister
	projTasks schedulerProjectTaskLister
	roots     schedulerCascadeRootStore
	runners   schedulerRunnerRegistry
	placement schedulerPlacementService
	leases    schedulerLeaseStore
	expirer   schedulerLeaseExpirer
	publisher schedulerCommandPublisher
	deliverer schedulerCommandDeliverer
	pauses    schedulerPauseChecker
	leaseTTL  time.Duration
	nowUnixMS func() int64

	// cascade is the optional feature-cascade tracker. When set, RunFeatureNow
	// will register the feature for cascade dispatch when there are queued
	// tasks. Wired after construction via SetFeatureCascade to avoid a
	// circular dependency (cascade needs a FeatureRunner = *SchedulerService).
	cascade featureCascadeRegistrar

	mu     sync.RWMutex
	status schedulerStatusSnapshot
}

// featureCascadeRegistrar is the slice of FeatureCascadeService that
// SchedulerService needs. Kept as an interface so tests can inject a fake
// without pulling in the full cascade service.
type featureCascadeRegistrar interface {
	Register(projectID, featureID string)
	IsActive(projectID, featureID string) bool
}

// SetFeatureCascade wires the feature-cascade tracker. Pass nil to disable
// cascade behaviour (RunFeatureNow then dispatches what fits and stops; the
// user must re-trigger to drain leftovers).
func (s *SchedulerService) SetFeatureCascade(c featureCascadeRegistrar) {
	s.cascade = c
}

type SchedulerResult = types.SchedulerResult

type schedulerStatusSnapshot struct {
	Started            bool
	Running            bool
	Interval           time.Duration
	LastTickAt         time.Time
	LastSuccessAt      time.Time
	LastError          string
	TotalTicks         int64
	LastProjectResults map[string]types.SchedulerResult
	LastExpiredLeases  int64
}

// NewSchedulerService constructs a scheduler from already-tested service/storage primitives.
func NewSchedulerService(tasks schedulerTaskService, pauses schedulerPauseChecker, deps ...interface{}) *SchedulerService {
	svc := &SchedulerService{
		tasks:     tasks,
		pauses:    pauses,
		leaseTTL:  defaultDispatchLeaseTTL,
		nowUnixMS: func() int64 { return time.Now().UnixMilli() },
	}
	if v, ok := tasks.(schedulerProjectLister); ok {
		svc.projects = v
	}
	if v, ok := tasks.(schedulerFeatureTaskLister); ok {
		svc.featTasks = v
	}
	if v, ok := tasks.(schedulerProjectTaskLister); ok {
		svc.projTasks = v
	}
	for _, dep := range deps {
		if v, ok := dep.(schedulerCascadeRootStore); ok {
			svc.roots = v
		}
		if v, ok := dep.(schedulerRunnerRegistry); ok {
			svc.runners = v
		} else if v, ok := dep.(interface {
			ListRunners(context.Context) (*types.RunnerListResponse, error)
		}); ok {
			svc.runners = runnerListResponseAdapter{source: v}
		}
		if v, ok := dep.(schedulerPlacementService); ok {
			svc.placement = v
		}
		if v, ok := dep.(schedulerLeaseStore); ok {
			svc.leases = v
		}
		if v, ok := dep.(schedulerLeaseExpirer); ok {
			svc.expirer = v
		}
		if v, ok := dep.(schedulerCommandPublisher); ok {
			// Publisher and deliverer are two faces of one object, so they
			// are assigned together: a later dep that takes over publishing
			// must also take over (or clear) delivery reporting, otherwise
			// dispatches would be published through one object and judged
			// delivered through another.
			svc.publisher = v
			svc.deliverer, _ = dep.(schedulerCommandDeliverer)
		}
	}
	return svc
}

func (s *SchedulerService) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultSchedulerInterval
	}
	s.mu.Lock()
	s.status.Started = true
	s.status.Running = true
	s.status.Interval = interval
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			s.status.Running = false
			s.mu.Unlock()
			slog.Info("scheduler stopped")
		}()

		s.RunOnce(ctx)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.RunOnce(ctx)
			}
		}
	}()
}

func (s *SchedulerService) Status() types.SchedulerStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	status := types.SchedulerStatus{
		Started:           s.status.Started,
		Running:           s.status.Running,
		Interval:          s.status.Interval.String(),
		LastError:         s.status.LastError,
		TotalTicks:        s.status.TotalTicks,
		LastExpiredLeases: s.status.LastExpiredLeases,
	}
	if !s.status.LastTickAt.IsZero() {
		status.LastTickAt = s.status.LastTickAt.Format(time.RFC3339)
	}
	if !s.status.LastSuccessAt.IsZero() {
		status.LastSuccessAt = s.status.LastSuccessAt.Format(time.RFC3339)
	}
	if s.status.LastProjectResults != nil {
		status.LastProjectResults = make(map[string]types.SchedulerResult, len(s.status.LastProjectResults))
		for projectID, result := range s.status.LastProjectResults {
			status.LastProjectResults[projectID] = result
		}
	}
	return status
}

func (s *SchedulerService) RunOnce(ctx context.Context) {
	now := time.Now().UTC()
	results := make(map[string]SchedulerResult)
	var expired int64
	var lastErr string

	if s.expirer != nil {
		count, err := s.expirer.ExpireDispatchLeases(ctx, s.nowUnixMS())
		if err != nil {
			lastErr = fmt.Sprintf("expire dispatch leases: %v", err)
		} else {
			expired = count
		}
	}

	if lastErr == "" {
		if s.projects == nil {
			lastErr = "scheduler project lister is unavailable"
		} else {
			projects, err := s.projects.ListProjects(ctx)
			if err != nil {
				lastErr = fmt.Sprintf("list projects: %v", err)
			} else {
				for _, projectID := range projects {
					result, err := s.ScheduleProject(ctx, projectID)
					if err != nil {
						lastErr = fmt.Sprintf("schedule project %s: %v", projectID, err)
						break
					}
					if result != nil {
						results[projectID] = *result
					}
				}
			}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.TotalTicks++
	s.status.LastTickAt = now
	s.status.LastExpiredLeases = expired
	s.status.LastProjectResults = results
	s.status.LastError = lastErr
	if lastErr == "" {
		s.status.LastSuccessAt = now
	}
}

type runnerListResponseAdapter struct {
	source interface {
		ListRunners(context.Context) (*types.RunnerListResponse, error)
	}
}

func (a runnerListResponseAdapter) ListRunners(ctx context.Context) ([]types.RunnerInfo, error) {
	resp, err := a.source.ListRunners(ctx)
	if err != nil || resp == nil {
		return nil, err
	}
	return append([]types.RunnerInfo(nil), resp.Runners...), nil
}

func (s *SchedulerService) ScheduleProject(ctx context.Context, projectID string) (*SchedulerResult, error) {
	if s.tasks == nil || s.runners == nil || s.placement == nil || s.leases == nil {
		return nil, fmt.Errorf("scheduler dependencies are incomplete")
	}

	tasks, err := s.tasks.GetReady(ctx, projectID, nil)
	if err != nil {
		return nil, fmt.Errorf("load ready tasks: %w", err)
	}
	result := &SchedulerResult{ProjectID: projectID, Considered: len(tasks)}
	if len(tasks) == 0 {
		return result, nil
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

	reservedSlots := make(map[string]int)
	for _, task := range tasks {
		if skip, reason := s.shouldSkipTask(projectID, task); skip {
			result.Skipped++
			switch reason {
			case skipReasonTasksPaused:
				result.SkippedTasksPaused++
			case skipReasonAutomationsPaused:
				result.SkippedAutomationsPaused++
			case skipReasonFeaturePaused:
				result.SkippedFeaturePaused++
			}
			continue
		}
		candidate, reasons := s.selectCandidate(task, projectID, runners, placement, reservedSlots)
		if candidate == nil {
			result.Skipped++
			result.SkippedNoCandidate++
			if err := s.recordNoCandidate(ctx, projectID, task.ID, reasons); err != nil {
				return nil, err
			}
			continue
		}

		now := s.nowUnixMS()
		lease, created, err := s.leases.CreateDispatchLease(ctx, storage.DispatchLeaseCreate{
			ProjectID:         projectID,
			TaskID:            task.ID,
			AssignedRunnerID:  candidate.RunnerID,
			AssignedMachineID: candidate.MachineID,
			PushedAt:          now,
			ExpiresAt:         now + s.leaseTTL.Milliseconds(),
		})
		if err != nil {
			return nil, fmt.Errorf("create dispatch lease for %s: %w", task.ID, err)
		}
		if !created {
			result.Skipped++
			result.SkippedAlreadyLeased++
			continue
		}
		if !s.publishDispatch(candidate.RunnerID, map[string]any{
			"taskId":    task.ID,
			"projectId": projectID,
			"lease":     lease,
			"expiresAt": lease.ExpiresAt,
			// Inline the resolved task so the runner can process
			// this dispatch without an HTTP round-trip back to
			// GetReadyTasks. Legacy runners that don't understand
			// "task" simply ignore the extra field.
			"task": task,
		}) {
			// The command never reached the runner, so the lease we
			// just wrote describes work nobody has. Undo it now rather
			// than let it read as in-flight for its whole TTL — this is
			// the tick that births the phantom leases a user then runs
			// into as "already_leased" on an explicit run.
			s.undoUndeliveredLease(ctx, projectID, task.ID)
			result.Skipped++
			result.SkippedRunnerUnreachable++
			continue
		}
		reservedSlots[candidate.RunnerID]++
		result.Dispatched++
	}
	return result, nil
}

// Skip reasons returned by shouldSkipTask alongside its boolean. The reason is
// meaningful only when the boolean is true; it names which pause dial applied.
const (
	skipReasonTasksPaused       = "tasks_paused"
	skipReasonAutomationsPaused = "automations_paused"
	skipReasonFeaturePaused     = "feature_paused"
)

// shouldSkipTask decides whether a task should be skipped by the scheduler
// based on the two independent pause switches:
//
//   - tasks_paused (per project): applies to user-authored / non-automation
//     tasks only. Does NOT affect automation-generated tasks.
//   - automations_paused (per project): applies to automation-generated
//     tasks only. Independent of tasks_paused.
//
// This mirrors the runner-side carve-out in handleCommand and the poll loop.
// The two switches must remain fully independent — pausing manual task
// execution should not silently halt automation work, and pausing autos
// should not affect manual tasks. See unit tests in scheduler_test.go
// (TestSchedulerService_PauseIndependence*).
func (s *SchedulerService) shouldSkipTask(projectID string, task types.ResolvedTask) (bool, string) {
	if s.pauses == nil {
		return false, ""
	}
	// The feature dial is checked FIRST and applies to automation-generated
	// tasks as well. The two project dials are a carve-out of each other by
	// origin — who authored the task — but a feature hold is about the WORK:
	// "stop this feature", not "stop this kind of task". A feature whose
	// automation-generated follow-ups kept dispatching would not be held at
	// all, which is the one thing the switch promises.
	if feat, ok := s.pauses.(schedulerFeaturePauseChecker); ok {
		if task.FeatureID != "" && feat.IsFeaturePaused(projectID, task.FeatureID) {
			return true, skipReasonFeaturePaused
		}
	}

	isAutomation := strings.HasPrefix(task.GeneratedBy, "automation:")
	if isAutomation {
		// Automation tasks respect ONLY the autos-paused switch.
		if scoped, ok := s.pauses.(schedulerProjectAutomationPauseChecker); ok {
			return scoped.IsAutomationsPausedForProject(projectID), skipReasonAutomationsPaused
		}
		return s.pauses.IsAutomationsPaused(), skipReasonAutomationsPaused
	}
	// Non-automation (manual/user) tasks respect ONLY the tasks-paused switch.
	return s.pauses.IsPaused(projectID), skipReasonTasksPaused
}

// RunTaskNow is the user-explicit "run this task now" entry point used by the
// PWA "x" shortcut and the TUI's runner-controller fallback.
//
// Unlike ScheduleProject (which iterates every ready task on every tick), this
// targets a single task: load the project's ready set, find the requested
// task, pick an eligible runner via the same selectCandidate logic, create a
// dispatch lease, and publish a dispatch command. It bypasses
// shouldSkipTask's pause check because the user explicitly asked for this run
// — pause is meant to halt automatic scheduling, not block manual overrides.
//
// When force is true, the dispatch payload includes "force": true so the
// runner side accepts the dispatch even when the project is paused there.
//
// The response always sets Dispatched and (on failure) Reason; errors are
// returned only for unexpected infrastructure problems.
func (s *SchedulerService) RunTaskNow(ctx context.Context, projectID, taskID string, force bool) (*types.RunTaskResponse, error) {
	resp := &types.RunTaskResponse{ProjectID: projectID, TaskID: taskID}

	if s.runners == nil || s.placement == nil || s.leases == nil {
		resp.Reason = "scheduler_not_configured"
		resp.Detail = "scheduler is missing dependencies required for dispatch"
		return resp, nil
	}

	tasks, err := s.tasks.GetReady(ctx, projectID, nil)
	if err != nil {
		return nil, fmt.Errorf("load ready tasks: %w", err)
	}
	var task *types.ResolvedTask
	for i := range tasks {
		if tasks[i].ID == taskID {
			task = &tasks[i]
			break
		}
	}
	if task == nil {
		resp.Reason = "task_not_ready"
		resp.Detail = "task is not in the ready set for this project"
		return resp, nil
	}

	runners, err := s.runners.ListRunners(ctx)
	if err != nil {
		return nil, fmt.Errorf("list runners: %w", err)
	}
	if len(runners) == 0 {
		resp.Reason = "no_online_runner"
		resp.Detail = "no runners are registered"
		return resp, nil
	}

	placement, err := s.placement.Get(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get project placement: %w", err)
	}
	if placement == nil {
		placement = &types.ProjectPlacement{ProjectID: projectID, Affinity: types.PlacementAffinitySoft}
	}

	candidate, reasons := s.selectCandidate(*task, projectID, runners, placement, nil)
	if candidate == nil {
		resp.Reason = "no_eligible_runner"
		if len(reasons) > 0 {
			resp.Detail = strings.Join(reasons, "; ")
		}
		return resp, nil
	}

	now := s.nowUnixMS()
	lease, created, err := s.leases.CreateDispatchLease(ctx, storage.DispatchLeaseCreate{
		ProjectID:         projectID,
		TaskID:            task.ID,
		AssignedRunnerID:  candidate.RunnerID,
		AssignedMachineID: candidate.MachineID,
		PushedAt:          now,
		ExpiresAt:         now + s.leaseTTL.Milliseconds(),
	})
	if err != nil {
		return nil, fmt.Errorf("create dispatch lease: %w", err)
	}
	if !created {
		// Look up the existing lease so the caller can see which runner owns
		// it and (when force=true) so we can release+redispatch it.
		existing, lookupErr := s.leases.GetDispatchLeaseRow(ctx, projectID, task.ID)
		if lookupErr != nil {
			return nil, fmt.Errorf("lookup existing dispatch lease: %w", lookupErr)
		}
		if !force {
			resp.Reason = "already_leased"
			resp.Detail = describeActiveLease(existing)
			if existing != nil {
				resp.RunnerID = existing.AssignedRunnerID
				resp.LeaseID = existing.LeaseID
				resp.LeaseState = existing.State
				if existing.ExpiresAt > 0 {
					resp.ExpiresAt = time.UnixMilli(existing.ExpiresAt).UTC().Format(time.RFC3339)
				}
			}
			return resp, nil
		}
		// force=true: drop the stale lease and try once more. We intentionally
		// keep this inline (rather than recursing) so we can attribute the
		// release to whichever runner owned the lease.
		if existing != nil {
			if _, relErr := s.leases.ReleaseDispatchLease(ctx, projectID, task.ID, existing.AssignedRunnerID); relErr != nil {
				return nil, fmt.Errorf("release stale dispatch lease: %w", relErr)
			}
		}
		lease, created, err = s.leases.CreateDispatchLease(ctx, storage.DispatchLeaseCreate{
			ProjectID:         projectID,
			TaskID:            task.ID,
			AssignedRunnerID:  candidate.RunnerID,
			AssignedMachineID: candidate.MachineID,
			PushedAt:          now,
			ExpiresAt:         now + s.leaseTTL.Milliseconds(),
		})
		if err != nil {
			return nil, fmt.Errorf("recreate dispatch lease after force release: %w", err)
		}
		if !created {
			// Still couldn't create — surface a clear error rather than
			// silently no-op'ing the force.
			resp.Reason = "already_leased"
			resp.Detail = "task lease could not be reclaimed even with force"
			return resp, nil
		}
	}

	{
		payload := map[string]any{
			"taskId":    task.ID,
			"projectId": projectID,
			"lease":     lease,
			"expiresAt": lease.ExpiresAt,
			// Inline the resolved task so the runner can process this
			// dispatch without an HTTP round-trip back to GetReadyTasks.
			"task": task,
			// RunTaskNow is by definition a user-initiated dispatch, so the
			// runner-side payload always carries force=true. This bypasses
			// the runner's pause gate (which exists to halt the automatic
			// poll-loop / ScheduleProject tick, not manual overrides).
			// The `force` parameter on this method controls lease-release
			// semantics (release an active lease before dispatch) — a
			// separate concern from telling the runner "this is manual."
			// Without this the PWA's "x" key silently fails on paused
			// projects: server reports dispatched=true but the runner
			// rejects with runner_paused, leaving the user staring at a
			// "Triggered" toast while nothing actually runs.
			"force": true,
		}
		if !s.publishDispatch(candidate.RunnerID, payload) {
			// Lease written, command lost: report the truth instead of a
			// "Triggered" that never happened, and drop the lease so the
			// next attempt (scheduler tick or another click) isn't refused
			// as already_leased for the rest of the TTL.
			s.undoUndeliveredLease(ctx, projectID, task.ID)
			resp.Reason = reasonRunnerUnreachable
			resp.Detail = unreachableDetail(candidate.RunnerID)
			resp.RunnerID = candidate.RunnerID
			return resp, nil
		}
	}

	resp.Dispatched = true
	resp.RunnerID = candidate.RunnerID
	resp.LeaseID = lease.LeaseID
	if lease.ExpiresAt > 0 {
		resp.ExpiresAt = time.UnixMilli(lease.ExpiresAt).UTC().Format(time.RFC3339)
	}
	return resp, nil
}

type schedulerCandidate struct {
	runner types.RunnerInfo
}

type schedulerMachineCandidate struct {
	machineID   string
	runners     []schedulerCandidate
	score       int
	activeTasks int
}

func (s *SchedulerService) selectCandidate(task types.ResolvedTask, projectID string, runners []types.RunnerInfo, placement *types.ProjectPlacement, reservedSlots map[string]int) (*types.RunnerInfo, []string) {
	machines := make(map[string]*schedulerMachineCandidate)
	reasons := make([]string, 0, len(runners))
	for _, runner := range runners {
		runner.ActiveTasks += reservedSlots[runner.RunnerID]
		reason, ok := runnerEligibleForTask(task, projectID, runner, placement)
		if !ok {
			reasons = append(reasons, runner.RunnerID+": "+reason)
			continue
		}

		machineID := runner.MachineID
		if machineID == "" {
			machineID = "runner:" + runner.RunnerID
		}
		candidate := machines[machineID]
		if candidate == nil {
			candidate = &schedulerMachineCandidate{
				machineID: machineID,
				score:     scoreMachine(machineID, placement, task),
			}
			machines[machineID] = candidate
		}
		candidate.runners = append(candidate.runners, schedulerCandidate{runner: runner})
		candidate.activeTasks += runner.ActiveTasks
	}
	if len(machines) == 0 {
		return nil, reasons
	}

	machineCandidates := make([]schedulerMachineCandidate, 0, len(machines))
	for _, candidate := range machines {
		machineCandidates = append(machineCandidates, *candidate)
	}
	sort.SliceStable(machineCandidates, func(i, j int) bool {
		if machineCandidates[i].score != machineCandidates[j].score {
			return machineCandidates[i].score > machineCandidates[j].score
		}
		if machineCandidates[i].activeTasks != machineCandidates[j].activeTasks {
			return machineCandidates[i].activeTasks < machineCandidates[j].activeTasks
		}
		return machineCandidates[i].machineID < machineCandidates[j].machineID
	})

	runnersOnMachine := machineCandidates[0].runners
	sort.SliceStable(runnersOnMachine, func(i, j int) bool {
		left := runnersOnMachine[i].runner
		right := runnersOnMachine[j].runner
		if left.ActiveTasks != right.ActiveTasks {
			return left.ActiveTasks < right.ActiveTasks
		}
		leftRemaining := remainingCapacity(left)
		rightRemaining := remainingCapacity(right)
		if leftRemaining != rightRemaining {
			return leftRemaining > rightRemaining
		}
		return left.RunnerID < right.RunnerID
	})
	return &runnersOnMachine[0].runner, reasons
}

func runnerEligibleForTask(task types.ResolvedTask, projectID string, runner types.RunnerInfo, placement *types.ProjectPlacement) (string, bool) {
	if runner.Status != types.RunnerStatusOnline {
		return "runner not online", false
	}
	if !runner.DispatchPush {
		return "runner does not support push dispatch", false
	}
	if runner.Draining {
		return "runner draining", false
	}
	// Runner-scoped pause (PUT /runners/{runnerId}/pause). Unlike the
	// per-project dials handled by shouldSkipTask, this one has no
	// automation carve-out and no force override: a paused runner is simply
	// not a placement candidate, so RunTaskNow / RunFeatureNow route around
	// it instead of pushing leases it will only reject.
	if runner.Paused {
		return "runner paused", false
	}
	if !runnerAllowsProject(runner, projectID) {
		return "project not allowed", false
	}
	if task.Executor != "" && !stringSliceContains(runner.Executors, task.Executor) {
		return "executor not supported", false
	}
	if missing := missingStrings(task.RequiresCapability, runner.Capabilities); len(missing) > 0 {
		return "missing task capabilities: " + strings.Join(missing, ","), false
	}
	if missing := missingStrings(placement.RequiredCapabilities, runner.Capabilities); len(missing) > 0 {
		return "missing project capabilities: " + strings.Join(missing, ","), false
	}
	if missing := missingLabels(placement.RequiredLabels, runner.Labels); len(missing) > 0 {
		return "missing required labels", false
	}
	if missing := missingResources(placement.Resources, runner.Resources, runner.Capacity); len(missing) > 0 {
		return "missing project resources: " + strings.Join(missing, ","), false
	}
	if placement.WorkspacePolicy == types.WorkspacePolicyWorktree && len(runner.WorkspaceRoots) == 0 {
		return "no workspace roots for worktree policy", false
	}
	if runner.MaxParallel > 0 && runner.ActiveTasks >= runner.MaxParallel {
		return "runner at capacity", false
	}
	if placement.Affinity == types.PlacementAffinityStrict && !machineAllowedByStrictAffinity(runner.MachineID, placement) {
		return "strict affinity mismatch", false
	}
	if reason, ok := machineAffinitySatisfied(task, runner.MachineID); !ok {
		return reason, false
	}
	return "eligible", true
}

// machineAffinitySatisfied enforces a task's own machine_affinity against a
// candidate runner's machine. Only "local" is a hard filter; "preferred" is
// expressed as a score in scoreMachine, and "none" is unconstrained.
//
// This is called from both dispatch paths — here for push, and from
// TaskServiceImpl.filterByRunnerEligibility for pull. Enforcing it in only
// one place would mean a "local" task still gets picked up by a polling
// runner on another machine, which is the exact failure the field exists to
// prevent.
func machineAffinitySatisfied(task types.ResolvedTask, runnerMachineID string) (string, bool) {
	if types.ResolveMachineAffinity(task.MachineAffinity, task.OriginMachineID) != types.MachineAffinityLocal {
		return "eligible", true
	}
	origin := strings.TrimSpace(task.OriginMachineID)
	if origin == "" {
		// machine_affinity=local with nothing to be local to. Refuse rather
		// than fall back to "anywhere": the author asked for a constraint,
		// and silently ignoring it is worse than an unplaced task with a
		// reason attached.
		return reasonMachineAffinityUnresolved, false
	}
	if strings.TrimSpace(runnerMachineID) != origin {
		return reasonMachineAffinityMismatch, false
	}
	return "eligible", true
}

func scoreMachine(machineID string, placement *types.ProjectPlacement, task types.ResolvedTask) int {
	score := 100
	if stringSliceContains(placement.PreferredMachines, machineID) {
		score += 50
	}
	if len(placement.AllowedMachines) > 0 && stringSliceContains(placement.AllowedMachines, machineID) {
		score += 25
	}
	// Task-level origin affinity outranks the project-level preference: the
	// machine the task was authored on is a stronger signal than a blanket
	// project policy, and it is the only signal that distinguishes two tasks
	// in the same project created from different machines.
	if machineIsTaskOrigin(machineID, task) {
		switch types.ResolveMachineAffinity(task.MachineAffinity, task.OriginMachineID) {
		case types.MachineAffinityLocal, types.MachineAffinityPreferred:
			score += 75
		}
	}
	return score
}

// machineIsTaskOrigin reports whether machineID is the machine the task was
// created on. An empty origin never matches — including against the synthetic
// "runner:<id>" id selectCandidate substitutes for a runner that reports no
// machine id, which must not be mistaken for a match.
func machineIsTaskOrigin(machineID string, task types.ResolvedTask) bool {
	origin := strings.TrimSpace(task.OriginMachineID)
	return origin != "" && machineID == origin
}

func remainingCapacity(runner types.RunnerInfo) int {
	if runner.MaxParallel <= 0 {
		return int(^uint(0) >> 1)
	}
	return runner.MaxParallel - runner.ActiveTasks
}

func runnerAllowsProject(runner types.RunnerInfo, projectID string) bool {
	return len(runner.Projects) == 0 || stringSliceContains(runner.Projects, projectID)
}

func machineAllowedByStrictAffinity(machineID string, placement *types.ProjectPlacement) bool {
	if len(placement.AllowedMachines) > 0 {
		return stringSliceContains(placement.AllowedMachines, machineID)
	}
	if len(placement.PreferredMachines) > 0 {
		return stringSliceContains(placement.PreferredMachines, machineID)
	}
	return true
}

func missingResources(required, resources, capacity map[string]interface{}) []string {
	missing := make([]string, 0)
	for key, want := range required {
		if resourceRequirementMet(want, resources[key]) || resourceRequirementMet(want, capacity[key]) {
			continue
		}
		missing = append(missing, key)
	}
	sort.Strings(missing)
	return missing
}

func resourceRequirementMet(required, actual interface{}) bool {
	if req, ok := numericValue(required); ok {
		got, ok := numericValue(actual)
		return ok && got >= req
	}
	return reflect.DeepEqual(actual, required)
}

func numericValue(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func missingStrings(required, actual []string) []string {
	missing := make([]string, 0)
	for _, req := range required {
		if !stringSliceContains(actual, req) {
			missing = append(missing, req)
		}
	}
	return missing
}

func missingLabels(required, actual map[string]string) []string {
	missing := make([]string, 0)
	for key, want := range required {
		if actual[key] != want {
			missing = append(missing, key)
		}
	}
	return missing
}

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// reasonRunnerUnreachable is the skip token for a dispatch whose lease was
// created but whose command never reached the runner: its command stream was
// not connected at publish time. Distinct from no_eligible_runner (placement
// refused the task) and from already_leased (someone else holds the lease) —
// here placement succeeded and the lease was ours; only the delivery failed.
const reasonRunnerUnreachable = "runner_unreachable"

// Machine-affinity ineligibility tokens. These flow out through the same
// per-runner `reasons` slice as every other eligibility check, so they land
// in placement reasons and are visible via task_placement_reasons rather
// than presenting as an unexplained stuck task.
const (
	// reasonMachineAffinityMismatch: the task asked to run on its origin
	// machine and this runner is on a different one.
	reasonMachineAffinityMismatch = "machine_affinity_mismatch"
	// reasonMachineAffinityUnresolved: the task asked to run on its origin
	// machine but carries no origin machine id — usually a task created
	// before origin stamping, or by a client that could not resolve its own
	// machine id. Relax machine_affinity or set origin_machine_id.
	reasonMachineAffinityUnresolved = "machine_affinity_unresolved"
)

// publishDispatch publishes a dispatch command and reports whether it
// actually reached the runner's live command stream.
//
// Dispatch is an SSE publish to a topic nobody is required to be listening
// on. When the runner's stream is down — it restarted, the API restarted
// under it, the network blipped — the command evaporates, but the lease row
// the caller just wrote stays "pushed" for its full TTL. Everything
// downstream then reads that task as dispatched: the PWA rendered it as
// "already in flight", and every later dispatch attempt was refused as
// already_leased, for a minute, over a command no runner ever saw.
//
// Returning false lets the caller undo the lease at the moment the loss
// happens, which is the only moment the server can still tell. False means
// measured-and-lost; a publisher that cannot measure delivery returns true,
// preserving the old fire-and-forget behaviour.
func (s *SchedulerService) publishDispatch(runnerID string, payload map[string]any) bool {
	if s.publisher == nil {
		// Nothing is wired to deliver commands (headless tests): there is
		// no delivery to have failed, so leave the lease alone.
		return true
	}
	if s.deliverer == nil {
		s.publisher.PublishRunnerCommand(runnerID, "dispatch", payload)
		return true
	}
	delivered, dropped := s.deliverer.PublishRunnerCommandTracked(runnerID, "dispatch", payload)
	if delivered > 0 {
		return true
	}
	slog.Warn("dispatch command not delivered; runner command stream is not connected",
		"runner_id", runnerID,
		"task_id", payload["taskId"],
		"project_id", payload["projectId"],
		"dropped", dropped,
	)
	return false
}

// undoUndeliveredLease wipes the lease written for a dispatch that never
// reached its runner. Failure to clear is logged, not returned: the caller is
// already reporting a skip, and a lingering lease self-heals at TTL — the old
// behaviour — whereas failing the whole request would turn a degraded
// dispatch into an error.
func (s *SchedulerService) undoUndeliveredLease(ctx context.Context, projectID, taskID string) {
	if s.leases == nil {
		return
	}
	if _, err := s.leases.ClearDispatchLease(ctx, projectID, taskID); err != nil {
		slog.Warn("failed to clear lease for undelivered dispatch",
			"project_id", projectID, "task_id", taskID, "error", err)
	}
}

// unreachableDetail is the human-facing half of reasonRunnerUnreachable.
func unreachableDetail(runnerID string) string {
	return fmt.Sprintf("%s is registered but its command stream is not connected; the dispatch was not delivered", runnerID)
}

// describeActiveLease explains an already_leased skip in terms of what the
// lease's state actually means for the caller.
//
// The two states are not the same situation and used to share one sentence.
// An acked lease means a runner took the work and is running it — wait for
// it. A pushed lease means a dispatch went out and nothing came back: the
// runner may be starting it (the ack lands after workdir resolution, which
// can clone a repo) or may never have seen it. Telling a user the task is
// "already in flight" when the truth is "we are waiting to hear back" sends
// them hunting for a process that may not exist.
func describeActiveLease(row *storage.DispatchLeaseRow) string {
	if row == nil {
		return "task already has an active dispatch lease"
	}
	runner := row.AssignedRunnerID
	if runner == "" {
		runner = "a runner"
	}
	switch row.State {
	case storage.DispatchLeaseStateAcked:
		return fmt.Sprintf("%s acknowledged this dispatch and is running the task", runner)
	case storage.DispatchLeaseStatePushed:
		detail := fmt.Sprintf("dispatch was pushed to %s and has not been acknowledged yet", runner)
		if row.ExpiresAt > 0 {
			detail += fmt.Sprintf("; the lease expires at %s and the task becomes dispatchable again then",
				time.UnixMilli(row.ExpiresAt).UTC().Format(time.RFC3339))
		}
		return detail
	default:
		return "task already has an active dispatch lease"
	}
}

func (s *SchedulerService) recordNoCandidate(ctx context.Context, projectID, taskID string, reasons []string) error {
	now := s.nowUnixMS()
	reason := strings.Join(reasons, "; ")
	if reason == "" {
		reason = "no runners available"
	}
	row := &storage.PlacementReasonRow{
		ProjectID: projectID,
		TaskID:    taskID,
		Decision:  "no_candidate",
		Reason:    reason,
		CreatedAt: now,
	}
	if encoded, err := json.Marshal(reasons); err == nil {
		row.MissingLabels = string(encoded)
	}
	if err := s.leases.RecordPlacementReason(ctx, row); err != nil {
		return fmt.Errorf("record placement reason for %s: %w", taskID, err)
	}
	// If a prior scheduling pass left a lease against a runner that is no
	// longer eligible, that lease is by definition stale — the current pass
	// has decided there is no candidate at all. Wipe it so observability tools
	// (PWA runner "PENDING" chip, dispatch diagnostics) don't report a task as
	// pending against a runner that will never execute it. Without this the
	// lease would linger for its full TTL and confuse the operator into
	// thinking the runner is holding work.
	if _, err := s.leases.ClearDispatchLease(ctx, projectID, taskID); err != nil {
		return fmt.Errorf("clear stale dispatch lease for %s: %w", taskID, err)
	}
	return nil
}
