package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/types"
	braincron "github.com/huynle/brain-api/pkg/cron"
	"github.com/huynle/brain-api/pkg/schedule"
)

const (
	defaultTimelineDenseThreshold = 48
	defaultTimelineBudget         = 100000
)

type timelineEntryLister interface {
	List(context.Context, types.ListEntriesRequest) (*types.ListEntriesResponse, error)
}

type timelineEventReader interface {
	Recent(context.Context, int, map[string]string) ([]types.Event, error)
}

// automationTargetResolver is the one AutomationService method the timeline
// borrows. scheduledTargetProjects decides which projects a cron automation
// fires for, so the projection cannot disagree with the scheduler about them.
type automationTargetResolver interface {
	scheduledTargetProjects(ctx context.Context, automation types.BrainEntry) ([]string, error)
}

type TimelineService struct {
	entries timelineEntryLister
	events  timelineEventReader
	targets automationTargetResolver
	now     func() time.Time
}

type TimelineServiceOption func(*TimelineService)

func WithTimelineClock(now func() time.Time) TimelineServiceOption {
	return func(service *TimelineService) { service.now = now }
}

// WithTimelineTargets wires the scheduler's project resolution for cron
// automations that select projects with a filter. Without it such an
// automation is reported as a warning and not projected.
func WithTimelineTargets(resolver automationTargetResolver) TimelineServiceOption {
	return func(service *TimelineService) {
		if resolver != nil {
			service.targets = resolver
		}
	}
}

func NewTimelineService(entries timelineEntryLister, events timelineEventReader, options ...TimelineServiceOption) *TimelineService {
	service := &TimelineService{entries: entries, events: events, targets: &AutomationService{}, now: time.Now}
	for _, option := range options {
		option(service)
	}
	return service
}

// Timeline loads tenant-scoped source records before delegating recurrence and
// aggregation behavior to the pure projection core.
func (s *TimelineService) Timeline(ctx context.Context, from, to time.Time, project string) (*types.TimelineResponse, error) {
	var entries []types.BrainEntry
	for _, entryType := range []string{"task", "automation", "reminder"} {
		loaded, err := s.listType(ctx, entryType, project)
		if err != nil {
			return nil, err
		}
		entries = append(entries, loaded...)
	}
	filters := map[string]string{}
	if project != "" {
		filters["project_id"] = project
	}
	events, err := s.events.Recent(ctx, defaultTimelineBudget, filters)
	if err != nil {
		return nil, fmt.Errorf("list timeline events: %w", err)
	}
	targets := s.automationTargets(ctx, entries, project)
	remaining, err := s.automationRunsRemaining(ctx, entries, targets)
	if err != nil {
		return nil, err
	}
	result := BuildTimeline(entries, events, TimelineProjectionOptions{
		From: from, To: to, Now: s.now(), RunsRemaining: remaining, AutomationTargets: targets,
	})
	return &result, nil
}

// automationTargetSet is one cron automation's resolved fan-out: the projects
// it fires for, or the error that stopped resolution.
type automationTargetSet struct {
	Projects []string
	Err      error
}

// automationProjectKey identifies one automation's runs for one target project.
type automationProjectKey struct {
	AutomationID string
	Project      string
}

// automationTargets resolves, for every cron automation the timeline may
// project, the projects it fires for. A project-scoped timeline keeps only
// that project's runs and the unscoped run, as before.
func (s *TimelineService) automationTargets(ctx context.Context, entries []types.BrainEntry, project string) map[string]automationTargetSet {
	targets := make(map[string]automationTargetSet)
	for _, entry := range entries {
		if !cronProjectionCandidate(entry) {
			continue
		}
		projects, err := s.targets.scheduledTargetProjects(ctx, entry)
		if err == nil && project != "" {
			projects = scopeTargetsToProject(projects, project)
		}
		targets[entry.ID] = automationTargetSet{Projects: projects, Err: err}
	}
	return targets
}

