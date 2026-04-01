package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DreamCommand implements the Command interface for the dream command.
type DreamCommand struct {
	Project string // "" = list mode, else project name
	Config  *UnifiedConfig
	Flags   *DreamFlags

	// httpClient is injectable for testing; defaults to a 3-second-timeout client.
	httpClient *http.Client
}

// DreamFlags holds dream command flags.
type DreamFlags struct {
	Enable   bool
	Disable  bool
	Schedule string
}

// Type returns the command type identifier.
func (c *DreamCommand) Type() string {
	return "dream"
}

// Execute runs the dream command.
func (c *DreamCommand) Execute() error {
	// Validate flags: --enable and --disable are mutually exclusive
	if c.Flags.Enable && c.Flags.Disable {
		return fmt.Errorf("cannot use --enable and --disable together")
	}

	if !c.isAPIAvailable() {
		return fmt.Errorf("brain API is not available at %s — start it with: brain server start", c.apiURL())
	}

	// Route based on flags and project
	switch {
	case c.Flags.Enable:
		if c.Project == "" {
			return fmt.Errorf("project is required with --enable")
		}
		return c.enableDream()
	case c.Flags.Disable:
		if c.Project == "" {
			return fmt.Errorf("project is required with --disable")
		}
		return c.disableDream()
	case c.Project == "":
		return c.listDreamProjects()
	default:
		return c.showDreamContent()
	}
}

// =============================================================================
// API Client Methods (duplicated from TokenCommand pattern)
// =============================================================================

// getHTTPClient returns the HTTP client, creating a default one if not injected.
func (c *DreamCommand) getHTTPClient() *http.Client {
	if c.httpClient != nil {
		return c.httpClient
	}
	return &http.Client{Timeout: 3 * time.Second}
}

// apiURL returns the Brain API base URL from config, defaulting to http://localhost:3333.
func (c *DreamCommand) apiURL() string {
	if c.Config.Runner.BrainAPIURL != "" {
		return c.Config.Runner.BrainAPIURL
	}
	return "http://localhost:3333"
}

// apiToken returns the Bearer token for API authentication.
func (c *DreamCommand) apiToken() string {
	return c.Config.Runner.APIToken
}

