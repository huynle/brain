package runner

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Mock Event Poster
// =============================================================================

// mockEventPoster records calls to PostEvents for test assertions.
type mockEventPoster struct {
	mu       sync.Mutex
	calls    [][]types.Event
	failNext int // number of calls that should return an error
	err      error
}

func newMockEventPoster() *mockEventPoster {
	return &mockEventPoster{}
}

func (m *mockEventPoster) PostEvents(ctx context.Context, events []types.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failNext > 0 {
		m.failNext--
		return m.err
	}
	m.calls = append(m.calls, events)
	return nil
}

func (m *mockEventPoster) getCalls() [][]types.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([][]types.Event, len(m.calls))
	copy(cp, m.calls)
	return cp
}

func (m *mockEventPoster) totalEventsReceived() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := 0
	for _, batch := range m.calls {
		total += len(batch)
	}
	return total
}

func (m *mockEventPoster) setFailures(n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failNext = n
	m.err = err
}

// =============================================================================
// Default Test Config
// =============================================================================

func testForwarderConfig() EventForwarderConfig {
	return EventForwarderConfig{
		BatchSize:     3,
		FlushInterval: 50 * time.Millisecond,
		MaxQueueSize:  20,
		RetryAttempts: 2,
	}
}

// =============================================================================
// Tests
// =============================================================================

