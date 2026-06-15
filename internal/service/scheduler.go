package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

const defaultDispatchLeaseTTL = 60 * time.Second

type schedulerTaskService interface {
	GetReady(ctx context.Context, projectID string, opts *api.TaskFilterOptions) ([]types.ResolvedTask, error)
}

type schedulerRunnerRegistry interface {
	ListRunners(ctx context.Context) ([]types.RunnerInfo, error)
}

type schedulerPlacementService interface {
	Get(ctx context.Context, projectID string) (*types.ProjectPlacement, error)
}

type schedulerLeaseStore interface {
	CreateDispatchLease(ctx context.Context, in storage.DispatchLeaseCreate) (*storage.DispatchLeaseRow, bool, error)
	RecordPlacementReason(ctx context.Context, row *storage.PlacementReasonRow) error
}

type schedulerCommandPublisher interface {
	PublishRunnerCommand(runnerID string, command string, payload interface{})
}

type schedulerPauseChecker interface {
	IsPaused(projectID string) bool
	IsAutomationsPaused() bool
}

// SchedulerService owns Brain push dispatch placement decisions.
type SchedulerService struct {
	tasks     schedulerTaskService
	runners   schedulerRunnerRegistry
	placement schedulerPlacementService
	leases    schedulerLeaseStore
	publisher schedulerCommandPublisher
	pauses    schedulerPauseChecker
	leaseTTL  time.Duration
	nowUnixMS func() int64
}

type SchedulerResult struct {
	ProjectID  string
	Considered int
	Dispatched int
	Skipped    int
}

// NewSchedulerService constructs a scheduler from already-tested service/storage primitives.
func NewSchedulerService(tasks schedulerTaskService, pauses schedulerPauseChecker, deps interface{}) *SchedulerService {
	svc := &SchedulerService{
		tasks:     tasks,
		pauses:    pauses,
		leaseTTL:  defaultDispatchLeaseTTL,
		nowUnixMS: func() int64 { return time.Now().UnixMilli() },
	}
	if v, ok := deps.(schedulerRunnerRegistry); ok {
		svc.runners = v
	}
	if v, ok := deps.(schedulerPlacementService); ok {
		svc.placement = v
	}
	if v, ok := deps.(schedulerLeaseStore); ok {
		svc.leases = v
	}
	if v, ok := deps.(schedulerCommandPublisher); ok {
		svc.publisher = v
	}
	return svc
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

	for _, task := range tasks {
		if s.shouldSkipTask(projectID, task) {
			result.Skipped++
			continue
		}
		candidate, reasons := s.selectCandidate(task, projectID, runners, placement)
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
	return s.pauses.IsAutomationsPaused() && strings.HasPrefix(task.GeneratedBy, "automation:")
}

type schedulerCandidate struct {
	runner types.RunnerInfo
	score  int
}

func (s *SchedulerService) selectCandidate(task types.ResolvedTask, projectID string, runners []types.RunnerInfo, placement *types.ProjectPlacement) (*types.RunnerInfo, []string) {
	candidates := make([]schedulerCandidate, 0, len(runners))
	reasons := make([]string, 0, len(runners))
	for _, runner := range runners {
		score, reason, ok := scoreRunner(task, projectID, runner, placement)
		if !ok {
			reasons = append(reasons, runner.RunnerID+": "+reason)
			continue
		}
		candidates = append(candidates, schedulerCandidate{runner: runner, score: score})
	}
	if len(candidates) == 0 {
		return nil, reasons
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if candidates[i].runner.ActiveTasks != candidates[j].runner.ActiveTasks {
			return candidates[i].runner.ActiveTasks < candidates[j].runner.ActiveTasks
		}
		return candidates[i].runner.RunnerID < candidates[j].runner.RunnerID
	})
	return &candidates[0].runner, reasons
}

func scoreRunner(task types.ResolvedTask, projectID string, runner types.RunnerInfo, placement *types.ProjectPlacement) (int, string, bool) {
	if runner.Status != types.RunnerStatusOnline {
		return 0, "runner not online", false
	}
	if !runner.DispatchPush {
		return 0, "runner does not support push dispatch", false
	}
	if runner.Draining {
		return 0, "runner draining", false
	}
	if !runnerAllowsProject(runner, projectID) {
		return 0, "project not allowed", false
	}
	if task.Executor != "" && !stringSliceContains(runner.Executors, task.Executor) {
		return 0, "executor not supported", false
	}
	if missing := missingStrings(task.RequiresCapability, runner.Capabilities); len(missing) > 0 {
		return 0, "missing task capabilities: " + strings.Join(missing, ","), false
	}
	if missing := missingStrings(placement.RequiredCapabilities, runner.Capabilities); len(missing) > 0 {
		return 0, "missing project capabilities: " + strings.Join(missing, ","), false
	}
	if missing := missingLabels(placement.RequiredLabels, runner.Labels); len(missing) > 0 {
		return 0, "missing required labels", false
	}
	if placement.WorkspacePolicy == types.WorkspacePolicyWorktree && len(runner.WorkspaceRoots) == 0 {
		return 0, "no workspace roots for worktree policy", false
	}
	if runner.MaxParallel > 0 && runner.ActiveTasks >= runner.MaxParallel {
		return 0, "runner at capacity", false
	}
	if placement.Affinity == types.PlacementAffinityStrict && !machineAllowedByStrictAffinity(runner.MachineID, placement) {
		return 0, "strict affinity mismatch", false
	}

	score := 100
	if stringSliceContains(placement.PreferredMachines, runner.MachineID) {
		score += 50
	}
	if len(placement.AllowedMachines) > 0 && stringSliceContains(placement.AllowedMachines, runner.MachineID) {
		score += 25
	}
	score -= runner.ActiveTasks
	return score, "eligible", true
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
