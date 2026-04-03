package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

// DreamFlags holds flags for the dream command.
type DreamFlags struct {
	Enable   bool
	Disable  bool
	Now      bool
	Schedule string
}

// DreamCommand handles the `brain dream` command.
type DreamCommand struct {
	Project string
	Config  *UnifiedConfig
	Flags   *DreamFlags
}

// Type returns the command type identifier.
func (c *DreamCommand) Type() string {
	return "dream"
}

// Execute runs the dream command.
func (c *DreamCommand) Execute() error {
	// Validate mutually exclusive flags
	if c.Flags.Enable && c.Flags.Disable {
		return fmt.Errorf("cannot use --enable and --disable together")
	}
	if c.Flags.Disable && c.Flags.Now {
		return fmt.Errorf("cannot use --now with --disable")
	}

	if !c.isAPIAvailable() {
		return fmt.Errorf("brain API not available at %s", c.apiURL())
	}

	// --now: inline execution (standalone, does not require --enable)
	if c.Flags.Now {
		if c.Project == "" {
			return fmt.Errorf("project is required with --now")
		}
		if c.Flags.Enable {
			// --enable --now: enable the recurring schedule AND run now
			if err := c.enableDream(); err != nil {
				return err
			}
		}
		return c.executeDreamNow()
	}

	// --enable (without --now)
	if c.Flags.Enable {
		return c.enableDream()
	}

	// --disable
	if c.Flags.Disable {
		return c.disableDream()
	}

	// No flags: show dream content (or list all dream-enabled projects)
	if c.Project == "" {
		return c.listDreamProjects()
	}
	return c.showDream()
}

// apiURL returns the Brain API base URL from config.
func (c *DreamCommand) apiURL() string {
	if c.Config != nil && c.Config.MCP.APIURL != "" {
		return c.Config.MCP.APIURL
	}
	port := 3333
	if c.Config != nil && c.Config.Server.Port != 0 {
		port = c.Config.Server.Port
	}
	host := "localhost"
	if c.Config != nil && c.Config.Server.Host != "" {
		host = c.Config.Server.Host
	}
	return fmt.Sprintf("http://%s:%d", host, port)
}

