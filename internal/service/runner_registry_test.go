package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/glebarez/go-sqlite"
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