// scopeTargetsToProject keeps the targets a project-scoped timeline shows: that
// project, and the unscoped run (project "").
func scopeTargetsToProject(projects []string, project string) []string {
	var kept []string
	for _, target := range projects {
		if target == project || target == "" {
			kept = append(kept, target)
		}
	}
	return kept
}

// automationRunsRemaining returns, for each capped cron automation and each of
// its target projects, how many runs that project's max_runs still allows. Each
// count is the scheduler's own (countAutomationRuns for that project), so the
// projection stops where the scheduler stops.
func (s *TimelineService) automationRunsRemaining(ctx context.Context, entries []types.BrainEntry, targets map[string]automationTargetSet) (map[automationProjectKey]int, error) {
	remaining := make(map[automationProjectKey]int)
	for _, entry := range entries {
		if entry.Type != "automation" || entry.MaxRuns == nil || *entry.MaxRuns <= 0 {
			continue
		}
		resolved, ok := targets[entry.ID]
		if !ok || resolved.Err != nil {
			continue
		}
		for _, project := range resolved.Projects {
			used, err := countAutomationRuns(ctx, s.entries, entry.ID, project, *entry.MaxRuns)
			if err != nil {
				return nil, err
			}
			remaining[automationProjectKey{AutomationID: entry.ID, Project: project}] = max(0, *entry.MaxRuns-used)
		}
	}
	return remaining, nil
}

func (s *TimelineService) listType(ctx context.Context, entryType, project string) ([]types.BrainEntry, error) {
	const pageSize = 200
	var entries []types.BrainEntry
	for offset := 0; ; offset += pageSize {
		response, err := s.entries.List(ctx, types.ListEntriesRequest{Type: entryType, Project: project, Limit: pageSize, Offset: offset})
		if err != nil {
			return nil, fmt.Errorf("list timeline %s entries: %w", entryType, err)
		}
		if response == nil || len(response.Entries) == 0 {
			return entries, nil
		}
		entries = append(entries, response.Entries...)
		if len(response.Entries) < pageSize {
			return entries, nil
		}
	}
}

type TimelineProjectionOptions struct {
	From                time.Time
	To                  time.Time
	Now                 time.Time
	DenseDailyThreshold int
	ExpansionBudget     int
	// RunsRemaining caps projected runs per automation and target project by
	// the runs that project's max_runs still allows. Pairs absent from the map
	// are uncapped.
	RunsRemaining map[automationProjectKey]int
	// AutomationTargets maps a cron automation's ID to the projects it fires
	// for. An automation absent from the map is resolved without a project
	// lister: an unfiltered one fires once for its own project (or unscoped),
	// and a filtered one is reported as unresolved.
	AutomationTargets map[string]automationTargetSet
}

type timelineBuilder struct {
	opts      TimelineProjectionOptions
	items     []types.TimelineItem
	warnings  []types.TimelineWarning
	remaining int
	truncated bool
	features  map[string]bool
}

// BuildTimeline combines recorded events with read-only projections generated
// from the same persisted schedule fields consumed by Brain's runners.
func BuildTimeline(entries []types.BrainEntry, events []types.Event, opts TimelineProjectionOptions) types.TimelineResponse {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.DenseDailyThreshold <= 0 {
		opts.DenseDailyThreshold = defaultTimelineDenseThreshold
	}
	if opts.ExpansionBudget <= 0 {
		opts.ExpansionBudget = defaultTimelineBudget
	}
	b := &timelineBuilder{opts: opts, remaining: opts.ExpansionBudget, features: make(map[string]bool)}
	b.addActual(events)
	for i := range entries {
		b.projectEntry(entries[i])
	}
	b.aggregateDenseDays()
	sort.SliceStable(b.items, func(i, j int) bool { return b.items[i].Timestamp.Before(b.items[j].Timestamp) })
	if b.items == nil {
		b.items = []types.TimelineItem{}
	}
	if b.warnings == nil {
		b.warnings = []types.TimelineWarning{}
	}
	return types.TimelineResponse{From: opts.From, To: opts.To, GeneratedAt: opts.Now, Items: b.items, Warnings: b.warnings, Truncated: b.truncated}
}

