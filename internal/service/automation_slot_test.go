package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/schedule"
)

// Slot-based evaluation of cron automations, driven on a fake clock.
//
// Each test moves one clock: the entry timestamps (types.TimeNowUTC, which
// Save stamps as created and modified) and the service clock together, so a
// test decides when an entry was written and when each tick runs. Ticks are
// explicit instants, which is how production ticks look from the scheduler.

// slotFixture is one brain and the evaluator under test.
type slotFixture struct {
	t     *testing.T
	brain *BrainServiceImpl
	svc   *AutomationService
	now   time.Time
}

func newSlotFixture(t *testing.T, start time.Time, projects ...string) *slotFixture {
	t.Helper()
	brain, _, _ := newTestBrainService(t)
	original := types.TimeNowUTC
	t.Cleanup(func() { types.TimeNowUTC = original })
	f := &slotFixture{t: t, brain: brain}
	f.svc = f.newService(projects...)
	f.setNow(start)
	return f
}

// newService returns an evaluator with an empty schedule cache, which is what
// a restarted server has.
func (f *slotFixture) newService(projects ...string) *AutomationService {
	svc := NewAutomationService(f.brain)
	svc.SetClock(func() time.Time { return f.now })
	if len(projects) > 0 {
		svc.SetProjectLister(&stubProjectLister{projects: projects})
	}
	return svc
}

func (f *slotFixture) setNow(now time.Time) {
	f.now = now
	types.TimeNowUTC = func() time.Time { return now }
}

// tick moves both clocks to at and runs one scheduler tick.
func (f *slotFixture) tick(at time.Time) {
	f.t.Helper()
	f.setNow(at)
	if err := f.svc.CheckScheduled(context.Background(), at); err != nil {
		f.t.Fatalf("CheckScheduled at %s: %v", at.Format(time.RFC3339), err)
	}
}

// slotAutomation describes one cron automation for the fixture to save.
type slotAutomation struct {
	project  string // owning project; empty with global set
	global   bool
	startsAt string
	maxRuns  *int
	trigger  types.TriggerConfig // Type is always cron
}

func (f *slotFixture) save(a slotAutomation) *types.CreateEntryResponse {
	f.t.Helper()
	trigger := a.trigger
	trigger.Type = "cron"
	req := types.CreateEntryRequest{
		Type:     "automation",
		Title:    "Slot automation",
		Content:  "slot fixture",
		Status:   "active",
		Project:  a.project,
		StartsAt: a.startsAt,
		MaxRuns:  a.maxRuns,
		Trigger:  &trigger,
		Action:   &types.AutomationAction{Type: "prompt", DirectPrompt: "run for {{.Project}}"},
	}
	if a.global {
		req.Global = serviceBoolPtr(true)
	}
	resp, err := f.brain.Save(context.Background(), req)
	if err != nil {
		f.t.Fatalf("Save automation: %v", err)
	}
	// Saved at the fixture's instant: the entry's modified time is that
	// instant, as it would be for an entry written then.
	stampModified(f.t, f.brain, resp.Path, f.now)
	return resp
}

// stampModified makes an entry's modified time the instant at. Brain reports
// Modified from the entry file's mtime (pkg/markdown), not from the injected
// clock, so a fake-clock test sets the mtime and re-indexes the file, and the
// index reports the same instant the test chose.
func stampModified(t *testing.T, brain *BrainServiceImpl, path string, at time.Time) {
	t.Helper()
	full := filepath.Join(brain.config.BrainDir, path)
	if err := os.Chtimes(full, at, at); err != nil {
		t.Fatalf("set mtime of %s: %v", path, err)
	}
	if err := brain.indexer.IndexFile(path); err != nil {
		t.Fatalf("re-index %s: %v", path, err)
	}
}

// stampAutomationsModified sets the modified time of every automation in the
// brain to at (see stampModified). Tests that evaluate fixed instants stamp
// their automations before the first evaluation, so the entries predate it.
func stampAutomationsModified(t *testing.T, brain *BrainServiceImpl, at time.Time) {
	t.Helper()
	resp, err := brain.List(context.Background(), types.ListEntriesRequest{Type: "automation", Limit: 1000})
	if err != nil {
		t.Fatalf("list automations: %v", err)
	}
	for _, automation := range resp.Entries {
		stampModified(t, brain, automation.Path, at)
	}
}

