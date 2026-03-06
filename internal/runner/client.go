package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// API Error
// =============================================================================

// APIError represents an HTTP error from the Brain API.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("api error (%d): %s", e.StatusCode, e.Body)
}

// =============================================================================
// API Client
// =============================================================================

// APIClient communicates with the Brain API over HTTP.
type APIClient struct {
	cfg    RunnerConfig
	client *http.Client

	mu          sync.Mutex
	healthCache *APIHealth
	healthAt    time.Time
}

const healthCacheTTL = 10 * time.Second

// NewAPIClient creates a new API client with the given configuration.
func NewAPIClient(cfg RunnerConfig) *APIClient {
	return &APIClient{
		cfg: cfg,
		client: &http.Client{
			Timeout: time.Duration(cfg.APITimeout) * time.Millisecond,
		},
	}
}

// =============================================================================
// Health Check
// =============================================================================

// CheckHealth returns the health status of the Brain API.
// Results are cached for 10 seconds.
func (c *APIClient) CheckHealth(ctx context.Context) (APIHealth, error) {
	c.mu.Lock()
	if c.healthCache != nil && time.Since(c.healthAt) < healthCacheTTL {
		h := *c.healthCache
		c.mu.Unlock()
		return h, nil
	}
	c.mu.Unlock()

	resp, err := c.doRequest(ctx, http.MethodGet, "/health", nil)
	if err != nil {
		unhealthy := APIHealth{Status: "unhealthy"}
		c.mu.Lock()
		c.healthCache = &unhealthy
		c.healthAt = time.Now()
		c.mu.Unlock()
		return unhealthy, nil
	}
	defer resp.Body.Close()

	var health APIHealth
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		unhealthy := APIHealth{Status: "unhealthy"}
		c.mu.Lock()
		c.healthCache = &unhealthy
		c.healthAt = time.Now()
		c.mu.Unlock()
		return unhealthy, nil
	}

	c.mu.Lock()
	c.healthCache = &health
	c.healthAt = time.Now()
	c.mu.Unlock()

	return health, nil
}

// =============================================================================
// Task Queries
// =============================================================================

// ListProjects returns all project IDs known to the Brain API.
func (c *APIClient) ListProjects(ctx context.Context) ([]string, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/api/v1/tasks", nil)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var data types.ProjectListResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode projects: %w", err)
	}
	return data.Projects, nil
}

// GetReadyTasks returns tasks that are ready for execution in a project.
func (c *APIClient) GetReadyTasks(ctx context.Context, projectID string) ([]types.ResolvedTask, error) {
	path := fmt.Sprintf("/api/v1/tasks/%s/ready", projectID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("get ready tasks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var data types.TaskListResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode ready tasks: %w", err)
	}
	return data.Tasks, nil
}

