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

// eventBuffer is the depth of one connection's event channel.
const eventBuffer = 32

// stream is ONE connection's event channel plus the state needed to close it
// exactly once.
//
// It is a separate type because a Client outlives its connections. Every
// Connect gets a fresh stream, so a dropped connection closes only the
// generation that owned it and the next Connect hands the caller a live
// channel. Holding the channel on the Client instead — allocated once in the
// constructor, closed on the first disconnect — made the client single-use:
// reconnects kept opening real sockets while every event parsed off them was
// dropped by the closed check, so a runner went permanently deaf to dispatch
// commands while the server still counted them as delivered. That is
// indistinguishable from an idle server, and it stayed that way for 17 hours
// in production on 2026-09-03.
type stream struct {
	// mu serializes sends on ch against the close of ch. A sender holds it
	// for the whole duration of the send, so anything that closes the channel
	// must cancel the connection's context first — that is what unblocks an
	// in-flight send and lets it release the lock. Guarding the closed flag
	// with the same lock that covers the send is what makes the
	// check-then-send safe; checking under a lock that is dropped before the
	// send leaves a window where the close can land underneath a sender,
	// which is a "send on closed channel" panic.
	mu     sync.Mutex
	ch     chan Event
	closed bool
}

func newStream() *stream {
	return &stream{ch: make(chan Event, eventBuffer)}
}

// send delivers an event unless this generation is already closed.
func (s *stream) send(ctx context.Context, event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}

	select {
	case s.ch <- event:
	case <-ctx.Done():
	}
}

// close closes the channel exactly once. Callers must cancel the owning
// context first — see the note on stream.mu.
func (s *stream) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.ch)
	}
}

// Client connects to an SSE endpoint and emits events on a channel.
//
// A Client is reusable: call Connect again to reconnect after a drop. Each
// Connect returns the channel for that connection, so a reconnect loop must
// re-read it every iteration rather than caching the first one.
type Client struct {
	apiURL    string
	apiToken  string
	projectID string

	mu        sync.Mutex
	cancel    context.CancelFunc
	connected bool
	cur       *stream // the generation the most recent Connect handed out
}

// NewClient creates a new SSE client for the given API URL, token, and project.
func NewClient(apiURL, apiToken, projectID string) *Client {
	return &Client{
		apiURL:    strings.TrimRight(apiURL, "/"),
		apiToken:  apiToken,
		projectID: projectID,
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

// Connect starts an SSE connection and returns the read-only event channel for
// THAT connection. The channel receives parsed events; a "disconnected" event
// is sent when the connection is lost, after which the channel is closed.
//
// Calling Connect again reconnects. It returns a NEW channel and retires the
// previous connection, so a reconnect loop must re-read the channel each time:
//
//	for {
//	    ch := client.Connect(ctx)
//	    for event := range ch { ... }
//	    // ch is closed; loop to reconnect
//	}
//
// Caching the channel from the first Connect reads a closed channel forever.
func (c *Client) Connect(ctx context.Context) <-chan Event {
	ctx, cancel := context.WithCancel(ctx)
	s := newStream()

	c.mu.Lock()
	prevCancel := c.cancel
	prev := c.cur
	c.cancel = cancel
	c.cur = s
	c.connected = false
	c.mu.Unlock()

	// Retire the previous generation before starting another one. Without
	// this its listen goroutine stays parked in scanner.Scan() holding a
	// socket open for the life of the process, and every reconnect leaks one
	// more — a runner reconnecting on a 60s backoff accumulated dozens.
	// Cancel before close, for the reason on stream.mu.
	if prevCancel != nil {
		prevCancel()
	}
	if prev != nil {
		prev.close()
	}

	go c.listen(ctx, s)
	return s.ch
}

// Close stops the SSE connection.
func (c *Client) Close() {
	// Cancel before closing the stream. An in-flight send holds stream.mu
	// until its send completes or its context is done, so on a full channel
	// with no reader, cancelling is the only thing that releases the lock.
	c.mu.Lock()
	cancel := c.cancel
	c.cancel = nil
	c.connected = false
	s := c.cur
	c.cur = nil
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if s != nil {
		s.close()
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
func (c *Client) listen(ctx context.Context, s *stream) {
	defer func() {
		// Only clear connected if this generation is still the current one.
		// A newer Connect may already have taken over, and a slow-exiting
		// predecessor must not report its successor as disconnected.
		c.mu.Lock()
		if c.cur == s {
			c.connected = false
		}
		c.mu.Unlock()

		s.close()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.streamURL(), nil)
	if err != nil {
		s.send(ctx, Event{Type: "disconnected"})
		return
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if c.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.send(ctx, Event{Type: "disconnected"})
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
					s.send(ctx, Event{Type: "error", Data: []byte(fmt.Sprintf(`{"message":%q}`, err.Error()))})
				} else if event != nil {
					// Track connected state, but only while this generation
					// is still current — same reason as the defer.
					if event.Type == "connected" {
						c.mu.Lock()
						if c.cur == s {
							c.connected = true
						}
						c.mu.Unlock()
					}
					s.send(ctx, *event)
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

	s.send(ctx, Event{Type: "disconnected"})
}
