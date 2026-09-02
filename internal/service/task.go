package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/cron"
	"github.com/huynle/brain-api/pkg/frontmatter"
	"github.com/huynle/brain-api/pkg/markdown"
)

// Compile-time check that TaskServiceImpl implements api.TaskService.
var _ api.TaskService = (*TaskServiceImpl)(nil)

// TaskServiceImpl implements api.TaskService using a StorageLayer and persistent claims.
type TaskServiceImpl struct {
	config  *config.Config
	storage *storage.StorageLayer
	indexer *indexer.Indexer
}

// DefaultLeaseDuration is the default lease duration for task claims (10 minutes).
// This matches the previous stale claim threshold.
const DefaultLeaseDuration = 10 * time.Minute

// NewTaskService creates a new TaskServiceImpl.
//
// idx is required: CheckoutFeature writes a task file straight to disk, and
// without an indexer that file would be missing from SQLite (and therefore
// from search, the link graph, and orphan detection) until the next boot
// index. It is a constructor argument rather than an option so a caller
// cannot silently end up with the un-indexed behaviour.
func NewTaskService(cfg *config.Config, store *storage.StorageLayer, idx *indexer.Indexer) *TaskServiceImpl {
	return &TaskServiceImpl{
		config:  cfg,
		storage: store,
		indexer: idx,
	}
}

// DefaultClaimCleanupInterval is the default interval for the background claim cleanup goroutine.
const DefaultClaimCleanupInterval = 60 * time.Second

// StartClaimCleanup launches a background goroutine that periodically expires stale claims.
// The goroutine calls storage.ExpireStaleClaims on each tick and logs the count of removed claims.
// It respects context cancellation for clean shutdown.
func (s *TaskServiceImpl) StartClaimCleanup(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.Info("claim cleanup goroutine stopped")
				return
			case <-ticker.C:
				count, err := s.storage.ExpireStaleClaims(ctx)
				if err != nil {
					slog.Error("claim cleanup failed", "error", err)
					continue
				}
				if count > 0 {
					slog.Info("expired stale claims", "count", count)
				}
			}
		}
	}()
}

// isExpired returns true if the claim's lease has expired (expires_at < now).
func isExpired(claim *storage.TaskClaimRow) bool {
	return time.Now().UnixMilli() > claim.ExpiresAt
}

// Abandonment reason codes surfaced on ResolvedTask.AbandonReason. The set is
// intentionally narrow — every value corresponds to an underlying signal
// produced by an existing background job (StartClaimCleanup, RunLifecycleSweep,
// reapOrphanedTasks) so no new sweeper is introduced by the resume feature.
const (
	AbandonReasonNoClaim       = "no_claim"       // status=in_progress but no task_claims row (never claimed or already cleaned)
	AbandonReasonClaimExpired  = "claim_expired"  // claim exists but expires_at < now
	AbandonReasonRunnerOffline = "runner_offline" // claim exists, unexpired, but runner status is offline/stale
	AbandonReasonOrphanReaped  = "orphan_reaped"  // reapOrphanedTasks transitioned this task to blocked
)

// OrphanReaperMarker is the exact note text the runner-side orphan reaper
// appends when it transitions a stale in_progress task to blocked. Extracted
// as a constant so the reaper (runner.go) and the enrichAbandonmentState
// grep-fallback here cannot drift silently. If this text ever changes, both
// call sites update together.
const OrphanReaperMarker = "*Marked blocked by runner orphan reaper"

// ListProjects scans <brainDir>/projects/ for subdirectories containing a task/ subfolder.
func (s *TaskServiceImpl) ListProjects(ctx context.Context) ([]string, error) {
	projectsDir := filepath.Join(s.config.BrainDir, "projects")

	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("read projects dir: %w", err)
	}

	var projects []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		taskDir := filepath.Join(projectsDir, entry.Name(), "task")
		info, err := os.Stat(taskDir)
		if err != nil || !info.IsDir() {
			continue
		}
		projects = append(projects, entry.Name())
	}

	if projects == nil {
		projects = []string{}
	}
	return projects, nil
}

// GetTasks returns all tasks for a project with dependency resolution.
// Server-side task defaults are applied after resolution: empty task fields
// are filled from config.TaskDefaults so runners receive ready-to-use tasks.
func (s *TaskServiceImpl) GetTasks(ctx context.Context, projectId string) (*types.TaskListResponse, error) {
	entries, err := s.getAllTasks(ctx, projectId)
	if err != nil {
		return nil, err
	}
	result := ResolveDependencies(entries)
	s.applyTaskDefaults(result.Tasks)
	if err := s.enrichDispatchDiagnostics(ctx, projectId, result.Tasks); err != nil {
		return nil, err
	}
	if err := s.enrichAbandonmentState(ctx, projectId, result.Tasks); err != nil {
		return nil, err
	}
	if err := s.enrichUndispatchable(ctx, result.Tasks); err != nil {
		return nil, err
	}
	return result, nil
}

// enrichUndispatchable derives UndispatchableReason for pending tasks whose
// executor no live runner advertises.
//
// Read-only and derived at response time, following enrichAbandonmentState:
// no new sweeper, no stored state. One ListRunners call per response, not per
// task.
//
// Deliberately narrow. It reports ONLY the executor mismatch, which is a
// static property of the fleet's registrations — not capacity, capabilities,
// pauses or affinity, which are per-decision and already surface through
// placement reasons. A task is called undispatchable only when NO known
// runner could ever take it as configured.
func (s *TaskServiceImpl) enrichUndispatchable(ctx context.Context, tasks []types.ResolvedTask) error {
	if len(tasks) == 0 || s.storage == nil {
		return nil
	}
	var pending bool
	for i := range tasks {
		if tasks[i].Status == "pending" {
			pending = true
			break
		}
	}
	if !pending {
		return nil
	}

	runners, err := s.storage.ListRunners(ctx)
	if err != nil {
		// A diagnostic must never fail the listing it annotates.
		return nil
	}
	// An empty fleet means "no runner has registered yet", which is a normal
	// transient state at boot — not a mismatch to report against.
	supported := make(map[string]bool)
	var known int
	for _, r := range runners {
		if r.Status == "offline" {
			continue
		}
		known++
		for _, e := range r.Executors {
			supported[e] = true
		}
	}
	if known == 0 {
		return nil
	}

	for i := range tasks {
		t := &tasks[i]
		if t.Status != "pending" {
			continue
		}
		executor := t.Executor
		if executor == "" {
			// Matches filterByExecutors: an unset executor means opencode.
			executor = "opencode"
		}
		if supported[executor] {
			continue
		}
		t.UndispatchableReason = "no_runner_supports_executor:" + executor
	}
	return nil
}

func (s *TaskServiceImpl) enrichDispatchDiagnostics(ctx context.Context, projectID string, tasks []types.ResolvedTask) error {
	for i := range tasks {
		task := &tasks[i]
		lease, err := s.storage.GetDispatchLease(ctx, projectID, task.ID)
		if err != nil {
			return fmt.Errorf("get dispatch lease for %s: %w", task.ID, err)
		}
		// Cap per-task placement history at PlacementReasonRetention to
		// keep this hot-path bounded. The GetReady endpoint used to
		// take 5+ seconds when the DB had 75k+ decisions per task; the
		// PWA only needs the newest handful to show the "latest
		// placement decision" hint anyway. The unbounded history is
		// still available via the /placement-reasons diagnostic
		// endpoint for deep debugging.
		reasons, err := s.storage.ListPlacementReasonsLimit(ctx, projectID, task.ID, storage.PlacementReasonRetention)
		if err != nil {
			return fmt.Errorf("list placement reasons for %s: %w", task.ID, err)
		}
		task.DispatchLease = lease
		if len(reasons) > 0 {
			task.PlacementReasons = reasons
			task.LastPlacementReason = &task.PlacementReasons[len(task.PlacementReasons)-1]
		}
	}
	return nil
}

// enrichAbandonmentState derives IsAbandoned + AbandonReason on each task
// from signals produced by existing background jobs. No new sweeper. The
// definition of "abandoned":
//
//   - status="in_progress" AND (no claim | claim expired | claim's runner offline)
//   - status="blocked" AND body contains the orphan-reaper marker
//
// Everything else — pending, active, completed, cancelled, agent-self-blocked —
// is NOT abandoned and gets no Resume affordance.
//
// Runner-status cache: one runner lookup per unique runner_id across the task
// list, not per task, so a project with 100 tasks claimed by 3 runners does
// 3 lookups, not 100.
func (s *TaskServiceImpl) enrichAbandonmentState(ctx context.Context, projectID string, tasks []types.ResolvedTask) error {
	if len(tasks) == 0 {
		return nil
	}
	runnerStatus := make(map[string]string) // runnerID → status; "" means "not looked up yet"
	nowMs := time.Now().UnixMilli()

	for i := range tasks {
		task := &tasks[i]
		if task.Status != "in_progress" && task.Status != "blocked" {
			continue // fast-path: only these two statuses can be abandoned
		}

		if task.Status == "blocked" {
			// The orphan reaper is the only path that legitimately sets
			// blocked-with-abandonment. Agent-self-blocked and user-blocked
			// tasks stay non-resumable — the Blocked Task Inspector automation
			// covers those.
			if strings.Contains(task.Content, OrphanReaperMarker) {
				task.IsAbandoned = true
				task.AbandonReason = AbandonReasonOrphanReaped
			}
			continue
		}

		// status == "in_progress" — three sub-cases from the claim state.
		claim, err := s.storage.GetClaim(ctx, projectID, task.ID)
		if err != nil {
			return fmt.Errorf("get claim for %s: %w", task.ID, err)
		}
		if claim == nil {
			task.IsAbandoned = true
			task.AbandonReason = AbandonReasonNoClaim
			continue
		}
		if claim.ExpiresAt < nowMs {
			task.IsAbandoned = true
			task.AbandonReason = AbandonReasonClaimExpired
			continue
		}
		// Claim is unexpired — but the runner might still be dead (heartbeat
		// stopped before the claim naturally expired). Check runner status.
		status, seen := runnerStatus[claim.RunnerID]
		if !seen {
			runner, err := s.storage.GetRunner(ctx, claim.RunnerID)
			if err != nil {
				return fmt.Errorf("get runner %s: %w", claim.RunnerID, err)
			}
			if runner != nil {
				status = runner.Status
			}
			runnerStatus[claim.RunnerID] = status
		}
		if status == "offline" || status == "stale" {
			task.IsAbandoned = true
			task.AbandonReason = AbandonReasonRunnerOffline
		}
	}
	return nil
}

// GetTask returns a single resolved task by ID for a project.
// Server-side defaults are applied (same as GetTasks).
func (s *TaskServiceImpl) GetTask(ctx context.Context, projectId, taskId string) (*types.ResolvedTask, error) {
	resp, err := s.GetTasks(ctx, projectId)
	if err != nil {
		return nil, err
	}
	for _, t := range resp.Tasks {
		if t.ID == taskId {
			task := t
			return &task, nil
		}
	}
	// Wrap the shared sentinel rather than returning bare prose. Two handlers
	// worked around the unwrapped error by testing `task == nil` — which is true
	// on EVERY error path, so a storage failure was reported as "task not found"
	// with a 404. A third call site matched on the string "not found".
	return nil, fmt.Errorf("task %q not found in project %q: %w", taskId, projectId, api.ErrNotFound)
}