// GetNextTask returns the highest-priority ready task, or nil if none.
func (c *APIClient) GetNextTask(ctx context.Context, projectID string) (*types.ResolvedTask, error) {
	path := fmt.Sprintf("/api/v1/tasks/%s/next", projectID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("get next task: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var data struct {
		Task types.ResolvedTask `json:"task"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode next task: %w", err)
	}
	return &data.Task, nil
}

// GetAllTasks returns all tasks in a project.
func (c *APIClient) GetAllTasks(ctx context.Context, projectID string) ([]types.ResolvedTask, error) {
	path := fmt.Sprintf("/api/v1/tasks/%s", projectID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("get all tasks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var data struct {
		Tasks []types.ResolvedTask `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode all tasks: %w", err)
	}
	return data.Tasks, nil
}

// =============================================================================
// Task Mutations
// =============================================================================

// UpdateTaskStatus changes the status of a task entry.
func (c *APIClient) UpdateTaskStatus(ctx context.Context, taskPath, status string) error {
	encodedPath := encodePathComponent(taskPath)
	apiPath := fmt.Sprintf("/api/v1/entries/%s", encodedPath)

	body := map[string]string{"status": status}
	resp, err := c.doJSONRequest(ctx, http.MethodPatch, apiPath, body)
	if err != nil {
		return fmt.Errorf("update task status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// AppendToTask appends content to a task entry.
func (c *APIClient) AppendToTask(ctx context.Context, taskPath, content string) error {
	encodedPath := encodePathComponent(taskPath)
	apiPath := fmt.Sprintf("/api/v1/entries/%s", encodedPath)

	body := map[string]string{"append": content}
	resp, err := c.doJSONRequest(ctx, http.MethodPatch, apiPath, body)
	if err != nil {
		return fmt.Errorf("append to task: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// GetEntry fetches a brain entry by path.
func (c *APIClient) GetEntry(ctx context.Context, entryPath string) (*types.BrainEntry, error) {
	encodedPath := encodePathComponent(entryPath)
	apiPath := fmt.Sprintf("/api/v1/entries/%s", encodedPath)

	resp, err := c.doRequest(ctx, http.MethodGet, apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("get entry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var data struct {
		Entry types.BrainEntry `json:"entry"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode entry: %w", err)
	}

	return &data.Entry, nil
}

// UpdateEntry updates specific fields of a brain entry.
func (c *APIClient) UpdateEntry(ctx context.Context, entryPath string, updates map[string]interface{}) (*types.BrainEntry, error) {
	encodedPath := encodePathComponent(entryPath)
	apiPath := fmt.Sprintf("/api/v1/entries/%s", encodedPath)

	resp, err := c.doJSONRequest(ctx, http.MethodPatch, apiPath, updates)
	if err != nil {
		return nil, fmt.Errorf("update entry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var data struct {
		Entry types.BrainEntry `json:"entry"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode entry: %w", err)
	}

	return &data.Entry, nil
}

// ClaimTask attempts to claim a task for a runner.
func (c *APIClient) ClaimTask(ctx context.Context, projectID, taskID, runnerID string) (ClaimResult, error) {
	path := fmt.Sprintf("/api/v1/tasks/%s/%s/claim", projectID, taskID)
	body := map[string]string{"runnerId": runnerID}

	resp, err := c.doJSONRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return ClaimResult{}, fmt.Errorf("claim task: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		var data struct {
			ClaimedBy string `json:"claimedBy"`
		}
		json.NewDecoder(resp.Body).Decode(&data)
		return ClaimResult{
			Success:   false,
			TaskID:    taskID,
			ClaimedBy: data.ClaimedBy,
			Message:   "Task already claimed",
		}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return ClaimResult{}, c.readError(resp)
	}

	return ClaimResult{Success: true, TaskID: taskID}, nil
}

// ReleaseTask releases a previously claimed task.
func (c *APIClient) ReleaseTask(ctx context.Context, projectID, taskID string) error {
	path := fmt.Sprintf("/api/v1/tasks/%s/%s/release", projectID, taskID)
	resp, err := c.doRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return fmt.Errorf("release task: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// DeleteEntry deletes a brain entry by path.
func (c *APIClient) DeleteEntry(ctx context.Context, entryPath string) error {
	encodedPath := encodePathComponent(entryPath)
	apiPath := fmt.Sprintf("/api/v1/entries/%s?confirm=true", encodedPath)

	resp, err := c.doRequest(ctx, http.MethodDelete, apiPath, nil)
	if err != nil {
		return fmt.Errorf("delete entry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// GetFeature fetches a feature and its tasks by project ID and feature ID.
func (c *APIClient) GetFeature(ctx context.Context, projectID, featureID string) (*types.FeatureResponse, error) {
	apiPath := fmt.Sprintf("/api/v1/tasks/%s/features/%s", projectID, featureID)

	resp, err := c.doRequest(ctx, http.MethodGet, apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("get feature: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var data struct {
		Feature *types.FeatureResponse `json:"feature"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode feature: %w", err)
	}

	return data.Feature, nil
}

// =============================================================================
// Internal Helpers
// =============================================================================

// doRequest performs an HTTP request with auth headers and context.
func (c *APIClient) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	reqURL := c.cfg.BrainAPIURL + path

	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.cfg.APIToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIToken)
	}

	return c.client.Do(req)
}

// doJSONRequest marshals body to JSON and performs the request.
func (c *APIClient) doJSONRequest(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request body: %w", err)
	}
	return c.doRequest(ctx, method, path, strings.NewReader(string(data)))
}

// readError reads the response body and returns an APIError.
func (c *APIClient) readError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	return &APIError{
		StatusCode: resp.StatusCode,
		Body:       strings.TrimSpace(string(body)),
	}
}

// encodePathComponent percent-encodes a path component like JavaScript's
// encodeURIComponent — encoding slashes, spaces, and other special chars.
func encodePathComponent(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if isUnreserved(c) {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// isUnreserved returns true for RFC 3986 unreserved characters.
func isUnreserved(c byte) bool {
	return (c >= 'A' && c <= 'Z') ||
		(c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') ||
		c == '-' || c == '_' || c == '.' || c == '~'
}
