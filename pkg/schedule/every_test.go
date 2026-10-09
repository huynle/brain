package schedule

import (
	"context"
	"testing"
	"time"
)

func utc(y int, m time.Month, d, h, mi int) time.Time {
	return time.Date(y, m, d, h, mi, 0, 0, time.UTC)
}

// every 4d at 03:00 in New York keeps 03:00 local through both DST
// transitions: the UTC instant moves, the wall clock does not.
func TestEvery4dAt0300_StaysLocalAcrossDST(t *testing.T) {
	anchor := local(newYork, 2026, 2, 27, 10, 0) // a Friday
	s := mustCompile(t, Spec{Every: "4d", At: "03:00", Timezone: "America/New_York", Anchor: anchor})

	pinned := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"before spring-forward (EST)", utc(2026, 3, 8, 0, 0), utc(2026, 3, 7, 8, 0)},
		{"after spring-forward (EDT)", utc(2026, 3, 12, 0, 0), utc(2026, 3, 11, 7, 0)},
		{"before fall-back (EDT)", utc(2026, 11, 1, 0, 0), utc(2026, 10, 29, 7, 0)},
		{"after fall-back (EST)", utc(2026, 11, 3, 0, 0), utc(2026, 11, 2, 8, 0)},
	}
	for _, p := range pinned {
		assertSlot(t, "LatestSlot "+p.name, mustLatest(t, s, p.now, 0), p.want, 0)
	}

	// Walk a full year of slots with NextSlot.
	prev := mustNext(t, s, anchor.AddDate(0, 0, -1), 0)
	assertSlot(t, "first slot", prev, local(newYork, 2026, 2, 27, 3, 0), 0)
	for i := 0; i < 100; i++ {
		next := mustNext(t, s, prev.At, 0)
		got := next.Base.In(newYork)
		if h, m, sec := got.Clock(); h != 3 || m != 0 || sec != 0 {
			t.Fatalf("slot %d at %v: local clock is not 03:00:00", i, got)
		}
		py, pm, pd := prev.Base.In(newYork).Date()
		if want := time.Date(py, pm, pd+4, 0, 0, 0, 0, time.UTC); got.Year() != want.Year() || got.YearDay() != want.YearDay() {
			t.Fatalf("slot %d at %v is not 4 calendar days after %v", i, got, prev.Base)
		}
		prev = next
	}
}

func TestEvery_CalendarAnchors(t *testing.T) {
	monday := local(newYork, 2026, 1, 5, 10, 0)
	tests := []struct {
		name       string
		spec       Spec
		now        time.Time
		latestBase time.Time
		nextBase   time.Time
	}{
		{"every 2d", Spec{Every: "2d", At: "09:00", Timezone: "America/New_York", Anchor: monday},
			local(newYork, 2026, 1, 8, 8, 59), local(newYork, 2026, 1, 7, 9, 0), local(newYork, 2026, 1, 9, 9, 0)},
		{"every 14d from a Monday is every other Monday", Spec{Every: "14d", At: "09:00", Timezone: "America/New_York", Anchor: monday},
			local(newYork, 2026, 3, 15, 12, 0), utc(2026, 3, 2, 14, 0), utc(2026, 3, 16, 13, 0)},
		{"every 2w equals every 14d", Spec{Every: "2w", At: "09:00", Timezone: "America/New_York", Anchor: monday},
			local(newYork, 2026, 3, 15, 12, 0), utc(2026, 3, 2, 14, 0), utc(2026, 3, 16, 13, 0)},
		{"slot on the anchor date before the anchor instant", Spec{Every: "1d", At: "03:00", Timezone: "America/New_York", Anchor: monday},
			local(newYork, 2026, 1, 5, 5, 0), local(newYork, 2026, 1, 5, 3, 0), local(newYork, 2026, 1, 6, 3, 0)},
		{"anchor date is taken in the schedule's zone", Spec{Every: "1d", At: "09:00", Timezone: "America/New_York", Anchor: utc(2026, 1, 5, 3, 0)},
			local(newYork, 2026, 1, 4, 10, 0), local(newYork, 2026, 1, 4, 9, 0), local(newYork, 2026, 1, 5, 9, 0)},
		{"month end", Spec{Every: "2d", At: "00:00", Anchor: utc(2026, 1, 30, 12, 0)},
			utc(2026, 2, 2, 0, 0), utc(2026, 2, 1, 0, 0), utc(2026, 2, 3, 0, 0)},
		{"leap day", Spec{Every: "1d", At: "00:00", Anchor: utc(2028, 2, 27, 0, 0)},
			utc(2028, 2, 29, 12, 0), utc(2028, 2, 29, 0, 0), utc(2028, 3, 1, 0, 0)},
		{"30d over a common February", Spec{Every: "30d", At: "06:00", Anchor: utc(2027, 1, 31, 0, 0)},
			utc(2027, 3, 2, 6, 0), utc(2027, 3, 2, 6, 0), utc(2027, 4, 1, 6, 0)},
		{"no at: anchor wall time, whole seconds, across DST", Spec{Every: "1d", Timezone: "America/New_York",
			Anchor: time.Date(2026, 3, 1, 14, 23, 17, 500_000_000, newYork)},
			local(newYork, 2026, 3, 9, 15, 0), time.Date(2026, 3, 9, 14, 23, 17, 0, newYork), time.Date(2026, 3, 10, 14, 23, 17, 0, newYork)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := mustCompile(t, tt.spec)
			assertSlot(t, "LatestSlot", mustLatest(t, s, tt.now, 0), tt.latestBase, 0)
			assertSlot(t, "NextSlot", mustNext(t, s, tt.now, 0), tt.nextBase, 0)
		})
	}
}

