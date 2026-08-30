package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// APIClient is an HTTP client for the Brain API REST endpoints.
type APIClient struct {
	baseURL    string
	authToken  string // optional Bearer token for authenticated APIs
	httpClient *http.Client
}

// NewAPIClient creates a new API client with the given base URL.
// The base URL should not include the /api/v1 prefix.
func NewAPIClient(baseURL string) *APIClient {
	return &APIClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// WithAuthToken returns a copy of the client with the given Bearer token set.
// Requests made with this client will include an Authorization header.
func (c *APIClient) WithAuthToken(token string) *APIClient {
	return &APIClient{
		baseURL:    c.baseURL,
		authToken:  token,
		httpClient: c.httpClient,
	}
}

// apiErrorResponse matches the Brain API error format.
type apiErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// Request makes an HTTP request to the Brain API.
// method: HTTP method (GET, POST, PATCH, DELETE)
// path: API path relative to /api/v1 (e.g., "/entries", "/health")
// body: request body (marshaled to JSON for POST/PATCH/PUT; nil for GET/DELETE)
// queryParams: URL query parameters (nil if none)
// result: pointer to decode JSON response into
func (c *APIClient) Request(ctx context.Context, method, path string, body any, queryParams map[string]string, result any) error {
	// Build URL
	u := c.baseURL + "/api/v1" + path

	if len(queryParams) > 0 {
		params := url.Values{}
		for k, v := range queryParams {
			if v != "" {
				params.Set(k, v)
			}
		}
		if encoded := params.Encode(); encoded != "" {
			u += "?" + encoded
		}
	}

	// Build request body
	var bodyReader io.Reader
	if body != nil && (method == "POST" || method == "PATCH" || method == "PUT" || method == "DELETE") {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	// Execute
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	// Check for HTTP errors
	if err := checkAPIError(resp, respBody); err != nil {
		return err
	}

	// Decode response
	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}

// UploadAttachment uploads a file from the filesystem of the process running
// this client as multipart/form-data to the Brain API.
//
// The path is opened locally, so this is only meaningful when the client shares
// a filesystem with whoever supplied the path. Remote callers must use
// UploadAttachmentContent.
func (c *APIClient) UploadAttachment(ctx context.Context, projectID, filePath string, metadata map[string]string) (*types.CreateAttachmentResponse, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open attachment file: %w", err)
	}
	defer file.Close()

	return c.UploadAttachmentContent(ctx, projectID, filepath.Base(filePath), file, metadata)
}

// UploadAttachmentContent uploads raw bytes as multipart/form-data under the
// given filename. It touches no filesystem, so it works over any transport.
func (c *APIClient) UploadAttachmentContent(ctx context.Context, projectID, filename string, content io.Reader, metadata map[string]string) (*types.CreateAttachmentResponse, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("project_id", projectID); err != nil {
		return nil, fmt.Errorf("write project_id field: %w", err)
	}
	if len(metadata) > 0 {
		data, err := json.Marshal(metadata)
		if err != nil {
			return nil, fmt.Errorf("marshal metadata: %w", err)
		}
		if err := writer.WriteField("metadata", string(data)); err != nil {
			return nil, fmt.Errorf("write metadata field: %w", err)
		}
	}

	part, err := writer.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return nil, fmt.Errorf("create file part: %w", err)
	}
	if _, err := io.Copy(part, content); err != nil {
		return nil, fmt.Errorf("copy file part: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/attachments", &body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if err := checkAPIError(resp, respBody); err != nil {
		return nil, err
	}

	var result types.CreateAttachmentResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
	}
	return &result, nil
}

// DownloadAttachmentText downloads extracted plain text for an attachment.
func (c *APIClient) DownloadAttachmentText(ctx context.Context, projectID, attachmentID string) (string, error) {
	params := url.Values{}
	if projectID != "" {
		params.Set("project_id", projectID)
	}
	u := c.baseURL + "/api/v1/attachments/" + url.PathEscape(attachmentID) + "/text"
	if encoded := params.Encode(); encoded != "" {
		u += "?" + encoded
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if err := checkAPIError(resp, respBody); err != nil {
		return "", err
	}
	return string(respBody), nil
}

// ExtractAttachmentText triggers server-side text extraction for an attachment.
func (c *APIClient) ExtractAttachmentText(ctx context.Context, projectID, attachmentID string, req types.AttachmentExtractionRequest) (*types.AttachmentExtractionResult, error) {
	if req.AttachmentID == "" {
		req.AttachmentID = attachmentID
	}
	var result types.AttachmentExtractionResult
	if err := c.Request(ctx, http.MethodPost, "/attachments/"+url.PathEscape(attachmentID)+"/extract", req, map[string]string{"project_id": projectID}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// attachmentContent issues the raw-content request for an attachment and
// returns the live response. The caller owns the body and must close it.
func (c *APIClient) attachmentContent(ctx context.Context, projectID, attachmentID string) (*http.Response, error) {
	params := url.Values{}
	if projectID != "" {
		params.Set("project_id", projectID)
	}
	u := c.baseURL + "/api/v1/attachments/" + url.PathEscape(attachmentID) + "/content"
	if encoded := params.Encode(); encoded != "" {
		u += "?" + encoded
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		return nil, checkAPIError(resp, respBody)
	}
	return resp, nil
}

// DownloadAttachmentBytes returns an attachment's raw bytes and content type,
// reading at most limit bytes. Anything larger is an error rather than a silent
// truncation, so the caller can fall back to the REST content endpoint.
func (c *APIClient) DownloadAttachmentBytes(ctx context.Context, projectID, attachmentID string, limit int64) ([]byte, string, error) {
	resp, err := c.attachmentContent(ctx, projectID, attachmentID)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	// Read one byte past the limit: if it arrives, the attachment is too big.
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, "", fmt.Errorf("read attachment content: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, "", fmt.Errorf("attachment %s is larger than the %d byte inline limit: fetch it from GET /api/v1/attachments/%s/content instead",
			attachmentID, limit, attachmentID)
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// DownloadAttachmentToFile streams raw attachment bytes to outputPath on the
// filesystem of the process running this client. Remote callers must use
// DownloadAttachmentBytes.
func (c *APIClient) DownloadAttachmentToFile(ctx context.Context, projectID, attachmentID, outputPath string) error {
	resp, err := c.attachmentContent(ctx, projectID, attachmentID)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if dir := filepath.Dir(outputPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
	}
	out, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("write output file: %w", err)
	}
	return nil
}

func checkAPIError(resp *http.Response, respBody []byte) error {
	if resp.StatusCode < 400 {
		return nil
	}
	var apiErr apiErrorResponse
	if err := json.Unmarshal(respBody, &apiErr); err != nil {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}
	if apiErr.Message != "" {
		return fmt.Errorf("%s", apiErr.Message)
	}
	return fmt.Errorf("API error: %s", apiErr.Error)
}
