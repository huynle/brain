package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/huynle/brain-api/internal/sse"
)

// =============================================================================
// Backoff constants
// =============================================================================

const (
	// initialBackoff is the starting reconnect delay.
	initialBackoff = 2 * time.Second

	// maxBackoff is the ceiling for exponential backoff.
	maxBackoff = 60 * time.Second

	// backoffMultiplier doubles the delay on each consecutive failure.
	backoffMultiplier = 2
)

// =============================================================================
// SSEListener
// =============================================================================

// SSEListener watches SSE streams for task changes and commands, signaling
// the runner to poll or handling server-pushed commands.
type SSEListener struct {
	apiURL    string
	apiToken  string
	projects  []string
	runnerID  string
	wakeCh    chan<- struct{}
	commandCh chan<- RunnerCommand
	clients   []*sse.Client

	mu     sync.Mutex
	cancel context.CancelFunc
}

// NewSSEListener creates a new SSE listener.
// commandCh and runnerID may be zero-valued for backward compat (no runner stream).
func NewSSEListener(apiURL, apiToken string, projects []string, wakeCh chan<- struct{}) *SSEListener {
	return &SSEListener{
		apiURL:   apiURL,
		apiToken: apiToken,
		projects: projects,
		wakeCh:   wakeCh,
	}
}

// SetRunnerStream configures the runner-scoped SSE stream.
// Must be called before Start().
func (l *SSEListener) SetRunnerStream(runnerID string, commandCh chan<- RunnerCommand) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.runnerID = runnerID
	l.commandCh = commandCh
}

// Start begins listening to SSE streams for all projects and (optionally)
// the runner command stream. Blocks until the context is cancelled or Stop is called.
func (l *SSEListener) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	l.mu.Lock()
	l.cancel = cancel
	l.mu.Unlock()

	var wg sync.WaitGroup

	// Per-project task streams
	for _, projectID := range l.projects {
		client := sse.NewClient(l.apiURL, l.apiToken, projectID)
		l.mu.Lock()
		l.clients = append(l.clients, client)
		l.mu.Unlock()

		wg.Add(1)
		go func(c *sse.Client, pid string) {
			defer wg.Done()
			l.listenProject(ctx, c, pid)
		}(client, projectID)
	}

	// Runner-scoped command stream
	l.mu.Lock()
	runnerID := l.runnerID
	commandCh := l.commandCh
	l.mu.Unlock()

	if runnerID != "" && commandCh != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.listenRunner(ctx, runnerID)
		}()
	}

	wg.Wait()
}

// listenProject listens to a single project's SSE stream and sends wake signals.
func (l *SSEListener) listenProject(ctx context.Context, client *sse.Client, projectID string) {
	backoff := initialBackoff

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		ch := client.Connect(ctx)
		connected := false

		for event := range ch {
			select {
			case <-ctx.Done():
				return
			default:
			}

			if event.Type == "connected" {
				connected = true
				backoff = initialBackoff // reset on successful connection
			}

			if event.Type == "tasks_snapshot" || event.Type == "tasks_changed" {
				// Non-blocking send — if the channel is full, drop the signal
				select {
				case l.wakeCh <- struct{}{}:
					slog.Debug("SSE wake signal sent", "project", projectID)
				default:
					slog.Debug("SSE wake signal dropped (channel full)", "project", projectID)
				}
			}

			if event.Type == "disconnected" {
				slog.Debug("SSE disconnected, will reconnect", "project", projectID)
				break
			}
		}

		// Reset backoff if we had a successful connection
		if connected {
			backoff = initialBackoff
		}

		// Reconnect with exponential backoff
		slog.Debug("SSE reconnecting", "project", projectID, "backoff", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		// Increase backoff for next failure (capped at maxBackoff)
		backoff = backoff * time.Duration(backoffMultiplier)
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// listenRunner connects to the runner-scoped SSE stream at
// GET /api/v1/runners/{runnerId}/stream and handles command events.
func (l *SSEListener) listenRunner(ctx context.Context, runnerID string) {
	backoff := initialBackoff

	streamURL := fmt.Sprintf("%s/api/v1/runners/%s/stream",
		strings.TrimRight(l.apiURL, "/"), runnerID)

	// Create a dedicated SSE client for the runner stream.
	// We use a synthetic "project ID" that won't collide with real projects
	// since the sse.Client builds its URL from projectID — so we use a raw client approach.
	client := sse.NewClientWithURL(l.apiURL, l.apiToken, streamURL)

	l.mu.Lock()
	l.clients = append(l.clients, client)
	l.mu.Unlock()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		ch := client.Connect(ctx)
		connected := false

		for event := range ch {
			select {
			case <-ctx.Done():
				return
			default:
			}

			if event.Type == "connected" {
				connected = true
				backoff = initialBackoff
				slog.Info("runner SSE stream connected", "runner_id", runnerID)
			}

			if event.Type == "tasks_changed" {
				// Wake signal (same as project streams)
				select {
				case l.wakeCh <- struct{}{}:
					slog.Debug("runner SSE wake signal sent", "runner_id", runnerID)
				default:
				}
			}

			if event.Type == "command" {
				l.handleCommandEvent(event, runnerID)
			}

			if event.Type == "disconnected" {
				slog.Debug("runner SSE disconnected, will reconnect", "runner_id", runnerID)
				break
			}
		}

		if connected {
			backoff = initialBackoff
		}

		slog.Debug("runner SSE reconnecting", "runner_id", runnerID, "backoff", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff = backoff * time.Duration(backoffMultiplier)
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// handleCommandEvent parses a "command" SSE event and sends it to the commandCh.
func (l *SSEListener) handleCommandEvent(event sse.Event, runnerID string) {
	l.mu.Lock()
	commandCh := l.commandCh
	l.mu.Unlock()

	if commandCh == nil {
		return
	}

	var cmd RunnerCommand
	if err := json.Unmarshal(event.Data, &cmd); err != nil {
		slog.Warn("failed to parse runner command", "error", err, "runner_id", runnerID)
		return
	}

	// Non-blocking send to command channel
	select {
	case commandCh <- cmd:
		slog.Debug("runner command sent", "type", cmd.Type, "runner_id", runnerID)
	default:
		slog.Warn("runner command dropped (channel full)", "type", cmd.Type, "runner_id", runnerID)
	}
}

// Stop closes all SSE connections.
func (l *SSEListener) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}

	for _, client := range l.clients {
		client.Close()
	}
	l.clients = nil
}
