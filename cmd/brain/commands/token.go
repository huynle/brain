package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/tokens"
)

// TokenCommand implements the Command interface for the token command.
type TokenCommand struct {
	Subcommand string
	Name       string
	Config     *UnifiedConfig
	Flags      *TokenFlags

	// httpClient is injectable for testing; defaults to a 3-second-timeout client.
	httpClient *http.Client
}

// TokenFlags holds token command flags.
type TokenFlags struct {
	Name string
}

// Type returns the command type identifier.
func (c *TokenCommand) Type() string {
	return "token"
}

// Execute runs the token command.
func (c *TokenCommand) Execute() error {
	brainDir := expandPath(c.Config.Server.BrainDir)

	switch c.Subcommand {
	case "create":
		return c.createToken(brainDir)
	case "list":
		return c.listTokens(brainDir)
	case "revoke":
		return c.revokeToken(brainDir)
	default:
		return fmt.Errorf("unknown subcommand: %s", c.Subcommand)
	}
}

// expandPath expands ~ to home directory.
func expandPath(path string) string {
	if path == "~" {
		homeDir, _ := os.UserHomeDir()
		return homeDir
	}
	if strings.HasPrefix(path, "~/") {
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, path[2:])
	}
	return path
}

// getHTTPClient returns the HTTP client, creating a default one if not injected.
func (c *TokenCommand) getHTTPClient() *http.Client {
	if c.httpClient != nil {
		return c.httpClient
	}
	return &http.Client{Timeout: 3 * time.Second}
}

// apiURL returns the Brain API base URL from config, defaulting to http://localhost:3333.
func (c *TokenCommand) apiURL() string {
	if c.Config.Runner.BrainAPIURL != "" {
		return c.Config.Runner.BrainAPIURL
	}
	return "http://localhost:3333"
}

// apiToken returns the Bearer token for API authentication.
func (c *TokenCommand) apiToken() string {
	return c.Config.Runner.APIToken
}

// isAPIAvailable checks if the Brain API server is reachable by hitting the health endpoint.
func (c *TokenCommand) isAPIAvailable() bool {
	url := c.apiURL() + "/api/v1/health"
	resp, err := c.getHTTPClient().Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// apiRequest makes an authenticated HTTP request to the Brain API.
// Returns the response body and status code, or an error.
func (c *TokenCommand) apiRequest(method, path string, body io.Reader) ([]byte, int, error) {
	url := c.apiURL() + path
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token := c.apiToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.getHTTPClient().Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}
	return data, resp.StatusCode, nil
}

// createToken creates a new API token with smart fallback:
//  1. Try authenticated API (POST /tokens)
//  2. On 401 → try unauthenticated bootstrap (POST /tokens/bootstrap)
//  3. On bootstrap 403 (tokens exist) → fall back to direct DB access
//  4. If API unreachable → fall back to direct DB access
func (c *TokenCommand) createToken(brainDir string) error {
	if c.Name == "" {
		return fmt.Errorf("token name is required (use --name)")
	}

	if c.isAPIAvailable() {
		err := c.createTokenViaAPI()
		if err == nil {
			return nil
		}
		// If auth failed, try bootstrap endpoint
		if isAuthError(err) {
			fmt.Println("Note: Authentication failed, trying bootstrap endpoint...")
			bootstrapErr := c.createTokenViaBootstrap()
			if bootstrapErr == nil {
				return nil
			}
			// Bootstrap also failed (tokens exist on server) → fall back to direct DB
			fmt.Println("Note: Bootstrap unavailable, falling back to direct database access")
			return c.createTokenDirect(brainDir)
		}
		return err
	}

	fmt.Println("Note: API server not running, using direct database access")
	return c.createTokenDirect(brainDir)
}

// isAuthError checks if an error is an authentication failure (401).
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Missing authentication token") ||
		strings.Contains(msg, "Invalid authentication token") ||
		strings.Contains(msg, "Unauthorized")
}