// applyTaskDefaults fills empty fields on resolved tasks from server-side
// config.TaskDefaults. Non-empty task fields are never overwritten (task
// frontmatter wins). Nil *bool fields get the config default; non-nil keep
// their value. If TaskDefaults is zero-value, this is a no-op.
func (s *TaskServiceImpl) applyTaskDefaults(tasks []types.ResolvedTask) {
	d := s.config.TaskDefaults

	for i := range tasks {
		resolveBuiltinMonitorPrompt(&tasks[i])
	}

	// Quick check: if all defaults are zero, nothing to do.
	if d.Agent == "" && d.Model == "" && d.Executor == "" &&
		len(d.Extensions) == 0 && d.ExecutionMode == "" &&
		d.CompleteOnIdle == nil && d.MergePolicy == "" && d.MergeStrategy == "" &&
		d.MergeTargetBranch == "" && d.RemoteBranchPolicy == "" &&
		d.OpenPRBeforeMerge == nil && d.TargetWorkdir == "" {
		return
	}

	for i := range tasks {
		t := &tasks[i]

		// String fields: fill only if task field is empty
		if t.Agent == "" && d.Agent != "" {
			t.Agent = d.Agent
		}
		if t.Model == "" && d.Model != "" {
			t.Model = d.Model
		}
		if t.Executor == "" && d.Executor != "" {
			t.Executor = d.Executor
		}
		if len(t.Extensions) == 0 && len(d.Extensions) > 0 {
			t.Extensions = append([]string(nil), d.Extensions...)
		}
		if t.ExecutionMode == "" && d.ExecutionMode != "" {
			t.ExecutionMode = d.ExecutionMode
		}
		if t.MergePolicy == "" && d.MergePolicy != "" {
			t.MergePolicy = d.MergePolicy
		}
		if t.MergeStrategy == "" && d.MergeStrategy != "" {
			t.MergeStrategy = d.MergeStrategy
		}
		if t.MergeTargetBranch == "" && d.MergeTargetBranch != "" {
			t.MergeTargetBranch = d.MergeTargetBranch
		}
		if t.RemoteBranchPolicy == "" && d.RemoteBranchPolicy != "" {
			t.RemoteBranchPolicy = d.RemoteBranchPolicy
		}
		if t.TargetWorkdir == "" && d.TargetWorkdir != "" {
			t.TargetWorkdir = d.TargetWorkdir
		}

		// Auto-derive git_branch from feature_id in worktree mode (defense-in-depth:
		// the runner layer also does this, but deriving here ensures the API response
		// already has the correct git_branch field set).
		if t.GitBranch == "" && t.ExecutionMode == "worktree" && t.FeatureID != "" {
			t.GitBranch = t.FeatureID
		}

		// *bool fields: fill only if task field is nil
		if t.CompleteOnIdle == nil && d.CompleteOnIdle != nil {
			v := *d.CompleteOnIdle
			t.CompleteOnIdle = &v
		}
		if t.OpenPRBeforeMerge == nil && d.OpenPRBeforeMerge != nil {
			v := *d.OpenPRBeforeMerge
			t.OpenPRBeforeMerge = &v
		}
	}
}

func resolveBuiltinMonitorPrompt(task *types.ResolvedTask) {
	for _, tag := range task.Tags {
		parsed := ParseMonitorTag(tag)
		if parsed == nil {
			continue
		}
		if _, ok := monitorTemplates[parsed.TemplateID]; !ok {
			continue
		}
		task.DirectPrompt = buildMonitorPrompt(parsed.TemplateID, parsed.Scope)
		return
	}
}

// getAllTasks fetches all task BrainEntries for a project from storage.
func (s *TaskServiceImpl) getAllTasks(ctx context.Context, projectId string) ([]types.BrainEntry, error) {
	pathPrefix := "projects/" + projectId + "/task"
	rows, err := s.storage.ListNotes(ctx, &storage.ListOptions{
		Type:       "task",
		PathPrefix: pathPrefix,
		Limit:      10000, // effectively unlimited
	})
	if err != nil {
		return nil, fmt.Errorf("list tasks for project %q: %w", projectId, err)
	}

	entries := make([]types.BrainEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, NoteRowToBrainEntry(row))
	}
	return entries, nil
}

// GetReady returns tasks that are ready to execute.
// If opts is non-nil and contains FeatureIDs, only tasks matching those features are returned.
// If opts contains Executors, only tasks matching those executor types are returned.
func (s *TaskServiceImpl) GetReady(ctx context.Context, projectId string, opts *api.TaskFilterOptions) ([]types.ResolvedTask, error) {
	result, err := s.GetTasks(ctx, projectId)
	if err != nil {
		return nil, err
	}
	ready := GetReadyTasks(result)
	return s.applyTaskFilterOptions(ctx, projectId, ready, opts)
}

// GetWaiting returns tasks waiting on dependencies.
func (s *TaskServiceImpl) GetWaiting(ctx context.Context, projectId string) ([]types.ResolvedTask, error) {
	result, err := s.GetTasks(ctx, projectId)
	if err != nil {
		return nil, err
	}
	return GetWaitingTasks(result), nil
}

// GetBlocked returns tasks that are blocked.
func (s *TaskServiceImpl) GetBlocked(ctx context.Context, projectId string) ([]types.ResolvedTask, error) {
	result, err := s.GetTasks(ctx, projectId)
	if err != nil {
		return nil, err
	}
	return GetBlockedTasks(result), nil
}

// GetNext returns the next task to execute (highest priority ready task).
// If opts is non-nil and contains FeatureIDs, only tasks matching those features are considered.
// If opts contains Executors, only tasks matching those executor types are considered.
func (s *TaskServiceImpl) GetNext(ctx context.Context, projectId string, opts *api.TaskFilterOptions) (*types.ResolvedTask, error) {
	result, err := s.GetTasks(ctx, projectId)
	if err != nil {
		return nil, err
	}
	ready := GetReadyTasks(result)
	ready, err = s.filterDispatchReservedTasks(ctx, projectId, ready, runnerIDFromOptions(opts))
	if err != nil {
		return nil, err
	}
	if opts != nil {
		ready, err = s.applyTaskFilterOptions(ctx, projectId, ready, opts)
		if err != nil {
			return nil, err
		}
	}
	return pickHighestPriority(ready), nil
}

func (s *TaskServiceImpl) applyTaskFilterOptions(ctx context.Context, projectID string, tasks []types.ResolvedTask, opts *api.TaskFilterOptions) ([]types.ResolvedTask, error) {
	if opts == nil {
		return tasks, nil
	}
	if len(opts.FeatureIDs) > 0 {
		tasks = filterByFeatureIDs(tasks, opts.FeatureIDs)
	}
	if len(opts.Executors) > 0 {
		tasks = filterByExecutors(tasks, opts.Executors)
	}
	if opts.GeneratedByPrefix != "" {
		tasks = filterByGeneratedByPrefix(tasks, opts.GeneratedByPrefix)
	}
	if opts.RunnerID == "" {
		return tasks, nil
	}

	runner, err := s.storage.GetRunner(ctx, opts.RunnerID)
	if err != nil {
		return nil, fmt.Errorf("get runner %q: %w", opts.RunnerID, err)
	}
	if runner == nil {
		// Unknown runner: we cannot check executors, capabilities or labels,
		// and historically that meant no filtering at all. Machine affinity
		// cannot inherit that default. An unregistered runner has no machine
		// id to compare, so serving it a machine_affinity=local task would
		// hand pinned work to the one caller we know least about — the
		// bypass is trivial and silent. Withhold only those tasks; every
		// other filter keeps its existing fail-open behavior.
		return filterOutMachinePinnedTasks(tasks), nil
	}

	return s.filterByRunnerEligibility(ctx, projectID, tasks, runner)
}

// filterOutMachinePinnedTasks drops every task whose resolved affinity is
// "local". Used when the requesting runner's machine cannot be established at
// all, where the honest answer is "this task is not for you".
func filterOutMachinePinnedTasks(tasks []types.ResolvedTask) []types.ResolvedTask {
	filtered := make([]types.ResolvedTask, 0, len(tasks))
	for _, task := range tasks {
		if types.ResolveMachineAffinity(task.MachineAffinity, task.OriginMachineID) == types.MachineAffinityLocal {
			continue
		}
		filtered = append(filtered, task)
	}
	return filtered
}

func (s *TaskServiceImpl) filterByRunnerEligibility(ctx context.Context, projectID string, tasks []types.ResolvedTask, runner *storage.RunnerRow) ([]types.ResolvedTask, error) {
	if len(runner.Executors) > 0 {
		tasks = filterByExecutors(tasks, runner.Executors)
	}

	capabilities := make(map[string]bool, len(runner.Capabilities))
	for _, capability := range runner.Capabilities {
		capabilities[capability] = true
	}

	filtered := make([]types.ResolvedTask, 0, len(tasks))
	for _, task := range tasks {
		if !runnerHasRequiredCapabilities(task, capabilities) {
			continue
		}
		// Mirror of the push path's check in runnerEligibleForTask. A
		// machine_affinity=local task must be invisible to a polling runner
		// on another machine, or the pull path quietly undoes the pin.
		if _, ok := machineAffinitySatisfied(task, runnerMachineID(runner)); !ok {
			continue
		}
		if task.FeatureID != "" {
			assignment, err := s.storage.GetFeatureAssignment(ctx, projectID, task.FeatureID)
			if err != nil {
				return nil, fmt.Errorf("get feature assignment %q/%q: %w", projectID, task.FeatureID, err)
			}
			if assignment != nil && assignment.RunnerID != runner.RunnerID {
				continue
			}
		}
		filtered = append(filtered, task)
	}
	return filtered, nil
}

func filterByGeneratedByPrefix(tasks []types.ResolvedTask, prefix string) []types.ResolvedTask {
	filtered := make([]types.ResolvedTask, 0, len(tasks))
	for _, task := range tasks {
		if strings.HasPrefix(task.GeneratedBy, prefix) {
			filtered = append(filtered, task)
		}
	}
	return filtered
}

func runnerIDFromOptions(opts *api.TaskFilterOptions) string {
	if opts == nil {
		return ""
	}
	return opts.RunnerID
}

func (s *TaskServiceImpl) filterDispatchReservedTasks(ctx context.Context, projectID string, tasks []types.ResolvedTask, runnerID string) ([]types.ResolvedTask, error) {
	if len(tasks) == 0 {
		return tasks, nil
	}

	now := time.Now().UnixMilli()
	filtered := make([]types.ResolvedTask, 0, len(tasks))
	for _, task := range tasks {
		lease, err := s.storage.GetDispatchLeaseRow(ctx, projectID, task.ID)
		if err != nil {
			return nil, err
		}
		if activeDispatchLeaseForOtherRunner(lease, runnerID, now) {
			continue
		}
		filtered = append(filtered, task)
	}
	return filtered, nil
}

func activeDispatchLeaseForOtherRunner(lease *storage.DispatchLeaseRow, runnerID string, now int64) bool {
	if !isActiveDispatchLease(lease, now) {
		return false
	}
	return runnerID == "" || lease.AssignedRunnerID != runnerID
}

func isActiveDispatchLease(lease *storage.DispatchLeaseRow, now int64) bool {
	if lease == nil || lease.ExpiresAt < now {
		return false
	}
	return lease.State == storage.DispatchLeaseStatePushed || lease.State == storage.DispatchLeaseStateAcked
}

func runnerHasRequiredCapabilities(task types.ResolvedTask, capabilities map[string]bool) bool {
	for _, required := range task.RequiresCapability {
		if !capabilities[required] {
			return false
		}
	}
	return true
}

// ClaimTask claims a task for a runner. Returns ErrConflict if already claimed by another runner.
// Claims are persisted to SQLite via the storage layer, surviving API server restarts.
func (s *TaskServiceImpl) ClaimTask(ctx context.Context, projectId, taskId, runnerId string) (*types.ClaimResponse, error) {
	return s.ClaimTaskWithDuration(ctx, projectId, taskId, runnerId, DefaultLeaseDuration)
}