func TestEventForwarder_ForwardsEventsAsynchronously(t *testing.T) {
	poster := newMockEventPoster()
	cfg := testForwarderConfig()
	cfg.BatchSize = 1 // flush immediately on each event

	fwd := NewEventForwarder(poster, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fwd.Start(ctx)

	// Send a runner event via the handler
	fwd.Handle(RunnerEvent{
		Type:      EventTaskStarted,
		RunnerID:  "runner_test",
		ProjectID: "proj-1",
		TaskID:    "task-1",
	})

	// Wait for async flush
	time.Sleep(200 * time.Millisecond)
	fwd.Stop()

	calls := poster.getCalls()
	if len(calls) == 0 {
		t.Fatal("expected at least one PostEvents call, got none")
	}

	total := poster.totalEventsReceived()
	if total != 1 {
		t.Fatalf("expected 1 event forwarded, got %d", total)
	}

	// Verify conversion to unified type
	evt := calls[0][0]
	if evt.Type != types.EventTaskStarted {
		t.Errorf("event type = %q, want %q", evt.Type, types.EventTaskStarted)
	}
	if evt.Source != types.EventSourceRunner {
		t.Errorf("event source = %q, want %q", evt.Source, types.EventSourceRunner)
	}
	if evt.ProjectID != "proj-1" {
		t.Errorf("project_id = %q, want %q", evt.ProjectID, "proj-1")
	}
}

func TestEventForwarder_BatchesMultipleEvents(t *testing.T) {
	poster := newMockEventPoster()
	cfg := testForwarderConfig()
	cfg.BatchSize = 3
	cfg.FlushInterval = 5 * time.Second // don't flush by timer during test

	fwd := NewEventForwarder(poster, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fwd.Start(ctx)

	// Send 3 events (should trigger a batch flush)
	for i := 0; i < 3; i++ {
		fwd.Handle(RunnerEvent{
			Type:     EventTaskStarted,
			RunnerID: "runner_test",
			TaskID:   "task-" + string(rune('A'+i)),
		})
	}

	// Wait for batch to be flushed
	time.Sleep(300 * time.Millisecond)
	fwd.Stop()

	calls := poster.getCalls()
	if len(calls) == 0 {
		t.Fatal("expected at least one batch call, got none")
	}

	// Should be sent in a single batch of 3
	if len(calls[0]) != 3 {
		t.Errorf("first batch size = %d, want 3", len(calls[0]))
	}
}

func TestEventForwarder_FlushesOnInterval(t *testing.T) {
	poster := newMockEventPoster()
	cfg := testForwarderConfig()
	cfg.BatchSize = 100 // won't be reached
	cfg.FlushInterval = 50 * time.Millisecond

	fwd := NewEventForwarder(poster, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fwd.Start(ctx)

	// Send 2 events (below batch size, should flush by timer)
	fwd.Handle(RunnerEvent{Type: EventTaskStarted, RunnerID: "r1"})
	fwd.Handle(RunnerEvent{Type: EventTaskCompleted, RunnerID: "r1"})

	// Wait for timer flush
	time.Sleep(300 * time.Millisecond)
	fwd.Stop()

	total := poster.totalEventsReceived()
	if total != 2 {
		t.Fatalf("expected 2 events flushed by timer, got %d", total)
	}
}

func TestEventForwarder_RetriesOnFailure(t *testing.T) {
	poster := newMockEventPoster()
	poster.setFailures(1, &APIError{StatusCode: 500, Body: "server error"})
	cfg := testForwarderConfig()
	cfg.BatchSize = 1
	cfg.RetryAttempts = 3

	fwd := NewEventForwarder(poster, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fwd.Start(ctx)

	fwd.Handle(RunnerEvent{Type: EventTaskStarted, RunnerID: "r1", TaskID: "task-1"})

	// Wait for retry
	time.Sleep(500 * time.Millisecond)
	fwd.Stop()

	// After 1 failure and 1 success, the event should have been delivered
	total := poster.totalEventsReceived()
	if total != 1 {
		t.Fatalf("expected 1 event delivered after retry, got %d", total)
	}
}

func TestEventForwarder_DropsOldestWhenQueueFull(t *testing.T) {
	poster := newMockEventPoster()
	// Make all posts fail so events queue up
	poster.setFailures(1000, &APIError{StatusCode: 500, Body: "offline"})
	cfg := testForwarderConfig()
	cfg.MaxQueueSize = 5
	cfg.BatchSize = 100 // don't auto-batch
	cfg.FlushInterval = 10 * time.Millisecond

	fwd := NewEventForwarder(poster, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fwd.Start(ctx)

	// Enqueue more events than the max queue size
	for i := 0; i < 10; i++ {
		fwd.Handle(RunnerEvent{
			Type:     EventTaskStarted,
			RunnerID: "r1",
			TaskID:   "task-" + string(rune('0'+i)),
		})
	}

	// Give time for queue to be exercised
	time.Sleep(100 * time.Millisecond)
	fwd.Stop()

	// The queue should not exceed max size. Verify via stats.
	stats := fwd.Stats()
	if stats.Dropped == 0 {
		t.Error("expected some events to be dropped when queue is full")
	}
}

func TestEventForwarder_GracefulDrainOnStop(t *testing.T) {
	poster := newMockEventPoster()
	cfg := testForwarderConfig()
	cfg.BatchSize = 100                  // won't auto-batch by size
	cfg.FlushInterval = 10 * time.Second // won't auto-flush by timer

	fwd := NewEventForwarder(poster, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fwd.Start(ctx)

	// Enqueue events
	for i := 0; i < 5; i++ {
		fwd.Handle(RunnerEvent{
			Type:     EventTaskCompleted,
			RunnerID: "r1",
			TaskID:   "task-" + string(rune('A'+i)),
		})
	}

	// Small delay to let events enter the channel
	time.Sleep(50 * time.Millisecond)

	// Stop should drain remaining events
	fwd.Stop()

	total := poster.totalEventsReceived()
	if total != 5 {
		t.Fatalf("expected all 5 events drained on stop, got %d", total)
	}
}

func TestEventForwarder_DoesNotBlockCaller(t *testing.T) {
	poster := newMockEventPoster()
	cfg := testForwarderConfig()

	fwd := NewEventForwarder(poster, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fwd.Start(ctx)

	// Handle should return immediately even under load
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			fwd.Handle(RunnerEvent{
				Type:     EventTaskStarted,
				RunnerID: "r1",
			})
		}
		close(done)
	}()

	select {
	case <-done:
		// good - Handle returned quickly
	case <-time.After(2 * time.Second):
		t.Fatal("Handle() blocked for too long, should be non-blocking")
	}

	fwd.Stop()
}

func TestEventForwarder_StatsTracking(t *testing.T) {
	poster := newMockEventPoster()
	cfg := testForwarderConfig()
	cfg.BatchSize = 2

	fwd := NewEventForwarder(poster, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fwd.Start(ctx)

	// Send 4 events (2 batches of 2)
	for i := 0; i < 4; i++ {
		fwd.Handle(RunnerEvent{
			Type:     EventTaskStarted,
			RunnerID: "r1",
		})
	}

	time.Sleep(300 * time.Millisecond)
	fwd.Stop()

	stats := fwd.Stats()
	if stats.Sent != 4 {
		t.Errorf("stats.Sent = %d, want 4", stats.Sent)
	}
	if stats.Queued < 4 {
		t.Errorf("stats.Queued = %d, want >= 4", stats.Queued)
	}
}

func TestEventForwarder_HandleBeforeStart(t *testing.T) {
	poster := newMockEventPoster()
	cfg := testForwarderConfig()

	fwd := NewEventForwarder(poster, cfg)

	// Handle before Start should not panic
	fwd.Handle(RunnerEvent{
		Type:     EventTaskStarted,
		RunnerID: "r1",
	})

	// Start and stop to drain
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fwd.Start(ctx)
	time.Sleep(200 * time.Millisecond)
	fwd.Stop()

	// Event should still be queued and sent
	total := poster.totalEventsReceived()
	if total != 1 {
		t.Fatalf("expected 1 event from pre-Start handle, got %d", total)
	}
}

func TestEventForwarder_ContextCancellation(t *testing.T) {
	poster := newMockEventPoster()
	cfg := testForwarderConfig()
	cfg.FlushInterval = 10 * time.Second

	fwd := NewEventForwarder(poster, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	fwd.Start(ctx)

	fwd.Handle(RunnerEvent{Type: EventTaskStarted, RunnerID: "r1"})
	time.Sleep(50 * time.Millisecond)

	// Cancel context triggers drain
	cancel()
	fwd.Stop()

	total := poster.totalEventsReceived()
	if total != 1 {
		t.Fatalf("expected 1 event drained on cancel, got %d", total)
	}
}
