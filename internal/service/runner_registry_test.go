package service

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"

	_ "github.com/glebarez/go-sqlite"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestRunnerRegistryService(t *testing.T) (*RunnerRegistryServiceImpl, *storage.StorageLayer) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}

	store, err := storage.NewWithDB(db)
	if err != nil {
		t.Fatalf("NewWithDB failed: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	svc := NewRunnerRegistryService(store)
	return svc, store
}

// ---------------------------------------------------------------------------
// Register
// ---------------------------------------------------------------------------

func TestRunnerRegistry_Register_NewRunner(t *testing.T) {
	svc, _ := newTestRunnerRegistryService(t)
	ctx := context.Background()

	req := types.RunnerRegistration{
		RunnerID:    "runner-1",
		Hostname:    "host-a",
		Labels:      map[string]string{"env": "prod"},
		Executors:   []string{"opencode"},
		MaxParallel: 3,
	}

	info, err := svc.Register(ctx, req)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if info.RunnerID != "runner-1" {
		t.Errorf("expected runner_id runner-1, got %s", info.RunnerID)
	}
	if info.Hostname != "host-a" {
		t.Errorf("expected hostname host-a, got %s", info.Hostname)
	}
	if info.MaxParallel != 3 {
		t.Errorf("expected max_parallel 3, got %d", info.MaxParallel)
	}
	if info.Status != types.RunnerStatusOnline {
		t.Errorf("expected status online, got %s", info.Status)
	}
	if info.RegisteredAt == "" {
		t.Error("expected registered_at to be set")
	}
	if info.LastHeartbeat == "" {
		t.Error("expected last_heartbeat to be set")
	}
}

func TestRunnerRegistration_HasCapabilitiesField(t *testing.T) {
	registrationType := reflect.TypeOf(types.RunnerRegistration{})
	field, ok := registrationType.FieldByName("Capabilities")
	if !ok {
		t.Fatal("RunnerRegistration should accept capabilities")
	}
	if field.Type != reflect.TypeOf([]string{}) {
		t.Fatalf("Capabilities type = %s, want []string", field.Type)
	}
}

func TestRunnerRegistry_Register_PersistsCapabilities(t *testing.T) {
	svc, _ := newTestRunnerRegistryService(t)
	ctx := context.Background()

	info, err := svc.Register(ctx, types.RunnerRegistration{
		RunnerID:     "runner-cap",
		Hostname:     "host-cap",
		Executors:    []string{"opencode"},
		Capabilities: []string{"docker", "gpu"},
		MaxParallel:  2,
	})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	assertStringSliceEqual(t, info.Capabilities, []string{"docker", "gpu"})

	got, err := svc.GetRunner(ctx, "runner-cap")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	assertStringSliceEqual(t, got.Capabilities, []string{"docker", "gpu"})
}

func TestRowToRunnerInfo_IncludesCapabilities(t *testing.T) {
	now := time.Now().UnixMilli()
	info := rowToRunnerInfo(&storage.RunnerRow{
		RunnerID:      "runner-cap",
		Hostname:      "host-cap",
		Labels:        map[string]string{},
		Executors:     []string{"opencode"},
		Capabilities:  []string{"docker", "gpu"},
		MaxParallel:   1,
		RegisteredAt:  now,
		LastHeartbeat: now,
		Status:        string(types.RunnerStatusOnline),
	})

	assertStringSliceEqual(t, info.Capabilities, []string{"docker", "gpu"})
}

func TestRunnerRegistry_GetListAndHeartbeatPreserveCapabilities(t *testing.T) {
	svc, store := newTestRunnerRegistryService(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	if err := store.UpsertRunner(ctx, &storage.RunnerRow{
		RunnerID:      "runner-cap",
		Hostname:      "host-cap",
		Labels:        map[string]string{"env": "prod"},
		Executors:     []string{"opencode"},
		Capabilities:  []string{"docker", "gpu"},
		MaxParallel:   2,
		RegisteredAt:  now,
		LastHeartbeat: now,
		Status:        string(types.RunnerStatusOnline),
	}); err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	got, err := svc.GetRunner(ctx, "runner-cap")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	assertStringSliceEqual(t, got.Capabilities, []string{"docker", "gpu"})

	listed, err := svc.ListRunners(ctx)
	if err != nil {
		t.Fatalf("ListRunners failed: %v", err)
	}
	if listed.Total != 1 {
		t.Fatalf("Total = %d, want 1", listed.Total)
	}
	assertStringSliceEqual(t, listed.Runners[0].Capabilities, []string{"docker", "gpu"})

	if err := svc.Heartbeat(ctx, "runner-cap", types.RunnerHeartbeatRequest{RunningTasks: 1}); err != nil {
		t.Fatalf("Heartbeat failed: %v", err)
	}
	afterHeartbeat, err := svc.GetRunner(ctx, "runner-cap")
	if err != nil {
		t.Fatalf("GetRunner after heartbeat failed: %v", err)
	}
	assertStringSliceEqual(t, afterHeartbeat.Capabilities, []string{"docker", "gpu"})
}

func assertStringSliceEqual(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("slice = %#v, want %#v", got, want)
	}
}

func TestRunnerRegistry_Register_PersistsDispatchMetadataAndProjects(t *testing.T) {
	svc, _ := newTestRunnerRegistryService(t)
	ctx := context.Background()

	info, err := svc.Register(ctx, types.RunnerRegistration{
		RunnerID:       "runner-dispatch",
		MachineID:      "machine-explicit",
		Hostname:       "host-dispatch",
		Executors:      []string{"opencode", "pi"},
		Capabilities:   []string{"docker"},
		DispatchPush:   true,
		WorkspaceRoots: []string{"/work/brain", "/tmp/worktrees"},
		Projects:       []string{"brain-api", "brain-docs"},
		Resources: map[string]interface{}{
			"os":   "darwin",
			"arch": "arm64",
		},
		Capacity: map[string]interface{}{
			"max_parallel": float64(4),
		},
		Draining:    true,
		MaxParallel: 4,
	})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	assertRunnerInfoDispatchMetadata(t, info)

	got, err := svc.GetRunner(ctx, "runner-dispatch")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	assertRunnerInfoDispatchMetadata(t, got)

	listed, err := svc.ListRunners(ctx)
	if err != nil {
		t.Fatalf("ListRunners failed: %v", err)
	}
	if listed.Total != 1 {
		t.Fatalf("Total = %d, want 1", listed.Total)
	}
	assertRunnerInfoDispatchMetadata(t, &listed.Runners[0])
}

func TestRowToRunnerInfo_MachineIDFallsBackToLegacyLabel(t *testing.T) {
	now := time.Now().UnixMilli()
	info := rowToRunnerInfo(&storage.RunnerRow{
		RunnerID:      "runner-legacy",
		Hostname:      "host-legacy",
		Labels:        map[string]string{"_machine_id": "machine-legacy"},
		Executors:     []string{"opencode"},
		MaxParallel:   1,
		RegisteredAt:  now,
		LastHeartbeat: now,
		Status:        string(types.RunnerStatusOnline),
	})

	if info.MachineID != "machine-legacy" {
		t.Fatalf("MachineID = %q, want legacy label fallback", info.MachineID)
	}
}

func assertRunnerInfoDispatchMetadata(t *testing.T, info *types.RunnerInfo) {
	t.Helper()
	if info.MachineID != "machine-explicit" {
		t.Fatalf("MachineID = %q, want machine-explicit", info.MachineID)
	}
	if !info.DispatchPush {
		t.Fatal("DispatchPush = false, want true")
	}
	assertStringSliceEqual(t, info.WorkspaceRoots, []string{"/work/brain", "/tmp/worktrees"})
	assertStringSliceEqual(t, info.Projects, []string{"brain-api", "brain-docs"})
	if info.Resources["os"] != "darwin" || info.Resources["arch"] != "arm64" {
		t.Fatalf("Resources = %#v, want os/arch", info.Resources)
	}
	if info.Capacity["max_parallel"] != float64(4) {
		t.Fatalf("Capacity = %#v, want max_parallel", info.Capacity)
	}
	if !info.Draining {
		t.Fatal("Draining = false, want true")
	}
}

func TestRunnerRegistry_Heartbeat_UpdatesDispatchRuntimeMetadata(t *testing.T) {
	svc, _ := newTestRunnerRegistryService(t)
	ctx := context.Background()

	_, err := svc.Register(ctx, types.RunnerRegistration{
		RunnerID:       "runner-heartbeat-meta",
		MachineID:      "machine-heartbeat",
		Hostname:       "host-heartbeat",
		Executors:      []string{"opencode"},
		WorkspaceRoots: []string{"/old/root"},
		Projects:       []string{"old-project"},
		Resources:      map[string]interface{}{"os": "darwin"},
		Capacity:       map[string]interface{}{"available": float64(1)},
		MaxParallel:    2,
	})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	draining := true
	dispatchPush := true
	err = svc.Heartbeat(ctx, "runner-heartbeat-meta", types.RunnerHeartbeatRequest{
		RunningTasks:   1,
		DispatchPush:   &dispatchPush,
		Labels:         map[string]string{"pool": "fast"},
		WorkspaceRoots: []string{"/new/root", "/worktrees"},
		Projects:       []string{"brain-api", "brain-docs"},
		Resources:      map[string]interface{}{"os": "darwin", "arch": "arm64"},
		Capacity:       map[string]interface{}{"available": float64(3), "max_parallel": float64(4)},
		Draining:       &draining,
	})
	if err != nil {
		t.Fatalf("Heartbeat failed: %v", err)
	}

	got, err := svc.GetRunner(ctx, "runner-heartbeat-meta")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if !got.DispatchPush {
		t.Fatal("DispatchPush = false, want true")
	}
	if got.ActiveTasks != 1 {
		t.Fatalf("ActiveTasks = %d, want 1", got.ActiveTasks)
	}
	if got.Labels["pool"] != "fast" {
		t.Fatalf("Labels = %#v, want heartbeat pool label", got.Labels)
	}
	assertStringSliceEqual(t, got.WorkspaceRoots, []string{"/new/root", "/worktrees"})
	assertStringSliceEqual(t, got.Projects, []string{"brain-api", "brain-docs"})
	if got.Resources["arch"] != "arm64" {
		t.Fatalf("Resources = %#v, want updated arch", got.Resources)
	}
	if got.Capacity["available"] != float64(3) || got.Capacity["max_parallel"] != float64(4) {
		t.Fatalf("Capacity = %#v, want heartbeat values", got.Capacity)
	}
	if !got.Draining {
		t.Fatal("Draining = false, want true")
	}
}

func TestRunnerRegistry_Register_ReRegister(t *testing.T) {
	svc, _ := newTestRunnerRegistryService(t)
	ctx := context.Background()

	req := types.RunnerRegistration{
		RunnerID:    "runner-1",
		Hostname:    "host-a",
		MaxParallel: 2,
	}
	_, err := svc.Register(ctx, req)
	if err != nil {
		t.Fatalf("first Register failed: %v", err)
	}

	// Re-register with updated hostname
	req.Hostname = "host-b"
	req.MaxParallel = 5
	info, err := svc.Register(ctx, req)
	if err != nil {
		t.Fatalf("second Register failed: %v", err)
	}
	if info.Hostname != "host-b" {
		t.Errorf("expected hostname host-b after re-register, got %s", info.Hostname)
	}
	if info.MaxParallel != 5 {
		t.Errorf("expected max_parallel 5 after re-register, got %d", info.MaxParallel)
	}
}

func TestRunnerRegistry_Register_DefaultMaxParallel(t *testing.T) {
	svc, _ := newTestRunnerRegistryService(t)
	ctx := context.Background()

	req := types.RunnerRegistration{
		RunnerID: "runner-1",
		Hostname: "host-a",
		// MaxParallel is 0 (not set)
	}

	info, err := svc.Register(ctx, req)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if info.MaxParallel != 1 {
		t.Errorf("expected max_parallel to default to 1, got %d", info.MaxParallel)
	}
}

// ---------------------------------------------------------------------------
// Heartbeat
// ---------------------------------------------------------------------------

func TestRunnerRegistry_Heartbeat_Success(t *testing.T) {
	svc, _ := newTestRunnerRegistryService(t)
	ctx := context.Background()

	// Register first
	_, err := svc.Register(ctx, types.RunnerRegistration{
		RunnerID: "runner-1",
		Hostname: "host-a",
	})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Heartbeat
	err = svc.Heartbeat(ctx, "runner-1", types.RunnerHeartbeatRequest{
		RunningTasks: 2,
	})
	if err != nil {
		t.Fatalf("Heartbeat failed: %v", err)
	}

	// Verify runner still retrievable with online status
	info, err := svc.GetRunner(ctx, "runner-1")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if info.Status != types.RunnerStatusOnline {
		t.Errorf("expected status online after heartbeat, got %s", info.Status)
	}
	if info.ActiveTasks != 2 {
		t.Errorf("expected active tasks 2 after heartbeat, got %d", info.ActiveTasks)
	}
}

func TestRunnerRegistry_Heartbeat_NonexistentRunner(t *testing.T) {
	svc, _ := newTestRunnerRegistryService(t)
	ctx := context.Background()

	err := svc.Heartbeat(ctx, "nonexistent", types.RunnerHeartbeatRequest{
		RunningTasks: 0,
	})
	if err == nil {
		t.Fatal("expected error for heartbeat on nonexistent runner")
	}
}

// ---------------------------------------------------------------------------
// Deregister
// ---------------------------------------------------------------------------

func TestRunnerRegistry_Deregister_Success(t *testing.T) {
	svc, store := newTestRunnerRegistryService(t)
	ctx := context.Background()

	// Register
	_, err := svc.Register(ctx, types.RunnerRegistration{
		RunnerID: "runner-1",
		Hostname: "host-a",
	})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Create a claim so we can verify it gets released
	ok, _, claimErr := store.ClaimTask(ctx, "proj1", "task1", "runner-1", 5*time.Minute)
	if claimErr != nil {
		t.Fatalf("ClaimTask failed: %v", claimErr)
	}
	if !ok {
		t.Fatal("ClaimTask should have succeeded")
	}

	// Deregister
	err = svc.Deregister(ctx, "runner-1")
	if err != nil {
		t.Fatalf("Deregister failed: %v", err)
	}

	// Verify runner is gone
	info, err := svc.GetRunner(ctx, "runner-1")
	if err == nil && info != nil {
		t.Error("expected runner to be deleted after deregister")
	}

	// Verify claims were released
	claim, err := store.GetClaim(ctx, "proj1", "task1")
	if err != nil {
		t.Fatalf("GetClaim failed: %v", err)
	}
	if claim != nil {
		t.Error("expected claim to be released after deregister")
	}
}

func TestRunnerRegistry_Deregister_Nonexistent(t *testing.T) {
	svc, _ := newTestRunnerRegistryService(t)
	ctx := context.Background()

	err := svc.Deregister(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for deregistering nonexistent runner")
	}
}

func TestRunnerRegistry_Deregister_ReleasesFeatureAssignments(t *testing.T) {
	svc, store := newTestRunnerRegistryService(t)
	ctx := context.Background()

	_, err := svc.Register(ctx, types.RunnerRegistration{
		RunnerID: "runner-1",
		Hostname: "host-a",
	})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if _, err := store.ForceAssignFeature(ctx, "proj1", "feature-a", "runner-1", "auto", "active"); err != nil {
		t.Fatalf("ForceAssignFeature feature-a failed: %v", err)
	}
	if _, err := store.ForceAssignFeature(ctx, "proj2", "feature-b", "runner-1", "manual", "active"); err != nil {
		t.Fatalf("ForceAssignFeature feature-b failed: %v", err)
	}

	if err := svc.Deregister(ctx, "runner-1"); err != nil {
		t.Fatalf("Deregister failed: %v", err)
	}

	assignments, err := store.ListFeatureAssignmentsByRunner(ctx, "runner-1")
	if err != nil {
		t.Fatalf("ListFeatureAssignmentsByRunner failed: %v", err)
	}
	if len(assignments) != 0 {
		t.Fatalf("expected feature assignments to be released after deregister, got %+v", assignments)
	}
}

// ---------------------------------------------------------------------------
// ListRunners
// ---------------------------------------------------------------------------

func TestRunnerRegistry_ListRunners_Empty(t *testing.T) {
	svc, _ := newTestRunnerRegistryService(t)
	ctx := context.Background()

	resp, err := svc.ListRunners(ctx)
	if err != nil {
		t.Fatalf("ListRunners failed: %v", err)
	}
	if resp.Total != 0 {
		t.Errorf("expected 0 runners, got %d", resp.Total)
	}
	if len(resp.Runners) != 0 {
		t.Errorf("expected empty runners list, got %d", len(resp.Runners))
	}
}

func TestRunnerRegistry_ListRunners_Multiple(t *testing.T) {
	svc, _ := newTestRunnerRegistryService(t)
	ctx := context.Background()

	_, _ = svc.Register(ctx, types.RunnerRegistration{
		RunnerID: "runner-1",
		Hostname: "host-a",
	})
	_, _ = svc.Register(ctx, types.RunnerRegistration{
		RunnerID: "runner-2",
		Hostname: "host-b",
	})

	resp, err := svc.ListRunners(ctx)
	if err != nil {
		t.Fatalf("ListRunners failed: %v", err)
	}
	if resp.Total != 2 {
		t.Errorf("expected 2 runners, got %d", resp.Total)
	}
}

func TestRunnerRegistry_ListRunnersIncludesFeatureAssignments(t *testing.T) {
	svc, store := newTestRunnerRegistryService(t)
	ctx := context.Background()

	_, _ = svc.Register(ctx, types.RunnerRegistration{
		RunnerID: "runner-1",
		Hostname: "host-a",
	})
	_, _ = svc.Register(ctx, types.RunnerRegistration{
		RunnerID: "runner-2",
		Hostname: "host-b",
	})
	if _, err := store.ForceAssignFeature(ctx, "brain-api", "feature-manual", "runner-1", "manual", "active"); err != nil {
		t.Fatalf("ForceAssignFeature manual failed: %v", err)
	}
	if _, err := store.ForceAssignFeature(ctx, "brain-api", "feature-auto", "runner-1", "auto", "active"); err != nil {
		t.Fatalf("ForceAssignFeature auto failed: %v", err)
	}

	resp, err := svc.ListRunners(ctx)
	if err != nil {
		t.Fatalf("ListRunners failed: %v", err)
	}

	var runnerOne *types.RunnerInfo
	for i := range resp.Runners {
		if resp.Runners[i].RunnerID == "runner-1" {
			runnerOne = &resp.Runners[i]
			break
		}
	}
	if runnerOne == nil {
		t.Fatal("runner-1 not found")
	}
	if len(runnerOne.FeatureAssignments) != 2 {
		t.Fatalf("assignments = %d, want 2", len(runnerOne.FeatureAssignments))
	}
	if runnerOne.FeatureAssignments[0].FeatureID != "feature-auto" || runnerOne.FeatureAssignments[0].Source != "auto" {
		t.Fatalf("first assignment = %+v, want feature-auto auto", runnerOne.FeatureAssignments[0])
	}
	if runnerOne.FeatureAssignments[1].FeatureID != "feature-manual" || runnerOne.FeatureAssignments[1].Source != "manual" {
		t.Fatalf("second assignment = %+v, want feature-manual manual", runnerOne.FeatureAssignments[1])
	}
}

// ---------------------------------------------------------------------------
// GetRunner
// ---------------------------------------------------------------------------

func TestRunnerRegistry_GetRunner_Found(t *testing.T) {
	svc, _ := newTestRunnerRegistryService(t)
	ctx := context.Background()

	_, _ = svc.Register(ctx, types.RunnerRegistration{
		RunnerID:  "runner-1",
		Hostname:  "host-a",
		Executors: []string{"opencode", "pi"},
	})

	info, err := svc.GetRunner(ctx, "runner-1")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if info.RunnerID != "runner-1" {
		t.Errorf("expected runner-1, got %s", info.RunnerID)
	}
	if len(info.Executors) != 2 {
		t.Errorf("expected 2 executors, got %d", len(info.Executors))
	}
}

func TestRunnerRegistry_GetRunner_NotFound(t *testing.T) {
	svc, _ := newTestRunnerRegistryService(t)
	ctx := context.Background()

	_, err := svc.GetRunner(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent runner")
	}
}

// ---------------------------------------------------------------------------
// Status Computation
// ---------------------------------------------------------------------------

func TestRunnerRegistry_StatusComputation_Online(t *testing.T) {
	svc, _ := newTestRunnerRegistryService(t)
	ctx := context.Background()

	// Register a runner (heartbeat is fresh)
	_, _ = svc.Register(ctx, types.RunnerRegistration{
		RunnerID: "runner-1",
		Hostname: "host-a",
	})

	info, err := svc.GetRunner(ctx, "runner-1")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if info.Status != types.RunnerStatusOnline {
		t.Errorf("expected online status for fresh runner, got %s", info.Status)
	}
}

func TestRunnerRegistry_StatusComputation_Stale(t *testing.T) {
	svc, store := newTestRunnerRegistryService(t)
	ctx := context.Background()

	// Register runner
	_, _ = svc.Register(ctx, types.RunnerRegistration{
		RunnerID: "runner-1",
		Hostname: "host-a",
	})

	// Manually set heartbeat to 2 minutes ago (>90s, <5min = stale)
	twoMinAgo := time.Now().Add(-2 * time.Minute).UnixMilli()
	err := store.UpsertRunner(ctx, &storage.RunnerRow{
		RunnerID:      "runner-1",
		Hostname:      "host-a",
		Labels:        map[string]string{},
		Executors:     []string{},
		MaxParallel:   1,
		RegisteredAt:  twoMinAgo,
		LastHeartbeat: twoMinAgo,
		Status:        "online",
	})
	if err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	info, err := svc.GetRunner(ctx, "runner-1")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if info.Status != types.RunnerStatusStale {
		t.Errorf("expected stale status for 2min-old heartbeat, got %s", info.Status)
	}
}

func TestRunnerRegistry_StatusComputation_Offline(t *testing.T) {
	svc, store := newTestRunnerRegistryService(t)
	ctx := context.Background()

	// Register runner
	_, _ = svc.Register(ctx, types.RunnerRegistration{
		RunnerID: "runner-1",
		Hostname: "host-a",
	})

	// Manually set heartbeat to 10 minutes ago (>5min = offline)
	tenMinAgo := time.Now().Add(-10 * time.Minute).UnixMilli()
	err := store.UpsertRunner(ctx, &storage.RunnerRow{
		RunnerID:      "runner-1",
		Hostname:      "host-a",
		Labels:        map[string]string{},
		Executors:     []string{},
		MaxParallel:   1,
		RegisteredAt:  tenMinAgo,
		LastHeartbeat: tenMinAgo,
		Status:        "online",
	})
	if err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	info, err := svc.GetRunner(ctx, "runner-1")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if info.Status != types.RunnerStatusOffline {
		t.Errorf("expected offline status for 10min-old heartbeat, got %s", info.Status)
	}
}

// ---------------------------------------------------------------------------
// Lifecycle Management — StartLifecycleManager
// ---------------------------------------------------------------------------

func TestLifecycleManager_MarksStaleRunners(t *testing.T) {
	svc, store := newTestRunnerRegistryService(t)
	ctx := context.Background()

	now := time.Now().UnixMilli()

	// Register a runner with heartbeat 2 minutes ago (>90s = stale threshold)
	err := store.UpsertRunner(ctx, &storage.RunnerRow{
		RunnerID:      "runner-stale",
		Hostname:      "host-a",
		Labels:        map[string]string{},
		Executors:     []string{},
		MaxParallel:   1,
		RegisteredAt:  now - 120000,
		LastHeartbeat: now - 120000, // 2 min ago
		Status:        "online",
	})
	if err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	// Run one lifecycle sweep
	svc.RunLifecycleSweep(ctx)

	// Verify runner is now marked stale in the DB
	row, err := store.GetRunner(ctx, "runner-stale")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if row.Status != "stale" {
		t.Errorf("expected status 'stale', got %q", row.Status)
	}
}

func TestLifecycleManager_MarksOfflineAndReleasesClaims(t *testing.T) {
	svc, store := newTestRunnerRegistryService(t)
	ctx := context.Background()

	now := time.Now().UnixMilli()

	// Register a runner with heartbeat 10 minutes ago (>5min = offline threshold)
	err := store.UpsertRunner(ctx, &storage.RunnerRow{
		RunnerID:      "runner-dead",
		Hostname:      "host-a",
		Labels:        map[string]string{},
		Executors:     []string{},
		MaxParallel:   1,
		RegisteredAt:  now - 600000,
		LastHeartbeat: now - 600000, // 10 min ago
		Status:        "online",
	})
	if err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	// Create a claim held by this runner
	ok, _, claimErr := store.ClaimTask(ctx, "proj1", "task1", "runner-dead", 30*time.Minute)
	if claimErr != nil {
		t.Fatalf("ClaimTask failed: %v", claimErr)
	}
	if !ok {
		t.Fatal("ClaimTask should have succeeded")
	}

	// Run one lifecycle sweep
	svc.RunLifecycleSweep(ctx)

	// Verify runner is now offline
	row, err := store.GetRunner(ctx, "runner-dead")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if row.Status != "offline" {
		t.Errorf("expected status 'offline', got %q", row.Status)
	}

	// Verify claim was released
	claim, err := store.GetClaim(ctx, "proj1", "task1")
	if err != nil {
		t.Fatalf("GetClaim failed: %v", err)
	}
	if claim != nil {
		t.Error("expected claim to be released for offline runner")
	}
}

func TestLifecycleManager_MarksOfflineAndReleasesFeatureAssignments(t *testing.T) {
	svc, store := newTestRunnerRegistryService(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	err := store.UpsertRunner(ctx, &storage.RunnerRow{
		RunnerID:      "runner-dead",
		Hostname:      "host-a",
		Labels:        map[string]string{},
		Executors:     []string{},
		MaxParallel:   1,
		RegisteredAt:  now - 600000,
		LastHeartbeat: now - 600000,
		Status:        "online",
	})
	if err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}
	if _, err := store.ForceAssignFeature(ctx, "proj1", "feature-a", "runner-dead", "auto", "active"); err != nil {
		t.Fatalf("ForceAssignFeature failed: %v", err)
	}

	svc.RunLifecycleSweep(ctx)

	assignments, err := store.ListFeatureAssignmentsByRunner(ctx, "runner-dead")
	if err != nil {
		t.Fatalf("ListFeatureAssignmentsByRunner failed: %v", err)
	}
	if len(assignments) != 0 {
		t.Fatalf("expected feature assignments to be released for offline runner, got %+v", assignments)
	}
}

func TestLifecycleManager_StaleToOfflineTransition(t *testing.T) {
	svc, store := newTestRunnerRegistryService(t)
	ctx := context.Background()

	now := time.Now().UnixMilli()

	// Runner already marked stale, heartbeat 6 minutes ago (>5min = should go offline)
	err := store.UpsertRunner(ctx, &storage.RunnerRow{
		RunnerID:      "runner-stale-old",
		Hostname:      "host-a",
		Labels:        map[string]string{},
		Executors:     []string{},
		MaxParallel:   1,
		RegisteredAt:  now - 360000,
		LastHeartbeat: now - 360000, // 6 min ago
		Status:        "stale",
	})
	if err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	// Create a claim
	ok, _, claimErr := store.ClaimTask(ctx, "proj1", "task2", "runner-stale-old", 30*time.Minute)
	if claimErr != nil {
		t.Fatalf("ClaimTask failed: %v", claimErr)
	}
	if !ok {
		t.Fatal("ClaimTask should have succeeded")
	}

	// Run lifecycle sweep
	svc.RunLifecycleSweep(ctx)

	// Should transition to offline
	row, err := store.GetRunner(ctx, "runner-stale-old")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if row.Status != "offline" {
		t.Errorf("expected status 'offline', got %q", row.Status)
	}

	// Claim should be released
	claim, err := store.GetClaim(ctx, "proj1", "task2")
	if err != nil {
		t.Fatalf("GetClaim failed: %v", err)
	}
	if claim != nil {
		t.Error("expected claim to be released for offline runner")
	}
}

func TestLifecycleManager_OnlineRunnerUnchanged(t *testing.T) {
	svc, store := newTestRunnerRegistryService(t)
	ctx := context.Background()

	// Register a runner with fresh heartbeat (should stay online)
	_, err := svc.Register(ctx, types.RunnerRegistration{
		RunnerID:    "runner-fresh",
		Hostname:    "host-a",
		MaxParallel: 1,
	})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Create a claim
	ok, _, claimErr := store.ClaimTask(ctx, "proj1", "task3", "runner-fresh", 30*time.Minute)
	if claimErr != nil {
		t.Fatalf("ClaimTask failed: %v", claimErr)
	}
	if !ok {
		t.Fatal("ClaimTask should have succeeded")
	}

	// Run lifecycle sweep
	svc.RunLifecycleSweep(ctx)

	// Status should still be online
	row, err := store.GetRunner(ctx, "runner-fresh")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if row.Status != "online" {
		t.Errorf("expected status 'online', got %q", row.Status)
	}

	// Claim should still exist
	claim, err := store.GetClaim(ctx, "proj1", "task3")
	if err != nil {
		t.Fatalf("GetClaim failed: %v", err)
	}
	if claim == nil {
		t.Error("expected claim to still exist for online runner")
	}
}

func TestLifecycleManager_MixedRunners(t *testing.T) {
	svc, store := newTestRunnerRegistryService(t)
	ctx := context.Background()

	now := time.Now().UnixMilli()

	// Fresh runner (online — should stay)
	err := store.UpsertRunner(ctx, &storage.RunnerRow{
		RunnerID:      "runner-online",
		Hostname:      "host-a",
		Labels:        map[string]string{},
		Executors:     []string{},
		MaxParallel:   1,
		RegisteredAt:  now,
		LastHeartbeat: now,
		Status:        "online",
	})
	if err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	// 2 min old heartbeat (should become stale)
	err = store.UpsertRunner(ctx, &storage.RunnerRow{
		RunnerID:      "runner-becoming-stale",
		Hostname:      "host-b",
		Labels:        map[string]string{},
		Executors:     []string{},
		MaxParallel:   1,
		RegisteredAt:  now - 120000,
		LastHeartbeat: now - 120000,
		Status:        "online",
	})
	if err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	// 10 min old heartbeat (should become offline)
	err = store.UpsertRunner(ctx, &storage.RunnerRow{
		RunnerID:      "runner-becoming-offline",
		Hostname:      "host-c",
		Labels:        map[string]string{},
		Executors:     []string{},
		MaxParallel:   1,
		RegisteredAt:  now - 600000,
		LastHeartbeat: now - 600000,
		Status:        "online",
	})
	if err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	// Run one lifecycle sweep
	svc.RunLifecycleSweep(ctx)

	// Verify each runner's state
	tests := []struct {
		runnerID       string
		expectedStatus string
	}{
		{"runner-online", "online"},
		{"runner-becoming-stale", "stale"},
		{"runner-becoming-offline", "offline"},
	}

	for _, tt := range tests {
		row, err := store.GetRunner(ctx, tt.runnerID)
		if err != nil {
			t.Fatalf("GetRunner(%s) failed: %v", tt.runnerID, err)
		}
		if row.Status != tt.expectedStatus {
			t.Errorf("runner %s: expected status %q, got %q", tt.runnerID, tt.expectedStatus, row.Status)
		}
	}
}

func TestLifecycleManager_BackgroundGoroutine(t *testing.T) {
	svc, store := newTestRunnerRegistryService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	now := time.Now().UnixMilli()

	// Register a stale runner
	err := store.UpsertRunner(ctx, &storage.RunnerRow{
		RunnerID:      "runner-bg",
		Hostname:      "host-a",
		Labels:        map[string]string{},
		Executors:     []string{},
		MaxParallel:   1,
		RegisteredAt:  now - 120000,
		LastHeartbeat: now - 120000, // 2 min ago
		Status:        "online",
	})
	if err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	// Start lifecycle manager with a very short interval
	svc.StartLifecycleManager(ctx, 50*time.Millisecond)

	// Wait for at least one sweep
	time.Sleep(150 * time.Millisecond)

	// Verify runner was transitioned
	row, err := store.GetRunner(ctx, "runner-bg")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if row.Status != "stale" {
		t.Errorf("expected status 'stale' after background sweep, got %q", row.Status)
	}

	// Cancel context and verify goroutine stops (no panics)
	cancel()
	time.Sleep(100 * time.Millisecond)
}

func TestLifecycleManager_AlreadyOfflineNotReprocessed(t *testing.T) {
	svc, store := newTestRunnerRegistryService(t)
	ctx := context.Background()

	now := time.Now().UnixMilli()

	// Runner already offline — should not be reprocessed
	err := store.UpsertRunner(ctx, &storage.RunnerRow{
		RunnerID:      "runner-already-offline",
		Hostname:      "host-a",
		Labels:        map[string]string{},
		Executors:     []string{},
		MaxParallel:   1,
		RegisteredAt:  now - 600000,
		LastHeartbeat: now - 600000,
		Status:        "offline",
	})
	if err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	// Create a claim (simulating leftover — normally wouldn't exist, but tests robustness)
	ok, _, claimErr := store.ClaimTask(ctx, "proj1", "task-leftover", "runner-already-offline", 30*time.Minute)
	if claimErr != nil {
		t.Fatalf("ClaimTask failed: %v", claimErr)
	}
	if !ok {
		t.Fatal("ClaimTask should have succeeded")
	}

	// Run lifecycle sweep
	svc.RunLifecycleSweep(ctx)

	// Status should remain offline
	row, err := store.GetRunner(ctx, "runner-already-offline")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if row.Status != "offline" {
		t.Errorf("expected status 'offline', got %q", row.Status)
	}

	// Claim should still exist — we don't re-release for already-offline runners
	// (that was handled on the transition to offline)
	claim, err := store.GetClaim(ctx, "proj1", "task-leftover")
	if err != nil {
		t.Fatalf("GetClaim failed: %v", err)
	}
	if claim == nil {
		t.Error("expected claim to still exist for already-offline runner (no re-release)")
	}
}

func TestRunnerRegistry_ListRunners_ComputedStatus(t *testing.T) {
	svc, store := newTestRunnerRegistryService(t)
	ctx := context.Background()

	now := time.Now().UnixMilli()

	// Fresh runner (online)
	_ = store.UpsertRunner(ctx, &storage.RunnerRow{
		RunnerID:      "runner-online",
		Hostname:      "host-a",
		Labels:        map[string]string{},
		Executors:     []string{},
		MaxParallel:   1,
		RegisteredAt:  now,
		LastHeartbeat: now,
		Status:        "online",
	})

	// 2-minute-old heartbeat (stale)
	_ = store.UpsertRunner(ctx, &storage.RunnerRow{
		RunnerID:      "runner-stale",
		Hostname:      "host-b",
		Labels:        map[string]string{},
		Executors:     []string{},
		MaxParallel:   1,
		RegisteredAt:  now - 120000,
		LastHeartbeat: now - 120000,
		Status:        "online",
	})

	// 10-minute-old heartbeat (offline)
	_ = store.UpsertRunner(ctx, &storage.RunnerRow{
		RunnerID:      "runner-offline",
		Hostname:      "host-c",
		Labels:        map[string]string{},
		Executors:     []string{},
		MaxParallel:   1,
		RegisteredAt:  now - 600000,
		LastHeartbeat: now - 600000,
		Status:        "online",
	})

	resp, err := svc.ListRunners(ctx)
	if err != nil {
		t.Fatalf("ListRunners failed: %v", err)
	}
	if resp.Total != 3 {
		t.Fatalf("expected 3 runners, got %d", resp.Total)
	}

	statusMap := make(map[string]types.RunnerStatus)
	for _, r := range resp.Runners {
		statusMap[r.RunnerID] = r.Status
	}

	if statusMap["runner-online"] != types.RunnerStatusOnline {
		t.Errorf("expected runner-online to be online, got %s", statusMap["runner-online"])
	}
	if statusMap["runner-stale"] != types.RunnerStatusStale {
		t.Errorf("expected runner-stale to be stale, got %s", statusMap["runner-stale"])
	}
	if statusMap["runner-offline"] != types.RunnerStatusOffline {
		t.Errorf("expected runner-offline to be offline, got %s", statusMap["runner-offline"])
	}
}

// ---------------------------------------------------------------------------
// SSE Events — Lifecycle Sweep
// ---------------------------------------------------------------------------

func newTestRunnerRegistryServiceWithHub(t *testing.T) (*RunnerRegistryServiceImpl, *storage.StorageLayer, *realtime.Hub) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}

	store, err := storage.NewWithDB(db)
	if err != nil {
		t.Fatalf("NewWithDB failed: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	hub := realtime.NewHub()
	svc := NewRunnerRegistryService(store)
	svc.SetHub(hub)
	return svc, store, hub
}

func TestLifecycleSweep_EmitsOfflineSSEEvent(t *testing.T) {
	svc, store, hub := newTestRunnerRegistryServiceWithHub(t)
	ctx := context.Background()

	// Subscribe to runner lifecycle events
	ch, unsub := hub.Subscribe(realtime.RunnerLifecycleTopic)
	defer unsub()

	now := time.Now().UnixMilli()

	// Register a runner with heartbeat 10 minutes ago (>5min = offline)
	err := store.UpsertRunner(ctx, &storage.RunnerRow{
		RunnerID:      "runner-dead",
		Hostname:      "host-dead",
		Labels:        map[string]string{},
		Executors:     []string{},
		MaxParallel:   1,
		RegisteredAt:  now - 600000,
		LastHeartbeat: now - 600000,
		Status:        "online",
	})
	if err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	// Run lifecycle sweep
	svc.RunLifecycleSweep(ctx)

	// Should receive a runner_offline event
	select {
	case msg := <-ch:
		if msg.Event != "runner_offline" {
			t.Errorf("event = %q, want %q", msg.Event, "runner_offline")
		}
		data, ok := msg.Data.(types.SSERunnerOfflineData)
		if !ok {
			t.Fatalf("data type = %T, want types.SSERunnerOfflineData", msg.Data)
		}
		if data.RunnerID != "runner-dead" {
			t.Errorf("runnerId = %q, want %q", data.RunnerID, "runner-dead")
		}
		if data.Status != "offline" {
			t.Errorf("status = %q, want %q", data.Status, "offline")
		}
		if data.Hostname != "host-dead" {
			t.Errorf("hostname = %q, want %q", data.Hostname, "host-dead")
		}
		if data.Timestamp == "" {
			t.Error("timestamp should not be empty")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runner_offline event")
	}
}

func TestLifecycleSweep_EmitsStaleSSEEvent(t *testing.T) {
	svc, store, hub := newTestRunnerRegistryServiceWithHub(t)
	ctx := context.Background()

	// Subscribe to runner lifecycle events
	ch, unsub := hub.Subscribe(realtime.RunnerLifecycleTopic)
	defer unsub()

	now := time.Now().UnixMilli()

	// Register a runner with heartbeat 2 minutes ago (>90s = stale)
	err := store.UpsertRunner(ctx, &storage.RunnerRow{
		RunnerID:      "runner-slow",
		Hostname:      "host-slow",
		Labels:        map[string]string{},
		Executors:     []string{},
		MaxParallel:   1,
		RegisteredAt:  now - 120000,
		LastHeartbeat: now - 120000,
		Status:        "online",
	})
	if err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	// Run lifecycle sweep
	svc.RunLifecycleSweep(ctx)

	// Should receive a runner_offline event with stale status
	select {
	case msg := <-ch:
		if msg.Event != "runner_offline" {
			t.Errorf("event = %q, want %q", msg.Event, "runner_offline")
		}
		data, ok := msg.Data.(types.SSERunnerOfflineData)
		if !ok {
			t.Fatalf("data type = %T, want types.SSERunnerOfflineData", msg.Data)
		}
		if data.RunnerID != "runner-slow" {
			t.Errorf("runnerId = %q, want %q", data.RunnerID, "runner-slow")
		}
		if data.Status != "stale" {
			t.Errorf("status = %q, want %q", data.Status, "stale")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runner_offline (stale) event")
	}
}

func TestLifecycleSweep_NoEventForAlreadyOfflineRunner(t *testing.T) {
	svc, store, hub := newTestRunnerRegistryServiceWithHub(t)
	ctx := context.Background()

	// Subscribe to runner lifecycle events
	ch, unsub := hub.Subscribe(realtime.RunnerLifecycleTopic)
	defer unsub()

	now := time.Now().UnixMilli()

	// Register a runner already marked offline
	err := store.UpsertRunner(ctx, &storage.RunnerRow{
		RunnerID:      "runner-already-offline",
		Hostname:      "host-a",
		Labels:        map[string]string{},
		Executors:     []string{},
		MaxParallel:   1,
		RegisteredAt:  now - 600000,
		LastHeartbeat: now - 600000,
		Status:        "offline",
	})
	if err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	// Run lifecycle sweep
	svc.RunLifecycleSweep(ctx)

	// Should NOT receive any event (already offline)
	select {
	case msg := <-ch:
		t.Fatalf("should not receive event for already-offline runner, got: %+v", msg)
	case <-time.After(100 * time.Millisecond):
		// Expected — no event
	}
}

func TestLifecycleSweep_NoEventForOnlineRunner(t *testing.T) {
	svc, _, hub := newTestRunnerRegistryServiceWithHub(t)
	ctx := context.Background()

	// Subscribe to runner lifecycle events
	ch, unsub := hub.Subscribe(realtime.RunnerLifecycleTopic)
	defer unsub()

	// Register a fresh runner (heartbeat is now)
	_, err := svc.Register(ctx, types.RunnerRegistration{
		RunnerID: "runner-alive",
		Hostname: "host-alive",
	})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Run lifecycle sweep
	svc.RunLifecycleSweep(ctx)

	// Should NOT receive any event (runner is online)
	select {
	case msg := <-ch:
		t.Fatalf("should not receive event for online runner, got: %+v", msg)
	case <-time.After(100 * time.Millisecond):
		// Expected — no event
	}
}
