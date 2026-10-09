package schedule

import (
	"context"
	"testing"
	"time"
)

var (
	newYork = mustLoad("America/New_York")
	london  = mustLoad("Europe/London")
)

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// local builds a wall-clock time in loc (time.Date semantics).
func local(loc *time.Location, y int, m time.Month, d, h, mi int) time.Time {
	return time.Date(y, m, d, h, mi, 0, 0, loc)
}

func mustCompile(t *testing.T, spec Spec) *Schedule {
	t.Helper()
	s, err := Compile(spec)
	if err != nil {
		t.Fatalf("Compile(%+v): %v", spec, err)
	}
	return s
}

func mustLatest(t *testing.T, s *Schedule, now time.Time, offset time.Duration) Slot {
	t.Helper()
	slot, ok, err := s.LatestSlot(context.Background(), now, offset)
	if err != nil || !ok {
		t.Fatalf("LatestSlot(%v, %v) = %v, %v, %v; want a slot", now, offset, slot, ok, err)
	}
	return slot
}

func mustNext(t *testing.T, s *Schedule, after time.Time, offset time.Duration) Slot {
	t.Helper()
	slot, ok, err := s.NextSlot(context.Background(), after, offset)
	if err != nil || !ok {
		t.Fatalf("NextSlot(%v, %v) = %v, %v, %v; want a slot", after, offset, slot, ok, err)
	}
	return slot
}

func assertSlot(t *testing.T, what string, got Slot, base time.Time, offset time.Duration) {
	t.Helper()
	if !got.Base.Equal(base) || !got.At.Equal(base.Add(offset)) {
		t.Errorf("%s = {Base: %v, At: %v}, want {Base: %v, At: %v}", what, got.Base, got.At, base, base.Add(offset))
	}
}

func TestCronSlots(t *testing.T) {
	ny := func(y int, m time.Month, d, h, mi int) time.Time { return local(newYork, y, m, d, h, mi) }
	tests := []struct {
		name       string
		cron       string
		offset     time.Duration
		now        time.Time
		latestBase time.Time
		nextBase   time.Time
	}{
		{"daily, mid-day", "0 3 * * *", 0, ny(2026, 3, 10, 12, 0), ny(2026, 3, 10, 3, 0), ny(2026, 3, 11, 3, 0)},
		{"now exactly at the slot", "0 3 * * *", 0, ny(2026, 3, 10, 3, 0), ny(2026, 3, 10, 3, 0), ny(2026, 3, 11, 3, 0)},
		{"a nanosecond before the slot", "0 3 * * *", 0, ny(2026, 3, 10, 3, 0).Add(-1), ny(2026, 3, 9, 3, 0), ny(2026, 3, 10, 3, 0)},
		{"stagger: base passed, At not yet", "0 3 * * *", 45 * time.Minute, ny(2026, 3, 10, 3, 30), ny(2026, 3, 9, 3, 0), ny(2026, 3, 10, 3, 0)},
		{"stagger: exactly at At", "0 3 * * *", 45 * time.Minute, ny(2026, 3, 10, 3, 45), ny(2026, 3, 10, 3, 0), ny(2026, 3, 11, 3, 0)},
		{"stagger crosses midnight", "30 23 * * *", 2 * time.Hour, ny(2026, 3, 11, 1, 0), ny(2026, 3, 9, 23, 30), ny(2026, 3, 10, 23, 30)},
		// Cron keeps its DST rules (addendum #7): a skipped wall time does
		// not fire; a repeated one fires once, at its first occurrence.
		{"spring-forward gap does not fire", "30 2 * * *", 0, ny(2026, 3, 8, 12, 0), ny(2026, 3, 7, 2, 30), ny(2026, 3, 9, 2, 30)},
		{"fall-back fires at first occurrence", "30 1 * * *", 0,
			time.Date(2026, 11, 1, 6, 45, 0, 0, time.UTC),  // 01:45 EST, second pass
			time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC),  // 01:30 EDT, first pass
			time.Date(2026, 11, 2, 6, 30, 0, 0, time.UTC)}, // 01:30 EST next day
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := mustCompile(t, Spec{Cron: tt.cron, Timezone: "America/New_York", Stagger: 3 * time.Hour})
			got := mustLatest(t, s, tt.now, tt.offset)
			assertSlot(t, "LatestSlot", got, tt.latestBase, tt.offset)
			if got.Base.Location().String() != "America/New_York" || got.At.Location().String() != "America/New_York" {
				t.Errorf("slot locations = %v/%v, want America/New_York", got.Base.Location(), got.At.Location())
			}
			assertSlot(t, "NextSlot", mustNext(t, s, tt.now, tt.offset), tt.nextBase, tt.offset)
		})
	}
}

func TestCronSlots_DefaultUTC(t *testing.T) {
	s := mustCompile(t, Spec{Cron: "0 0 * * *"})
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.FixedZone("X", 5*3600))
	assertSlot(t, "LatestSlot", mustLatest(t, s, now, 0), time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC), 0)
	assertSlot(t, "NextSlot", mustNext(t, s, now, 0), time.Date(2026, 3, 11, 0, 0, 0, 0, time.UTC), 0)
}

func TestCronSlots_ScheduleOffset(t *testing.T) {
	s := mustCompile(t, Spec{Cron: "0 3 * * *", Timezone: "America/New_York", Stagger: 2 * time.Hour})
	off := s.Offset("brain:builtin-dream-consolidation", "hindsight") // 49m23s
	got := mustLatest(t, s, local(newYork, 2026, 3, 10, 12, 0), off)
	assertSlot(t, "LatestSlot", got, local(newYork, 2026, 3, 10, 3, 0), 49*time.Minute+23*time.Second)
}

func TestCronSlots_Impossible(t *testing.T) {
	s := mustCompile(t, Spec{Cron: "0 0 30 2 *"})
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	if slot, ok, err := s.LatestSlot(context.Background(), now, 0); ok || err != nil {
		t.Errorf("LatestSlot = %v, %v, %v; want no slot, no error", slot, ok, err)
	}
	if slot, ok, err := s.NextSlot(context.Background(), now, 0); ok || err != nil {
		t.Errorf("NextSlot = %v, %v, %v; want no slot, no error", slot, ok, err)
	}
}