// ClaimTaskWithDuration claims a task with a custom lease duration.
// Used for pre-claims (dispatch) with shorter expiry.
func (s *TaskServiceImpl) ClaimTaskWithDuration(ctx context.Context, projectId, taskId, runnerId string, leaseDuration time.Duration) (*types.ClaimResponse, error) {
	featureID, err := s.featureIDForTaskClaim(ctx, projectId, taskId)
	if err != nil {
		return nil, err
	}

	lease, err := s.storage.GetDispatchLeaseRow(ctx, projectId, taskId)
	if err != nil {
		return nil, err
	}
	if activeDispatchLeaseForOtherRunner(lease, runnerId, time.Now().UnixMilli()) {
		slog.Warn("claim conflict with dispatch lease", "project", projectId, "task_id", taskId, "runner_id", runnerId, "assigned_runner", lease.AssignedRunnerID)
		stale := false
		return &types.ClaimResponse{
			Success:   false,
			TaskID:    taskId,
			RunnerID:  runnerId,
			Error:     "dispatch lease reserved",
			Message:   fmt.Sprintf("task %s is reserved for runner %s", taskId, lease.AssignedRunnerID),
			ClaimedBy: lease.AssignedRunnerID,
			IsStale:   &stale,
		}, api.ErrConflict
	}

	ok, existing, err := s.storage.ClaimTask(ctx, projectId, taskId, runnerId, leaseDuration)
	if err != nil {
		return nil, fmt.Errorf("storage claim task: %w", err)
	}

	if !ok {
		// Claim failed — another active runner holds it
		stale := false
		if existing != nil {
			stale = isExpired(existing)
		}
		holder := ""
		if existing != nil {
			holder = existing.RunnerID
		}
		slog.Warn("claim conflict", "project", projectId, "task_id", taskId, "runner_id", runnerId, "held_by", holder)
		return &types.ClaimResponse{
			Success:   false,
			TaskID:    taskId,
			RunnerID:  runnerId,
			Error:     "already claimed",
			Message:   fmt.Sprintf("task %s is already claimed by %s", taskId, holder),
			ClaimedBy: holder,
			IsStale:   &stale,
		}, api.ErrConflict
	}
	if featureID != "" {
		assigned, assignment, err := s.storage.AssignFeatureIfEmpty(ctx, projectId, featureID, runnerId, "auto", "active")
		if err != nil {
			if releaseErr := s.ReleaseTask(ctx, projectId, taskId, runnerId); releaseErr != nil {
				slog.Warn("failed to release task after feature assignment error", "project", projectId, "task_id", taskId, "runner_id", runnerId, "release_error", releaseErr)
			}
			return nil, fmt.Errorf("assign feature for claimed task: %w", err)
		}
		if !assigned {
			if assignment == nil {
				if releaseErr := s.ReleaseTask(ctx, projectId, taskId, runnerId); releaseErr != nil {
					slog.Warn("failed to release task after missing feature assignment", "project", projectId, "task_id", taskId, "runner_id", runnerId, "release_error", releaseErr)
				}
				return nil, fmt.Errorf("feature %q assignment disappeared", featureID)
			}
			if assignment.RunnerID != runnerId {
				if releaseErr := s.ReleaseTask(ctx, projectId, taskId, runnerId); releaseErr != nil {
					slog.Warn("failed to release task after feature assignment conflict", "project", projectId, "task_id", taskId, "runner_id", runnerId, "release_error", releaseErr)
				}
				slog.Warn("feature assignment conflict", "project", projectId, "feature_id", featureID, "runner_id", runnerId, "assigned_to", assignment.RunnerID)
				return &types.ClaimResponse{
					Success:   false,
					TaskID:    taskId,
					RunnerID:  runnerId,
					Error:     "feature assigned to another runner",
					Message:   fmt.Sprintf("feature %s is assigned to %s", featureID, assignment.RunnerID),
					ClaimedBy: assignment.RunnerID,
				}, api.ErrConflict
			}
		}
	}

	claimedAt := time.Now().UTC().Format(time.RFC3339)
	slog.Info("task claimed", "project", projectId, "task_id", taskId, "runner_id", runnerId)
	return &types.ClaimResponse{
		Success:   true,
		TaskID:    taskId,
		RunnerID:  runnerId,
		ClaimedAt: claimedAt,
	}, nil
}

func (s *TaskServiceImpl) featureIDForTaskClaim(ctx context.Context, projectID, taskID string) (string, error) {
	note, err := s.storage.GetNoteByPath(ctx, fmt.Sprintf("projects/%s/task/%s.md", projectID, taskID))
	if err != nil {
		return "", fmt.Errorf("get task note for claim: %w", err)
	}
	if note == nil {
		return "", nil
	}
	return NoteRowToBrainEntry(note).FeatureID, nil
}

// ReleaseTask releases a task claim. Returns ErrNotFound if not claimed,
// ErrConflict if claimed by a different runner.
func (s *TaskServiceImpl) ReleaseTask(ctx context.Context, projectId, taskId, runnerId string) error {
	// First check if a claim exists at all and who owns it
	existing, err := s.storage.GetClaim(ctx, projectId, taskId)
	if err != nil {
		return fmt.Errorf("storage get claim: %w", err)
	}
	if existing == nil {
		return api.ErrNotFound
	}
	if existing.RunnerID != runnerId {
		return api.ErrConflict
	}

	released, err := s.storage.ReleaseClaim(ctx, projectId, taskId, runnerId)
	if err != nil {
		return fmt.Errorf("storage release claim: %w", err)
	}
	if !released {
		return api.ErrNotFound
	}

	slog.Info("task released", "project", projectId, "task_id", taskId, "runner_id", runnerId)
	return nil
}

// AckDispatch acknowledges that a runner received a pushed dispatch command.
func (s *TaskServiceImpl) AckDispatch(ctx context.Context, projectId, taskId, runnerId, leaseId string) (*types.DispatchAckResponse, error) {
	now := time.Now().UnixMilli()
	ok, err := s.storage.AckDispatchLease(ctx, projectId, taskId, runnerId, leaseId, now)
	resp := &types.DispatchAckResponse{Success: ok, ProjectID: projectId, TaskID: taskId, RunnerID: runnerId, LeaseID: leaseId}
	if err != nil {
		return resp, fmt.Errorf("storage ack dispatch lease: %w", err)
	}
	if !ok {
		resp.Error = "dispatch lease not found or not ackable"
		return resp, api.ErrNotFound
	}
	return resp, nil
}

// RejectDispatch records a structured rejection reason for a pushed dispatch lease.
func (s *TaskServiceImpl) RejectDispatch(ctx context.Context, projectId, taskId, runnerId, leaseId string, reason types.DispatchRejectReason) (*types.DispatchRejectResponse, error) {
	now := time.Now().UnixMilli()
	encodedReason, err := json.Marshal(reason)
	if err != nil {
		return nil, fmt.Errorf("marshal dispatch rejection reason: %w", err)
	}
	ok, err := s.storage.RejectDispatchLease(ctx, projectId, taskId, runnerId, leaseId, now, string(encodedReason))
	resp := &types.DispatchRejectResponse{Success: ok, ProjectID: projectId, TaskID: taskId, RunnerID: runnerId, LeaseID: leaseId, Reason: reason}
	if err != nil {
		return resp, fmt.Errorf("storage reject dispatch lease: %w", err)
	}
	if !ok {
		resp.Error = "dispatch lease not found or not rejectable"
		return resp, api.ErrNotFound
	}

	// Record the rejection into the append-only placement-reason history so
	// the reason survives the next push overwriting the single-row dispatch
	// lease. task_dispatch_leases is keyed by (project_id, task_id) and holds
	// only the LATEST attempt, so a task rejected every 5s (e.g. an AI-mode
	// checkout with no git context looping on "workdir_unavailable") shows a
	// fresh lease each time and no trace of how many times it tried or why.
	// task_placement_reasons is the history table already surfaced on
	// ResolvedTask.PlacementReasons and in the PWA task panel; recording here
	// gives the operator a visible "N attempts, reason: workdir_unavailable"
	// list instead of a task that silently sits pending forever.
	//
	// Best-effort: the reject itself already succeeded, so a history write
	// failure must not turn a successful rejection into an error.
	humanReason := reason.Code
	if reason.Message != "" {
		if humanReason != "" {
			humanReason += ": " + reason.Message
		} else {
			humanReason = reason.Message
		}
	}
	if humanReason == "" {
		humanReason = "dispatch rejected by runner"
	}
	var machineID string
	if lease, lerr := s.storage.GetDispatchLeaseRow(ctx, projectId, taskId); lerr == nil && lease != nil {
		machineID = lease.AssignedMachineID
	}
	prRow := &storage.PlacementReasonRow{
		ProjectID: projectId,
		TaskID:    taskId,
		RunnerID:  runnerId,
		MachineID: machineID,
		Decision:  "dispatch_rejected",
		Reason:    humanReason,
		CreatedAt: now,
	}
	if len(reason.Details) > 0 {
		if encoded, derr := json.Marshal(reason.Details); derr == nil {
			prRow.MissingLabels = string(encoded)
		}
	}
	if perr := s.storage.RecordPlacementReason(ctx, prRow); perr != nil {
		slog.Warn("record dispatch rejection history failed",
			"project", projectId, "task_id", taskId, "runner_id", runnerId, "error", perr)
	}

	return resp, nil
}

// ReleaseDispatch explicitly releases/finalizes a dispatch lease owned by a runner.
func (s *TaskServiceImpl) ReleaseDispatch(ctx context.Context, projectId, taskId, runnerId string) (*types.DispatchReleaseResponse, error) {
	ok, err := s.storage.ReleaseDispatchLease(ctx, projectId, taskId, runnerId)
	resp := &types.DispatchReleaseResponse{Success: ok, ProjectID: projectId, TaskID: taskId, RunnerID: runnerId}
	if err != nil {
		return resp, fmt.Errorf("storage release dispatch lease: %w", err)
	}
	if !ok {
		resp.Error = "dispatch lease not found"
		return resp, api.ErrNotFound
	}
	return resp, nil
}

// RenewClaim extends the claim's expiry by DefaultLeaseDuration.
// Returns ErrNotFound if the claim doesn't exist, is expired, or is owned by a different runner.
func (s *TaskServiceImpl) RenewClaim(ctx context.Context, projectId, taskId, runnerId string) (*types.RenewClaimResponse, error) {
	// Verify the claim exists and is owned by this runner
	existing, err := s.storage.GetClaim(ctx, projectId, taskId)
	if err != nil {
		return nil, fmt.Errorf("storage get claim: %w", err)
	}
	if existing == nil {
		return &types.RenewClaimResponse{
			Success:  false,
			TaskID:   taskId,
			RunnerID: runnerId,
			Error:    "claim not found",
		}, api.ErrNotFound
	}
	if existing.RunnerID != runnerId {
		return &types.RenewClaimResponse{
			Success:  false,
			TaskID:   taskId,
			RunnerID: runnerId,
			Error:    "claim owned by different runner",
		}, api.ErrConflict
	}
	if isExpired(existing) {
		return &types.RenewClaimResponse{
			Success:  false,
			TaskID:   taskId,
			RunnerID: runnerId,
			Error:    "claim expired",
		}, api.ErrNotFound
	}

	// Extend the expiry
	newExpiry := time.Now().Add(DefaultLeaseDuration)
	if err := s.storage.RenewClaim(ctx, projectId, taskId, runnerId, newExpiry); err != nil {
		return nil, fmt.Errorf("storage renew claim: %w", err)
	}

	slog.Info("claim renewed", "project", projectId, "task_id", taskId, "runner_id", runnerId, "expires_at", newExpiry.UTC().Format(time.RFC3339))
	return &types.RenewClaimResponse{
		Success:   true,
		TaskID:    taskId,
		RunnerID:  runnerId,
		ExpiresAt: newExpiry.UTC().Format(time.RFC3339),
	}, nil
}

// GetClaimStatus returns the claim status of a task.
func (s *TaskServiceImpl) GetClaimStatus(ctx context.Context, projectId, taskId string) (*types.ClaimStatusResponse, error) {
	existing, err := s.storage.GetClaim(ctx, projectId, taskId)
	if err != nil {
		return nil, fmt.Errorf("storage get claim: %w", err)
	}

	if existing == nil {
		return &types.ClaimStatusResponse{
			TaskID:  taskId,
			Claimed: false,
			IsStale: false,
		}, nil
	}

	expired := isExpired(existing)
	claimedAt := time.UnixMilli(existing.ClaimedAt).UTC().Format(time.RFC3339)
	return &types.ClaimStatusResponse{
		TaskID:    taskId,
		Claimed:   true,
		RunnerID:  existing.RunnerID,
		ClaimedAt: claimedAt,
		IsStale:   expired,
	}, nil
}

// GetLiveClaim reports whether a task is actively held by a live runner.
//
// "Live" means all three of: a claim row exists, it has not expired, and
// the owning runner is currently online. Anything less is precisely the
// abandoned case — a crashed runner's claim, or a lease nobody renewed —
// which the resume flow is designed to recover, and which must therefore
// NOT block a destructive operation.
//
// Mirrors the live-claim safety check in ResumeTask; both exist so that
// "is someone actually running this right now?" has one answer.
func (s *TaskServiceImpl) GetLiveClaim(ctx context.Context, projectId, taskId string) (*types.LiveClaim, error) {
	claim, err := s.storage.GetClaim(ctx, projectId, taskId)
	if err != nil {
		return nil, fmt.Errorf("storage get claim: %w", err)
	}
	if claim == nil || isExpired(claim) {
		return &types.LiveClaim{Live: false}, nil
	}

	// A claim we cannot attribute to an online runner is treated as not
	// live: failing open here keeps a degraded runner registry from
	// blocking every delete in the system.
	runner, rerr := s.storage.GetRunner(ctx, claim.RunnerID)
	if rerr != nil || runner == nil || runner.Status != "online" {
		return &types.LiveClaim{Live: false}, nil
	}

	return &types.LiveClaim{Live: true, RunnerID: claim.RunnerID}, nil
}

