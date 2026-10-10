package schedule

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// recorder is a DayFilter that logs every day it is asked about and
// answers with allow.
type recorder struct {
	mu    sync.Mutex
	days  []time.Time
	allow func(day time.Time) bool
}

func (r *recorder) Allowed(_ context.Context, day time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.days = append(r.days, day)
	return r.allow(day), nil
}

func (r *recorder) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.days)
}

// notOn rejects the given local dates (compared by Date() in the day's own
// location, which is the schedule's).
func notOn(dates ...time.Time) DayFilter {
	return DayFilterFunc(func(_ context.Context, day time.Time) (bool, error) {
		y, m, d := day.Date()
		for _, x := range dates {
			xy, xm, xd := x.Date()
			if y == xy && m == xm && d == xd {
				return false, nil
			}
		}
		return true, nil
	})
}

// 2026-01-05 is a Monday, so 2026-01-09 is a Friday and 01-10/01-11 the
// weekend.
func TestDayFilter_CronSkipsWeekend(t *testing.T) {
	s := mustCompile(t, Spec{Cron: "0 9 * * *", Timezone: "America/New_York", DayFilters: []DayFilter{weekdaysOnly}})
	sunday := local(newYork, 2026, 1, 11, 12, 0)
	assertSlot(t, "LatestSlot", mustLatest(t, s, sunday, 0), local(newYork, 2026, 1, 9, 9, 0), 0)
	assertSlot(t, "NextSlot", mustNext(t, s, sunday, 0), local(newYork, 2026, 1, 12, 9, 0), 0)
}

// A rejected day's slot is dropped, not moved: every 4d keeps its grid.
func TestDayFilter_SkipsWithoutShifting(t *testing.T) {
	s := mustCompile(t, Spec{Every: "4d", At: "09:00", Anchor: utc(2026, 1, 1, 0, 0),
		DayFilters: []DayFilter{notOn(utc(2026, 1, 5, 0, 0))}})
	// Slots: Jan 1, (Jan 5 rejected), Jan 9, Jan 13.
	assertSlot(t, "LatestSlot", mustLatest(t, s, utc(2026, 1, 8, 12, 0), 0), utc(2026, 1, 1, 9, 0), 0)
	assertSlot(t, "NextSlot", mustNext(t, s, utc(2026, 1, 1, 9, 0), 0), utc(2026, 1, 9, 9, 0), 0)
}

func TestDayFilter_SubDayGridKept(t *testing.T) {
	// The anchor is a Monday midnight a week earlier, so Friday 22:30 and
	// the next Monday 00:00 both lie on the 90-minute grid.
	s := mustCompile(t, Spec{Every: "90m", Anchor: utc(2026, 1, 5, 0, 0), DayFilters: []DayFilter{weekdaysOnly}})
	sunday := utc(2026, 1, 11, 12, 0)
	assertSlot(t, "LatestSlot", mustLatest(t, s, sunday, 0), utc(2026, 1, 9, 22, 30), 0)
	assertSlot(t, "NextSlot", mustNext(t, s, sunday, 0), utc(2026, 1, 12, 0, 0), 0)
}

func TestDayFilter_AllFiltersMustAllow(t *testing.T) {
	s := mustCompile(t, Spec{Cron: "0 9 * * *", Timezone: "America/New_York",
		DayFilters: []DayFilter{weekdaysOnly, notOn(local(newYork, 2026, 1, 12, 0, 0))}})
	got := mustNext(t, s, local(newYork, 2026, 1, 9, 9, 0), 0)
	assertSlot(t, "NextSlot", got, local(newYork, 2026, 1, 13, 9, 0), 0)
}

// Addendum #8: the filter judges the base slot's local date, before the
// stagger offset, and is handed that date's local midnight.
func TestDayFilter_JudgesBaseDateAtLocalMidnight(t *testing.T) {
	rec := &recorder{allow: func(day time.Time) bool {
		wd := day.Weekday()
		return wd != time.Saturday && wd != time.Sunday
	}}
	s := mustCompile(t, Spec{Cron: "30 23 * * *", Timezone: "America/New_York", Stagger: 3 * time.Hour, DayFilters: []DayFilter{rec}})
	off := 2 * time.Hour
	now := local(newYork, 2026, 1, 10, 2, 0) // Saturday
	// Friday 23:30 is due at Saturday 01:30: allowed, because its base is Friday.
	assertSlot(t, "LatestSlot", mustLatest(t, s, now, off), local(newYork, 2026, 1, 9, 23, 30), off)
	// Saturday's and Sunday's bases are rejected even though Sunday's At
	// (Monday 01:30) falls on a weekday.
	assertSlot(t, "NextSlot", mustNext(t, s, now, off), local(newYork, 2026, 1, 12, 23, 30), off)
	for _, day := range rec.days {
		if day.Location().String() != "America/New_York" {
			t.Errorf("filter day %v is not in the schedule's location", day)
		}
		if h, m, sec := day.Clock(); h != 0 || m != 0 || sec != 0 || day.Nanosecond() != 0 {
			t.Errorf("filter day %v is not local midnight", day)
		}
	}
	if len(rec.days) == 0 {
		t.Fatal("filter was never consulted")
	}
}

