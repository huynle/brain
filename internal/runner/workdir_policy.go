package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// validateSpawnWorkdir is the shared task/ad-hoc execution-directory policy.
// An empty allowlist means HOME ONLY, never unrestricted execution.
func validateSpawnWorkdir(workdir string, config RunnerConfig) error {
	if workdir == "" || !filepath.IsAbs(workdir) {
		return fmt.Errorf("workdir must be an absolute path")
	}
	info, err := os.Stat(workdir)
	if err != nil {
		return fmt.Errorf("workdir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("workdir is not a directory")
	}
	// Resolve before cleaning: link/../dir follows link before traversing up.
	canonical, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		return fmt.Errorf("canonical workdir: %w", err)
	}
	return validateWorkdirPreflight(canonical, config)
}

// canonicalWorkdirPath resolves a prospective directory through its existing
// ancestor, preserving raw symlink/.. semantics until symlinks are resolved.
// Lstat deliberately stops at dangling symlinks, which EvalSymlinks then rejects.
func canonicalWorkdirPath(workdir string) (string, error) {
	if workdir == "" || !filepath.IsAbs(workdir) {
		return "", fmt.Errorf("workdir must be an absolute path")
	}
	candidate := workdir
	suffix := ""
	for {
		// A trailing slash makes Lstat follow a symlink; remove only separators,
		// never dot segments, so dangling ancestors still fail closed.
		candidate = strings.TrimRight(candidate, string(filepath.Separator))
		if candidate == "" {
			candidate = string(filepath.Separator)
		}
		_, err := os.Lstat(candidate)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("workdir: %w", err)
		}
		// Split, unlike Dir/Clean, does not collapse link/.. in the ancestor.
		parent, base := filepath.Split(candidate)
		suffix = filepath.Join(base, suffix)
		candidate = parent
	}
	canonical, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("canonical workdir: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("workdir: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workdir is not a directory")
	}
	return filepath.Join(canonical, suffix), nil
}

// validateWorkdirPreflight checks a prospective directory before mkdir/clone/
// worktree add without creating anything.
func validateWorkdirPreflight(workdir string, config RunnerConfig) error {
	canonical, err := canonicalWorkdirPath(workdir)
	if err != nil {
		return err
	}
	roots := config.Control.AllowedWorkdirRoots
	if len(roots) == 0 {
		home, err := os.UserHomeDir()
		if err != nil || home == "" || !filepath.IsAbs(home) {
			return fmt.Errorf("no allowed workdir roots configured and home unavailable")
		}
		roots = []string{home}
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		abs, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		abs, err = filepath.Abs(abs)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			continue
		}
		rel, err := filepath.Rel(abs, canonical)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return nil
		}
	}
	return fmt.Errorf("workdir %q is not under any allowed root: %v", workdir, roots)
}
