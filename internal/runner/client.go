package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

	resp, err := c.doRequest(ctx, http.MethodGet, "/api/v1/health", nil)
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
// Optional featureIDs filter results to specific features.
func (c *APIClient) GetReadyTasks(ctx context.Context, projectID string, featureIDs ...string) ([]types.ResolvedTask, error) {
	path := fmt.Sprintf("/api/v1/tasks/%s/ready", projectID)
	if len(featureIDs) > 0 {
		params := url.Values{}
		for _, fid := range featureIDs {
			params.Add("feature_id", fid)
		}
		path += "?" + params.Encode()
	}
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
// Optional featureIDs filter results to specific features.
func (c *APIClient) GetNextTask(ctx context.Context, projectID string, featureIDs ...string) (*types.ResolvedTask, error) {
	path := fmt.Sprintf("/api/v1/tasks/%s/next", projectID)
	if len(featureIDs) > 0 {
		params := url.Values{}
		for _, fid := range featureIDs {
			params.Add("feature_id", fid)
		}
		path += "?" + params.Encode()
	}
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

	// Read body first to check for null/empty response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read next task body: %w", err)
	}

	// Handle null, empty, or no-task responses
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || trimmed == "null" || trimmed == "{}" {
		return nil, nil
	}

	var task types.ResolvedTask
	if err := json.Unmarshal(body, &task); err != nil {
		return nil, fmt.Errorf("decode next task: %w", err)
	}

	// Extra safety: if the decoded task has no ID, treat as no task
	if task.ID == "" {
		return nil, nil
	}

	return &task, nil
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
	// Use the /metadata endpoint which updates both the metadata JSON and the
	// status DB column directly, bypassing the file-read/write cycle that
	// can clobber the status back to the frontmatter's original value.
	return c.UpdateMetadata(ctx, taskPath, map[string]interface{}{
		"status": status,
	})
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
// The brain API returns the entry as a flat top-level JSON object (not wrapped).
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

	var entry types.BrainEntry
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		return nil, fmt.Errorf("decode entry: %w", err)
	}

	return &entry, nil
}

// UpdateEntry updates specific fields of a brain entry.
// The brain API returns the updated entry as a flat top-level JSON object (not wrapped).
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

	var entry types.BrainEntry
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		return nil, fmt.Errorf("decode entry: %w", err)
	}

	return &entry, nil
}

