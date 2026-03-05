package plugins

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// InstallOptions contains options for plugin installation
type InstallOptions struct {
	// Force allows overwriting existing installations
	Force bool

	// DryRun simulates the installation without making changes
	DryRun bool

	// APIURL is the Brain API URL to configure in the plugin
	APIURL string
}

// expandPath expands ~ and ~/ to the user's home directory
func expandPath(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home
	}

	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}

	return path
}

// ensureDir creates a directory and all parent directories if they don't exist
func ensureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// copyFile copies a file from src to dst with the specified mode
func copyFile(src, dst string, mode os.FileMode) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy content: %w", err)
	}

	if err := dstFile.Chmod(mode); err != nil {
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	return nil
}

// getTarget finds a target by ID
func getTarget(targetID string) Target {
	targets := GetAvailableTargets()
	for _, target := range targets {
		if target.ID() == targetID {
			return target
		}
	}
	return nil
}

// InstallPlugin installs a plugin for the specified target
func InstallPlugin(targetID string, opts InstallOptions) error {
	target := getTarget(targetID)
	if target == nil {
		return fmt.Errorf("unknown target: %s", targetID)
	}

	if target.Exists() && !opts.Force {
		return fmt.Errorf("target %s is already installed (use --force to overwrite)", targetID)
	}

	return target.Install(opts)
}

// UninstallPlugin removes a plugin for the specified target
func UninstallPlugin(targetID string) error {
	target := getTarget(targetID)
	if target == nil {
		return fmt.Errorf("unknown target: %s", targetID)
	}

	if !target.Exists() {
		return fmt.Errorf("target %s is not installed", targetID)
	}

	return target.Uninstall()
}
