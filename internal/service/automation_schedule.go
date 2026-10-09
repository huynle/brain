package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/schedule"
)

// Slot-based scheduling for cron-triggered automations.
//
// A schedule yields slots (pkg/schedule). An automation fires once per slot
// per target project, where the target's stable stagger offset shifts its
// slots. CheckScheduled evaluates one tick; this file holds the trigger
// translation, the compile cache and the per-target bookkeeping.

// scheduledOnTimeWindow is how late a slot may be seen and still count as on
// time. The scheduler ticks once a minute, so every slot is first seen within
// one window of its instant. A later sighting is a catch-up.
const scheduledOnTimeWindow = time.Minute

// scheduledAuditScanLimit bounds how many of an automation's newest run
// audits are read to recover its last handled slot after a restart.
const scheduledAuditScanLimit = maxRunsPageSize

// compiledAutomationSchedule is one automation trigger compiled for one
// modification of its entry. err is set, and sched nil, when the trigger
// cannot be scheduled; such an entry is skipped.
type compiledAutomationSchedule struct {
	modified string
	sched    *schedule.Schedule
	catchUp  schedule.CatchUp
	err      error
}

// scheduleKey identifies one target of a scheduled automation: the entry and
// the project its runs are for.
type scheduleKey struct {
	automationID string
	project      string
}

// scheduledRun is one slot that is due for a target on this tick.
type scheduledRun struct {
	slot   schedule.Slot
	onTime bool
}

// isScheduledCronAutomation reports whether the slot evaluator owns an
// automation. Goal automations are driven exclusively by the goal reconcile
// loop (see automationMatchesEvent for the event-path guard); without this
// exclusion a goal carrying a cron trigger would be dispatched twice.
func isScheduledCronAutomation(automation types.BrainEntry) bool {
	if automation.Trigger == nil || automation.Action == nil || automation.Trigger.Type != "cron" {
		return false
	}
	if isGoalAutomation(automation) {
		return false
	}
	return automation.Trigger.Schedule != "" || automation.Trigger.Every != ""
}

// automationScheduleSpec translates an automation's trigger into a
// schedule.Spec. Day filters are attached by the caller (dayFiltersFor).
func automationScheduleSpec(automation types.BrainEntry) (schedule.Spec, error) {
	if automation.Trigger == nil {
		return schedule.Spec{}, errors.New("automation has no trigger")
	}
	tc := automation.Trigger
	spec := schedule.Spec{
		Cron:     tc.Schedule,
		Every:    tc.Every,
		At:       tc.At,
		Timezone: tc.Timezone,
		Anchor:   automationAnchor(automation),
	}
	if tc.Stagger != "" {
		stagger, err := time.ParseDuration(tc.Stagger)
		if err != nil {
			return schedule.Spec{}, fmt.Errorf("trigger.stagger: %w", err)
		}
		spec.Stagger = stagger
	}
	return spec, nil
}

// automationAnchor is where an interval schedule starts: starts_at when it is
// set and parses, else the entry's creation instant. The zero time means the
// anchor is missing, which Compile rejects for an interval.
func automationAnchor(automation types.BrainEntry) time.Time {
	if automation.StartsAt != "" {
		if start, err := time.Parse(time.RFC3339, automation.StartsAt); err == nil {
			return start
		}
	}
	created, _ := time.Parse(time.RFC3339, automation.Created)
	return created
}

// dayFiltersFor returns the day filters that gate an automation's slots. The
// built-in calendar and event filters arrive in a later change, so none apply
// yet and every slot exists.
func (s *AutomationService) dayFiltersFor(automation types.BrainEntry) []schedule.DayFilter {
	return nil
}

// compiledScheduleFor returns automation's compiled schedule, compiling it
// again only when the entry was modified since it was last compiled. ok is
// false when the trigger cannot be scheduled. A warning is logged once per
// modification, not on every tick.
func (s *AutomationService) compiledScheduleFor(automation types.BrainEntry) (*compiledAutomationSchedule, bool) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if cached, ok := s.compiled[automation.ID]; ok && cached.modified == automation.Modified {
		return cached, cached.err == nil
	}
	compiled := s.compileAutomationSchedule(automation)
	if compiled.err != nil {
		slog.Warn("automation schedule skipped: trigger cannot be scheduled",
			"automation", automation.ID, "error", compiled.err)
	}
	if s.compiled == nil {
		s.compiled = make(map[string]*compiledAutomationSchedule)
	}
	s.compiled[automation.ID] = compiled
	return compiled, compiled.err == nil
}

func (s *AutomationService) compileAutomationSchedule(automation types.BrainEntry) *compiledAutomationSchedule {
	compiled := &compiledAutomationSchedule{modified: automation.Modified}
	spec, err := automationScheduleSpec(automation)
	if err != nil {
		compiled.err = err
		return compiled
	}
	spec.DayFilters = s.dayFiltersFor(automation)
	sched, err := schedule.Compile(spec)
	if err != nil {
		compiled.err = err
		return compiled
	}
	catchUp, err := schedule.ParseCatchUp(automation.Trigger.CatchUp)
	if err != nil {
		compiled.err = fmt.Errorf("trigger.catch_up: %w", err)
		return compiled
	}
	compiled.sched = sched
	compiled.catchUp = catchUp
	return compiled
}

