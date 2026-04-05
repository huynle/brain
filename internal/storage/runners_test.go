package storage

import (
	"context"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeRunner(id, hostname string) *RunnerRow {
	now := time.Now().UnixMilli()
	return &RunnerRow{
		RunnerID:      id,
		Hostname:      hostname,
		Labels:        map[string]string{"env": "test"},
		Executors:     []string{"opencode"},
		MaxParallel:   2,
		FeatureIDs:    "",
		RegisteredAt:  now,
		LastHeartbeat: now,
		Status:        "online",
	}
}

// ---------------------------------------------------------------------------
// UpsertRunner
// ---------------------------------------------------------------------------

func TestUpsertRunner_Insert(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	r := makeRunner("runner-1", "host-a")
	if err := s.UpsertRunner(ctx, r); err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	// Verify it was persisted
	got, err := s.GetRunner(ctx, "runner-1")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected runner, got nil")
	}
	if got.RunnerID != "runner-1" {
		t.Errorf("runner_id = %q, want %q", got.RunnerID, "runner-1")
	}
	if got.Hostname != "host-a" {
		t.Errorf("hostname = %q, want %q", got.Hostname, "host-a")
	}
	if got.MaxParallel != 2 {
		t.Errorf("max_parallel = %d, want %d", got.MaxParallel, 2)
	}
	if got.Status != "online" {
		t.Errorf("status = %q, want %q", got.Status, "online")
	}
}

func TestUpsertRunner_JSONFields(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	r := &RunnerRow{
		RunnerID:      "runner-json",
		Hostname:      "host-json",
		Labels:        map[string]string{"env": "prod", "region": "us-east-1"},
		Executors:     []string{"opencode", "bash"},
		MaxParallel:   4,
		FeatureIDs:    "feat-1,feat-2",
		RegisteredAt:  time.Now().UnixMilli(),
		LastHeartbeat: time.Now().UnixMilli(),
		Status:        "online",
	}

	if err := s.UpsertRunner(ctx, r); err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	got, err := s.GetRunner(ctx, "runner-json")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected runner, got nil")
	}

	// Labels roundtrip
	if len(got.Labels) != 2 {
		t.Errorf("labels length = %d, want 2", len(got.Labels))
	}
	if got.Labels["env"] != "prod" {
		t.Errorf("labels[env] = %q, want %q", got.Labels["env"], "prod")
	}
	if got.Labels["region"] != "us-east-1" {
		t.Errorf("labels[region] = %q, want %q", got.Labels["region"], "us-east-1")
	}

	// Executors roundtrip
	if len(got.Executors) != 2 {
		t.Errorf("executors length = %d, want 2", len(got.Executors))
	}
	if got.Executors[0] != "opencode" {
		t.Errorf("executors[0] = %q, want %q", got.Executors[0], "opencode")
	}
	if got.Executors[1] != "bash" {
		t.Errorf("executors[1] = %q, want %q", got.Executors[1], "bash")
	}

	if got.FeatureIDs != "feat-1,feat-2" {
		t.Errorf("feature_ids = %q, want %q", got.FeatureIDs, "feat-1,feat-2")
	}
}

func TestUpsertRunner_Replace(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	r1 := makeRunner("runner-1", "host-a")
	if err := s.UpsertRunner(ctx, r1); err != nil {
		t.Fatalf("first UpsertRunner failed: %v", err)
	}

	// Upsert with new hostname and labels
	r2 := makeRunner("runner-1", "host-b")
	r2.Labels = map[string]string{"env": "staging"}
	r2.MaxParallel = 8
	if err := s.UpsertRunner(ctx, r2); err != nil {
		t.Fatalf("second UpsertRunner failed: %v", err)
	}

	got, err := s.GetRunner(ctx, "runner-1")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected runner, got nil")
	}
	if got.Hostname != "host-b" {
		t.Errorf("hostname = %q, want %q (after upsert)", got.Hostname, "host-b")
	}
	if got.MaxParallel != 8 {
		t.Errorf("max_parallel = %d, want %d (after upsert)", got.MaxParallel, 8)
	}
	if got.Labels["env"] != "staging" {
		t.Errorf("labels[env] = %q, want %q (after upsert)", got.Labels["env"], "staging")
	}

	// Should still only be one runner
	all, err := s.ListRunners(ctx)
	if err != nil {
		t.Fatalf("ListRunners failed: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("expected 1 runner after upsert, got %d", len(all))
	}
}