// slotUTC is a UTC instant with second precision, for readable test tables.
func slotUTC(year int, month time.Month, day, hour, minute, second int) time.Time {
	return time.Date(year, month, day, hour, minute, second, 0, time.UTC)
}

// slotTasks returns the tasks one automation generated across projects for
// one slot, found by the slot suffix of their dedup key.
func slotTasks(t *testing.T, brain *BrainServiceImpl, projects []string, automationID string, slot time.Time) []types.BrainEntry {
	t.Helper()
	suffix := ":" + slot.UTC().Format(time.RFC3339)
	var out []types.BrainEntry
	for _, project := range projects {
		for _, task := range generatedTasksFor(t, brain, project, automationID) {
			if strings.HasSuffix(task.GeneratedKey, suffix) {
				out = append(out, task)
			}
		}
	}
	return out
}

// allGeneratedTasks counts every task one automation generated, across the
// projects it touched.
func allGeneratedTasks(t *testing.T, brain *BrainServiceImpl, projects []string, automationID string) int {
	t.Helper()
	n := 0
	for _, project := range projects {
		n += len(generatedTasksFor(t, brain, project, automationID))
	}
	return n
}

// auditsSkippedFor returns the run audits of one automation that were skipped
// for the given reason.
func auditsSkippedFor(t *testing.T, brain *BrainServiceImpl, automationID, reason string) []types.BrainEntry {
	t.Helper()
	needle := "skip_reason: " + reason + "\n"
	var out []types.BrainEntry
	for _, audit := range automationRunAudits(t, brain, automationID) {
		if strings.Contains(audit.Content, needle) {
			out = append(out, audit)
		}
	}
	return out
}

func wantScheduledFor(t *testing.T, label string, got types.BrainEntry, want time.Time) {
	t.Helper()
	if got.ScheduledFor != want.UTC().Format(time.RFC3339) {
		t.Errorf("%s: scheduled_for = %q, want %s", label, got.ScheduledFor, want.UTC().Format(time.RFC3339))
	}
}

// ceilToMinute returns the first whole-minute instant at or after t: the
// first tick that can see a slot at t.
func ceilToMinute(t time.Time) time.Time {
	floor := t.Truncate(time.Minute)
	if floor.Equal(t) {
		return floor
	}
	return floor.Add(time.Minute)
}

// ---------------------------------------------------------------------------
// On time
// ---------------------------------------------------------------------------

func TestSlot_OnTimeCronFiresOncePerSlot(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 0, 0, 0), "p")
	auto := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{Schedule: "0 3 * * *"}})

	f.tick(slotUTC(2026, 10, 8, 2, 59, 0))
	if n := len(generatedTasksFor(t, f.brain, "p", auto.ID)); n != 0 {
		t.Fatalf("before the slot: %d tasks, want 0", n)
	}

	f.tick(slotUTC(2026, 10, 8, 3, 0, 0))
	tasks := generatedTasksFor(t, f.brain, "p", auto.ID)
	if len(tasks) != 1 {
		t.Fatalf("on the slot: %d tasks, want 1", len(tasks))
	}
	wantKey := fmt.Sprintf("sched:%s:p:2026-10-08T03:00:00Z", auto.ID)
	if tasks[0].GeneratedKey != wantKey {
		t.Errorf("dedup key = %q, want %q", tasks[0].GeneratedKey, wantKey)
	}
	audits := automationRunAudits(t, f.brain, auto.ID)
	if len(audits) != 1 {
		t.Fatalf("run audits = %d, want 1", len(audits))
	}
	wantScheduledFor(t, "queued audit", audits[0], slotUTC(2026, 10, 8, 3, 0, 0))

	// The same slot, seen again within the minute and on later ticks, does not
	// fire again.
	f.tick(slotUTC(2026, 10, 8, 3, 0, 30))
	f.tick(slotUTC(2026, 10, 8, 3, 1, 0))
	if n := len(generatedTasksFor(t, f.brain, "p", auto.ID)); n != 1 {
		t.Fatalf("after the slot repeated: %d tasks, want 1", n)
	}

	f.tick(slotUTC(2026, 10, 9, 2, 59, 0))
	f.tick(slotUTC(2026, 10, 9, 3, 0, 0))
	if n := len(generatedTasksFor(t, f.brain, "p", auto.ID)); n != 2 {
		t.Fatalf("next day's slot: %d tasks, want 2", n)
	}
}

