package service

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/types"
)

// Effective view of one automation for one project (GET /automations/{id}/effective).
//
// The view reuses the binding rules from automation_binding.go and the target
// rules the scheduler uses, so it cannot drift from what would actually run:
// the overlay comes from boundTarget/effectiveAutomation, the governing binding
// from pickBindingWinners, and project selection from filteredTargetProjects.

// effectiveFieldKeys lists every field a binding can override, under the names
// the view reports in Fields.
var effectiveFieldKeys = []string{
	"trigger.schedule", "trigger.every", "trigger.at", "trigger.stagger",
	"trigger.catch_up", "trigger.calendar", "trigger.skip_if_event", "trigger.only_if_event",
	"timezone", "starts_at", "expires_at", "max_runs",
	"action.agent", "action.model", "action.executor", "action.target_workdir",
	"action.execution_mode", "action.timeout", "action.prompt_append",
}

// inheritedFields marks every overridable field as inherited from the parent.
func inheritedFields() map[string]string {
	fields := make(map[string]string, len(effectiveFieldKeys))
	for _, key := range effectiveFieldKeys {
		fields[key] = types.EffectiveFieldInherited
	}
	return fields
}

// overlayFields reports which fields binding overrides. It applies the same
// presence rules effectiveAutomation does, so a field is overridden exactly
// when the overlay replaces it.
func overlayFields(binding types.BrainEntry) map[string]string {
	fields := inheritedFields()
	set := func(key string, overridden bool) {
		if overridden {
			fields[key] = types.EffectiveFieldOverridden
		}
	}

	if bt := binding.Trigger; bt != nil {
		timing := bt.Schedule != "" || bt.Every != ""
		set("trigger.schedule", timing)
		set("trigger.every", timing)
		set("trigger.at", timing)
		set("trigger.stagger", bt.Stagger != "")
		set("trigger.catch_up", bt.CatchUp != "")
		set("trigger.calendar", bt.Calendar != "")
		set("trigger.skip_if_event", bt.SkipIfEvent != nil)
		set("trigger.only_if_event", bt.OnlyIfEvent != nil)
	}
	bindAction := types.AutomationAction{}
	if binding.Action != nil {
		bindAction = *binding.Action
	}
	set("timezone", firstNonEmpty(bindingTriggerTimezone(binding), binding.Timezone) != "")
	set("starts_at", binding.StartsAt != "")
	set("expires_at", binding.ExpiresAt != "")
	set("max_runs", binding.MaxRuns != nil && *binding.MaxRuns != 0)
	set("action.agent", firstNonEmpty(binding.Agent, bindAction.Agent) != "")
	set("action.model", firstNonEmpty(binding.Model, bindAction.Model) != "")
	set("action.executor", firstNonEmpty(binding.Executor, bindAction.Executor) != "")
	set("action.target_workdir", firstNonEmpty(binding.TargetWorkdir, bindAction.TargetWorkdir) != "")
	set("action.execution_mode", firstNonEmpty(binding.ExecutionMode, bindAction.ExecutionMode) != "")
	set("action.timeout", bindAction.Timeout != "")
	set("action.prompt_append", bindAction.PromptAppend != "")
	return fields
}

// effectiveConfigOf returns the trigger, action and lifecycle of entry, copied
// so the view shares no state with the entry it came from.
func effectiveConfigOf(entry types.BrainEntry) *types.AutomationEffectiveConfig {
	cfg := &types.AutomationEffectiveConfig{
		ID:        entry.ID,
		Trigger:   copyTrigger(entry.Trigger),
		Action:    copyAction(entry.Action),
		Timezone:  entry.Timezone,
		StartsAt:  entry.StartsAt,
		ExpiresAt: entry.ExpiresAt,
	}
	if entry.MaxRuns != nil {
		maxRuns := *entry.MaxRuns
		cfg.MaxRuns = &maxRuns
	}
	return cfg
}

// EffectiveAutomation reports the config project runs automation pathOrID under,
// and whether the scheduler fires it for that project.
//
// pathOrID may name a global automation, a project-owned one, or a binding. The
// project must be a valid project ID, and it must be the owner of a project-owned
// automation or binding. Errors wrap api.ErrNotFound for an unknown or
// non-automation entry and api.ErrInvalidInput for a bad or mismatched project.
func (s *AutomationService) EffectiveAutomation(ctx context.Context, pathOrID, project string) (*types.AutomationEffective, error) {
	if err := validateProjectID(project); err != nil {
		return nil, fmt.Errorf("%w: %v", api.ErrInvalidInput, err)
	}
	entry, err := s.brain.Recall(ctx, pathOrID)
	if err != nil {
		return nil, err
	}
	if entry.Type != "automation" {
		return nil, api.ErrNotFound
	}
	switch {
	case entry.Extends != "":
		return s.effectiveOfBinding(ctx, *entry, project)
	case entry.ProjectID != "":
		return effectiveOfOwned(*entry, project)
	default:
		return s.effectiveOfParent(ctx, *entry, project)
	}
}

