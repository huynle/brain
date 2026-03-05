package commands

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/huynle/brain-api/internal/doctor"
)

// DoctorFlags holds flags for the doctor command.
type DoctorFlags struct {
	Fix              bool
	Force            bool
	DryRun           bool
	Verbose          bool
	SkipVersionCheck bool
}

// DoctorCommand implements the doctor command.
type DoctorCommand struct {
	Config *UnifiedConfig
	Flags  *DoctorFlags
	Out    io.Writer
}

// Type returns the command type.
func (c *DoctorCommand) Type() string {
	return "doctor"
}

// Execute runs the doctor command.
func (c *DoctorCommand) Execute() error {
	// Get writer
	out := c.Out
	if out == nil {
		out = os.Stdout
	}

	// Expand tilde in brain directory path
	brainDir := expandPath(c.Config.Server.BrainDir)

	// Create doctor options
	opts := doctor.DoctorOptions{
		Fix:              c.Flags.Fix,
		Force:            c.Flags.Force,
		DryRun:           c.Flags.DryRun,
		Verbose:          c.Flags.Verbose,
		SkipVersionCheck: c.Flags.SkipVersionCheck,
		BrainDir:         brainDir,
	}

	// Create doctor service
	service := doctor.NewDoctorService()

	var result *doctor.DoctorResult
	var err error

	// Run diagnostics or fix
	if c.Flags.Fix {
		if c.Flags.DryRun {
			fmt.Fprintf(out, "🔍 DRY RUN: Checking what would be fixed...\n\n")
		} else {
			fmt.Fprintf(out, "🔧 Running diagnostics and fixes...\n\n")
		}
		result, err = service.Fix(opts)
	} else {
		fmt.Fprintf(out, "🔍 Running diagnostics...\n\n")
		result, err = service.Diagnose(opts)
	}

	if err != nil {
		return fmt.Errorf("doctor failed: %w", err)
	}

	// Display results
	c.displayResults(out, result)

	return nil
}

// displayResults outputs the check results.
func (c *DoctorCommand) displayResults(out io.Writer, result *doctor.DoctorResult) {
	// Track counts
	passCount := 0
	warnCount := 0
	failCount := 0
	fixableCount := 0

	// Display each check
	for _, check := range result.Checks {
		// Skip passed checks unless verbose
		if check.Status == doctor.CheckStatusPass && !c.Flags.Verbose {
			passCount++
			continue
		}

		// Count status
		switch check.Status {
		case doctor.CheckStatusPass:
			passCount++
		case doctor.CheckStatusWarn:
			warnCount++
		case doctor.CheckStatusFail:
			failCount++
			if check.Fixable {
				fixableCount++
			}
		}

		// Display check
		fmt.Fprintf(out, "%s  %s\n", check.Status.Symbol(), formatCheckName(check.Name))
		if check.Message != "" {
			fmt.Fprintf(out, "   %s\n", check.Message)
		}
	}

	// Display summary
	fmt.Fprintf(out, "\n📊 Summary:\n")
	fmt.Fprintf(out, "   ✅ Passed: %d\n", passCount)
	if warnCount > 0 {
		fmt.Fprintf(out, "   ⚠️  Warnings: %d\n", warnCount)
	}
	if failCount > 0 {
		fmt.Fprintf(out, "   ❌ Failed: %d\n", failCount)
	}

	// Suggest fixes if needed
	if failCount > 0 && !c.Flags.Fix && fixableCount > 0 {
		fmt.Fprintf(out, "\n💡 Tip: Run with --fix to automatically resolve %d fixable issue(s)\n", fixableCount)
	}

	// Report fix success
	if c.Flags.Fix && !c.Flags.DryRun && failCount == 0 {
		fmt.Fprintf(out, "\n✅ All issues resolved!\n")
	}

	// Report dry run
	if c.Flags.DryRun && c.Flags.Fix {
		fmt.Fprintf(out, "\n💡 This was a dry run. Use --fix without --dry-run to apply changes.\n")
	}
}

// formatCheckName converts a check name to a readable format.
func formatCheckName(name string) string {
	// Convert kebab-case to Title Case
	parts := strings.Split(name, "-")
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, " ")
}
