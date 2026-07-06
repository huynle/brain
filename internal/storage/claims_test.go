package storage

import (
	"context"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// ClaimTask
// ---------------------------------------------------------------------------

func TestClaimTask_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	ok, existing, err := s.ClaimTask(ctx, "proj1", "task1", "runner-a", 30*time.Second)
	if err != nil {
		t.Fatalf("ClaimTask failed: %v", err)
	}
	if !ok {
		t.Fatal("expected claim to succeed")
	}
	if existing != nil {
		t.Errorf("expected nil existing claim, got %+v", existing)
	}

	// Verify claim was persisted
	claim, err := s.GetClaim(ctx, "proj1", "task1")
	if err != nil {
		t.Fatalf("GetClaim failed: %v", err)
	}
	if claim == nil {
		t.Fatal("expected claim, got nil")
	}
	if claim.RunnerID != "runner-a" {
		t.Errorf("runner_id = %q, want %q", claim.RunnerID, "runner-a")
	}
	if claim.ProjectID != "proj1" {
		t.Errorf("project_id = %q, want %q", claim.ProjectID, "proj1")
	}
	if claim.TaskID != "task1" {
		t.Errorf("task_id = %q, want %q", claim.TaskID, "task1")
	}
}

func TestClaimTask_SameRunnerReClaim(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// First claim
	ok, _, err := s.ClaimTask(ctx, "proj1", "task1", "runner-a", 30*time.Second)
	if err != nil {
		t.Fatalf("first ClaimTask failed: %v", err)
	}
	if !ok {
		t.Fatal("first claim should succeed")
	}

	// Same runner re-claims — should succeed (extends lease)
	ok, _, err = s.ClaimTask(ctx, "proj1", "task1", "runner-a", 60*time.Second)
	if err != nil {
		t.Fatalf("re-claim failed: %v", err)
	}
	if !ok {
		t.Fatal("re-claim by same runner should succeed")
	}
}

func TestClaimTask_DifferentRunnerBlocked(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Runner A claims
	ok, _, err := s.ClaimTask(ctx, "proj1", "task1", "runner-a", 30*time.Second)
	if err != nil {
		t.Fatalf("runner-a claim failed: %v", err)
	}
	if !ok {
		t.Fatal("runner-a claim should succeed")
	}

	// Runner B tries to claim — should fail
	ok, existing, err := s.ClaimTask(ctx, "proj1", "task1", "runner-b", 30*time.Second)
	if err != nil {
		t.Fatalf("runner-b claim failed with error: %v", err)
	}
	if ok {
		t.Fatal("runner-b claim should fail (task held by runner-a)")
	}
	if existing == nil {
		t.Fatal("expected existing claim info")
	}
	if existing.RunnerID != "runner-a" {
		t.Errorf("existing runner = %q, want %q", existing.RunnerID, "runner-a")
	}
}

func TestClaimTask_ExpiredClaimTakeover(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Runner A claims with very short lease (already expired)
	// Insert directly with past expiry to simulate expired claim
	now := time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO task_claims (project_id, task_id, runner_id, claimed_at, expires_at) VALUES (?, ?, ?, ?, ?)",
		"proj1", "task1", "runner-a", now-10000, now-5000,
	)
	if err != nil {
		t.Fatalf("insert expired claim: %v", err)
	}

	// Runner B claims — should succeed because claim is expired
	ok, _, err := s.ClaimTask(ctx, "proj1", "task1", "runner-b", 30*time.Second)
	if err != nil {
		t.Fatalf("runner-b takeover failed: %v", err)
	}
	if !ok {
		t.Fatal("runner-b should take over expired claim")
	}

	// Verify runner-b now owns the claim
	claim, err := s.GetClaim(ctx, "proj1", "task1")
	if err != nil {
		t.Fatalf("GetClaim failed: %v", err)
	}
	if claim.RunnerID != "runner-b" {
		t.Errorf("runner_id = %q, want %q", claim.RunnerID, "runner-b")
	}
}

func TestClaimTask_DifferentProjects(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Same task ID in different projects should both succeed
	ok1, _, err := s.ClaimTask(ctx, "proj1", "task1", "runner-a", 30*time.Second)
	if err != nil {
		t.Fatalf("proj1 claim failed: %v", err)
	}
	ok2, _, err := s.ClaimTask(ctx, "proj2", "task1", "runner-b", 30*time.Second)
	if err != nil {
		t.Fatalf("proj2 claim failed: %v", err)
	}

	if !ok1 || !ok2 {
		t.Errorf("both claims should succeed: proj1=%v, proj2=%v", ok1, ok2)
	}
}