// DispatchTask creates a lease-compatible direct dispatch for a target runner.
// Dispatch pre-claims and leases have a shorter 60-second expiry to allow quick recovery if the runner doesn't respond.
func (s *TaskServiceImpl) DispatchTask(ctx context.Context, projectId, taskId, targetRunnerId string) (*types.DispatchResponse, error) {
	const dispatchLeaseDuration = 60 * time.Second
	claimResp, err := s.ClaimTaskWithDuration(ctx, projectId, taskId, targetRunnerId, dispatchLeaseDuration)
	if err != nil {
		return dispatchResponseFromClaim(claimResp, taskId, targetRunnerId), err
	}

	now := time.Now()
	lease, created, err := s.storage.CreateDispatchLease(ctx, storage.DispatchLeaseCreate{
		ProjectID:        projectId,
		TaskID:           taskId,
		AssignedRunnerID: targetRunnerId,
		PushedAt:         now.UnixMilli(),
		ExpiresAt:        now.Add(dispatchLeaseDuration).UnixMilli(),
	})
	if err != nil {
		return nil, fmt.Errorf("create dispatch lease: %w", err)
	}
	if !created {
		stale := false
		holder := ""
		if lease != nil {
			holder = lease.AssignedRunnerID
		}
		return &types.DispatchResponse{
			Success:   false,
			TaskID:    taskId,
			RunnerID:  targetRunnerId,
			Error:     "dispatch lease exists",
			ClaimedBy: holder,
			IsStale:   &stale,
		}, api.ErrConflict
	}

	resp := dispatchResponseFromClaim(claimResp, taskId, targetRunnerId)
	resp.LeaseID = lease.LeaseID
	resp.ExpiresAt = time.UnixMilli(lease.ExpiresAt).UTC().Format(time.RFC3339)
	return resp, nil
}

func dispatchResponseFromClaim(claimResp *types.ClaimResponse, taskId, runnerId string) *types.DispatchResponse {
	resp := &types.DispatchResponse{Success: false, TaskID: taskId, RunnerID: runnerId}
	if claimResp == nil {
		return resp
	}
	resp.Success = claimResp.Success
	resp.TaskID = claimResp.TaskID
	resp.RunnerID = claimResp.RunnerID
	resp.Error = claimResp.Error
	resp.Message = claimResp.Message
	resp.ClaimedBy = claimResp.ClaimedBy
	resp.IsStale = claimResp.IsStale
	return resp
}

// GetMultiTaskStatus returns status of multiple tasks.
func (s *TaskServiceImpl) GetMultiTaskStatus(ctx context.Context, projectId string, req types.MultiTaskStatusRequest) (*types.MultiTaskStatusResponse, error) {
	result, err := s.GetTasks(ctx, projectId)
	if err != nil {
		return nil, err
	}

	// Build lookup map of resolved tasks by ID
	taskMap := make(map[string]types.ResolvedTask, len(result.Tasks))
	for _, t := range result.Tasks {
		taskMap[t.ID] = t
	}

	// Collect requested tasks.
	//
	// Ids that resolve to nothing are recorded rather than skipped. This is the
	// gate an orchestrator polls to decide whether spawned subtasks finished, so
	// a silently-dropped id was answered with "all completed" — vacuously true
	// for a task that never existed. A mistyped id, an id from another project,
	// or an archived-away task all took that path.
	var tasks []types.ResolvedTask
	var notFound []string
	allCompleted := true
	for _, id := range req.TaskIDs {
		t, ok := taskMap[id]
		if !ok {
			notFound = append(notFound, id)
			// Cannot assert completion about a task we could not find.
			allCompleted = false
			continue
		}
		tasks = append(tasks, t)
		if t.Status != "completed" && t.Status != "validated" {
			allCompleted = false
		}
	}

	if tasks == nil {
		tasks = []types.ResolvedTask{}
	}
	// An empty request asserts nothing, so it completes nothing.
	if len(req.TaskIDs) == 0 {
		allCompleted = false
	}

	return &types.MultiTaskStatusResponse{
		Tasks:        tasks,
		AllCompleted: allCompleted,
		NotFound:     notFound,
	}, nil
}

// GetTasksByFeature returns all resolved tasks belonging to a specific feature.
// This satisfies the FeatureTaskLister interface used by EventServiceImpl
// for server-side feature completion detection.
func (s *TaskServiceImpl) GetTasksByFeature(ctx context.Context, projectID, featureID string) ([]types.ResolvedTask, error) {
	result, err := s.GetTasks(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return filterByFeatureIDs(result.Tasks, []string{featureID}), nil
}

// GetFeatures returns computed features for a project.
func (s *TaskServiceImpl) GetFeatures(ctx context.Context, projectId string) (*types.FeatureListResponse, error) {
	result, err := s.GetTasks(ctx, projectId)
	if err != nil {
		return nil, err
	}

	featureResult := ComputeAndResolveFeatures(result.Tasks)
	return featuresToResponse(featureResult.Features), nil
}

// GetReadyFeatures returns features that are ready.
func (s *TaskServiceImpl) GetReadyFeatures(ctx context.Context, projectId string) (*types.FeatureListResponse, error) {
	result, err := s.GetTasks(ctx, projectId)
	if err != nil {
		return nil, err
	}

	featureResult := ComputeAndResolveFeatures(result.Tasks)
	readyFeatures := GetReadyFeatures(featureResult.Features)
	return featuresToResponse(readyFeatures), nil
}

// GetFeature returns a single feature by ID.
func (s *TaskServiceImpl) GetFeature(ctx context.Context, projectId, featureId string) (*types.FeatureResponse, error) {
	result, err := s.GetTasks(ctx, projectId)
	if err != nil {
		return nil, err
	}

	featureResult := ComputeAndResolveFeatures(result.Tasks)
	for _, f := range featureResult.Features {
		if f.ID == featureId {
			return &types.FeatureResponse{
				Feature: computedFeatureToFeature(f),
			}, nil
		}
	}

	return nil, api.ErrNotFound
}

// CheckoutFeature marks a feature for checkout.
func (s *TaskServiceImpl) CheckoutFeature(ctx context.Context, projectId, featureId string, opts *types.FeatureCheckoutOptions) (*types.CheckoutFeatureResult, error) {
	// Sanitize inputs
	sanitizedProjectID := strings.TrimSpace(projectId)
	sanitizedFeatureID := strings.TrimSpace(featureId)

	if sanitizedProjectID == "" {
		return nil, fmt.Errorf("projectId is required")
	}
	if sanitizedFeatureID == "" {
		return nil, fmt.Errorf("featureId is required")
	}

	// Normalize options with defaults
	normalizedOpts := normalizeFeatureCheckoutOptions(opts)

	// Generate checkout task key
	generatedKey := fmt.Sprintf("feature-checkout:%s:round-1", sanitizedFeatureID)

	// Check if checkout task already exists (idempotency)
	taskDir := filepath.Join(s.config.BrainDir, "projects", sanitizedProjectID, "task")
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create task directory: %w", err)
	}

	// Look for an existing checkout task with this generated_key.
	//
	// The key carries no mode and no round counter, so a second "Review &
	// merge…" on the same feature used to be a permanent no-op that threw
	// away whatever the user had just chosen — including the checkout mode.
	// A user whose first attempt produced an AI task could never get the
	// simple path to take, and the modal reported it as reassurance rather
	// than as a refusal.
	//
	// A still-pending task has not been claimed by anything, so replacing it
	// with the mode actually requested loses no work. Anything past pending
	// is left alone: it is running or has run, and quietly deleting it would
	// discard real history.
	existingTask, err := findCheckoutTaskByKey(taskDir, generatedKey)
	var supersededID string
	if err == nil && existingTask != nil {
		// A checkout task written before this build RECORDED checkout_mode
		// without acting on it, so a stored "simple" task can carry no
		// executor and no script — the exact shape that could never run. It
		// matches on mode, so a mode comparison alone would hand that dead
		// task back forever and the fix would never reach any feature that
		// already had one. Compare what the task can DO, not just what it
		// says.
		stale := normalizedOpts.CheckoutMode == "simple" &&
			existingTask.Executor != "script"
		// Compare FOLDED modes. "" is a first-class stored value — the write
		// side deliberately omits checkout_mode rather than persisting "ai"
		// as a default, and every consumer treats empty as "ai". A raw string
		// compare made the two front doors disagree by construction: the PWA
		// always sends "ai", the MCP tool passes "" through when the caller
		// omits it, so alternating between them deleted and recreated a
		// byte-identical task forever, never converging — losing the task's
		// id and resetting its retry attempt_count each time.
		sameMode := foldCheckoutModeValue(existingTask.Mode) ==
			foldCheckoutModeValue(normalizedOpts.CheckoutMode)
		replaceable := existingTask.Resp.Status == "pending"
		if (sameMode && !stale) || !replaceable {
			return &types.CheckoutFeatureResult{
				Created:      false,
				GeneratedKey: generatedKey,
				Task:         existingTask.Resp,
			}, nil
		}
		if err := os.Remove(existingTask.FilePath); err != nil {
			return nil, fmt.Errorf("supersede checkout task %s: %w", existingTask.Resp.ID, err)
		}
		if s.indexer != nil {
			// Best-effort: the file is already gone, and a stale index row
			// must not fail a checkout the user asked for.
			_ = s.indexer.RemoveFile(existingTask.Resp.Path)
		}
		supersededID = existingTask.Resp.ID
	}

	// Get all feature tasks to build depends_on list
	featureTasks, err := s.getFeatureTasksFromFilesystem(sanitizedProjectID, sanitizedFeatureID)
	if err != nil {
		return nil, fmt.Errorf("failed to get feature tasks: %w", err)
	}

	// Extract non-generated task IDs
	dependsOn := extractUniqueNonGeneratedTaskIds(featureTasks)

	// Generate new task ID
	shortID := markdown.GenerateShortID()
	filename := shortID + ".md"
	taskPath := filepath.Join(taskDir, filename)

	// Resolve the repo the feature's work actually happened in. The manual
	// endpoint inherits nothing from an automation entry, so without this the
	// checkout task carries no workdir at all and the runner resolves it
	// against whatever its own default happens to be — which for a git merge
	// means merging some unrelated checkout, or none.
	//
	// This context is applied to the checkout task in ALL modes below, not
	// just the simple/script path. An AI-mode checkout runs in worktree mode,
	// and worktree mode needs a valid git repo context (target_workdir/workdir
	// or git_remote+repo_cache_dir); without it the runner rejects the
	// dispatch with "workdir_unavailable" and the checkout task loops as
	// pending forever.
	checkoutGit := gitContextFromFeatureEntries(featureTasks)
	checkoutWorkdir := checkoutGit.TargetWorkdir

	// Build checkout task content, routed by checkout_mode.
	//
	// This routing is the whole point of the mode and it did not exist:
	// CheckoutFeature used to WRITE checkout_mode into frontmatter and never
	// read it, so "Simple" produced the same LLM prose task as "AI" — no
	// executor, no script, nothing deterministic. The squash-merge script
	// lived only inside the built-in automation, reachable only from a
	// feature.completed event, which this endpoint does not publish. Picking
	// Simple in the modal therefore could not do what the modal said it did.
	content := buildFeatureCheckoutContent(sanitizedFeatureID, normalizedOpts)
	var checkoutScript string
	if normalizedOpts.CheckoutMode == "simple" {
		// The branch name is ESCAPED, never filtered. An earlier pass stripped
		// it to [A-Za-z0-9._/-], which silently mangled names git accepts
		// perfectly well (release/v1.0+rc1, fix/issue#42, feature/über-fix).
		// The script's "source branch no longer exists" guard then reported
		// that mangled name as already-merged and exited 0, so the task went
		// GREEN having merged nothing — the worst possible failure for a
		// merge tool.
		sourceBranch := strings.TrimSpace(normalizedOpts.ExecutionBranch)
		if sourceBranch == "" {
			sourceBranch = sanitizedFeatureID
		}
		checkoutScript = renderSimpleFeatureCheckoutScript(simpleCheckoutScriptParams{
			FeatureExpr:      shellSingleQuoted(sanitizedFeatureID),
			ProjectExpr:      shellSingleQuoted(sanitizedProjectID),
			SourceBranchExpr: "'" + shellSingleQuoted(sourceBranch) + "'",
			TargetBranch:     normalizedOpts.MergeTargetBranch,
			RemoteDelete:     normalizedOpts.RemoteBranchPolicy == "delete",
		})
		content = checkoutScript
	}

	// Build frontmatter
	trueVal := true
	fm := &frontmatter.Frontmatter{
		Type:     "task",
		Title:    fmt.Sprintf("Feature checkout: %s", sanitizedFeatureID),
		Status:   "pending",
		Priority: "medium",
		Created:  time.Now().Format(time.RFC3339),
		Tags:     []string{"checkout", sanitizedFeatureID},
	}

	// Set fields that use frontmatter struct fields
	fm.FeatureID = sanitizedFeatureID
	fm.DependsOn = dependsOn
	fm.Generated = &trueVal
	fm.GeneratedKind = "feature_checkout"
	fm.GeneratedKey = generatedKey
	fm.GeneratedBy = "feature-checkout"

	// Set merge-related fields
	if normalizedOpts.ExecutionBranch != "" {
		fm.GitBranch = normalizedOpts.ExecutionBranch
	}
	if normalizedOpts.MergeTargetBranch != "" {
		fm.MergeTargetBranch = normalizedOpts.MergeTargetBranch
	}
	if normalizedOpts.MergePolicy != "" {
		fm.MergePolicy = normalizedOpts.MergePolicy
	}
	if normalizedOpts.MergeStrategy != "" {
		fm.MergeStrategy = normalizedOpts.MergeStrategy
	}
	if normalizedOpts.RemoteBranchPolicy != "" {
		fm.RemoteBranchPolicy = normalizedOpts.RemoteBranchPolicy
	}
	if normalizedOpts.CheckoutMode != "" {
		fm.CheckoutMode = normalizedOpts.CheckoutMode
	}
	if normalizedOpts.ExecutionMode != "" {
		fm.ExecutionMode = normalizedOpts.ExecutionMode
	}
	// Inherit the feature's git repo context in ALL modes. In worktree mode
	// (the AI-checkout default) the runner needs a valid repo to resolve a
	// workdir; leaving these empty is what caused AI checkouts to be rejected
	// with "workdir_unavailable" and loop as pending. target_workdir/workdir
	// let the runner find the local repo; git_remote lets it clone when no
	// local path is valid on the executing machine.
	if checkoutGit.TargetWorkdir != "" {
		fm.TargetWorkdir = checkoutGit.TargetWorkdir
	}
	if checkoutGit.Workdir != "" {
		fm.Workdir = checkoutGit.Workdir
	}
	if checkoutGit.GitRemote != "" {
		fm.GitRemote = checkoutGit.GitRemote
	}
	if checkoutScript != "" {
		// The script executor reads direct_prompt as the command to run, so
		// the body alone is not enough — a task with script content but no
		// executor resolves to opencode and hands an LLM a bash script as a
		// prompt.
		fm.Executor = "script"
		fm.DirectPrompt = checkoutScript
		// The script does its own `git checkout <target>`; a worktree would
		// put it on a detached feature branch with nothing to merge into.
		fm.ExecutionMode = "current_branch"
		if checkoutWorkdir != "" {
			fm.TargetWorkdir = checkoutWorkdir
		}
	}
	if normalizedOpts.OpenPRBeforeMerge {
		fm.OpenPRBeforeMerge = &normalizedOpts.OpenPRBeforeMerge
	}

	// Write file
	fmYAML := frontmatter.Serialize(fm)
	var fileBuilder strings.Builder
	fileBuilder.WriteString("---\n")
	fileBuilder.WriteString(fmYAML)
	fileBuilder.WriteString("---\n")
	if content != "" {
		fileBuilder.WriteString("\n")
		fileBuilder.WriteString(content)
		fileBuilder.WriteString("\n")
	}
	fileContent := fileBuilder.String()
	if err := os.WriteFile(taskPath, []byte(fileContent), 0644); err != nil {
		return nil, fmt.Errorf("failed to write checkout task: %w", err)
	}

	// Index the file. Every other writer in the codebase does this (see
	// BrainServiceImpl.Save); skipping it leaves the checkout task invisible
	// to search, the link graph, and orphan detection until a restart runs
	// the boot indexer. Returning the error is safe: the idempotency check
	// above scans the filesystem, not the index, so a retry finds the file
	// already on disk and reports Created=false instead of duplicating it.
	relPath := fmt.Sprintf("projects/%s/task/%s", sanitizedProjectID, filename)
	if err := s.indexer.IndexFile(relPath); err != nil {
		return nil, fmt.Errorf("index checkout task %q: %w", relPath, err)
	}

	// Build response
	result := &types.CheckoutFeatureResult{
		Created:          true,
		GeneratedKey:     generatedKey,
		Superseded:       supersededID != "",
		SupersededTaskID: supersededID,
		Task: &types.CreateEntryResponse{
			ID:     shortID,
			Path:   relPath,
			Title:  fmt.Sprintf("Feature checkout: %s", sanitizedFeatureID),
			Type:   "task",
			Status: "pending",
		},
	}

	return result, nil
}

