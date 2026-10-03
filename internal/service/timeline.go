package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/types"
	braincron "github.com/huynle/brain-api/pkg/cron"
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

type TimelineService struct {
	entries timelineEntryLister
	events  timelineEventReader
	now     func() time.Time
}

type TimelineServiceOption func(*TimelineService)

func WithTimelineClock(now func() time.Time) TimelineServiceOption {
	return func(service *TimelineService) { service.now = now }
}

func NewTimelineService(entries timelineEntryLister, events timelineEventReader, options ...TimelineServiceOption) *TimelineService {
	service := &TimelineService{entries: entries, events: events, now: time.Now}
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
	result := BuildTimeline(entries, events, TimelineProjectionOptions{From: from, To: to, Now: s.now()})
	return &result, nil
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

func (b *timelineBuilder) projectAutomation(entry types.BrainEntry) {
	if entry.Status != "active" || entry.Trigger == nil || entry.Trigger.Type != "cron" || entry.Trigger.Schedule == "" {
		return
	}
	b.addCron(entry, "automation", entry.ID, entry.Trigger.Schedule, entry.Trigger.Timezone, b.projectionStart(), b.opts.To, b.remaining, "automation.projected")
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

func (b *timelineBuilder) addCron(entry types.BrainEntry, sourceKind, sourceID, expression, timezone string, start, end time.Time, limit int, eventType string) {
	schedule, err := braincron.Parse(expression)
	if err != nil {
		b.warn(sourceID, err)
		return
	}
	loc := braincron.LoadTimezone(timezone)
	cursor := start.In(loc).Add(-time.Nanosecond)
	budgetLimited := limit >= b.remaining
	for emitted := 0; emitted < limit; emitted++ {
		next := schedule.NextAfter(cursor)
		if next.IsZero() || !next.Before(end) {
			return
		}
		if !b.appendProjection(entry, sourceKind, sourceID, next, types.TimelineKindExecution, eventType, expression, timezone) {
			return
		}
		cursor = next
	}
	if budgetLimited {
		next := schedule.NextAfter(cursor)
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
	b.items = append(b.items, types.TimelineItem{
		ID:   fmt.Sprintf("projection:%s:%s:%s:%d", sourceKind, sourceID, kind, at.Unix()),
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
		key := item.SourceKind + "\x00" + item.SourceID + "\x00" + item.TemporalKind + "\x00" + day
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