func (b *timelineBuilder) addActual(events []types.Event) {
	for _, event := range events {
		if event.Timestamp.Before(b.opts.From) || !event.Timestamp.Before(b.opts.To) {
			continue
		}
		b.items = append(b.items, types.TimelineItem{
			ID: event.ID, Type: event.Type, Source: event.Source, Timestamp: event.Timestamp,
			ProjectID: event.ProjectID, TaskID: event.TaskID, TaskPath: event.TaskPath,
			TaskTitle: event.TaskTitle, FeatureID: event.FeatureID, RunnerID: event.RunnerID,
			Reason: event.Reason, Metadata: event.Metadata, TemporalState: types.TimelineStateActual,
		})
	}
}

func (b *timelineBuilder) projectEntry(entry types.BrainEntry) {
	switch entry.Type {
	case "task":
		b.projectTask(entry)
		b.projectFeature(entry)
	case "automation":
		b.projectAutomation(entry)
	case "reminder":
		b.projectReminder(entry)
	}
}

func (b *timelineBuilder) projectionStart() time.Time {
	if b.opts.Now.After(b.opts.From) {
		return b.opts.Now
	}
	return b.opts.From
}

func taskScheduleEligible(entry types.BrainEntry) bool {
	if entry.ScheduleEnabled != nil && !*entry.ScheduleEnabled {
		return false
	}
	switch entry.Status {
	case "active", "completed", "blocked":
		return true
	default:
		return false
	}
}

func countedRuns(runs []types.CronRun) int {
	count := 0
	for _, run := range runs {
		switch run.Status {
		case "completed", "failed", "skipped", "in_progress":
			count++
		}
	}
	return count
}

func (b *timelineBuilder) projectTask(entry types.BrainEntry) {
	b.addMilestone(entry, "task", entry.ID, entry.StartsAt, types.TimelineKindStart, "task.start")
	b.addMilestone(entry, "task", entry.ID, entry.ExpiresAt, types.TimelineKindExpiry, "task.expiry")
	if !taskScheduleEligible(entry) {
		return
	}
	limit := b.remaining
	if entry.MaxRuns != nil && *entry.MaxRuns > 0 {
		limit = min(limit, *entry.MaxRuns-countedRuns(entry.Runs))
	}
	if limit <= 0 {
		return
	}
	start, end, ok := b.sourceWindow(entry.StartsAt, entry.ExpiresAt, entry.Timezone)
	if !ok {
		return
	}
	if entry.Schedule != "" {
		b.addCron(entry, "task", entry.ID, entry.Schedule, entry.Timezone, start, end, limit, "task.projected")
	} else if entry.RunOnceAt != "" {
		b.addOneTime(entry, "task", entry.ID, entry.RunOnceAt, entry.Timezone, types.TimelineKindExecution, "task.projected")
	}
}

func (b *timelineBuilder) projectFeature(entry types.BrainEntry) {
	if entry.FeatureID == "" {
		return
	}
	key := entry.ProjectID + "\x00" + entry.FeatureID
	if b.features[key] {
		return
	}
	b.features[key] = true
	b.addMilestone(entry, "feature", entry.FeatureID, entry.FeatureStartsAt, types.TimelineKindStart, "feature.start")
	b.addMilestone(entry, "feature", entry.FeatureID, entry.FeatureExpiresAt, types.TimelineKindExpiry, "feature.expiry")
	start, end, ok := b.sourceWindow(entry.FeatureStartsAt, entry.FeatureExpiresAt, entry.FeatureTimezone)
	if !ok {
		return
	}
	if entry.FeatureSchedule != "" {
		b.addCron(entry, "feature", entry.FeatureID, entry.FeatureSchedule, entry.FeatureTimezone, start, end, b.remaining, "feature.projected")
	} else if entry.FeatureRunOnceAt != "" {
		b.addOneTime(entry, "feature", entry.FeatureID, entry.FeatureRunOnceAt, entry.FeatureTimezone, types.TimelineKindExecution, "feature.projected")
	}
}