// normalizeFeatureCheckoutOptions returns normalized options with defaults.
func normalizeFeatureCheckoutOptions(opts *types.FeatureCheckoutOptions) *types.FeatureCheckoutOptions {
	if opts == nil {
		opts = &types.FeatureCheckoutOptions{}
	}

	normalized := &types.FeatureCheckoutOptions{
		ExecutionBranch:    strings.TrimSpace(opts.ExecutionBranch),
		MergeTargetBranch:  strings.TrimSpace(opts.MergeTargetBranch),
		MergePolicy:        strings.TrimSpace(opts.MergePolicy),
		MergeStrategy:      strings.TrimSpace(opts.MergeStrategy),
		RemoteBranchPolicy: strings.TrimSpace(opts.RemoteBranchPolicy),
		CheckoutMode:       strings.TrimSpace(opts.CheckoutMode),
		ExecutionMode:      strings.TrimSpace(opts.ExecutionMode),
		OpenPRBeforeMerge:  opts.OpenPRBeforeMerge,
	}

	// Unlike the entries API, the checkout endpoint decodes options straight
	// off the request body with no enum validation, so an unrecognized mode
	// would otherwise be persisted into frontmatter and then silently behave
	// as "ai" at fold time. Drop it instead of writing a value we'd ignore.
	if !types.IsValidCheckoutMode(normalized.CheckoutMode) {
		normalized.CheckoutMode = ""
	}

	// Apply defaults
	if normalized.MergePolicy == "" {
		normalized.MergePolicy = "auto_merge"
	}
	if normalized.MergeStrategy == "" {
		normalized.MergeStrategy = "squash"
	}
	if normalized.RemoteBranchPolicy == "" {
		normalized.RemoteBranchPolicy = "delete"
	}
	if normalized.ExecutionMode == "" {
		normalized.ExecutionMode = "worktree"
	}

	return normalized
}

// buildFeatureCheckoutContent builds the content body for a checkout task.
func buildFeatureCheckoutContent(featureID string, opts *types.FeatureCheckoutOptions) string {
	executionBranch := opts.ExecutionBranch
	if executionBranch == "" {
		executionBranch = "(default branch for execution context)"
	}
	mergeTargetBranch := opts.MergeTargetBranch
	if mergeTargetBranch == "" {
		mergeTargetBranch = "(no explicit merge target)"
	}

	lines := []string{
		fmt.Sprintf("Automated feature checkout for %s.", featureID),
		"",
		"Merge intent:",
		fmt.Sprintf("- execution_branch: %s", executionBranch),
		fmt.Sprintf("- merge_target_branch: %s", mergeTargetBranch),
		fmt.Sprintf("- merge_policy: %s", opts.MergePolicy),
		fmt.Sprintf("- merge_strategy: %s", opts.MergeStrategy),
		fmt.Sprintf("- remote_branch_policy: %s", opts.RemoteBranchPolicy),
		fmt.Sprintf("- open_pr_before_merge: %v", opts.OpenPRBeforeMerge),
		"",
		"Safety gates before merge:",
		"- checkout validation pass",
		"- merge precheck pass",
		"- verification commands pass",
		"",
		"Guardrails:",
		"- If merge target is a protected branch, use open_pr_before_merge before auto merge",
		"- For optional PR-before-merge flow, ensure PR checks are green before final merge",
		"- cleanup only after confirmed successful push",
	}

	return strings.Join(lines, "\n")
}

// existingCheckoutTask is what findCheckoutTaskByKey reports about a
// checkout task already on disk: the response shape callers return, plus the
// two fields CheckoutFeature needs to decide whether that task still answers
// the request it just received.
type existingCheckoutTask struct {
	Resp *types.CreateEntryResponse
	// Mode is the stored checkout_mode ("" for entries written before modes).
	Mode string
	// Executor is the stored executor, used to tell a task that merely
	// RECORDS a mode from one that can actually carry it out.
	Executor string
	// FilePath is the absolute path on disk, for superseding.
	FilePath string
}

// foldCheckoutModeValue resolves a stored or requested checkout mode to the
// value the rest of the system acts on. Empty means "ai" everywhere else
// (foldCheckoutMode in event_service.go, and the routing in CheckoutFeature),
// so any comparison of two modes has to agree with that.
func foldCheckoutModeValue(mode string) string {
	if strings.TrimSpace(mode) == "" {
		return "ai"
	}
	return mode
}

// findCheckoutTaskByKey searches for a checkout task with the given generated_key.
func findCheckoutTaskByKey(taskDir, generatedKey string) (*existingCheckoutTask, error) {
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		filePath := filepath.Join(taskDir, entry.Name())
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		doc, err := frontmatter.Parse(string(content))
		if err != nil {
			continue
		}

		if doc.Frontmatter.GeneratedKey == generatedKey {
			// Found existing task
			shortID := strings.TrimSuffix(entry.Name(), ".md")
			title := doc.Frontmatter.Title
			status := doc.Frontmatter.Status

			return &existingCheckoutTask{
				Resp: &types.CreateEntryResponse{
					ID: shortID,
					// The "/task/" segment was missing here while the
					// creation path below emits it, so the already-exists
					// branch handed back a path that resolves to nothing —
					// a 404 for a task that exists.
					Path: fmt.Sprintf("projects/%s/task/%s",
						filepath.Base(filepath.Dir(taskDir)), entry.Name()),
					Title:  title,
					Type:   "task",
					Status: status,
				},
				Mode:     doc.Frontmatter.CheckoutMode,
				Executor: doc.Frontmatter.Executor,
				FilePath: filePath,
			}, nil
		}
	}

	return nil, fmt.Errorf("not found")
}

// getFeatureTasksFromFilesystem reads tasks from filesystem for a feature.
func (s *TaskServiceImpl) getFeatureTasksFromFilesystem(projectID, featureID string) ([]types.BrainEntry, error) {
	taskDir := filepath.Join(s.config.BrainDir, "projects", projectID, "task")
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []types.BrainEntry{}, nil
		}
		return nil, err
	}

	var tasks []types.BrainEntry
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		filePath := filepath.Join(taskDir, entry.Name())
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		doc, err := frontmatter.Parse(string(content))
		if err != nil {
			continue
		}

		taskFeatureID := doc.Frontmatter.FeatureID
		if taskFeatureID != featureID {
			continue
		}

		// Parse Generated field
		generated := doc.Frontmatter.Generated

		shortID := strings.TrimSuffix(entry.Name(), ".md")
		tasks = append(tasks, types.BrainEntry{
			ID:            shortID,
			Status:        doc.Frontmatter.Status,
			Generated:     generated,
			GeneratedKind: doc.Frontmatter.GeneratedKind,
			// The repo the work happened in. Carried so CheckoutFeature can
			// resolve a workdir for the checkout task it generates: the
			// manual endpoint inherits none from an automation entry, and a
			// git script with no workdir runs wherever the runner happens to
			// be. Dropping these here silently produced exactly that.
			TargetWorkdir: doc.Frontmatter.TargetWorkdir,
			Workdir:       doc.Frontmatter.Workdir,
			GitRemote:     doc.Frontmatter.GitRemote,
		})
	}

	return tasks, nil
}

