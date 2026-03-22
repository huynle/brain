package cron

import (
	"testing"
	"time"
)

// =============================================================================
// Parse Tests
// =============================================================================

func TestParse_WildcardAll(t *testing.T) {
	s, err := Parse("* * * * *")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if s == nil {
		t.Fatal("Parse returned nil schedule")
	}

	// Should match any time
	times := []time.Time{
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 15, 12, 30, 0, 0, time.UTC),
		time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC),
	}
	for _, tt := range times {
		if !s.Matches(tt) {
			t.Errorf("* * * * * should match %v", tt)
		}
	}
}

func TestParse_EveryNMinutes(t *testing.T) {
	s, err := Parse("*/15 * * * *")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if s == nil {
		t.Fatal("Parse returned nil schedule")
	}

	// Should match minutes 0, 15, 30, 45
	matches := []int{0, 15, 30, 45}
	for _, m := range matches {
		tt := time.Date(2026, 1, 1, 10, m, 0, 0, time.UTC)
		if !s.Matches(tt) {
			t.Errorf("*/15 should match minute %d", m)
		}
	}

	// Should NOT match other minutes
	noMatch := []int{1, 5, 10, 14, 16, 29, 31, 44, 46, 59}
	for _, m := range noMatch {
		tt := time.Date(2026, 1, 1, 10, m, 0, 0, time.UTC)
		if s.Matches(tt) {
			t.Errorf("*/15 should NOT match minute %d", m)
		}
	}
}

func TestParse_SpecificValues(t *testing.T) {
	s, err := Parse("30 9 * * *")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if s == nil {
		t.Fatal("Parse returned nil schedule")
	}

	// Should match 9:30
	match := time.Date(2026, 3, 15, 9, 30, 0, 0, time.UTC)
	if !s.Matches(match) {
		t.Error("30 9 * * * should match 9:30")
	}

	// Should NOT match other times
	noMatch := []time.Time{
		time.Date(2026, 3, 15, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 15, 10, 30, 0, 0, time.UTC),
		time.Date(2026, 3, 15, 9, 31, 0, 0, time.UTC),
	}
	for _, tt := range noMatch {
		if s.Matches(tt) {
			t.Errorf("30 9 * * * should NOT match %v", tt)
		}
	}
}

func TestParse_Range(t *testing.T) {
	s, err := Parse("0 9-17 * * *")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if s == nil {
		t.Fatal("Parse returned nil schedule")
	}

	// Should match hours 9 through 17 at minute 0
	for h := 9; h <= 17; h++ {
		tt := time.Date(2026, 1, 1, h, 0, 0, 0, time.UTC)
		if !s.Matches(tt) {
			t.Errorf("0 9-17 should match hour %d", h)
		}
	}

	// Should NOT match hours outside range
	for _, h := range []int{0, 1, 8, 18, 23} {
		tt := time.Date(2026, 1, 1, h, 0, 0, 0, time.UTC)
		if s.Matches(tt) {
			t.Errorf("0 9-17 should NOT match hour %d", h)
		}
	}
}

func TestParse_List(t *testing.T) {
	// 1=Monday, 3=Wednesday, 5=Friday (cron: 0=Sunday, 1=Monday, ...)
	s, err := Parse("0 0 * * 1,3,5")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if s == nil {
		t.Fatal("Parse returned nil schedule")
	}

	// 2026-03-23 is Monday, 2026-03-25 is Wednesday, 2026-03-27 is Friday
	monday := time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC)
	wednesday := time.Date(2026, 3, 25, 0, 0, 0, 0, time.UTC)
	friday := time.Date(2026, 3, 27, 0, 0, 0, 0, time.UTC)

	if !s.Matches(monday) {
		t.Errorf("should match Monday (weekday=%d)", monday.Weekday())
	}
	if !s.Matches(wednesday) {
		t.Errorf("should match Wednesday (weekday=%d)", wednesday.Weekday())
	}
	if !s.Matches(friday) {
		t.Errorf("should match Friday (weekday=%d)", friday.Weekday())
	}

	// 2026-03-22 is Sunday, 2026-03-24 is Tuesday
	sunday := time.Date(2026, 3, 22, 0, 0, 0, 0, time.UTC)
	tuesday := time.Date(2026, 3, 24, 0, 0, 0, 0, time.UTC)

	if s.Matches(sunday) {
		t.Errorf("should NOT match Sunday (weekday=%d)", sunday.Weekday())
	}
	if s.Matches(tuesday) {
		t.Errorf("should NOT match Tuesday (weekday=%d)", tuesday.Weekday())
	}
}

func TestParse_StepWithRange(t *testing.T) {
	// Every 2 hours from 9 to 17: 9, 11, 13, 15, 17
	s, err := Parse("0 9-17/2 * * *")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if s == nil {
		t.Fatal("Parse returned nil schedule")
	}

	matches := []int{9, 11, 13, 15, 17}
	for _, h := range matches {
		tt := time.Date(2026, 1, 1, h, 0, 0, 0, time.UTC)
		if !s.Matches(tt) {
			t.Errorf("0 9-17/2 should match hour %d", h)
		}
	}

	noMatch := []int{10, 12, 14, 16, 8, 18}
	for _, h := range noMatch {
		tt := time.Date(2026, 1, 1, h, 0, 0, 0, time.UTC)
		if s.Matches(tt) {
			t.Errorf("0 9-17/2 should NOT match hour %d", h)
		}
	}
}

