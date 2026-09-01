package service

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// Thresholds for computing runner status from heartbeat age.
const (
	// RunnerOnlineThreshold is the max heartbeat age for "online" status.
	RunnerOnlineThreshold = 90 * time.Second
	// RunnerStaleThreshold is the max heartbeat age for "stale" status.
	// Beyond this, the runner is considered "offline".
	RunnerStaleThreshold = 5 * time.Minute
)

// Compile-time check that RunnerRegistryServiceImpl implements api.RunnerRegistryService.
var _ api.RunnerRegistryService = (*RunnerRegistryServiceImpl)(nil)

// RunnerRegistryServiceImpl implements api.RunnerRegistryService using the storage layer.
type RunnerRegistryServiceImpl struct {
	storage *storage.StorageLayer
	hub     *realtime.Hub
}

// NewRunnerRegistryService creates a new RunnerRegistryServiceImpl.
func NewRunnerRegistryService(store *storage.StorageLayer) *RunnerRegistryServiceImpl {
	return &RunnerRegistryServiceImpl{storage: store}
}

// SetHub sets the realtime hub for publishing runner lifecycle SSE events.
// This is called after construction to avoid circular dependencies.
func (s *RunnerRegistryServiceImpl) SetHub(hub *realtime.Hub) {
	s.hub = hub
}

// Register registers or re-registers a runner. Sets status to online and
// timestamps to now.
func (s *RunnerRegistryServiceImpl) Register(ctx context.Context, req types.RunnerRegistration) (*types.RunnerInfo, error) {
	now := time.Now().UnixMilli()

	maxParallel := req.MaxParallel
	if maxParallel <= 0 {
		maxParallel = 1
	}

	labels := req.Labels
	if labels == nil {
		labels = make(map[string]string)
	}
	// Keep writing the legacy label for older rows/readers while also storing
	// machine_id as a first-class column for scheduling.
	if req.MachineID != "" {
		labels[machineIDLabel] = req.MachineID
	}

	executors := req.Executors
	if executors == nil {
		executors = []string{}
	}
	capabilities := req.Capabilities
	if capabilities == nil {
		capabilities = []string{}
	}

	row := &storage.RunnerRow{
		RunnerID:       req.RunnerID,
		MachineID:      req.MachineID,
		Hostname:       req.Hostname,
		Labels:         labels,
		Executors:      executors,
		Capabilities:   capabilities,
		DispatchPush:   req.DispatchPush,
		WorkspaceRoots: req.WorkspaceRoots,
		Projects:       req.Projects,
		Resources:      req.Resources,
		Capacity:       req.Capacity,
		Draining:       req.Draining,
		MaxParallel:    maxParallel,
		RegisteredAt:   now,
		LastHeartbeat:  now,
		Status:         string(types.RunnerStatusOnline),
	}

	if err := s.storage.UpsertRunner(ctx, row); err != nil {
		return nil, fmt.Errorf("register runner: %w", err)
	}

	// The pause dial lives in runner_pause_state, not on the runners row,
	// so registration cannot clear an operator's pause (nor can the
	// deregister that `brain runner stop` performs). Read it back so the
	// registration response reflects reality rather than the zero value on
	// the row we just built.
	if stored, err := s.storage.GetRunner(ctx, req.RunnerID); err == nil && stored != nil {
		row.Paused = stored.Paused
	}

	return rowToRunnerInfo(row), nil
}

// Heartbeat updates a runner's heartbeat timestamp and running task count.
// When the request carries an instance list, the runner's instance registry
// rows are reconciled to exactly that set (self-healing for missed reports).
func (s *RunnerRegistryServiceImpl) Heartbeat(ctx context.Context, runnerID string, req types.RunnerHeartbeatRequest) error {
	if err := s.storage.UpdateHeartbeat(ctx, runnerID, req.RunningTasks, req.Stats); err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}
	if req.DispatchPush != nil || req.Labels != nil || req.WorkspaceRoots != nil || req.Projects != nil || req.Resources != nil || req.Capacity != nil || req.Draining != nil {
		if err := s.storage.UpdateRunnerDispatchMetadata(ctx, runnerID, req.DispatchPush, req.Labels, req.WorkspaceRoots, req.Projects, req.Resources, req.Capacity, req.Draining); err != nil {
			return fmt.Errorf("heartbeat dispatch metadata: %w", err)
		}
	}
	if req.Instances != nil {
		rows := make([]storage.InstanceRow, 0, len(req.Instances))
		for i := range req.Instances {
			rows = append(rows, instanceToRow(runnerID, &req.Instances[i]))
		}
		if err := s.storage.ReplaceInstancesForRunner(ctx, runnerID, rows); err != nil {
			return fmt.Errorf("heartbeat instance reconcile: %w", err)
		}
	}
	return nil
}

