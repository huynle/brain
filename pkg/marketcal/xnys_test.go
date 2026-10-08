package marketcal

import (
	"slices"
	"testing"
	"time"
)

// nyseGolden is NYSE's published full-day holiday table (early closes are
// out of scope), keyed by year.
//
// Sources (accessed 2026-10-09):
//   - 2026, 2027, 2028: https://www.nyse.com/markets/hours-calendars
//     ("All NYSE markets observe U.S. holidays as listed below for 2026,
//     2027, and 2028"; footnote: "Because the holiday falls on Saturday,
//     January 1, 2028, no New Year's Day holiday is observed.")
//   - 2024, 2025: the same page as archived on 2024-12-24,
//     https://web.archive.org/web/20241224150435/https://www.nyse.com/markets/hours-calendars
//     ("... for 2024, 2025, and 2026"; its 2026 column matches the live page).
//
// The 2024-12-24 snapshot predates the 2025-01-09 National Day of Mourning
// closure; that row is a shipped one-off closure, sourced in
// oneOffClosureSources below.
var nyseGolden = map[int][]struct{ date, name string }{
	2024: {
		{"2024-01-01", "New Year's Day"},
		{"2024-01-15", "Martin Luther King, Jr. Day"},
		{"2024-02-19", "Washington's Birthday"},
		{"2024-03-29", "Good Friday"},
		{"2024-05-27", "Memorial Day"},
		{"2024-06-19", "Juneteenth National Independence Day"},
		{"2024-07-04", "Independence Day"},
		{"2024-09-02", "Labor Day"},
		{"2024-11-28", "Thanksgiving Day"},
		{"2024-12-25", "Christmas Day"},
	},
	2025: {
		{"2025-01-01", "New Year's Day"},
		{"2025-01-09", "National Day of Mourning for President Jimmy Carter"},
		{"2025-01-20", "Martin Luther King, Jr. Day"},
		{"2025-02-17", "Washington's Birthday"},
		{"2025-04-18", "Good Friday"},
		{"2025-05-26", "Memorial Day"},
		{"2025-06-19", "Juneteenth National Independence Day"},
		{"2025-07-04", "Independence Day"},
		{"2025-09-01", "Labor Day"},
		{"2025-11-27", "Thanksgiving Day"},
		{"2025-12-25", "Christmas Day"},
	},
	2026: {
		{"2026-01-01", "New Year's Day"},
		{"2026-01-19", "Martin Luther King, Jr. Day"},
		{"2026-02-16", "Washington's Birthday"},
		{"2026-04-03", "Good Friday"},
		{"2026-05-25", "Memorial Day"},
		{"2026-06-19", "Juneteenth National Independence Day"},
		{"2026-07-03", "Independence Day (observed)"},
		{"2026-09-07", "Labor Day"},
		{"2026-11-26", "Thanksgiving Day"},
		{"2026-12-25", "Christmas Day"},
	},
	2027: {
		{"2027-01-01", "New Year's Day"},
		{"2027-01-18", "Martin Luther King, Jr. Day"},
		{"2027-02-15", "Washington's Birthday"},
		{"2027-03-26", "Good Friday"},
		{"2027-05-31", "Memorial Day"},
		{"2027-06-18", "Juneteenth National Independence Day (observed)"},
		{"2027-07-05", "Independence Day (observed)"},
		{"2027-09-06", "Labor Day"},
		{"2027-11-25", "Thanksgiving Day"},
		{"2027-12-24", "Christmas Day (observed)"},
	},
	2028: {
		// No New Year's Day: January 1, 2028 is a Saturday.
		{"2028-01-17", "Martin Luther King, Jr. Day"},
		{"2028-02-21", "Washington's Birthday"},
		{"2028-04-14", "Good Friday"},
		{"2028-05-29", "Memorial Day"},
		{"2028-06-19", "Juneteenth National Independence Day"},
		{"2028-07-04", "Independence Day"},
		{"2028-09-04", "Labor Day"},
		{"2028-11-23", "Thanksgiving Day"},
		{"2028-12-25", "Christmas Day"},
	},
}

func mustDate(t *testing.T, s string) Date {
	t.Helper()
	d, err := ParseDate(s)
	if err != nil {
		t.Fatalf("ParseDate(%q): %v", s, err)
	}
	return d
}

