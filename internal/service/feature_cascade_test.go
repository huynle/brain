package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// fakeFeatureRunner records every RunFeatureNow call so cascade tests can
// assert exactly which (projectID, featureID, force) triples fired.
type fakeFeatureRunner struct {
	mu    sync.Mutex
	calls []fakeFeatureRunnerCall
	// nextResponse is returned for the next call; defaults to dispatched=true
	// with no queued tasks so the cascade stays active.
	nextResponse *types.RunFeatureResponse
	// dispatchedReason lets a test simulate "no_ready_tasks" to verify the
	// cascade unregisters on drain.
	overrideReason string
	count          int32
}

type fakeFeatureRunnerCall struct {
	projectID string
	featureID string
	force     bool
}

func (f *fakeFeatureRunner) RunFeatureNow(ctx context.Context, projectID, featureID string, force bool) (*types.RunFeatureResponse, error) {
	atomic.AddInt32(&f.count, 1)
	f.mu.Lock()
	f.calls = append(f.calls, fakeFeatureRunnerCall{projectID, featureID, force})
	resp := f.nextResponse
	reason := f.overrideReason
	f.mu.Unlock()

	if resp != nil {
		// Clone so successive calls don't share state.
		r := *resp
		return &r, nil
	}
	return &types.RunFeatureResponse{
		ProjectID:       projectID,
		FeatureID:       featureID,
		Dispatched:      true,
		DispatchedCount: 1,
		Reason:          reason,
	}, nil
}

func (f *fakeFeatureRunner) callCount() int { return int(atomic.LoadInt32(&f.count)) }

func (f *fakeFeatureRunner) snapshotCalls() []fakeFeatureRunnerCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeFeatureRunnerCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// waitFor polls the predicate every 5ms up to 500ms; fails the test otherwise.
// Used to deflake event-driven assertions without sleeping for a fixed time.
func waitFor(t *testing.T, name string, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waitFor %q: condition never became true within 500ms", name)
}

func TestFeatureCascade_RegisterAndIsActive(t *testing.T) {
	svc := NewFeatureCascadeService(nil, &fakeFeatureRunner{})

	if svc.IsActive("proj", "feat") {
		t.Fatal("IsActive before Register = true, want false")
	}

	svc.Register("proj", "feat")
	if !svc.IsActive("proj", "feat") {
		t.Fatal("IsActive after Register = false, want true")
	}

	// Idempotent.
	svc.Register("proj", "feat")
	if !svc.IsActive("proj", "feat") {
		t.Fatal("IsActive after second Register = false, want true")
	}

	svc.Unregister("proj", "feat")
	if svc.IsActive("proj", "feat") {
		t.Fatal("IsActive after Unregister = true, want false")
	}
}

func TestFeatureCascade_IgnoresEmptyKeys(t *testing.T) {
	svc := NewFeatureCascadeService(nil, &fakeFeatureRunner{})
	svc.Register("", "feat")
	svc.Register("proj", "")
	if svc.IsActive("", "feat") || svc.IsActive("proj", "") {
		t.Fatal("empty-key registration should be a no-op")
	}
}

func TestFeatureCascade_DispatchesOnTaskCompleted(t *testing.T) {
	hub := realtime.NewEventHub()
	runner := &fakeFeatureRunner{}
	svc := NewFeatureCascadeService(hub, runner)
	svc.Register("proj", "feat")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)

	hub.Publish(types.Event{
		Type:      types.EventTaskCompleted,
		Source:    types.EventSourceRunner,
		ProjectID: "proj",
		FeatureID: "feat",
		TaskID:    "task-1",
	})

	waitFor(t, "cascade dispatch fires", func() bool { return runner.callCount() == 1 })

	calls := runner.snapshotCalls()
	if calls[0].projectID != "proj" || calls[0].featureID != "feat" {
		t.Fatalf("call = %+v, want proj/feat", calls[0])
	}
	if !calls[0].force {
		t.Fatal("cascade must always call with force=true to bypass pause")
	}
}

func TestFeatureCascade_IgnoresUnregisteredFeature(t *testing.T) {
	hub := realtime.NewEventHub()
	runner := &fakeFeatureRunner{}
	svc := NewFeatureCascadeService(hub, runner)
	// Note: no Register call.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)

	hub.Publish(types.Event{
		Type:      types.EventTaskCompleted,
		ProjectID: "proj",
		FeatureID: "feat",
		TaskID:    "task-1",
	})

	// Give the goroutine a chance to (incorrectly) run.
	time.Sleep(50 * time.Millisecond)

	if runner.callCount() != 0 {
		t.Fatalf("RunFeatureNow called %d times for unregistered feature; want 0", runner.callCount())
	}
}

func TestFeatureCascade_UnregistersOnDrain(t *testing.T) {
	hub := realtime.NewEventHub()
	runner := &fakeFeatureRunner{overrideReason: "no_ready_tasks"}
	svc := NewFeatureCascadeService(hub, runner)
	svc.Register("proj", "feat")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)

	hub.Publish(types.Event{
		Type:      types.EventTaskCompleted,
		ProjectID: "proj",
		FeatureID: "feat",
		TaskID:    "task-1",
	})

	waitFor(t, "first event handled", func() bool { return runner.callCount() == 1 })
	waitFor(t, "cascade auto-unregisters on drain", func() bool { return !svc.IsActive("proj", "feat") })

	// Subsequent event should not re-fire RunFeatureNow.
	hub.Publish(types.Event{
		Type:      types.EventTaskCompleted,
		ProjectID: "proj",
		FeatureID: "feat",
		TaskID:    "task-2",
	})
	time.Sleep(50 * time.Millisecond)
	if runner.callCount() != 1 {
		t.Fatalf("RunFeatureNow called %d times after unregister; want 1", runner.callCount())
	}
}

