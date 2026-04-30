package runner

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/huynle/brain-api/internal/sse"
)

// SSEListener watches SSE streams for task changes and signals the runner to poll.
type SSEListener struct {
	apiURL     string
	apiToken   string
	runnerID   string
	projects   []string
	wakeCh     chan<- struct{}
	shutdownCh chan<- string
	clients    []*sse.Client

	mu     sync.Mutex
	cancel context.CancelFunc
}

// NewSSEListener creates a new SSE listener.
func NewSSEListener(apiURL, apiToken, runnerID string, projects []string, wakeCh chan<- struct{}, shutdownCh chan<- string) *SSEListener {
	return &SSEListener{
		apiURL:     apiURL,
		apiToken:   apiToken,
		runnerID:   runnerID,
		projects:   projects,
		wakeCh:     wakeCh,
		shutdownCh: shutdownCh,
	}
}

// Start begins listening to SSE streams for all projects.
// Blocks until the context is cancelled or Stop is called.
func (l *SSEListener) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	l.mu.Lock()
	l.cancel = cancel
	l.mu.Unlock()

	var wg sync.WaitGroup

	subscriptions := append([]string{}, l.projects...)
	if l.runnerID != "" {
		subscriptions = append(subscriptions, l.runnerID)
	}

	for _, subscriptionID := range subscriptions {
		client := sse.NewClient(l.apiURL, l.apiToken, subscriptionID)
		l.mu.Lock()
		l.clients = append(l.clients, client)
		l.mu.Unlock()

		wg.Add(1)
		go func(c *sse.Client, pid string) {
			defer wg.Done()
			l.listenProject(ctx, c, pid)
		}(client, subscriptionID)
	}

	wg.Wait()
}

// listenProject listens to a single project's SSE stream and sends wake signals.
func (l *SSEListener) listenProject(ctx context.Context, client *sse.Client, projectID string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		ch := client.Connect(ctx)

		for event := range ch {
			select {
			case <-ctx.Done():
				return
			default:
			}

			if event.Type == "tasks_snapshot" {
				// Non-blocking send — if the channel is full, drop the signal
				select {
				case l.wakeCh <- struct{}{}:
					slog.Debug("SSE wake signal sent", "project", projectID)
				default:
					slog.Debug("SSE wake signal dropped (channel full)", "project", projectID)
				}
			}

			if event.Type == "shutdown" {
				reason := "remote shutdown requested"
				var payload struct {
					Reason string `json:"reason"`
				}
				if err := json.Unmarshal(event.Data, &payload); err == nil && payload.Reason != "" {
					reason = payload.Reason
				}
				select {
				case l.shutdownCh <- reason:
					slog.Info("SSE shutdown command received", "runner", projectID)
				default:
					slog.Debug("SSE shutdown command dropped (channel full)", "runner", projectID)
				}
			}

			if event.Type == "disconnected" {
				slog.Debug("SSE disconnected, will reconnect", "project", projectID)
				break
			}
		}

		// Reconnect delay
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
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