func TestSlot_StaggeredFanOutFiresEachProjectOnceInItsOwnMinute(t *testing.T) {
	projects := []string{"alpha", "bravo", "charlie", "delta", "echo"}
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 12, 0, 0), projects...)
	auto := f.save(slotAutomation{global: true, trigger: types.TriggerConfig{
		Schedule: "0 3 * * *",
		Stagger:  "2h",
		Filter:   map[string]string{"project": "*"},
	}})

	day := slotUTC(2026, 10, 9, 3, 0, 0)
	want := make(map[string]time.Time, len(projects))
	for _, project := range projects {
		want[project] = ceilToMinute(day.Add(schedule.StaggerOffset(auto.ID, project, 2*time.Hour)))
	}

	fired := make(map[string]time.Time, len(projects))
	for at := slotUTC(2026, 10, 9, 2, 0, 0); !at.After(slotUTC(2026, 10, 9, 5, 30, 0)); at = at.Add(time.Minute) {
		f.tick(at)
		for _, project := range projects {
			if _, seen := fired[project]; !seen && len(generatedTasksFor(t, f.brain, project, auto.ID)) > 0 {
				fired[project] = at
			}
		}
	}

	distinct := make(map[time.Time]struct{})
	for _, project := range projects {
		got, ok := fired[project]
		if !ok {
			t.Errorf("project %s never fired", project)
			continue
		}
		if !got.Equal(want[project]) {
			t.Errorf("project %s fired at %s, want its own minute %s", project,
				got.Format(time.RFC3339), want[project].Format(time.RFC3339))
		}
		distinct[got] = struct{}{}
		if n := len(generatedTasksFor(t, f.brain, project, auto.ID)); n != 1 {
			t.Errorf("project %s: %d tasks across the window, want exactly 1", project, n)
		}
	}
	if len(distinct) < 2 {
		t.Errorf("all %d projects fired in one minute; the stagger spread nothing", len(projects))
	}
}

func TestSlot_EveryFourDaysFiresOnAnchoredDaysAtLocalTime(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load zone: %v", err)
	}
	f := newSlotFixture(t, slotUTC(2026, 9, 30, 12, 0, 0), "p")
	auto := f.save(slotAutomation{
		project:  "p",
		startsAt: "2026-10-01T00:00:00-04:00",
		trigger:  types.TriggerConfig{Every: "4d", At: "03:00", Timezone: "America/New_York"},
	})

	// Every day from the anchor through DST's end at 03:00 local, ticked just
	// after the local slot time. The 4-day steps cross the November fallback.
	first := time.Date(2026, 10, 1, 3, 0, 30, 0, ny)
	var fires []time.Time
	for i := 0; i <= 33; i++ {
		at := first.AddDate(0, 0, i)
		before := allGeneratedTasks(t, f.brain, []string{"p"}, auto.ID)
		f.tick(at.UTC())
		after := allGeneratedTasks(t, f.brain, []string{"p"}, auto.ID)
		if after == before+1 {
			fires = append(fires, at.Add(-30*time.Second))
		} else if after != before {
			t.Fatalf("day %d: %d new tasks, want 0 or 1", i, after-before)
		}
		if i%4 != 0 && after != before {
			t.Errorf("day %d is not an anchored day but fired", i)
		}
	}

	wantDays := []int{0, 4, 8, 12, 16, 20, 24, 28, 32}
	if len(fires) != len(wantDays) {
		t.Fatalf("fired on %d days, want %d", len(fires), len(wantDays))
	}
	for i, day := range wantDays {
		want := first.AddDate(0, 0, day).Add(-30 * time.Second)
		if !fires[i].Equal(want) {
			t.Errorf("fire %d at %s, want the local 03:00 slot %s", i, fires[i].UTC(), want.UTC())
		}
	}
	// The slot after the fallback is 03:00 EST, which is 08:00 UTC.
	last := first.AddDate(0, 0, 32)
	if got := last.UTC().Format(time.RFC3339); got != "2026-11-02T08:00:30Z" {
		t.Fatalf("test setup: last tick is %s, want 2026-11-02T08:00:30Z (03:00:30 EST)", got)
	}
}

