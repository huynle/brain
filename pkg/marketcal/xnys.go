package marketcal

import (
	"fmt"
	"slices"
	"time"
)

// extraClosureName names extra_closed dates that no built-in rule covers.
const extraClosureName = "Extra closure"

// Holiday is a weekday on which the exchange is closed for the full day.
type Holiday struct {
	Date Date
	// Name is a display label, e.g. "Good Friday" or "Independence Day
	// (observed)"; extra_closed dates not covered by a built-in rule are
	// named "Extra closure".
	Name string
}

// XNYSOptions adjusts the built-in XNYS calendar without a release. Dates
// are strict "YYYY-MM-DD" strings.
type XNYSOptions struct {
	// ExtraClosed marks additional full-day closures (for example a new
	// national day of mourning).
	ExtraClosed []string
	// ExtraOpen marks dates as trading days, overriding every built-in rule
	// (weekends, holidays and shipped one-off closures). A date may not be
	// listed in both ExtraClosed and ExtraOpen.
	ExtraOpen []string
}

// XNYS is the full-day trading calendar of the US equity markets (NYSE and
// NASDAQ). It is immutable after NewXNYS and safe for concurrent use.
type XNYS struct {
	extraClosed map[Date]bool
	extraOpen   map[Date]bool
}

// NewXNYS builds an XNYS calendar. It returns an error naming the offending
// entry if any option date is not a valid "YYYY-MM-DD" date, or if a date is
// listed as both extra closed and extra open.
func NewXNYS(opts XNYSOptions) (*XNYS, error) {
	c := &XNYS{
		extraClosed: make(map[Date]bool, len(opts.ExtraClosed)),
		extraOpen:   make(map[Date]bool, len(opts.ExtraOpen)),
	}
	for i, s := range opts.ExtraClosed {
		d, err := ParseDate(s)
		if err != nil {
			return nil, fmt.Errorf("marketcal: xnys extra_closed[%d]: %w", i, err)
		}
		c.extraClosed[d] = true
	}
	for i, s := range opts.ExtraOpen {
		d, err := ParseDate(s)
		if err != nil {
			return nil, fmt.Errorf("marketcal: xnys extra_open[%d]: %w", i, err)
		}
		if c.extraClosed[d] {
			return nil, fmt.Errorf("marketcal: xnys extra_open[%d]: %s is also listed in extra_closed", i, d)
		}
		c.extraOpen[d] = true
	}
	return c, nil
}

// Name returns the calendar's identifier, "xnys".
func (c *XNYS) Name() string { return "xnys" }

// IsOpen reports whether d is a trading day. Early-close (half) days count
// as open. Out-of-range fields in d are normalised as by time.Date.
func (c *XNYS) IsOpen(d Date) bool {
	d = DateOf(d.midnightUTC())
	if c.extraOpen[d] {
		return true
	}
	if isWeekend(d) {
		return false
	}
	return !slices.ContainsFunc(c.Holidays(d.Year), func(h Holiday) bool { return h.Date == d })
}

// Holidays lists the weekday full-day closures in year, in date order:
// rule-based holidays at their observed dates, shipped one-off closures and
// ExtraClosed dates, minus ExtraOpen dates. For every weekday d,
// IsOpen(d) reports whether d is absent from Holidays(d.Year). The returned
// slice is newly allocated on each call.
func (c *XNYS) Holidays(year int) []Holiday {
	hs := ruleHolidays(year)
	for _, h := range oneOffClosures {
		if h.Date.Year == year {
			hs = append(hs, h)
		}
	}
	for d := range c.extraClosed {
		if d.Year == year && !isWeekend(d) && !slices.ContainsFunc(hs, func(h Holiday) bool { return h.Date == d }) {
			hs = append(hs, Holiday{Date: d, Name: extraClosureName})
		}
	}
	hs = slices.DeleteFunc(hs, func(h Holiday) bool { return c.extraOpen[h.Date] })
	slices.SortFunc(hs, func(a, b Holiday) int { return a.Date.compare(b.Date) })
	return hs
}