// extractUniqueNonGeneratedTaskIds extracts unique task IDs from non-generated tasks.
func extractUniqueNonGeneratedTaskIds(tasks []types.BrainEntry) []string {
	seen := make(map[string]bool)
	var result []string

	for _, task := range tasks {
		// Skip generated tasks
		if task.Generated != nil && *task.Generated {
			continue
		}
		// Skip empty IDs
		if task.ID == "" {
			continue
		}
		// Skip duplicates
		if seen[task.ID] {
			continue
		}
		seen[task.ID] = true
		result = append(result, task.ID)
	}

	// Sort for deterministic output
	if len(result) > 0 {
		sortStrings(result)
	}
	return result
}

// isTerminalCheckoutStatus returns true if the status is terminal.
func isTerminalCheckoutStatus(status string) bool {
	return status == "completed" ||
		status == "validated" ||
		status == "cancelled" ||
		status == "superseded" ||
		status == "archived"
}

// extractGeneratedDependentTasks extracts checkout and review tasks.
func extractGeneratedDependentTasks(tasks []types.BrainEntry) []types.BrainEntry {
	var result []types.BrainEntry
	for _, task := range tasks {
		if task.Generated == nil || !*task.Generated {
			continue
		}
		if task.GeneratedKind == "feature_checkout" || task.GeneratedKind == "feature_review" {
			result = append(result, task)
		}
	}
	return result
}

// sortStrings sorts a string slice in place.
func sortStrings(s []string) {
	// Simple insertion sort for small slices
	for i := 1; i < len(s); i++ {
		key := s[i]
		j := i - 1
		for j >= 0 && s[j] > key {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = key
	}
}

// TriggerTask manually triggers a task. Behavior depends on whether the task
// has a cron schedule:
//
//   - Scheduled tasks: validates the schedule, creates a run record, computes
//     next_run, and resets the task to pending.
//   - Non-scheduled (ad-hoc) tasks: resets the task to pending so the runner
//     picks it up on its next poll. No-op (with reason) if the task is already
//     pending or in_progress.
func (s *TaskServiceImpl) TriggerTask(ctx context.Context, projectId, taskId string) (*types.TriggerResponse, error) {
	// 1. Find the task
	tasks, err := s.getAllTasks(ctx, projectId)
	if err != nil {
		return nil, fmt.Errorf("failed to get tasks: %w", err)
	}

	var task *types.BrainEntry
	for i := range tasks {
		if tasks[i].ID == taskId {
			task = &tasks[i]
			break
		}
	}
	if task == nil {
		return nil, fmt.Errorf("task not found: %s/%s", projectId, taskId)
	}

	// 2. Non-scheduled (ad-hoc) tasks: reset to pending for the runner.
	if task.Schedule == "" {
		return s.triggerAdHocTask(ctx, task)
	}

	// 3. Validate schedule_enabled
	if task.ScheduleEnabled != nil && !*task.ScheduleEnabled {
		return &types.TriggerResponse{
			Success: true, TaskID: taskId, Triggered: false,
			Reason: "schedule is disabled",
		}, nil
	}

	// 4. Validate eligible status (active, completed, blocked)
	eligibleStatuses := map[string]bool{"active": true, "completed": true, "blocked": true}
	if !eligibleStatuses[task.Status] {
		return &types.TriggerResponse{
			Success: true, TaskID: taskId, Triggered: false,
			Reason: fmt.Sprintf("task status %q is not eligible for triggering", task.Status),
		}, nil
	}

	// 5. Check max_runs
	if task.MaxRuns != nil && *task.MaxRuns > 0 {
		runCount := countCompletedRuns(task.Runs)
		if runCount >= *task.MaxRuns {
			return &types.TriggerResponse{
				Success: true, TaskID: taskId, Triggered: false,
				Reason: fmt.Sprintf("max_runs reached (%d/%d)", runCount, *task.MaxRuns),
			}, nil
		}
	}

	// 6. Parse cron expression and compute next_run
	sched, err := cron.Parse(task.Schedule)
	if err != nil {
		return nil, fmt.Errorf("invalid schedule expression %q: %w", task.Schedule, err)
	}
	now := time.Now().UTC()
	nextRun := sched.NextAfter(now)

	// 7. Generate run ID and create run record
	runID := generateTriggerRunID(now)

	// Build updated runs array
	runs := make([]interface{}, 0, len(task.Runs)+1)
	for _, r := range task.Runs {
		runMap := map[string]interface{}{
			"run_id":  r.RunID,
			"status":  r.Status,
			"started": r.Started,
		}
		if r.Completed != "" {
			runMap["completed"] = r.Completed
		}
		if r.SkipReason != "" {
			runMap["skip_reason"] = r.SkipReason
		}
		runs = append(runs, runMap)
	}
	runs = append(runs, map[string]interface{}{
		"run_id":  runID,
		"status":  "in_progress",
		"started": now.Format(time.RFC3339),
		"tasks":   1,
	})

	// 8. Update metadata: runs + next_run + status
	if _, err := s.storage.MergeMetadata(ctx, task.Path, map[string]interface{}{
		"runs":     runs,
		"next_run": nextRun.Format(time.RFC3339),
		"status":   "pending",
	}); err != nil {
		return nil, fmt.Errorf("failed to update task metadata: %w", err)
	}

	return &types.TriggerResponse{
		Success:   true,
		TaskID:    taskId,
		Triggered: true,
		RunID:     runID,
		NextRun:   nextRun.Format(time.RFC3339),
	}, nil
}

// triggerAdHocTask handles manual trigger for tasks without a cron schedule.
// It resets eligible tasks to "pending" so the runner picks them up on the next
// poll. Tasks already pending or actively running are reported as a no-op with
// a descriptive reason.
func (s *TaskServiceImpl) triggerAdHocTask(ctx context.Context, task *types.BrainEntry) (*types.TriggerResponse, error) {
	switch task.Status {
	case "pending":
		return &types.TriggerResponse{
			Success: true, TaskID: task.ID, Triggered: false,
			Reason: "task is already pending",
		}, nil
	case "in_progress", "active":
		return &types.TriggerResponse{
			Success: true, TaskID: task.ID, Triggered: false,
			Reason: fmt.Sprintf("task is already %s", task.Status),
		}, nil
	}

	// Reset to pending so the runner picks it up. This covers blocked,
	// completed, cancelled, failed, draft, validated, and any other status.
	if _, err := s.storage.MergeMetadata(ctx, task.Path, map[string]interface{}{
		"status": "pending",
	}); err != nil {
		return nil, fmt.Errorf("failed to reset task to pending: %w", err)
	}

	return &types.TriggerResponse{
		Success:   true,
		TaskID:    task.ID,
		Triggered: true,
		Reason:    fmt.Sprintf("reset task from %q to pending", task.Status),
	}, nil
}

// ResumeTask flips an abandoned task back to pending so the runner can re-claim
// and spawn it with IsResume=true. Idempotent: calling twice on a task already
// pending with resume_requested=true returns Resumed=false with an explanatory
// Reason instead of an error.
//
// Cleanup side effects (defense in depth against the various stuck states):
//   - Deletes any lingering task_claims row (may already be gone via
//     StartClaimCleanup or ReleaseAllByRunner).
//   - Clears any dispatch lease still in acked state (the SIGKILL-leaves-acked
//     path noted in the design research).
//   - Writes metadata.resume_requested=true + resume_requested_at=now. The
//     runner reads resume_requested at claim time and passes IsResume=true to
//     the executor's prompt builder, then clears the flag.
//
// The status flip goes through storage.MergeMetadata — matching triggerAdHocTask.
// Frontmatter sync to the .md file is a follow-up concern (pre-existing pattern
// in this file uses MergeMetadata directly for status changes).
func (s *TaskServiceImpl) ResumeTask(ctx context.Context, projectID, taskID string, opts *types.ResumeTaskOptions) (*types.ResumeTaskResult, error) {
	if opts == nil {
		opts = &types.ResumeTaskOptions{}
	}

	// Load the task via the enriched GetTask path so we get IsAbandoned +
	// AbandonReason computed the same way every other caller sees them.
	task, err := s.GetTask(ctx, projectID, taskID)
	if err != nil {
		return nil, fmt.Errorf("resume: %w", err)
	}
	if task == nil {
		return nil, fmt.Errorf("resume: task not found: %s/%s", projectID, taskID)
	}

	result := &types.ResumeTaskResult{
		TaskID:             taskID,
		PriorStatus:        task.Status,
		PriorSessionsCount: len(task.Sessions),
		AbandonReason:      task.AbandonReason,
	}

	// Idempotency + un-stuck: distinguish "already resumed and waiting for the
	// runner to claim" from "was already pending WITHOUT the flag" (which the
	// user may reasonably want to force through — e.g. an auto-reset via
	// claim-renewal-fail left the task pending with no resume hint). If the
	// flag is already set: idempotent no-op. If not: fall through to the full
	// resume path so the flag gets stamped and the runner routes via IsResume.
	if task.Status == "pending" && task.ResumeRequested {
		result.Reason = "resume already requested; runner will pick up on next poll"
		return result, nil
	}

	// Terminal statuses are outside the resume gate — user should use Trigger.
	switch task.Status {
	case "completed", "validated", "cancelled", "superseded", "archived":
		if !opts.Force {
			result.Reason = fmt.Sprintf("task status %q is terminal; use trigger to re-run", task.Status)
			return result, nil
		}
	}

	// Abandonment gate. Force bypasses (but does NOT override the live-claim
	// safety check below). For pending tasks specifically, force is
	// REQUIRED — regular pending tasks aren't in scope for Resume; a batch
	// endpoint calling ResumeTask on every task should NOT silently stamp
	// the resume flag on incidental pending tasks. Force+pending = "un-stick"
	// (TestResumeTask_StuckPendingUnstuck).
	if !task.IsAbandoned && !opts.Force {
		result.Reason = fmt.Sprintf("task is not abandoned (status=%q); use trigger or force=true", task.Status)
		return result, nil
	}

	// Cleanup: delete claim + acked dispatch lease. Both are best-effort;
	// downstream sweepers will eventually clean these up too. We do them here
	// so the runner sees a clean slate on re-claim.
	//
	// SAFETY: before releasing the claim, re-check the claim's runner status.
	// If a live runner has claimed this task since enrichAbandonmentState ran
	// (the atomic claim upsert would have evicted an expired claim, then the
	// live runner grabbed a fresh one), releasing that claim here would enable
	// a second runner to claim the same task → concurrent double-execution.
	// Refuse in that case, even with force=true. The user must abort the live
	// runner out-of-band before resuming — force only bypasses the IsAbandoned
	// gate, never the live-claim safety.
	if claim, err := s.storage.GetClaim(ctx, projectID, taskID); err == nil && claim != nil {
		runner, rerr := s.storage.GetRunner(ctx, claim.RunnerID)
		if rerr == nil && runner != nil && runner.Status == "online" {
			result.Reason = fmt.Sprintf(
				"task is claimed by online runner %q since enrichment view; abort that runner or wait for its lease to lapse before resuming",
				claim.RunnerID,
			)
			return result, nil
		}
		if _, err := s.storage.ReleaseClaim(ctx, projectID, taskID, claim.RunnerID); err != nil {
			slog.Debug("resume: release claim failed (continuing)",
				"project", projectID, "task_id", taskID, "error", err)
		}
	}
	if _, err := s.storage.ClearDispatchLease(ctx, projectID, taskID); err != nil {
		slog.Debug("resume: clear dispatch lease failed (continuing)",
			"project", projectID, "task_id", taskID, "error", err)
	}

	// Apply the state change. Status goes to pending so the runner picks it up
	// on the next poll; resume_requested is the flag the runner reads at claim
	// time to route through the IsResume=true prompt template.
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.storage.MergeMetadata(ctx, task.Path, map[string]interface{}{
		"status":              "pending",
		"resume_requested":    true,
		"resume_requested_at": now,
	}); err != nil {
		return nil, fmt.Errorf("resume: update metadata: %w", err)
	}

	result.Resumed = true
	slog.Info("resume: task resumed",
		"project", projectID, "task_id", taskID,
		"prior_status", result.PriorStatus,
		"abandon_reason", result.AbandonReason,
		"prior_sessions_count", result.PriorSessionsCount,
		"force", opts.Force,
	)
	return result, nil
}

// terminalStatuses is the set of statuses that ResumeFeature refuses to
// touch during a batch WITHOUT force. A plain batch resume ("resume
// everything abandoned in this feature") must never resurrect work that
// already reached a terminal state. With force=true the batch delegates to
// ResumeTask, which honors force for terminal statuses exactly like the
// single-task endpoint — parity, so a batch force isn't weaker than N
// single-task forces. The live-claim safety inside ResumeTask remains
// absolute either way.
var terminalStatuses = map[string]bool{
	"completed":  true,
	"validated":  true,
	"cancelled":  true,
	"superseded": true,
	"archived":   true,
}

// resumeFeatureInFlight guards ResumeFeature against a concurrent second
// call on the same feature racing through the same task set. Two Resume
// clicks during PWA lag or a webhook + user click both firing would
// otherwise emit duplicate task.resume_requested + task.status_changed
// events. The lock is per-feature (project|feature key) so different
// features can resume in parallel; only same-feature calls serialize.
//
// Uses a refcounted entry so the sync.Map doesn't grow unboundedly when
// an attacker hammers /features/<random>/resume with unique IDs:
// LoadOrStore inserts an entry; Lock/Unlock happens; the last waiter
// (refs=0) removes the entry so lookup can GC.
var resumeFeatureInFlight sync.Map // key "proj|feature" → *resumeInflightEntry

type resumeInflightEntry struct {
	mu   sync.Mutex
	refs atomic.Int32
}

// acquireResumeFeatureLock takes (or creates) the per-key mutex and returns
// a release function that removes the entry from the map when the last
// waiter is done. Callers MUST defer the returned release; otherwise the
// mutex leaks and future calls will observe permanent contention on that
// key.
func acquireResumeFeatureLock(ctx context.Context, key string) (func(), error) {
	loaded, _ := resumeFeatureInFlight.LoadOrStore(key, &resumeInflightEntry{})
	entry := loaded.(*resumeInflightEntry)
	entry.refs.Add(1)
	// Acquire under a channel-select so ctx cancellation doesn't require
	// us to spin — Lock itself doesn't accept a context so we serialize
	// waiters via the mutex directly.
	entry.mu.Lock()
	if err := ctx.Err(); err != nil {
		entry.mu.Unlock()
		if entry.refs.Add(-1) == 0 {
			resumeFeatureInFlight.CompareAndDelete(key, entry)
		}
		return nil, err
	}
	return func() {
		entry.mu.Unlock()
		if entry.refs.Add(-1) == 0 {
			// Last waiter — delete from the map. CompareAndDelete guards
			// against a race where a new caller LoadOrStores the same key
			// between our Add and Delete; in that case they get the same
			// (still-empty) entry which is safe to reuse. If a NEW entry
			// was stored (different pointer), we correctly leave it alone.
			resumeFeatureInFlight.CompareAndDelete(key, entry)
		}
	}, nil
}

// NOTE: response-size truncation is a HANDLER responsibility, not
// service — the handler needs the full Results slice to emit per-task
// SSE events before truncating for the response body. See
// HandleResumeFeature and api.resumeFeatureMaxResults.

// ResumeFeature fans out ResumeTask across every task in the feature, gathering
// per-task outcomes into a single response. Non-abandoned tasks appear as
// skipped results with an explanatory Reason — the batch does NOT fail if some
// tasks are unresumable, so callers can invoke this optimistically ("resume
// everything you can in this feature") without pre-filtering.
//
// Force semantics at batch level: bypasses the abandonment gate for tasks
// whose runtime state doesn't automatically qualify, AND the terminal-status
// batch exclusion (per-task ResumeTask already honors terminal+force, so the
// batch matches the single-task endpoint). Live-claim refusal inside
// ResumeTask stays absolute regardless of force. See terminalStatuses above.
//
// Errors from individual ResumeTask calls (unlike no-op skips) are converted
// into skipped results with the error text in Reason. The overall call only
// returns an error for setup failures (feature enumeration, storage failure).
func (s *TaskServiceImpl) ResumeFeature(ctx context.Context, projectID, featureID string, opts *types.ResumeTaskOptions) (*types.ResumeFeatureResult, error) {
	if opts == nil {
		opts = &types.ResumeTaskOptions{}
	}
	sanitizedProjectID := strings.TrimSpace(projectID)
	sanitizedFeatureID := strings.TrimSpace(featureID)
	if sanitizedProjectID == "" {
		return nil, fmt.Errorf("resume feature: projectID is required")
	}
	if sanitizedFeatureID == "" {
		return nil, fmt.Errorf("resume feature: featureID is required")
	}

	// Enumerate feature tasks FIRST — before we take any lock. Unknown
	// features (or features with zero tasks) must not leave a mutex
	// entry in the sync.Map, otherwise attackers can OOM the process by
	// hammering /resume with random featureIds.
	featureTasks, err := s.getFeatureTasksFromFilesystem(sanitizedProjectID, sanitizedFeatureID)
	if err != nil {
		return nil, fmt.Errorf("resume feature: enumerate tasks: %w", err)
	}
	if len(featureTasks) == 0 {
		return nil, fmt.Errorf("resume feature: not found — feature %q has no tasks in project %q", sanitizedFeatureID, sanitizedProjectID)
	}

	// Per-feature in-flight lock via refcounted helper: two concurrent
	// POSTs to /resume on the same feature serialize; the last waiter
	// deletes the map entry so lookups don't grow unboundedly.
	release, err := acquireResumeFeatureLock(ctx, sanitizedProjectID+"|"+sanitizedFeatureID)
	if err != nil {
		return nil, err
	}
	defer release()

	result := &types.ResumeFeatureResult{
		FeatureID: sanitizedFeatureID,
		Results:   make([]types.ResumeTaskResult, 0, len(featureTasks)),
	}

	for _, task := range featureTasks {
		if task.ID == "" {
			continue
		}
		// Terminal-status guard — the safety net against a plain batch
		// resume silently resurrecting historically-completed work. Runs
		// BEFORE ResumeTask so the skip reason is explicit about the batch
		// exclusion. Force bypasses it: the task then flows through
		// ResumeTask, whose own force path handles terminal statuses the
		// same way the single-task endpoint does.
		if !opts.Force && terminalStatuses[task.Status] {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return result, ctxErr
			}
			result.Results = append(result.Results, types.ResumeTaskResult{
				TaskID:      task.ID,
				Resumed:     false,
				PriorStatus: task.Status,
				Reason:      fmt.Sprintf("terminal_status_excluded_from_batch (%s)", task.Status),
			})
			result.TotalSkipped++
			continue
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			// Bail early on client disconnect / cancellation. Preserves
			// results gathered so far and surfaces the cancellation to
			// the caller.
			return result, ctxErr
		}
		taskResult, err := s.ResumeTask(ctx, sanitizedProjectID, task.ID, opts)
		if err != nil {
			// Per-task failure → skipped result with error in Reason so the
			// caller sees partial failures without the whole batch failing.
			slog.Warn("resume feature: per-task ResumeTask failed",
				"project", sanitizedProjectID,
				"feature", sanitizedFeatureID,
				"task_id", task.ID,
				"error", err,
			)
			result.Results = append(result.Results, types.ResumeTaskResult{
				TaskID:  task.ID,
				Resumed: false,
				Reason:  "internal_error",
			})
			result.TotalSkipped++
			continue
		}
		result.Results = append(result.Results, *taskResult)
		if taskResult.Resumed {
			result.TotalResumed++
		} else {
			result.TotalSkipped++
		}
	}

	// NOTE: response-size truncation is a HANDLER responsibility, not
	// service — the handler needs the full Results slice to emit per-task
	// SSE events before truncating for the response body. See
	// HandleResumeFeature.

	slog.Info("resume feature: batch complete",
		"project", sanitizedProjectID,
		"feature", sanitizedFeatureID,
		"total_resumed", result.TotalResumed,
		"total_skipped", result.TotalSkipped,
		"force", opts.Force,
	)
	return result, nil
}

