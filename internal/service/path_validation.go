package service

import (
	"fmt"
	"path/filepath"
	"strings"
)

// validateProjectID protects service-level project directory access, including
// callers that do not pass through HTTP validation.
func validateProjectID(projectID string) error {
	return validatePathSegment(projectID, "project id")
}

// validatePathSegment validates directory structure, not semantic names: custom
// entry types remain supported. Callers apply any documented defaults first.
func validatePathSegment(value, name string) error {
	if value == "" {
		return fmt.Errorf("%s required", name)
	}
	if value == "." || value == ".." || strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("invalid %s %q", name, value)
	}
	if strings.ContainsAny(value, `/\`) || strings.Contains(value, "..") {
		return fmt.Errorf("invalid %s %q: must not contain path separators", name, value)
	}
	// Cleaning must not change the meaning of a plain directory name.
	if filepath.Clean(value) != value {
		return fmt.Errorf("invalid %s %q", name, value)
	}
	return nil
}