// Deregister removes a runner and releases all runtime ownership held by it.
func (s *RunnerRegistryServiceImpl) Deregister(ctx context.Context, runnerID string) error {
	// Release all claims held by this runner first
	_, err := s.storage.ReleaseAllByRunner(ctx, runnerID)
	if err != nil {
		return fmt.Errorf("release claims on deregister: %w", err)
	}
	if _, err := s.storage.ClearFeatureAssignmentsByRunner(ctx, runnerID); err != nil {
		return fmt.Errorf("clear feature assignments on deregister: %w", err)
	}
	if _, err := s.storage.DeleteInstancesByRunner(ctx, runnerID); err != nil {
		return fmt.Errorf("delete instances on deregister: %w", err)
	}

	// Delete the runner record
	deleted, err := s.storage.DeleteRunner(ctx, runnerID)
	if err != nil {
		return fmt.Errorf("deregister runner: %w", err)
	}
	if !deleted {
		return api.ErrNotFound
	}

	return nil
}

// ListRunners returns all runners with computed status based on heartbeat age.
func (s *RunnerRegistryServiceImpl) ListRunners(ctx context.Context) (*types.RunnerListResponse, error) {
	rows, err := s.storage.ListRunners(ctx)
	if err != nil {
		return nil, fmt.Errorf("list runners: %w", err)
	}

	runners := make([]types.RunnerInfo, 0, len(rows))
	for i := range rows {
		info := rowToRunnerInfo(&rows[i])
		info.Status = computeRunnerStatus(rows[i].LastHeartbeat)
		if info.Status != types.RunnerStatusOnline {
			continue
		}
		if err := s.attachFeatureAssignments(ctx, info); err != nil {
			return nil, err
		}
		runners = append(runners, *info)
	}

	return &types.RunnerListResponse{
		Runners: runners,
		Total:   len(runners),
	}, nil
}

// GetRunner returns a single runner by ID with computed status.
func (s *RunnerRegistryServiceImpl) GetRunner(ctx context.Context, runnerID string) (*types.RunnerInfo, error) {
	row, err := s.storage.GetRunner(ctx, runnerID)
	if err != nil {
		return nil, fmt.Errorf("get runner: %w", err)
	}
	if row == nil {
		return nil, api.ErrNotFound
	}

	info := rowToRunnerInfo(row)
	info.Status = computeRunnerStatus(row.LastHeartbeat)
	if err := s.attachFeatureAssignments(ctx, info); err != nil {
		return nil, err
	}
	return info, nil
}

// UpdateConfig updates a runner's max_parallel configuration and persists it to the database.
func (s *RunnerRegistryServiceImpl) UpdateConfig(ctx context.Context, runnerID string, maxParallel int) error {
	if err := s.storage.UpdateRunnerMaxParallel(ctx, runnerID, maxParallel); err != nil {
		return fmt.Errorf("update config: %w", err)
	}
	return nil
}

// SetPaused writes the runner-scoped pause dial toggled by
// PUT /runners/{runnerId}/pause|resume.
//
// This is what makes a runner pause stick. The SSE command the handler
// publishes alongside it only reaches a runner that is connected right now;
// this state is read by the scheduler on every tick (ListRunners ->
// runnerEligibleForTask) and by the runner itself when it reconciles, so the
// pause survives SSE reconnects, runner restarts, and deregistration.
func (s *RunnerRegistryServiceImpl) SetPaused(ctx context.Context, runnerID string, paused bool) error {
	found, err := s.storage.SetRunnerPaused(ctx, runnerID, paused)
	if err != nil {
		return fmt.Errorf("set runner paused: %w", err)
	}
	if !found {
		return api.ErrNotFound
	}
	return nil
}

