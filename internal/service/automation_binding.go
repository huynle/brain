package service

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/types"
)

// Per-project bindings.
//
// A binding is a project-owned automation with extends: <global automation id>.
// It customizes or opts out of its parent for one project without copying the
// parent. This file holds the overlay that produces a binding's effective
// config, the tag and save-time rules that keep bindings discoverable, and the
// target selection the scheduler, the event path and manual runs share. The
// evaluation itself stays with the scheduler (automation_schedule.go) and the
// event path (automation_service.go).

// bindingTagPrefix namespaces the tag that marks a binding's parent. Bindings
// are found by this tag, so Save and Update keep it in sync with extends.
const bindingTagPrefix = "extends:"

// bindingTag returns the tag that marks an automation as a binding of parentID.
func bindingTag(parentID string) string {
	return bindingTagPrefix + parentID
}

// syncExtendsTag returns tags with every extends: tag removed and, when
// parentID is set, exactly one extends:<parentID> tag appended. A stale tag
// never survives an update that changed or cleared extends.
func syncExtendsTag(tags []string, parentID string) []string {
	if parentID == "" && !hasExtendsTag(tags) {
		// Nothing to add or remove: keep the caller's slice, including nil,
		// so entries that never had a binding serialize exactly as before.
		return tags
	}
	out := make([]string, 0, len(tags)+1)
	for _, tag := range tags {
		if strings.HasPrefix(tag, bindingTagPrefix) {
			continue
		}
		out = append(out, tag)
	}
	if parentID != "" {
		out = append(out, bindingTag(parentID))
	}
	return out
}

// bindingIsActive reports whether a binding opts its project in, or keeps it
// in. Only status active counts: inactive, archived, completed and expired
// bindings all opt the project out.
func bindingIsActive(binding types.BrainEntry) bool {
	return binding.Status == "active"
}

// effectiveAutomation returns the config one project runs under: parent with
// binding's overrides applied. Each class of field follows its own rule:
//
//   - Timing: a binding schedule or every replaces the parent's schedule,
//     every and at as one unit. timezone, stagger, catch_up, calendar,
//     skip_if_event and only_if_event override individually.
//   - Execution: agent, model, executor, target_workdir and execution_mode
//     are resolved as the scheduler resolves them, firstNonEmpty over the
//     top-level and action fields, binding first. Both top-level and action
//     copies are set so every reader agrees. timeout is action-only.
//   - Lifecycle: starts_at and expires_at override when non-empty. max_runs
//     overrides when non-zero, so -1 is explicit unlimited and 0 inherits.
//   - Prompt: prompt_append is appended to the parent's prompt after a blank
//     line. A binding never supplies direct_prompt, trigger.type, action.type
//     or a project filter.
//
// The effective ID stays the parent's, so history and dedup stay keyed to the
// parent. ProjectID is the binding's project and Binding names the binding.
// Modified is the later of the two writes, so the slot floor sees both. The
// result shares no mutable state with parent, so changing it leaves the parent
// intact.
func effectiveAutomation(parent, binding types.BrainEntry) types.BrainEntry {
	eff := parent
	eff.Trigger = copyTrigger(parent.Trigger)
	eff.Action = copyAction(parent.Action)
	if parent.MaxRuns != nil {
		m := *parent.MaxRuns
		eff.MaxRuns = &m
	}
	if eff.Trigger == nil {
		eff.Trigger = &types.TriggerConfig{}
	}
	if eff.Action == nil {
		eff.Action = &types.AutomationAction{}
	}
	eff.ProjectID = bindingProject(binding)
	eff.Binding = binding.ID
	eff.Extends = ""

	bindAction := types.AutomationAction{}
	if binding.Action != nil {
		bindAction = *binding.Action
	}
	parentAction := types.AutomationAction{}
	if parent.Action != nil {
		parentAction = *parent.Action
	}

	if bt := binding.Trigger; bt != nil {
		if bt.Schedule != "" || bt.Every != "" {
			eff.Trigger.Schedule = bt.Schedule
			eff.Trigger.Every = bt.Every
			eff.Trigger.At = bt.At
			// An every anchor is the instant its schedule was written, so
			// the binding's created time anchors a binding's schedule.
			if binding.Created != "" {
				eff.Created = binding.Created
			}
		}
		if bt.Stagger != "" {
			eff.Trigger.Stagger = bt.Stagger
		}
		if bt.CatchUp != "" {
			eff.Trigger.CatchUp = bt.CatchUp
		}
		if bt.Calendar != "" {
			eff.Trigger.Calendar = bt.Calendar
		}
		if bt.SkipIfEvent != nil {
			eff.Trigger.SkipIfEvent = bt.SkipIfEvent
		}
		if bt.OnlyIfEvent != nil {
			eff.Trigger.OnlyIfEvent = bt.OnlyIfEvent
		}
	}
	if tz := firstNonEmpty(bindingTriggerTimezone(binding), binding.Timezone); tz != "" {
		eff.Trigger.Timezone = tz
		eff.Timezone = tz
	}

	if binding.StartsAt != "" {
		eff.StartsAt = binding.StartsAt
	}
	if binding.ExpiresAt != "" {
		eff.ExpiresAt = binding.ExpiresAt
	}
	if binding.MaxRuns != nil && *binding.MaxRuns != 0 {
		m := *binding.MaxRuns
		eff.MaxRuns = &m
	}

	agent := firstNonEmpty(binding.Agent, bindAction.Agent, parent.Agent, parentAction.Agent)
	eff.Agent, eff.Action.Agent = agent, agent
	model := firstNonEmpty(binding.Model, bindAction.Model, parent.Model, parentAction.Model)
	eff.Model, eff.Action.Model = model, model
	executor := firstNonEmpty(binding.Executor, bindAction.Executor, parent.Executor, parentAction.Executor)
	eff.Executor, eff.Action.Executor = executor, executor
	workdir := firstNonEmpty(binding.TargetWorkdir, bindAction.TargetWorkdir, parent.TargetWorkdir, parentAction.TargetWorkdir)
	eff.TargetWorkdir, eff.Action.TargetWorkdir = workdir, workdir
	mode := firstNonEmpty(binding.ExecutionMode, bindAction.ExecutionMode, parent.ExecutionMode, parentAction.ExecutionMode)
	eff.ExecutionMode, eff.Action.ExecutionMode = mode, mode
	eff.Action.Timeout = firstNonEmpty(bindAction.Timeout, parentAction.Timeout)

	if bindAction.PromptAppend != "" {
		base := firstNonEmpty(eff.Action.DirectPrompt, eff.DirectPrompt)
		if base != "" {
			base += "\n\n"
		}
		prompt := base + bindAction.PromptAppend
		eff.Action.DirectPrompt = prompt
		eff.DirectPrompt = prompt
	}

	if later, ok := laterModified(parent.Modified, binding.Modified); ok {
		eff.Modified = later
	}
	return eff
}