// countCompletedRuns counts runs with terminal-ish statuses.
// Mirrors countRuns in internal/runner/schedule.go.
func countCompletedRuns(runs []types.CronRun) int {
	count := 0
	for _, r := range runs {
		switch r.Status {
		case "completed", "failed", "skipped", "in_progress":
			count++
		}
	}
	return count
}

// generateTriggerRunID creates a unique run identifier.
// Format: YYYYMMDD-HHmm-XXXXXX (6 hex chars).
func generateTriggerRunID(t time.Time) string {
	b := make([]byte, 3)
	// crypto/rand.Read never returns an error as of Go 1.24 — it
	// panics if the system entropy source fails.
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s-%s", t.Format("20060102-1504"), hex.EncodeToString(b))
}

// =============================================================================
// Helper functions
// =============================================================================

// NoteRowToBrainEntry converts a storage NoteRow to a types.BrainEntry,
// parsing the JSON metadata field into proper BrainEntry fields.
func NoteRowToBrainEntry(row *storage.NoteRow) types.BrainEntry {
	entry := types.BrainEntry{
		Path:  row.Path,
		Title: row.Title,
		ID:    row.ShortID,
	}

	// Nullable string fields
	if row.Type != nil {
		entry.Type = *row.Type
	}
	if row.Status != nil {
		entry.Status = *row.Status
	}
	if row.Priority != nil {
		entry.Priority = *row.Priority
	}
	if row.ProjectID != nil {
		entry.ProjectID = *row.ProjectID
	}
	if row.FeatureID != nil {
		entry.FeatureID = *row.FeatureID
	}
	if row.Created != nil {
		entry.Created = *row.Created
	}
	if row.Modified != nil {
		entry.Modified = *row.Modified
	}
	if row.Body != nil {
		entry.Content = *row.Body
	}

	// Parse JSON metadata for additional fields
	if row.Metadata != "" && row.Metadata != "{}" {
		var meta map[string]interface{}
		if err := json.Unmarshal([]byte(row.Metadata), &meta); err == nil {
			parseMetadataIntoEntry(&entry, meta)
		}
	}

	return entry
}

