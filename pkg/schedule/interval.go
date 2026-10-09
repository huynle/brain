package schedule

import (
	"fmt"
	"strconv"
)

// Unit is the unit of an Every interval.
type Unit byte

// Interval units, spelled as in the every grammar.
const (
	Minute Unit = 'm'
	Hour   Unit = 'h'
	Day    Unit = 'd'
	Week   Unit = 'w'
)

// maxIntervalDays caps an interval at 100 years of 365.25 days, far beyond
// any real schedule, so interval arithmetic can never overflow a
// time.Duration (about 292 years).
const maxIntervalDays = 36525

// unitsPerMaxInterval is the largest N accepted for each unit.
var unitsPerMaxInterval = map[Unit]int{
	Minute: maxIntervalDays * 24 * 60,
	Hour:   maxIntervalDays * 24,
	Day:    maxIntervalDays,
	Week:   maxIntervalDays / 7,
}

// Interval is a parsed Every value: N units.
type Interval struct {
	N    int
	Unit Unit
}

// ParseEvery parses an interval in the grammar <positive integer><unit>,
// unit one of m (minutes), h (hours), d (days) or w (weeks) — the units
// time.ParseDuration lacks. Nothing else is accepted: no sign, no spaces,
// no fractions, no upper-case units, no compound values such as "1h30m".
// Intervals longer than 100 years are rejected.
func ParseEvery(s string) (Interval, error) {
	if len(s) < 2 {
		return Interval{}, fmt.Errorf("invalid interval %q: want <positive integer><m|h|d|w>", s)
	}
	unit := Unit(s[len(s)-1])
	maxN, ok := unitsPerMaxInterval[unit]
	if !ok {
		return Interval{}, fmt.Errorf("invalid interval %q: unit must be m, h, d or w", s)
	}
	digits := s[:len(s)-1]
	if !allDigits(digits) {
		return Interval{}, fmt.Errorf("invalid interval %q: want <positive integer><m|h|d|w>", s)
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n > maxN {
		return Interval{}, fmt.Errorf("invalid interval %q: longer than 100 years", s)
	}
	if n == 0 {
		return Interval{}, fmt.Errorf("invalid interval %q: must be positive", s)
	}
	return Interval{N: n, Unit: unit}, nil
}

// ParseAt parses a 24-hour "HH:MM" time of day: exactly two digits, a
// colon and two digits, 00:00 through 23:59.
func ParseAt(s string) (hour, minute int, err error) {
	if len(s) != 5 || s[2] != ':' || !allDigits(s[:2]) || !allDigits(s[3:]) {
		return 0, 0, fmt.Errorf("invalid time of day %q: want 24-hour HH:MM", s)
	}
	hour = int(s[0]-'0')*10 + int(s[1]-'0')
	minute = int(s[3]-'0')*10 + int(s[4]-'0')
	if hour > 23 || minute > 59 {
		return 0, 0, fmt.Errorf("invalid time of day %q: want 00:00 through 23:59", s)
	}
	return hour, minute, nil
}

// Calendar reports whether the interval steps calendar days (d, w) rather
// than absolute durations (m, h).
func (i Interval) Calendar() bool { return i.Unit == Day || i.Unit == Week }

// String renders the interval in the every grammar.
func (i Interval) String() string { return strconv.Itoa(i.N) + string(rune(i.Unit)) }

// allDigits reports whether s is non-empty and only ASCII digits.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
