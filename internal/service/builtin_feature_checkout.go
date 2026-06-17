package service

import (
	"context"
	"fmt"

	"github.com/huynle/brain-api/internal/types"
)

const BuiltInFeatureCheckoutGeneratedBy = "brain:builtin-feature-checkout"

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
func EnsureBuiltInFeatureCheckoutAutomation(ctx context.Context, brain *BrainServiceImpl, cfg BuiltInFeatureCheckoutConfig) error {
	if !cfg.Enabled || brain == nil {
		return nil
	}

	existing, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Limit: 1000})
	if err != nil {
		return fmt.Errorf("list automations: %w", err)
	}
	for _, entry := range existing.Entries {
		if entry.GeneratedBy == BuiltInFeatureCheckoutGeneratedBy {
			return nil
		}
	}

	prompt := "Load the feature-checkout skill and process feature {{.FeatureID}} in project {{.ProjectID}}. Validate implementation coverage against dependency tasks' user_original_request intent. Create gap tasks if needed. If no gaps remain, open a Brain-native merge request into " + cfg.MergeTargetBranch + ". Start now."
	if cfg.MergeTargetBranch == "" {
		prompt = "Load the feature-checkout skill and process feature {{.FeatureID}} in project {{.ProjectID}}. Validate implementation coverage against dependency tasks' user_original_request intent. Create gap tasks if needed. If no gaps remain, open a Brain-native merge request into the configured merge target branch. Start now."
	}

	_, err = brain.Save(ctx, types.CreateEntryRequest{
		Type:        "automation",
		Title:       "Built-in feature checkout",
		Content:     "Creates a feature checkout task when a feature completes.",
		Status:      "active",
		Global:      serviceBoolPtr(true),
		Generated:   serviceBoolPtr(true),
		GeneratedBy: BuiltInFeatureCheckoutGeneratedBy,
		Trigger: &types.TriggerConfig{
			Type:    "event",
			Event:   types.EventFeatureCompleted,
			OncePer: "feature_id",
		},
		Action: &types.AutomationAction{
			Type:          "prompt",
			DirectPrompt:  prompt,
			Agent:         cfg.Agent,
			Model:         cfg.Model,
			Executor:      cfg.Executor,
			ExecutionMode: cfg.ExecutionMode,
			TargetWorkdir: cfg.TargetWorkdir,
		},
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

func serviceBoolPtr(v bool) *bool { return &v }
