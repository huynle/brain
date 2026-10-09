package cron

import (
	"testing"
	"time"
)

// Reference dates (2026, UTC):
//   Mon 2026-03-23, Tue 2026-03-24, Wed 2026-03-25, Sun 2026-03-22,
//   Mon 2026-03-30, Wed 2026-04-01, Mon 2026-04-13, Mon 2026-06-01.

func at3(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 3, 0, 0, 0, time.UTC)
}

func mustParse(t *testing.T, expr string) *Schedule {
	t.Helper()
	s, err := Parse(expr)
	if err != nil {
		t.Fatalf("Parse(%q) returned error: %v", expr, err)
	}
	return s
}

// Vixie cron (and POSIX crontab(5)): when NEITHER day field starts with
// '*', a day qualifies if it matches day-of-month OR day-of-week. Decision
// #6 of the automation scheduling addendum adopts that rule.
func TestMatches_DayFields_BothRestricted_IsOR(t *testing.T) {
	tests := []struct {
		expr string
		at   time.Time
		want bool
	}{
		{"0 3 1 * 1", at3(2026, 3, 23), true},  // Monday, not the 1st
		{"0 3 1 * 1", at3(2026, 4, 1), true},   // the 1st, a Wednesday
		{"0 3 1 * 1", at3(2026, 6, 1), true},   // both
		{"0 3 1 * 1", at3(2026, 3, 24), false}, // neither
		{"0 3 1-7 * 1", at3(2026, 3, 23), true},
		{"0 3 15 * 7", at3(2026, 3, 22), true}, // 7 = Sunday, via DOW
		{"0 3 15 * 7", at3(2026, 3, 15), true}, // via DOM (a Sunday too, still one match)
		{"0 3 15 * 7", at3(2026, 3, 16), false},
	}
	for _, tt := range tests {
		s := mustParse(t, tt.expr)
		if got := s.Matches(tt.at); got != tt.want {
			t.Errorf("%q Matches(%s %s) = %v, want %v",
				tt.expr, tt.at.Format("2006-01-02"), tt.at.Weekday(), got, tt.want)
		}
	}
}

// When either field starts with '*' — including stepped forms like "*/2" —
// the fields AND together, exactly as before. "*/2" restricts to odd days
// but still counts as starred, which is the Vixie quirk the addendum keeps.
func TestMatches_DayFields_StarPrefixed_IsAND(t *testing.T) {
	tests := []struct {
		expr string
		at   time.Time
		want bool
	}{
		{"0 3 */2 * 1", at3(2026, 3, 23), true},  // odd day AND Monday
		{"0 3 */2 * 1", at3(2026, 3, 30), false}, // Monday, even day
		{"0 3 */2 * 1", at3(2026, 3, 25), false}, // odd day, Wednesday
		{"0 3 1 * */1", at3(2026, 4, 1), true},   // "*/1" DOW = every day, starred
		{"0 3 1 * */1", at3(2026, 3, 23), false}, // starred DOW → DOM decides
		{"0 3 * * 1", at3(2026, 3, 23), true},
		{"0 3 * * 1", at3(2026, 4, 1), false},
		{"0 3 1 * *", at3(2026, 4, 1), true},
		{"0 3 1 * *", at3(2026, 3, 23), false},
	}
	for _, tt := range tests {
		s := mustParse(t, tt.expr)
		if got := s.Matches(tt.at); got != tt.want {
			t.Errorf("%q Matches(%s %s) = %v, want %v",
				tt.expr, tt.at.Format("2006-01-02"), tt.at.Weekday(), got, tt.want)
		}
	}
}

// NextAfter must use the same predicate as Matches, or the search skips
// days Matches would accept.
func TestNextAfter_DayFields_BothRestricted_IsOR(t *testing.T) {
	s := mustParse(t, "0 3 1 * 1")

	// From Tuesday the 24th: Monday the 30th comes before the 1st.
	if got, want := s.NextAfter(at3(2026, 3, 24)), at3(2026, 3, 30); !got.Equal(want) {
		t.Errorf("NextAfter(Tue 03-24) = %v, want %v (next Monday)", got, want)
	}
	// From Monday the 30th after the run: the 1st (a Wednesday) is next.
	// Under AND semantics this would jump to Monday 2026-06-01.
	if got, want := s.NextAfter(at3(2026, 3, 30)), at3(2026, 4, 1); !got.Equal(want) {
		t.Errorf("NextAfter(Mon 03-30 03:00) = %v, want %v (the 1st)", got, want)
	}
}

func TestNextAfter_DayFields_StarPrefixed_IsAND(t *testing.T) {
	s := mustParse(t, "0 3 */2 * 1")
	// Mondays 03-30 and 04-06 are even days; 04-13 is the first odd Monday.
	if got, want := s.NextAfter(at3(2026, 3, 24)), at3(2026, 4, 13); !got.Equal(want) {
		t.Errorf("NextAfter(Tue 03-24) = %v, want %v (next odd-day Monday)", got, want)
	}
}

// DayFieldsBothRestricted reports exactly the expressions whose meaning
// changed under the OR rule, for the startup scheduling report.
func TestDayFieldsBothRestricted(t *testing.T) {
	tests := []struct {
		expr string
		want bool
	}{
		{"0 3 1 * 1", true},
		{"0 3 1-7 * 1-5", true},
		{"0 3 15 * 7", true},
		{"0 3 */2 * 1", false},
		{"0 3 1 * */1", false},
		{"0 3 * * 1", false},
		{"0 3 1 * *", false},
		{"* * * * *", false},
	}
	for _, tt := range tests {
		if got := mustParse(t, tt.expr).DayFieldsBothRestricted(); got != tt.want {
			t.Errorf("%q DayFieldsBothRestricted() = %v, want %v", tt.expr, got, tt.want)
		}
	}
}