func TestUpsertRunner_NilLabelsAndExecutors(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	r := &RunnerRow{
		RunnerID:      "runner-nil",
		Hostname:      "host-nil",
		Labels:        nil,
		Executors:     nil,
		MaxParallel:   1,
		RegisteredAt:  time.Now().UnixMilli(),
		LastHeartbeat: time.Now().UnixMilli(),
		Status:        "online",
	}

	if err := s.UpsertRunner(ctx, r); err != nil {
		t.Fatalf("UpsertRunner with nil fields failed: %v", err)
	}

	got, err := s.GetRunner(ctx, "runner-nil")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected runner, got nil")
	}

	// nil map marshals as "null", which should unmarshal back to nil
	// nil slice marshals as "null", same behavior
	// This is acceptable — callers should check for nil
}

// ---------------------------------------------------------------------------
// GetRunner
// ---------------------------------------------------------------------------

func TestGetRunner_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	got, err := s.GetRunner(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// ListRunners
// ---------------------------------------------------------------------------

func TestListRunners_Empty(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	runners, err := s.ListRunners(ctx)
	if err != nil {
		t.Fatalf("ListRunners failed: %v", err)
	}
	if len(runners) != 0 {
		t.Errorf("expected empty list, got %d runners", len(runners))
	}
}

func TestListRunners_Multiple(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	now := time.Now().UnixMilli()

	// Insert three runners with different registration times
	for i, id := range []string{"runner-1", "runner-2", "runner-3"} {
		r := makeRunner(id, "host-"+id)
		r.RegisteredAt = now + int64(i*1000) // staggered registration
		if err := s.UpsertRunner(ctx, r); err != nil {
			t.Fatalf("UpsertRunner(%s) failed: %v", id, err)
		}
	}

	runners, err := s.ListRunners(ctx)
	if err != nil {
		t.Fatalf("ListRunners failed: %v", err)
	}
	if len(runners) != 3 {
		t.Fatalf("expected 3 runners, got %d", len(runners))
	}

	// Ordered by registered_at DESC — newest first
	if runners[0].RunnerID != "runner-3" {
		t.Errorf("first runner = %q, want %q (newest first)", runners[0].RunnerID, "runner-3")
	}
	if runners[2].RunnerID != "runner-1" {
		t.Errorf("last runner = %q, want %q (oldest last)", runners[2].RunnerID, "runner-1")
	}
}

// ---------------------------------------------------------------------------
// ListRunnersByStatus
// ---------------------------------------------------------------------------

func TestListRunnersByStatus(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert one online and one offline runner
	r1 := makeRunner("runner-online", "host-1")
	r1.Status = "online"
	r2 := makeRunner("runner-offline", "host-2")
	r2.Status = "offline"

	if err := s.UpsertRunner(ctx, r1); err != nil {
		t.Fatalf("UpsertRunner(online) failed: %v", err)
	}
	if err := s.UpsertRunner(ctx, r2); err != nil {
		t.Fatalf("UpsertRunner(offline) failed: %v", err)
	}

	online, err := s.ListRunnersByStatus(ctx, "online")
	if err != nil {
		t.Fatalf("ListRunnersByStatus(online) failed: %v", err)
	}
	if len(online) != 1 {
		t.Fatalf("expected 1 online runner, got %d", len(online))
	}
	if online[0].RunnerID != "runner-online" {
		t.Errorf("online runner = %q, want %q", online[0].RunnerID, "runner-online")
	}

	offline, err := s.ListRunnersByStatus(ctx, "offline")
	if err != nil {
		t.Fatalf("ListRunnersByStatus(offline) failed: %v", err)
	}
	if len(offline) != 1 {
		t.Fatalf("expected 1 offline runner, got %d", len(offline))
	}
	if offline[0].RunnerID != "runner-offline" {
		t.Errorf("offline runner = %q, want %q", offline[0].RunnerID, "runner-offline")
	}

	// No runners with status "draining"
	draining, err := s.ListRunnersByStatus(ctx, "draining")
	if err != nil {
		t.Fatalf("ListRunnersByStatus(draining) failed: %v", err)
	}
	if len(draining) != 0 {
		t.Errorf("expected 0 draining runners, got %d", len(draining))
	}
}

// ---------------------------------------------------------------------------
// DeleteRunner
// ---------------------------------------------------------------------------

func TestDeleteRunner_Exists(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	r := makeRunner("runner-del", "host-del")
	if err := s.UpsertRunner(ctx, r); err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	deleted, err := s.DeleteRunner(ctx, "runner-del")
	if err != nil {
		t.Fatalf("DeleteRunner failed: %v", err)
	}
	if !deleted {
		t.Error("expected deleted=true")
	}

	// Verify it's gone
	got, err := s.GetRunner(ctx, "runner-del")
	if err != nil {
		t.Fatalf("GetRunner after delete failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after delete, got %+v", got)
	}
}

func TestDeleteRunner_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	deleted, err := s.DeleteRunner(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("DeleteRunner failed: %v", err)
	}
	if deleted {
		t.Error("expected deleted=false for nonexistent runner")
	}
}

