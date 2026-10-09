package cron

import (
	"testing"
	"time"
)

func TestPrevAtOrBefore_UTC(t *testing.T) {
	d := func(y int, m time.Month, day, h, mi int) time.Time { return utc(y, m, day, h, mi) }
	tests := []struct {
		name string
		expr string
		from time.Time
		want time.Time
	}{
		{"earlier minute", "*/15 * * * *", d(2026, 1, 1, 10, 16).Add(30 * time.Second), d(2026, 1, 1, 10, 15)},
		{"inclusive of exact match", "*/15 * * * *", d(2026, 1, 1, 10, 15), d(2026, 1, 1, 10, 15)},
		{"seconds zeroed", "*/15 * * * *", d(2026, 1, 1, 10, 15).Add(59*time.Second + 999*time.Millisecond), d(2026, 1, 1, 10, 15)},
		{"previous day", "30 9 * * *", d(2026, 1, 2, 9, 0), d(2026, 1, 1, 9, 30)},
		{"start of month", "0 0 1 * *", d(2026, 3, 15, 12, 0), d(2026, 3, 1, 0, 0)},
		{"skips month without the 31st", "0 12 31 * *", d(2026, 4, 15, 0, 0), d(2026, 3, 31, 12, 0)},
		{"skips February entirely", "0 12 30,31 * *", d(2026, 3, 1, 0, 0), d(2026, 1, 31, 12, 0)},
		{"month end, common year", "59 23 * * *", d(2026, 3, 1, 0, 0), d(2026, 2, 28, 23, 59)},
		{"month end, leap year", "59 23 * * *", d(2028, 3, 1, 0, 0), d(2028, 2, 29, 23, 59)},
		{"leap day, years back", "0 0 29 2 *", d(2026, 6, 1, 0, 0), d(2024, 2, 29, 0, 0)},
		{"leap day, exact", "0 0 29 2 *", d(2028, 2, 29, 0, 0), d(2028, 2, 29, 0, 0)},
		{"leap day, 2100 is not leap", "0 0 29 2 *", d(2100, 3, 1, 0, 0), d(2096, 2, 29, 0, 0)},
		{"year boundary, exact", "0 0 1 1 *", d(2026, 1, 1, 0, 0), d(2026, 1, 1, 0, 0)},
		{"year boundary, a minute before", "0 0 1 1 *", d(2025, 12, 31, 23, 59), d(2025, 1, 1, 0, 0)},
		{"OR: Monday before the 1st", "0 3 1 * 1", d(2026, 4, 1, 2, 0), d(2026, 3, 30, 3, 0)},
		{"OR: the 1st, a Wednesday", "0 3 1 * 1", d(2026, 4, 2, 0, 0), d(2026, 4, 1, 3, 0)},
		{"AND: odd-day Monday", "0 3 */2 * 1", d(2026, 4, 12, 0, 0), d(2026, 3, 23, 3, 0)},
		// Feb 29 on a Sunday: 2004, 2032, 2060, 2088, then 2128 (2100 skips
		// its leap day) — a 40-year gap the search must still bridge.
		{"leap Sunday, 22 years back", "0 0 29 2 */7", d(2026, 1, 1, 0, 0), d(2004, 2, 29, 0, 0)},
		{"leap Sunday, 40-year gap", "0 0 29 2 */7", d(2127, 12, 31, 0, 0), d(2088, 2, 29, 0, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := mustParse(t, tt.expr)
			got := s.PrevAtOrBefore(tt.from)
			if !got.Equal(tt.want) {
				t.Errorf("%q PrevAtOrBefore(%v) = %v, want %v", tt.expr, tt.from, got, tt.want)
			}
		})
	}
}

// NextAfter must bridge the same 40-year leap-Sunday gap.
func TestNextAfter_LeapSunday40YearGap(t *testing.T) {
	s := mustParse(t, "0 0 29 2 */7")
	if got, want := s.NextAfter(utc(2089, 1, 1, 0, 0)), utc(2128, 2, 29, 0, 0); !got.Equal(want) {
		t.Errorf("NextAfter(2089-01-01) = %v, want %v", got, want)
	}
}

// Expressions that can never fire return the zero time in both directions,
// and quickly: a bounded search, not an unbounded walk.
func TestImpossibleExpressions_ReturnZero(t *testing.T) {
	from := utc(2026, 6, 15, 12, 0)
	for _, expr := range []string{"0 0 30 2 *", "0 0 31 2 *", "0 0 31 4,6,9,11 *", "* * 30-31 2 *"} {
		s := mustParse(t, expr)
		start := time.Now()
		if got := s.PrevAtOrBefore(from); !got.IsZero() {
			t.Errorf("%q PrevAtOrBefore = %v, want the zero time", expr, got)
		}
		if got := s.NextAfter(from); !got.IsZero() {
			t.Errorf("%q NextAfter = %v, want the zero time", expr, got)
		}
		if el := time.Since(start); el > 250*time.Millisecond {
			t.Errorf("%q: impossible-expression search took %v; the search must be bounded", expr, el)
		}
	}
}