func mustXNYS(t *testing.T, opts XNYSOptions) *XNYS {
	t.Helper()
	c, err := NewXNYS(opts)
	if err != nil {
		t.Fatalf("NewXNYS(%+v): %v", opts, err)
	}
	return c
}

func goldenHolidays(t *testing.T, year int) []Holiday {
	t.Helper()
	var out []Holiday
	for _, g := range nyseGolden[year] {
		out = append(out, Holiday{Date: mustDate(t, g.date), Name: g.name})
	}
	return out
}

func TestXNYS_Name(t *testing.T) {
	if got := mustXNYS(t, XNYSOptions{}).Name(); got != "xnys" {
		t.Errorf("Name() = %q, want %q", got, "xnys")
	}
}

func TestXNYS_Holidays_MatchNYSEPublishedTables(t *testing.T) {
	c := mustXNYS(t, XNYSOptions{})
	for _, year := range []int{2024, 2025, 2026, 2027, 2028} {
		t.Run(time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC).Format("2006"), func(t *testing.T) {
			want := goldenHolidays(t, year)
			got := c.Holidays(year)
			if !slices.Equal(got, want) {
				t.Errorf("Holidays(%d) mismatch\n got: %v\nwant: %v", year, got, want)
			}
		})
	}
}

// TestXNYS_IsOpen_EveryDay2024To2028 checks every calendar day: closed on
// weekends and on published holidays, open on every other day.
func TestXNYS_IsOpen_EveryDay2024To2028(t *testing.T) {
	c := mustXNYS(t, XNYSOptions{})
	closed := map[Date]bool{}
	for year := 2024; year <= 2028; year++ {
		for _, h := range goldenHolidays(t, year) {
			closed[h.Date] = true
		}
	}
	for d := (Date{2024, time.January, 1}); d.Year <= 2028; d = d.AddDays(1) {
		wd := d.Weekday()
		want := wd != time.Saturday && wd != time.Sunday && !closed[d]
		if got := c.IsOpen(d); got != want {
			t.Errorf("IsOpen(%v, %v) = %v, want %v", d, wd, got, want)
		}
	}
}

func TestXNYS_IsOpen_ObservanceEdgeCases(t *testing.T) {
	c := mustXNYS(t, XNYSOptions{})
	tests := []struct {
		date string
		open bool
		why  string
	}{
		// New Year's Day on Saturday is not observed: prior Dec 31 stays open.
		{"2027-12-31", true, "Jan 1 2028 is a Saturday; not observed"},
		{"2021-12-31", true, "Jan 1 2022 is a Saturday; not observed"},
		{"2010-12-31", true, "Jan 1 2011 is a Saturday; not observed"},
		{"2004-12-31", true, "Jan 1 2005 is a Saturday; not observed"},
		// New Year's Day on Sunday is observed Monday.
		{"2023-01-02", false, "Jan 1 2023 is a Sunday; observed Monday"},
		{"2017-01-02", false, "Jan 1 2017 is a Sunday; observed Monday"},
		// Other Saturday holidays move to Friday, Sunday holidays to Monday.
		{"2026-07-03", false, "Jul 4 2026 is a Saturday"},
		{"2020-07-03", false, "Jul 4 2020 is a Saturday"},
		{"2027-07-05", false, "Jul 4 2027 is a Sunday"},
		{"2021-07-05", false, "Jul 4 2021 is a Sunday"},
		{"2021-12-24", false, "Dec 25 2021 is a Saturday"},
		{"2027-12-24", false, "Dec 25 2027 is a Saturday"},
		{"2022-12-26", false, "Dec 25 2022 is a Sunday"},
		{"2027-06-18", false, "Jun 19 2027 is a Saturday"},
		// Juneteenth starts in 2022 (first observance Monday Jun 20 2022).
		{"2022-06-20", false, "Jun 19 2022 is a Sunday; first NYSE Juneteenth"},
		{"2021-06-18", true, "Juneteenth not an NYSE holiday before 2022"},
		{"2020-06-19", true, "Juneteenth not an NYSE holiday before 2022"},
		// Good Friday follows Easter; Easter Monday is a trading day.
		{"2026-04-03", false, "Good Friday 2026"},
		{"2026-04-06", true, "Easter Monday is open"},
		{"2008-03-21", false, "Good Friday 2008 (early Easter, Mar 23)"},
		{"2011-04-22", false, "Good Friday 2011 (late Easter, Apr 24)"},
		// Nth-weekday rules at month edges.
		{"2029-11-22", false, "4th Thursday when November has five"},
		{"2029-11-29", true, "5th Thursday of November is not Thanksgiving"},
		{"2027-05-31", false, "last Monday of May is the 5th Monday"},
		{"2027-05-24", true, "4th Monday of May 2027 is not Memorial Day"},
		{"2025-09-01", false, "Labor Day on September 1"},
		{"2026-01-19", false, "MLK Day: 3rd Monday of January"},
		{"2026-02-16", false, "Washington's Birthday: 3rd Monday of February"},
		// Early-close days are trading days (early closes are out of scope).
		{"2026-11-27", true, "day after Thanksgiving: early close, open"},
		{"2026-12-24", true, "Christmas Eve 2026: early close, open"},
		// Weekends.
		{"2026-10-10", false, "Saturday"},
		{"2026-10-11", false, "Sunday"},
		{"2026-10-09", true, "ordinary Friday"},
	}
	for _, tt := range tests {
		t.Run(tt.date, func(t *testing.T) {
			if got := c.IsOpen(mustDate(t, tt.date)); got != tt.open {
				t.Errorf("IsOpen(%s) = %v, want %v (%s)", tt.date, got, tt.open, tt.why)
			}
		})
	}
}

