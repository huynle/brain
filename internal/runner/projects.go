package runner

import "path/filepath"

// FilterProjects applies include and exclude glob patterns to a list of projects.
// If include is non-empty, only projects matching at least one include pattern are kept.
// Projects matching any exclude pattern are removed.
// Exclude takes precedence over include (a project matching both is excluded).
func FilterProjects(projects, include, exclude []string) []string {
	if len(projects) == 0 {
		return nil
	}

	result := make([]string, 0, len(projects))

	for _, p := range projects {
		// Step 1: If include patterns specified, project must match at least one.
		if len(include) > 0 {
			if !matchesAny(p, include) {
				continue
			}
		}

		// Step 2: If project matches any exclude pattern, skip it.
		if matchesAny(p, exclude) {
			continue
		}

		result = append(result, p)
	}

	return result
}

// matchesAny returns true if name matches at least one of the glob patterns.
// Invalid patterns are skipped gracefully.
func matchesAny(name string, patterns []string) bool {
	for _, pattern := range patterns {
		matched, err := filepath.Match(pattern, name)
		if err != nil {
			// Invalid glob pattern — skip it.
			continue
		}
		if matched {
			return true
		}
	}
	return false
}