// isAPIAvailable checks if the Brain API server is reachable by hitting the health endpoint.
func (c *DreamCommand) isAPIAvailable() bool {
	u := c.apiURL() + "/api/v1/health"
	resp, err := c.getHTTPClient().Get(u)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// apiRequest makes an authenticated HTTP request to the Brain API.
// Returns the response body and status code, or an error.
func (c *DreamCommand) apiRequest(method, path string, body io.Reader) ([]byte, int, error) {
	u := c.apiURL() + path
	req, err := http.NewRequest(method, u, body)
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

// =============================================================================
// Subcommand Implementations
// =============================================================================

// enableDream creates a dream monitor for the project via POST /api/v1/monitors.
func (c *DreamCommand) enableDream() error {
	reqBody, _ := json.Marshal(map[string]string{
		"template_id": "dream",
		"scope_type":  "project",
		"project":     c.Project,
		"schedule":    c.Flags.Schedule,
	})

	data, status, err := c.apiRequest("POST", "/api/v1/monitors", bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("enable dream: %w", err)
	}

	if status == http.StatusConflict {
		return fmt.Errorf("dream mode is already enabled for project %q", c.Project)
	}
	if status != http.StatusCreated && status != http.StatusOK {
		var errResp struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &errResp) == nil && errResp.Message != "" {
			return fmt.Errorf("enable dream: %s", errResp.Message)
		}
		return fmt.Errorf("enable dream: API returned status %d", status)
	}

	var resp struct {
		TaskID   string `json:"task_id"`
		Schedule string `json:"schedule"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		// Non-fatal: still succeeded
		fmt.Printf("Dream mode enabled for project %q\n", c.Project)
		return nil
	}

	fmt.Printf("Dream mode enabled for project %q\n", c.Project)
	if resp.TaskID != "" {
		fmt.Printf("  Task ID:  %s\n", resp.TaskID)
	}
	if resp.Schedule != "" {
		fmt.Printf("  Schedule: %s\n", resp.Schedule)
	}
	return nil
}

// disableDream removes the dream monitor for the project via DELETE /api/v1/monitors/by-scope.
func (c *DreamCommand) disableDream() error {
	reqBody, _ := json.Marshal(map[string]interface{}{
		"templateId": "dream",
		"scope": map[string]string{
			"type":    "project",
			"project": c.Project,
		},
	})

	data, status, err := c.apiRequest("DELETE", "/api/v1/monitors/by-scope", bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("disable dream: %w", err)
	}

	if status == http.StatusNotFound {
		return fmt.Errorf("dream mode is not enabled for project %q", c.Project)
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		var errResp struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &errResp) == nil && errResp.Message != "" {
			return fmt.Errorf("disable dream: %s", errResp.Message)
		}
		return fmt.Errorf("disable dream: API returned status %d", status)
	}

	fmt.Printf("Dream mode disabled for project %q\n", c.Project)
	return nil
}

// listDreamProjects lists all dream-enabled projects via GET /api/v1/monitors?template_id=dream.
func (c *DreamCommand) listDreamProjects() error {
	data, status, err := c.apiRequest("GET", "/api/v1/monitors?template_id=dream", nil)
	if err != nil {
		return fmt.Errorf("list dream projects: %w", err)
	}

	if status != http.StatusOK {
		return fmt.Errorf("list dream projects: API returned status %d", status)
	}

	var resp struct {
		Monitors []struct {
			TaskID   string `json:"task_id"`
			Project  string `json:"project"`
			Schedule string `json:"schedule"`
			Status   string `json:"status"`
		} `json:"monitors"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	if len(resp.Monitors) == 0 {
		fmt.Println("No dream-enabled projects found")
		fmt.Println()
		fmt.Println("Enable dream mode:")
		fmt.Println("  brain dream <project> --enable")
		return nil
	}

	fmt.Println("Dream-Enabled Projects")
	fmt.Println(strings.Repeat("─", 70))
	fmt.Printf("%-25s %-25s %s\n", "Project", "Schedule", "Status")
	fmt.Println(strings.Repeat("─", 70))

	for _, m := range resp.Monitors {
		schedule := m.Schedule
		if schedule == "" {
			schedule = "(default)"
		}
		status := m.Status
		if status == "" {
			status = "active"
		}
		fmt.Printf("%-25s %-25s %s\n", m.Project, schedule, status)
	}

	fmt.Println(strings.Repeat("─", 70))
	fmt.Printf("Total: %d project(s)\n", len(resp.Monitors))
	return nil
}

// showDreamContent prints dream content for a specific project.
// Searches for a dream entry via GET /api/v1/search?query=Project+Dream&type=dream,
// finds the one matching this project, then fetches full content via GET /api/v1/entries/<path>.
func (c *DreamCommand) showDreamContent() error {
	// Search for dream entries matching the project
	query := url.QueryEscape("Project Dream")
	searchPath := fmt.Sprintf("/api/v1/search?query=%s&type=dream&project=%s", query, url.QueryEscape(c.Project))
	data, status, err := c.apiRequest("GET", searchPath, nil)
	if err != nil {
		return fmt.Errorf("search dream content: %w", err)
	}

	if status != http.StatusOK {
		return fmt.Errorf("search dream content: API returned status %d", status)
	}

	var searchResp struct {
		Results []struct {
			Path    string `json:"path"`
			Title   string `json:"title"`
			Project string `json:"project"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &searchResp); err != nil {
		return fmt.Errorf("parse search response: %w", err)
	}

	// Find matching entry for this project
	var entryPath string
	for _, r := range searchResp.Results {
		if r.Project == c.Project || strings.Contains(r.Path, c.Project) {
			entryPath = r.Path
			break
		}
	}

	if entryPath == "" {
		fmt.Printf("No dream content found for project %q\n", c.Project)
		fmt.Println()
		fmt.Println("Dream content is generated when dream mode runs.")
		fmt.Println("Enable it with: brain dream", c.Project, "--enable")
		return nil
	}

	// Fetch full entry content
	entryData, entryStatus, err := c.apiRequest("GET", "/api/v1/entries/"+url.PathEscape(entryPath), nil)
	if err != nil {
		return fmt.Errorf("fetch dream entry: %w", err)
	}

	if entryStatus != http.StatusOK {
		return fmt.Errorf("fetch dream entry: API returned status %d", entryStatus)
	}

	var entry struct {
		Content string `json:"content"`
		Title   string `json:"title"`
	}
	if err := json.Unmarshal(entryData, &entry); err != nil {
		return fmt.Errorf("parse entry response: %w", err)
	}

	fmt.Println(entry.Content)
	return nil
}
