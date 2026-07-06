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
	"github.com/huynle/brain-api/internal/service"
	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/frontmatter"
)

// =============================================================================
// Migrate Command
// =============================================================================

// MigrateFlags holds flags for the migrate command.
type MigrateFlags struct {
	DryRun  bool   // --dry-run
	Force   bool   // --force (overwrite existing automation entries)
	Format  string // --format (json, short)
	Project string // --project (scope goal migration to a project)
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
	case "goals":
		return c.executeGoals(out)
	case "":
		return fmt.Errorf("missing subcommand\nUsage: brain migrate <subcommand> [flags]\n\nAvailable subcommands:\n  automations  Convert hardcoded monitor tasks to automation entries\n  goals        Convert legacy V1 goals to goal automation entries")
	default:
		return fmt.Errorf("unknown migrate subcommand: %q\nUsage: brain migrate <subcommand> [flags]\n\nAvailable subcommands:\n  automations  Convert hardcoded monitor tasks to automation entries\n  goals        Convert legacy V1 goals to goal automation entries", c.Subcommand)
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

	client := c.getAPIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Step 2: Create missing automation entries through the configured API.
	// This makes migrations work for remote/API-backed TUIs where writing local
	// files is not enough to update the active index.
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Step 2: Sync automation entries to API")
	fmt.Fprintln(out)

	apiCreatedCount, apiSkippedCount, apiAvailable := c.syncDefaultAutomationsToAPI(ctx, out, client, automationFiles)

	// Step 3: Find and disable existing monitor tasks
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Step 3: Disable existing monitor tasks")
	fmt.Fprintln(out)

	// Search for tasks with monitor tags
	params := map[string]string{
		"type": "task",
		"tags": "monitor",
	}

	resp, err := client.ListEntries(ctx, params)
	if err != nil {
		if apiAvailable {
			fmt.Fprintf(out, "  ⚠️  Could not list monitor tasks: %v\n", err)
		} else {
			fmt.Fprintln(out, "  Skipping monitor task migration (API unavailable).")
			fmt.Fprintln(out, "  Run this command again when the API is running to complete migration.")
		}
		return nil // Don't fail — file deployment still succeeded
	}

	if len(resp.Entries) == 0 {
		fmt.Fprintln(out, "  No existing monitor tasks found.")
		fmt.Fprintln(out)
		c.printSummary(out, createdCount, skippedCount, apiCreatedCount, apiSkippedCount, 0)
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
	c.printSummary(out, createdCount, skippedCount, apiCreatedCount, apiSkippedCount, disabledCount)
	return nil
}

func (c *MigrateCommand) syncDefaultAutomationsToAPI(ctx context.Context, out io.Writer, client *runner.APIClient, automationFiles []string) (created, skipped int, available bool) {
	existing, err := client.ListEntries(ctx, map[string]string{"type": "automation", "global": "true", "limit": "1000"})
	if err != nil {
		fmt.Fprintf(out, "  ⚠️  Could not connect to brain API: %v\n", err)
		fmt.Fprintln(out, "  Skipping API automation sync.")
		return 0, 0, false
	}

	existingByTitle := make(map[string]types.BrainEntry, len(existing.Entries))
	for _, entry := range existing.Entries {
		existingByTitle[entry.Title] = entry
	}

	for _, name := range automationFiles {
		content, err := assets.GetAutomation(name)
		if err != nil {
			fmt.Fprintf(out, "  ⚠️  Failed to load %s: %v\n", name, err)
			continue
		}

		req, err := automationAssetCreateRequest(content)
		if err != nil {
			fmt.Fprintf(out, "  ⚠️  Failed to parse %s: %v\n", name, err)
			continue
		}

		if existing, ok := existingByTitle[req.Title]; ok && !c.Flags.Force {
			skipped++
			if c.Flags.DryRun {
				fmt.Fprintf(out, "  DRY RUN: Would skip API entry %s (already exists as %s)\n", req.Title, existing.ID)
			} else {
				fmt.Fprintf(out, "  ⏭  Skipped API entry %s (already exists as %s)\n", req.Title, existing.ID)
			}
			continue
		}

		if c.Flags.DryRun {
			created++
			fmt.Fprintf(out, "  DRY RUN: Would create API entry %s\n", req.Title)
			continue
		}

		createdEntry, err := client.CreateEntry(ctx, req)
		if err != nil {
			fmt.Fprintf(out, "  ⚠️  Failed to create API entry %s: %v\n", req.Title, err)
			continue
		}

		created++
		fmt.Fprintf(out, "  ✅ Created API entry %s (%s)\n", req.Title, createdEntry.ID)
	}

	return created, skipped, true
}

func automationAssetCreateRequest(content []byte) (types.CreateEntryRequest, error) {
	doc, err := frontmatter.Parse(string(content))
	if err != nil {
		return types.CreateEntryRequest{}, err
	}
	fm := doc.Frontmatter
	if fm.Type != "automation" {
		return types.CreateEntryRequest{}, fmt.Errorf("asset type = %q, want automation", fm.Type)
	}
	global := true
	return types.CreateEntryRequest{
		Type:    fm.Type,
		Title:   fm.Title,
		Content: doc.Body,
		Tags:    fm.Tags,
		Status:  fm.Status,
		Global:  &global,
		MaxRuns: fm.MaxRuns,
		Trigger: automationAssetTrigger(fm.Trigger),
		Action:  automationAssetAction(fm.Action),
		Retry:   automationAssetRetry(fm.Retry),
	}, nil
}

func automationAssetTrigger(t *frontmatter.TriggerConfig) *types.TriggerConfig {
	if t == nil {
		return nil
	}
	return &types.TriggerConfig{
		Type:                   t.Type,
		Event:                  t.Event,
		Schedule:               t.Schedule,
		Filter:                 t.Filter,
		OncePer:                t.OncePer,
		Webhook:                t.Webhook,
		IgnoreAutomationEvents: t.IgnoreAutomationEvents,
		Cooldown:               t.Cooldown,
		MaxConcurrent:          t.MaxConcurrent,
	}
}

func automationAssetAction(a *frontmatter.AutomationAction) *types.AutomationAction {
	if a == nil {
		return nil
	}
	return &types.AutomationAction{
		Type:               a.Type,
		DirectPrompt:       a.DirectPrompt,
		Command:            a.Command,
		Agent:              a.Agent,
		Model:              a.Model,
		ExecutionMode:      a.ExecutionMode,
		CompleteOnIdle:     a.CompleteOnIdle,
		Timeout:            a.Timeout,
		RequiresCapability: a.RequiresCapability,
	}
}

func automationAssetRetry(r *frontmatter.AutomationRetry) *types.AutomationRetry {
	if r == nil {
		return nil
	}
	return &types.AutomationRetry{
		MaxAttempts: r.MaxAttempts,
		Backoff:     r.Backoff,
		Delay:       r.Delay,
	}
}

// printSummary prints the migration summary.
func (c *MigrateCommand) printSummary(out io.Writer, created, skipped, apiCreated, apiSkipped, disabled int) {
	if c.Flags.DryRun {
		fmt.Fprintln(out, "DRY RUN Summary:")
	} else {
		fmt.Fprintln(out, "Migration complete!")
	}
	fmt.Fprintf(out, "  Automation files created:   %d\n", created)
	if skipped > 0 {
		fmt.Fprintf(out, "  Automation files skipped:   %d (use --force to overwrite)\n", skipped)
	}
	fmt.Fprintf(out, "  API entries created:        %d\n", apiCreated)
	if apiSkipped > 0 {
		fmt.Fprintf(out, "  API entries skipped:        %d (use --force to create new copies)\n", apiSkipped)
	}
	fmt.Fprintf(out, "  Monitor tasks disabled:    %d\n", disabled)
	if !c.Flags.DryRun {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "The new automation entries are in global/automation/ and are synced")
		fmt.Fprintln(out, "to the configured Brain API when available.")
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

// =============================================================================
// Migrate: Legacy Goals → Goal Automation Entries
// =============================================================================

// goalReconcilerTag marks a legacy V1 goal reconciler task.
const goalReconcilerTag = "goal:reconciler"

// goalPlanTag marks a legacy V1 goal plan entry.
const goalPlanTag = "goal:plan"

// tagsContain reports whether the tag slice contains the given tag.
func tagsContain(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

// goalSlugFromGeneratedKey extracts the goal slug from a generated key of the
// form "goal:<slug>:<suffix>" (e.g. "goal:oauth:plan" → "oauth" with
// suffix ":plan"). Returns empty when the key does not match.
func goalSlugFromGeneratedKey(key, suffix string) string {
	key = strings.TrimSpace(key)
	const prefix = "goal:"
	if key == "" || !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, suffix) {
		return ""
	}
	slug := key[len(prefix) : len(key)-len(suffix)]
	return strings.TrimSpace(slug)
}

// goalIDTagSlug returns the goal id encoded in a "goal:<id>" tag, excluding the
// reserved V1 marker tags. Returns empty when the tag is not a goal-id tag.
func goalIDTagSlug(tag string) string {
	const prefix = "goal:"
	if !strings.HasPrefix(tag, prefix) {
		return ""
	}
	id := strings.TrimSpace(tag[len(prefix):])
	switch id {
	case "", "v1", "plan", "reconciler", "automation", "implementation":
		return ""
	}
	return id
}

// executeGoals migrates legacy V1 goals (goal:plan + goal:reconciler) to goal
// automation entries, then disables/annotates the legacy entries.
//
// The migration performs three steps:
//  1. List legacy goal plans (and their paired reconciler tasks).
//  2. Convert each plan into a goal automation entry via the service layer and
//     create it through the API (deduping against existing goal automations).
//  3. Disable/annotate the successfully converted legacy plan + reconciler.
//
// When the brain API is unavailable the command degrades gracefully: it prints
// a notice and returns nil (no error) so the caller is not failed.
func (c *MigrateCommand) executeGoals(out io.Writer) error {
	fmt.Fprintln(out, "Migrate: Legacy Goals → Goal Automation Entries")
	fmt.Fprintln(out, strings.Repeat("─", 55))
	fmt.Fprintln(out)

	client := c.getAPIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// -------------------------------------------------------------------------
	// Step 1: List legacy goal plans.
	// -------------------------------------------------------------------------
	fmt.Fprintln(out, "Step 1: List legacy goal plans")
	fmt.Fprintln(out)

	planParams := map[string]string{"type": "plan", "tags": goalPlanTag}
	if c.Flags.Project != "" {
		planParams["project"] = c.Flags.Project
	}

	plansResp, err := client.ListEntries(ctx, planParams)
	if err != nil {
		// API-down graceful handling: do NOT return the error.
		fmt.Fprintf(out, "  ⚠️  Could not connect to brain API: %v\n", err)
		fmt.Fprintln(out, "  Skipping goal migration (API unavailable). Run again when the API is running.")
		return nil
	}

	// Filter to entries that actually carry the goal:plan tag (the API tag
	// filter may be loose).
	var plans []types.BrainEntry
	for _, entry := range plansResp.Entries {
		if tagsContain(entry.Tags, goalPlanTag) {
			plans = append(plans, entry)
		}
	}

	if len(plans) == 0 {
		fmt.Fprintln(out, "  No legacy goals found.")
		fmt.Fprintln(out)
		c.printGoalsSummary(out, 0, 0, 0)
		return nil
	}

	fmt.Fprintf(out, "  Found %d legacy goal plan(s).\n", len(plans))

	// -------------------------------------------------------------------------
	// Pair reconciler tasks by slug.
	// -------------------------------------------------------------------------
	recParams := map[string]string{"type": "task", "tags": goalReconcilerTag}
	if c.Flags.Project != "" {
		recParams["project"] = c.Flags.Project
	}

	reconcilersBySlug := make(map[string]types.BrainEntry)
	if recResp, recErr := client.ListEntries(ctx, recParams); recErr != nil {
		// Reconciler is optional; warn but continue with an empty map.
		fmt.Fprintf(out, "  ⚠️  Could not list reconciler tasks: %v (continuing without reconcilers)\n", recErr)
	} else {
		for _, entry := range recResp.Entries {
			// Defensive: only include entries that actually carry the
			// goal:reconciler tag (the API tag filter may be loose).
			if !tagsContain(entry.Tags, goalReconcilerTag) {
				continue
			}
			slug := goalSlugFromGeneratedKey(entry.GeneratedKey, ":reconcile")
			if slug == "" {
				continue
			}
			reconcilersBySlug[slug] = entry
		}
	}

	// -------------------------------------------------------------------------
	// List existing goal automations for dedup.
	// -------------------------------------------------------------------------
	existingGoalIDs := make(map[string]bool)
	autoParams := map[string]string{"type": "automation", "tags": "goal"}
	if c.Flags.Project != "" {
		autoParams["project"] = c.Flags.Project
	}
	if autoResp, autoErr := client.ListEntries(ctx, autoParams); autoErr == nil {
		for _, entry := range autoResp.Entries {
			for _, tag := range entry.Tags {
				if id := goalIDTagSlug(tag); id != "" {
					existingGoalIDs[id] = true
				}
			}
		}
	}

	// -------------------------------------------------------------------------
	// Step 2: Convert each plan into a goal automation entry.
	// -------------------------------------------------------------------------
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Step 2: Convert legacy goals to automation entries")
	fmt.Fprintln(out)

	created := 0
	skipped := 0

	// Track converted plans + reconcilers for Step 3 disabling.
	var convertedPlans []types.BrainEntry
	var convertedReconcilers []types.BrainEntry

	for _, plan := range plans {
		slug := goalSlugFromGeneratedKey(plan.GeneratedKey, ":plan")
		var reconcilerPtr *types.BrainEntry
		var reconciler types.BrainEntry
		hasReconciler := false
		if slug != "" {
			if rec, ok := reconcilersBySlug[slug]; ok {
				reconciler = rec
				reconcilerPtr = &reconciler
				hasReconciler = true
			}
		}

		input, convErr := service.LegacyGoalToInput(plan, reconcilerPtr)
		if convErr != nil {
			fmt.Fprintf(out, "  ⚠️  Failed to convert %s (%s): %v\n", plan.Title, plan.ID, convErr)
			continue
		}

		built, buildErr := service.BuildGoalAutomation(input)
		if buildErr != nil {
			fmt.Fprintf(out, "  ⚠️  Failed to build goal automation for %s (%s): %v\n", plan.Title, plan.ID, buildErr)
			continue
		}

		// Dedup against existing goal automations.
		if existingGoalIDs[input.Config.ID] && !c.Flags.Force {
			fmt.Fprintf(out, "  ⏭  Skipped %s (goal automation already exists)\n", input.Config.ID)
			skipped++
			continue
		}

		if c.Flags.DryRun {
			fmt.Fprintf(out, "  DRY RUN: Would create goal automation %s (id=%s)\n", built.Title, input.Config.ID)
			created++
			convertedPlans = append(convertedPlans, plan)
			if hasReconciler {
				convertedReconcilers = append(convertedReconcilers, reconciler)
			}
			continue
		}

		req := types.CreateEntryRequest{
			Type:        built.Type,
			Title:       built.Title,
			Content:     built.Content,
			Tags:        built.Tags,
			Status:      built.Status,
			Project:     built.ProjectID,
			FeatureID:   built.FeatureID,
			Trigger:     built.Trigger,
			Action:      built.Action,
			Goal:        built.Goal,
			GeneratedBy: built.GeneratedBy,
		}

		createdEntry, createErr := client.CreateEntry(ctx, req)
		if createErr != nil {
			fmt.Fprintf(out, "  ⚠️  Failed to create goal automation %s: %v\n", input.Config.ID, createErr)
			continue
		}

		fmt.Fprintf(out, "  ✅ Created goal automation %s (%s)\n", built.Title, createdEntry.ID)
		created++
		convertedPlans = append(convertedPlans, plan)
		if hasReconciler {
			convertedReconcilers = append(convertedReconcilers, reconciler)
		}
	}

	// -------------------------------------------------------------------------
	// Step 3: Disable/annotate the successfully converted legacy entries.
	// -------------------------------------------------------------------------
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Step 3: Disable legacy goal entries")
	fmt.Fprintln(out)

	disabled := 0
	const migrationNote = "Migrated to goal automation by `brain migrate goals`."

	for _, plan := range convertedPlans {
		if c.Flags.DryRun {
			fmt.Fprintf(out, "  DRY RUN: Would archive legacy plan %s (%s)\n", plan.Title, plan.ID)
			disabled++
			continue
		}
		// Idempotency guard.
		if plan.Status == "archived" {
			fmt.Fprintf(out, "  ⏭  Already archived: %s (%s)\n", plan.Title, plan.ID)
			continue
		}
		updates := map[string]interface{}{
			"status": "archived",
			"append": migrationNote,
		}
		if _, updErr := client.UpdateEntry(ctx, plan.Path, updates); updErr != nil {
			fmt.Fprintf(out, "  ⚠️  Failed to archive legacy plan %s (%s): %v\n", plan.Title, plan.ID, updErr)
			continue
		}
		fmt.Fprintf(out, "  ✅ Archived legacy plan %s (%s)\n", plan.Title, plan.ID)
		disabled++
	}

	for _, reconciler := range convertedReconcilers {
		if c.Flags.DryRun {
			fmt.Fprintf(out, "  DRY RUN: Would cancel legacy reconciler %s (%s)\n", reconciler.Title, reconciler.ID)
			disabled++
			continue
		}
		// Idempotency guard.
		if reconciler.Status == "archived" || reconciler.Status == "cancelled" {
			fmt.Fprintf(out, "  ⏭  Already disabled: %s (%s)\n", reconciler.Title, reconciler.ID)
			continue
		}
		updates := map[string]interface{}{
			"status": "cancelled",
			"append": migrationNote,
		}
		if _, updErr := client.UpdateEntry(ctx, reconciler.Path, updates); updErr != nil {
			fmt.Fprintf(out, "  ⚠️  Failed to cancel legacy reconciler %s (%s): %v\n", reconciler.Title, reconciler.ID, updErr)
			continue
		}
		fmt.Fprintf(out, "  ✅ Cancelled legacy reconciler %s (%s)\n", reconciler.Title, reconciler.ID)
		disabled++
	}

	fmt.Fprintln(out)
	c.printGoalsSummary(out, created, skipped, disabled)
	return nil
}

// printGoalsSummary prints the goal migration summary.
func (c *MigrateCommand) printGoalsSummary(out io.Writer, created, skipped, disabled int) {
	if c.Flags.DryRun {
		fmt.Fprintln(out, "DRY RUN Summary:")
	} else {
		fmt.Fprintln(out, "Migration complete!")
	}
	fmt.Fprintf(out, "  Goal automations created:        %d\n", created)
	if skipped > 0 {
		fmt.Fprintf(out, "  Goals skipped (already migrated): %d (use --force to recreate)\n", skipped)
	} else {
		fmt.Fprintf(out, "  Goals skipped (already migrated): %d\n", skipped)
	}
	fmt.Fprintf(out, "  Legacy entries disabled:         %d\n", disabled)
}
