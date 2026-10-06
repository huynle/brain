package mcp

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/huynle/brain-api/sdk/brain"
)

// NewStdioSDKClient composes the public SDK's actual shared HTTP transport only
// for local stdio. Legacy MCP DTOs, query options, error text and filesystem
// adapters remain intact. This never wraps APIClient.Request in an SDK request,
// or reconstructs hosted authority. Hosted NewAPIClient remains unchanged.
func NewStdioSDKClient(baseURL string) (*APIClient, error) {
	transport, err := brain.NewHTTPTransport(baseURL, nil)
	if err != nil {
		return nil, err
	}
	token := os.Getenv("BRAIN_API_TOKEN")
	if strings.ContainsAny(token, "\r\n\x00") {
		return nil, &brain.Error{Code: "invalid_configuration"}
	}
	return &APIClient{baseURL: strings.TrimRight(baseURL, "/"), authToken: token, httpClient: &http.Client{Timeout: 30 * time.Second, Transport: transport}}, nil
}
