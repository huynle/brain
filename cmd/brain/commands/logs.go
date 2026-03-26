package commands

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/lifecycle"
)

// LogsFlags holds flags for the logs command.
type LogsFlags struct {
	Follow bool   // Follow log output (like tail -f)
	Lines  int    // Number of lines to show
	Since  string // Show logs since duration (e.g., "1h", "30m", "2d")
	Level  string // Filter by log level (debug, info, warn, error)
}

// LogsCommand displays server logs.
type LogsCommand struct {
	Config *UnifiedConfig
	Flags  *LogsFlags
	Out    io.Writer
}

func (c *LogsCommand) Type() string {
	return "logs"
}

func (c *LogsCommand) Execute() error {
	c.Out = getWriter(c.Out)
	// Determine log file path
	logFile := c.Config.Server.LogFile
	if logFile == "" {
		homeDir, _ := os.UserHomeDir()
		logFile = filepath.Join(homeDir, ".local", "state", "brain-api", "brain-api.log")
	}

	// Check if log file exists
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		fmt.Fprintf(c.Out, "Log file does not exist: %s\n", logFile)
		return nil
	}

	// Parse since duration if provided
	var since time.Duration
	if c.Flags.Since != "" {
		var err error
		since, err = lifecycle.ParseDuration(c.Flags.Since)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", c.Flags.Since, err)
		}
	}

	// Open log file
	f, err := os.Open(logFile)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer f.Close()

	// If following, tail the file
	if c.Flags.Follow {
		return c.followLogs(f, since)
	}

	// Otherwise, show last N lines
	return c.showLines(f, since)
}

func (c *LogsCommand) showLines(f *os.File, since time.Duration) error {
	lines := c.Flags.Lines
	if lines == 0 {
		lines = 100 // Default 100 lines
	}

	// Calculate cutoff time if since is specified
	var cutoff time.Time
	if since > 0 {
		cutoff = time.Now().Add(-since)
	}

	// Read all lines
	var allLines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		
		// Apply filters
		if !c.shouldShowLine(line, cutoff) {
			continue
		}
		
		allLines = append(allLines, line)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read log file: %w", err)
	}

	// Show last N lines
	start := 0
	if len(allLines) > lines {
		start = len(allLines) - lines
	}

	for i := start; i < len(allLines); i++ {
		fmt.Fprintln(c.Out, allLines[i])
	}

	return nil
}

func (c *LogsCommand) followLogs(f *os.File, since time.Duration) error {
	// First, show existing content
	if err := c.showLines(f, since); err != nil {
		return err
	}

	// Calculate cutoff time if since is specified
	var cutoff time.Time
	if since > 0 {
		cutoff = time.Now().Add(-since)
	}

	// Then watch for new lines
	scanner := bufio.NewScanner(f)
	for {
		if scanner.Scan() {
			line := scanner.Text()
			
			// Apply filters
			if c.shouldShowLine(line, cutoff) {
				fmt.Fprintln(c.Out, line)
			}
		} else {
			// No new data, sleep briefly
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// shouldShowLine checks if a log line should be displayed based on filters.
func (c *LogsCommand) shouldShowLine(line string, cutoff time.Time) bool {
	// Time filter (simple heuristic - check if line starts with RFC3339-like timestamp)
	if !cutoff.IsZero() {
		// Try to parse timestamp from line (assuming format: 2006-01-02T15:04:05...)
		if len(line) >= 19 {
			if ts, err := time.ParseInLocation("2006-01-02T15:04:05", line[:19], time.Local); err == nil {
				if ts.Before(cutoff) {
					return false
				}
			}
		}
	}

	// Level filter
	if c.Flags.Level != "" {
		level := strings.ToLower(c.Flags.Level)
		lineLower := strings.ToLower(line)
		
		// Check if line contains the level
		switch level {
		case "debug":
			if !strings.Contains(lineLower, "debug") && !strings.Contains(lineLower, "dbug") {
				return false
			}
		case "info":
			if !strings.Contains(lineLower, "info") {
				return false
			}
		case "warn", "warning":
			if !strings.Contains(lineLower, "warn") {
				return false
			}
		case "error", "err":
			if !strings.Contains(lineLower, "error") && !strings.Contains(lineLower, "err") {
				return false
			}
		}
	}

	return true
}
