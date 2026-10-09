package cron

import (
	"testing"
	"time"
)

// DST reference instants (2026):
//
//	America/New_York  spring forward Sun 03-08 02:00 EST → 03:00 EDT (02:xx never happens)
//	                  fall back      Sun 11-01 02:00 EDT → 01:00 EST (01:xx happens twice)
//	Europe/London     spring forward Sun 03-29 01:00 GMT → 02:00 BST (01:xx never happens)
//	                  fall back      Sun 10-25 02:00 BST → 01:00 GMT (01:xx happens twice)
//
// Decision #7 of the automation scheduling addendum: for cron a nonexistent
// local time does not fire, and a repeated local time fires once, at its
// FIRST occurrence. Go's time.Date does not promise which occurrence it
// returns for a repeated wall time — it picks the first in New York and the
// second in London — so London is where a naive implementation goes wrong.

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", name, err)
	}
	return loc
}

func utc(y int, m time.Month, d, h, mi int) time.Time {
	return time.Date(y, m, d, h, mi, 0, 0, time.UTC)
}

type fallBack struct {
	zone        string
	first01_30  time.Time // first occurrence of local 01:30 on the fall-back date
	second01_30 time.Time // second occurrence of local 01:30
	dayStart    time.Time // local midnight starting the fall-back date
}

func fallBacks(t *testing.T) []fallBack {
	return []fallBack{
		{"America/New_York", utc(2026, 11, 1, 5, 30), utc(2026, 11, 1, 6, 30), utc(2026, 11, 1, 4, 0)},
		{"Europe/London", utc(2026, 10, 25, 0, 30), utc(2026, 10, 25, 1, 30), utc(2026, 10, 24, 23, 0)},
	}
}

func TestMatches_RepeatedLocalTime_OnlyFirstOccurrence(t *testing.T) {
	s := mustParse(t, "30 1 * * *")
	for _, fb := range fallBacks(t) {
		loc := mustZone(t, fb.zone)
		first, second := fb.first01_30.In(loc), fb.second01_30.In(loc)
		if first.Hour() != 1 || second.Hour() != 1 || first.Minute() != 30 || second.Minute() != 30 {
			t.Fatalf("%s: fixture is not a repeated 01:30: %v / %v", fb.zone, first, second)
		}
		if !s.Matches(first) {
			t.Errorf("%s: Matches(first 01:30 %v) = false, want true", fb.zone, first)
		}
		if s.Matches(second) {
			t.Errorf("%s: Matches(second 01:30 %v) = true, want false (repeated wall time fires once)", fb.zone, second)
		}
	}
}

func TestNextAfter_FallBack_ResolvesToFirstOccurrence(t *testing.T) {
	s := mustParse(t, "30 1 * * *")
	for _, fb := range fallBacks(t) {
		loc := mustZone(t, fb.zone)
		got := s.NextAfter(fb.dayStart.In(loc))
		if !got.Equal(fb.first01_30) {
			t.Errorf("%s: NextAfter(local midnight) = %v (%v UTC), want first occurrence %v",
				fb.zone, got, got.UTC(), fb.first01_30.In(loc))
		}
		if got.Location() != loc {
			t.Errorf("%s: NextAfter returned location %v, want %v", fb.zone, got.Location(), loc)
		}
	}
}

// Starting inside the second pass of the repeated hour, the first 01:30 is
// already in the past and the second must not fire: the next run is the
// following day's 01:30.
func TestNextAfter_FromSecondPass_SkipsRepeatedSlot(t *testing.T) {
	s := mustParse(t, "30 1 * * *")
	for _, fb := range fallBacks(t) {
		loc := mustZone(t, fb.zone)
		from := fb.second01_30.Add(-20 * time.Minute).In(loc) // second-pass 01:10
		got := s.NextAfter(from)
		next := fb.first01_30.In(loc).AddDate(0, 0, 1)
		want := time.Date(next.Year(), next.Month(), next.Day(), 1, 30, 0, 0, loc)
		if !got.Equal(want) {
			t.Errorf("%s: NextAfter(second-pass 01:10 %v) = %v, want %v", fb.zone, from, got, want)
		}
	}
}

func TestNextAfter_Every15_SkipsSecondPassOfRepeatedHour(t *testing.T) {
	s := mustParse(t, "*/15 * * * *")
	for _, fb := range fallBacks(t) {
		loc := mustZone(t, fb.zone)
		from := fb.first01_30.Add(20 * time.Minute).In(loc) // first-pass 01:50
		got := s.NextAfter(from)
		// The next wall slot not already used is 02:00, which comes after the
		// whole second pass of 01:xx.
		want := fb.second01_30.Add(30 * time.Minute)
		if !got.Equal(want) {
			t.Errorf("%s: NextAfter(first-pass 01:50) = %v, want 02:00 %v", fb.zone, got, want.In(loc))
		}
	}
}

func TestNextAfter_SpringForward_NonexistentTimeDoesNotFire(t *testing.T) {
	tests := []struct {
		zone string
		expr string
		from time.Time
		want time.Time
	}{
		// 02:30 does not exist on 03-08 in New York: next is 03-09 02:30 EDT.
		{"America/New_York", "30 2 * * *", utc(2026, 3, 8, 5, 0), utc(2026, 3, 9, 6, 30)},
		// 01:30 does not exist on 03-29 in London: next is 03-30 01:30 BST.
		{"Europe/London", "30 1 * * *", utc(2026, 3, 29, 0, 0), utc(2026, 3, 30, 0, 30)},
	}
	for _, tt := range tests {
		loc := mustZone(t, tt.zone)
		s := mustParse(t, tt.expr)
		if got := s.NextAfter(tt.from.In(loc)); !got.Equal(tt.want) {
			t.Errorf("%s %q: NextAfter(%v) = %v, want %v", tt.zone, tt.expr, tt.from.In(loc), got, tt.want.In(loc))
		}
	}
}

// Over a whole transition day, Matches (probed at every real minute) and a
// NextAfter chain must agree on the run count: a 23-hour day has 92
// quarter-hour runs, a 25-hour day still has only 96 distinct wall slots.
func TestTransitionDays_MatchesAndNextAfterAgree(t *testing.T) {
	s := mustParse(t, "*/15 * * * *")
	tests := []struct {
		zone       string
		start, end time.Time // UTC bounds of the local calendar day
		want       int
	}{
		{"America/New_York", utc(2026, 3, 8, 5, 0), utc(2026, 3, 9, 4, 0), 92},
		{"America/New_York", utc(2026, 11, 1, 4, 0), utc(2026, 11, 2, 5, 0), 96},
		{"Europe/London", utc(2026, 3, 29, 0, 0), utc(2026, 3, 29, 23, 0), 92},
		{"Europe/London", utc(2026, 10, 24, 23, 0), utc(2026, 10, 26, 0, 0), 96},
	}
	for _, tt := range tests {
		loc := mustZone(t, tt.zone)

		probed := 0
		for c := tt.start; c.Before(tt.end); c = c.Add(time.Minute) {
			if s.Matches(c.In(loc)) {
				probed++
			}
		}
		chained := 0
		for c := s.NextAfter(tt.start.Add(-time.Minute).In(loc)); c.Before(tt.end); c = s.NextAfter(c) {
			if c.IsZero() {
				t.Fatalf("%s: NextAfter chain hit the zero time", tt.zone)
			}
			chained++
		}
		if probed != tt.want || chained != tt.want {
			t.Errorf("%s day %s: Matches count = %d, NextAfter chain = %d, want %d",
				tt.zone, tt.start.In(loc).Format("2006-01-02"), probed, chained, tt.want)
		}
	}
}
