package schedule

import (
	"fmt"
	"strconv"
	"time"
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

// days returns the calendar-day length of a d or w interval.
func (i Interval) days() int {
	if i.Unit == Week {
		return 7 * i.N
	}
	return i.N
}

// wall is a local wall-clock reading: a calendar date and time of day with
// no zone attached.
type wall struct {
	year                    int
	month                   time.Month
	day, hour, min, sec, ns int
}

func wallOf(t time.Time) wall {
	y, mo, d := t.Date()
	h, mi, sec := t.Clock()
	return wall{y, mo, d, h, mi, sec, t.Nanosecond()}
}

// before orders wall readings chronologically.
func (w wall) before(v wall) bool {
	a := [...]int{w.year, int(w.month), w.day, w.hour, w.min, w.sec, w.ns}
	b := [...]int{v.year, int(v.month), v.day, v.hour, v.min, v.sec, v.ns}
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// resolveWall maps local wall time w to an instant in loc under addendum
// decision #7: a wall time skipped by a forward offset change (DST
// spring-forward) resolves to the first valid instant after it — the
// transition itself, so 02:30 becomes 03:00 in New York — and a wall time
// that occurs twice resolves to its first occurrence.
//
// time.Date cannot be trusted with either case. For a skipped time it
// returns some other wall time, earlier in New York (01:30), later in
// London (02:30) and even on the previous date in Santiago (23:00 for a
// skipped midnight); for a repeated time it picks the first pass in New
// York but the second in London. The round trip through wallOf exposes the
// substitution, and the zone bounds of the substitute locate the
// transition.
func resolveWall(w wall, loc *time.Location) time.Time {
	t := time.Date(w.year, w.month, w.day, w.hour, w.min, w.sec, w.ns, loc)
	if wallOf(t) == w {
		return firstOccurrence(t)
	}
	// w was skipped. t sits on one side of the gap: before it, the gap
	// starts where t's zone period ends; after it, where t's period begins.
	start, end := t.ZoneBounds()
	if wallOf(t).before(w) {
		if !end.IsZero() {
			return end
		}
	} else if !start.IsZero() {
		return start
	}
	return t
}

// firstOccurrence maps t to the earliest instant showing the same local
// wall clock.
func firstOccurrence(t time.Time) time.Time {
	for i := 0; i < 4; i++ {
		u, ok := earlierOccurrence(t)
		if !ok {
			break
		}
		t = u
	}
	return t
}

// earlierOccurrence returns an earlier instant showing exactly t's local
// wall clock, if one exists. An earlier instant u with the same wall clock
// satisfies u = t - (offset(u) - offset(t)) for an offset larger than t's;
// the candidates are the offsets of the zone periods just before t's, and
// the wall-clock comparison is the proof. Two periods are examined so a
// metadata-only transition inside a repeated span does not hide it. (The
// same technique as pkg/cron, which does not export it.)
func earlierOccurrence(t time.Time) (time.Time, bool) {
	_, off := t.Zone()
	p := t
	for i := 0; i < 2; i++ {
		start, _ := p.ZoneBounds()
		if start.IsZero() {
			break // fixed zone, or no earlier transition
		}
		prev := start.Add(-time.Nanosecond)
		if _, prevOff := prev.Zone(); prevOff > off {
			u := t.Add(-time.Duration(prevOff-off) * time.Second)
			if wallOf(u) == wallOf(t) {
				return u, true
			}
		}
		p = prev
	}
	return time.Time{}, false
}

// dayNumber counts days from 1970-01-01 to a calendar date.
func dayNumber(y int, m time.Month, d int) int {
	return int(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

// floorDiv is integer division rounding toward negative infinity.
func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// calendarSlot returns base slot k of a d or w interval: the local
// calendar date anchorDate + k*days, at the schedule's time of day.
func (s *Schedule) calendarSlot(k int) time.Time {
	y, m, d := time.Date(s.anchorWall.year, s.anchorWall.month, s.anchorWall.day+k*s.every.days(), 0, 0, 0, 0, time.UTC).Date()
	return resolveWall(wall{y, m, d, s.anchorWall.hour, s.anchorWall.min, s.anchorWall.sec, 0}, s.loc)
}

// calendarIndex returns floor(local days from the anchor date to t's local
// date / step): the index of the last slot dated on or before t's date.
func (s *Schedule) calendarIndex(t time.Time) int {
	y, m, d := t.In(s.loc).Date()
	diff := dayNumber(y, m, d) - dayNumber(s.anchorWall.year, s.anchorWall.month, s.anchorWall.day)
	return floorDiv(diff, s.every.days())
}

// calendarPrev returns the latest d/w base slot at or before t. Dates
// order slots, but instants decide: around a DST change the slot dated on
// t's own date may still lie after t, so a few neighbours are checked.
func (s *Schedule) calendarPrev(t time.Time) (time.Time, bool) {
	k := s.calendarIndex(t) + 1
	for i := 0; i < 4 && k >= 0; i, k = i+1, k-1 {
		if b := s.calendarSlot(k); !b.After(t) {
			return b, true
		}
	}
	return time.Time{}, false
}

// calendarNext returns the earliest d/w base slot after t.
func (s *Schedule) calendarNext(t time.Time) (time.Time, bool) {
	k := max(s.calendarIndex(t)-1, 0)
	for i := 0; i < 4; i, k = i+1, k+1 {
		if b := s.calendarSlot(k); b.After(t) {
			return b, true
		}
	}
	return time.Time{}, false
}
