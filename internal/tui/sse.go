package tui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/huynle/brain-api/internal/types"
)

// reconnectMsg is sent internally to trigger SSE reconnection.
type reconnectMsg struct{}

// SSEClient connects to the Brain API's SSE endpoint and produces
// bubbletea messages from the event stream.
type SSEClient struct {
	apiURL    string
	projectID string

	// msgCh is the internal channel used to pass messages from the
	// SSE goroutine to the bubbletea Cmd continuation pattern.
	msgCh chan tea.Msg

	// cancel stops the SSE goroutine.
	cancel context.CancelFunc
}

// NewSSEClient creates a new SSE client for the given API URL and project.
func NewSSEClient(apiURL, projectID string) *SSEClient {
	return &SSEClient{
		apiURL:    strings.TrimRight(apiURL, "/"),
		projectID: projectID,
		msgCh:     make(chan tea.Msg, 32),
	}
}

// streamURL returns the full SSE stream URL.
func (c *SSEClient) streamURL() string {
	return fmt.Sprintf("%s/api/v1/tasks/%s/stream", c.apiURL, c.projectID)
}

// Connect returns a tea.Cmd that starts the SSE connection and yields
// the first received message. Subsequent messages are delivered via
// the waitForSSEMsg continuation pattern.
func (c *SSEClient) Connect(ctx context.Context) tea.Cmd {
	sseCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	// Start the SSE listener goroutine
	go c.listenSSE(sseCtx, c.msgCh)

	// Return a Cmd that waits for the first message
	return c.waitForSSEMsg()
}

// waitForSSEMsg returns a tea.Cmd that blocks until the next SSE message
// arrives on the internal channel.
func (c *SSEClient) waitForSSEMsg() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-c.msgCh
		if !ok {
			return SSEDisconnectedMsg{ProjectID: c.projectID}
		}
		return msg
	}
}

// WaitForNextMsg returns a tea.Cmd to receive the next SSE message.
// Call this from Update() after processing each SSE message to keep
// the continuation chain going.
func (c *SSEClient) WaitForNextMsg() tea.Cmd {
	return c.waitForSSEMsg()
}

// Stop cancels the SSE connection.
func (c *SSEClient) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}

// Reconnect returns a tea.Cmd that waits for the given delay then
// produces a reconnectMsg to trigger a new connection attempt.
func (c *SSEClient) Reconnect(delay time.Duration) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(delay)
		return reconnectMsg{}
	}
}

// listenSSE connects to the SSE endpoint and sends parsed messages
// to the provided channel. Blocks until context is cancelled or
// the connection is lost. Sends SSEDisconnectedMsg on connection loss.
func (c *SSEClient) listenSSE(ctx context.Context, msgCh chan<- tea.Msg) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.streamURL(), nil)
	if err != nil {
		select {
		case msgCh <- SSEDisconnectedMsg{ProjectID: c.projectID}:
		case <-ctx.Done():
		}
		return
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Connection refused, timeout, etc.
		select {
		case msgCh <- SSEDisconnectedMsg{ProjectID: c.projectID}:
		case <-ctx.Done():
		}
		return
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
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
				msg, err := parseSSEEvent(lines)
				if err != nil {
					// Parse error - send as SSEErrorMsg
					select {
					case msgCh <- SSEErrorMsg{Err: err}:
					case <-ctx.Done():
						return
					}
				} else if msg != nil {
					// Valid message (nil means ignored, e.g. heartbeat)
					select {
					case msgCh <- msg:
					case <-ctx.Done():
						return
					}
				}
				lines = nil
			}
		} else {
			lines = append(lines, line)
		}
	}

	// Scanner finished - connection lost or server closed
	// Check if it was a context cancellation (graceful shutdown)
	if ctx.Err() != nil {
		return
	}

	select {
	case msgCh <- SSEDisconnectedMsg{ProjectID: c.projectID}:
	case <-ctx.Done():
	}
}

// parseSSEEvent parses a collected set of SSE lines into a tea.Msg.
// SSE format: "event: <type>\ndata: <json>\n\n"
// Returns (nil, nil) for events that should be ignored (heartbeat, unknown).
// Returns (nil, error) for parse errors.
func parseSSEEvent(lines []string) (tea.Msg, error) {
	var eventType string
	var dataStr string

	for _, line := range lines {
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			dataStr = strings.TrimPrefix(line, "data: ")
		}
	}

	// Missing event or data line - ignore
	if eventType == "" || dataStr == "" {
		return nil, nil
	}

	switch eventType {
	case "connected":
		// Validate JSON is parseable
		var data types.SSEConnectedData
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			return nil, fmt.Errorf("parse connected event: %w", err)
		}
		return SSEConnectedMsg{ProjectID: data.ProjectID}, nil

	case "tasks_snapshot":
		var data types.SSETasksSnapshotData
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			return nil, fmt.Errorf("parse tasks_snapshot event: %w", err)
		}
		return TasksUpdatedMsg{
			Tasks:     data.Tasks,
			Stats:     data.Stats,
			ProjectID: data.ProjectID,
		}, nil

	case "heartbeat":
		// Heartbeat is ignored (used for keepalive only)
		return nil, nil

	case "error":
		var data types.SSEErrorData
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			return nil, fmt.Errorf("parse error event: %w", err)
		}
		return SSEErrorMsg{Err: fmt.Errorf("%s", data.Message), ProjectID: data.ProjectID}, nil

	default:
		// Unknown event type - ignore
		return nil, nil
	}
}