func TestParse_Complex(t *testing.T) {
	// Every 15 minutes during business hours on weekdays
	s, err := Parse("*/15 9-17 * * 1-5")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if s == nil {
		t.Fatal("Parse returned nil schedule")
	}

	// Monday 10:30 — should match
	mon := time.Date(2026, 3, 23, 10, 30, 0, 0, time.UTC)
	if !s.Matches(mon) {
		t.Error("should match Monday 10:30")
	}

	// Monday 10:31 — should NOT match (not on 15-min boundary)
	mon2 := time.Date(2026, 3, 23, 10, 31, 0, 0, time.UTC)
	if s.Matches(mon2) {
		t.Error("should NOT match Monday 10:31")
	}

	// Saturday 10:30 — should NOT match (weekend)
	sat := time.Date(2026, 3, 28, 10, 30, 0, 0, time.UTC)
	if s.Matches(sat) {
		t.Error("should NOT match Saturday 10:30")
	}

	// Monday 20:00 — should NOT match (outside business hours)
	monLate := time.Date(2026, 3, 23, 20, 0, 0, 0, time.UTC)
	if s.Matches(monLate) {
		t.Error("should NOT match Monday 20:00")
	}
}

func TestParse_StepFromValue(t *testing.T) {
	// 5/10 means starting at 5, every 10: 5, 15, 25, 35, 45, 55
	s, err := Parse("5/10 * * * *")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if s == nil {
		t.Fatal("Parse returned nil schedule")
	}

	matches := []int{5, 15, 25, 35, 45, 55}
	for _, m := range matches {
		tt := time.Date(2026, 1, 1, 10, m, 0, 0, time.UTC)
		if !s.Matches(tt) {
			t.Errorf("5/10 should match minute %d", m)
		}
	}

	noMatch := []int{0, 4, 6, 10, 14, 16, 20}
	for _, m := range noMatch {
		tt := time.Date(2026, 1, 1, 10, m, 0, 0, time.UTC)
		if s.Matches(tt) {
			t.Errorf("5/10 should NOT match minute %d", m)
		}
	}
}

func TestParse_Invalid(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{"empty", ""},
		{"too few fields", "* * *"},
		{"too many fields", "* * * * * *"},
		{"invalid minute", "60 * * * *"},
		{"invalid hour", "* 25 * * *"},
		{"invalid day of month", "* * 32 * *"},
		{"invalid month", "* * * 13 *"},
		{"invalid day of week", "* * * * 8"},
		{"invalid range", "* 17-9 * * *"},
		{"non-numeric", "abc * * * *"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.expr)
			if err == nil {
				t.Errorf("Parse(%q) should return error", tt.expr)
			}
		})
	}
}

// =============================================================================
// NextAfter Tests
// =============================================================================

func TestNextAfter_EveryMinute(t *testing.T) {
	s, err := Parse("* * * * *")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	now := time.Date(2026, 1, 1, 10, 30, 15, 0, time.UTC)
	next := s.NextAfter(now)

	expected := time.Date(2026, 1, 1, 10, 31, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("NextAfter = %v, want %v", next, expected)
	}
}

func TestNextAfter_Every15Minutes(t *testing.T) {
	s, err := Parse("*/15 * * * *")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	now := time.Date(2026, 1, 1, 10, 16, 0, 0, time.UTC)
	next := s.NextAfter(now)

	expected := time.Date(2026, 1, 1, 10, 30, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("NextAfter = %v, want %v", next, expected)
	}
}

func TestNextAfter_Wraparound_Minute(t *testing.T) {
	s, err := Parse("0 * * * *")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	// At 10:30, next :00 is 11:00
	now := time.Date(2026, 1, 1, 10, 30, 0, 0, time.UTC)
	next := s.NextAfter(now)

	expected := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("NextAfter = %v, want %v", next, expected)
	}
}

func TestNextAfter_Wraparound_Hour(t *testing.T) {
	s, err := Parse("30 9 * * *")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	// At 10:00, next 9:30 is tomorrow
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	next := s.NextAfter(now)

	expected := time.Date(2026, 1, 2, 9, 30, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("NextAfter = %v, want %v", next, expected)
	}
}

func TestNextAfter_Wraparound_DayOfWeek(t *testing.T) {
	// Only on Mondays (1)
	s, err := Parse("0 0 * * 1")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	// 2026-03-22 is Sunday, next Monday is 2026-03-23
	now := time.Date(2026, 3, 22, 12, 0, 0, 0, time.UTC)
	next := s.NextAfter(now)

	expected := time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("NextAfter = %v, want %v", next, expected)
	}
}

func TestNextAfter_Wraparound_Month(t *testing.T) {
	// 1st of month at midnight
	s, err := Parse("0 0 1 * *")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	// On Jan 15, next 1st is Feb 1
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	next := s.NextAfter(now)

	expected := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("NextAfter = %v, want %v", next, expected)
	}
}

func TestNextAfter_ExactMatch_AdvancesToNext(t *testing.T) {
	s, err := Parse("*/15 * * * *")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	// Exactly at a match time — should advance to next
	now := time.Date(2026, 1, 1, 10, 15, 0, 0, time.UTC)
	next := s.NextAfter(now)

	expected := time.Date(2026, 1, 1, 10, 30, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("NextAfter = %v, want %v", next, expected)
	}
}

func TestParse_DayOfWeek_Sunday_Both0And7(t *testing.T) {
	// Both 0 and 7 should represent Sunday
	s0, err := Parse("0 0 * * 0")
	if err != nil {
		t.Fatalf("Parse(0) error: %v", err)
	}

	sunday := time.Date(2026, 3, 22, 0, 0, 0, 0, time.UTC) // Sunday
	if !s0.Matches(sunday) {
		t.Error("day-of-week 0 should match Sunday")
	}

	// 7 should also work for Sunday
	s7, err := Parse("0 0 * * 7")
	if err != nil {
		t.Fatalf("Parse(7) error: %v", err)
	}
	if !s7.Matches(sunday) {
		t.Error("day-of-week 7 should match Sunday")
	}
}