// createTokenViaBootstrap creates a token through the unauthenticated bootstrap endpoint.
// This only works when zero tokens exist on the server.
func (c *TokenCommand) createTokenViaBootstrap() error {
	reqBody, _ := json.Marshal(map[string]string{"name": c.Name})
	url := c.apiURL() + "/api/v1/tokens/bootstrap"
	req, err := http.NewRequest("POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// No auth header — bootstrap is unauthenticated

	resp, err := c.getHTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("bootstrap request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("bootstrap forbidden: tokens already exist on server")
	}
	if resp.StatusCode != http.StatusCreated {
		var errResp struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &errResp) == nil && errResp.Message != "" {
			return fmt.Errorf("bootstrap token: %s", errResp.Message)
		}
		return fmt.Errorf("bootstrap token: API returned status %d", resp.StatusCode)
	}

	var respData struct {
		Name      string `json:"name"`
		Token     string `json:"token"`
		CreatedAt string `json:"created_at"`
	}
	if err := json.Unmarshal(data, &respData); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	apiURL := c.apiURL()
	fmt.Printf("✓ Token created via bootstrap\n")
	fmt.Printf("  Name:  %s\n", respData.Name)
	fmt.Printf("  Token: %s\n", respData.Token)
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Printf("  export BRAIN_API_TOKEN=%s\n", respData.Token)
	fmt.Printf("  curl -H \"Authorization: Bearer %s\" %s/api/v1/health\n", respData.Token, apiURL)

	return nil
}

// createTokenViaAPI creates a token through the API.
func (c *TokenCommand) createTokenViaAPI() error {
	reqBody, _ := json.Marshal(map[string]string{"name": c.Name})
	data, status, err := c.apiRequest("POST", "/api/v1/tokens", bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("create token via API: %w", err)
	}

	if status != http.StatusCreated {
		var errResp struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &errResp) == nil && errResp.Message != "" {
			return fmt.Errorf("create token: %s", errResp.Message)
		}
		return fmt.Errorf("create token: API returned status %d", status)
	}

	var resp struct {
		Name      string `json:"name"`
		Token     string `json:"token"`
		CreatedAt string `json:"created_at"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	apiURL := c.apiURL()
	fmt.Printf("✓ Token created successfully\n")
	fmt.Printf("  Name:  %s\n", resp.Name)
	fmt.Printf("  Token: %s\n", resp.Token)
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Printf("  export BRAIN_API_TOKEN=%s\n", resp.Token)
	fmt.Printf("  curl -H \"Authorization: Bearer %s\" %s/api/v1/health\n", resp.Token, apiURL)

	return nil
}

// createTokenDirect creates a token via direct DB access (fallback).
func (c *TokenCommand) createTokenDirect(brainDir string) error {
	token, err := tokens.CreateTokenDirect(brainDir, c.Name)
	if err != nil {
		return fmt.Errorf("create token: %w", err)
	}

	fmt.Printf("✓ Token created successfully\n")
	fmt.Printf("  Name:  %s\n", token.Name)
	fmt.Printf("  Token: %s\n", token.Token)

	return nil
}

// listTokens lists all API tokens, preferring API when available.
// Falls back to direct DB access on auth failure.
func (c *TokenCommand) listTokens(brainDir string) error {
	if c.isAPIAvailable() {
		err := c.listTokensViaAPI()
		if err == nil {
			return nil
		}
		if isAuthError(err) {
			fmt.Println("Note: Authentication failed, falling back to direct database access")
			return c.listTokensDirect(brainDir)
		}
		return err
	}

	fmt.Println("Note: API server not running, using direct database access")
	return c.listTokensDirect(brainDir)
}

// listTokensViaAPI lists tokens through the API.
func (c *TokenCommand) listTokensViaAPI() error {
	data, status, err := c.apiRequest("GET", "/api/v1/tokens", nil)
	if err != nil {
		return fmt.Errorf("list tokens via API: %w", err)
	}

	if status != http.StatusOK {
		return parseAPIError("list tokens", data, status)
	}

	var resp struct {
		Tokens []struct {
			Name        string `json:"name"`
			TokenPrefix string `json:"token_prefix"`
			CreatedAt   string `json:"created_at"`
			Status      string `json:"status"`
		} `json:"tokens"`
		Active  int `json:"active"`
		Revoked int `json:"revoked"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	if len(resp.Tokens) == 0 {
		fmt.Println("No tokens found")
		return nil
	}

	fmt.Println("API Tokens")
	fmt.Println(strings.Repeat("─", 80))
	fmt.Printf("%-20s %-45s %s\n", "Name", "Token", "Created")
	fmt.Println(strings.Repeat("─", 80))

	for _, t := range resp.Tokens {
		prefix := t.TokenPrefix
		if prefix != "" && !strings.HasSuffix(prefix, "...") {
			prefix += "..."
		}
		fmt.Printf("%-20s %-45s %s\n", t.Name, prefix, t.CreatedAt)
	}

	fmt.Println(strings.Repeat("─", 80))
	fmt.Printf("Total: %d tokens (active: %d, revoked: %d)\n", len(resp.Tokens), resp.Active, resp.Revoked)

	return nil
}

