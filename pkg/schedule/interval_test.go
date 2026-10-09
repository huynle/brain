package schedule

import "testing"

func TestParseEvery_Valid(t *testing.T) {
	tests := []struct {
		in       string
		want     Interval
		calendar bool
	}{
		{"1m", Interval{1, Minute}, false},
		{"90m", Interval{90, Minute}, false},
		{"12h", Interval{12, Hour}, false},
		{"1d", Interval{1, Day}, true},
		{"4d", Interval{4, Day}, true},
		{"14d", Interval{14, Day}, true},
		{"2w", Interval{2, Week}, true},
		// The upper bound: 100 years of 365.25 days.
		{"36525d", Interval{36525, Day}, true},
		{"5217w", Interval{5217, Week}, true},
		{"52596000m", Interval{52596000, Minute}, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseEvery(tt.in)
			if err != nil {
				t.Fatalf("ParseEvery(%q) error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseEvery(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
			if got.Calendar() != tt.calendar {
				t.Errorf("ParseEvery(%q).Calendar() = %v, want %v", tt.in, got.Calendar(), tt.calendar)
			}
			if got.String() != tt.in {
				t.Errorf("ParseEvery(%q).String() = %q, want round trip", tt.in, got.String())
			}
		})
	}
}

func TestParseEvery_Invalid(t *testing.T) {
	for _, in := range []string{
		"", "d", "4", "0d", "00m", "-1d", "+1d", "1.5d", "4D", "4H", "4 d", " 4d", "4d ",
		"4dd", "4s", "1y", "4x", "d4", "1h30m", "１d",
		"99999999999999999999d", // overflows int
		"36526d", "5218w", "52596001m", "876601h", // longer than 100 years
	} {
		t.Run(in, func(t *testing.T) {
			if got, err := ParseEvery(in); err == nil {
				t.Errorf("ParseEvery(%q) = %+v, want error", in, got)
			}
		})
	}
}

func TestParseAt(t *testing.T) {
	valid := []struct {
		in           string
		hour, minute int
	}{
		{"00:00", 0, 0},
		{"03:00", 3, 0},
		{"09:05", 9, 5},
		{"12:30", 12, 30},
		{"23:59", 23, 59},
	}
	for _, tt := range valid {
		t.Run(tt.in, func(t *testing.T) {
			h, m, err := ParseAt(tt.in)
			if err != nil {
				t.Fatalf("ParseAt(%q) error: %v", tt.in, err)
			}
			if h != tt.hour || m != tt.minute {
				t.Errorf("ParseAt(%q) = %d:%d, want %d:%d", tt.in, h, m, tt.hour, tt.minute)
			}
		})
	}
	for _, in := range []string{
		"", "3:00", "03:0", "24:00", "23:60", "99:99", "12-00", "12:00:00", "ab:cd",
		" 03:00", "03:00 ", "+3:00", "-1:00", "3pm", "1200", "１２:００",
	} {
		t.Run("invalid "+in, func(t *testing.T) {
			if h, m, err := ParseAt(in); err == nil {
				t.Errorf("ParseAt(%q) = %d:%d, want error", in, h, m)
			}
		})
	}
}
