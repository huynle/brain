package commands

import (
	"bufio"
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
	"github.com/mattn/go-isatty"
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
	Yes     bool   // --yes (confirm the dream stagger offer without a terminal)
}

// MigrateCommand implements the Command interface for migration operations.
type MigrateCommand struct {
	Subcommand string // "automations" (only supported subcommand for now)
	Config     *UnifiedConfig
	Flags      *MigrateFlags
	Out        io.Writer

	// apiClient is injectable for testing; nil means create from config.
	apiClient *runner.APIClient

	// In and StdinIsTerminal default to the process's stdin; tests replace
	// them to drive the stagger confirmation.
	In              io.Reader
	StdinIsTerminal func() bool
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

	// Step 3: Dream monitors become per-project bindings of the global parent.
	// Runs before the disabling step so a dream monitor is only ever disabled
	// once its binding exists (or already did).
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Step 3: Migrate dream monitors to project bindings")
	fmt.Fprintln(out)

	dream, err := c.migrateDreamMonitorsToBindings(ctx, out, client)
	if err != nil {
		return err
	}

	// Step 4: Offer the dream stagger to an installed copy that lacks it.
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Step 4: Offer dream stagger")
	fmt.Fprintln(out)
	c.offerDreamStagger(ctx, out, client, automationsDir)

	// Step 5: Find and disable existing monitor tasks
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Step 5: Disable existing monitor tasks")
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
		c.printSummary(out, createdCount, skippedCount, apiCreatedCount, apiSkippedCount, dream.disabled)
		return nil
	}

	disabledCount := dream.disabled
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
		if dream.handled[entry.ID] {
			fmt.Fprintf(out, "  ⏭  Handled by dream binding migration: %s (%s)\n", entry.Title, entry.ID)
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
		// Lifecycle bounds and the binding parent are automation fields;
		// dropping them here would silently widen the synced entry.
		StartsAt:  fm.StartsAt,
		ExpiresAt: fm.ExpiresAt,
		Timezone:  fm.Timezone,
		Extends:   fm.Extends,
		Trigger:   automationAssetTrigger(fm.Trigger),
		Action:    automationAssetAction(fm.Action),
		Retry:     automationAssetRetry(fm.Retry),
	}, nil
}

func automationAssetTrigger(t *frontmatter.TriggerConfig) *types.TriggerConfig {
	if t == nil {
		return nil
	}
	return &types.TriggerConfig{
		Type:                   t.Type,
		Event:                  t.Event,
		Events:                 t.Events,
		Schedule:               t.Schedule,
		Timezone:               t.Timezone,
		Every:                  t.Every,
		At:                     t.At,
		Stagger:                t.Stagger,
		CatchUp:                t.CatchUp,
		Calendar:               t.Calendar,
		SkipIfEvent:            automationAssetCalendarFilter(t.SkipIfEvent),
		OnlyIfEvent:            automationAssetCalendarFilter(t.OnlyIfEvent),
		Match:                  t.Match,
		Offset:                 t.Offset,
		Filter:                 t.Filter,
		OncePer:                t.OncePer,
		Webhook:                t.Webhook,
		IgnoreAutomationEvents: t.IgnoreAutomationEvents,
		Cooldown:               t.Cooldown,
		MaxConcurrent:          t.MaxConcurrent,
	}
}

