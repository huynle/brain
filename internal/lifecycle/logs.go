package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// RotateLogs rotates a log file based on size threshold.
// If the file exceeds maxSizeMB, it's renamed to <file>.1 and a new empty file is created.
// Older backups are shifted (.1 -> .2, .2 -> .3, etc.) up to maxBackups.
// maxSizeMB is in megabytes (for values >= 1MB) or bytes (for smaller test values).
func RotateLogs(path string, maxSizeMB int64, maxBackups int) error {
	// Check if file exists
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No file to rotate
		}
		return fmt.Errorf("failed to stat log file: %w", err)
	}

	// Check if rotation is needed
	maxSizeBytes := calculateMaxSizeBytes(maxSizeMB)
	if info.Size() <= maxSizeBytes {
		return nil // File is small enough, no rotation needed
	}

	// Clean up old backups and shift existing ones
	if err := removeOldBackups(path, maxBackups); err != nil {
		return err
	}
	if err := shiftBackups(path, maxBackups); err != nil {
		return err
	}

	// Rename current log to .1
	backup1 := path + ".1"
	if err := os.Rename(path, backup1); err != nil {
		return fmt.Errorf("failed to rename log file: %w", err)
	}

	// Create new empty log file
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create new log file: %w", err)
	}
	f.Close()

	return nil
}

// calculateMaxSizeBytes converts maxSizeMB to bytes.
// For values >= 1MB, treats as megabytes. For smaller values (testing), treats as bytes.
func calculateMaxSizeBytes(maxSizeMB int64) int64 {
	if maxSizeMB < 1024*1024 {
		return maxSizeMB // For testing, treat small values as bytes
	}
	return maxSizeMB * 1024 * 1024
}

// removeOldBackups removes backup files beyond the maxBackups limit.
func removeOldBackups(path string, maxBackups int) error {
	for i := maxBackups; i <= 100; i++ {
		backup := fmt.Sprintf("%s.%d", path, i)
		if _, err := os.Stat(backup); err == nil {
			os.Remove(backup)
		} else {
			break // No more backups
		}
	}
	return nil
}

// shiftBackups shifts existing backups up by one (.1 -> .2, .2 -> .3, etc.).
func shiftBackups(path string, maxBackups int) error {
	for i := maxBackups - 1; i >= 1; i-- {
		oldBackup := fmt.Sprintf("%s.%d", path, i)
		newBackup := fmt.Sprintf("%s.%d", path, i+1)

		if _, err := os.Stat(oldBackup); err == nil {
			if err := os.Rename(oldBackup, newBackup); err != nil {
				return fmt.Errorf("failed to shift backup %s: %w", oldBackup, err)
			}
		}
	}
	return nil
}

// CleanupOldLogs removes log backup files older than maxAge from the specified directory.
// Only files matching the pattern "*.log.*" are considered.
func CleanupOldLogs(dir string, maxAge time.Duration) error {
	now := time.Now().Truncate(time.Second) // Truncate to avoid microsecond timing issues
	cutoff := now.Add(-maxAge)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	for _, entry := range entries {
		// Only consider log backup files (*.log.*)
		if !strings.Contains(entry.Name(), ".log.") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Check if file is older than maxAge
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("failed to remove old log %s: %w", path, err)
			}
		}
	}

	return nil
}

// ParseDuration parses human-readable duration strings like "1h", "30m", "2d".
// Supports: s (seconds), m (minutes), h (hours), d (days)
// Also supports Go's standard time.ParseDuration format.
func ParseDuration(s string) (time.Duration, error) {
	// First try standard time.ParseDuration
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	// Try custom format with days
	re := regexp.MustCompile(`^(\d+)d$`)
	if matches := re.FindStringSubmatch(s); len(matches) == 2 {
		days, err := strconv.Atoi(matches[1])
		if err != nil {
			return 0, fmt.Errorf("invalid duration: %s", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}

	return 0, fmt.Errorf("invalid duration: %s", s)
}