// scheduledRunFor reports the slot for one target that is due on this tick.
//
// The latest slot at or before now is the only candidate. A slot is due when it
// is newer than the target's floor (see scheduleFloor) and the catch-up policy
// allows it. A slot that is not due is not an error: it is simply not owed.
func (s *AutomationService) scheduledRunFor(ctx context.Context, compiled *compiledAutomationSchedule, automation types.BrainEntry, project string, now time.Time) (scheduledRun, bool, error) {
	offset := schedule.StaggerOffset(automation.ID, project, compiled.sched.Stagger())
	slot, ok, err := compiled.sched.LatestSlot(ctx, now, offset)
	if err != nil || !ok {
		return scheduledRun{}, false, err
	}
	floor, err := s.scheduleFloor(ctx, automation, project, now)
	if err != nil {
		return scheduledRun{}, false, err
	}
	if !schedule.Due(slot, floor, now, compiled.catchUp) {
		return scheduledRun{}, false, nil
	}
	return scheduledRun{slot: slot, onTime: now.Sub(slot.At) < scheduledOnTimeWindow}, true, nil
}

// scheduleFloor is the instant before which no slot for target may fire: the
// latest of the target's last handled slot, the entry's last write, and its
// starts_at. Editing a schedule or creating an entry therefore never fires a
// slot that predates it.
func (s *AutomationService) scheduleFloor(ctx context.Context, automation types.BrainEntry, project string, now time.Time) (time.Time, error) {
	floor, err := s.lastHandledSlot(ctx, automation, project, now)
	if err != nil {
		return time.Time{}, err
	}
	stamp := automation.Modified
	if stamp == "" {
		stamp = automation.Created
	}
	if written, err := time.Parse(time.RFC3339, stamp); err == nil && written.After(floor) {
		floor = written
	}
	if start, err := time.Parse(time.RFC3339, automation.StartsAt); err == nil && start.After(floor) {
		floor = start
	}
	return floor, nil
}

// lastHandledSlot returns the target's last handled slot. It is cached in
// memory, and on a cache miss it is read from the newest run audits that carry
// scheduled_for. With no such audit (an upgrade, or a first sight) it baselines
// at one window before now: a slot that is due on time still fires, and no
// historic slot is replayed.
func (s *AutomationService) lastHandledSlot(ctx context.Context, automation types.BrainEntry, project string, now time.Time) (time.Time, error) {
	key := scheduleKey{automationID: automation.ID, project: project}
	s.cacheMu.Lock()
	last, cached := s.handled[key]
	s.cacheMu.Unlock()
	if cached {
		return last, nil
	}

	audits, err := s.listRunAudits(ctx, project, automation.ID, scheduledAuditScanLimit)
	if err != nil {
		return time.Time{}, err
	}
	for _, audit := range audits {
		if slot, err := time.Parse(time.RFC3339, audit.ScheduledFor); err == nil && slot.After(last) {
			last = slot
		}
	}
	if last.IsZero() {
		last = now.Add(-scheduledOnTimeWindow)
	}
	s.recordHandledSlot(key, last)
	return last, nil
}

// recordHandledSlot moves a target's last handled slot forward to at. It never
// moves backward, so a late or repeated record cannot reopen a handled slot.
func (s *AutomationService) recordHandledSlot(key scheduleKey, at time.Time) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.handled == nil {
		s.handled = make(map[scheduleKey]time.Time)
	}
	if at.After(s.handled[key]) {
		s.handled[key] = at
	}
}

// fireScheduledSlot handles one due slot for one target: it fires the
// automation, or records why it did not. Either way the slot is handled, so
// it can never run late. Only a failure to write leaves it open, to be
// retried on the next tick.
func (s *AutomationService) fireScheduledSlot(ctx context.Context, automation types.BrainEntry, project string, slot schedule.Slot) error {
	key := scheduleKey{automationID: automation.ID, project: project}
	evt := types.Event{ProjectID: project}

	// Lifecycle first, as for every firing. An expired target completes its
	// entry, and an exhausted one writes its own max_runs audit.
	ok, err := s.lifecycleAllowsAt(ctx, automation, project, slot.At)
	if err != nil {
		return err
	}
	if !ok {
		s.recordHandledSlot(key, slot.At)
		return nil
	}

	// Only a due slot reaches this gate, so a paused automation writes a skip
	// audit when it had work to do, not on every tick. The slot is still
	// handled and recorded as skipped, so a long pause never replays when it
	// ends.
	if s.isAutomationPaused(automation, evt) {
		if _, err := s.createRunAudit(ctx, automationRunAudit{
			automation:   automation,
			evt:          evt,
			project:      project,
			status:       "skipped",
			skipReason:   "paused",
			scheduledFor: slot.At,
		}); err != nil {
			return err
		}
		s.recordHandledSlot(key, slot.At)
		return nil
	}

	// An update action is applied in process, as on the event path. A cron
	// slot has no feature to act on, so the action records why it did nothing.
	if types.NormalizeAutomationActionType(automation.Action.Type) == types.AutomationActionUpdate {
		if err := s.applyUpdateAction(ctx, automation, evt); err != nil {
			return err
		}
		s.recordHandledSlot(key, slot.At)
		return nil
	}

	if _, err := s.createTask(ctx, automation, evt, scheduledDedupKey(automation.ID, project, slot.At), slot.At); err != nil {
		return err
	}
	s.recordHandledSlot(key, slot.At)
	return nil
}

// scheduledDedupKey names the task generated for one slot of one target. A
// slot can produce at most one task, even when two evaluators race for it.
func scheduledDedupKey(automationID, project string, slot time.Time) string {
	return fmt.Sprintf("sched:%s:%s:%s", automationID, project, slot.UTC().Format(time.RFC3339))
}

// forgetUnscheduled drops the caches of automations that the latest tick did
// not evaluate, so the caches track only live entries.
func (s *AutomationService) forgetUnscheduled(live map[string]struct{}) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	for id := range s.compiled {
		if _, ok := live[id]; !ok {
			delete(s.compiled, id)
		}
	}
	for key := range s.handled {
		if _, ok := live[key.automationID]; !ok {
			delete(s.handled, key)
		}
	}
}