// ---------------------------------------------------------------------------
// ReleaseClaim
// ---------------------------------------------------------------------------

func TestReleaseClaim_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Claim first
	ok, _, err := s.ClaimTask(ctx, "proj1", "task1", "runner-a", 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("setup claim failed: err=%v ok=%v", err, ok)
	}

	// Release by the owner
	released, err := s.ReleaseClaim(ctx, "proj1", "task1", "runner-a")
	if err != nil {
		t.Fatalf("ReleaseClaim failed: %v", err)
	}
	if !released {
		t.Fatal("expected release to succeed")
	}

	// Verify claim is gone
	claim, err := s.GetClaim(ctx, "proj1", "task1")
	if err != nil {
		t.Fatalf("GetClaim failed: %v", err)
	}
	if claim != nil {
		t.Errorf("expected nil claim after release, got %+v", claim)
	}
}

func TestReleaseClaim_WrongRunner(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Claim by runner-a
	ok, _, err := s.ClaimTask(ctx, "proj1", "task1", "runner-a", 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("setup claim failed: err=%v ok=%v", err, ok)
	}

	// Runner-b tries to release — should fail
	released, err := s.ReleaseClaim(ctx, "proj1", "task1", "runner-b")
	if err != nil {
		t.Fatalf("ReleaseClaim failed: %v", err)
	}
	if released {
		t.Fatal("release by wrong runner should return false")
	}

	// Claim should still exist
	claim, err := s.GetClaim(ctx, "proj1", "task1")
	if err != nil {
		t.Fatalf("GetClaim failed: %v", err)
	}
	if claim == nil {
		t.Fatal("claim should still exist after failed release")
	}
	if claim.RunnerID != "runner-a" {
		t.Errorf("runner_id = %q, want %q", claim.RunnerID, "runner-a")
	}
}

func TestReleaseClaim_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	released, err := s.ReleaseClaim(ctx, "proj1", "nonexistent", "runner-a")
	if err != nil {
		t.Fatalf("ReleaseClaim failed: %v", err)
	}
	if released {
		t.Fatal("release of nonexistent claim should return false")
	}
}

// ---------------------------------------------------------------------------
// GetClaim
// ---------------------------------------------------------------------------

func TestGetClaim_Exists(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	ok, _, err := s.ClaimTask(ctx, "proj1", "task1", "runner-a", 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("setup claim failed: err=%v ok=%v", err, ok)
	}

	claim, err := s.GetClaim(ctx, "proj1", "task1")
	if err != nil {
		t.Fatalf("GetClaim failed: %v", err)
	}
	if claim == nil {
		t.Fatal("expected claim, got nil")
	}
	if claim.RunnerID != "runner-a" {
		t.Errorf("runner_id = %q, want %q", claim.RunnerID, "runner-a")
	}
	if claim.ClaimedAt == 0 {
		t.Error("claimed_at should be non-zero")
	}
	if claim.ExpiresAt == 0 {
		t.Error("expires_at should be non-zero")
	}
	if claim.ExpiresAt <= claim.ClaimedAt {
		t.Errorf("expires_at (%d) should be after claimed_at (%d)", claim.ExpiresAt, claim.ClaimedAt)
	}
}

func TestGetClaim_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	claim, err := s.GetClaim(ctx, "proj1", "nonexistent")
	if err != nil {
		t.Fatalf("GetClaim failed: %v", err)
	}
	if claim != nil {
		t.Errorf("expected nil for nonexistent claim, got %+v", claim)
	}
}

// ---------------------------------------------------------------------------
// GetClaimsByRunner
// ---------------------------------------------------------------------------

func TestGetClaimsByRunner_MultipleClaims(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Runner-a claims multiple tasks
	for _, taskID := range []string{"task1", "task2", "task3"} {
		ok, _, err := s.ClaimTask(ctx, "proj1", taskID, "runner-a", 30*time.Second)
		if err != nil || !ok {
			t.Fatalf("claim %s failed: err=%v ok=%v", taskID, err, ok)
		}
	}

	// Runner-b claims one task
	ok, _, err := s.ClaimTask(ctx, "proj1", "task4", "runner-b", 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("runner-b claim failed: err=%v ok=%v", err, ok)
	}

	// Get claims for runner-a
	claims, err := s.GetClaimsByRunner(ctx, "runner-a")
	if err != nil {
		t.Fatalf("GetClaimsByRunner failed: %v", err)
	}
	if len(claims) != 3 {
		t.Fatalf("got %d claims, want 3", len(claims))
	}
	for _, c := range claims {
		if c.RunnerID != "runner-a" {
			t.Errorf("claim runner_id = %q, want %q", c.RunnerID, "runner-a")
		}
	}
}

