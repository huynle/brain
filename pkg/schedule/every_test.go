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

// Sub-day intervals add absolute durations to the anchor: slot k is
// Anchor + k*N units for k >= 0, whatever the zone does.
func TestEverySubDay(t *testing.T) {
	anchor := utc(2026, 1, 5, 10, 0)
	tests := []struct {
		name       string
		spec       Spec
		now        time.Time
		latestBase time.Time
		nextBase   time.Time
	}{
		{"90m mid-interval", Spec{Every: "90m", Anchor: anchor},
			utc(2026, 1, 5, 13, 15), utc(2026, 1, 5, 13, 0), utc(2026, 1, 5, 14, 30)},
		{"90m exactly at a slot", Spec{Every: "90m", Anchor: anchor},
			utc(2026, 1, 5, 14, 30), utc(2026, 1, 5, 14, 30), utc(2026, 1, 5, 16, 0)},
		{"90m a nanosecond before a slot", Spec{Every: "90m", Anchor: anchor},
			utc(2026, 1, 5, 14, 30).Add(-1), utc(2026, 1, 5, 13, 0), utc(2026, 1, 5, 14, 30)},
		{"90m at the anchor", Spec{Every: "90m", Anchor: anchor},
			anchor, anchor, utc(2026, 1, 5, 11, 30)},
		{"90m days later", Spec{Every: "90m", Anchor: anchor},
			utc(2026, 1, 9, 0, 59), utc(2026, 1, 8, 23, 30), utc(2026, 1, 9, 1, 0)},
		{"6h", Spec{Every: "6h", Anchor: anchor},
			utc(2026, 1, 6, 3, 0), utc(2026, 1, 5, 22, 0), utc(2026, 1, 6, 4, 0)},
		{"anchor truncated to whole seconds", Spec{Every: "1m", Anchor: anchor.Add(1500 * time.Millisecond)},
			anchor.Add(90 * time.Second), anchor.Add(61 * time.Second), anchor.Add(121 * time.Second)},
		// Absolute stepping: across fall-back, every 1h shows 01:00 twice
		// in New York; across spring-forward it never shows 02:00.
		{"1h across fall-back", Spec{Every: "1h", Timezone: "America/New_York", Anchor: utc(2026, 10, 31, 0, 0)},
			utc(2026, 11, 1, 6, 20), utc(2026, 11, 1, 6, 0), utc(2026, 11, 1, 7, 0)},
		{"1h across spring-forward", Spec{Every: "1h", Timezone: "America/New_York", Anchor: utc(2026, 3, 7, 0, 0)},
			utc(2026, 3, 8, 7, 20), utc(2026, 3, 8, 7, 0), utc(2026, 3, 8, 8, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := mustCompile(t, tt.spec)
			assertSlot(t, "LatestSlot", mustLatest(t, s, tt.now, 0), tt.latestBase, 0)
			assertSlot(t, "NextSlot", mustNext(t, s, tt.now, 0), tt.nextBase, 0)
		})
	}
}

func TestEverySubDay_ConstantSpacingAcrossDST(t *testing.T) {
	for _, every := range []string{"1h", "90m", "7h"} {
		s := mustCompile(t, Spec{Every: every, Timezone: "America/New_York", Anchor: utc(2026, 3, 1, 0, 0)})
		iv, _ := ParseEvery(every)
		step := time.Duration(iv.N) * time.Minute
		if iv.Unit == Hour {
			step = time.Duration(iv.N) * time.Hour
		}
		slot := mustNext(t, s, utc(2026, 3, 1, 0, 0).Add(-1), 0)
		for i := 0; slot.Base.Before(utc(2026, 11, 10, 0, 0)); i++ {
			next := mustNext(t, s, slot.At, 0)
			if d := next.Base.Sub(slot.Base); d != step {
				t.Fatalf("every %s: slot %d at %v is %v after %v, want %v", every, i, next.Base, d, slot.Base, step)
			}
			if next.Base.Location().String() != "America/New_York" {
				t.Fatalf("every %s: slot location %v, want America/New_York", every, next.Base.Location())
			}
			slot = next
		}
	}
}

func TestEverySubDay_NothingBeforeAnchor(t *testing.T) {
	anchor := utc(2026, 1, 5, 10, 0)
	s := mustCompile(t, Spec{Every: "90m", Anchor: anchor})
	if slot, ok, err := s.LatestSlot(context.Background(), anchor.Add(-1), 0); ok || err != nil {
		t.Errorf("LatestSlot before the anchor = %v, %v, %v; want no slot", slot, ok, err)
	}
	assertSlot(t, "NextSlot", mustNext(t, s, utc(1990, 1, 1, 0, 0), 0), anchor, 0)
	// Centuries away the arithmetic must not overflow a time.Duration.
	far := utc(2400, 6, 1, 0, 0)
	got := mustLatest(t, s, far, 0)
	// Checked in Unix seconds: a time.Duration saturates near 292 years.
	if got.Base.After(far) || far.Unix()-got.Base.Unix() >= 90*60 || (got.Base.Unix()-anchor.Unix())%(90*60) != 0 {
		t.Errorf("LatestSlot(%v) = %v, not the slot grid's latest", far, got.Base)
	}
}

func TestEverySubDay_Stagger(t *testing.T) {
	anchor := utc(2026, 1, 5, 10, 0)
	s := mustCompile(t, Spec{Every: "90m", Anchor: anchor, Stagger: time.Hour})
	off := 20 * time.Minute
	// Base 13:00 is passed at 13:15 but its At (13:20) is not.
	assertSlot(t, "LatestSlot", mustLatest(t, s, utc(2026, 1, 5, 13, 15), off), utc(2026, 1, 5, 11, 30), off)
	assertSlot(t, "NextSlot", mustNext(t, s, utc(2026, 1, 5, 13, 15), off), utc(2026, 1, 5, 13, 0), off)
}
