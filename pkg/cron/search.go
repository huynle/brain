package cron

import "time"

// searchYears bounds how far NextAfter and PrevAtOrBefore look before
// concluding a schedule never fires. The longest gap between two firings of
// a satisfiable expression is 40 years: Feb 29 on a fixed weekday ("0 0 29
// 2 */7") recurs every 28 years except across a skipped century leap day
// (2088 → 2128). A century leaves ample margin while keeping the search for
// an impossible expression ("0 0 30 2 *") to a few thousand cheap steps.
const searchYears = 100

// civil is a local wall-clock minute: a calendar date and time of day with
// no zone attached. Searching in civil time keeps the field arithmetic free
// of DST; resolve turns a civil minute into a real instant afterwards.
type civil struct {
	year   int
	month  time.Month
	day    int
	hour   int
	minute int
}

func civilOf(t time.Time) civil {
	y, m, d := t.Date()
	return civil{y, m, d, t.Hour(), t.Minute()}
}

func daysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func weekdayOfFirst(year int, month time.Month) int {
	return int(time.Date(year, month, 1, 0, 0, 0, 0, time.UTC).Weekday())
}

// resolve maps a civil minute to the instant it names in loc. It reports
// false when that wall time does not exist (skipped by a DST spring-forward
// or any other forward offset change); a wall time that occurs twice
// resolves to its first occurrence.
//
// time.Date never fails: it silently substitutes another wall time for a
// nonexistent one, and on US zones the substitute for a missing 02:00 is
// 01:00 — an hour EARLIER. An earlier instant-stepping search trusted that
// result, stalled, and returned the zero time for an ordinary "0 2 * * *".
// Comparing the round trip is what makes the substitution visible.
func resolve(c civil, loc *time.Location) (time.Time, bool) {
	t := time.Date(c.year, c.month, c.day, c.hour, c.minute, 0, 0, loc)
	if civilOf(t) != c {
		// time.Date normalized a nonexistent time to some other wall time.
		return time.Time{}, false
	}
	return firstOccurrence(t), true
}

// nextFrom returns the smallest value >= from (and <= hi) in the set.
func (fs *fieldSet) nextFrom(from, hi int) (int, bool) {
	for v := from; v <= hi; v++ {
		if fs.has(v) {
			return v, true
		}
	}
	return 0, false
}

// nextCivil returns the earliest civil minute at or after c whose fields
// match, searching through the end of year limit. c.day, c.hour and
// c.minute may sit one above their range (past month end, 24, 60); the loop
// normalizes them by moving to the start of the next unit.
func (s *Schedule) nextCivil(c civil, limit int) (civil, bool) {
	startOfNextMonth := func(c civil) civil {
		y, m := c.year, c.month+1
		if m > time.December {
			y, m = y+1, time.January
		}
		return civil{y, m, 1, 0, 0}
	}
	for c.year <= limit {
		if !s.fields[3].has(int(c.month)) {
			c = startOfNextMonth(c)
			continue
		}
		dim := daysIn(c.year, c.month)
		wd1 := weekdayOfFirst(c.year, c.month)
		d := c.day
		for d <= dim && !s.dayMatches(d, (wd1+d-1)%7) {
			d++
		}
		if d > dim {
			c = startOfNextMonth(c)
			continue
		}
		if d != c.day {
			c.day, c.hour, c.minute = d, 0, 0
		}
		h, ok := s.fields[1].nextFrom(c.hour, 23)
		if !ok {
			c.day, c.hour, c.minute = c.day+1, 0, 0
			continue
		}
		if h != c.hour {
			c.hour, c.minute = h, 0
		}
		m, ok := s.fields[0].nextFrom(c.minute, 59)
		if !ok {
			c.hour, c.minute = c.hour+1, 0
			continue
		}
		c.minute = m
		return c, true
	}
	return civil{}, false
}

// prevFrom returns the largest value <= from (and >= lo) in the set.
func (fs *fieldSet) prevFrom(from, lo int) (int, bool) {
	for v := from; v >= lo; v-- {
		if fs.has(v) {
			return v, true
		}
	}
	return 0, false
}