// isAPIAvailable checks if the Brain API server is reachable.
func (c *DreamCommand) isAPIAvailable() bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(c.apiURL() + "/api/v1/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// executeDreamNow runs dream consolidation inline by spawning opencode directly.
// This is a self-contained execution — no TUI runner needed.
func (c *DreamCommand) executeDreamNow() error {
	fmt.Printf("Running dream consolidation for %q...\n\n", c.Project)

	// 1. Fetch the dream prompt from the API
	prompt, err := c.fetchDreamPrompt()
	if err != nil {
		return fmt.Errorf("failed to get dream prompt: %w", err)
	}

	// 2. Resolve the opencode binary
	opencodeBin := "opencode"
	if c.Config != nil && c.Config.Runner.Opencode.Bin != "" {
		opencodeBin = c.Config.Runner.Opencode.Bin
	}
	binPath, err := exec.LookPath(opencodeBin)
	if err != nil {
		return fmt.Errorf("%s not found in PATH: %w", opencodeBin, err)
	}

	// 3. Build opencode run args
	args := []string{"run"}
	if c.Config != nil && c.Config.Runner.Opencode.Agent != "" {
		args = append(args, "--agent", c.Config.Runner.Opencode.Agent)
	}
	if c.Config != nil && c.Config.Runner.Opencode.Model != "" {
		args = append(args, "--model", c.Config.Runner.Opencode.Model)
	}
	args = append(args, prompt)

	// 4. Spawn inline with inherited stdio
	start := time.Now()
	cmd := exec.Command(binPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("dream consolidation failed: %w", err)
	}

	elapsed := time.Since(start).Round(time.Second)
	fmt.Printf("\nDream consolidation complete (%s)\n", elapsed)
	fmt.Println("View the dream with:")
	fmt.Printf("  brain dream %s\n", c.Project)
	return nil
}

// fetchDreamPrompt fetches the dream prompt for this project from the API.
// Falls back to a built-in prompt if the API endpoint is not available.
func (c *DreamCommand) fetchDreamPrompt() (string, error) {
	// Try the API endpoint that returns the dream prompt
	promptURL := fmt.Sprintf("%s/api/v1/monitors/templates/dream/prompt?project=%s",
		c.apiURL(),
		url.QueryEscape(c.Project),
	)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(promptURL)
	if err == nil && resp.StatusCode == http.StatusOK {
		defer resp.Body.Close()
		var data struct {
			Prompt string `json:"prompt"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&data); err == nil && data.Prompt != "" {
			return data.Prompt, nil
		}
	}
	if resp != nil {
		resp.Body.Close()
	}

	// Fallback: build a simple dream prompt inline
	return c.buildFallbackDreamPrompt(), nil
}

// buildFallbackDreamPrompt constructs a dream prompt when the API endpoint isn't available.
func (c *DreamCommand) buildFallbackDreamPrompt() string {
	projectFilter := ""
	if c.Project != "" {
		projectFilter = fmt.Sprintf(`, project: "%s"`, c.Project)
	}

	return fmt.Sprintf(`You are the **Dream Consolidator** — read all knowledge in project %q and synthesize it into a single comprehensive "Project Dream" document.

## Phase 1: Gate Checks (SKIP for --now invocation)

This is an ad-hoc invocation via 'brain dream --now'. Skip cooldown and threshold checks — run consolidation unconditionally.

## Phase 2: Read All Project Knowledge

Gather every piece of knowledge by type. For each call, then call brain_recall on every returned entry to get full content.

- brain_list({ type: "decision"%[2]s }) — architectural decisions
- brain_list({ type: "pattern"%[2]s }) — reusable patterns
- brain_list({ type: "learning"%[2]s }) — learnings and best practices
- brain_list({ type: "summary"%[2]s }) — session summaries
- brain_list({ type: "plan", status: "active"%[2]s }) — active plans
- brain_list({ type: "exploration"%[2]s }) — research and investigations
- brain_list({ type: "idea"%[2]s }) — future ideas

## Phase 3: Synthesize Dream Document

Consolidate everything into a structured markdown document with these sections:

### Project Identity
Purpose, technologies, primary goals.

### Architecture & Design
Architectural decisions, component structure, design patterns, system boundaries.

### Active Context
Current work in progress, priorities, blockers, recent completions.

### Conventions & Preferences
Coding style, naming conventions, workflow patterns, testing approach, tooling.

### Key Decisions
Compressed ADRs — title, context (1-2 sentences), decision, consequences.

### Learnings & Patterns
Reusable knowledge, gotchas, performance insights, proven approaches.

### Open Questions & Ideas
Unresolved questions, proposed features, exploration candidates.

**Guidelines:** Compress don't copy. Prioritize recency. Resolve contradictions. Target 2000-4000 words.

## Phase 4: Save Dream Entry

1. Search for existing dream: brain_search({ query: "Project Dream", type: "dream"%[2]s })
2. If found, delete it: brain_delete({ path: "<path>", confirm: true })
3. Save new dream: brain_save({ type: "dream", title: "Project Dream: %[1]s", content: "<document>", tags: ["dream", "consolidation"]%[2]s })

## Safety Rules
- NEVER modify existing entries — read and synthesize only
- NEVER fabricate information — only use what you actually read`,
		c.Project, projectFilter)
}

// apiRequest makes an authenticated HTTP request to the Brain API.
func (c *DreamCommand) apiRequest(method, path string, body io.Reader) ([]byte, int, error) {
	reqURL := c.apiURL() + path
	req, err := http.NewRequest(method, reqURL, body)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
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

// enableDream enables dream mode for this project via POST /api/v1/monitors.
func (c *DreamCommand) enableDream() error {
	if c.Project == "" {
		return fmt.Errorf("project is required with --enable")
	}

	reqBody := map[string]interface{}{
		"template_id": "dream",
		"scope_type":  "project",
		"project":     c.Project,
	}
	if c.Flags.Schedule != "" {
		reqBody["schedule"] = c.Flags.Schedule
	}

	bodyBytes, _ := json.Marshal(reqBody)
	data, status, err := c.apiRequest("POST", "/api/v1/monitors", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("enable dream: %w", err)
	}

	if status == http.StatusConflict {
		return fmt.Errorf("dream mode is already enabled for project %q", c.Project)
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return fmt.Errorf("enable dream: API returned status %d: %s", status, string(data))
	}

	fmt.Printf("Dream mode enabled for project %q\n", c.Project)
	return nil
}

// disableDream disables dream mode for this project via DELETE /api/v1/monitors/by-scope.
func (c *DreamCommand) disableDream() error {
	if c.Project == "" {
		return fmt.Errorf("project is required with --disable")
	}

	reqBody := map[string]interface{}{
		"templateId": "dream",
		"scope": map[string]string{
			"type":    "project",
			"project": c.Project,
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)
	_, status, err := c.apiRequest("DELETE", "/api/v1/monitors/by-scope", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("disable dream: %w", err)
	}

	if status == http.StatusNotFound {
		return fmt.Errorf("dream mode is not enabled for project %q", c.Project)
	}
	if status != http.StatusOK {
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
			ID    string `json:"id"`
			Scope struct {
				Type    string `json:"type"`
				Project string `json:"project"`
			} `json:"scope"`
			Enabled  bool   `json:"enabled"`
			Schedule string `json:"schedule"`
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
		project := m.Scope.Project
		if project == "" {
			project = "(all)"
		}
		schedule := m.Schedule
		if schedule == "" {
			schedule = "(default)"
		}
		status := "enabled"
		if !m.Enabled {
			status = "disabled"
		}
		fmt.Printf("%-25s %-25s %s\n", project, schedule, status)
	}

	fmt.Println(strings.Repeat("─", 70))
	fmt.Printf("Total: %d project(s)\n", len(resp.Monitors))
	return nil
}

// showDream displays dream content for the project.
func (c *DreamCommand) showDream() error {
	listPath := fmt.Sprintf("/api/v1/entries?type=dream&project=%s", url.QueryEscape(c.Project))
	data, status, err := c.apiRequest("GET", listPath, nil)
	if err != nil {
		return fmt.Errorf("list dream entries: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("list dream entries: API returned status %d", status)
	}

	var listResp struct {
		Entries []struct {
			Path      string `json:"path"`
			Title     string `json:"title"`
			Content   string `json:"content"`
			ProjectID string `json:"project_id"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(data, &listResp); err != nil {
		return fmt.Errorf("parse list response: %w", err)
	}

	// Find matching entry for this project (content is included in list response)
	for _, e := range listResp.Entries {
		if e.ProjectID == c.Project || strings.Contains(e.Path, "projects/"+c.Project+"/") {
			fmt.Println(e.Content)
			return nil
		}
	}

	fmt.Printf("No dream content found for project %q\n", c.Project)
	fmt.Println()
	fmt.Println("Dream content is generated when dream mode runs.")
	fmt.Printf("  brain dream %s --enable --now\n", c.Project)
	return nil
}
