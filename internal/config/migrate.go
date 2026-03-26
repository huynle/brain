package config

import (
	"log/slog"
	"os"
	"path/filepath"
)

// MigrateDataDir renames the legacy .zk directory to .brain-data if needed.
// Returns the path to the data directory (whether migrated or not).
// Safe to call multiple times — no-op if already migrated or no legacy dir exists.
func MigrateDataDir(brainDir string) string {
	newPath := filepath.Join(brainDir, DataDir)
	legacyPath := filepath.Join(brainDir, LegacyDataDir)

	// If new directory already exists, use it
	if dirExists(newPath) {
		return newPath
	}

	// If legacy directory exists, rename it
	if dirExists(legacyPath) {
		slog.Info("migrating data directory",
			"from", legacyPath,
			"to", newPath,
		)
		if err := os.Rename(legacyPath, newPath); err != nil {
			slog.Warn("failed to migrate data directory, using legacy path",
				"error", err,
				"legacy", legacyPath,
			)
			return legacyPath
		}
		slog.Info("data directory migrated successfully")
		return newPath
	}

	// Neither exists — return new path (will be created on first use)
	return newPath
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