// bindingTriggerTimezone returns the timezone a binding sets on its trigger.
func bindingTriggerTimezone(binding types.BrainEntry) string {
	if binding.Trigger == nil {
		return ""
	}
	return binding.Trigger.Timezone
}

// laterModified returns whichever RFC 3339 instant is later, as the original
// string. ok is false when neither parses to a later value than the parent.
func laterModified(parent, binding string) (string, bool) {
	if binding == "" {
		return "", false
	}
	b, err := time.Parse(time.RFC3339, binding)
	if err != nil {
		return "", false
	}
	p, err := time.Parse(time.RFC3339, parent)
	if err != nil || b.After(p) {
		return binding, true
	}
	return "", false
}

// copyTrigger returns a copy of tc that shares no mutable state with it.
func copyTrigger(tc *types.TriggerConfig) *types.TriggerConfig {
	if tc == nil {
		return nil
	}
	c := *tc
	if tc.Filter != nil {
		c.Filter = make(map[string]string, len(tc.Filter))
		for k, v := range tc.Filter {
			c.Filter[k] = v
		}
	}
	return &c
}

// copyAction returns a copy of action that shares no mutable state with it.
func copyAction(action *types.AutomationAction) *types.AutomationAction {
	if action == nil {
		return nil
	}
	c := *action
	return &c
}

// hasExtendsTag reports whether any tag is an extends: tag.
func hasExtendsTag(tags []string) bool {
	for _, tag := range tags {
		if strings.HasPrefix(tag, bindingTagPrefix) {
			return true
		}
	}
	return false
}

// bindingsSupported reports whether this service serves the single local
// tenant. Bindings are single-mode only: a project writer could override the
// agent, executor and workdir of a global prompt, and no tenant authorization
// rule yet says which writers may do that. A service without a storage handle
// fails closed.
func (s *BrainServiceImpl) bindingsSupported() bool {
	return s != nil && s.storage != nil && s.storage.TenantID() == tenant.Local
}

// validateBindingWrite checks a binding before it is written. The single
// tenant gate comes first, then the placement rules: a binding is saved with
// its own project, never as a global entry. Last, a project may hold only one
// binding per parent. selfID is the entry's own ID ("" on create), so an
// update does not collide with itself.
func (s *BrainServiceImpl) validateBindingWrite(ctx context.Context, selfID, project string, global bool, parentID string) error {
	if !s.bindingsSupported() {
		return invalidAutomationField("extends", "bindings are available in single-tenant mode only")
	}
	if global {
		return invalidAutomationField("extends", "a global automation cannot be a binding: a binding belongs to one project")
	}
	if project == "" {
		// Save files an unprojected entry under "default", so that is the
		// project the binding is keyed to.
		project = "default"
	}
	siblings, err := s.bindingsOfParent(ctx, parentID)
	if err != nil {
		return err
	}
	for _, sibling := range siblings {
		if sibling.ID != selfID && bindingProject(sibling) == project {
			return invalidAutomationField("extends", fmt.Sprintf("project %q already has a binding of %q", project, parentID))
		}
	}
	return nil
}