// cronProjectionCandidate reports whether an automation is clock-driven the way
// the scheduler evaluates it: active, cron-typed with a schedule or an every
// interval, and not a goal (goals are driven by the goal loop).
func cronProjectionCandidate(entry types.BrainEntry) bool {
	return entry.Type == "automation" && entry.Status == "active" && entry.Trigger != nil &&
		entry.Trigger.Type == "cron" && (entry.Trigger.Schedule != "" || entry.Trigger.Every != "") &&
		!isGoalAutomation(entry)
}

func (b *timelineBuilder) projectAutomation(entry types.BrainEntry) {
	if !cronProjectionCandidate(entry) {
		return
	}
	// The window is the automation's own starts_at..expires_at, clipped to the
	// requested range, so a run outside that lifecycle is never projected.
	start, end, ok := b.sourceWindow(entry.StartsAt, entry.ExpiresAt, entry.Trigger.Timezone)
	if !ok {
		return
	}
	spec, err := automationScheduleSpec(entry)
	if err != nil {
		b.warn(entry.ID, err)
		return
	}
	sched, err := schedule.Compile(spec)
	if err != nil {
		b.warn(entry.ID, err)
		return
	}
	resolved := b.automationTargetsFor(entry)
	if resolved.Err != nil {
		b.warn(entry.ID, resolved.Err)
		return
	}
	rule := automationProjectionRule(entry)
	for _, project := range resolved.Projects {
		limit := b.remaining
		if left, capped := b.opts.RunsRemaining[automationProjectKey{AutomationID: entry.ID, Project: project}]; capped {
			limit = min(limit, left)
		}
		if limit <= 0 {
			continue
		}
		target := entry
		target.ProjectID = project
		b.addSchedule(target, sched, sched.Offset(entry.ID, project), rule, entry.Trigger.Timezone, start, end, limit)
	}
}

// automationTargetsFor returns the resolved targets of one cron automation,
// resolving it without a project lister when the caller pre-resolved nothing.
func (b *timelineBuilder) automationTargetsFor(entry types.BrainEntry) automationTargetSet {
	if resolved, ok := b.opts.AutomationTargets[entry.ID]; ok {
		return resolved
	}
	projects, err := (&AutomationService{}).scheduledTargetProjects(context.Background(), entry)
	return automationTargetSet{Projects: projects, Err: err}
}

// automationProjectionRule is the rule a projection carries: the cron
// expression, or the every interval with its time of day.
func automationProjectionRule(entry types.BrainEntry) string {
	if entry.Trigger.Schedule != "" {
		return entry.Trigger.Schedule
	}
	if entry.Trigger.At != "" {
		return "every " + entry.Trigger.Every + " at " + entry.Trigger.At
	}
	return "every " + entry.Trigger.Every
}

func reminderEligible(entry types.BrainEntry) bool {
	if entry.Reminder == nil {
		return false
	}
	return entry.Status == "active" || (entry.Status == "pending" && entry.Reminder.Repeats())
}

func (b *timelineBuilder) projectReminder(entry types.BrainEntry) {
	if !reminderEligible(entry) || !entry.Reminder.IsDated() {
		return
	}
	first, err := entry.Reminder.RemindAtTime()
	if err != nil {
		b.warn(entry.ID, err)
		return
	}
	loc := braincron.LoadTimezone(entry.Reminder.Timezone)
	current := first.In(loc)
	for {
		if current.After(b.opts.To) || current.Equal(b.opts.To) || entry.Reminder.RepeatEnded(current) {
			return
		}
		if !current.Before(b.projectionStart()) {
			if !b.appendProjection(entry, "reminder", entry.Reminder.ID, current, types.TimelineKindReminder, "reminder.projected", entry.Reminder.Repeat, entry.Reminder.Timezone) {
				return
			}
		}
		next, ok := nextReminderOccurrence(current, entry.Reminder.Repeat, loc)
		if !ok {
			return
		}
		current = next
	}
}

