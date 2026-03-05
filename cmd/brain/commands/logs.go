package commands

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// LogsFlags holds flags for the logs command.
type LogsFlags struct {
	Follow bool // Follow log output (like tail -f)
	Lines  int  // Number of lines to show
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

	// Open log file
	f, err := os.Open(logFile)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer f.Close()

	// If following, tail the file
	if c.Flags.Follow {
		return c.followLogs(f)
	}

	// Otherwise, show last N lines
	return c.showLines(f)
}

func (c *LogsCommand) showLines(f *os.File) error {
	lines := c.Flags.Lines
	if lines == 0 {
		lines = 100 // Default 100 lines
	}

	// Read all lines
	var allLines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
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

func (c *LogsCommand) followLogs(f *os.File) error {
	// First, show existing content
	if err := c.showLines(f); err != nil {
		return err
	}

	// Then watch for new lines
	scanner := bufio.NewScanner(f)
	for {
		if scanner.Scan() {
			fmt.Fprintln(c.Out, scanner.Text())
		} else {
			// No new data, sleep briefly
			time.Sleep(100 * time.Millisecond)
		}
	}
}
