// Package pathutil provides cross-platform path manipulation utilities.
package pathutil

import (
	"os"
	"path/filepath"
	"strings"
)

// ExpandTilde replaces a leading ~ in a file path with the user's home directory.
//
// Examples:
//
//	"~/brain"     → "/home/user/brain"
//	"~"           → "/home/user"
//	"/abs/path"   → "/abs/path"  (unchanged)
//	"relative"    → "relative"   (unchanged)
//	""            → ""           (unchanged)
func ExpandTilde(path string) string {
	if path == "" {
		return path
	}
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