// parseMetadataIntoEntry extracts known fields from the metadata JSON map
// into the BrainEntry struct fields.
func parseMetadataIntoEntry(entry *types.BrainEntry, meta map[string]interface{}) {
	// parent_id: hierarchical parent reference
	if v, ok := metaString(meta, "parent_id"); ok {
		entry.ParentID = v
	}

	// depends_on: can be string or []string in JSON
	if v, ok := metaStringSlice(meta, "depends_on"); ok {
		entry.DependsOn = v
	} else if v, ok := metaString(meta, "depends_on"); ok {
		entry.DependsOn = []string{v}
	}

	if v, ok := metaStringSlice(meta, "tags"); ok {
		entry.Tags = v
	}
	if v, ok := metaString(meta, "completed_at"); ok {
		entry.CompletedAt = v
	}
	if v, ok := meta["attachments"]; ok {
		data, err := json.Marshal(v)
		if err == nil {
			var attachments []types.AttachmentReference
			if err := json.Unmarshal(data, &attachments); err == nil {
				entry.Attachments = attachments
			}
		}
	}

	// Schedule fields
	if v, ok := metaString(meta, "schedule"); ok {
		entry.Schedule = v
	}
	if v, ok := metaBool(meta, "schedule_enabled"); ok {
		entry.ScheduleEnabled = &v
	}
	if v, ok := metaString(meta, "next_run"); ok {
		entry.NextRun = v
	}
	if v, ok := metaInt(meta, "max_runs"); ok {
		entry.MaxRuns = &v
	}
	if v, ok := metaString(meta, "starts_at"); ok {
		entry.StartsAt = v
	}
	if v, ok := metaString(meta, "expires_at"); ok {
		entry.ExpiresAt = v
	}
	if v, ok := metaString(meta, "run_once_at"); ok {
		entry.RunOnceAt = v
	}
	if v, ok := metaString(meta, "timezone"); ok {
		entry.Timezone = v
	}

	// Git/execution fields
	if v, ok := metaString(meta, "workdir"); ok {
		entry.Workdir = v
	}
	if v, ok := metaString(meta, "git_remote"); ok {
		entry.GitRemote = v
	}
	if v, ok := metaString(meta, "git_branch"); ok {
		entry.GitBranch = v
	}
	if v, ok := metaString(meta, "merge_target_branch"); ok {
		entry.MergeTargetBranch = v
	}
	if v, ok := metaString(meta, "merge_policy"); ok {
		entry.MergePolicy = v
	}
	if v, ok := metaString(meta, "merge_strategy"); ok {
		entry.MergeStrategy = v
	}
	if v, ok := metaString(meta, "remote_branch_policy"); ok {
		entry.RemoteBranchPolicy = v
	}
	if v, ok := metaBool(meta, "open_pr_before_merge"); ok {
		entry.OpenPRBeforeMerge = &v
	}
	// checkout_mode selects which feature-checkout automation handles this
	// task's feature when it completes ("ai" prompt vs "simple" deterministic
	// squash-merge). It is written to frontmatter and indexed, but was never
	// read back here — so CheckFeatureCompletion's fold always saw "" and
	// defaulted every feature to the AI path. The simple path was
	// unreachable no matter what a user configured.
	if v, ok := metaString(meta, "checkout_mode"); ok {
		entry.CheckoutMode = v
	}
	if v, ok := metaString(meta, "execution_mode"); ok {
		entry.ExecutionMode = v
	}
	if v, ok := metaString(meta, "session_mode"); ok {
		entry.SessionMode = v
	}

	// Task execution fields
	if v, ok := metaString(meta, "user_original_request"); ok {
		entry.UserOriginalRequest = v
	}
	if v, ok := metaString(meta, "direct_prompt"); ok {
		entry.DirectPrompt = v
	}
	if v, ok := metaString(meta, "agent"); ok {
		entry.Agent = v
	}
	if v, ok := metaString(meta, "model"); ok {
		entry.Model = v
	}
	if v, ok := metaString(meta, "executor"); ok {
		entry.Executor = v
	}
	if v, ok := meta["extensions"]; ok {
		if arr, ok := v.([]interface{}); ok {
			for _, item := range arr {
				if s, ok := item.(string); ok {
					entry.Extensions = append(entry.Extensions, s)
				}
			}
		}
	}
	if v, ok := metaBool(meta, "complete_on_idle"); ok {
		entry.CompleteOnIdle = &v
	}
	if v, ok := metaString(meta, "target_workdir"); ok {
		entry.TargetWorkdir = v
	}

	// Origin provenance. Frontmatter-persisted, so this runs on every
	// re-index; without it the fields survive on disk but vanish from the
	// API response, and the scheduler/runner never see them.
	if v, ok := metaString(meta, "origin_machine_id"); ok {
		entry.OriginMachineID = v
	}
	if v, ok := metaString(meta, "origin_client_id"); ok {
		entry.OriginClientID = v
	}
	if v, ok := metaString(meta, "origin_path"); ok {
		entry.OriginPath = v
	}
	if v, ok := metaString(meta, "machine_affinity"); ok {
		entry.MachineAffinity = v
	}

	// Resume-abandoned-tasks flow. Both are runtime-only (not in frontmatter).
	if v, ok := metaBool(meta, "resume_requested"); ok {
		entry.ResumeRequested = v
	}
	if v, ok := metaString(meta, "resume_requested_at"); ok {
		entry.ResumeRequestedAt = v
	}

	// Failure retry accounting, written by the runner on each failed run.
	if v, ok := metaInt(meta, "attempt_count"); ok {
		entry.AttemptCount = v
	}
	if v, ok := metaString(meta, "last_failed_at"); ok {
		entry.LastFailedAt = v
	}
	if v, ok := metaString(meta, "executor"); ok {
		entry.Executor = v
	}
	if v, ok := metaStringSlice(meta, "extensions"); ok {
		entry.Extensions = v
	}
	if v, ok := metaStringSlice(meta, "requires_capability"); ok {
		entry.RequiresCapability = v
	} else if v, ok := metaString(meta, "requires_capability"); ok {
		entry.RequiresCapability = []string{v}
	}

	// Feature grouping (feature_id from metadata as fallback)
	if v, ok := metaString(meta, "feature_id"); ok {
		if entry.FeatureID == "" {
			entry.FeatureID = v
		}
	}
	if v, ok := metaString(meta, "feature_priority"); ok {
		entry.FeaturePriority = v
	}
	if v, ok := metaStringSlice(meta, "feature_depends_on"); ok {
		entry.FeatureDependsOn = v
	}

	// Feature-level schedule fields
	if v, ok := metaString(meta, "feature_schedule"); ok {
		entry.FeatureSchedule = v
	}
	if v, ok := metaString(meta, "feature_starts_at"); ok {
		entry.FeatureStartsAt = v
	}
	if v, ok := metaString(meta, "feature_expires_at"); ok {
		entry.FeatureExpiresAt = v
	}
	if v, ok := metaString(meta, "feature_run_once_at"); ok {
		entry.FeatureRunOnceAt = v
	}
	if v, ok := metaString(meta, "feature_timezone"); ok {
		entry.FeatureTimezone = v
	}

	// Generated entry metadata
	if v, ok := metaBool(meta, "generated"); ok {
		entry.Generated = &v
	}
	if v, ok := metaString(meta, "generated_kind"); ok {
		entry.GeneratedKind = v
	}
	if v, ok := metaString(meta, "generated_key"); ok {
		entry.GeneratedKey = v
	}
	if v, ok := metaString(meta, "generated_by"); ok {
		entry.GeneratedBy = v
	}
	if v, ok := metaString(meta, "automation_run_id"); ok {
		entry.AutomationRunID = v
	}

	// Automation fields (nested maps from metadata JSON)
	if v, ok := meta["trigger"]; ok {
		data, err := json.Marshal(v)
		if err == nil {
			var tc types.TriggerConfig
			if err := json.Unmarshal(data, &tc); err == nil {
				entry.Trigger = &tc
			}
		}
	}
	if v, ok := meta["action"]; ok {
		entry.Action = metaToAutomationAction(v)
	}
	if v, ok := meta["retry"]; ok {
		entry.Retry = metaToAutomationRetry(v)
	}
	if v, ok := meta["goal"]; ok {
		data, err := json.Marshal(v)
		if err == nil {
			var gc types.GoalConfig
			if err := json.Unmarshal(data, &gc); err == nil {
				entry.Goal = &gc
			}
		}
	}
	// The reminder block, read back out of notes.metadata.
	//
	// This is the hop that made checkout_mode write-only. The sweeper reads
	// entries through brain.List -> NoteRowToBrainEntry -> here, so without
	// this arm every reminder looks UNDATED and nothing ever fires — while
	// the markdown on disk is perfectly correct and every other layer agrees.
	if v, ok := meta["reminder"]; ok {
		data, err := json.Marshal(v)
		if err == nil {
			var rc types.ReminderConfig
			if err := json.Unmarshal(data, &rc); err == nil {
				entry.Reminder = &rc
			}
		}
	}

	// Sessions: map[string]SessionInfo from metadata JSON
	if sessionsRaw, ok := meta["sessions"]; ok {
		if sessionsMap, ok := sessionsRaw.(map[string]interface{}); ok {
			sessions := make(map[string]types.SessionInfo, len(sessionsMap))
			for sid, infoRaw := range sessionsMap {
				si := types.SessionInfo{}
				if infoMap, ok := infoRaw.(map[string]interface{}); ok {
					str := func(k string) string {
						if v, ok := infoMap[k].(string); ok {
							return v
						}
						return ""
					}
					si.Timestamp = str("timestamp")
					si.CronID = str("cron_id")
					si.RunID = str("run_id")
					si.RunnerID = str("runner_id")
					si.MachineID = str("machine_id")
					si.Hostname = str("hostname")
					si.Workdir = str("workdir")
				}
				sessions[sid] = si
			}
			entry.Sessions = sessions
		}
	}

	// Run finalizations: map[runID]RunFinalization from metadata JSON.
	if finalizationsRaw, ok := meta["run_finalizations"]; ok {
		if finalizationsMap, ok := finalizationsRaw.(map[string]interface{}); ok {
			finalizations := make(map[string]types.RunFinalization, len(finalizationsMap))
			for runID, infoRaw := range finalizationsMap {
				finalization := types.RunFinalization{}
				if infoMap, ok := infoRaw.(map[string]interface{}); ok {
					if status, ok := infoMap["status"].(string); ok {
						finalization.Status = status
					}
					if finalizedAt, ok := infoMap["finalized_at"].(string); ok {
						finalization.FinalizedAt = finalizedAt
					}
					if sessionID, ok := infoMap["session_id"].(string); ok {
						finalization.SessionID = sessionID
					}
				}
				finalizations[runID] = finalization
			}
			entry.RunFinalizations = finalizations
		}
	}

	// Schedule fields
	if v, ok := metaString(meta, "schedule"); ok {
		entry.Schedule = v
	}
	if v, ok := metaBool(meta, "schedule_enabled"); ok {
		entry.ScheduleEnabled = &v
	}
	if v, ok := metaBool(meta, "complete_on_idle"); ok {
		entry.CompleteOnIdle = &v
	}
	if v, ok := metaString(meta, "direct_prompt"); ok {
		entry.DirectPrompt = v
	}
	if v, ok := metaInt(meta, "max_runs"); ok {
		entry.MaxRuns = &v
	}
	if v, ok := metaString(meta, "next_run"); ok {
		entry.NextRun = v
	}
	if v, ok := metaString(meta, "run_once_at"); ok {
		entry.RunOnceAt = v
	}
	if v, ok := metaString(meta, "timezone"); ok {
		entry.Timezone = v
	}

	// Runs: []CronRun from metadata JSON
	if runsRaw, ok := meta["runs"]; ok {
		if runsSlice, ok := runsRaw.([]interface{}); ok {
			for _, runRaw := range runsSlice {
				if runMap, ok := runRaw.(map[string]interface{}); ok {
					run := types.CronRun{}
					if v, ok := runMap["run_id"].(string); ok {
						run.RunID = v
					}
					if v, ok := runMap["status"].(string); ok {
						run.Status = v
					}
					if v, ok := runMap["started"].(string); ok {
						run.Started = v
					}
					if v, ok := runMap["completed"].(string); ok {
						run.Completed = v
					}
					if v, ok := runMap["skip_reason"].(string); ok {
						run.SkipReason = v
					}
					entry.Runs = append(entry.Runs, run)
				}
			}
		}
	}
}

// metaString extracts a string value from a metadata map.
func metaString(meta map[string]interface{}, key string) (string, bool) {
	v, ok := meta[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// metaBool extracts a bool value from a metadata map.
func metaBool(meta map[string]interface{}, key string) (bool, bool) {
	v, ok := meta[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

// metaInt extracts an int value from a metadata map (JSON numbers are float64).
func metaInt(meta map[string]interface{}, key string) (int, bool) {
	v, ok := meta[key]
	if !ok {
		return 0, false
	}
	f, ok := v.(float64)
	if !ok {
		return 0, false
	}
	return int(f), true
}

// metaStringSlice extracts a []string from a metadata map.
func metaStringSlice(meta map[string]interface{}, key string) ([]string, bool) {
	v, ok := meta[key]
	if !ok {
		return nil, false
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil, false
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result, true
}

// computedFeatureToFeature converts a ComputedFeature to a types.Feature.
//
// The two stat types speak different taxonomies and must not be mapped by
// field name. FeatureTaskStats counts STATUS (pending, in_progress,
// completed, blocked); types.TaskStats counts dependency CLASSIFICATION
// (ready, waiting, blocked, status_blocked, not_pending).
//
// They were bridged with Ready: f.TaskStats.Pending, which is wrong — a
// pending task whose dependencies are unmet is waiting, not ready. Waiting,
// StatusBlocked and NotPending were never set at all, so they read as zero.
// Live, a feature holding 4 ready and 2 waiting tasks reported
// "Ready: 6 | Waiting: 0", and a feature of 15 drafts reported
// "Not pending: 0" while listing fifteen not_pending tasks.
//
// Counting classification directly is also what makes features/feature_get
// agree with the tasks tool, which classifies the same way.
func computedFeatureToFeature(f *ComputedFeature) types.Feature {
	stats := &types.TaskStats{}
	for _, task := range f.Tasks {
		stats.Total++
		switch task.Classification {
		case "ready":
			stats.Ready++
		case "waiting":
			stats.Waiting++
		case "blocked":
			stats.Blocked++
		default:
			stats.NotPending++
		}
		if task.Status == "blocked" {
			stats.StatusBlocked++
		}
	}

	return types.Feature{
		FeatureID:             f.ID,
		Tasks:                 f.Tasks,
		Ready:                 f.Classification == "ready",
		Stats:                 stats,
		UnresolvedFeatureDeps: f.UnresolvedFeatureDeps,
	}
}

// featuresToResponse converts a slice of ComputedFeatures to a FeatureListResponse.
func featuresToResponse(features []*ComputedFeature) *types.FeatureListResponse {
	result := make([]types.Feature, 0, len(features))
	for _, f := range features {
		result = append(result, computedFeatureToFeature(f))
	}
	return &types.FeatureListResponse{
		Features: result,
	}
}