// bindingsOfParent returns every binding of parentID, whatever its status.
// Bindings are found by their extends tag. Status is deliberately not
// filtered: an inactive binding is an opt-out, and the scheduler must see it.
func (s *BrainServiceImpl) bindingsOfParent(ctx context.Context, parentID string) ([]types.BrainEntry, error) {
	const pageSize = 200
	var out []types.BrainEntry
	for offset := 0; ; offset += pageSize {
		resp, err := s.List(ctx, types.ListEntriesRequest{
			Type:   "automation",
			Tags:   bindingTag(parentID),
			Limit:  pageSize,
			Offset: offset,
		})
		if err != nil {
			return nil, fmt.Errorf("list bindings of %s: %w", parentID, err)
		}
		if resp == nil {
			return out, nil
		}
		out = append(out, resp.Entries...)
		if len(resp.Entries) < pageSize {
			return out, nil
		}
	}
}

// bindingProject is the project a binding belongs to. The stored project is
// preferred, and the entry's path is the fallback.
func bindingProject(binding types.BrainEntry) string {
	if binding.ProjectID != "" {
		return binding.ProjectID
	}
	return extractProjectFromPath(binding.Path)
}

// Target selection.
//
// A global automation fires for the projects its filter selects, plus each
// project with an active binding (opt in), minus each project whose binding is
// not active (opt out). A project-owned automation fires only for its project,
// and its bindings are never consulted.

// Target states name what a project's schedule was built from. A change of
// state (a binding added, edited, opted out or removed) moves the project's
// handled cursor to now, so the change never fires a slot that predates it.
const (
	targetStateParent = "parent"
	targetStateAbsent = "-"
)

// automationTarget is one project one cron automation fires for on a tick.
// entry is the config that project runs under: the parent's own, or the
// parent with its binding applied.
type automationTarget struct {
	project string
	entry   types.BrainEntry
	state   string
}

// boundTarget is the target a binding produces for its project.
func boundTarget(parent, binding types.BrainEntry) automationTarget {
	project := bindingProject(binding)
	eff := effectiveAutomation(parent, binding)
	eff.ProjectID = project
	return automationTarget{
		project: project,
		entry:   eff,
		state:   "binding:" + binding.ID + "@" + binding.Modified,
	}
}

// resolveScheduledTargets lists the projects one cron automation fires for,
// each with the config it runs under. Bindings are found by their extends tag
// and read in every status, so an inactive binding opts its project out.
func (s *AutomationService) resolveScheduledTargets(ctx context.Context, automation types.BrainEntry) ([]automationTarget, error) {
	if automation.ProjectID != "" {
		return []automationTarget{{project: automation.ProjectID, entry: automation, state: targetStateParent}}, nil
	}
	base, err := s.filteredTargetProjects(ctx, automation)
	if err != nil {
		return nil, err
	}
	winners, err := s.bindingWinnersOf(ctx, automation.ID)
	if err != nil {
		return nil, fmt.Errorf("list bindings of automation %s: %w", automation.ID, err)
	}

	targets := make([]automationTarget, 0, len(base)+len(winners))
	seen := make(map[string]struct{}, len(base))
	for _, project := range base {
		seen[project] = struct{}{}
		binding, bound := winners[project]
		if !bound {
			targets = append(targets, automationTarget{project: project, entry: automation, state: targetStateParent})
			continue
		}
		if !bindingIsActive(binding) {
			continue // opted out
		}
		targets = append(targets, boundTarget(automation, binding))
	}

	optIns := make([]string, 0, len(winners))
	for project, binding := range winners {
		if _, inFilter := seen[project]; inFilter || !bindingIsActive(binding) {
			continue
		}
		optIns = append(optIns, project)
	}
	sort.Strings(optIns)
	for _, project := range optIns {
		targets = append(targets, boundTarget(automation, winners[project]))
	}
	return targets, nil
}

// scheduledTargetProjects is the project list resolveScheduledTargets yields.
// The timeline and the manual-run fan-out read it through the same rules the
// scheduler uses.
func (s *AutomationService) scheduledTargetProjects(ctx context.Context, automation types.BrainEntry) ([]string, error) {
	targets, err := s.resolveScheduledTargets(ctx, automation)
	if err != nil {
		return nil, err
	}
	projects := make([]string, 0, len(targets))
	for _, target := range targets {
		projects = append(projects, target.project)
	}
	return projects, nil
}