// When DST skips midnight the day starts at the first instant after it.
func TestDayFilter_SkippedMidnight(t *testing.T) {
	rec := &recorder{allow: func(time.Time) bool { return true }}
	s := mustCompile(t, Spec{Cron: "0 12 * * *", Timezone: "America/Santiago", DayFilters: []DayFilter{rec}})
	mustLatest(t, s, utc(2026, 9, 6, 18, 0), 0)
	if len(rec.days) != 1 {
		t.Fatalf("filter consulted %d times, want 1: %v", len(rec.days), rec.days)
	}
	if want := utc(2026, 9, 6, 4, 0); !rec.days[0].Equal(want) { // 01:00 -03, midnight -04 never happened
		t.Errorf("filter day = %v, want %v", rec.days[0], want)
	}
}

// Each candidate day is consulted once, not once per slot on it.
func TestDayFilter_OneCallPerDay(t *testing.T) {
	rec := &recorder{allow: func(day time.Time) bool {
		wd := day.Weekday()
		return wd != time.Saturday && wd != time.Sunday
	}}
	s := mustCompile(t, Spec{Cron: "*/5 * * * *", DayFilters: []DayFilter{rec}})
	assertSlot(t, "LatestSlot", mustLatest(t, s, utc(2026, 1, 11, 23, 59), 0), utc(2026, 1, 9, 23, 55), 0)
	if got := rec.calls(); got != 3 { // Sunday, Saturday, Friday
		t.Errorf("filter consulted %d times, want 3: %v", got, rec.days)
	}
}

func TestDayFilter_ErrorReturnedUnchanged(t *testing.T) {
	boom := errors.New("calendar unavailable")
	failing := DayFilterFunc(func(context.Context, time.Time) (bool, error) { return false, boom })
	s := mustCompile(t, Spec{Every: "1d", At: "09:00", Anchor: utc(2026, 1, 1, 0, 0), DayFilters: []DayFilter{failing}})
	now := utc(2026, 1, 10, 12, 0)
	if slot, ok, err := s.LatestSlot(context.Background(), now, 0); err != boom || ok {
		t.Errorf("LatestSlot = %v, %v, %v; want the filter's error", slot, ok, err)
	}
	if slot, ok, err := s.NextSlot(context.Background(), now, 0); err != boom || ok {
		t.Errorf("NextSlot = %v, %v, %v; want the filter's error", slot, ok, err)
	}
}

// A filter that rejects every day ends the search after a bounded number of
// days with "no slot", like an impossible cron expression. Each day is
// consulted at most once per search.
func TestDayFilter_BoundedSearch(t *testing.T) {
	specs := []Spec{
		{Cron: "* * * * *"},
		{Every: "1d", At: "09:00", Anchor: utc(2026, 1, 1, 0, 0)},
		{Every: "90m", Anchor: utc(2026, 1, 1, 0, 0)},
	}
	now := utc(2027, 6, 1, 12, 0)
	search := map[string]func(*Schedule) (Slot, bool, error){
		"LatestSlot": func(s *Schedule) (Slot, bool, error) { return s.LatestSlot(context.Background(), now, 0) },
		"NextSlot":   func(s *Schedule) (Slot, bool, error) { return s.NextSlot(context.Background(), now, 0) },
	}
	for _, spec := range specs {
		for name, run := range search {
			rec := &recorder{allow: func(time.Time) bool { return false }}
			spec.DayFilters = []DayFilter{rec}
			s := mustCompile(t, spec)
			start := time.Now()
			if slot, ok, err := run(s); ok || err != nil {
				t.Errorf("%s %+v = %v, %v, %v; want no slot, no error", name, spec, slot, ok, err)
			}
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Errorf("%s %+v: bounded search took %v", name, spec, elapsed)
			}
			if n := rec.calls(); n == 0 || n > maxRejectedDays {
				t.Errorf("%s %+v: filter consulted %d times, want a bounded search", name, spec, n)
			}
			seen := map[time.Time]bool{}
			for _, d := range rec.days {
				if seen[d] {
					t.Errorf("%s %+v: day %v consulted twice", name, spec, d)
					break
				}
				seen[d] = true
			}
		}
	}
}

func TestDayFilter_ContextCancelled(t *testing.T) {
	rec := &recorder{allow: func(time.Time) bool { return false }}
	s := mustCompile(t, Spec{Cron: "0 9 * * *", DayFilters: []DayFilter{rec}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok, err := s.LatestSlot(ctx, utc(2026, 1, 10, 12, 0), 0); !errors.Is(err, context.Canceled) || ok {
		t.Errorf("LatestSlot with a cancelled context = %v, %v; want context.Canceled", ok, err)
	}
	if _, ok, err := s.NextSlot(ctx, utc(2026, 1, 10, 12, 0), 0); !errors.Is(err, context.Canceled) || ok {
		t.Errorf("NextSlot with a cancelled context = %v, %v; want context.Canceled", ok, err)
	}
	if n := rec.calls(); n > 2 {
		t.Errorf("filter consulted %d times after cancellation", n)
	}
}