// ---------------------------------------------------------------------------
// Downtime, catch-up and upgrade
// ---------------------------------------------------------------------------

func TestSlot_DowntimeCatchUpFiresTheLatestMissedSlotOncePerProject(t *testing.T) {
	projects := []string{"a", "b", "c"}
	f := newSlotFixture(t, slotUTC(2026, 10, 7, 0, 0, 0), projects...)
	auto := f.save(slotAutomation{global: true, trigger: types.TriggerConfig{
		Schedule: "0 3 * * *",
		Filter:   map[string]string{"project": "*"},
	}})
	f.tick(slotUTC(2026, 10, 8, 3, 0, 30))
	if n := allGeneratedTasks(t, f.brain, projects, auto.ID); n != 3 {
		t.Fatalf("day one: %d tasks, want 3", n)
	}

	// The server is down until 10:00 the next day. A restarted evaluator must
	// find the last handled slot in the run audits, not replay the whole gap.
	f.svc = f.newService(projects...)
	missed := slotUTC(2026, 10, 9, 3, 0, 0)

	f.tick(slotUTC(2026, 10, 9, 10, 0, 0))
	if n := len(slotTasks(t, f.brain, projects, auto.ID, missed)); n != 1 {
		t.Fatalf("first tick after downtime: %d catch-up tasks, want exactly 1", n)
	}
	f.tick(slotUTC(2026, 10, 9, 10, 1, 0))
	if n := len(slotTasks(t, f.brain, projects, auto.ID, missed)); n != 2 {
		t.Fatalf("second tick: %d catch-up tasks, want 2", n)
	}
	f.tick(slotUTC(2026, 10, 9, 10, 2, 0))
	f.tick(slotUTC(2026, 10, 9, 10, 3, 0))
	if n := len(slotTasks(t, f.brain, projects, auto.ID, missed)); n != 3 {
		t.Fatalf("after three ticks: %d catch-up tasks, want 3 (one per project)", n)
	}
	if n := allGeneratedTasks(t, f.brain, projects, auto.ID); n != 6 {
		t.Fatalf("total tasks = %d, want 6; no slot may fire twice", n)
	}
}