func automationAssetCalendarFilter(f *frontmatter.CalendarEventFilter) *types.CalendarEventFilter {
	if f == nil {
		return nil
	}
	return &types.CalendarEventFilter{
		Calendar:    f.Calendar,
		Title:       f.Title,
		Description: f.Description,
		Location:    f.Location,
		AllDay:      f.AllDay,
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
		Executor:           a.Executor,
		TargetWorkdir:      a.TargetWorkdir,
		ExecutionMode:      a.ExecutionMode,
		SessionMode:        a.SessionMode,
		CompleteOnIdle:     a.CompleteOnIdle,
		Timeout:            a.Timeout,
		RequiresCapability: a.RequiresCapability,
		SetStatus:          a.SetStatus,
		PromptAppend:       a.PromptAppend,
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

// =============================================================================
// Migrate: Dream monitors → per-project bindings
// =============================================================================

// dreamTemplateID is the monitor template whose tasks become dream bindings.
const dreamTemplateID = "dream"

// dreamParentTitle is the title of the global automation that dream bindings extend.
const dreamParentTitle = "Dream Consolidation"

// dreamMigration reports what the dream step did. handled holds every monitor
// ID the step owns (migrated, planned, or kept because its binding failed), so
// the monitor-disabling step leaves those alone.
type dreamMigration struct {
	disabled int
	handled  map[string]bool
}

// dreamProjectOfMonitor returns the project of a project-scoped dream monitor
// (monitor:dream:project:<P>). Other scopes and templates return false.
func dreamProjectOfMonitor(entry types.BrainEntry) (string, bool) {
	for _, tag := range entry.Tags {
		parsed := service.ParseMonitorTag(tag)
		if parsed == nil || parsed.TemplateID != dreamTemplateID {
			continue
		}
		if parsed.Scope.Type != "project" || parsed.Scope.Project == "" {
			continue
		}
		return parsed.Scope.Project, true
	}
	return "", false
}

// bindingProjectOf returns the project a binding belongs to: its stored project,
// or the project segment of its path when the stored one is missing.
func bindingProjectOf(entry types.BrainEntry) string {
	if entry.ProjectID != "" {
		return entry.ProjectID
	}
	parts := strings.Split(entry.Path, "/")
	if len(parts) >= 2 && parts[0] == "projects" {
		return parts[1]
	}
	return ""
}

// dreamParentID finds the single global Dream Consolidation automation. None or
// several is an error: bindings must extend exactly one parent.
func (c *MigrateCommand) dreamParentID(ctx context.Context, client *runner.APIClient) (string, error) {
	resp, err := client.ListEntries(ctx, map[string]string{"type": "automation", "global": "true", "limit": "1000"})
	if err != nil {
		return "", fmt.Errorf("list global automations: %w", err)
	}
	var matches []types.BrainEntry
	for _, entry := range resp.Entries {
		if entry.Title == dreamParentTitle && entry.Extends == "" {
			matches = append(matches, entry)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no global %q automation found; run the deploy step (or `brain init`) so it exists, then re-run `brain migrate automations`", dreamParentTitle)
	case 1:
		return matches[0].ID, nil
	default:
		ids := make([]string, 0, len(matches))
		for _, m := range matches {
			ids = append(ids, m.ID)
		}
		return "", fmt.Errorf("found %d global %q automations (%s); remove the duplicates (--force installs can create them) before migrating", len(matches), dreamParentTitle, strings.Join(ids, ", "))
	}
}

// dreamBindingRequest is the binding a dream monitor becomes. It carries only
// the fields the monitor actually sets, so anything unset inherits the parent.
// trigger.type and action.type stay empty: a binding may not set them.
func dreamBindingRequest(parentID, project string, monitor types.BrainEntry) types.CreateEntryRequest {
	global := false
	req := types.CreateEntryRequest{
		Type:    "automation",
		Title:   dreamParentTitle + " (" + project + ")",
		Content: fmt.Sprintf("Per-project binding of the global %s automation, migrated from monitor task %s by `brain migrate automations`.", dreamParentTitle, monitor.ID),
		Tags:    []string{"automation", "dream"},
		Status:  "active",
		Project: project,
		Global:  &global,
		Extends: parentID,
		Agent:   monitor.Agent,
		Model:   monitor.Model,
	}
	req.Timezone = monitor.Timezone
	if monitor.Schedule != "" || monitor.Timezone != "" {
		req.Trigger = &types.TriggerConfig{Schedule: monitor.Schedule, Timezone: monitor.Timezone}
	}
	return req
}

// migrateDreamMonitorsToBindings converts each enabled project dream monitor
// into a binding of the global Dream Consolidation automation, then disables
// the monitor's schedule. A project that already has a binding gets no second
// one; its monitor is still disabled. A monitor is disabled only after its
// binding exists, so a failed create leaves that project's dream running.
func (c *MigrateCommand) migrateDreamMonitorsToBindings(ctx context.Context, out io.Writer, client *runner.APIClient) (dreamMigration, error) {
	res := dreamMigration{handled: make(map[string]bool)}

	resp, err := client.ListEntries(ctx, map[string]string{"type": "task", "tags": "monitor"})
	if err != nil {
		// The monitor-disabling step reports the outage.
		return res, nil
	}

	type target struct {
		monitor types.BrainEntry
		project string
	}
	var targets []target
	for _, entry := range resp.Entries {
		project, ok := dreamProjectOfMonitor(entry)
		if !ok {
			continue
		}
		if entry.ScheduleEnabled != nil && !*entry.ScheduleEnabled {
			continue
		}
		targets = append(targets, target{monitor: entry, project: project})
	}
	if len(targets) == 0 {
		fmt.Fprintln(out, "  No enabled dream monitor tasks to migrate.")
		return res, nil
	}

	dryRun := c.Flags != nil && c.Flags.DryRun

	parentID, err := c.dreamParentID(ctx, client)
	if err != nil {
		return res, err
	}

	for _, t := range targets {
		res.handled[t.monitor.ID] = true

		bindingID, err := c.existingBindingID(ctx, client, parentID, t.project)
		if err != nil {
			return res, err
		}

		if bindingID != "" {
			fmt.Fprintf(out, "  ⏭  Binding already exists for project %s (%s)\n", t.project, bindingID)
		} else {
			req := dreamBindingRequest(parentID, t.project, t.monitor)
			if dryRun {
				fmt.Fprintf(out, "  DRY RUN: Would create binding in project %s extending %s (schedule %q, timezone %q, agent %q, model %q)\n",
					t.project, parentID, t.monitor.Schedule, t.monitor.Timezone, t.monitor.Agent, t.monitor.Model)
				bindingID = "<new binding>"
			} else {
				created, err := client.CreateEntry(ctx, req)
				if err != nil {
					fmt.Fprintf(out, "  ⚠️  Failed to create binding for project %s: %v (monitor %s left enabled)\n", t.project, err, t.monitor.ID)
					continue
				}
				bindingID = created.ID
				fmt.Fprintf(out, "  ✅ Created binding for project %s (%s)\n", t.project, bindingID)
			}
		}

		note := fmt.Sprintf("Migrated to binding %s of %s by `brain migrate automations`. Schedule disabled; the binding runs this project's dream.", bindingID, dreamParentTitle)
		if dryRun {
			fmt.Fprintf(out, "  DRY RUN: Would disable monitor %s (%s), pointing at its binding\n", t.monitor.Title, t.monitor.ID)
			res.disabled++
			continue
		}
		updates := map[string]interface{}{
			"schedule_enabled": false,
			"append":           note,
		}
		if _, err := client.UpdateEntry(ctx, t.monitor.Path, updates); err != nil {
			fmt.Fprintf(out, "  ⚠️  Failed to disable monitor %s (%s): %v\n", t.monitor.Title, t.monitor.ID, err)
			continue
		}
		res.disabled++
		fmt.Fprintf(out, "  ✅ Disabled monitor %s (%s)\n", t.monitor.Title, t.monitor.ID)
	}

	return res, nil
}

// existingBindingID returns the ID of the binding that project already has for
// parentID, or "" when it has none.
func (c *MigrateCommand) existingBindingID(ctx context.Context, client *runner.APIClient, parentID, project string) (string, error) {
	resp, err := client.ListEntries(ctx, map[string]string{"type": "automation", "tags": "extends:" + parentID})
	if err != nil {
		return "", fmt.Errorf("list bindings of %s: %w", parentID, err)
	}
	for _, entry := range resp.Entries {
		if entry.Extends == parentID && bindingProjectOf(entry) == project {
			return entry.ID, nil
		}
	}
	return "", nil
}

// =============================================================================
// Migrate: Dream stagger offer
// =============================================================================

// dreamStagger is the stagger the shipped dream template carries. Each project's
// dream starts at a stable offset within it, so the projects do not all dream at
// 03:00 at once.
const dreamStagger = "2h"

// dreamAssetFile is the installed file name of the dream template.
const dreamAssetFile = "dream-consolidation.md"

// stdinTerminal reports whether confirmation questions can be asked.
func (c *MigrateCommand) stdinTerminal() bool {
	if c.StdinIsTerminal != nil {
		return c.StdinIsTerminal()
	}
	// A character-device check is not enough: /dev/null is one.
	return isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())
}

// confirmYesNo asks a y/N question on the command's input and reports a yes.
func (c *MigrateCommand) confirmYesNo(out io.Writer, question string) bool {
	fmt.Fprintf(out, "%s [y/N] ", question)
	in := c.In
	if in == nil {
		in = os.Stdin
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(out)
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

// offerDreamStagger adds stagger to an installed dream automation that lacks it,
// both in its file and in the live global entry. It asks y/N on a terminal,
// applies with --yes, and otherwise changes nothing. Refusing is not an error:
// the rest of the migration still runs.
func (c *MigrateCommand) offerDreamStagger(ctx context.Context, out io.Writer, client *runner.APIClient, automationsDir string) {
	path := filepath.Join(automationsDir, dreamAssetFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(out, "  Dream Consolidation is not installed as a file; nothing to stagger.")
		return
	}
	doc, err := frontmatter.Parse(string(raw))
	if err != nil || doc.Frontmatter.Trigger == nil {
		fmt.Fprintf(out, "  ⚠️  Skipped stagger: cannot read the trigger of %s\n", path)
		return
	}
	if doc.Frontmatter.Trigger.Stagger != "" {
		fmt.Fprintf(out, "  Dream Consolidation already has stagger %s; nothing to do.\n", doc.Frontmatter.Trigger.Stagger)
		return
	}
	updated, err := insertTriggerStagger(raw, dreamStagger)
	if err != nil {
		fmt.Fprintf(out, "  ⚠️  Skipped stagger for %s: %v\n", path, err)
		return
	}

	dryRun := c.Flags != nil && c.Flags.DryRun
	yes := c.Flags != nil && c.Flags.Yes
	if dryRun {
		fmt.Fprintf(out, "  DRY RUN: Would offer to add stagger: %s to %s\n", dreamStagger, path)
		return
	}
	if !yes {
		if !c.stdinTerminal() {
			fmt.Fprintf(out, "  Not applying stagger: stdin is not a terminal. Re-run with --yes to add stagger: %s to %s.\n", dreamStagger, path)
			return
		}
		question := fmt.Sprintf("Apply stagger: %s to %s and to the live Dream Consolidation automation?", dreamStagger, path)
		if !c.confirmYesNo(out, question) {
			fmt.Fprintln(out, "  Skipped stagger.")
			return
		}
	}

	if err := os.WriteFile(path, updated, 0644); err != nil {
		fmt.Fprintf(out, "  ⚠️  Failed to write %s: %v\n", path, err)
		return
	}
	fmt.Fprintf(out, "  ✅ Added stagger: %s to %s\n", dreamStagger, path)
	c.applyDreamStaggerToLive(ctx, out, client)
}

// applyDreamStaggerToLive sets stagger on the one global Dream Consolidation
// entry the scheduler runs. An ambiguous or unreachable API is reported, not
// guessed at; the file change takes effect after the next index or restart.
func (c *MigrateCommand) applyDreamStaggerToLive(ctx context.Context, out io.Writer, client *runner.APIClient) {
	resp, err := client.ListEntries(ctx, map[string]string{"type": "automation", "global": "true", "limit": "1000"})
	if err != nil {
		fmt.Fprintf(out, "  ⚠️  Live automation not updated (API unavailable): %v\n", err)
		return
	}
	var live []types.BrainEntry
	for _, entry := range resp.Entries {
		if entry.Title == dreamParentTitle && entry.Extends == "" {
			live = append(live, entry)
		}
	}
	if len(live) != 1 {
		fmt.Fprintf(out, "  ⚠️  Live automation not updated: found %d global %q automations, want 1\n", len(live), dreamParentTitle)
		return
	}
	entry := live[0]
	trigger := types.TriggerConfig{}
	if entry.Trigger != nil {
		trigger = *entry.Trigger
	}
	if trigger.Stagger != "" {
		fmt.Fprintf(out, "  Live automation %s already has stagger %s.\n", entry.ID, trigger.Stagger)
		return
	}
	trigger.Stagger = dreamStagger
	if _, err := client.UpdateEntry(ctx, entry.Path, map[string]interface{}{"trigger": trigger}); err != nil {
		fmt.Fprintf(out, "  ⚠️  Failed to update live automation %s: %v\n", entry.ID, err)
		return
	}
	fmt.Fprintf(out, "  ✅ Updated live automation %s (stagger %s)\n", entry.ID, dreamStagger)
}

// insertTriggerStagger returns the installed automation file with a stagger line
// added as the first key of its block-style trigger. It re-parses the result and
// fails rather than return a file whose stagger it cannot read back.
func insertTriggerStagger(raw []byte, stagger string) ([]byte, error) {
	lines := strings.SplitAfter(string(raw), "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r\n") != "---" {
		return nil, fmt.Errorf("no frontmatter block")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r\n") == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, fmt.Errorf("unterminated frontmatter block")
	}
	trig := -1
	for i := 1; i < end; i++ {
		if strings.TrimRight(lines[i], " \t\r\n") == "trigger:" {
			trig = i
			break
		}
	}
	if trig < 0 {
		return nil, fmt.Errorf("no block-style trigger: key")
	}
	indent := ""
	for i := trig + 1; i < end; i++ {
		line := strings.TrimRight(lines[i], "\r\n")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, " ") {
			return nil, fmt.Errorf("trigger has no indented keys")
		}
		indent = line[:len(line)-len(strings.TrimLeft(line, " "))]
		break
	}
	if indent == "" {
		return nil, fmt.Errorf("trigger has no keys")
	}

	result := make([]string, 0, len(lines)+1)
	result = append(result, lines[:trig+1]...)
	result = append(result, indent+"stagger: "+stagger+"\n")
	result = append(result, lines[trig+1:]...)
	text := strings.Join(result, "")

	doc, err := frontmatter.Parse(text)
	if err != nil {
		return nil, fmt.Errorf("re-parse with stagger: %w", err)
	}
	if doc.Frontmatter.Trigger == nil || doc.Frontmatter.Trigger.Stagger != stagger {
		return nil, fmt.Errorf("stagger did not round-trip")
	}
	return []byte(text), nil
}
