package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/huynle/brain-api/cmd/brain/assets"
	"github.com/huynle/brain-api/internal/runner"
)

// =============================================================================
// Migrate Command
// =============================================================================

// MigrateFlags holds flags for the migrate command.
type MigrateFlags struct {
	DryRun bool   // --dry-run
	Force  bool   // --force (overwrite existing automation entries)
	Format string // --format (json, short)
}

// MigrateCommand implements the Command interface for migration operations.
type MigrateCommand struct {
	Subcommand string // "automations" (only supported subcommand for now)
	Config     *UnifiedConfig
	Flags      *MigrateFlags
	Out        io.Writer

	// apiClient is injectable for testing; nil means create from config.
	apiClient *runner.APIClient
}

// Type returns the command type identifier.
func (c *MigrateCommand) Type() string {
	return "migrate"
}

// Execute runs the migrate command.
func (c *MigrateCommand) Execute() error {
	out := c.out()

	switch c.Subcommand {
	case "automations":
		return c.executeAutomations(out)
	case "":
		return fmt.Errorf("missing subcommand\nUsage: brain migrate automations [flags]\n\nAvailable subcommands:\n  automations  Convert hardcoded monitor tasks to automation entries")
	default:
		return fmt.Errorf("unknown migrate subcommand: %q\nUsage: brain migrate automations [flags]", c.Subcommand)
	}
}

// out returns the output writer, defaulting to os.Stdout.
func (c *MigrateCommand) out() io.Writer {
	if c.Out != nil {
		return c.Out
	}
	return os.Stdout
}

// getAPIClient returns the injected client or creates one from config.
func (c *MigrateCommand) getAPIClient() *runner.APIClient {
	if c.apiClient != nil {
		return c.apiClient
	}
	c.apiClient = runner.NewAPIClient(c.Config.Runner)
	return c.apiClient
}

// templateToAutomationFile maps monitor template IDs to their automation file names.
var templateToAutomationFile = map[string]string{
	"blocked-inspector": "blocked-inspector.md",
	"dream":             "dream-consolidation.md",
	"feature-review":    "feature-review.md",
}