func TestSlot_CatchUpCapLetsOnTimeRunsThrough(t *testing.T) {
	const window = 2 * time.Hour
	candidates := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		candidates = append(candidates, fmt.Sprintf("p%02d", i))
	}

	f := newSlotFixture(t, slotUTC(2026, 10, 7, 0, 0, 0))
	auto := f.save(slotAutomation{global: true, trigger: types.TriggerConfig{
		Schedule: "0 3 * * *",
		Stagger:  "2h",
		Filter:   map[string]string{"project": "*"},
	}})

	// Pick three projects by their stable offsets: B fires on time at T, and A
	// and C are at least two minutes behind T, so both are catch-ups.
	sort.Slice(candidates, func(i, j int) bool {
		return schedule.StaggerOffset(auto.ID, candidates[i], window) <
			schedule.StaggerOffset(auto.ID, candidates[j], window)
	})
	offset := func(p string) time.Duration { return schedule.StaggerOffset(auto.ID, p, window) }
	late, early := candidates[0], candidates[1]
	onTime := candidates[len(candidates)-1]
	if offset(onTime)-offset(early) < 2*time.Minute {
		t.Fatalf("test setup: offsets %s/%s too close", early, onTime)
	}
	// List order decides which catch-up the cap admits first: late, then
	// early (deferred), then onTime (on time, never capped).
	projects := []string{late, early, onTime}
	f.svc = f.newService(projects...)

	f.tick(slotUTC(2026, 10, 8, 12, 0, 0)) // baseline: no slot is replayed
	if n := allGeneratedTasks(t, f.brain, projects, auto.ID); n != 0 {
		t.Fatalf("baseline tick created %d tasks, want 0", n)
	}

	at := slotUTC(2026, 10, 9, 3, 0, 0).Add(offset(onTime) + 30*time.Second)
	f.tick(at)
	if n := len(slotTasks(t, f.brain, []string{onTime}, auto.ID, slotUTC(2026, 10, 9, 3, 0, 0).Add(offset(onTime)))); n != 1 {
		t.Fatalf("on-time project %s: %d tasks, want 1", onTime, n)
	}
	if n := allGeneratedTasks(t, f.brain, projects, auto.ID); n != 2 {
		t.Fatalf("one tick with two late and one on-time project: %d tasks, want 2 (one catch-up plus the on-time run)", n)
	}

	f.tick(at.Add(time.Minute))
	if n := allGeneratedTasks(t, f.brain, projects, auto.ID); n != 3 {
		t.Fatalf("next tick: %d tasks, want 3 (the deferred catch-up)", n)
	}
	f.tick(at.Add(2 * time.Minute))
	if n := allGeneratedTasks(t, f.brain, projects, auto.ID); n != 3 {
		t.Fatalf("after all three handled: %d tasks, want 3", n)
	}
}

func TestSlot_CatchUpNoneNeverFiresALateSlot(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 0, 0, 0), "p")
	auto := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{
		Schedule: "0 3 * * *",
		CatchUp:  "none",
	}})
	f.tick(slotUTC(2026, 10, 8, 12, 0, 0))
	f.tick(slotUTC(2026, 10, 9, 10, 0, 0)) // the 03:00 slot is seven hours late
	if n := len(generatedTasksFor(t, f.brain, "p", auto.ID)); n != 0 {
		t.Fatalf("late slot with catch_up none: %d tasks, want 0", n)
	}
	f.tick(slotUTC(2026, 10, 10, 3, 0, 20))
	if n := len(generatedTasksFor(t, f.brain, "p", auto.ID)); n != 1 {
		t.Fatalf("next on-time slot: %d tasks, want 1", n)
	}
}

// An upgrade leaves automations with run audits that predate scheduled_for.
// The first evaluation must baseline at now, not replay the history.
func TestSlot_FirstSightBaselineNeverReplaysHistory(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 1, 0, 0, 0), "p")
	auto := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{Schedule: "0 3 * * *"}})

	// A legacy audit from before this change: tagged, but with no scheduled_for.
	_, err := f.brain.Save(context.Background(), types.CreateEntryRequest{
		Type:    "automation_run",
		Title:   "Automation Run: " + auto.ID,
		Content: "## Automation Run Audit\n\nautomation_id: " + auto.ID + "\nproject: p\n",
		Tags:    []string{"automation:" + auto.ID},
		Status:  "queued",
		Project: "p",
	})
	if err != nil {
		t.Fatalf("save legacy audit: %v", err)
	}

	f.svc = f.newService("p")
	f.tick(slotUTC(2026, 10, 9, 10, 0, 0))
	if n := len(generatedTasksFor(t, f.brain, "p", auto.ID)); n != 0 {
		t.Fatalf("upgrade baseline replayed a slot: %d tasks, want 0", n)
	}
	f.tick(slotUTC(2026, 10, 10, 3, 0, 20))
	if n := len(generatedTasksFor(t, f.brain, "p", auto.ID)); n != 1 {
		t.Fatalf("first real slot after upgrade: %d tasks, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// Pause, skips and limits
// ---------------------------------------------------------------------------

func TestSlot_NoBurstAfterUnpause(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 0, 0, 0), "p")
	auto := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{Schedule: "0 * * * *"}})
	paused := &fakeAutomationPauseChecker{paused: true}
	f.svc.SetPauseChecker(paused)

	// Five hours of minute ticks while paused: each hourly slot is handled
	// once, as a paused skip.
	for at := slotUTC(2026, 10, 8, 9, 0, 0); at.Before(slotUTC(2026, 10, 8, 14, 0, 0)); at = at.Add(time.Minute) {
		f.tick(at)
	}
	skips := auditsSkippedFor(t, f.brain, auto.ID, "paused")
	if len(skips) != 5 {
		t.Fatalf("paused skips = %d, want one per hourly slot (5)", len(skips))
	}
	if n := allGeneratedTasks(t, f.brain, []string{"p"}, auto.ID); n != 0 {
		t.Fatalf("paused: %d tasks, want 0", n)
	}

	paused.paused = false
	f.tick(slotUTC(2026, 10, 8, 14, 0, 0))
	for at := slotUTC(2026, 10, 8, 14, 1, 0); at.Before(slotUTC(2026, 10, 8, 15, 0, 0)); at = at.Add(time.Minute) {
		f.tick(at)
	}
	if n := allGeneratedTasks(t, f.brain, []string{"p"}, auto.ID); n != 1 {
		t.Fatalf("after unpause: %d tasks, want 1; the paused slots must not burst", n)
	}
}