// UpdateAffinity updates a runner's feature affinity.
func (s *RunnerRegistryServiceImpl) UpdateAffinity(ctx context.Context, runnerID string, featureIDs []string) error {
	err := s.storage.UpdateAffinity(ctx, runnerID, featureIDs)
	if err != nil {
		return fmt.Errorf("update affinity: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Instance Registry
// ---------------------------------------------------------------------------

// UpsertInstance records or updates an OpenCode instance reported by a runner.
func (s *RunnerRegistryServiceImpl) UpsertInstance(ctx context.Context, runnerID string, inst types.OpencodeInstance) error {
	row := instanceToRow(runnerID, &inst)
	if err := s.storage.UpsertInstance(ctx, &row); err != nil {
		return fmt.Errorf("upsert instance: %w", err)
	}
	return nil
}

// DeleteInstance removes an instance record reported by a runner.
func (s *RunnerRegistryServiceImpl) DeleteInstance(ctx context.Context, runnerID, instanceID string) error {
	deleted, err := s.storage.DeleteInstance(ctx, runnerID, instanceID)
	if err != nil {
		return fmt.Errorf("delete instance: %w", err)
	}
	if !deleted {
		return api.ErrNotFound
	}
	return nil
}

// GetInstance returns a single instance scoped to a runner.
func (s *RunnerRegistryServiceImpl) GetInstance(ctx context.Context, runnerID, instanceID string) (*types.OpencodeInstance, error) {
	row, err := s.storage.GetInstance(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("get instance: %w", err)
	}
	if row == nil || row.RunnerID != runnerID {
		return nil, api.ErrNotFound
	}
	inst := rowToInstance(row)
	return &inst, nil
}

// ListInstances returns all instances reported by one runner.
func (s *RunnerRegistryServiceImpl) ListInstances(ctx context.Context, runnerID string) (*types.InstanceListResponse, error) {
	rows, err := s.storage.ListInstancesByRunner(ctx, runnerID)
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}
	// Only when there is nothing to report: a deregistered or mistyped runner id
	// rendered "Total: 0 / No instances found", which is also what an online
	// runner sitting idle looks like — and those call for opposite responses.
	// GetRunner already returns api.ErrNotFound, so this just consults it.
	if len(rows) == 0 {
		if _, err := s.GetRunner(ctx, runnerID); err != nil {
			return nil, err
		}
	}
	return instanceListResponse(rows), nil
}

// ListAllInstances returns every instance across all runners.
func (s *RunnerRegistryServiceImpl) ListAllInstances(ctx context.Context) (*types.InstanceListResponse, error) {
	rows, err := s.storage.ListAllInstances(ctx)
	if err != nil {
		return nil, fmt.Errorf("list all instances: %w", err)
	}
	return instanceListResponse(rows), nil
}

func instanceListResponse(rows []storage.InstanceRow) *types.InstanceListResponse {
	instances := make([]types.OpencodeInstance, 0, len(rows))
	for i := range rows {
		instances = append(instances, rowToInstance(&rows[i]))
	}
	return &types.InstanceListResponse{Instances: instances, Total: len(instances)}
}

func instanceToRow(runnerID string, inst *types.OpencodeInstance) storage.InstanceRow {
	lastSeen := inst.LastSeen
	if lastSeen == 0 {
		lastSeen = time.Now().UnixMilli()
	}
	kind := inst.Kind
	if kind == "" {
		kind = types.InstanceKindTask
	}
	status := inst.Status
	if status == "" {
		status = types.InstanceStatusStarting
	}
	executor := inst.Executor
	if executor == "" {
		executor = "opencode"
	}
	return storage.InstanceRow{
		InstanceID: inst.InstanceID,
		RunnerID:   runnerID,
		Hostname:   inst.Hostname,
		Kind:       kind,
		ProjectID:  inst.ProjectID,
		TaskID:     inst.TaskID,
		FeatureID:  inst.FeatureID,
		Priority:   inst.Priority,
		Title:      inst.Title,
		Workdir:    inst.Workdir,
		Port:       inst.Port,
		PID:        inst.PID,
		SessionIDs: inst.SessionIDs,
		Status:     status,
		Executor:   executor,
		Agent:      inst.Agent,
		Model:      inst.Model,
		StartedAt:  inst.StartedAt,
		LastSeen:   lastSeen,
	}
}

func rowToInstance(row *storage.InstanceRow) types.OpencodeInstance {
	return types.OpencodeInstance{
		InstanceID: row.InstanceID,
		RunnerID:   row.RunnerID,
		Hostname:   row.Hostname,
		Kind:       row.Kind,
		ProjectID:  row.ProjectID,
		TaskID:     row.TaskID,
		FeatureID:  row.FeatureID,
		Priority:   row.Priority,
		Title:      row.Title,
		Workdir:    row.Workdir,
		Port:       row.Port,
		PID:        row.PID,
		SessionIDs: row.SessionIDs,
		Status:     row.Status,
		Executor:   row.Executor,
		Agent:      row.Agent,
		Model:      row.Model,
		StartedAt:  row.StartedAt,
		LastSeen:   row.LastSeen,
	}
}

// ---------------------------------------------------------------------------
// Lifecycle Management
// ---------------------------------------------------------------------------

// DefaultLifecycleInterval is the default interval for the background lifecycle goroutine.
const DefaultLifecycleInterval = 60 * time.Second

// StartLifecycleManager launches a background goroutine that periodically sweeps
// all runners and transitions their status based on heartbeat age. Stale runners
// (heartbeat > 90s) get marked "stale". Offline runners (heartbeat > 5min) get
// marked "offline" and have their runtime ownership released. Respects context cancellation.
func (s *RunnerRegistryServiceImpl) StartLifecycleManager(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.Info("runner lifecycle manager stopped")
				return
			case <-ticker.C:
				s.RunLifecycleSweep(ctx)
			}
		}
	}()
}

