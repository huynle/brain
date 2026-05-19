package storage

import (
	"context"
	"sync"
	"testing"
)

func TestAssignFeatureIfEmpty_AssignsAndPersists(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	ok, existing, err := s.AssignFeatureIfEmpty(ctx, "proj1", "feat1", "runner-a", "auto", "active")
	if err != nil {
		t.Fatalf("AssignFeatureIfEmpty failed: %v", err)
	}
	if !ok {
		t.Fatal("expected assignment to succeed")
	}
	if existing != nil {
		t.Fatalf("expected nil existing assignment, got %+v", existing)
	}

	assignment, err := s.GetFeatureAssignment(ctx, "proj1", "feat1")
	if err != nil {
		t.Fatalf("GetFeatureAssignment failed: %v", err)
	}
	assertFeatureAssignment(t, assignment, "proj1", "feat1", "runner-a", "auto", "active")
	if assignment.AssignedAt == 0 {
		t.Fatal("assigned_at should be non-zero")
	}
	if assignment.UpdatedAt == 0 {
		t.Fatal("updated_at should be non-zero")
	}
}

func TestAssignFeatureIfEmpty_ExistingAssignmentBlocksDifferentRunner(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	ok, _, err := s.AssignFeatureIfEmpty(ctx, "proj1", "feat1", "runner-a", "auto", "active")
	if err != nil || !ok {
		t.Fatalf("setup assignment failed: ok=%v err=%v", ok, err)
	}

	ok, existing, err := s.AssignFeatureIfEmpty(ctx, "proj1", "feat1", "runner-b", "auto", "active")
	if err != nil {
		t.Fatalf("second AssignFeatureIfEmpty failed: %v", err)
	}
	if ok {
		t.Fatal("second runner should not assign occupied feature")
	}
	assertFeatureAssignment(t, existing, "proj1", "feat1", "runner-a", "auto", "active")
}

func TestAssignFeatureIfEmpty_SameFeatureDifferentProjects(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	ok1, _, err := s.AssignFeatureIfEmpty(ctx, "proj1", "feat1", "runner-a", "auto", "active")
	if err != nil {
		t.Fatalf("proj1 assignment failed: %v", err)
	}
	ok2, _, err := s.AssignFeatureIfEmpty(ctx, "proj2", "feat1", "runner-b", "auto", "active")
	if err != nil {
		t.Fatalf("proj2 assignment failed: %v", err)
	}
	if !ok1 || !ok2 {
		t.Fatalf("both project-scoped assignments should succeed: proj1=%v proj2=%v", ok1, ok2)
	}
}

func TestAssignFeatureIfEmpty_CompetingRunnersOneWinner(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	type result struct {
		runner   string
		ok       bool
		existing *FeatureAssignmentRow
		err      error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var wg sync.WaitGroup

	for _, runnerID := range []string{"runner-a", "runner-b"} {
		wg.Add(1)
		go func(runnerID string) {
			defer wg.Done()
			<-start
			ok, existing, err := s.AssignFeatureIfEmpty(ctx, "proj1", "feat1", runnerID, "auto", "active")
			results <- result{runner: runnerID, ok: ok, existing: existing, err: err}
		}(runnerID)
	}
	close(start)
	wg.Wait()
	close(results)

	winners := 0
	losers := 0
	var winner string
	for res := range results {
		if res.err != nil {
			t.Fatalf("runner %s failed: %v", res.runner, res.err)
		}
		if res.ok {
			winners++
			winner = res.runner
			if res.existing != nil {
				t.Fatalf("winner %s got existing assignment %+v", res.runner, res.existing)
			}
		} else {
			losers++
			if res.existing == nil {
				t.Fatalf("loser %s should receive existing assignment", res.runner)
			}
		}
	}
	if winners != 1 || losers != 1 {
		t.Fatalf("got winners=%d losers=%d, want 1 winner and 1 loser", winners, losers)
	}

	assignment, err := s.GetFeatureAssignment(ctx, "proj1", "feat1")
	if err != nil {
		t.Fatalf("GetFeatureAssignment failed: %v", err)
	}
	assertFeatureAssignment(t, assignment, "proj1", "feat1", winner, "auto", "active")
}

func TestForceAssignFeature_ReassignsExistingAssignment(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	initial, err := s.ForceAssignFeature(ctx, "proj1", "feat1", "runner-a", "auto", "active")
	if err != nil {
		t.Fatalf("initial ForceAssignFeature failed: %v", err)
	}
	reassigned, err := s.ForceAssignFeature(ctx, "proj1", "feat1", "runner-b", "manual", "active")
	if err != nil {
		t.Fatalf("reassign ForceAssignFeature failed: %v", err)
	}

	assertFeatureAssignment(t, reassigned, "proj1", "feat1", "runner-b", "manual", "active")
	if reassigned.AssignedAt == initial.AssignedAt {
		t.Error("assigned_at should change on forced reassignment")
	}
	if reassigned.UpdatedAt < reassigned.AssignedAt {
		t.Errorf("updated_at (%d) should be at or after assigned_at (%d)", reassigned.UpdatedAt, reassigned.AssignedAt)
	}
}

func TestClearFeatureAssignment_RemovesAssignment(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	_, err := s.ForceAssignFeature(ctx, "proj1", "feat1", "runner-a", "manual", "active")
	if err != nil {
		t.Fatalf("setup ForceAssignFeature failed: %v", err)
	}

	cleared, err := s.ClearFeatureAssignment(ctx, "proj1", "feat1")
	if err != nil {
		t.Fatalf("ClearFeatureAssignment failed: %v", err)
	}
	if !cleared {
		t.Fatal("expected clear to report true")
	}
	assignment, err := s.GetFeatureAssignment(ctx, "proj1", "feat1")
	if err != nil {
		t.Fatalf("GetFeatureAssignment failed: %v", err)
	}
	if assignment != nil {
		t.Fatalf("expected nil assignment after clear, got %+v", assignment)
	}
}

func TestClearFeatureAssignment_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	cleared, err := s.ClearFeatureAssignment(ctx, "proj1", "missing")
	if err != nil {
		t.Fatalf("ClearFeatureAssignment failed: %v", err)
	}
	if cleared {
		t.Fatal("expected clear of missing assignment to report false")
	}
}