func TestSlot_SkippedSlotsAreRecordedAsHandled(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 9, 30, 0), "p")
	auto := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{
		Schedule:      "0 * * * *",
		MaxConcurrent: 1,
	}})

	f.tick(slotUTC(2026, 10, 8, 10, 0, 0))
	if n := len(generatedTasksFor(t, f.brain, "p", auto.ID)); n != 1 {
		t.Fatalf("10:00 tasks = %d, want 1", n)
	}

	// The 11:00 slot finds the 10:00 task still pending, so it is skipped.
	f.tick(slotUTC(2026, 10, 8, 11, 0, 0))
	skips := auditsSkippedFor(t, f.brain, auto.ID, "max_concurrent")
	if len(skips) != 1 {
		t.Fatalf("max_concurrent skips = %d, want 1", len(skips))
	}
	wantScheduledFor(t, "skip audit", skips[0], slotUTC(2026, 10, 8, 11, 0, 0))

	// Finish the first task. The 11:00 slot was handled by its skip, so a later
	// tick must not run it late.
	first := generatedTasksFor(t, f.brain, "p", auto.ID)[0]
	f.setNow(slotUTC(2026, 10, 8, 11, 30, 0))
	completed := "completed"
	if _, err := f.brain.Update(context.Background(), first.Path, types.UpdateEntryRequest{Status: &completed}); err != nil {
		t.Fatalf("complete task: %v", err)
	}
	f.tick(slotUTC(2026, 10, 8, 11, 31, 0))
	if n := len(generatedTasksFor(t, f.brain, "p", auto.ID)); n != 1 {
		t.Fatalf("after the skipped slot was handled: %d tasks, want 1 (no late 11:00 run)", n)
	}

	f.tick(slotUTC(2026, 10, 8, 12, 0, 0))
	if n := len(generatedTasksFor(t, f.brain, "p", auto.ID)); n != 2 {
		t.Fatalf("12:00 slot: %d tasks, want 2", n)
	}
}

// max_runs stops one global automation for one project. The first skip for
// the exhausted project is recorded with its slot; later slots stay silent,
// as they always have.
func TestSlot_MaxRunsSkipRecordsItsSlotOnce(t *testing.T) {
	one := 1
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 9, 30, 0), "p")
	auto := f.save(slotAutomation{global: true, maxRuns: &one, trigger: types.TriggerConfig{
		Schedule: "0 * * * *",
		Filter:   map[string]string{"project": "*"},
	}})

	f.tick(slotUTC(2026, 10, 8, 10, 0, 0))
	f.tick(slotUTC(2026, 10, 8, 11, 0, 0))
	f.tick(slotUTC(2026, 10, 8, 12, 0, 0))

	if n := allGeneratedTasks(t, f.brain, []string{"p"}, auto.ID); n != 1 {
		t.Fatalf("tasks = %d, want 1 (max_runs)", n)
	}
	skips := auditsSkippedFor(t, f.brain, auto.ID, "max_runs")
	if len(skips) != 1 {
		t.Fatalf("max_runs skip audits = %d, want 1", len(skips))
	}
	wantScheduledFor(t, "max_runs skip", skips[0], slotUTC(2026, 10, 8, 11, 0, 0))
}