func TestGetClaimsByRunner_Empty(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	claims, err := s.GetClaimsByRunner(ctx, "nonexistent-runner")
	if err != nil {
		t.Fatalf("GetClaimsByRunner failed: %v", err)
	}
	if claims != nil {
		t.Errorf("expected nil for runner with no claims, got %d claims", len(claims))
	}
}

// ---------------------------------------------------------------------------
// ExpireStaleClaims
// ---------------------------------------------------------------------------

func TestExpireStaleClaims_RemovesExpired(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	now := time.Now().UnixMilli()

	// Insert expired claims directly
	for i, taskID := range []string{"task1", "task2"} {
		_, err := s.db.ExecContext(ctx,
			"INSERT INTO task_claims (project_id, task_id, runner_id, claimed_at, expires_at) VALUES (?, ?, ?, ?, ?)",
			"proj1", taskID, "runner-a", now-20000, now-10000+int64(i),
		)
		if err != nil {
			t.Fatalf("insert expired claim %s: %v", taskID, err)
		}
	}

	// Insert a valid (non-expired) claim
	ok, _, err := s.ClaimTask(ctx, "proj1", "task3", "runner-b", 5*time.Minute)
	if err != nil || !ok {
		t.Fatalf("setup valid claim failed: err=%v ok=%v", err, ok)
	}

	// Expire stale
	count, err := s.ExpireStaleClaims(ctx)
	if err != nil {
		t.Fatalf("ExpireStaleClaims failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expired count = %d, want 2", count)
	}

	// Valid claim should still exist
	claim, err := s.GetClaim(ctx, "proj1", "task3")
	if err != nil {
		t.Fatalf("GetClaim failed: %v", err)
	}
	if claim == nil {
		t.Fatal("valid claim should not be expired")
	}
}

func TestExpireStaleClaims_NoneExpired(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert a valid claim
	ok, _, err := s.ClaimTask(ctx, "proj1", "task1", "runner-a", 5*time.Minute)
	if err != nil || !ok {
		t.Fatalf("setup claim failed: err=%v ok=%v", err, ok)
	}

	count, err := s.ExpireStaleClaims(ctx)
	if err != nil {
		t.Fatalf("ExpireStaleClaims failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expired count = %d, want 0", count)
	}
}

// ---------------------------------------------------------------------------
// ReleaseAllByRunner
// ---------------------------------------------------------------------------

func TestReleaseAllByRunner_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Runner-a claims 3 tasks
	for _, taskID := range []string{"task1", "task2", "task3"} {
		ok, _, err := s.ClaimTask(ctx, "proj1", taskID, "runner-a", 30*time.Second)
		if err != nil || !ok {
			t.Fatalf("claim %s failed: err=%v ok=%v", taskID, err, ok)
		}
	}

	// Runner-b claims 1 task
	ok, _, err := s.ClaimTask(ctx, "proj1", "task4", "runner-b", 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("runner-b claim failed: err=%v ok=%v", err, ok)
	}

	// Release all by runner-a
	count, err := s.ReleaseAllByRunner(ctx, "runner-a")
	if err != nil {
		t.Fatalf("ReleaseAllByRunner failed: %v", err)
	}
	if count != 3 {
		t.Errorf("released count = %d, want 3", count)
	}

	// Runner-a should have no claims
	claims, err := s.GetClaimsByRunner(ctx, "runner-a")
	if err != nil {
		t.Fatalf("GetClaimsByRunner failed: %v", err)
	}
	if len(claims) != 0 {
		t.Errorf("runner-a still has %d claims", len(claims))
	}

	// Runner-b should still have their claim
	claim, err := s.GetClaim(ctx, "proj1", "task4")
	if err != nil {
		t.Fatalf("GetClaim failed: %v", err)
	}
	if claim == nil || claim.RunnerID != "runner-b" {
		t.Error("runner-b claim should be unaffected")
	}
}

func TestReleaseAllByRunner_NoClaims(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	count, err := s.ReleaseAllByRunner(ctx, "nonexistent-runner")
	if err != nil {
		t.Fatalf("ReleaseAllByRunner failed: %v", err)
	}
	if count != 0 {
		t.Errorf("released count = %d, want 0", count)
	}
}

