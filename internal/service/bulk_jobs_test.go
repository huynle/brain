package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/types"
)

func bulkFixture(t *testing.T, count int) (*BulkJobService, context.Context, []string) {
	t.Helper()
	brain, store, dir := newTestBrainService(t)
	ctx := tenant.Into(context.Background(), tenant.Local)
	var paths []string
	for n := 0; n < count; n++ {
		p := fmt.Sprintf("projects/bulk/task/item-%04d.md", n)
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p), []byte("---\ntitle: Test\ntype: task\nstatus: completed\nfeature_id: group\n---\nBody\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := brain.indexer.IndexFile(p); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return NewBulkJobService(brain, store, NewTaskService(brain.config, store, brain.indexer), nil), ctx, paths
}
func drainBulk(t *testing.T, s *BulkJobService, ctx context.Context, id string) *types.BulkJob {
	t.Helper()
	for n := 0; n < 2000; n++ {
		worked, err := s.ProcessOne(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			break
		}
	}
	j, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return j
}
func TestBulkJobStableSnapshotAndIdempotency(t *testing.T) {
	s, ctx, paths := bulkFixture(t, 151)
	req := types.BulkJobRequest{RequestID: "snapshot-1", Operation: "archive", Paths: paths[:2], Filters: []types.BulkUpdateFilter{{Project: strPtr("bulk"), Type: strPtr("task"), Status: strPtr("completed")}}}
	j, err := s.Create(ctx, req, "test")
	if err != nil {
		t.Fatal(err)
	}
	if j.Total != 151 {
		t.Fatalf("total %d", j.Total)
	}
	// New matching work must not join an already accepted job.
	newer, err := s.brain.Save(ctx, types.CreateEntryRequest{Project: "bulk", Type: "task", Title: "Later", Content: "later", Status: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Create(ctx, req, "test")
	if err != nil || again.ID != j.ID {
		t.Fatalf("duplicate: %v %v", again, err)
	}
	req.Operation = "delete"
	if _, err = s.Create(ctx, req, "test"); !errors.Is(err, api.ErrConflict) {
		t.Fatalf("want conflict: %v", err)
	}
	result := drainBulk(t, s, ctx, j.ID)
	if result.Succeeded != 151 || result.State != "completed" {
		t.Fatalf("%+v", result)
	}
	later, err := s.brain.Recall(ctx, newer.Path)
	if err != nil || later.Status != "completed" {
		t.Fatalf("new target changed: %v %v", later, err)
	}
	items, err := s.Items(ctx, j.ID, 100, 100)
	if err != nil || len(items) != 51 {
		t.Fatalf("pagination %d %v", len(items), err)
	}
	for _, i := range items {
		if i.Attempts != 1 {
			t.Fatalf("repeated item %+v", i)
		}
	}
}
func TestBulkJobPauseRestartAndUncertainNeverReplayed(t *testing.T) {
	s, ctx, paths := bulkFixture(t, 4)
	j, err := s.Create(ctx, types.BulkJobRequest{RequestID: "restart-1", Operation: "delete", Paths: paths}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProcessOne(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ControlJob(ctx, j.ID, "pause"); err != nil {
		t.Fatal(err)
	}
	if worked, err := s.ProcessOne(ctx); err != nil || worked {
		t.Fatalf("paused work ran %v %v", worked, err)
	}
	if _, err = s.ControlJob(ctx, j.ID, "resume"); err != nil {
		t.Fatal(err)
	}
	if err = s.store.TransitionBulkJob(ctx, j.ID, "queued", "running"); err != nil {
		t.Fatal(err)
	}
	interrupted, err := s.store.ClaimBulkJobItem(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate process death after its durable claim, before an acknowledged write.
	if err = s.store.RecoverBulkJobs(ctx); err != nil {
		t.Fatal(err)
	}
	replacement := NewBulkJobService(s.brain, s.store, s.tasks, nil)
	result := drainBulk(t, replacement, ctx, j.ID)
	if result.State != "needs_attention" || result.Succeeded != 3 || result.Uncertain != 1 {
		t.Fatalf("%+v", result)
	}
	if _, err = s.brain.Recall(ctx, interrupted.Path); err != nil {
		t.Fatalf("interrupted item was replayed: %v", err)
	}
	if _, err = s.ControlJob(ctx, j.ID, "retry"); err != nil {
		t.Fatal(err)
	}
	result = drainBulk(t, s, ctx, j.ID)
	if result.Uncertain != 1 || result.Succeeded != 3 {
		t.Fatalf("retry replayed uncertain %+v", result)
	}
}
func TestBulkJobChangedEntryAndScopeFailClosed(t *testing.T) {
	s, ctx, paths := bulkFixture(t, 2)
	j, err := s.Create(ctx, types.BulkJobRequest{RequestID: "changed-1", Operation: "delete", Paths: paths}, "test")
	if err != nil {
		t.Fatal(err)
	}
	title := "Edited after submission"
	if _, err = s.brain.Update(ctx, paths[0], types.UpdateEntryRequest{Title: &title}); err != nil {
		t.Fatal(err)
	}
	result := drainBulk(t, s, ctx, j.ID)
	if result.Succeeded != 1 || result.Failed != 1 || result.Uncertain != 0 {
		t.Fatalf("%+v", result)
	}
	for _, bad := range []context.Context{context.Background(), tenant.Into(ctx, tenant.MustParse("foreign"))} {
		if _, err = s.List(bad); err == nil {
			t.Fatal("unscoped list succeeded")
		}
		if _, err = s.Get(bad, j.ID); err == nil {
			t.Fatal("unscoped read succeeded")
		}
		if _, err = s.ControlJob(bad, j.ID, "retry"); err == nil {
			t.Fatal("unscoped control succeeded")
		}
	}
	if _, err = s.brain.Recall(ctx, paths[0]); err != nil {
		t.Fatal("changed entry removed")
	}
}

type bulkClaimTasks struct {
	api.TaskService
	live bool
}

func (s *bulkClaimTasks) GetLiveClaim(context.Context, string, string) (*types.LiveClaim, error) {
	return &types.LiveClaim{Live: s.live, RunnerID: "live-runner"}, nil
}
func TestBulkJobClaimsAndSafeRetry(t *testing.T) {
	s, ctx, paths := bulkFixture(t, 2)
	claims := &bulkClaimTasks{live: true}
	s.tasks = claims
	j, err := s.Create(ctx, types.BulkJobRequest{RequestID: "claims-01", Operation: "delete", Paths: paths}, "test")
	if err != nil {
		t.Fatal(err)
	}
	result := drainBulk(t, s, ctx, j.ID)
	if result.Failed != 2 || result.Succeeded != 0 {
		t.Fatalf("%+v", result)
	}
	claims.live = false
	if _, err = s.ControlJob(ctx, j.ID, "retry"); err != nil {
		t.Fatal(err)
	}
	result = drainBulk(t, s, ctx, j.ID)
	if result.Succeeded != 2 || result.Failed != 0 {
		t.Fatalf("%+v", result)
	}
}

func TestBulkJobAdmissionWaitsForBootIndex(t *testing.T) {
	s, ctx, paths := bulkFixture(t, 1)
	ready := make(chan struct{})
	s.ready = ready
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	req := types.BulkJobRequest{RequestID: "boot-index-1", Operation: "delete", Paths: paths}
	if _, err := s.Create(cancelled, req, "test"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancelled admission: %v", err)
	}
	close(ready)
	j, err := s.Create(ctx, req, "test")
	if err != nil {
		t.Fatal(err)
	}
	if j.Total != 1 {
		t.Fatalf("%+v", j)
	}
}

type bulkCompletionEvents struct {
	api.EventService
	checks int
}

func (e *bulkCompletionEvents) CheckFeatureCompletion(context.Context, string, string, string) {
	e.checks++
}
func TestBulkJobCoalescesFeatureChecks(t *testing.T) {
	s, ctx, paths := bulkFixture(t, 6)
	status := "pending"
	for _, path := range paths {
		if _, err := s.brain.Update(ctx, path, types.UpdateEntryRequest{Status: &status}); err != nil {
			t.Fatal(err)
		}
	}
	events := &bulkCompletionEvents{}
	s.SetEventService(events)
	// Hold periodic flushing to exercise the mandatory end-of-job flush.
	s.lastPublish = time.Now().Add(time.Hour)
	j, err := s.Create(ctx, types.BulkJobRequest{RequestID: "coalesced-01", Operation: "set_status", Status: "completed", Paths: paths}, "test")
	if err != nil {
		t.Fatal(err)
	}
	result := drainBulk(t, s, ctx, j.ID)
	if result.Succeeded != 6 || events.checks != 1 {
		t.Fatalf("result %+v, checks=%d", result, events.checks)
	}
}
