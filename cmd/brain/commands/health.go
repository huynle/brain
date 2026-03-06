package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/huynle/brain-api/internal/lifecycle"
)

// HealthFlags holds flags for the health command.
type HealthFlags struct {
	Wait    bool // Wait for server to become healthy
	Timeout int  // Timeout in seconds for --wait
}

// HealthCommand checks the server health by calling the /health endpoint.
type HealthCommand struct {
	Config *UnifiedConfig
	Flags  *HealthFlags
	Out    io.Writer
}

func (c *HealthCommand) Type() string {
	return "health"
}

func (c *HealthCommand) Execute() error {
	c.Out = getWriter(c.Out)
	// Get port
	port := c.Config.Server.Port
	if port == 0 {
		port = 3333
	}

	// Check if server is running via PID file
	pidFile := c.Config.Server.PIDFile
	if pidFile == "" {
		homeDir, _ := os.UserHomeDir()
		pidFile = filepath.Join(homeDir, ".local", "state", "brain-api", "brain-api.pid")
	}

	state, err := lifecycle.GetServerStatus(pidFile, port)
	if err != nil {
		fmt.Fprintf(c.Out, "Error: %v\n", err)
		return fmt.Errorf("server unreachable")
	}

	if state.Status != lifecycle.ServerStatusRunning {
		fmt.Fprintf(c.Out, "Server not running (status: %s)\n", state.Status)
		return fmt.Errorf("server not running")
	}

	// If --wait flag, wait for server to become healthy
	if c.Flags.Wait {
		timeout := c.Flags.Timeout
		if timeout == 0 {
			timeout = 30 // Default 30 seconds
		}
		return c.waitForHealth(port, timeout)
	}

	// Check health endpoint
	return c.checkHealth(port)
}

func (c *HealthCommand) checkHealth(port int) error {
	url := fmt.Sprintf("http://localhost:%d/api/v1/health", port)
	
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(c.Out, "Health check failed: %v\n", err)
		return fmt.Errorf("server unreachable")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(c.Out, "Health check returned status %d\n", resp.StatusCode)
		return fmt.Errorf("unhealthy")
	}

	// Parse response
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Fprintf(c.Out, "Failed to parse health response: %v\n", err)
		return fmt.Errorf("unhealthy")
	}

	// Print health info
	status, _ := result["status"].(string)
	timestamp, _ := result["timestamp"].(string)
	
	fmt.Fprintf(c.Out, "Status: %s\n", status)
	fmt.Fprintf(c.Out, "Timestamp: %s\n", timestamp)

	if status != "healthy" {
		return fmt.Errorf("unhealthy")
	}

	return nil
}

func (c *HealthCommand) waitForHealth(port int, timeout int) error {
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	
	for time.Now().Before(deadline) {
		err := c.checkHealth(port)
		if err == nil {
			return nil
		}
		time.Sleep(1 * time.Second)
	}

	fmt.Fprintf(c.Out, "Timeout waiting for server to become healthy\n")
	return fmt.Errorf("timeout")
}
