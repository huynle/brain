// Package lifecycle provides primitives for server process lifecycle management,
// including PID file management, daemonization, and signal handling.
package lifecycle

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// WritePID writes the given PID to the specified file path.
// The file is created with 0644 permissions.
func WritePID(path string, pid int) error {
	data := []byte(strconv.Itoa(pid))
	return os.WriteFile(path, data, 0o644)
}

// ReadPID reads a PID from the specified file path.
// Returns an error if the file doesn't exist or contains invalid data.
func ReadPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("invalid PID in file: %w", err)
	}

	return pid, nil
}

// ClearPID removes the PID file at the specified path.
// Does not return an error if the file doesn't exist.
func ClearPID(path string) error {
	err := os.Remove(path)
	if err != nil && os.IsNotExist(err) {
		return nil // Not an error if file doesn't exist
	}
	return err
}

// IsProcessRunning checks if a process with the given PID is running.
// Returns false if the process doesn't exist or is not accessible.
func IsProcessRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Sending signal 0 checks if process exists without affecting it
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}