func TestListFeatureAssignmentsByRunner(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	seedFeatureAssignments(t, s, ctx)

	assignments, err := s.ListFeatureAssignmentsByRunner(ctx, "runner-a")
	if err != nil {
		t.Fatalf("ListFeatureAssignmentsByRunner failed: %v", err)
	}
	if len(assignments) != 2 {
		t.Fatalf("got %d assignments, want 2", len(assignments))
	}
	for _, assignment := range assignments {
		if assignment.RunnerID != "runner-a" {
			t.Errorf("runner_id = %q, want runner-a", assignment.RunnerID)
		}
	}
}

func TestListFeatureAssignmentsByProject(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	seedFeatureAssignments(t, s, ctx)

	assignments, err := s.ListFeatureAssignmentsByProject(ctx, "proj1")
	if err != nil {
		t.Fatalf("ListFeatureAssignmentsByProject failed: %v", err)
	}
	if len(assignments) != 2 {
		t.Fatalf("got %d assignments, want 2", len(assignments))
	}
	for _, assignment := range assignments {
		if assignment.ProjectID != "proj1" {
			t.Errorf("project_id = %q, want proj1", assignment.ProjectID)
		}
	}
}

func TestGetFeatureAssignment_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	assignment, err := s.GetFeatureAssignment(ctx, "proj1", "missing")
	if err != nil {
		t.Fatalf("GetFeatureAssignment failed: %v", err)
	}
	if assignment != nil {
		t.Fatalf("expected nil for missing assignment, got %+v", assignment)
	}
}

func seedFeatureAssignments(t *testing.T, s *StorageLayer, ctx context.Context) {
	t.Helper()
	seeds := []struct {
		projectID string
		featureID string
		runnerID  string
	}{
		{"proj1", "feat1", "runner-a"},
		{"proj1", "feat2", "runner-b"},
		{"proj2", "feat3", "runner-a"},
	}
	for _, seed := range seeds {
		_, err := s.ForceAssignFeature(ctx, seed.projectID, seed.featureID, seed.runnerID, "manual", "active")
		if err != nil {
			t.Fatalf("seed assignment %+v failed: %v", seed, err)
		}
	}
}

func assertFeatureAssignment(t *testing.T, got *FeatureAssignmentRow, projectID, featureID, runnerID, source, status string) {
	t.Helper()
	if got == nil {
		t.Fatal("expected assignment, got nil")
	}
	if got.ProjectID != projectID {
		t.Errorf("project_id = %q, want %q", got.ProjectID, projectID)
	}
	if got.FeatureID != featureID {
		t.Errorf("feature_id = %q, want %q", got.FeatureID, featureID)
	}
	if got.RunnerID != runnerID {
		t.Errorf("runner_id = %q, want %q", got.RunnerID, runnerID)
	}
	if got.Source != source {
		t.Errorf("source = %q, want %q", got.Source, source)
	}
	if got.Status != status {
		t.Errorf("status = %q, want %q", got.Status, status)
	}
}
