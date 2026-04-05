package runner

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Log Streamer Configuration
// =============================================================================

const (
	// DefaultLogBatchSize is the maximum number of log lines per batch before flushing.
	DefaultLogBatchSize = 50

	// DefaultLogFlushInterval is how often buffered log lines are flushed if the
	// batch size threshold hasn't been reached.
	DefaultLogFlushInterval = 2 * time.Second
)

// LogStreamerConfig configures a LogStreamer.
type LogStreamerConfig struct {
	// Client is the API client used to POST log batches.
	Client Client

	// RunnerID identifies this runner in the log metadata.
	RunnerID string

	// ProjectID is the project the task belongs to.
	ProjectID string

	// TaskID is the task whose logs we are streaming.
	TaskID string

	// BatchSize is the max lines per batch. Defaults to DefaultLogBatchSize.
	BatchSize int

	// FlushInterval is the max time between flushes. Defaults to DefaultLogFlushInterval.
	FlushInterval time.Duration
}

// =============================================================================
// Log Streamer
// =============================================================================

// LogStreamer is an io.Writer that captures executor stdout/stderr, splits it
// into lines, and periodically POSTs batches to the Brain API. It is safe for
// concurrent use.
//
// Design:
//   - Write() is non-blocking: it buffers lines and wakes the flush goroutine.
//   - A background goroutine flushes on BatchSize or FlushInterval.
//   - POST failures are logged but never propagate to the writer.
type LogStreamer struct {
	cfg LogStreamerConfig

	mu      sync.Mutex
	buf     []types.LogLine
	partial string // partial line (no trailing newline yet)

	flushCh chan struct{}
	done    chan struct{}
	cancel  context.CancelFunc
}

// NewLogStreamer creates and starts a LogStreamer. Call Stop() when the task
// completes to flush remaining lines and release resources.
func NewLogStreamer(cfg LogStreamerConfig) *LogStreamer {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultLogBatchSize
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = DefaultLogFlushInterval
	}

	ctx, cancel := context.WithCancel(context.Background())
	ls := &LogStreamer{
		cfg:     cfg,
		flushCh: make(chan struct{}, 1),
		done:    make(chan struct{}),
		cancel:  cancel,
	}

	go ls.run(ctx)
	return ls
}

// Write implements io.Writer. It splits data into lines and buffers them.
// Bytes without a trailing newline are held in a partial buffer until the
// next write completes the line. Write never returns an error.
func (ls *LogStreamer) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)

	ls.mu.Lock()
	text := ls.partial + string(p)
	ls.partial = ""

	lines := strings.Split(text, "\n")
	// Last element is either empty (if text ended with \n) or a partial line
	if len(lines) > 0 {
		last := lines[len(lines)-1]
		lines = lines[:len(lines)-1]
		if last != "" {
			ls.partial = last
		}
	}

	for _, line := range lines {
		if line == "" {
			continue
		}
		level := detectLevel(line)
		ls.buf = append(ls.buf, types.LogLine{
			Timestamp: now,
			Level:     level,
			Content:   line,
		})
	}

	shouldFlush := len(ls.buf) >= ls.cfg.BatchSize
	ls.mu.Unlock()

	if shouldFlush {
		ls.wake()
	}

	return len(p), nil
}

// Stop flushes any remaining buffered lines (including partial line) and
// stops the background goroutine. It blocks until the final flush completes.
func (ls *LogStreamer) Stop() {
	// Flush any remaining partial line
	ls.mu.Lock()
	if ls.partial != "" {
		now := time.Now().UTC().Format(time.RFC3339)
		level := detectLevel(ls.partial)
		ls.buf = append(ls.buf, types.LogLine{
			Timestamp: now,
			Level:     level,
			Content:   ls.partial,
		})
		ls.partial = ""
	}
	ls.mu.Unlock()

	ls.cancel()
	<-ls.done
}

// =============================================================================
// Internal
// =============================================================================

// run is the background flush loop.
func (ls *LogStreamer) run(ctx context.Context) {
	defer close(ls.done)

	ticker := time.NewTicker(ls.cfg.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Final flush
			ls.flush(context.Background())
			return
		case <-ticker.C:
			ls.flush(ctx)
		case <-ls.flushCh:
			ls.flush(ctx)
		}
	}
}

// flush drains the buffer and POSTs to the API.
func (ls *LogStreamer) flush(ctx context.Context) {
	ls.mu.Lock()
	if len(ls.buf) == 0 {
		ls.mu.Unlock()
		return
	}
	batch := ls.buf
	ls.buf = nil
	ls.mu.Unlock()

	err := ls.cfg.Client.PostTaskLogs(ctx, ls.cfg.ProjectID, ls.cfg.TaskID, ls.cfg.RunnerID, batch)
	if err != nil {
		slog.Warn("log streamer: POST failed",
			"project", ls.cfg.ProjectID,
			"task", ls.cfg.TaskID,
			"lines", len(batch),
			"error", err,
		)
	}
}

// wake signals the flush goroutine to drain the buffer.
func (ls *LogStreamer) wake() {
	select {
	case ls.flushCh <- struct{}{}:
	default:
	}
}

// detectLevel heuristically determines the log level from line content.
func detectLevel(line string) string {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "error") || strings.Contains(lower, "fatal") || strings.Contains(lower, "panic") {
		return "error"
	}
	if strings.Contains(lower, "warn") {
		return "warn"
	}
	return "info"
}
