package mcp

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
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
	// The ambient credential never crosses an unencrypted non-loopback hop.
	// NewHTTPTransport has already validated scheme/host/userinfo.
	if u, err := url.Parse(baseURL); token != "" && (err != nil || (strings.EqualFold(u.Scheme, "http") && !loopbackHost(u.Hostname()))) {
		host := ""
		if err == nil {
			host = u.Hostname()
		}
		return nil, fmt.Errorf("refusing to send BRAIN_API_TOKEN over plain http to non-loopback host %q; use https or a loopback API URL: %w", host, &brain.Error{Code: "insecure_transport"})
	}
	return &APIClient{baseURL: strings.TrimRight(baseURL, "/"), authToken: token, httpClient: &http.Client{Timeout: 30 * time.Second, Transport: transport}}, nil
}

// loopbackHost is exact: "localhost" or an IP literal in a loopback range.
// Names that merely start with "localhost" or "127." are not loopback.
func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