func nextReminderOccurrence(current time.Time, repeat string, loc *time.Location) (time.Time, bool) {
	local := current.In(loc)
	switch types.NormalizeReminderRepeat(repeat) {
	case types.ReminderRepeatDaily:
		return local.AddDate(0, 0, 1), true
	case types.ReminderRepeatWeekly:
		return local.AddDate(0, 0, 7), true
	case types.ReminderRepeatMonthly:
		return local.AddDate(0, 1, 0), true
	case types.ReminderRepeatYearly:
		return local.AddDate(1, 0, 0), true
	default:
		return time.Time{}, false
	}
}

func (b *timelineBuilder) sourceWindow(startsAt, expiresAt, timezone string) (time.Time, time.Time, bool) {
	start, end := b.projectionStart(), b.opts.To
	if startsAt != "" {
		parsed, err := time.Parse(time.RFC3339, startsAt)
		if err != nil {
			return start, end, true
		}
		if parsed.After(start) {
			start = parsed
		}
	}
	if expiresAt != "" {
		parsed, err := time.Parse(time.RFC3339, expiresAt)
		if err != nil {
			return start, end, true
		}
		if parsed.Before(end) {
			end = parsed
		}
	}
	return start, end, start.Before(end)
}

// addSchedule appends one target's runs of a compiled schedule, from start
// (inclusive) to end (exclusive), at most limit of them. Each instant is
// NextSlot's own, so a projected run lands exactly when the scheduler's slot
// does.
func (b *timelineBuilder) addSchedule(entry types.BrainEntry, sched *schedule.Schedule, offset time.Duration, rule, timezone string, start, end time.Time, limit int) {
	ctx := context.Background()
	budgetLimited := limit >= b.remaining
	cursor := start.Add(-time.Nanosecond)
	for emitted := 0; emitted < limit; emitted++ {
		slot, ok, err := sched.NextSlot(ctx, cursor, offset)
		if err != nil {
			b.warn(entry.ID, err)
			return
		}
		if !ok || !slot.At.Before(end) {
			return
		}
		if !b.appendProjection(entry, "automation", entry.ID, slot.At, types.TimelineKindExecution, "automation.projected", rule, timezone) {
			return
		}
		cursor = slot.At
	}
	if budgetLimited {
		slot, ok, err := sched.NextSlot(ctx, cursor, offset)
		if err == nil && ok && slot.At.Before(end) {
			b.truncated = true
		}
	}
}

func (b *timelineBuilder) addCron(entry types.BrainEntry, sourceKind, sourceID, expression, timezone string, start, end time.Time, limit int, eventType string) {
	parsed, err := braincron.Parse(expression)
	if err != nil {
		b.warn(sourceID, err)
		return
	}
	loc := braincron.LoadTimezone(timezone)
	cursor := start.In(loc).Add(-time.Nanosecond)
	budgetLimited := limit >= b.remaining
	for emitted := 0; emitted < limit; emitted++ {
		next := parsed.NextAfter(cursor)
		if next.IsZero() || !next.Before(end) {
			return
		}
		if !b.appendProjection(entry, sourceKind, sourceID, next, types.TimelineKindExecution, eventType, expression, timezone) {
			return
		}
		cursor = next
	}
	if budgetLimited {
		next := parsed.NextAfter(cursor)
		if !next.IsZero() && next.Before(end) {
			b.truncated = true
		}
	}
}