func TestFeatureCascade_RespondsToFailedAndCancelledEvents(t *testing.T) {
	hub := realtime.NewEventHub()
	runner := &fakeFeatureRunner{}
	svc := NewFeatureCascadeService(hub, runner)
	svc.Register("proj", "feat")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)

	hub.Publish(types.Event{Type: types.EventTaskFailed, ProjectID: "proj", FeatureID: "feat", TaskID: "t1"})
	waitFor(t, "task.failed triggers cascade", func() bool { return runner.callCount() == 1 })

	hub.Publish(types.Event{Type: types.EventTaskCancelled, ProjectID: "proj", FeatureID: "feat", TaskID: "t2"})
	waitFor(t, "task.cancelled triggers cascade", func() bool { return runner.callCount() == 2 })
}

func TestFeatureCascade_IgnoresEventsWithoutFeatureID(t *testing.T) {
	hub := realtime.NewEventHub()
	runner := &fakeFeatureRunner{}
	svc := NewFeatureCascadeService(hub, runner)
	svc.Register("proj", "feat")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)

	// No FeatureID set on the event — should be ignored even though there's
	// an active cascade registration.
	hub.Publish(types.Event{
		Type:      types.EventTaskCompleted,
		ProjectID: "proj",
		TaskID:    "task-1",
	})
	time.Sleep(50 * time.Millisecond)

	if runner.callCount() != 0 {
		t.Fatalf("cascade fired %d times for event without FeatureID; want 0", runner.callCount())
	}
}

// ─── drain detection ─────────────────────────────────────────────

func intp(n int) *int { return &n }

// TestFeatureCascade_KeepsFeatureAliveMidFlight pins the fan-in bug.
//
// A feature with two tasks running and a third waiting on both reports
// reason "no_ready_tasks" the moment the FIRST of the two completes — it is
// still mid-flight, but nothing is READY at that instant. The cascade used
// to key drain detection on that token, unregistered here, and the second
// completion then found no cascade: the third task was never dispatched. On
// a paused project nothing else picks it up, so the tail of the feature was
// silently dropped.
//
// Reproduced live before the fix: the fan-in task sat at classification
// "ready" indefinitely under pause, then dispatched within a second of
// resuming the project — proving the task was fine and the cascade had
// dropped it.
func TestFeatureCascade_KeepsFeatureAliveMidFlight(t *testing.T) {
	runner := &fakeFeatureRunner{
		nextResponse: &types.RunFeatureResponse{
			Reason: "no_ready_tasks",
			// Two tasks still in flight: NOT drained, whatever the reason says.
			Outstanding: intp(2),
		},
	}
	svc := NewFeatureCascadeService(nil, runner)
	svc.Register("proj", "fanin")

	svc.handleEvent(context.Background(), types.Event{
		ProjectID: "proj", FeatureID: "fanin", Type: types.EventTaskCompleted,
	})

	if !svc.IsActive("proj", "fanin") {
		t.Fatal("cascade unregistered while the feature still had outstanding work; " +
			"the remaining task will never dispatch under a paused project")
	}
}

func TestFeatureCascade_UnregistersOnRealDrain(t *testing.T) {
	runner := &fakeFeatureRunner{
		nextResponse: &types.RunFeatureResponse{
			Reason:      "no_ready_tasks",
			Outstanding: intp(0),
		},
	}
	svc := NewFeatureCascadeService(nil, runner)
	svc.Register("proj", "done")

	svc.handleEvent(context.Background(), types.Event{
		ProjectID: "proj", FeatureID: "done", Type: types.EventTaskCompleted,
	})

	if svc.IsActive("proj", "done") {
		t.Fatal("cascade stayed registered after the feature genuinely drained")
	}
}

// A nil Outstanding means the server could not measure. It must never be
// confused with zero — that is the whole reason the field is a pointer.
func TestFeatureCascade_UnknownOutstandingFallsBackToReason(t *testing.T) {
	runner := &fakeFeatureRunner{
		nextResponse: &types.RunFeatureResponse{Reason: "no_ready_tasks"},
	}
	svc := NewFeatureCascadeService(nil, runner)
	svc.Register("proj", "legacy")

	svc.handleEvent(context.Background(), types.Event{
		ProjectID: "proj", FeatureID: "legacy", Type: types.EventTaskCompleted,
	})

	if svc.IsActive("proj", "legacy") {
		t.Fatal("with Outstanding unmeasurable, the legacy reason-based drain should still apply")
	}
}

// Outstanding > 0 with an in-flight reason must also keep the cascade — this
// is the pre-existing "feature_in_progress" path, now measured rather than
// inferred from a token.
func TestFeatureCascade_KeepsAliveWhileInProgress(t *testing.T) {
	runner := &fakeFeatureRunner{
		nextResponse: &types.RunFeatureResponse{
			Reason:      "feature_in_progress",
			Outstanding: intp(1),
		},
	}
	svc := NewFeatureCascadeService(nil, runner)
	svc.Register("proj", "busy")

	svc.handleEvent(context.Background(), types.Event{
		ProjectID: "proj", FeatureID: "busy", Type: types.EventTaskCompleted,
	})

	if !svc.IsActive("proj", "busy") {
		t.Fatal("cascade dropped a feature with work still in flight")
	}
}