// RunLifecycleSweep performs a single lifecycle sweep across all runners.
// For each runner that is not already "offline":
//   - heartbeat age >= RunnerStaleThreshold (5min): transition to "offline", release runtime ownership
//   - heartbeat age >= RunnerOnlineThreshold (90s): transition to "stale"
//   - otherwise: ensure status is "online"
//
// This method is exported to allow direct testing without timers.
func (s *RunnerRegistryServiceImpl) RunLifecycleSweep(ctx context.Context) {
	rows, err := s.storage.ListRunners(ctx)
	if err != nil {
		slog.Error("lifecycle sweep: list runners failed", "error", err)
		return
	}

	for i := range rows {
		row := &rows[i]

		// Skip already-offline runners — they were handled on transition
		if row.Status == string(types.RunnerStatusOffline) {
			continue
		}

		age := time.Since(time.UnixMilli(row.LastHeartbeat))
		newStatus := computeRunnerStatus(row.LastHeartbeat)

		if newStatus == types.RunnerStatusOffline {
			// Transition to offline: update status and release all runtime ownership
			if err := s.storage.SetRunnerStatus(ctx, row.RunnerID, string(types.RunnerStatusOffline)); err != nil {
				slog.Error("lifecycle sweep: set offline failed",
					"runner_id", row.RunnerID, "error", err)
				continue
			}
			released, err := s.storage.ReleaseAllByRunner(ctx, row.RunnerID)
			if err != nil {
				slog.Error("lifecycle sweep: release claims failed",
					"runner_id", row.RunnerID, "error", err)
				continue
			}
			featuresReleased, err := s.storage.ClearFeatureAssignmentsByRunner(ctx, row.RunnerID)
			if err != nil {
				slog.Error("lifecycle sweep: clear feature assignments failed",
					"runner_id", row.RunnerID, "error", err)
				continue
			}
			if _, err := s.storage.DeleteInstancesByRunner(ctx, row.RunnerID); err != nil {
				slog.Error("lifecycle sweep: delete instances failed",
					"runner_id", row.RunnerID, "error", err)
				continue
			}
			slog.Info("runner transitioned to offline",
				"runner_id", row.RunnerID,
				"heartbeat_age", age.Round(time.Second),
				"claims_released", released,
				"feature_assignments_released", featuresReleased)

			// Publish runner_offline SSE event
			if s.hub != nil {
				s.hub.PublishRunnerOffline(types.SSERunnerOfflineData{
					SSEEventData: types.SSEEventData{
						Type:      types.SSEEventRunnerOffline,
						Transport: "sse",
						Timestamp: types.TimeNowUTC().Format(time.RFC3339),
					},
					RunnerID: row.RunnerID,
					Hostname: row.Hostname,
					Status:   string(types.RunnerStatusOffline),
					Reason:   fmt.Sprintf("heartbeat timeout (%s)", age.Round(time.Second)),
				})
			}

		} else if newStatus == types.RunnerStatusStale && row.Status != string(types.RunnerStatusStale) {
			// Transition to stale (only if not already stale)
			if err := s.storage.SetRunnerStatus(ctx, row.RunnerID, string(types.RunnerStatusStale)); err != nil {
				slog.Error("lifecycle sweep: set stale failed",
					"runner_id", row.RunnerID, "error", err)
				continue
			}
			slog.Info("runner transitioned to stale",
				"runner_id", row.RunnerID,
				"heartbeat_age", age.Round(time.Second))

			// Publish runner_offline SSE event (stale is a degraded state worth notifying)
			if s.hub != nil {
				s.hub.PublishRunnerOffline(types.SSERunnerOfflineData{
					SSEEventData: types.SSEEventData{
						Type:      types.SSEEventRunnerOffline,
						Transport: "sse",
						Timestamp: types.TimeNowUTC().Format(time.RFC3339),
					},
					RunnerID: row.RunnerID,
					Hostname: row.Hostname,
					Status:   string(types.RunnerStatusStale),
					Reason:   fmt.Sprintf("heartbeat stale (%s)", age.Round(time.Second)),
				})
			}

		} else if newStatus == types.RunnerStatusOnline && row.Status != string(types.RunnerStatusOnline) {
			// Runner recovered (e.g., heartbeat came in) — set back to online
			if err := s.storage.SetRunnerStatus(ctx, row.RunnerID, string(types.RunnerStatusOnline)); err != nil {
				slog.Error("lifecycle sweep: set online failed",
					"runner_id", row.RunnerID, "error", err)
				continue
			}
			slog.Info("runner recovered to online",
				"runner_id", row.RunnerID,
				"heartbeat_age", age.Round(time.Second))
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// computeRunnerStatus computes the runner status from the last heartbeat timestamp.
// online: heartbeat < 90s ago
// stale:  heartbeat between 90s and 5min ago
// offline: heartbeat > 5min ago
func computeRunnerStatus(lastHeartbeatMs int64) types.RunnerStatus {
	age := time.Since(time.UnixMilli(lastHeartbeatMs))

	if age < RunnerOnlineThreshold {
		return types.RunnerStatusOnline
	}
	if age < RunnerStaleThreshold {
		return types.RunnerStatusStale
	}
	return types.RunnerStatusOffline
}

// machineIDLabel is the legacy labels key under which the machine id was stored
// before runners had a first-class machine_id column.
const machineIDLabel = "_machine_id"

// runnerMachineID returns a runner row's machine id, falling back to the
// legacy label for rows written before the first-class column existed.
// Callers that compare a machine id against a task's origin MUST go through
// this rather than reading row.MachineID directly, or an older runner reads
// as "no machine" and never matches its own tasks.
func runnerMachineID(row *storage.RunnerRow) string {
	if row == nil {
		return ""
	}
	if row.MachineID != "" {
		return row.MachineID
	}
	if row.Labels != nil {
		return row.Labels[machineIDLabel]
	}
	return ""
}

// rowToRunnerInfo converts a storage RunnerRow to an API RunnerInfo.
func rowToRunnerInfo(row *storage.RunnerRow) *types.RunnerInfo {
	machineID := runnerMachineID(row)
	activeTasks := 0
	if row.Labels != nil {
		if runningTasks := row.Labels["_running_tasks"]; runningTasks != "" {
			if parsed, err := strconv.Atoi(runningTasks); err == nil && parsed >= 0 {
				activeTasks = parsed
			}
		}
	}
	return &types.RunnerInfo{
		RunnerID:       row.RunnerID,
		MachineID:      machineID,
		Hostname:       row.Hostname,
		Labels:         row.Labels,
		Executors:      row.Executors,
		Projects:       row.Projects,
		Capabilities:   row.Capabilities,
		DispatchPush:   row.DispatchPush,
		WorkspaceRoots: row.WorkspaceRoots,
		Resources:      row.Resources,
		Capacity:       row.Capacity,
		Draining:       row.Draining,
		Paused:         row.Paused,
		MaxParallel:    row.MaxParallel,
		ActiveTasks:    activeTasks,
		FeatureIDs:     row.FeatureIDs,
		RegisteredAt:   time.UnixMilli(row.RegisteredAt).UTC().Format(time.RFC3339),
		LastHeartbeat:  time.UnixMilli(row.LastHeartbeat).UTC().Format(time.RFC3339),
		Status:         types.RunnerStatus(row.Status),
	}
}

func (s *RunnerRegistryServiceImpl) attachFeatureAssignments(ctx context.Context, info *types.RunnerInfo) error {
	assignments, err := s.storage.ListFeatureAssignmentsByRunner(ctx, info.RunnerID)
	if err != nil {
		return fmt.Errorf("list feature assignments for runner %s: %w", info.RunnerID, err)
	}
	if len(assignments) == 0 {
		return nil
	}

	info.FeatureAssignments = make([]types.FeatureAssignmentResponse, 0, len(assignments))
	for _, assignment := range assignments {
		info.FeatureAssignments = append(info.FeatureAssignments, types.FeatureAssignmentResponse{
			ProjectID:  assignment.ProjectID,
			FeatureID:  assignment.FeatureID,
			RunnerID:   assignment.RunnerID,
			Source:     assignment.Source,
			Status:     assignment.Status,
			AssignedAt: time.UnixMilli(assignment.AssignedAt).UTC().Format(time.RFC3339),
			UpdatedAt:  time.UnixMilli(assignment.UpdatedAt).UTC().Format(time.RFC3339),
		})
	}
	return nil
}
