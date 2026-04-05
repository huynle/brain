package runner

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// EventPoster Interface
// =============================================================================

// EventPoster sends a batch of events to the Brain API server.
// This is separate from Client to allow lightweight mocking.
type EventPoster interface {
	PostEvents(ctx context.Context, events []types.Event) error
}

// =============================================================================
// Configuration
// =============================================================================

// EventForwarderConfig controls the batching, queue, and retry behavior.
type EventForwarderConfig struct {
	// BatchSize is the number of events per batch POST (default 10).
	BatchSize int
	// FlushInterval is the max time between flushes (default 2s).
	FlushInterval time.Duration
	// MaxQueueSize is the max events buffered in-memory (default 1000).
	MaxQueueSize int
	// RetryAttempts is the number of retries on failure (default 3).
	RetryAttempts int
}

func (c EventForwarderConfig) withDefaults() EventForwarderConfig {
	if c.BatchSize <= 0 {
		c.BatchSize = 10
	}
	if c.FlushInterval <= 0 {
		c.FlushInterval = 2 * time.Second
	}
	if c.MaxQueueSize <= 0 {
		c.MaxQueueSize = 1000
	}
	if c.RetryAttempts <= 0 {
		c.RetryAttempts = 3
	}
	return c
}

// =============================================================================
// Stats
// =============================================================================

// EventForwarderStats tracks forwarding metrics.
type EventForwarderStats struct {
	Queued  int64 // total events queued
	Sent    int64 // total events successfully sent
	Failed  int64 // total events that failed after all retries
	Dropped int64 // total events dropped due to queue overflow
}

// =============================================================================
// EventForwarder
// =============================================================================

// EventForwarder registers as a runner OnEvent handler, converts RunnerEvent
// to unified types.Event, and asynchronously batches + POSTs them to the
// Brain API server.
type EventForwarder struct {
	poster EventPoster
	config EventForwarderConfig
	logger *log.Logger

	// Buffered channel acts as the event queue.
	queue chan types.Event

	// Stats tracked atomically.
	queued  atomic.Int64
	sent    atomic.Int64
	failed  atomic.Int64
	dropped atomic.Int64

	// Lifecycle
	stopOnce sync.Once
	done     chan struct{}
}

// NewEventForwarder creates a new forwarder. Call Start() to begin
// the flush loop and Stop() to drain and shut down.
func NewEventForwarder(poster EventPoster, cfg EventForwarderConfig) *EventForwarder {
	cfg = cfg.withDefaults()
	return &EventForwarder{
		poster: poster,
		config: cfg,
		logger: log.Default(),
		queue:  make(chan types.Event, cfg.MaxQueueSize),
		done:   make(chan struct{}),
	}
}

// Handle is the EventHandler func registered with TaskRunner.OnEvent().
// It converts a RunnerEvent to a types.Event and enqueues it.
// Non-blocking: drops oldest events if the queue is full.
func (ef *EventForwarder) Handle(event RunnerEvent) {
	evt := event.ToEvent()
	ef.queued.Add(1)

	select {
	case ef.queue <- evt:
		// enqueued successfully
	default:
		// Queue full - drop this event to prevent blocking
		ef.dropped.Add(1)
	}
}

// Start begins the background flush loop.
func (ef *EventForwarder) Start(ctx context.Context) {
	go ef.run(ctx)
}

// Stop signals the flush loop to drain remaining events and shut down.
// Blocks until all queued events are flushed or the drain times out.
func (ef *EventForwarder) Stop() {
	ef.stopOnce.Do(func() {
		close(ef.done)
	})
	// Give the run loop time to drain
	time.Sleep(100 * time.Millisecond)
}

// Stats returns a snapshot of the forwarding metrics.
func (ef *EventForwarder) Stats() EventForwarderStats {
	return EventForwarderStats{
		Queued:  ef.queued.Load(),
		Sent:    ef.sent.Load(),
		Failed:  ef.failed.Load(),
		Dropped: ef.dropped.Load(),
	}
}

// =============================================================================
// Internal
// =============================================================================

func (ef *EventForwarder) run(ctx context.Context) {
	ticker := time.NewTicker(ef.config.FlushInterval)
	defer ticker.Stop()

	batch := make([]types.Event, 0, ef.config.BatchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		// Copy the batch for sending
		toSend := make([]types.Event, len(batch))
		copy(toSend, batch)
		batch = batch[:0]

		ef.sendWithRetry(ctx, toSend)
	}

	for {
		select {
		case evt := <-ef.queue:
			batch = append(batch, evt)
			if len(batch) >= ef.config.BatchSize {
				flush()
			}

		case <-ticker.C:
			flush()

		case <-ef.done:
			// Drain remaining events from the queue
			ef.drain(ctx, batch)
			return

		case <-ctx.Done():
			// Context cancelled - drain what we can
			ef.drain(ctx, batch)
			return
		}
	}
}

// drain empties the queue channel and flushes any remaining events.
func (ef *EventForwarder) drain(ctx context.Context, batch []types.Event) {
	// Use a background context for drain (the original may be cancelled)
	drainCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Collect remaining events from channel
	for {
		select {
		case evt := <-ef.queue:
			batch = append(batch, evt)
		default:
			// Channel empty
			if len(batch) > 0 {
				ef.sendWithRetry(drainCtx, batch)
			}
			return
		}
	}
}

// sendWithRetry posts a batch of events, retrying with exponential backoff.
func (ef *EventForwarder) sendWithRetry(ctx context.Context, events []types.Event) {
	var lastErr error
	for attempt := 0; attempt <= ef.config.RetryAttempts; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 50ms, 100ms, 200ms, ...
			backoff := time.Duration(1<<uint(attempt-1)) * 50 * time.Millisecond
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				ef.failed.Add(int64(len(events)))
				return
			}
		}

		err := ef.poster.PostEvents(ctx, events)
		if err == nil {
			ef.sent.Add(int64(len(events)))
			return
		}
		lastErr = err
	}

	// All retries exhausted
	ef.failed.Add(int64(len(events)))
	if lastErr != nil {
		ef.logger.Printf("[event-forwarder] failed to send %d events after %d retries: %v",
			len(events), ef.config.RetryAttempts, lastErr)
	}
}
