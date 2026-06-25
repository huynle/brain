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
	RecordPlacementReason(ctx context.Context, row *storage.PlacementReasonRow) error
}

type schedulerLeaseExpirer interface {
	ExpireDispatchLeases(ctx context.Context, now int64) (int64, error)
}

type schedulerCommandPublisher interface {
	PublishRunnerCommand(runnerID string, command string, payload interface{})
}

type schedulerPauseChecker interface {
	IsPaused(projectID string) bool
	IsAutomationsPaused() bool
}

type schedulerProjectAutomationPauseChecker interface {
	IsAutomationsPausedForProject(projectID string) bool
}

// SchedulerService owns Brain push dispatch placement decisions.
type SchedulerService struct {
	tasks     schedulerTaskService
	projects  schedulerProjectLister
	runners   schedulerRunnerRegistry
	placement schedulerPlacementService
	leases    schedulerLeaseStore
	expirer   schedulerLeaseExpirer
	publisher schedulerCommandPublisher
	pauses    schedulerPauseChecker
	leaseTTL  time.Duration
	nowUnixMS func() int64

	mu     sync.RWMutex
	status schedulerStatusSnapshot
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
	for _, dep := range deps {
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
			svc.publisher = v
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
		if s.shouldSkipTask(projectID, task) {
			result.Skipped++
			continue
		}
		candidate, reasons := s.selectCandidate(task, projectID, runners, placement, reservedSlots)
		if candidate == nil {
			result.Skipped++
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
			continue
		}
		if s.publisher != nil {
			s.publisher.PublishRunnerCommand(candidate.RunnerID, "dispatch", map[string]any{
				"taskId":    task.ID,
				"projectId": projectID,
				"lease":     lease,
				"expiresAt": lease.ExpiresAt,
			})
		}
		reservedSlots[candidate.RunnerID]++
		result.Dispatched++
	}
	return result, nil
}

func (s *SchedulerService) shouldSkipTask(projectID string, task types.ResolvedTask) bool {
	if s.pauses == nil {
		return false
	}
	if s.pauses.IsPaused(projectID) {
		return true
	}
	if !strings.HasPrefix(task.GeneratedBy, "automation:") {
		return false
	}
	if scoped, ok := s.pauses.(schedulerProjectAutomationPauseChecker); ok {
		return scoped.IsAutomationsPausedForProject(projectID)
	}
	return s.pauses.IsAutomationsPaused()
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
			resp.Detail = "task already has an active dispatch lease"
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

	if s.publisher != nil {
		payload := map[string]any{
			"taskId":    task.ID,
			"projectId": projectID,
			"lease":     lease,
			"expiresAt": lease.ExpiresAt,
		}
		if force {
			payload["force"] = true
		}
		s.publisher.PublishRunnerCommand(candidate.RunnerID, "dispatch", payload)
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
			candidate = &schedulerMachineCandidate{machineID: machineID, score: scoreMachine(machineID, placement)}
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
	return "eligible", true
}

func scoreMachine(machineID string, placement *types.ProjectPlacement) int {
	score := 100
	if stringSliceContains(placement.PreferredMachines, machineID) {
		score += 50
	}
	if len(placement.AllowedMachines) > 0 && stringSliceContains(placement.AllowedMachines, machineID) {
		score += 25
	}
	return score
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
	return nil
}