// listTokensDirect lists tokens via direct DB access (fallback).
func (c *TokenCommand) listTokensDirect(brainDir string) error {
	tokenList, err := tokens.ListTokensDirect(brainDir)
	if err != nil {
		return fmt.Errorf("list tokens: %w", err)
	}

	if len(tokenList) == 0 {
		fmt.Println("No tokens found")
		return nil
	}

	fmt.Println("API Tokens")
	fmt.Println(strings.Repeat("─", 80))
	fmt.Printf("%-20s %-45s %s\n", "Name", "Token", "Created")
	fmt.Println(strings.Repeat("─", 80))

	for _, token := range tokenList {
		maskedToken := maskToken(token.Token)
		fmt.Printf("%-20s %-45s %s\n", token.Name, maskedToken, token.CreatedAt)
	}

	fmt.Println(strings.Repeat("─", 80))
	fmt.Printf("Total: %d tokens\n", len(tokenList))

	return nil
}

// revokeToken revokes an API token, preferring API when available.
// Falls back to direct DB access on auth failure.
func (c *TokenCommand) revokeToken(brainDir string) error {
	if c.Name == "" {
		return fmt.Errorf("token name is required")
	}

	if c.isAPIAvailable() {
		err := c.revokeTokenViaAPI()
		if err == nil {
			return nil
		}
		if isAuthError(err) {
			fmt.Println("Note: Authentication failed, falling back to direct database access")
			return c.revokeTokenDirect(brainDir)
		}
		return err
	}

	fmt.Println("Note: API server not running, using direct database access")
	return c.revokeTokenDirect(brainDir)
}

// revokeTokenViaAPI revokes a token through the API.
func (c *TokenCommand) revokeTokenViaAPI() error {
	data, status, err := c.apiRequest("DELETE", "/api/v1/tokens/"+c.Name, nil)
	if err != nil {
		return fmt.Errorf("revoke token via API: %w", err)
	}

	if status == http.StatusNotFound {
		return fmt.Errorf("revoke token: token '%s' not found", c.Name)
	}
	if status != http.StatusOK {
		return parseAPIError("revoke token", data, status)
	}

	fmt.Printf("✓ Token '%s' revoked successfully\n", c.Name)
	return nil
}

// revokeTokenDirect revokes a token via direct DB access (fallback).
func (c *TokenCommand) revokeTokenDirect(brainDir string) error {
	if err := tokens.RevokeTokenDirect(brainDir, c.Name); err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}

	fmt.Printf("✓ Token '%s' revoked successfully\n", c.Name)
	return nil
}

// parseAPIError extracts the error message from an API error response.
// Returns an error that includes the message from the response body,
// which allows isAuthError to detect 401 responses.
func parseAPIError(context string, data []byte, status int) error {
	var errResp struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &errResp) == nil && errResp.Message != "" {
		return fmt.Errorf("%s: %s", context, errResp.Message)
	}
	return fmt.Errorf("%s: API returned status %d", context, status)
}

// maskToken masks a token for display: shows first 8 chars + "..." + last 4 chars.
func maskToken(token string) string {
	if len(token) <= 12 {
		return token
	}
	return token[:8] + "..." + token[len(token)-4:]
}