func TestXNYS_Holidays_SaturdayNewYearHasNoEntry(t *testing.T) {
	c := mustXNYS(t, XNYSOptions{})
	for _, year := range []int{2005, 2011, 2022, 2028, 2033} {
		for _, h := range c.Holidays(year) {
			if h.Name == "New Year's Day" || h.Name == "New Year's Day (observed)" {
				t.Errorf("Holidays(%d) contains %v; Jan 1 %d is a Saturday", year, h, year)
			}
		}
		for _, h := range c.Holidays(year - 1) {
			if h.Date == (Date{year - 1, time.December, 31}) {
				t.Errorf("Holidays(%d) contains %v; Saturday New Year is not observed", year-1, h)
			}
		}
	}
}

func TestXNYS_Holidays_SortedWeekdaysWithinYear(t *testing.T) {
	c := mustXNYS(t, XNYSOptions{})
	for year := 2000; year <= 2060; year++ {
		hs := c.Holidays(year)
		for i, h := range hs {
			if h.Date.Year != year {
				t.Errorf("Holidays(%d)[%d] = %v is outside the year", year, i, h)
			}
			if wd := h.Date.Weekday(); wd == time.Saturday || wd == time.Sunday {
				t.Errorf("Holidays(%d)[%d] = %v falls on %v", year, i, h, wd)
			}
			if h.Name == "" {
				t.Errorf("Holidays(%d)[%d] has no name", year, i)
			}
			if i > 0 && !hs[i-1].Date.midnightUTC().Before(h.Date.midnightUTC()) {
				t.Errorf("Holidays(%d) not strictly ascending at %d: %v then %v", year, i, hs[i-1], h)
			}
			if c.IsOpen(h.Date) {
				t.Errorf("IsOpen(%v) = true for listed holiday %q", h.Date, h.Name)
			}
		}
	}
}