func TestEvery14d_IsAlwaysMonday(t *testing.T) {
	s := mustCompile(t, Spec{Every: "14d", At: "09:00", Timezone: "America/New_York", Anchor: local(newYork, 2026, 1, 5, 0, 0)})
	slot := mustNext(t, s, local(newYork, 2026, 1, 1, 0, 0), 0)
	for i := 0; i < 60; i++ {
		if wd := slot.Base.In(newYork).Weekday(); wd != time.Monday {
			t.Fatalf("slot %d at %v is a %v", i, slot.Base, wd)
		}
		slot = mustNext(t, s, slot.At, 0)
	}
}

func TestEvery_NoSlotBeforeAnchorDate(t *testing.T) {
	anchor := local(newYork, 2026, 1, 5, 10, 0)
	s := mustCompile(t, Spec{Every: "1d", At: "03:00", Timezone: "America/New_York", Anchor: anchor})
	before := local(newYork, 2026, 1, 4, 23, 59)
	if slot, ok, err := s.LatestSlot(context.Background(), before, 0); ok || err != nil {
		t.Errorf("LatestSlot before the anchor date = %v, %v, %v; want no slot", slot, ok, err)
	}
	assertSlot(t, "NextSlot", mustNext(t, s, utc(2000, 1, 1, 0, 0), 0), local(newYork, 2026, 1, 5, 3, 0), 0)
}

// Addendum #7: for every + at, a nonexistent local time fires at the first
// valid instant after it, and a repeated local time at its first
// occurrence. time.Date picks inconsistently across zones (earlier in New
// York, later in London, the second pass of a repeat in London, even the
// previous date in Santiago), so each direction is pinned in more than one
// zone.
func TestEveryAt_DST(t *testing.T) {
	tests := []struct {
		name string
		tz   string
		at   string
		day  time.Time // any instant on the transition date
		want time.Time
	}{
		{"New York gap 02:30 -> 03:00 EDT", "America/New_York", "02:30", utc(2026, 3, 8, 16, 0), utc(2026, 3, 8, 7, 0)},
		{"London gap 01:30 -> 02:00 BST", "Europe/London", "01:30", utc(2026, 3, 29, 12, 0), utc(2026, 3, 29, 1, 0)},
		{"Santiago midnight gap 00:00 -> 01:00", "America/Santiago", "00:00", utc(2026, 9, 6, 12, 0), utc(2026, 9, 6, 4, 0)},
		{"New York repeat 01:30 -> EDT pass", "America/New_York", "01:30", utc(2026, 11, 1, 16, 0), utc(2026, 11, 1, 5, 30)},
		{"London repeat 01:30 -> BST pass", "Europe/London", "01:30", utc(2026, 10, 25, 12, 0), utc(2026, 10, 25, 0, 30)},
		{"Santiago repeat 23:30 -> first pass", "America/Santiago", "23:30", utc(2026, 4, 5, 12, 0), utc(2026, 4, 5, 2, 30)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loc := mustLoad(tt.tz)
			anchor := tt.day.In(loc).AddDate(0, 0, -10)
			s := mustCompile(t, Spec{Every: "1d", At: tt.at, Timezone: tt.tz, Anchor: anchor})
			// The slot is found from both directions.
			assertSlot(t, "LatestSlot(slot)", mustLatest(t, s, tt.want, 0), tt.want, 0)
			assertSlot(t, "NextSlot(slot-1ns)", mustNext(t, s, tt.want.Add(-1), 0), tt.want, 0)
			before := mustLatest(t, s, tt.want.Add(-1), 0)
			if !before.Base.Before(tt.want.Add(-12 * time.Hour)) {
				t.Errorf("LatestSlot(slot-1ns) = %v, want the previous day's slot", before.Base)
			}
			after := mustNext(t, s, tt.want, 0)
			if !after.Base.After(tt.want.Add(12 * time.Hour)) {
				t.Errorf("NextSlot(slot) = %v, want the next day's slot", after.Base)
			}
		})
	}
}

func TestEvery_Stagger(t *testing.T) {
	s := mustCompile(t, Spec{Every: "1d", At: "23:30", Timezone: "America/New_York", Anchor: local(newYork, 2026, 1, 1, 0, 0), Stagger: 2 * time.Hour})
	got := mustLatest(t, s, local(newYork, 2026, 1, 10, 0, 59), 90*time.Minute)
	assertSlot(t, "LatestSlot", got, local(newYork, 2026, 1, 8, 23, 30), 90*time.Minute)
	got = mustNext(t, s, local(newYork, 2026, 1, 10, 0, 59), 90*time.Minute)
	assertSlot(t, "NextSlot", got, local(newYork, 2026, 1, 9, 23, 30), 90*time.Minute)
	// At == now counts as reached.
	got = mustLatest(t, s, local(newYork, 2026, 1, 10, 1, 0), 90*time.Minute)
	assertSlot(t, "LatestSlot(At)", got, local(newYork, 2026, 1, 9, 23, 30), 90*time.Minute)
}