// ---------------------------------------------------------------------------
// Edits, creation and races
// ---------------------------------------------------------------------------

func TestSlot_CreatingOrEditingASchedulePastSlotNeverFires(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 10, 0, 0), "p")
	auto := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{Schedule: "0 9 * * *"}})

	// Created at 10:00, so today's 09:00 slot predates the entry.
	f.tick(slotUTC(2026, 10, 8, 10, 1, 0))
	f.tick(slotUTC(2026, 10, 8, 10, 30, 0))
	if n := len(generatedTasksFor(t, f.brain, "p", auto.ID)); n != 0 {
		t.Fatalf("slot before creation fired: %d tasks, want 0", n)
	}

	// Edit at 12:00 to 11:00. The new 11:00 slot is today, but the edit is
	// newer than it, so it must not fire.
	f.setNow(slotUTC(2026, 10, 8, 12, 0, 0))
	edited := types.TriggerConfig{Type: "cron", Schedule: "0 11 * * *"}
	if _, err := f.brain.Update(context.Background(), auto.Path, types.UpdateEntryRequest{Trigger: &edited}); err != nil {
		t.Fatalf("edit schedule: %v", err)
	}
	stampModified(t, f.brain, auto.Path, f.now)
	f.tick(slotUTC(2026, 10, 8, 12, 1, 0))
	if n := len(generatedTasksFor(t, f.brain, "p", auto.ID)); n != 0 {
		t.Fatalf("slot before the edit fired: %d tasks, want 0", n)
	}

	f.tick(slotUTC(2026, 10, 9, 11, 0, 20))
	if n := len(generatedTasksFor(t, f.brain, "p", auto.ID)); n != 1 {
		t.Fatalf("first slot of the edited schedule: %d tasks, want 1", n)
	}
}

func TestSlot_DedupKeyStopsTwoEvaluatorsFromFiringOneSlot(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 0, 0, 0), "p")
	auto := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{Schedule: "0 3 * * *"}})
	ctx := context.Background()

	at := slotUTC(2026, 10, 8, 3, 0, 0)
	f.setNow(at)
	first, second := f.newService("p"), f.newService("p")
	if err := first.CheckScheduled(ctx, at); err != nil {
		t.Fatalf("first evaluator: %v", err)
	}
	if n := len(generatedTasksFor(t, f.brain, "p", auto.ID)); n != 1 {
		t.Fatalf("first evaluator: %d tasks, want 1", n)
	}

	// A second evaluator that read the slot as unhandled before the first one
	// wrote its audit reaches the same firing. The dedup key must stop it.
	entry, err := f.brain.Recall(ctx, auto.ID)
	if err != nil {
		t.Fatalf("recall automation: %v", err)
	}
	if _, err := second.createTask(ctx, *entry, types.Event{ProjectID: "p"}, scheduledDedupKey(auto.ID, "p", at), at); err != nil {
		t.Fatalf("second firing of the same slot: %v", err)
	}
	if n := len(generatedTasksFor(t, f.brain, "p", auto.ID)); n != 1 {
		t.Fatalf("two firings of one slot: %d tasks, want 1", n)
	}
	if n := len(auditsSkippedFor(t, f.brain, auto.ID, "dedup")); n != 1 {
		t.Fatalf("dedup audits = %d, want 1", n)
	}
}