// executeAutomations migrates existing hardcoded monitor tasks to automation entries.
//
// The migration performs two steps:
//  1. Deploy default automation entry files to global/automation/ (same as brain init)
//  2. Find existing monitor tasks and disable their schedules (preserving them for reference)
func (c *MigrateCommand) executeAutomations(out io.Writer) error {
	brainDir := expandPath(c.Config.Server.BrainDir)

	fmt.Fprintln(out, "Migrate: Hardcoded Monitors → Automation Entries")
	fmt.Fprintln(out, strings.Repeat("─", 55))
	fmt.Fprintln(out)

	// Step 1: Deploy default automation entries from embedded assets
	fmt.Fprintln(out, "Step 1: Deploy default automation entries")
	fmt.Fprintln(out)

	automationsDir := filepath.Join(brainDir, "global", "automation")

	// Ensure directory exists
	if !c.Flags.DryRun {
		if err := os.MkdirAll(automationsDir, 0755); err != nil {
			return fmt.Errorf("create automation directory: %w", err)
		}
	}

	automationFiles := assets.ListAutomations()
	createdCount := 0
	skippedCount := 0

	for _, name := range automationFiles {
		destPath := filepath.Join(automationsDir, name)
		exists := fileExists(destPath)

		if exists && !c.Flags.Force {
			skippedCount++
			if c.Flags.DryRun {
				fmt.Fprintf(out, "  DRY RUN: Would skip %s (already exists)\n", name)
			} else {
				fmt.Fprintf(out, "  ⏭  Skipped %s (already exists)\n", name)
			}
			continue
		}

		if c.Flags.DryRun {
			if exists {
				fmt.Fprintf(out, "  DRY RUN: Would overwrite %s\n", name)
			} else {
				fmt.Fprintf(out, "  DRY RUN: Would create %s\n", name)
			}
			createdCount++
			continue
		}

		content, err := assets.GetAutomation(name)
		if err != nil {
			fmt.Fprintf(out, "  ⚠️  Failed to load %s: %v\n", name, err)
			continue
		}

		if err := os.WriteFile(destPath, content, 0644); err != nil {
			fmt.Fprintf(out, "  ⚠️  Failed to write %s: %v\n", name, err)
			continue
		}

		createdCount++
		fmt.Fprintf(out, "  ✅ Created %s\n", name)
	}

	fmt.Fprintf(out, "\n  Created: %d, Skipped: %d\n", createdCount, skippedCount)

	// Step 2: Find and disable existing monitor tasks
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Step 2: Disable existing monitor tasks")
	fmt.Fprintln(out)

	client := c.getAPIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Search for tasks with monitor tags
	params := map[string]string{
		"type": "task",
		"tags": "monitor",
	}

	resp, err := client.ListEntries(ctx, params)
	if err != nil {
		fmt.Fprintf(out, "  ⚠️  Could not connect to brain API: %v\n", err)
		fmt.Fprintln(out, "  Skipping monitor task migration (API unavailable).")
		fmt.Fprintln(out, "  Run this command again when the API is running to complete migration.")
		return nil // Don't fail — file deployment still succeeded
	}

	if len(resp.Entries) == 0 {
		fmt.Fprintln(out, "  No existing monitor tasks found.")
		fmt.Fprintln(out)
		c.printSummary(out, createdCount, skippedCount, 0)
		return nil
	}

	disabledCount := 0
	for _, entry := range resp.Entries {
		// Check if this is a monitor task (has monitor:* tag)
		var monitorTag string
		for _, tag := range entry.Tags {
			if strings.HasPrefix(tag, "monitor:") {
				monitorTag = tag
				break
			}
		}
		if monitorTag == "" {
			continue
		}

		// Check if the monitor is for a template we've migrated
		templateID := extractTemplateID(monitorTag)
		if _, ok := templateToAutomationFile[templateID]; !ok {
			fmt.Fprintf(out, "  ⏭  Skipped %s (%s) — unknown template %q\n", entry.Title, entry.ID, templateID)
			continue
		}

		// Check if already disabled
		if entry.ScheduleEnabled != nil && !*entry.ScheduleEnabled {
			fmt.Fprintf(out, "  ⏭  Already disabled: %s (%s)\n", entry.Title, entry.ID)
			continue
		}

		if c.Flags.DryRun {
			fmt.Fprintf(out, "  DRY RUN: Would disable %s (%s)\n", entry.Title, entry.ID)
			disabledCount++
			continue
		}

		// Disable the schedule on the old monitor task
		updates := map[string]interface{}{
			"schedule_enabled": false,
			"append":           "Migrated to automation entry. Schedule disabled by `brain migrate automations`.",
		}
		_, err := client.UpdateEntry(ctx, entry.Path, updates)
		if err != nil {
			fmt.Fprintf(out, "  ⚠️  Failed to disable %s (%s): %v\n", entry.Title, entry.ID, err)
			continue
		}

		disabledCount++
		fmt.Fprintf(out, "  ✅ Disabled %s (%s)\n", entry.Title, entry.ID)
	}

	fmt.Fprintln(out)
	c.printSummary(out, createdCount, skippedCount, disabledCount)
	return nil
}

// printSummary prints the migration summary.
func (c *MigrateCommand) printSummary(out io.Writer, created, skipped, disabled int) {
	if c.Flags.DryRun {
		fmt.Fprintln(out, "DRY RUN Summary:")
	} else {
		fmt.Fprintln(out, "Migration complete!")
	}
	fmt.Fprintf(out, "  Automation entries created: %d\n", created)
	if skipped > 0 {
		fmt.Fprintf(out, "  Automation entries skipped: %d (use --force to overwrite)\n", skipped)
	}
	fmt.Fprintf(out, "  Monitor tasks disabled:    %d\n", disabled)
	if !c.Flags.DryRun {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "The new automation entries are in global/automation/ and will be")
		fmt.Fprintln(out, "picked up by the AutomationMatcher when the API server restarts.")
		fmt.Fprintln(out, "Old monitor tasks are preserved but disabled.")
	}
}

// extractTemplateID extracts the template ID from a monitor tag.
// e.g., "monitor:blocked-inspector:project:brain-api" → "blocked-inspector"
func extractTemplateID(tag string) string {
	const prefix = "monitor:"
	if !strings.HasPrefix(tag, prefix) {
		return ""
	}
	rest := tag[len(prefix):]
	// Template ID is the part before the next colon
	if idx := strings.Index(rest, ":"); idx > 0 {
		return rest[:idx]
	}
	return rest
}