func isWeekend(d Date) bool {
	wd := d.Weekday()
	return wd == time.Saturday || wd == time.Sunday
}

// oneOffClosures are the unscheduled full-day NYSE closures since 2000.
// Sources for each date are cited in xnys_test.go (oneOffClosureSources).
var oneOffClosures = []Holiday{
	{Date{2001, time.September, 11}, "September 11 attacks"},
	{Date{2001, time.September, 12}, "September 11 attacks"},
	{Date{2001, time.September, 13}, "September 11 attacks"},
	{Date{2001, time.September, 14}, "September 11 attacks"},
	{Date{2004, time.June, 11}, "National Day of Mourning for President Ronald Reagan"},
	{Date{2007, time.January, 2}, "National Day of Mourning for President Gerald R. Ford"},
	{Date{2012, time.October, 29}, "Hurricane Sandy"},
	{Date{2012, time.October, 30}, "Hurricane Sandy"},
	{Date{2018, time.December, 5}, "National Day of Mourning for President George H. W. Bush"},
	{Date{2025, time.January, 9}, "National Day of Mourning for President Jimmy Carter"},
}

// ruleHolidays returns the rule-based NYSE holidays of year at their
// observed dates, in date order.
//
// NYSE Rule 7.2 moves a Saturday holiday to the preceding Friday and a
// Sunday holiday to the following Monday, "unless unusual business
// conditions exist, such as the ending of a monthly or yearly accounting
// period" — which is why New Year's Day on a Saturday is not observed.
// Juneteenth was added to Rule 7.2 in 2021 and first observed in 2022. See
// https://www.federalregister.gov/documents/2021/10/05/2021-21742.
func ruleHolidays(year int) []Holiday {
	hs := make([]Holiday, 0, 10)
	// Saturday New Year's Day: December 31 of the prior year stays open.
	if nyd := (Date{year, time.January, 1}); nyd.Weekday() != time.Saturday {
		hs = append(hs, observed(nyd, "New Year's Day"))
	}
	hs = append(hs,
		Holiday{nthWeekday(year, time.January, time.Monday, 3), "Martin Luther King, Jr. Day"},
		Holiday{nthWeekday(year, time.February, time.Monday, 3), "Washington's Birthday"},
		Holiday{easterSunday(year).AddDays(-2), "Good Friday"},
		Holiday{lastWeekday(year, time.May, time.Monday), "Memorial Day"},
	)
	if year >= 2022 {
		hs = append(hs, observed(Date{year, time.June, 19}, "Juneteenth National Independence Day"))
	}
	hs = append(hs,
		observed(Date{year, time.July, 4}, "Independence Day"),
		Holiday{nthWeekday(year, time.September, time.Monday, 1), "Labor Day"},
		Holiday{nthWeekday(year, time.November, time.Thursday, 4), "Thanksgiving Day"},
		observed(Date{year, time.December, 25}, "Christmas Day"),
	)
	return hs
}

// observed shifts a fixed-date holiday off the weekend: Saturday to the
// preceding Friday, Sunday to the following Monday.
func observed(d Date, name string) Holiday {
	switch d.Weekday() {
	case time.Saturday:
		return Holiday{d.AddDays(-1), name + " (observed)"}
	case time.Sunday:
		return Holiday{d.AddDays(1), name + " (observed)"}
	}
	return Holiday{d, name}
}

// nthWeekday returns the n-th (1-based) weekday wd of month m.
func nthWeekday(year int, m time.Month, wd time.Weekday, n int) Date {
	first := Date{year, m, 1}
	offset := (int(wd) - int(first.Weekday()) + 7) % 7
	return first.AddDays(offset + 7*(n-1))
}

// lastWeekday returns the last weekday wd of month m.
func lastWeekday(year int, m time.Month, wd time.Weekday) Date {
	last := Date{year, m + 1, 1}.AddDays(-1)
	offset := (int(last.Weekday()) - int(wd) + 7) % 7
	return last.AddDays(-offset)
}