// ---------------------------------------------------------------------------
// UpdateHeartbeat
// ---------------------------------------------------------------------------

func TestUpdateHeartbeat_Simple(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	r := makeRunner("runner-hb", "host-hb")
	r.LastHeartbeat = time.Now().Add(-1 * time.Hour).UnixMilli() // old heartbeat
	if err := s.UpsertRunner(ctx, r); err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	before := time.Now().UnixMilli()
	if err := s.UpdateHeartbeat(ctx, "runner-hb", 0, nil); err != nil {
		t.Fatalf("UpdateHeartbeat failed: %v", err)
	}
	after := time.Now().UnixMilli()

	got, err := s.GetRunner(ctx, "runner-hb")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if got.LastHeartbeat < before || got.LastHeartbeat > after {
		t.Errorf("last_heartbeat = %d, expected between %d and %d", got.LastHeartbeat, before, after)
	}
}

func TestUpdateHeartbeat_WithStats(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	r := makeRunner("runner-stats", "host-stats")
	if err := s.UpsertRunner(ctx, r); err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	stats := map[string]interface{}{
		"completed": 42,
		"failed":    3,
	}
	if err := s.UpdateHeartbeat(ctx, "runner-stats", 5, stats); err != nil {
		t.Fatalf("UpdateHeartbeat with stats failed: %v", err)
	}

	got, err := s.GetRunner(ctx, "runner-stats")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if got.Labels["_running_tasks"] != "5" {
		t.Errorf("_running_tasks = %q, want %q", got.Labels["_running_tasks"], "5")
	}
	if got.Labels["_stat_completed"] != "42" {
		t.Errorf("_stat_completed = %q, want %q", got.Labels["_stat_completed"], "42")
	}
	if got.Labels["_stat_failed"] != "3" {
		t.Errorf("_stat_failed = %q, want %q", got.Labels["_stat_failed"], "3")
	}
	// Original label should still be present
	if got.Labels["env"] != "test" {
		t.Errorf("labels[env] = %q, want %q (should be preserved)", got.Labels["env"], "test")
	}
}

func TestUpdateHeartbeat_RunnerNotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	err := s.UpdateHeartbeat(ctx, "nonexistent", 0, nil)
	if err == nil {
		t.Fatal("expected error for nonexistent runner")
	}
}

// ---------------------------------------------------------------------------
// UpdateAffinity
// ---------------------------------------------------------------------------

func TestUpdateAffinity_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	r := makeRunner("runner-aff", "host-aff")
	if err := s.UpsertRunner(ctx, r); err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	if err := s.UpdateAffinity(ctx, "runner-aff", []string{"feat-a", "feat-b"}); err != nil {
		t.Fatalf("UpdateAffinity failed: %v", err)
	}

	got, err := s.GetRunner(ctx, "runner-aff")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if got.FeatureIDs != "feat-a,feat-b" {
		t.Errorf("feature_ids = %q, want %q", got.FeatureIDs, "feat-a,feat-b")
	}
}