// effectiveOfOwned is a project-owned automation. It has no parent and no
// bindings, so the view is its own config, and it runs only for its owner.
func effectiveOfOwned(entry types.BrainEntry, project string) (*types.AutomationEffective, error) {
	if project != entry.ProjectID {
		return nil, fmt.Errorf("%w: automation %s belongs to project %q, not %q",
			api.ErrInvalidInput, entry.ID, entry.ProjectID, project)
	}
	return &types.AutomationEffective{
		ID:         entry.ID,
		Project:    project,
		Automation: effectiveConfigOf(entry),
		Fields:     map[string]string{},
		Targeted:   entry.Status == "active",
	}, nil
}

// effectiveOfBinding reports a binding through its parent, for the binding's
// own project. A binding whose parent is gone, or no longer a global automation,
// is returned broken, with no config.
func (s *AutomationService) effectiveOfBinding(ctx context.Context, binding types.BrainEntry, project string) (*types.AutomationEffective, error) {
	owner := bindingProject(binding)
	if project != owner {
		return nil, fmt.Errorf("%w: binding %s belongs to project %q, not %q",
			api.ErrInvalidInput, binding.ID, owner, project)
	}
	parent, err := s.brain.Recall(ctx, binding.Extends)
	switch {
	case errors.Is(err, api.ErrNotFound):
		return brokenBindingView(binding, project, types.EffectiveBrokenParentMissing), nil
	case err != nil:
		return nil, err
	case parent.Type != "automation" || parent.Extends != "" || parent.ProjectID != "":
		return brokenBindingView(binding, project, types.EffectiveBrokenParentNotAutomation), nil
	}
	return s.effectiveOfParent(ctx, *parent, project)
}

// brokenBindingView is the view for a binding that cannot be resolved to a parent.
func brokenBindingView(binding types.BrainEntry, project, reason string) *types.AutomationEffective {
	return &types.AutomationEffective{
		ID:            binding.Extends,
		Project:       project,
		Fields:        map[string]string{},
		BindingID:     binding.ID,
		BindingStatus: binding.Status,
		Broken:        true,
		BrokenReason:  reason,
	}
}

// effectiveOfParent reports a global automation for one project. With an
// active binding for the project the overlaid config applies. With no binding
// the parent applies, and the scheduler's project selection decides targeting.
func (s *AutomationService) effectiveOfParent(ctx context.Context, parent types.BrainEntry, project string) (*types.AutomationEffective, error) {
	bindings, err := s.bindingsOfParentIfSupported(ctx, parent.ID)
	if err != nil {
		return nil, err
	}
	winners, losers := pickBindingWinners(bindings)

	binding, bound := winners[project]
	if !bound {
		targeted, err := s.unboundTargeted(ctx, parent, project)
		if err != nil {
			return nil, err
		}
		return &types.AutomationEffective{
			ID:         parent.ID,
			Project:    project,
			Automation: effectiveConfigOf(parent),
			Fields:     inheritedFields(),
			Targeted:   targeted,
		}, nil
	}

	view := &types.AutomationEffective{
		ID:            parent.ID,
		Project:       project,
		Automation:    effectiveConfigOf(boundTarget(parent, binding).entry),
		Fields:        overlayFields(binding),
		BindingID:     binding.ID,
		BindingStatus: binding.Status,
		Targeted:      parent.Status == "active" && bindingIsActive(binding),
	}
	for _, loser := range losers {
		if bindingProject(loser) == project {
			view.Broken = true
			view.BrokenReason = types.EffectiveBrokenDuplicateBinding
		}
	}
	return view, nil
}

// bindingsOfParentIfSupported returns every binding of parentID. Bindings are
// single-tenant only, so a tenant service has none.
func (s *AutomationService) bindingsOfParentIfSupported(ctx context.Context, parentID string) ([]types.BrainEntry, error) {
	if !s.brain.bindingsSupported() {
		return nil, nil
	}
	return s.brain.bindingsOfParent(ctx, parentID)
}

// unboundTargeted reports whether the scheduler fires a global automation with
// no binding for project. An inactive parent never fires. A scheduled cron
// automation fires for the projects its filter selects. Any other trigger
// matches the project through its filter, and no filter selects every project.
func (s *AutomationService) unboundTargeted(ctx context.Context, parent types.BrainEntry, project string) (bool, error) {
	if parent.Status != "active" {
		return false, nil
	}
	if isScheduledCronAutomation(parent) {
		projects, err := s.filteredTargetProjects(ctx, parent)
		if err != nil {
			return false, err
		}
		return slices.Contains(projects, project), nil
	}
	expr, filtered := automationProjectFilter(parent)
	return !filtered || types.MatchFilterValue(project, expr), nil
}