// oneOffClosureSources lists every shipped unscheduled full-day closure since
// 2000 with the source that confirms it (accessed 2026-10-09):
//
//   - 2001-09-11..14: NYSE did not open after the September 11 attacks and
//     reopened Monday 2001-09-17 ("The History of NYSE",
//     https://www.nyse.com/history-of-nyse).
//   - 2004-06-11: Nasdaq IR, "NASDAQ Will Be Closed Friday, June 11, 2004 In
//     Remembrance of President Reagan" (NYSE closed the same day; Executive
//     Order 13343 closed federal offices),
//     https://ir.nasdaq.com/news-releases/news-release-details/nasdaq-will-be-closed-friday-june-11-2004-remembrance-president
//   - 2007-01-02: Reuters, "Exchanges mark Ford's death with January 2 close",
//     https://www.reuters.com/article/business/exchanges-mark-fords-death-with-january-2-close-idUSN29240994
//   - 2012-10-29..30: NASDAQ OMX Equity Trader Alert 2012-44,
//     https://www.nasdaqtrader.com/TraderNews.aspx?id=ETA2012-44, and BBC,
//     "Hurricane Sandy to close US markets for second day",
//     https://www.bbc.com/news/business-20120344 (reopened 2012-10-31).
//   - 2018-12-05: ICE press release, "New York Stock Exchange to Honor
//     President George H. W. Bush",
//     https://ir.theice.com/press/news-details/2018/New-York-Stock-Exchange-to-Honor-President-George-H-W-Bush/default.aspx
//   - 2025-01-09: ICE press release, "The New York Stock Exchange Will Close
//     Markets on January 9 to Honor the Passing of Former President Jimmy
//     Carter on National Day of Mourning",
//     https://ir.theice.com/press/news-details/2024/The-New-York-Stock-Exchange-Will-Close-Markets-on-January-9-to-Honor-the-Passing-of-Former-President-Jimmy-Carter-on-National-Day-of-Mourning/default.aspx
var oneOffClosureSources = []struct{ date, name string }{
	{"2001-09-11", "September 11 attacks"},
	{"2001-09-12", "September 11 attacks"},
	{"2001-09-13", "September 11 attacks"},
	{"2001-09-14", "September 11 attacks"},
	{"2004-06-11", "National Day of Mourning for President Ronald Reagan"},
	{"2007-01-02", "National Day of Mourning for President Gerald R. Ford"},
	{"2012-10-29", "Hurricane Sandy"},
	{"2012-10-30", "Hurricane Sandy"},
	{"2018-12-05", "National Day of Mourning for President George H. W. Bush"},
	{"2025-01-09", "National Day of Mourning for President Jimmy Carter"},
}

// TestOneOffClosures_ExactlyTheSourcedList keeps the shipped list and the
// sourced list in lockstep, so no closure ships without a citation.
func TestOneOffClosures_ExactlyTheSourcedList(t *testing.T) {
	var want []Holiday
	for _, oc := range oneOffClosureSources {
		want = append(want, Holiday{Date: mustDate(t, oc.date), Name: oc.name})
	}
	if !slices.Equal(oneOffClosures, want) {
		t.Errorf("oneOffClosures = %v\nwant %v", oneOffClosures, want)
	}
}

func TestXNYS_OneOffClosures(t *testing.T) {
	c := mustXNYS(t, XNYSOptions{})
	for _, oc := range oneOffClosureSources {
		t.Run(oc.date, func(t *testing.T) {
			d := mustDate(t, oc.date)
			if c.IsOpen(d) {
				t.Errorf("IsOpen(%v) = true, want false (%s)", d, oc.name)
			}
			want := Holiday{Date: d, Name: oc.name}
			if !slices.Contains(c.Holidays(d.Year), want) {
				t.Errorf("Holidays(%d) = %v, want it to contain %v", d.Year, c.Holidays(d.Year), want)
			}
		})
	}
}

func TestXNYS_OneOffClosures_NeighboursOpen(t *testing.T) {
	c := mustXNYS(t, XNYSOptions{})
	for _, s := range []string{
		"2001-09-10", "2001-09-17", // reopened Monday after 9/11
		"2004-06-10", "2004-06-14",
		"2007-01-03", // Jan 1 New Year's, Jan 2 Ford, Jan 3 open
		"2012-10-26", "2012-10-31",
		"2018-12-04", "2018-12-06",
		"2025-01-08", "2025-01-10",
	} {
		if d := mustDate(t, s); !c.IsOpen(d) {
			t.Errorf("IsOpen(%v) = false, want true", d)
		}
	}
	// 2007 has New Year's Day and the Ford closure back to back, in order.
	got := c.Holidays(2007)
	if len(got) < 2 || got[0].Date != (Date{2007, time.January, 1}) || got[1].Date != (Date{2007, time.January, 2}) {
		t.Errorf("Holidays(2007) starts %v, want 2007-01-01 then 2007-01-02", got[:min(2, len(got))])
	}
}