func TestUpdateAffinity_EmptyList(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	r := makeRunner("runner-aff2", "host-aff2")
	r.FeatureIDs = "feat-old"
	if err := s.UpsertRunner(ctx, r); err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	// Clear affinity
	if err := s.UpdateAffinity(ctx, "runner-aff2", []string{}); err != nil {
		t.Fatalf("UpdateAffinity(empty) failed: %v", err)
	}

	got, err := s.GetRunner(ctx, "runner-aff2")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if got.FeatureIDs != "" {
		t.Errorf("feature_ids = %q, want empty string", got.FeatureIDs)
	}
}

func TestUpdateAffinity_RunnerNotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	err := s.UpdateAffinity(ctx, "nonexistent", []string{"feat-a"})
	if err == nil {
		t.Fatal("expected error for nonexistent runner")
	}
}

// ---------------------------------------------------------------------------
// SetRunnerStatus
// ---------------------------------------------------------------------------

func TestSetRunnerStatus_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	r := makeRunner("runner-st", "host-st")
	if err := s.UpsertRunner(ctx, r); err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	if err := s.SetRunnerStatus(ctx, "runner-st", "draining"); err != nil {
		t.Fatalf("SetRunnerStatus failed: %v", err)
	}

	got, err := s.GetRunner(ctx, "runner-st")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if got.Status != "draining" {
		t.Errorf("status = %q, want %q", got.Status, "draining")
	}
}

func TestSetRunnerStatus_RunnerNotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	err := s.SetRunnerStatus(ctx, "nonexistent", "offline")
	if err == nil {
		t.Fatal("expected error for nonexistent runner")
	}
}

// ---------------------------------------------------------------------------
// ExpireStaleRunners
// ---------------------------------------------------------------------------

func TestExpireStaleRunners_MarksStale(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	now := time.Now().UnixMilli()

	// Fresh runner (heartbeat just now)
	fresh := makeRunner("runner-fresh", "host-fresh")
	fresh.LastHeartbeat = now
	if err := s.UpsertRunner(ctx, fresh); err != nil {
		t.Fatalf("UpsertRunner(fresh) failed: %v", err)
	}

	// Stale runner (heartbeat 2 hours ago)
	stale := makeRunner("runner-stale", "host-stale")
	stale.LastHeartbeat = now - (2 * time.Hour).Milliseconds()
	if err := s.UpsertRunner(ctx, stale); err != nil {
		t.Fatalf("UpsertRunner(stale) failed: %v", err)
	}

	// Already offline (should not be re-counted)
	alreadyOff := makeRunner("runner-off", "host-off")
	alreadyOff.Status = "offline"
	alreadyOff.LastHeartbeat = now - (3 * time.Hour).Milliseconds()
	if err := s.UpsertRunner(ctx, alreadyOff); err != nil {
		t.Fatalf("UpsertRunner(already offline) failed: %v", err)
	}

	// Expire runners with threshold of 1 hour
	count, err := s.ExpireStaleRunners(ctx, 1*time.Hour)
	if err != nil {
		t.Fatalf("ExpireStaleRunners failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expired count = %d, want 1 (only the stale online runner)", count)
	}

	// Verify fresh is still online
	gotFresh, _ := s.GetRunner(ctx, "runner-fresh")
	if gotFresh.Status != "online" {
		t.Errorf("fresh runner status = %q, want %q", gotFresh.Status, "online")
	}

	// Verify stale is now offline
	gotStale, _ := s.GetRunner(ctx, "runner-stale")
	if gotStale.Status != "offline" {
		t.Errorf("stale runner status = %q, want %q", gotStale.Status, "offline")
	}
}

func TestExpireStaleRunners_NoneExpired(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	r := makeRunner("runner-fresh", "host-fresh")
	if err := s.UpsertRunner(ctx, r); err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	count, err := s.ExpireStaleRunners(ctx, 1*time.Hour)
	if err != nil {
		t.Fatalf("ExpireStaleRunners failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expired count = %d, want 0", count)
	}
}