func (b *timelineBuilder) addOneTime(entry types.BrainEntry, sourceKind, sourceID, value, timezone, kind, eventType string) {
	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		b.warn(sourceID, err)
		return
	}
	if at.Before(b.projectionStart()) || !at.Before(b.opts.To) {
		return
	}
	b.appendProjection(entry, sourceKind, sourceID, at, kind, eventType, "once", timezone)
}

func (b *timelineBuilder) addMilestone(entry types.BrainEntry, sourceKind, sourceID, value, kind, eventType string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		b.warn(sourceID, err)
		return
	}
	if at.Before(b.projectionStart()) || !at.Before(b.opts.To) {
		return
	}
	b.appendProjection(entry, sourceKind, sourceID, at, kind, eventType, "", "")
}

func (b *timelineBuilder) appendProjection(entry types.BrainEntry, sourceKind, sourceID string, at time.Time, kind, eventType, rule, timezone string) bool {
	if b.remaining <= 0 {
		b.truncated = true
		return false
	}
	b.remaining--
	taskID, featureID := "", entry.FeatureID
	if sourceKind == "task" {
		taskID = entry.ID
	}
	if sourceKind == "feature" {
		featureID = sourceID
	}
	id := fmt.Sprintf("projection:%s:%s:%s:%d", sourceKind, sourceID, kind, at.Unix())
	if sourceKind == "automation" && entry.ProjectID != "" {
		// Fan-out runs of one automation share instants across projects; the
		// project keeps their IDs distinct.
		id += ":" + entry.ProjectID
	}
	b.items = append(b.items, types.TimelineItem{
		ID:   id,
		Type: eventType, Source: "forecast", Timestamp: at.UTC(), ProjectID: entry.ProjectID,
		TaskID: taskID, TaskPath: entry.Path, TaskTitle: entry.Title, FeatureID: featureID,
		Summary: entry.Title, TemporalState: types.TimelineStateProjected, TemporalKind: kind,
		SourceKind: sourceKind, SourceID: sourceID, SourcePath: entry.Path,
		Timezone: timezone, ProjectionRule: rule, OccurrenceCount: 1,
	})
	return true
}

func (b *timelineBuilder) warn(sourceID string, err error) {
	b.warnings = append(b.warnings, types.TimelineWarning{SourceID: sourceID, Message: err.Error()})
}

func (b *timelineBuilder) aggregateDenseDays() {
	type group struct {
		indexes []int
		loc     *time.Location
	}
	groups := make(map[string]*group)
	for i := range b.items {
		item := b.items[i]
		if item.TemporalState != types.TimelineStateProjected || item.OccurrenceCount != 1 {
			continue
		}
		loc := braincron.LoadTimezone(item.Timezone)
		day := item.Timestamp.In(loc).Format("2006-01-02")
		// Runs for different projects are different runs, so the project is
		// part of the group key.
		key := item.SourceKind + "\x00" + item.SourceID + "\x00" + item.ProjectID + "\x00" + item.TemporalKind + "\x00" + day
		if groups[key] == nil {
			groups[key] = &group{loc: loc}
		}
		groups[key].indexes = append(groups[key].indexes, i)
	}
	remove := make(map[int]bool)
	for _, g := range groups {
		if len(g.indexes) <= b.opts.DenseDailyThreshold {
			continue
		}
		firstIndex := g.indexes[0]
		first, last := b.items[firstIndex].Timestamp, b.items[g.indexes[len(g.indexes)-1]].Timestamp
		b.items[firstIndex].OccurrenceCount = len(g.indexes)
		b.items[firstIndex].WindowStart, b.items[firstIndex].WindowEnd = &first, &last
		b.items[firstIndex].ID += ":aggregate"
		for _, index := range g.indexes[1:] {
			remove[index] = true
		}
	}
	if len(remove) == 0 {
		return
	}
	kept := b.items[:0]
	for i := range b.items {
		if !remove[i] {
			kept = append(kept, b.items[i])
		}
	}
	b.items = kept
}
