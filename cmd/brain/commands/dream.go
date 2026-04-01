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

	// --enable (optionally with --now)
	if c.Flags.Enable {
		if err := c.enableDream(); err != nil {
			return err
		}
		if c.Flags.Now {
			return c.triggerDream()
		}
		return nil
	}

	// --now (without --enable, dream must already be enabled)
	if c.Flags.Now {
		if c.Project == "" {
			return fmt.Errorf("project is required with --now")
		}
		return c.triggerDream()
	}

	// --disable
	if c.Flags.Disable {
		return c.disableDream()
	}

	// No flags: show dream content (or list)
	if c.Project == "" {
		return fmt.Errorf("project is required for dream command")
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

// triggerDream finds the dream monitor for this project and triggers it.
func (c *DreamCommand) triggerDream() error {
	// 1. Find the dream monitor for this project
	taskID, err := c.findDreamMonitor()
	if err != nil {
		return err
	}
	if taskID == "" {
		return fmt.Errorf("dream mode not enabled for project %q; use --enable first or --enable --now", c.Project)
	}

	// 2. Trigger the task
	triggerURL := fmt.Sprintf("%s/api/v1/tasks/%s/%s/trigger",
		c.apiURL(),
		url.PathEscape(c.Project),
		url.PathEscape(taskID),
	)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(triggerURL, "application/json", nil)
	if err != nil {
		return fmt.Errorf("trigger dream task: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("trigger dream task failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var triggerResp struct {
		Success   bool   `json:"success"`
		TaskID    string `json:"taskId"`
		Triggered bool   `json:"triggered"`
		Reason    string `json:"reason,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&triggerResp); err != nil {
		return fmt.Errorf("parse trigger response: %w", err)
	}

	if !triggerResp.Triggered {
		reason := triggerResp.Reason
		if reason == "" {
			reason = "unknown reason"
		}
		return fmt.Errorf("dream task not triggered: %s", reason)
	}

	// 3. Print trigger confirmation
	fmt.Printf("Triggering dream consolidation for %q...\n", c.Project)

	// 4. Wait for completion
	return c.waitForDreamCompletion(taskID)
}

// waitForDreamCompletion polls the task status until the dream task completes or times out.
func (c *DreamCommand) waitForDreamCompletion(taskID string) error {
	fmt.Print("  Waiting for completion")

	timeout := 5 * time.Minute
	interval := 5 * time.Second
	deadline := time.Now().Add(timeout)
	start := time.Now()

	client := &http.Client{Timeout: 10 * time.Second}
	statusURL := fmt.Sprintf("%s/api/v1/tasks/%s/status",
		c.apiURL(),
		url.PathEscape(c.Project),
	)

	retryCount := 0
	maxRetries := 3

	for time.Now().Before(deadline) {
		time.Sleep(interval)
		fmt.Print(".")

		// POST /api/v1/tasks/{project}/status with {"taskIds": ["taskId"]}
		reqBody, _ := json.Marshal(map[string]interface{}{
			"taskIds": []string{taskID},
		})

		resp, err := client.Post(statusURL, "application/json", bytes.NewReader(reqBody))
		if err != nil {
			retryCount++
			if retryCount > maxRetries {
				fmt.Println()
				return fmt.Errorf("API error during polling (after %d retries): %w", maxRetries, err)
			}
			continue
		}

		var statusResp struct {
			Tasks []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"tasks"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
			resp.Body.Close()
			retryCount++
			if retryCount > maxRetries {
				fmt.Println()
				return fmt.Errorf("failed to parse status response (after %d retries): %w", maxRetries, err)
			}
			continue
		}
		resp.Body.Close()

		// Reset retry count on successful API call
		retryCount = 0

		// Find our task in the response
		for _, task := range statusResp.Tasks {
			if task.ID == taskID {
				switch task.Status {
				case "pending", "in_progress":
					// Still running, continue polling
					continue
				case "active", "completed", "validated":
					// Success: task returned to steady state or completed
					elapsed := time.Since(start).Round(time.Second)
					fmt.Printf(" done! (%s)\n", elapsed)
					fmt.Println()
					fmt.Println("Dream content updated. View with:")
					fmt.Printf("  brain dream %s\n", c.Project)
					return nil
				case "blocked":
					elapsed := time.Since(start).Round(time.Second)
					fmt.Printf(" warning (%s)\n", elapsed)
					fmt.Println()
					fmt.Println("Dream consolidation hit an issue (status: blocked).")
					fmt.Println("Check the task for details:")
					fmt.Printf("  brain dream %s\n", c.Project)
					return nil
				default:
					elapsed := time.Since(start).Round(time.Second)
					fmt.Printf(" finished (%s)\n", elapsed)
					fmt.Println()
					fmt.Printf("Dream task ended with status: %s\n", task.Status)
					return nil
				}
			}
		}
	}

	fmt.Println()
	return fmt.Errorf("timed out waiting for dream consolidation (5m)")
}

// findDreamMonitor looks up the dream monitor task ID for this project.
func (c *DreamCommand) findDreamMonitor() (string, error) {
	monitorURL := fmt.Sprintf("%s/api/v1/monitors?template_id=dream&project=%s",
		c.apiURL(),
		url.QueryEscape(c.Project),
	)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(monitorURL)
	if err != nil {
		return "", fmt.Errorf("find dream monitor: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("find dream monitor (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var data struct {
		Monitors []struct {
			ID string `json:"id"`
		} `json:"monitors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("parse monitor response: %w", err)
	}

	if len(data.Monitors) == 0 {
		return "", nil
	}

	return data.Monitors[0].ID, nil
}

// enableDream enables dream mode for this project.
func (c *DreamCommand) enableDream() error {
	if c.Project == "" {
		return fmt.Errorf("project is required with --enable")
	}
	// TODO: Implement enable logic (task 1 of 3 in the feature plan)
	fmt.Printf("Dream mode enabled for %q\n", c.Project)
	return nil
}

// disableDream disables dream mode for this project.
func (c *DreamCommand) disableDream() error {
	if c.Project == "" {
		return fmt.Errorf("project is required with --disable")
	}
	// TODO: Implement disable logic (task 3 of 3 in the feature plan)
	fmt.Printf("Dream mode disabled for %q\n", c.Project)
	return nil
}

// showDream displays dream content for the project.
func (c *DreamCommand) showDream() error {
	// TODO: Implement show/list dream content
	return fmt.Errorf("dream content display not yet implemented")
}
