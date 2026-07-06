package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

// buildTaskQueryParams encodes TaskFetchOptions into URL query parameters.
func buildTaskQueryParams(opts *TaskFetchOptions) string {
	if opts == nil {
		return ""
	}
	params := url.Values{}
	for _, fid := range opts.FeatureIDs {
		params.Add("feature_id", fid)
	}
	if len(opts.Executors) > 0 {
		params.Set("executors", strings.Join(opts.Executors, ","))
	}
	if opts.RunnerID != "" {
		params.Set("runner_id", opts.RunnerID)
	}
	if opts.GeneratedByPrefix != "" {
		params.Set("generated_by_prefix", opts.GeneratedByPrefix)
	}
	if encoded := params.Encode(); encoded != "" {
		return "?" + encoded
	}
	return ""
}

// GetReadyTasks returns tasks that are ready for execution in a project.
// Pass nil opts for no filtering (backward compatible).
func (c *APIClient) GetReadyTasks(ctx context.Context, projectID string, opts *TaskFetchOptions) ([]types.ResolvedTask, error) {
	path := fmt.Sprintf("/api/v1/tasks/%s/ready", projectID) + buildTaskQueryParams(opts)
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
// Pass nil opts for no filtering (backward compatible).
func (c *APIClient) GetNextTask(ctx context.Context, projectID string, opts *TaskFetchOptions) (*types.ResolvedTask, error) {
	path := fmt.Sprintf("/api/v1/tasks/%s/next", projectID) + buildTaskQueryParams(opts)
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

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
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

// =============================================================================
// Goal Automation API
//
// These methods call the dedicated /api/v1/goals routes. A goal is an
// automation entry whose trigger/action drive a deterministic in-process
// reconcile loop. See internal/types/automation.go for the DTOs.
// =============================================================================

// CreateGoal creates a new goal automation. Returns the created goal summary.
func (c *APIClient) CreateGoal(ctx context.Context, req types.CreateGoalRequest) (*types.GoalSummary, error) {
	resp, err := c.doJSONRequest(ctx, http.MethodPost, "/api/v1/goals", req)
	if err != nil {
		return nil, fmt.Errorf("create goal: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, c.readError(resp)
	}

	var goal types.GoalSummary
	if err := json.NewDecoder(resp.Body).Decode(&goal); err != nil {
		return nil, fmt.Errorf("decode goal: %w", err)
	}

	return &goal, nil
}

// UpdateGoal merges the provided fields onto an existing goal automation.
// Returns the updated goal summary.
func (c *APIClient) UpdateGoal(ctx context.Context, goalID string, req types.UpdateGoalRequest) (*types.GoalSummary, error) {
	path := fmt.Sprintf("/api/v1/goals/%s", goalID)

	resp, err := c.doJSONRequest(ctx, http.MethodPatch, path, req)
	if err != nil {
		return nil, fmt.Errorf("update goal: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var goal types.GoalSummary
	if err := json.NewDecoder(resp.Body).Decode(&goal); err != nil {
		return nil, fmt.Errorf("decode goal: %w", err)
	}

	return &goal, nil
}

// ListGoals returns goal automations, optionally filtered by project and
// feature ID. Empty filter values are omitted from the query string.
func (c *APIClient) ListGoals(ctx context.Context, project, featureID string) ([]types.GoalSummary, error) {
	path := "/api/v1/goals"
	q := url.Values{}
	if project != "" {
		q.Set("project", project)
	}
	if featureID != "" {
		q.Set("feature_id", featureID)
	}
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("list goals: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var result struct {
		Goals []types.GoalSummary `json:"goals"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode goals: %w", err)
	}

	return result.Goals, nil
}

// RunGoal triggers a manual reconcile of the goal and returns the audit record
// for the resulting decision.
func (c *APIClient) RunGoal(ctx context.Context, goalID string) (*types.GoalReconcileAudit, error) {
	path := fmt.Sprintf("/api/v1/goals/%s/run", goalID)

	resp, err := c.doRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, fmt.Errorf("run goal: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var audit types.GoalReconcileAudit
	if err := json.NewDecoder(resp.Body).Decode(&audit); err != nil {
		return nil, fmt.Errorf("decode goal audit: %w", err)
	}

	return &audit, nil
}

// GoalProgress returns goal-scoped linked-task progress.
func (c *APIClient) GoalProgress(ctx context.Context, goalID string) (*types.GoalProgressResponse, error) {
	path := fmt.Sprintf("/api/v1/goals/%s/progress", goalID)

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("goal progress: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var progress types.GoalProgressResponse
	if err := json.NewDecoder(resp.Body).Decode(&progress); err != nil {
		return nil, fmt.Errorf("decode goal progress: %w", err)
	}

	return &progress, nil
}

// GoalAudit returns the goal's reconcile audit history. When limit > 0 it is
// passed through as the limit query parameter; otherwise no limit is sent.
func (c *APIClient) GoalAudit(ctx context.Context, goalID string, limit int) ([]types.GoalReconcileAudit, error) {
	path := fmt.Sprintf("/api/v1/goals/%s/audit", goalID)
	if limit > 0 {
		q := url.Values{}
		q.Set("limit", fmt.Sprintf("%d", limit))
		path += "?" + q.Encode()
	}

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("goal audit: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var result struct {
		Audit []types.GoalReconcileAudit `json:"audit"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode goal audit: %w", err)
	}

	return result.Audit, nil
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

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
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

// AckDispatch acknowledges receipt of a pushed dispatch command.
func (c *APIClient) AckDispatch(ctx context.Context, runnerID, projectID, taskID, leaseID string) (*types.DispatchAckResponse, error) {
	path := fmt.Sprintf("/api/v1/tasks/runners/%s/dispatch/ack", runnerID)
	body := types.DispatchAckRequest{LeaseID: leaseID, ProjectID: projectID, TaskID: taskID}
	resp, err := c.doJSONRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, fmt.Errorf("ack dispatch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}
	var result types.DispatchAckResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode dispatch ack response: %w", err)
	}
	return &result, nil
}

// RejectDispatch rejects a pushed dispatch command with a structured reason.
func (c *APIClient) RejectDispatch(ctx context.Context, runnerID, projectID, taskID, leaseID string, reason types.DispatchRejectReason) (*types.DispatchRejectResponse, error) {
	path := fmt.Sprintf("/api/v1/tasks/runners/%s/dispatch/reject", runnerID)
	body := types.DispatchRejectRequest{LeaseID: leaseID, ProjectID: projectID, TaskID: taskID, Reason: reason}
	resp, err := c.doJSONRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, fmt.Errorf("reject dispatch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}
	var result types.DispatchRejectResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode dispatch reject response: %w", err)
	}
	return &result, nil
}

// ReleaseDispatch explicitly releases/finalizes a pushed dispatch lease.
func (c *APIClient) ReleaseDispatch(ctx context.Context, runnerID, projectID, taskID string) (*types.DispatchReleaseResponse, error) {
	path := fmt.Sprintf("/api/v1/tasks/runners/%s/dispatch/release", runnerID)
	body := map[string]string{"projectId": projectID, "taskId": taskID}
	resp, err := c.doJSONRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, fmt.Errorf("release dispatch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}
	var result types.DispatchReleaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode dispatch release response: %w", err)
	}
	return &result, nil
}

// RenewClaim extends the lease on a claimed task.
// Returns nil on success, or an error if the claim doesn't exist, is expired,
// or is owned by a different runner. The caller should treat any error as a
// signal to abort the task.
func (c *APIClient) RenewClaim(ctx context.Context, projectID, taskID, runnerID string) error {
	path := fmt.Sprintf("/api/v1/tasks/%s/%s/renew", projectID, taskID)
	body := map[string]string{"runnerId": runnerID}

	resp, err := c.doJSONRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return fmt.Errorf("renew claim: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return c.readError(resp)
	}
	return nil
}

// ReleaseTask releases a previously claimed task.
// The runnerID must match the runner that originally claimed the task.
func (c *APIClient) ReleaseTask(ctx context.Context, projectID, taskID, runnerID string) error {
	path := fmt.Sprintf("/api/v1/tasks/%s/%s/release", projectID, taskID)
	body := map[string]string{"runnerId": runnerID}

	resp, err := c.doJSONRequest(ctx, http.MethodPost, path, body)
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

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return c.readError(resp)
	}
	return nil
}

// UploadAttachment uploads a local file as multipart/form-data and returns its metadata.
func (c *APIClient) UploadAttachment(ctx context.Context, projectID, filePath string, metadata map[string]string) (*types.Attachment, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read attachment file: %w", err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("project_id", projectID); err != nil {
		return nil, fmt.Errorf("write project field: %w", err)
	}
	if len(metadata) > 0 {
		raw, err := json.Marshal(metadata)
		if err != nil {
			return nil, fmt.Errorf("marshal attachment metadata: %w", err)
		}
		if err := writer.WriteField("metadata", string(raw)); err != nil {
			return nil, fmt.Errorf("write metadata field: %w", err)
		}
	}
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, fmt.Errorf("create file field: %w", err)
	}
	if _, err := part.Write(content); err != nil {
		return nil, fmt.Errorf("write file field: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	resp, err := c.doRequestWithHeaders(ctx, http.MethodPost, "/api/v1/attachments", body, map[string]string{"Content-Type": writer.FormDataContentType(), "Accept": "application/json"})
	if err != nil {
		return nil, fmt.Errorf("upload attachment: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}
	var result types.CreateAttachmentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode attachment upload: %w", err)
	}
	return &result.Attachment, nil
}

// GetAttachment fetches attachment metadata by ID within a project.
func (c *APIClient) GetAttachment(ctx context.Context, projectID, attachmentID string) (*types.Attachment, error) {
	path := "/api/v1/attachments/" + url.PathEscape(attachmentID) + "?project_id=" + url.QueryEscape(projectID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("get attachment: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}
	var attachment types.Attachment
	if err := json.NewDecoder(resp.Body).Decode(&attachment); err != nil {
		return nil, fmt.Errorf("decode attachment: %w", err)
	}
	return &attachment, nil
}

// ListAttachments lists all project attachments.
func (c *APIClient) ListAttachments(ctx context.Context, projectID string) (*types.ListAttachmentsResponse, error) {
	path := "/api/v1/attachments?project_id=" + url.QueryEscape(projectID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}
	var result types.ListAttachmentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode attachments: %w", err)
	}
	return &result, nil
}

// AttachEntryAttachment links an existing attachment to an entry.
func (c *APIClient) AttachEntryAttachment(ctx context.Context, projectID, entryID string, attachment types.AttachmentReference) (*types.AttachEntryAttachmentResponse, error) {
	path := "/api/v1/entries/" + encodePathComponent(entryID) + "/attachments?project_id=" + url.QueryEscape(projectID)
	resp, err := c.doJSONRequest(ctx, http.MethodPost, path, types.AttachEntryAttachmentRequest{Attachment: attachment})
	if err != nil {
		return nil, fmt.Errorf("attach entry attachment: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}
	var result types.AttachEntryAttachmentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode entry attachments: %w", err)
	}
	return &result, nil
}

// ListEntryAttachments lists attachments linked to an entry.
func (c *APIClient) ListEntryAttachments(ctx context.Context, projectID, entryID string) (*types.AttachEntryAttachmentResponse, error) {
	path := "/api/v1/entries/" + encodePathComponent(entryID) + "/attachments?project_id=" + url.QueryEscape(projectID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("list entry attachments: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}
	var result types.AttachEntryAttachmentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode entry attachments: %w", err)
	}
	return &result, nil
}

// DetachEntryAttachment removes an attachment link from an entry.
func (c *APIClient) DetachEntryAttachment(ctx context.Context, projectID, entryID, attachmentID, role string) (*types.AttachEntryAttachmentResponse, error) {
	params := url.Values{"project_id": {projectID}}
	if role != "" {
		params.Set("role", role)
	}
	path := "/api/v1/entries/" + encodePathComponent(entryID) + "/attachments/" + url.PathEscape(attachmentID) + "?" + params.Encode()
	resp, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, fmt.Errorf("detach entry attachment: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}
	var result types.AttachEntryAttachmentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode entry attachments: %w", err)
	}
	return &result, nil
}

// DeleteAttachment deletes an unreferenced attachment from a project.
func (c *APIClient) DeleteAttachment(ctx context.Context, projectID, attachmentID string) error {
	path := "/api/v1/attachments/" + url.PathEscape(attachmentID) + "?project_id=" + url.QueryEscape(projectID)
	resp, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("delete attachment: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// ExtractAttachmentText triggers server-side media-to-text extraction for an attachment.
func (c *APIClient) ExtractAttachmentText(ctx context.Context, projectID, attachmentID string) (*types.AttachmentExtractionResult, error) {
	path := "/api/v1/attachments/" + url.PathEscape(attachmentID) + "/extract?project_id=" + url.QueryEscape(projectID)
	resp, err := c.doRequestWithClient(ctx, c.longRunningClient(), http.MethodPost, path, nil)
	if err != nil {
		return nil, fmt.Errorf("extract attachment text: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}
	var result types.AttachmentExtractionResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode attachment extraction: %w", err)
	}
	return &result, nil
}

// DownloadAttachment returns metadata and exact original bytes, verifying SHA256 when present.
func (c *APIClient) DownloadAttachment(ctx context.Context, projectID, attachmentID string) (*types.Attachment, []byte, error) {
	attachment, err := c.GetAttachment(ctx, projectID, attachmentID)
	if err != nil {
		return nil, nil, err
	}
	path := "/api/v1/attachments/" + url.PathEscape(attachmentID) + "/content?project_id=" + url.QueryEscape(projectID)
	resp, err := c.doRequestWithHeaders(ctx, http.MethodGet, path, nil, map[string]string{"Accept": "application/octet-stream"})
	if err != nil {
		return nil, nil, fmt.Errorf("download attachment: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, c.readError(resp)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read attachment bytes: %w", err)
	}
	if attachment.SHA256 != "" {
		actual := sha256BytesHex(data)
		if !strings.EqualFold(actual, attachment.SHA256) {
			return nil, nil, fmt.Errorf("sha256 mismatch: got %s want %s", actual, attachment.SHA256)
		}
	}
	return attachment, data, nil
}

func sha256BytesHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
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

// GetFeatures returns all features and their tasks for a project.
func (c *APIClient) GetFeatures(ctx context.Context, projectID string) ([]types.Feature, error) {
	apiPath := fmt.Sprintf("/api/v1/tasks/%s/features", projectID)

	resp, err := c.doRequest(ctx, http.MethodGet, apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("get features: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var data types.FeatureListResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode features: %w", err)
	}

	return data.Features, nil
}

// AssignFeatureToRunner manually assigns or reassigns a project feature to a runner.
func (c *APIClient) AssignFeatureToRunner(ctx context.Context, projectID, featureID string, req types.FeatureAssignmentRequest) (*types.FeatureAssignmentResponse, error) {
	apiPath := fmt.Sprintf("/api/v1/tasks/%s/features/%s/assignment", projectID, featureID)

	resp, err := c.doJSONRequest(ctx, http.MethodPut, apiPath, req)
	if err != nil {
		return nil, fmt.Errorf("assign feature to runner: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var result types.FeatureAssignmentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode feature assignment: %w", err)
	}
	return &result, nil
}

// ClearFeatureAssignment manually clears a project feature assignment.
func (c *APIClient) ClearFeatureAssignment(ctx context.Context, projectID, featureID string) (*types.FeatureAssignmentResponse, error) {
	apiPath := fmt.Sprintf("/api/v1/tasks/%s/features/%s/assignment/clear", projectID, featureID)
	req := types.ClearFeatureAssignmentRequest{Intent: "clear"}

	resp, err := c.doJSONRequest(ctx, http.MethodPost, apiPath, req)
	if err != nil {
		return nil, fmt.Errorf("clear feature assignment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var result types.FeatureAssignmentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode feature assignment clear: %w", err)
	}
	return &result, nil
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

// BackfillEmbeddings generates embeddings for matching entries via POST /api/v1/embeddings/backfill.
func (c *APIClient) BackfillEmbeddings(ctx context.Context, req types.EmbeddingBackfillRequest) (*types.EmbeddingBackfillResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding backfill request: %w", err)
	}
	resp, err := c.doRequestWithClient(ctx, c.longRunningClient(), http.MethodPost, "/api/v1/embeddings/backfill", strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("backfill embeddings: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}
	var result types.EmbeddingBackfillResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode embedding backfill response: %w", err)
	}
	return &result, nil
}

// BackfillAttachmentExtraction extracts derived text for project attachments via POST /api/v1/attachments/backfill/extraction.
func (c *APIClient) BackfillAttachmentExtraction(ctx context.Context, projectID string, req types.AttachmentExtractionBackfillRequest) (*types.AttachmentExtractionBackfillResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal attachment extraction backfill request: %w", err)
	}
	path := "/api/v1/attachments/backfill/extraction?project_id=" + url.QueryEscape(projectID)
	resp, err := c.doRequestWithClient(ctx, c.longRunningClient(), http.MethodPost, path, strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("backfill attachment extraction: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}
	var result types.AttachmentExtractionBackfillResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode attachment extraction backfill response: %w", err)
	}
	return &result, nil
}

func (c *APIClient) longRunningClient() *http.Client {
	timeout := time.Duration(c.cfg.APITimeout) * time.Millisecond
	if timeout < 30*time.Minute {
		timeout = 30 * time.Minute
	}
	return &http.Client{Timeout: timeout}
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

// BulkUpdate applies updates to multiple entries in a single request.
func (c *APIClient) BulkUpdate(ctx context.Context, req types.BulkUpdateRequest) (*types.BulkUpdateResponse, error) {
	resp, err := c.doJSONRequest(ctx, http.MethodPost, "/api/v1/entries/bulk-update", req)
	if err != nil {
		return nil, fmt.Errorf("bulk update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var result types.BulkUpdateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode bulk update response: %w", err)
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

	resp, err := c.doRequestWithHeaders(ctx, http.MethodPatch, apiPath, strings.NewReader(content), map[string]string{
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

	resp, err := c.doRequestWithHeaders(ctx, http.MethodPatch, apiPath, strings.NewReader(fullContent), map[string]string{
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
// Event Forwarding
// =============================================================================

// PostEvents sends a batch of events to the Brain API.
// Implements the EventPoster interface for EventForwarder.
func (c *APIClient) PostEvents(ctx context.Context, events []types.Event) error {
	resp, err := c.doJSONRequest(ctx, http.MethodPost, "/api/v1/events", events)
	if err != nil {
		return fmt.Errorf("post events: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// EmitEvent publishes an event into the event bus via POST /api/v1/events/emit.
func (c *APIClient) EmitEvent(ctx context.Context, eventType string, payload map[string]any, dedupKey string) error {
	body := map[string]any{"type": eventType}
	if payload != nil {
		body["payload"] = payload
	}
	if dedupKey != "" {
		body["dedup_key"] = dedupKey
	}

	resp, err := c.doJSONRequest(ctx, http.MethodPost, "/api/v1/events/emit", body)
	if err != nil {
		return fmt.Errorf("emit event: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return c.readError(resp)
	}
	return nil
}

// =============================================================================
// Runner Registration & Heartbeat
// =============================================================================

// RegisterRunner registers this runner with the Brain API server.
// Returns the RunnerInfo on success or an error. A non-2xx response is returned
// as an *APIError so callers can decide whether to treat it as fatal.
func (c *APIClient) RegisterRunner(ctx context.Context, req types.RunnerRegistration) (*types.RunnerInfo, error) {
	resp, err := c.doJSONRequest(ctx, http.MethodPost, "/api/v1/runners/register", req)
	if err != nil {
		return nil, fmt.Errorf("register runner: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, c.readError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read register response body: %w", err)
	}

	var info types.RunnerInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("decode register response: %w", err)
	}
	if info.RunnerID == "" {
		var wrapped struct {
			Runner types.RunnerInfo `json:"runner"`
		}
		if err := json.Unmarshal(body, &wrapped); err != nil {
			return nil, fmt.Errorf("decode register response: %w", err)
		}
		if wrapped.Runner.RunnerID != "" {
			info = wrapped.Runner
		}
	}
	return &info, nil
}

// SendHeartbeat sends a heartbeat for this runner to the Brain API server.
func (c *APIClient) SendHeartbeat(ctx context.Context, runnerID string, req types.RunnerHeartbeatRequest) error {
	path := fmt.Sprintf("/api/v1/runners/%s/heartbeat", runnerID)
	resp, err := c.doJSONRequest(ctx, http.MethodPost, path, req)
	if err != nil {
		return fmt.Errorf("send heartbeat: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// UpsertInstance reports an OpenCode instance to the Brain API instance registry.
func (c *APIClient) UpsertInstance(ctx context.Context, runnerID string, inst types.OpencodeInstance) error {
	path := fmt.Sprintf("/api/v1/runners/%s/instances/%s", runnerID, inst.InstanceID)
	resp, err := c.doJSONRequest(ctx, http.MethodPut, path, inst)
	if err != nil {
		return fmt.Errorf("upsert instance: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// DeleteInstance removes an OpenCode instance from the Brain API instance registry.
func (c *APIClient) DeleteInstance(ctx context.Context, runnerID, instanceID string) error {
	path := fmt.Sprintf("/api/v1/runners/%s/instances/%s", runnerID, instanceID)
	resp, err := c.doJSONRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("delete instance: %w", err)
	}
	defer resp.Body.Close()

	// 404 is acceptable — the heartbeat reconcile may have already removed it.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return c.readError(resp)
	}
	return nil
}

// DeregisterRunner removes this runner from the Brain API server.
func (c *APIClient) DeregisterRunner(ctx context.Context, runnerID string) error {
	path := fmt.Sprintf("/api/v1/runners/%s/deregister", runnerID)
	resp, err := c.doJSONRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return fmt.Errorf("deregister runner: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// ListRunners fetches all registered runners from the Brain API.
func (c *APIClient) ListRunners(ctx context.Context) (*types.RunnerListResponse, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/api/v1/runners", nil)
	if err != nil {
		return nil, fmt.Errorf("list runners: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var result types.RunnerListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode runners response: %w", err)
	}
	return &result, nil
}

// GetServerRequests fetches the recent server-request log (all HTTP traffic in
// and out of the Brain server, annotated with the authenticated actor).
func (c *APIClient) GetServerRequests(ctx context.Context, limit int) (*types.ServerRequestListResponse, error) {
	resp, err := c.doRequest(ctx, http.MethodGet,
		fmt.Sprintf("/api/v1/server/requests/recent?limit=%d", limit), nil)
	if err != nil {
		return nil, fmt.Errorf("get server requests: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var result types.ServerRequestListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode server requests response: %w", err)
	}
	return &result, nil
}

// UpdateRunnerConfig updates a runner's maxParallel configuration.
func (c *APIClient) UpdateRunnerConfig(ctx context.Context, runnerID string, maxParallel int) error {
	path := fmt.Sprintf("/api/v1/runners/%s/config", runnerID)
	body := map[string]int{"maxParallel": maxParallel}

	resp, err := c.doJSONRequest(ctx, http.MethodPatch, path, body)
	if err != nil {
		return fmt.Errorf("update runner config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// ToggleRunnerFeature enables or disables a feature on a specific runner.
func (c *APIClient) ToggleRunnerFeature(ctx context.Context, runnerID, featureID string, enabled bool) error {
	path := fmt.Sprintf("/api/v1/runners/%s/features/%s/toggle", runnerID, featureID)
	body := map[string]bool{"enabled": enabled}

	resp, err := c.doJSONRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return fmt.Errorf("toggle runner feature: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// UpdateRunnerAffinity updates which features a runner can execute.
func (c *APIClient) UpdateRunnerAffinity(ctx context.Context, runnerID string, featureIDs []string) error {
	path := fmt.Sprintf("/api/v1/runners/%s/affinity", runnerID)
	body := map[string][]string{"feature_ids": featureIDs}

	resp, err := c.doJSONRequest(ctx, http.MethodPut, path, body)
	if err != nil {
		return fmt.Errorf("update runner affinity: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// =============================================================================
// Log Streaming
// =============================================================================

// PostTaskLogs sends a batch of log lines to the Brain API for a given task.
// This is fire-and-forget from the caller's perspective: failures are returned
// but should not block task execution.
func (c *APIClient) PostTaskLogs(ctx context.Context, projectID, taskID, runnerID string, lines []types.LogLine) error {
	if len(lines) == 0 {
		return nil
	}

	path := fmt.Sprintf("/api/v1/tasks/%s/%s/logs", projectID, taskID)
	body := types.LogIngestRequest{
		RunnerID: runnerID,
		Lines:    lines,
	}

	resp, err := c.doJSONRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return fmt.Errorf("post task logs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// GetTaskLogs fetches the persisted log lines for a task (historical + current),
// newest-bounded by limit. Used by the TUI logs pane to show stored output for
// completed tasks, not just the live SSE stream.
func (c *APIClient) GetTaskLogs(ctx context.Context, projectID, taskID string, limit int) (*types.LogQueryResponse, error) {
	path := fmt.Sprintf("/api/v1/tasks/%s/%s/logs?limit=%d", projectID, taskID, limit)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("get task logs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var result types.LogQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode task logs response: %w", err)
	}
	return &result, nil
}

// =============================================================================
// Configuration
// =============================================================================

// TaskDefaultsResponse mirrors the JSON from GET /api/v1/config/task-defaults.
type TaskDefaultsResponse struct {
	Agent              string `json:"agent"`
	Model              string `json:"model"`
	ExecutionMode      string `json:"execution_mode"`
	CompleteOnIdle     *bool  `json:"complete_on_idle"`
	MergePolicy        string `json:"merge_policy"`
	MergeStrategy      string `json:"merge_strategy"`
	MergeTargetBranch  string `json:"merge_target_branch"`
	RemoteBranchPolicy string `json:"remote_branch_policy"`
	OpenPRBeforeMerge  *bool  `json:"open_pr_before_merge"`
	TargetWorkdir      string `json:"target_workdir"`
}

// GetTaskDefaults fetches the server's task_defaults configuration.
// Returns nil (not an error) if the endpoint is unavailable (e.g., older server).
func (c *APIClient) GetTaskDefaults(ctx context.Context) (*TaskDefaultsResponse, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/api/v1/config/task-defaults", nil)
	if err != nil {
		return nil, nil // graceful fallback
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil // graceful fallback for older servers
	}

	var defaults TaskDefaultsResponse
	if err := json.NewDecoder(resp.Body).Decode(&defaults); err != nil {
		return nil, nil // graceful fallback
	}
	return &defaults, nil
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

// PauseAutomations pauses automation-generated task execution.
func (c *APIClient) PauseAutomations(ctx context.Context) error {
	resp, err := c.doRequest(ctx, http.MethodPost, "/api/v1/tasks/runner/automations/pause", nil)
	if err != nil {
		return fmt.Errorf("pause automations: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// ResumeAutomations resumes automation-generated task execution.
func (c *APIClient) ResumeAutomations(ctx context.Context) error {
	resp, err := c.doRequest(ctx, http.MethodPost, "/api/v1/tasks/runner/automations/resume", nil)
	if err != nil {
		return fmt.Errorf("resume automations: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// PauseProjectAutomations pauses automation-generated task execution for a specific project.
func (c *APIClient) PauseProjectAutomations(ctx context.Context, projectID string) error {
	path := fmt.Sprintf("/api/v1/tasks/runner/automations/pause/%s", projectID)
	resp, err := c.doRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return fmt.Errorf("pause project automations: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// ResumeProjectAutomations resumes automation-generated task execution for a specific project.
func (c *APIClient) ResumeProjectAutomations(ctx context.Context, projectID string) error {
	path := fmt.Sprintf("/api/v1/tasks/runner/automations/resume/%s", projectID)
	resp, err := c.doRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return fmt.Errorf("resume project automations: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.readError(resp)
	}
	return nil
}

// ShutdownRunner requests graceful shutdown for a specific registered runner.
func (c *APIClient) ShutdownRunner(ctx context.Context, runnerID string, reason string) error {
	body, err := json.Marshal(map[string]string{"reason": reason})
	if err != nil {
		return fmt.Errorf("encode shutdown runner request: %w", err)
	}
	path := fmt.Sprintf("/api/v1/runners/%s/shutdown", runnerID)
	resp, err := c.doRequest(ctx, http.MethodPut, path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("shutdown runner: %w", err)
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
	return c.doRequestWithClient(ctx, c.client, method, path, body)
}

func (c *APIClient) doRequestWithClient(ctx context.Context, client *http.Client, method, path string, body io.Reader) (*http.Response, error) {
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

	return client.Do(req)
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
