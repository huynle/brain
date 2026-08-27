package sse

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
)

// Client connects to an SSE endpoint and emits events on a channel.
type Client struct {
	apiURL    string
	apiToken  string
	projectID string

	mu        sync.Mutex
	cancel    context.CancelFunc
	connected bool

	// sendMu serializes sends on eventCh against the close of eventCh.
	// A sender holds it for the whole duration of the send, so anything
	// that closes the channel must cancel the context first — that is what
	// unblocks an in-flight send and lets it release the lock. Guarding the
	// closed flag with the same lock that covers the send is what makes the
	// check-then-send safe; checking under a lock that is dropped before the
	// send leaves a window where Close() can close the channel underneath a
	// sender, which is a "send on closed channel" panic.
	sendMu  sync.Mutex
	eventCh chan Event
	closed  bool // tracks whether eventCh has been closed
}

// NewClient creates a new SSE client for the given API URL, token, and project.
func NewClient(apiURL, apiToken, projectID string) *Client {
	return &Client{
		apiURL:    strings.TrimRight(apiURL, "/"),
		apiToken:  apiToken,
		projectID: projectID,
		eventCh:   make(chan Event, 32),
	}
}

// NewClientWithURL creates a new SSE client that connects to an explicit URL
// rather than deriving the stream URL from a project ID.
// This is used for runner-scoped SSE streams (e.g., /api/v1/runners/{id}/stream).
func NewClientWithURL(apiURL, apiToken, streamURL string) *Client {
	return &Client{
		apiURL:    streamURL, // store the full URL directly
		apiToken:  apiToken,
		projectID: "", // not used — streamURL overrides
		eventCh:   make(chan Event, 32),
	}
}

// streamURL returns the full SSE stream URL.
func (c *Client) streamURL() string {
	if c.projectID == "" {
		// Direct URL mode (set via NewClientWithURL)
		return c.apiURL
	}
	return fmt.Sprintf("%s/api/v1/tasks/%s/stream", c.apiURL, c.projectID)
}

// Connect starts the SSE connection. Returns a read-only event channel.
// The channel receives parsed events. A "disconnected" event is sent when
// the connection is lost. The channel is closed when the context is cancelled.
func (c *Client) Connect(ctx context.Context) <-chan Event {
	ctx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.cancel = cancel
	c.connected = false
	c.mu.Unlock()

	go c.listen(ctx)
	return c.eventCh
}

// Close stops the SSE connection.
func (c *Client) Close() {
	// Cancel before taking sendMu. An in-flight sendEvent holds sendMu until
	// its send completes or its context is done, so on a full channel with no
	// reader, cancelling is the only thing that releases the lock.
	c.mu.Lock()
	cancel := c.cancel
	c.cancel = nil
	c.connected = false
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}

	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.eventCh)
	}
}

// Connected returns whether the SSE stream is currently connected.
func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// listen connects to the SSE endpoint and sends parsed events to the channel.
// Blocks until context is cancelled or the connection is lost.
func (c *Client) listen(ctx context.Context) {
	defer func() {
		c.mu.Lock()
		c.connected = false
		c.mu.Unlock()

		c.sendMu.Lock()
		if !c.closed {
			c.closed = true
			close(c.eventCh)
		}
		c.sendMu.Unlock()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.streamURL(), nil)
	if err != nil {
		c.sendEvent(ctx, Event{Type: "disconnected"})
		return
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if c.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.sendEvent(ctx, Event{Type: "disconnected"})
		return
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 2*1024*1024), 2*1024*1024)
	var lines []string

	for scanner.Scan() {
		// Check context before processing
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()

		if line == "" {
			// Empty line = end of event block
			if len(lines) > 0 {
				event, err := ParseEvent(lines)
				if err != nil {
					slog.Debug("SSE parse error", "error", err)
					c.sendEvent(ctx, Event{Type: "error", Data: []byte(fmt.Sprintf(`{"message":%q}`, err.Error()))})
				} else if event != nil {
					// Track connected state
					if event.Type == "connected" {
						c.mu.Lock()
						c.connected = true
						c.mu.Unlock()
					}
					c.sendEvent(ctx, *event)
				}
				lines = nil
			}
		} else {
			lines = append(lines, line)
		}
	}

	// Scanner finished — connection lost or server closed
	if ctx.Err() != nil {
		return
	}

	c.sendEvent(ctx, Event{Type: "disconnected"})
}

// sendEvent sends an event to the channel, respecting context cancellation.
func (c *Client) sendEvent(ctx context.Context, event Event) {
	// Held across the send, not just the closed check — see sendMu.
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if c.closed {
		return
	}

	select {
	case c.eventCh <- event:
	case <-ctx.Done():
	}
}
