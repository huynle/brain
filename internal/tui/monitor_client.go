package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ScheduledTaskResult represents a found scheduled task entry.
type ScheduledTaskResult struct {
	Path string
}

// MonitorTaskResult represents a found monitor task.
type MonitorTaskResult struct {
	TaskID string
	Path   string
}

// MonitorClient provides HTTP methods for monitor/scheduled-task API calls.
type MonitorClient struct {
	apiURL string
	client *http.Client
}

// NewMonitorClient creates a new MonitorClient.
func NewMonitorClient(apiURL string) *MonitorClient {
	return &MonitorClient{
		apiURL: apiURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// monitorTag returns the tag string for a monitor entry.
// Format: monitor:<templateID>:feature:<featureID>
func monitorTag(templateID, featureID string) string {
	return fmt.Sprintf("monitor:%s:feature:%s", templateID, featureID)
}

// scheduledTaskTitle returns a human-readable title for a scheduled task.
// Converts kebab-case template ID to Title Case and appends the feature ID.
func scheduledTaskTitle(templateID, featureID string) string {
	// Convert "blocked-inspector" -> "Blocked Inspector"
	parts := strings.Split(templateID, "-")
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return fmt.Sprintf("%s: %s", strings.Join(parts, " "), featureID)
}

// CreateScheduledTask creates a scheduled task entry via POST /api/v1/entries.
// Handles 409 Conflict silently (returns nil, not error).
func (c *MonitorClient) CreateScheduledTask(ctx context.Context, templateID, featureID, project, schedule, prompt string) error {
	body := map[string]interface{}{
		"type":             "task",
		"title":            scheduledTaskTitle(templateID, featureID),
		"content":          prompt,
		"project":          project,
		"schedule":         schedule,
		"schedule_enabled": true,
		"complete_on_idle": true,
		"execution_mode":   "current_branch",
		"feature_id":       featureID,
		"tags":             []string{monitorTag(templateID, featureID)},
	}

	resp, err := c.doJSONRequest(ctx, http.MethodPost, "/api/v1/entries", body)
	if err != nil {
		return fmt.Errorf("create scheduled task: %w", err)
	}
	defer resp.Body.Close()

	// 409 Conflict = already exists, silently succeed
	if resp.StatusCode == http.StatusConflict {
		return nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.readError(resp)
	}

	return nil
}

// FindScheduledTask finds a scheduled task by template and feature ID.
// Returns nil if no matching task is found.
func (c *MonitorClient) FindScheduledTask(ctx context.Context, templateID, featureID string) (*ScheduledTaskResult, error) {
	tag := monitorTag(templateID, featureID)
	path := fmt.Sprintf("/api/v1/entries?type=task&tags=%s&limit=1", tag)

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("find scheduled task: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var data struct {
		Entries []struct {
			Path string `json:"path"`
		} `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode find scheduled task response: %w", err)
	}

	if len(data.Entries) == 0 {
		return nil, nil
	}

	return &ScheduledTaskResult{
		Path: data.Entries[0].Path,
	}, nil
}

// DeleteScheduledTask deletes a scheduled task entry by path.
func (c *MonitorClient) DeleteScheduledTask(ctx context.Context, taskPath string) error {
	encodedPath := encodeMonitorPathComponent(taskPath)
	apiPath := fmt.Sprintf("/api/v1/entries/%s?confirm=true", encodedPath)

	resp, err := c.doRequest(ctx, http.MethodDelete, apiPath, nil)
	if err != nil {
		return fmt.Errorf("delete scheduled task: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}

	return nil
}

// CreateMonitorTask creates a monitor task via POST /api/v1/monitors.
// Handles 409 Conflict silently (returns nil, not error).
func (c *MonitorClient) CreateMonitorTask(ctx context.Context, templateID, featureID, project string) error {
	body := map[string]interface{}{
		"template_id": templateID,
		"scope_type":  "feature",
		"feature_id":  featureID,
		"project":     project,
	}

	resp, err := c.doJSONRequest(ctx, http.MethodPost, "/api/v1/monitors", body)
	if err != nil {
		return fmt.Errorf("create monitor task: %w", err)
	}
	defer resp.Body.Close()

	// 409 Conflict = already exists, silently succeed
	if resp.StatusCode == http.StatusConflict {
		return nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.readError(resp)
	}

	return nil
}

// FindMonitorTask finds a monitor task by template, feature, and project.
// Returns nil if no matching monitor is found.
func (c *MonitorClient) FindMonitorTask(ctx context.Context, templateID, featureID, project string) (*MonitorTaskResult, error) {
	path := fmt.Sprintf("/api/v1/monitors?template_id=%s&feature_id=%s&project=%s", templateID, featureID, project)

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("find monitor task: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var data struct {
		Monitors []struct {
			TaskID string `json:"taskId"`
			Path   string `json:"path"`
		} `json:"monitors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode find monitor task response: %w", err)
	}

	if len(data.Monitors) == 0 {
		return nil, nil
	}

	return &MonitorTaskResult{
		TaskID: data.Monitors[0].TaskID,
		Path:   data.Monitors[0].Path,
	}, nil
}

// DeleteMonitorTask deletes a monitor task by task ID.
func (c *MonitorClient) DeleteMonitorTask(ctx context.Context, taskID string) error {
	apiPath := fmt.Sprintf("/api/v1/monitors/%s", taskID)

	resp, err := c.doRequest(ctx, http.MethodDelete, apiPath, nil)
	if err != nil {
		return fmt.Errorf("delete monitor task: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}

	return nil
}

// =============================================================================
// Internal Helpers
// =============================================================================

// doRequest performs an HTTP request with context.
func (c *MonitorClient) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	reqURL := c.apiURL + path

	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	return c.client.Do(req)
}

// doJSONRequest marshals body to JSON and performs the request.
func (c *MonitorClient) doJSONRequest(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request body: %w", err)
	}
	return c.doRequest(ctx, method, path, strings.NewReader(string(data)))
}

// readError reads the response body and returns an error.
func (c *MonitorClient) readError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("monitor api error (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

// encodeMonitorPathComponent percent-encodes a path component.
func encodeMonitorPathComponent(s string) string {
	var b strings.Builder
	for _, ch := range []byte(s) {
		if isMonitorUnreserved(ch) {
			b.WriteByte(ch)
		} else {
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}

// isMonitorUnreserved returns true for RFC 3986 unreserved characters.
func isMonitorUnreserved(c byte) bool {
	return (c >= 'A' && c <= 'Z') ||
		(c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') ||
		c == '-' || c == '_' || c == '.' || c == '~'
}