// ---------------------------------------------------------------------------
// RenewClaim
// ---------------------------------------------------------------------------

func TestRenewClaim_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Claim task
	ok, _, err := s.ClaimTask(ctx, "proj1", "task1", "runner-a", 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("setup claim failed: err=%v ok=%v", err, ok)
	}

	// Record original expiry
	original, _ := s.GetClaim(ctx, "proj1", "task1")
	originalExpiry := original.ExpiresAt

	// Renew with new expiry
	newExpiry := time.Now().Add(5 * time.Minute)
	err = s.RenewClaim(ctx, "proj1", "task1", "runner-a", newExpiry)
	if err != nil {
		t.Fatalf("RenewClaim failed: %v", err)
	}

	// Verify expiry was updated
	renewed, _ := s.GetClaim(ctx, "proj1", "task1")
	if renewed.ExpiresAt == originalExpiry {
		t.Error("expires_at should have changed")
	}
	if renewed.ExpiresAt != newExpiry.UnixMilli() {
		t.Errorf("expires_at = %d, want %d", renewed.ExpiresAt, newExpiry.UnixMilli())
	}
}

func TestRenewClaim_WrongRunner(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Claim by runner-a
	ok, _, err := s.ClaimTask(ctx, "proj1", "task1", "runner-a", 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("setup claim failed: err=%v ok=%v", err, ok)
	}

	// Runner-b tries to renew — should fail
	err = s.RenewClaim(ctx, "proj1", "task1", "runner-b", time.Now().Add(5*time.Minute))
	if err == nil {
		t.Fatal("expected error for renew by wrong runner")
	}
}

func TestRenewClaim_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	err := s.RenewClaim(ctx, "proj1", "nonexistent", "runner-a", time.Now().Add(5*time.Minute))
	if err == nil {
		t.Fatal("expected error for nonexistent claim")
	}
}

// ---------------------------------------------------------------------------
// Table-driven: concurrent claims scenario
// ---------------------------------------------------------------------------

func TestClaimTask_TableDriven(t *testing.T) {
	tests := []struct {
		name            string
		setup           func(s *StorageLayer, ctx context.Context) // pre-existing state
		projectID       string
		taskID          string
		runnerID        string
		leaseDuration   time.Duration
		wantOK          bool
		wantExistingNil bool
	}{
		{
			name:            "new claim on empty table",
			setup:           nil,
			projectID:       "proj1",
			taskID:          "task1",
			runnerID:        "runner-a",
			leaseDuration:   30 * time.Second,
			wantOK:          true,
			wantExistingNil: true,
		},
		{
			name: "same runner re-claim",
			setup: func(s *StorageLayer, ctx context.Context) {
				s.ClaimTask(ctx, "proj1", "task1", "runner-a", 30*time.Second)
			},
			projectID:       "proj1",
			taskID:          "task1",
			runnerID:        "runner-a",
			leaseDuration:   60 * time.Second,
			wantOK:          true,
			wantExistingNil: true,
		},
		{
			name: "different runner blocked by active claim",
			setup: func(s *StorageLayer, ctx context.Context) {
				s.ClaimTask(ctx, "proj1", "task1", "runner-a", 30*time.Second)
			},
			projectID:       "proj1",
			taskID:          "task1",
			runnerID:        "runner-b",
			leaseDuration:   30 * time.Second,
			wantOK:          false,
			wantExistingNil: false,
		},
		{
			name: "takeover expired claim",
			setup: func(s *StorageLayer, ctx context.Context) {
				now := time.Now().UnixMilli()
				s.db.ExecContext(ctx,
					"INSERT INTO task_claims (project_id, task_id, runner_id, claimed_at, expires_at) VALUES (?, ?, ?, ?, ?)",
					"proj1", "task1", "runner-a", now-10000, now-5000,
				)
			},
			projectID:       "proj1",
			taskID:          "task1",
			runnerID:        "runner-b",
			leaseDuration:   30 * time.Second,
			wantOK:          true,
			wantExistingNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStorage(t)
			ctx := context.Background()

			if tt.setup != nil {
				tt.setup(s, ctx)
			}

			ok, existing, err := s.ClaimTask(ctx, tt.projectID, tt.taskID, tt.runnerID, tt.leaseDuration)
			if err != nil {
				t.Fatalf("ClaimTask failed: %v", err)
			}
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if tt.wantExistingNil && existing != nil {
				t.Errorf("expected nil existing, got %+v", existing)
			}
			if !tt.wantExistingNil && existing == nil {
				t.Error("expected non-nil existing claim")
			}
		})
	}
}
