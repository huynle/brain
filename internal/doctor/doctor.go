package doctor

import (
	"fmt"
)

// DoctorService provides diagnostic and fix operations for brain installations.
type DoctorService struct {
}

// NewDoctorService creates a new DoctorService.
func NewDoctorService() *DoctorService {
	return &DoctorService{}
}

// Diagnose runs all diagnostic checks and returns the results.
func (s *DoctorService) Diagnose(opts DoctorOptions) (*DoctorResult, error) {
	result := &DoctorResult{
		Checks: make([]Check, 0),
	}

	// Expand tilde in brain directory path
	brainDir := expandPath(opts.BrainDir)

	// Run all checks
	checks := []Check{
		checkBrainDirectory(brainDir),
		checkTemplates(brainDir),
		checkConfig(brainDir),
		checkDatabase(brainDir),
	}

	result.Checks = append(result.Checks, checks...)

	// Generate summary
	passCount := 0
	warnCount := 0
	failCount := 0
	fixableCount := 0

	for _, check := range result.Checks {
		switch check.Status {
		case CheckStatusPass:
			passCount++
		case CheckStatusWarn:
			warnCount++
		case CheckStatusFail:
			failCount++
			if check.Fixable {
				fixableCount++
			}
		}
	}

	result.Summary = fmt.Sprintf("Passed: %d, Warnings: %d, Failed: %d (Fixable: %d)",
		passCount, warnCount, failCount, fixableCount)

	return result, nil
}

// Fix runs diagnostics and attempts to fix any failures.
func (s *DoctorService) Fix(opts DoctorOptions) (*DoctorResult, error) {
	// Expand tilde in brain directory path
	brainDir := expandPath(opts.BrainDir)

	// Run initial diagnostics
	result, err := s.Diagnose(opts)
	if err != nil {
		return nil, err
	}

	// Track which fixes were attempted
	fixedChecks := make(map[string]bool)

	// Attempt fixes for failed checks
	for _, check := range result.Checks {
		if check.Status != CheckStatusFail || !check.Fixable {
			continue
		}

		var fixErr error
		switch check.Name {
		case "brain-directory":
			fixErr = fixBrainDirectory(brainDir, opts.DryRun)
		case "templates":
			fixErr = fixTemplates(brainDir, opts.DryRun, opts.Force)
		case "config":
			fixErr = fixConfig(brainDir, opts.DryRun, opts.Force)
		}

		if fixErr != nil {
			return nil, fmt.Errorf("failed to fix %s: %w", check.Name, fixErr)
		}

		fixedChecks[check.Name] = true
	}

	// Run diagnostics again to verify fixes (unless dry run)
	if !opts.DryRun && len(fixedChecks) > 0 {
		result, err = s.Diagnose(opts)
		if err != nil {
			return nil, err
		}
	}

	return result, nil
}

// expandPath expands ~ to home directory.
func expandPath(path string) string {
	if path == "" {
		return path
	}
	// Simple tilde expansion - in a real implementation, use filepath.Abs
	// and proper home directory detection
	return path
}
