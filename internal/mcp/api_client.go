package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// APIClient is an HTTP client for the Brain API REST endpoints.
type APIClient struct {
	baseURL    string
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
	if resp.StatusCode >= 400 {
		var apiErr apiErrorResponse
		if err := json.Unmarshal(respBody, &apiErr); err != nil {
			return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
		}
		if apiErr.Message != "" {
			return fmt.Errorf("%s", apiErr.Message)
		}
		return fmt.Errorf("API error: %s", apiErr.Error)
	}

	// Decode response
	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}
