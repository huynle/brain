package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/huynle/brain-api/internal/sse"
	"github.com/huynle/brain-api/internal/types"
)

// reconnectMsg is sent internally to trigger SSE reconnection.
type reconnectMsg struct{}

// SSEClient connects to the Brain API's SSE endpoint and produces
// bubbletea messages from the event stream.
type SSEClient struct {
	apiURL    string
	apiToken  string
	projectID string

	// msgCh is the internal channel used to pass messages from the
	// SSE goroutine to the bubbletea Cmd continuation pattern.
	msgCh chan tea.Msg

	// cancel stops the SSE goroutine.
	cancel context.CancelFunc

	// client is the underlying generic SSE client.
	client *sse.Client
}

// NewSSEClient creates a new SSE client for the given API URL, token, and project.
func NewSSEClient(apiURL, apiToken, projectID string) *SSEClient {
	return &SSEClient{
		apiURL:    strings.TrimRight(apiURL, "/"),
		apiToken:  apiToken,
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

	// Create the underlying SSE client and start listening
	c.client = sse.NewClient(c.apiURL, c.apiToken, c.projectID)
	eventCh := c.client.Connect(sseCtx)

	// Start a goroutine that converts sse.Event → tea.Msg
	go c.bridgeEvents(sseCtx, eventCh)

	// Return a Cmd that waits for the first message
	return c.waitForSSEMsg()
}

// bridgeEvents reads from the generic SSE event channel and converts
// events to bubbletea messages on the internal msgCh.
func (c *SSEClient) bridgeEvents(ctx context.Context, eventCh <-chan sse.Event) {
	for event := range eventCh {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg := c.convertEvent(event)
		if msg == nil {
			continue
		}

		select {
		case c.msgCh <- msg:
		case <-ctx.Done():
			return
		}
	}

	// Channel closed — send disconnect
	select {
	case c.msgCh <- SSEDisconnectedMsg{ProjectID: c.projectID}:
	case <-ctx.Done():
	}
}

// convertEvent converts a generic sse.Event to a bubbletea tea.Msg.
// Returns nil for events that should be ignored.
func (c *SSEClient) convertEvent(event sse.Event) tea.Msg {
	switch event.Type {
	case "connected":
		var data types.SSEConnectedData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			return SSEErrorMsg{Err: fmt.Errorf("parse connected event: %w", err), ProjectID: c.projectID}
		}
		return SSEConnectedMsg{ProjectID: data.ProjectID}

	case "tasks_snapshot":
		var data types.SSETasksSnapshotData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			return SSEErrorMsg{Err: fmt.Errorf("parse tasks_snapshot event: %w", err), ProjectID: c.projectID}
		}
		return TasksUpdatedMsg{
			Tasks:     data.Tasks,
			Stats:     data.Stats,
			ProjectID: data.ProjectID,
		}

	case "error":
		var data types.SSEErrorData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			return SSEErrorMsg{Err: fmt.Errorf("parse error event: %w", err), ProjectID: c.projectID}
		}
		return SSEErrorMsg{Err: fmt.Errorf("%s", data.Message), ProjectID: data.ProjectID}

	case "disconnected":
		return SSEDisconnectedMsg{ProjectID: c.projectID}

	default:
		// Unknown or ignored event types (heartbeat, project_dirty handled by sse.Client)
		return nil
	}
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
	if c.client != nil {
		c.client.Close()
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

// parseSSEEvent parses a collected set of SSE lines into a tea.Msg.
// SSE format: "event: <type>\ndata: <json>\n\n"
// Returns (nil, nil) for events that should be ignored (heartbeat, unknown).
// Returns (nil, error) for parse errors.
//
// NOTE: This function is kept for backward compatibility with existing tests.
// New code should use sse.ParseEvent() from the internal/sse package.
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

	case "project_dirty":
		// Project data changed - the server will follow up with a tasks_snapshot
		// but we ignore this lightweight signal since the snapshot follows immediately.
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