// UpdateMetadata merges fields into the entry's metadata JSON column.
// This uses the /metadata suffix endpoint which works directly on SQLite
// without touching the filesystem.
func (c *APIClient) UpdateMetadata(ctx context.Context, entryPath string, fields map[string]interface{}) error {
	encodedPath := encodePathComponent(entryPath)
	apiPath := fmt.Sprintf("/api/v1/entries/%s/metadata", encodedPath)

	resp, err := c.doJSONRequest(ctx, http.MethodPatch, apiPath, fields)
	if err != nil {
		return fmt.Errorf("update metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}

	return nil
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

// MoveEntry moves a brain entry to a different project.
func (c *APIClient) MoveEntry(ctx context.Context, entryPath, targetProject string) (*types.MoveResult, error) {
	encodedPath := encodePathComponent(entryPath)
	apiPath := fmt.Sprintf("/api/v1/entries/%s/move", encodedPath)

	body := types.MoveEntryRequest{Project: targetProject}
	resp, err := c.doJSONRequest(ctx, http.MethodPost, apiPath, body)
	if err != nil {
		return nil, fmt.Errorf("move entry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var result types.MoveResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode move result: %w", err)
	}

	return &result, nil
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

	var feature types.FeatureResponse
	if err := json.NewDecoder(resp.Body).Decode(&feature); err != nil {
		return nil, fmt.Errorf("decode feature: %w", err)
	}

	return &feature, nil
}

// GetTasksByFeature fetches all tasks belonging to a feature within a project.
// Uses the dedicated feature endpoint which correctly filters by feature ID.
func (c *APIClient) GetTasksByFeature(ctx context.Context, projectID, featureID string) ([]types.ResolvedTask, error) {
	featureResp, err := c.GetFeature(ctx, projectID, featureID)
	if err != nil {
		return nil, fmt.Errorf("get tasks by feature: %w", err)
	}
	if featureResp == nil {
		return nil, nil
	}
	return featureResp.Feature.Tasks, nil
}

// =============================================================================
// Entry CRUD (CLI Methods)
// =============================================================================

// CreateEntry creates a new brain entry via POST /api/v1/entries.
func (c *APIClient) CreateEntry(ctx context.Context, req types.CreateEntryRequest) (*types.CreateEntryResponse, error) {
	resp, err := c.doJSONRequest(ctx, http.MethodPost, "/api/v1/entries", req)
	if err != nil {
		return nil, fmt.Errorf("create entry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, c.readError(resp)
	}

	var result types.CreateEntryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode create entry response: %w", err)
	}
	return &result, nil
}

// SearchEntries searches for entries via POST /api/v1/search.
func (c *APIClient) SearchEntries(ctx context.Context, req types.SearchRequest) (*types.SearchResponse, error) {
	resp, err := c.doJSONRequest(ctx, http.MethodPost, "/api/v1/search", req)
	if err != nil {
		return nil, fmt.Errorf("search entries: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var result types.SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}
	return &result, nil
}

// ListEntries lists entries with optional filters via GET /api/v1/entries.
func (c *APIClient) ListEntries(ctx context.Context, params map[string]string) (*types.ListEntriesResponse, error) {
	path := "/api/v1/entries"
	if len(params) > 0 {
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		path += "?" + q.Encode()
	}

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("list entries: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var result types.ListEntriesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode list entries response: %w", err)
	}
	return &result, nil
}

// GetEntryRaw returns the raw markdown body of an entry (no JSON wrapping).
// Uses Accept: text/markdown header; metadata is available in X-Brain-* response headers.
func (c *APIClient) GetEntryRaw(ctx context.Context, entryPath string) (string, http.Header, error) {
	encodedPath := encodePathComponent(entryPath)
	apiPath := fmt.Sprintf("/api/v1/entries/%s", encodedPath)

	resp, err := c.doRequestWithHeaders(ctx, http.MethodGet, apiPath, nil, map[string]string{
		"Accept": "text/markdown",
	})
	if err != nil {
		return "", nil, fmt.Errorf("get entry raw: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, c.readError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("read entry raw body: %w", err)
	}
	return string(body), resp.Header, nil
}

// GetEntryFull returns the full file content (YAML frontmatter + body).
// Uses Accept: text/x-brain-full header.
func (c *APIClient) GetEntryFull(ctx context.Context, entryPath string) (string, error) {
	encodedPath := encodePathComponent(entryPath)
	apiPath := fmt.Sprintf("/api/v1/entries/%s", encodedPath)

	resp, err := c.doRequestWithHeaders(ctx, http.MethodGet, apiPath, nil, map[string]string{
		"Accept": "text/x-brain-full",
	})
	if err != nil {
		return "", fmt.Errorf("get entry full: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", c.readError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read entry full body: %w", err)
	}
	return string(body), nil
}

// UpdateEntryRaw replaces entry content with raw markdown body.
// Uses Content-Type: text/markdown header.
func (c *APIClient) UpdateEntryRaw(ctx context.Context, entryPath string, content string) error {
	encodedPath := encodePathComponent(entryPath)
	apiPath := fmt.Sprintf("/api/v1/entries/%s", encodedPath)

	resp, err := c.doRequestWithHeaders(ctx, http.MethodPut, apiPath, strings.NewReader(content), map[string]string{
		"Content-Type": "text/markdown",
	})
	if err != nil {
		return fmt.Errorf("update entry raw: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// UpdateEntryFull replaces entry content and metadata from full frontmatter+body file.
// Uses Content-Type: text/x-brain-full header.
func (c *APIClient) UpdateEntryFull(ctx context.Context, entryPath string, fullContent string) error {
	encodedPath := encodePathComponent(entryPath)
	apiPath := fmt.Sprintf("/api/v1/entries/%s", encodedPath)

	resp, err := c.doRequestWithHeaders(ctx, http.MethodPut, apiPath, strings.NewReader(fullContent), map[string]string{
		"Content-Type": "text/x-brain-full",
	})
	if err != nil {
		return fmt.Errorf("update entry full: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// =============================================================================
// Runner Pause/Resume
// =============================================================================

// PauseProject pauses task execution for a specific project.
func (c *APIClient) PauseProject(ctx context.Context, projectID string) error {
	path := fmt.Sprintf("/api/v1/tasks/runner/pause/%s", projectID)
	resp, err := c.doRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return fmt.Errorf("pause project: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// ResumeProject resumes task execution for a specific project.
func (c *APIClient) ResumeProject(ctx context.Context, projectID string) error {
	path := fmt.Sprintf("/api/v1/tasks/runner/resume/%s", projectID)
	resp, err := c.doRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return fmt.Errorf("resume project: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// PauseAll pauses task execution for all projects.
func (c *APIClient) PauseAll(ctx context.Context) error {
	resp, err := c.doRequest(ctx, http.MethodPost, "/api/v1/tasks/runner/pause", nil)
	if err != nil {
		return fmt.Errorf("pause all: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// ResumeAll resumes task execution for all projects.
func (c *APIClient) ResumeAll(ctx context.Context) error {
	resp, err := c.doRequest(ctx, http.MethodPost, "/api/v1/tasks/runner/resume", nil)
	if err != nil {
		return fmt.Errorf("resume all: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// GetRunnerStatus returns the current runner status including pause state.
func (c *APIClient) GetRunnerStatus(ctx context.Context) (*types.RunnerStatusResponse, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/api/v1/tasks/runner/status", nil)
	if err != nil {
		return nil, fmt.Errorf("get runner status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var status types.RunnerStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decode runner status: %w", err)
	}
	return &status, nil
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

// doRequestWithHeaders performs an HTTP request with custom headers, overriding defaults.
// Headers provided in the overrides map replace the default Content-Type and Accept headers.
func (c *APIClient) doRequestWithHeaders(ctx context.Context, method, path string, body io.Reader, headers map[string]string) (*http.Response, error) {
	reqURL := c.cfg.BrainAPIURL + path

	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Set defaults first
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.cfg.APIToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIToken)
	}

	// Override with custom headers
	for k, v := range headers {
		req.Header.Set(k, v)
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

// encodePathComponent encodes a brain entry path for use in URLs.
// Brain paths like "projects/test1/task/abc.md" should keep slashes intact
// since the Go server uses wildcard routes (/*). Only encode special characters
// within each path segment (spaces, etc.).
func encodePathComponent(s string) string {
	// Split by slash, encode each segment, rejoin
	parts := strings.Split(s, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// isUnreserved returns true for RFC 3986 unreserved characters.
func isUnreserved(c byte) bool {
	return (c >= 'A' && c <= 'Z') ||
		(c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') ||
		c == '-' || c == '_' || c == '.' || c == '~'
}