func TestPrevAtOrBefore_DST(t *testing.T) {
	tests := []struct {
		name string
		zone string
		expr string
		from time.Time // UTC instant
		want time.Time // UTC instant
	}{
		// New York: 02:xx does not exist on 03-08; 01:xx repeats on 11-01
		// (first pass EDT = 05:xx UTC, second pass EST = 06:xx UTC).
		{"NY gap skipped", "America/New_York", "30 2 * * *", utc(2026, 3, 8, 16, 0), utc(2026, 3, 7, 7, 30)},
		{"NY gap hour, top", "America/New_York", "0 2 * * *", utc(2026, 3, 8, 7, 0), utc(2026, 3, 7, 7, 0)},
		{"NY repeat, from later that day", "America/New_York", "30 1 * * *", utc(2026, 11, 1, 17, 0), utc(2026, 11, 1, 5, 30)},
		{"NY repeat, from second pass", "America/New_York", "30 1 * * *", utc(2026, 11, 1, 6, 10), utc(2026, 11, 1, 5, 30)},
		{"NY every 15, from second pass", "America/New_York", "*/15 * * * *", utc(2026, 11, 1, 6, 10), utc(2026, 11, 1, 5, 45)},
		{"NY every 15, end of second pass", "America/New_York", "*/15 * * * *", utc(2026, 11, 1, 6, 59).Add(30 * time.Second), utc(2026, 11, 1, 5, 45)},
		{"NY every 15, after repeat", "America/New_York", "*/15 * * * *", utc(2026, 11, 1, 7, 0), utc(2026, 11, 1, 7, 0)},
		// London: 01:xx does not exist on 03-29; 01:xx repeats on 10-25
		// (first pass BST = 00:xx UTC, second pass GMT = 01:xx UTC).
		{"London gap skipped", "Europe/London", "30 1 * * *", utc(2026, 3, 29, 11, 0), utc(2026, 3, 28, 1, 30)},
		{"London repeat, from later that day", "Europe/London", "30 1 * * *", utc(2026, 10, 25, 12, 0), utc(2026, 10, 25, 0, 30)},
		{"London repeat, from second pass", "Europe/London", "30 1 * * *", utc(2026, 10, 25, 1, 10), utc(2026, 10, 25, 0, 30)},
		{"London every 15, from second pass", "Europe/London", "*/15 * * * *", utc(2026, 10, 25, 1, 10), utc(2026, 10, 25, 0, 45)},
		// Havana moves its clocks at midnight: 00:00 does not exist on
		// 03-08 and occurs twice on 11-01 (04:00 UTC CDT, then 05:00 UTC CST).
		{"Havana missing midnight", "America/Havana", "0 0 * * *", utc(2026, 3, 8, 12, 0), utc(2026, 3, 7, 5, 0)},
		{"Havana repeated midnight", "America/Havana", "0 0 * * *", utc(2026, 11, 1, 12, 0), utc(2026, 11, 1, 4, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loc := mustZone(t, tt.zone)
			s := mustParse(t, tt.expr)
			from := tt.from.In(loc)
			got := s.PrevAtOrBefore(from)
			if !got.Equal(tt.want) {
				t.Errorf("%q PrevAtOrBefore(%v) = %v, want %v", tt.expr, from, got, tt.want.In(loc))
			}
			if got.Location() != loc {
				t.Errorf("result location = %v, want %v", got.Location(), loc)
			}
		})
	}
}

// NextAfter agrees on Havana's repeated midnight: once, at the first pass.
func TestNextAfter_HavanaRepeatedMidnight(t *testing.T) {
	loc := mustZone(t, "America/Havana")
	s := mustParse(t, "0 0 * * *")
	got := s.NextAfter(utc(2026, 10, 31, 12, 0).In(loc))
	if want := utc(2026, 11, 1, 4, 0); !got.Equal(want) {
		t.Errorf("NextAfter = %v, want first-pass midnight %v", got, want.In(loc))
	}
	if again := s.NextAfter(got); !again.Equal(utc(2026, 11, 2, 5, 0)) {
		t.Errorf("NextAfter(first midnight) = %v, want next day's midnight (the repeat must not fire)", again)
	}
}
