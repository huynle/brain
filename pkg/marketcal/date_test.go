package marketcal

import (
	"testing"
	"time"
)

func TestParseDate_Valid(t *testing.T) {
	tests := []struct {
		in   string
		want Date
	}{
		{"2026-01-19", Date{2026, time.January, 19}},
		{"2024-02-29", Date{2024, time.February, 29}},
		{"2027-12-31", Date{2027, time.December, 31}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseDate(tt.in)
			if err != nil {
				t.Fatalf("ParseDate(%q) error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseDate(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseDate_Invalid(t *testing.T) {
	for _, in := range []string{
		"",
		"2026-1-19",   // month must be two digits
		"2026-01-9",   // day must be two digits
		"26-01-19",    // year must be four digits
		"2026-13-01",  // month out of range
		"2026-00-10",  // month out of range
		"2026-02-30",  // day out of range for month
		"2025-02-29",  // not a leap year
		"2026-01-19x", // trailing text
		" 2026-01-19", // leading space
		"2026/01/19",  // wrong separator
		"19-01-2026",  // wrong order
		"2026-01-19T00:00:00Z",
	} {
		t.Run(in, func(t *testing.T) {
			if got, err := ParseDate(in); err == nil {
				t.Errorf("ParseDate(%q) = %+v, want error", in, got)
			}
		})
	}
}

func TestDateOf_UsesTimesOwnLocation(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	// 23:30 in New York on Jan 1 is already Jan 2 in UTC; the civil date is
	// the one in the time's own location.
	tm := time.Date(2026, time.January, 1, 23, 30, 0, 0, ny)
	if got, want := DateOf(tm), (Date{2026, time.January, 1}); got != want {
		t.Errorf("DateOf(%v) = %+v, want %+v", tm, got, want)
	}
	if got, want := DateOf(tm.UTC()), (Date{2026, time.January, 2}); got != want {
		t.Errorf("DateOf(%v) = %+v, want %+v", tm.UTC(), got, want)
	}
}

func TestDate_String(t *testing.T) {
	tests := []struct {
		d    Date
		want string
	}{
		{Date{2027, time.July, 5}, "2027-07-05"},
		{Date{2001, time.September, 11}, "2001-09-11"},
		{Date{2025, time.December, 31}, "2025-12-31"},
	}
	for _, tt := range tests {
		if got := tt.d.String(); got != tt.want {
			t.Errorf("%+v.String() = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestDate_Weekday(t *testing.T) {
	tests := []struct {
		d    Date
		want time.Weekday
	}{
		{Date{2026, time.October, 9}, time.Friday},
		{Date{2028, time.January, 1}, time.Saturday},
		{Date{2027, time.July, 4}, time.Sunday},
		{Date{2000, time.January, 3}, time.Monday},
	}
	for _, tt := range tests {
		if got := tt.d.Weekday(); got != tt.want {
			t.Errorf("%v.Weekday() = %v, want %v", tt.d, got, tt.want)
		}
	}
}

func TestDate_AddDays(t *testing.T) {
	tests := []struct {
		d    Date
		n    int
		want Date
	}{
		{Date{2026, time.December, 31}, 1, Date{2027, time.January, 1}},
		{Date{2024, time.March, 1}, -1, Date{2024, time.February, 29}},
		{Date{2025, time.March, 1}, -1, Date{2025, time.February, 28}},
		{Date{2026, time.April, 5}, -2, Date{2026, time.April, 3}},
		{Date{2026, time.April, 5}, 0, Date{2026, time.April, 5}},
		{Date{2026, time.January, 1}, 365, Date{2027, time.January, 1}},
	}
	for _, tt := range tests {
		if got := tt.d.AddDays(tt.n); got != tt.want {
			t.Errorf("%v.AddDays(%d) = %+v, want %+v", tt.d, tt.n, got, tt.want)
		}
	}
}
