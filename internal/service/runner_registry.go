package service

import (
	"context"
	"fmt"
	"log/slog"
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

	executors := req.Executors
	if executors == nil {
		executors = []string{}
	}
	capabilities := req.Capabilities
	if capabilities == nil {
		capabilities = []string{}
	}

	row := &storage.RunnerRow{
		RunnerID:      req.RunnerID,
		Hostname:      req.Hostname,
		Labels:        labels,
		Executors:     executors,
		Capabilities:  capabilities,
		MaxParallel:   maxParallel,
		RegisteredAt:  now,
		LastHeartbeat: now,
		Status:        string(types.RunnerStatusOnline),
	}

	if err := s.storage.UpsertRunner(ctx, row); err != nil {
		return nil, fmt.Errorf("register runner: %w", err)
	}

	return rowToRunnerInfo(row), nil
}

// Heartbeat updates a runner's heartbeat timestamp and running task count.
func (s *RunnerRegistryServiceImpl) Heartbeat(ctx context.Context, runnerID string, req types.RunnerHeartbeatRequest) error {
	if err := s.storage.UpdateHeartbeat(ctx, runnerID, req.RunningTasks, req.Stats); err != nil {
		return fmt.Errorf("heartbeat: %w", err)
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

// UpdateAffinity updates a runner's feature affinity.
func (s *RunnerRegistryServiceImpl) UpdateAffinity(ctx context.Context, runnerID string, featureIDs []string) error {
	err := s.storage.UpdateAffinity(ctx, runnerID, featureIDs)
	if err != nil {
		return fmt.Errorf("update affinity: %w", err)
	}
	return nil
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

// rowToRunnerInfo converts a storage RunnerRow to an API RunnerInfo.
func rowToRunnerInfo(row *storage.RunnerRow) *types.RunnerInfo {
	return &types.RunnerInfo{
		RunnerID:      row.RunnerID,
		Hostname:      row.Hostname,
		Labels:        row.Labels,
		Executors:     row.Executors,
		Capabilities:  row.Capabilities,
		MaxParallel:   row.MaxParallel,
		FeatureIDs:    row.FeatureIDs,
		RegisteredAt:  time.UnixMilli(row.RegisteredAt).UTC().Format(time.RFC3339),
		LastHeartbeat: time.UnixMilli(row.LastHeartbeat).UTC().Format(time.RFC3339),
		Status:        types.RunnerStatus(row.Status),
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