func TestSlot_ConcurrentTicksFireASlotOnce(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 0, 0, 0), "p")
	auto := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{Schedule: "0 3 * * *"}})

	at := slotUTC(2026, 10, 8, 3, 0, 0)
	f.setNow(at)
	svc := f.newService("p")
	ctx := context.Background()

	const ticks = 8
	errs := make(chan error, ticks)
	var wg sync.WaitGroup
	for i := 0; i < ticks; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- svc.CheckScheduled(ctx, at)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent tick: %v", err)
		}
	}
	if n := len(generatedTasksFor(t, f.brain, "p", auto.ID)); n != 1 {
		t.Fatalf("%d concurrent ticks on one slot: %d tasks, want 1", ticks, n)
	}
}

// Manual runs are explicit overrides. They must not mark a slot handled, so
// the scheduled slot still fires on its own tick.
func TestSlot_ManualRunsNeverMarkASlotHandled(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 0, 0, 0), "p")
	auto := f.save(slotAutomation{project: "p", trigger: types.TriggerConfig{Schedule: "0 3 * * *"}})

	at := slotUTC(2026, 10, 8, 3, 0, 20)
	f.setNow(at)
	if _, err := f.svc.RunAutomationNow(context.Background(), auto.Path, ""); err != nil {
		t.Fatalf("manual run: %v", err)
	}
	f.tick(at)

	tasks := generatedTasksFor(t, f.brain, "p", auto.ID)
	if len(tasks) != 2 {
		t.Fatalf("manual run plus the slot: %d tasks, want 2", len(tasks))
	}
	if n := len(slotTasks(t, f.brain, []string{"p"}, auto.ID, slotUTC(2026, 10, 8, 3, 0, 0))); n != 1 {
		t.Fatalf("scheduled slot tasks = %d, want 1", n)
	}
}

// Ticks, manual runs and audit reads share one evaluator. Run under -race.
func TestSlot_TicksManualRunsAndReadsRunTogetherSafely(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 0, 0, 0), "p", "q")
	auto := f.save(slotAutomation{global: true, trigger: types.TriggerConfig{
		Schedule: "0 3 * * *",
		Filter:   map[string]string{"project": "*"},
	}})

	at := slotUTC(2026, 10, 8, 3, 0, 0)
	f.setNow(at)
	svc := f.svc
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make(chan error, 60)
	for i := 0; i < 20; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); errs <- svc.CheckScheduled(ctx, at) }()
		go func() {
			defer wg.Done()
			_, err := svc.RunAutomationNow(ctx, auto.Path, "p")
			errs <- err
		}()
		go func() {
			defer wg.Done()
			_, err := svc.listRunAudits(ctx, "p", auto.ID, 10)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent operation: %v", err)
		}
	}
	if n := len(slotTasks(t, f.brain, []string{"p", "q"}, auto.ID, at)); n != 2 {
		t.Fatalf("scheduled slot tasks = %d, want exactly one per project (2)", n)
	}
}

// An update action is applied in process on the event path, where it acts on
// the event's feature. A cron slot has no feature, so the slot must be recorded
// as a skip with the same reason the event path gives, not queued as a task
// with an empty prompt.
func TestSlot_UpdateActionOnCronSlotIsSkippedNotQueued(t *testing.T) {
	f := newSlotFixture(t, slotUTC(2026, 10, 8, 0, 0, 0), "p")
	resp, err := f.brain.Save(context.Background(), types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Cron update",
		Content: "update action on a schedule",
		Status:  "active",
		Project: "p",
		Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 3 * * *"},
		Action:  &types.AutomationAction{Type: "update", SetStatus: "archived"},
	})
	if err != nil {
		t.Fatalf("Save automation: %v", err)
	}
	stampModified(t, f.brain, resp.Path, f.now)

	f.tick(slotUTC(2026, 10, 8, 3, 0, 0))

	if n := len(generatedTasksFor(t, f.brain, "p", resp.ID)); n != 0 {
		t.Fatalf("update action on a cron slot created %d tasks, want 0", n)
	}
	skips := auditsSkippedFor(t, f.brain, resp.ID, "update action fired on an event with no feature")
	if len(skips) != 1 {
		t.Fatalf("update-action skip audits = %d, want 1", len(skips))
	}
}