// bindingWinnersOf returns the one binding that governs each project of a
// parent. It is empty when bindings are not served here (tenant mode).
func (s *AutomationService) bindingWinnersOf(ctx context.Context, parentID string) (map[string]types.BrainEntry, error) {
	if s == nil || s.brain == nil || !s.brain.bindingsSupported() {
		return map[string]types.BrainEntry{}, nil
	}
	bindings, err := s.brain.bindingsOfParent(ctx, parentID)
	if err != nil {
		return nil, err
	}
	winners, losers := pickBindingWinners(bindings)
	s.warnBindingLosers(losers)
	return winners, nil
}

// pickBindingWinners chooses, for each project, the one binding that governs
// it. Two bindings for one (parent, project) can exist when concurrent saves
// race past the save-time check. The oldest wins (created, then ID), and the
// others are returned as losers so they can be reported.
func pickBindingWinners(bindings []types.BrainEntry) (map[string]types.BrainEntry, []types.BrainEntry) {
	byProject := make(map[string][]types.BrainEntry)
	for _, binding := range bindings {
		if project := bindingProject(binding); project != "" {
			byProject[project] = append(byProject[project], binding)
		}
	}
	projects := make([]string, 0, len(byProject))
	for project := range byProject {
		projects = append(projects, project)
	}
	sort.Strings(projects)

	winners := make(map[string]types.BrainEntry, len(byProject))
	var losers []types.BrainEntry
	for _, project := range projects {
		group := byProject[project]
		sort.SliceStable(group, func(i, j int) bool { return bindingOlder(group[i], group[j]) })
		winners[project] = group[0]
		losers = append(losers, group[1:]...)
	}
	return winners, losers
}

// bindingOlder orders bindings by creation instant, then by ID. An unparseable
// creation instant sorts last.
func bindingOlder(a, b types.BrainEntry) bool {
	ca, cb := bindingCreated(a), bindingCreated(b)
	if !ca.Equal(cb) {
		return ca.Before(cb)
	}
	return a.ID < b.ID
}

func bindingCreated(binding types.BrainEntry) time.Time {
	created, err := time.Parse(time.RFC3339, binding.Created)
	if err != nil {
		return time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC)
	}
	return created
}

// warnBindingLosers logs each ignored duplicate once per modification of that
// binding, not on every tick.
func (s *AutomationService) warnBindingLosers(losers []types.BrainEntry) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.bindingWarned == nil {
		s.bindingWarned = make(map[string]string)
	}
	for _, loser := range losers {
		if s.bindingWarned[loser.ID] == loser.Modified {
			continue
		}
		s.bindingWarned[loser.ID] = loser.Modified
		slog.Warn("duplicate automation binding ignored: an older binding for this project wins",
			"binding", loser.ID, "parent", loser.Extends, "project", bindingProject(loser))
	}
}

// eventTargetFor decides whether one active automation fires for one event,
// and the config it fires under. A binding is never evaluated on its own. For
// a global parent, an event carrying a project consults that project's binding:
// an inactive one opts the project out, and an active one opts it in, so the
// parent's project filter does not gate it.
func (s *AutomationService) eventTargetFor(ctx context.Context, parent types.BrainEntry, evt types.Event) (types.BrainEntry, bool, error) {
	if parent.Extends != "" {
		return parent, false, nil
	}
	if parent.ProjectID != "" || evt.ProjectID == "" {
		return parent, automationMatchesEvent(parent, evt), nil
	}
	winners, err := s.bindingWinnersOf(ctx, parent.ID)
	if err != nil {
		return types.BrainEntry{}, false, fmt.Errorf("list bindings of automation %s: %w", parent.ID, err)
	}
	binding, bound := winners[evt.ProjectID]
	if !bound {
		return parent, automationMatchesEvent(parent, evt), nil
	}
	if !bindingIsActive(binding) {
		return parent, false, nil
	}
	target := boundTarget(parent, binding)
	return target.entry, automationMatchesEvent(withoutProjectFilter(target.entry), evt), nil
}

// withoutProjectFilter returns a copy of entry whose trigger no longer
// filters on project. An opted-in binding has already chosen its project.
func withoutProjectFilter(entry types.BrainEntry) types.BrainEntry {
	if entry.Trigger == nil {
		return entry
	}
	trigger := *entry.Trigger
	trigger.Filter = make(map[string]string, len(entry.Trigger.Filter))
	for key, value := range entry.Trigger.Filter {
		if key == "project" || key == "project_id" {
			continue
		}
		trigger.Filter[key] = value
	}
	entry.Trigger = &trigger
	return entry
}
