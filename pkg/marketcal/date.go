package marketcal

import (
	"fmt"
	"time"
)

// dateLayout is the only accepted textual form of a Date.
const dateLayout = "2006-01-02"

// Date is a civil (calendar) date with no time of day or location.
//
// Date values are comparable with == and usable as map keys. Methods
// normalise out-of-range fields the way time.Date does (February 30 becomes
// March 1 or 2), so build dates from untrusted input with ParseDate.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// DateOf returns the civil date of t in t's own location. Convert t with
// t.In(loc) first to get the date in another time zone.
func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{Year: y, Month: m, Day: d}
}

// ParseDate parses a strict "YYYY-MM-DD" date (four-digit year, two-digit
// month and day, no surrounding text). Out-of-range months and days, such as
// 2025-02-29, are rejected.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return Date{}, fmt.Errorf("invalid date %q: want YYYY-MM-DD", s)
	}
	return DateOf(t), nil
}

// String formats d as "YYYY-MM-DD".
func (d Date) String() string {
	return d.midnightUTC().Format(dateLayout)
}

// Weekday returns the day of the week of d.
func (d Date) Weekday() time.Weekday {
	return d.midnightUTC().Weekday()
}

// AddDays returns d shifted by n calendar days (n may be negative).
func (d Date) AddDays(n int) Date {
	return DateOf(d.midnightUTC().AddDate(0, 0, n))
}

// compare returns -1, 0 or +1 as d is before, equal to or after o.
func (d Date) compare(o Date) int {
	return d.midnightUTC().Compare(o.midnightUTC())
}

// midnightUTC anchors d at 00:00 UTC. UTC has no DST, so calendar-day
// arithmetic on the result never drifts across a day boundary.
func (d Date) midnightUTC() time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
}