// prevCivil returns the latest civil minute at or before c whose fields
// match, searching back to the start of year limit. c.day, c.hour and
// c.minute may sit one below their range (0, -1, -1); the loop normalizes
// them by moving to the end of the previous unit.
func (s *Schedule) prevCivil(c civil, limit int) (civil, bool) {
	endOfPrevMonth := func(c civil) civil {
		y, m := c.year, c.month-1
		if m < time.January {
			y, m = y-1, time.December
		}
		return civil{y, m, daysIn(y, m), 23, 59}
	}
	for c.year >= limit {
		if !s.fields[3].has(int(c.month)) {
			c = endOfPrevMonth(c)
			continue
		}
		wd1 := weekdayOfFirst(c.year, c.month)
		d := c.day
		for d >= 1 && !s.dayMatches(d, (wd1+d-1)%7) {
			d--
		}
		if d < 1 {
			c = endOfPrevMonth(c)
			continue
		}
		if d != c.day {
			c.day, c.hour, c.minute = d, 23, 59
		}
		h, ok := s.fields[1].prevFrom(c.hour, 0)
		if !ok {
			c.day, c.hour, c.minute = c.day-1, 23, 59
			continue
		}
		if h != c.hour {
			c.hour, c.minute = h, 59
		}
		m, ok := s.fields[0].prevFrom(c.minute, 0)
		if !ok {
			c.hour, c.minute = c.hour-1, 59
			continue
		}
		c.minute = m
		return c, true
	}
	return civil{}, false
}

// highWaterMark returns the instant whose wall clock is the latest shown at
// or before t. Normally that is t itself. In the second pass of a repeated
// hour the clock has been set back, so the latest wall time already seen is
// the one just before the transition — and every first-pass instant up to
// it is at or before t.
func highWaterMark(t time.Time) time.Time {
	for i := 0; i < 4; i++ {
		u, ok := earlierOccurrence(t)
		if !ok {
			break
		}
		_, end := u.ZoneBounds()
		t = end.Add(-time.Nanosecond)
	}
	return t
}

// NextAfter returns the next time after t that matches the schedule,
// evaluated in t's location; the result is in that location with seconds
// and nanoseconds zeroed. A run at t's own minute does not count.
//
// Matching follows the same rules as Matches: a local time skipped by a DST
// spring-forward never fires, and a local time repeated by a fall-back fires
// only at its first occurrence — so from inside the second pass of a
// repeated hour, the next run is after that hour. The search skips whole
// months, days and hours that cannot match.
//
// Returns the zero time when nothing matches within the bounded search
// (about a century) — "0 0 30 2 *" (February 30th) being the canonical case.
// Callers must test IsZero rather than formatting the result. The zero time
// renders as 0001-01-01T00:00:00Z, which reads as a valid RFC3339 timestamp
// in the past — and any scheduler comparing "now >= next_run" against it
// fires forever.
func (s *Schedule) NextAfter(t time.Time) time.Time {
	loc := t.Location()
	c := civilOf(t)
	c.minute++ // strictly after t's minute; nextCivil normalizes minute 60
	limit := c.year + searchYears
	for {
		w, ok := s.nextCivil(c, limit)
		if !ok {
			return time.Time{}
		}
		// A wall time skipped by DST does not fire. One that resolves to an
		// instant at or before t is a first pass that t, re-living a
		// repeated hour after a fall-back, has already left behind.
		if at, exists := resolve(w, loc); exists && at.After(t) {
			return at
		}
		c = w
		c.minute++
	}
}

// PrevAtOrBefore returns the latest time at or before t that matches the
// schedule, evaluated in t's location; the result is in that location with
// seconds and nanoseconds zeroed. A run at exactly t's minute counts.
//
// Matching follows the same rules as Matches: a local time skipped by a DST
// spring-forward never fires, and a local time repeated by a fall-back fires
// only at its first occurrence. The search steps backward over whole
// months, days and hours that cannot match rather than minute by minute.
//
// Returns the zero time when nothing matches within the bounded search
// (about a century) — "0 0 30 2 *" (February 30th) being the canonical case.
// Callers must test IsZero rather than formatting or comparing the result:
// the zero time renders as a valid-looking timestamp in year 1 and sorts
// before every real slot.
func (s *Schedule) PrevAtOrBefore(t time.Time) time.Time {
	loc := t.Location()
	c := civilOf(highWaterMark(t))
	limit := c.year - searchYears
	for {
		w, ok := s.prevCivil(c, limit)
		if !ok {
			return time.Time{}
		}
		if at, exists := resolve(w, loc); exists && !at.After(t) {
			return at
		}
		// w is skipped by DST (or, defensively, resolved past t): keep going.
		c = w
		c.minute--
	}
}
