// Package marketcal provides pure (I/O-free) trading-day calendars for
// schedule day filters.
//
// XNYS models full-day closures of the US equity markets (NYSE and NASDAQ):
//
//   - weekends;
//   - New Year's Day, Martin Luther King, Jr. Day (third Monday of January),
//     Washington's Birthday (third Monday of February), Good Friday (two days
//     before Gregorian Easter), Memorial Day (last Monday of May), Juneteenth
//     (from 2022), Independence Day, Labor Day (first Monday of September),
//     Thanksgiving Day (fourth Thursday of November) and Christmas Day;
//   - a fixed-date holiday on a Saturday is observed the preceding Friday and
//     one on a Sunday the following Monday, except that New Year's Day on a
//     Saturday is not observed (the preceding December 31 stays open);
//   - known unscheduled closures since 2000 (September 11, 2001, national days
//     of mourning, Hurricane Sandy);
//   - caller-supplied extra closed and extra open dates, which override the
//     rules.
//
// The rules describe the modern NYSE schedule. They are tested against
// NYSE's published holiday tables for 2024-2028 (see xnys_test.go) and were
// cross-checked against an independent list of NYSE closures for 2000-2024;
// years before 2000 are not historically accurate. Early closes are out of
// scope: a half-day session counts as open.
package marketcal
