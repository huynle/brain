package service

import (
	"context"
	"fmt"

	"github.com/huynle/brain-api/internal/types"
)

const BuiltInFeatureCheckoutGeneratedBy = "brain:builtin-feature-checkout"

// builtInCheckoutFilter is the trigger filter shared by both built-in
// feature-checkout automations, parameterised by checkout mode.
//
// The `project: "*"` entry is load-bearing, not decoration. Both built-ins
// are registered ONCE, globally, at server startup (see apiserver.Start) —
// there is no per-project materialization anywhere in the codebase. And a
// global automation is deliberately inert for project-scoped events unless
// it opts in explicitly (see globalAutomationMatchesProjectEvent): that
// default is what keeps one project's automations out of another's work.
//
// Without the wildcard these two automations matched nothing at all, so no
// feature ever got a checkout task and nothing was ever merged to the
// target branch. The wildcard is how a system-level automation says "yes,
// I really am meant to serve every project" — in its own trigger, where a
// user reading the entry can see it.
func builtInCheckoutFilter(checkoutMode string) map[string]string {
	return map[string]string{
		"checkout_mode": checkoutMode,
		"project":       "*",
	}
}

// BuiltInFeatureCheckoutConfig controls the built-in automation that creates
// feature checkout tasks after feature.completed events.
type BuiltInFeatureCheckoutConfig struct {
	Enabled            bool
	Agent              string
	Model              string
	Executor           string
	ExecutionMode      string
	TargetWorkdir      string
	MergeTargetBranch  string
	MergePolicy        string
	MergeStrategy      string
	RemoteBranchPolicy string
	OpenPRBeforeMerge  *bool
}

// EnsureBuiltInFeatureCheckoutAutomation creates the built-in feature checkout
// automation once. Existing user-created automations are not modified.
//
// Phase 3.2: the automation now carries a trigger filter of
// checkout_mode:"ai" so that only feature.completed events whose folded
// checkout mode is "ai" match it (the parallel simple/script automation
// carries checkout_mode:"simple"). Older built-in entries created before
// Phase 3 shipped had no filter; this function migrates them in place by
// UPDATE (only when the GeneratedBy marker matches our built-in — never
// touches user-created automations).
func EnsureBuiltInFeatureCheckoutAutomation(ctx context.Context, brain *BrainServiceImpl, cfg BuiltInFeatureCheckoutConfig) error {
	if !cfg.Enabled || brain == nil {
		return nil
	}

	desiredTrigger := &types.TriggerConfig{
		Type:    "event",
		Event:   types.EventFeatureCompleted,
		OncePer: "feature_id",
		Filter:  builtInCheckoutFilter("ai"),
	}

	prompt := "Load the feature-checkout skill and process feature {{.FeatureID}} in project {{.ProjectID}}. Validate implementation coverage against dependency tasks' user_original_request intent. Create gap tasks if needed. If no gaps remain, open a Brain-native merge request into " + cfg.MergeTargetBranch + ". Start now."
	if cfg.MergeTargetBranch == "" {
		prompt = "Load the feature-checkout skill and process feature {{.FeatureID}} in project {{.ProjectID}}. Validate implementation coverage against dependency tasks' user_original_request intent. Create gap tasks if needed. If no gaps remain, open a Brain-native merge request into the configured merge target branch. Start now."
	}
	desiredAction := &types.AutomationAction{
		Type:          "prompt",
		DirectPrompt:  prompt,
		Agent:         cfg.Agent,
		Model:         cfg.Model,
		Executor:      cfg.Executor,
		ExecutionMode: cfg.ExecutionMode,
		TargetWorkdir: cfg.TargetWorkdir,
	}

	existing, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Limit: 1000})
	if err != nil {
		return fmt.Errorf("list automations: %w", err)
	}
	for _, entry := range existing.Entries {
		if entry.GeneratedBy != BuiltInFeatureCheckoutGeneratedBy {
			continue
		}
		// Migrate the stored entry toward the shape this build + config
		// want. The built-in is OWNED BY CONFIG: its prompt, agent/model,
		// and merge fields are all derived from task_defaults, so an entry
		// written by an older build (or older config) must be reconciled
		// here — the same trap that kept the simple script pushless kept
		// this entry's prompt pointing at a stale merge target. Users who
		// want different behavior change config, not the entry; manual
		// edits to this generated entry do not survive a restart.
		update := types.UpdateEntryRequest{}
		changed := false
		if triggerNeedsCheckoutMigration(entry.Trigger, "ai") {
			update.Trigger = desiredTrigger
			changed = true
		}
		if entry.Action == nil ||
			entry.Action.DirectPrompt != desiredAction.DirectPrompt ||
			entry.Action.Agent != desiredAction.Agent ||
			entry.Action.Model != desiredAction.Model ||
			entry.Action.Executor != desiredAction.Executor ||
			entry.Action.ExecutionMode != desiredAction.ExecutionMode ||
			entry.Action.TargetWorkdir != desiredAction.TargetWorkdir {
			update.Action = desiredAction
			changed = true
		}
		if entry.MergeTargetBranch != cfg.MergeTargetBranch {
			update.MergeTargetBranch = &cfg.MergeTargetBranch
			changed = true
		}
		if entry.MergePolicy != cfg.MergePolicy {
			update.MergePolicy = &cfg.MergePolicy
			changed = true
		}
		if entry.MergeStrategy != cfg.MergeStrategy {
			update.MergeStrategy = &cfg.MergeStrategy
			changed = true
		}
		if entry.RemoteBranchPolicy != cfg.RemoteBranchPolicy {
			update.RemoteBranchPolicy = &cfg.RemoteBranchPolicy
			changed = true
		}
		if changed {
			if _, err := brain.Update(ctx, entry.Path, update); err != nil {
				return fmt.Errorf("migrate built-in feature checkout automation: %w", err)
			}
		}
		return nil
	}

	_, err = brain.Save(ctx, types.CreateEntryRequest{
		Type:               "automation",
		Title:              "Built-in feature checkout",
		Content:            "Creates a feature checkout task when a feature completes.",
		Status:             "active",
		Global:             serviceBoolPtr(true),
		Generated:          serviceBoolPtr(true),
		GeneratedBy:        BuiltInFeatureCheckoutGeneratedBy,
		Trigger:            desiredTrigger,
		Action:             desiredAction,
		MergeTargetBranch:  cfg.MergeTargetBranch,
		MergePolicy:        cfg.MergePolicy,
		MergeStrategy:      cfg.MergeStrategy,
		RemoteBranchPolicy: cfg.RemoteBranchPolicy,
		OpenPRBeforeMerge:  cfg.OpenPRBeforeMerge,
	})
	if err != nil {
		return fmt.Errorf("create built-in feature checkout automation: %w", err)
	}
	return nil
}

// triggerNeedsCheckoutMigration reports whether an existing built-in
// automation trigger differs from the shape we now require: the right
// checkout_mode AND the project wildcard that lets a global automation
// see project events at all.
//
// A nil trigger, a nil filter, a wrong mode, or a missing wildcard all
// count as "needs migration".
func triggerNeedsCheckoutMigration(trigger *types.TriggerConfig, wantMode string) bool {
	if trigger == nil || trigger.Filter == nil {
		return true
	}
	if trigger.Filter["checkout_mode"] != wantMode {
		return true
	}
	return trigger.Filter["project"] != "*"
}

func serviceBoolPtr(v bool) *bool { return &v }
