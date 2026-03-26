package service

import (
	"strings"
	"testing"
	"time"
)

// =============================================================================
// ParseCronExpression Tests
// =============================================================================

func TestParseCronExpression_EveryMinute(t *testing.T) {
	sched, err := ParseCronExpression("* * * * *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sched.Minute.Any {
		t.Error("expected Minute.Any to be true")
	}
	if !sched.Hour.Any {
		t.Error("expected Hour.Any to be true")
	}
	if !sched.DayOfMonth.Any {
		t.Error("expected DayOfMonth.Any to be true")
	}
	if !sched.Month.Any {
		t.Error("expected Month.Any to be true")
	}
	if !sched.DayOfWeek.Any {
		t.Error("expected DayOfWeek.Any to be true")
	}
}

func TestParseCronExpression_Every15Minutes(t *testing.T) {
	sched, err := ParseCronExpression("*/15 * * * *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// */15 = 0, 15, 30, 45
	expected := []int{0, 15, 30, 45}
	if len(sched.Minute.Values) != len(expected) {
		t.Fatalf("expected %d minute values, got %d: %v", len(expected), len(sched.Minute.Values), sched.Minute.Values)
	}
	for i, v := range expected {
		if sched.Minute.Values[i] != v {
			t.Errorf("minute[%d]: expected %d, got %d", i, v, sched.Minute.Values[i])
		}
	}
	if sched.Minute.Any {
		t.Error("expected Minute.Any to be false for */15")
	}
}

func TestParseCronExpression_SpecificValues(t *testing.T) {
	sched, err := ParseCronExpression("0 9 * * 1-5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// minute = 0
	if len(sched.Minute.Values) != 1 || sched.Minute.Values[0] != 0 {
		t.Errorf("expected minute [0], got %v", sched.Minute.Values)
	}
	// hour = 9
	if len(sched.Hour.Values) != 1 || sched.Hour.Values[0] != 9 {
		t.Errorf("expected hour [9], got %v", sched.Hour.Values)
	}
	// dayOfWeek = 1,2,3,4,5
	expectedDow := []int{1, 2, 3, 4, 5}
	if len(sched.DayOfWeek.Values) != len(expectedDow) {
		t.Fatalf("expected %d dow values, got %d: %v", len(expectedDow), len(sched.DayOfWeek.Values), sched.DayOfWeek.Values)
	}
	for i, v := range expectedDow {
		if sched.DayOfWeek.Values[i] != v {
			t.Errorf("dow[%d]: expected %d, got %d", i, v, sched.DayOfWeek.Values[i])
		}
	}
}

func TestParseCronExpression_CommaValues(t *testing.T) {
	sched, err := ParseCronExpression("0,30 * * * *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []int{0, 30}
	if len(sched.Minute.Values) != len(expected) {
		t.Fatalf("expected %d values, got %d: %v", len(expected), len(sched.Minute.Values), sched.Minute.Values)
	}
	for i, v := range expected {
		if sched.Minute.Values[i] != v {
			t.Errorf("minute[%d]: expected %d, got %d", i, v, sched.Minute.Values[i])
		}
	}
}

func TestParseCronExpression_RangeWithStep(t *testing.T) {
	sched, err := ParseCronExpression("1-10/3 * * * *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 1-10/3 = 1, 4, 7, 10
	expected := []int{1, 4, 7, 10}
	if len(sched.Minute.Values) != len(expected) {
		t.Fatalf("expected %d values, got %d: %v", len(expected), len(sched.Minute.Values), sched.Minute.Values)
	}
	for i, v := range expected {
		if sched.Minute.Values[i] != v {
			t.Errorf("minute[%d]: expected %d, got %d", i, v, sched.Minute.Values[i])
		}
	}
}

func TestParseCronExpression_InvalidFieldCount(t *testing.T) {
	_, err := ParseCronExpression("* * *")
	if err == nil {
		t.Fatal("expected error for 3 fields")
	}
	if !strings.Contains(err.Error(), "expected 5 fields") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestParseCronExpression_InvalidValue(t *testing.T) {
	_, err := ParseCronExpression("60 * * * *")
	if err == nil {
		t.Fatal("expected error for minute=60")
	}
}

func TestParseCronExpression_EmptyString(t *testing.T) {
	_, err := ParseCronExpression("")
	if err == nil {
		t.Fatal("expected error for empty string")
	}
}

func TestParseCronExpression_NumberWithStep(t *testing.T) {
	sched, err := ParseCronExpression("5/15 * * * *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 5/15 = starting at 5, every 15: 5, 20, 35, 50
	expected := []int{5, 20, 35, 50}
	if len(sched.Minute.Values) != len(expected) {
		t.Fatalf("expected %d values, got %d: %v", len(expected), len(sched.Minute.Values), sched.Minute.Values)
	}
	for i, v := range expected {
		if sched.Minute.Values[i] != v {
			t.Errorf("minute[%d]: expected %d, got %d", i, v, sched.Minute.Values[i])
		}
	}
}

func TestParseCronExpression_AllFieldsSpecific(t *testing.T) {
	// At 2:30 on day 15 of January, only on Mondays
	sched, err := ParseCronExpression("30 2 15 1 1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sched.Minute.Values) != 1 || sched.Minute.Values[0] != 30 {
		t.Errorf("expected minute [30], got %v", sched.Minute.Values)
	}
	if len(sched.Hour.Values) != 1 || sched.Hour.Values[0] != 2 {
		t.Errorf("expected hour [2], got %v", sched.Hour.Values)
	}
	if len(sched.DayOfMonth.Values) != 1 || sched.DayOfMonth.Values[0] != 15 {
		t.Errorf("expected dom [15], got %v", sched.DayOfMonth.Values)
	}
	if len(sched.Month.Values) != 1 || sched.Month.Values[0] != 1 {
		t.Errorf("expected month [1], got %v", sched.Month.Values)
	}
	if len(sched.DayOfWeek.Values) != 1 || sched.DayOfWeek.Values[0] != 1 {
		t.Errorf("expected dow [1], got %v", sched.DayOfWeek.Values)
	}
}

// =============================================================================
// GetNextRun Tests
// =============================================================================

func TestGetNextRun_EveryMinute(t *testing.T) {
	sched, err := ParseCronExpression("* * * * *")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	after := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	next := GetNextRun(sched, after)
	expected := time.Date(2025, 1, 1, 12, 1, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, next)
	}
}

func TestGetNextRun_Every15Minutes(t *testing.T) {
	sched, err := ParseCronExpression("*/15 * * * *")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	after := time.Date(2025, 1, 1, 12, 3, 0, 0, time.UTC)
	next := GetNextRun(sched, after)
	expected := time.Date(2025, 1, 1, 12, 15, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, next)
	}
}

func TestGetNextRun_SpecificHour(t *testing.T) {
	sched, err := ParseCronExpression("0 9 * * *")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	// After 10:00, next should be 9:00 next day
	after := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	next := GetNextRun(sched, after)
	expected := time.Date(2025, 1, 2, 9, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, next)
	}
}

func TestGetNextRun_WeekdayOnly(t *testing.T) {
	sched, err := ParseCronExpression("0 9 * * 1-5")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	// 2025-01-04 is Saturday. Next weekday is Monday 2025-01-06
	after := time.Date(2025, 1, 4, 10, 0, 0, 0, time.UTC)
	next := GetNextRun(sched, after)
	expected := time.Date(2025, 1, 6, 9, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, next)
	}
}

func TestGetNextRun_SkipsToNextMonth(t *testing.T) {
	sched, err := ParseCronExpression("0 0 1 * *")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	// After Jan 15, next should be Feb 1
	after := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	next := GetNextRun(sched, after)
	expected := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, next)
	}
}

func TestGetNextRun_AtExactBoundary(t *testing.T) {
	sched, err := ParseCronExpression("*/15 * * * *")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	// If we're at exactly 12:15, next should be 12:30 (not 12:15)
	after := time.Date(2025, 1, 1, 12, 15, 0, 0, time.UTC)
	next := GetNextRun(sched, after)
	expected := time.Date(2025, 1, 1, 12, 30, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, next)
	}
}

// =============================================================================
// GenerateRunID Tests
// =============================================================================

func TestGenerateRunID_Format(t *testing.T) {
	triggerTime := time.Date(2025, 3, 15, 14, 30, 0, 0, time.UTC)
	id := GenerateRunID(triggerTime)

	// Should start with "20250315-1430-"
	prefix := "20250315-1430-"
	if !strings.HasPrefix(id, prefix) {
		t.Errorf("expected prefix %q, got %q", prefix, id)
	}

	// Total length: 8 (date) + 1 (-) + 4 (time) + 1 (-) + 6 (random) = 20
	if len(id) != 20 {
		t.Errorf("expected length 20, got %d: %q", len(id), id)
	}
}

func TestGenerateRunID_Unique(t *testing.T) {
	triggerTime := time.Date(2025, 3, 15, 14, 30, 0, 0, time.UTC)
	id1 := GenerateRunID(triggerTime)
	id2 := GenerateRunID(triggerTime)
	if id1 == id2 {
		t.Error("expected unique IDs, got identical")
	}
}

func TestGenerateRunID_UsesUTC(t *testing.T) {
	// Use a non-UTC timezone
	loc := time.FixedZone("EST", -5*60*60)
	triggerTime := time.Date(2025, 3, 15, 9, 30, 0, 0, loc) // 9:30 EST = 14:30 UTC
	id := GenerateRunID(triggerTime)

	prefix := "20250315-1430-"
	if !strings.HasPrefix(id, prefix) {
		t.Errorf("expected UTC prefix %q, got %q", prefix, id)
	}
}

// =============================================================================
// matchesSchedule edge cases
// =============================================================================

func TestMatchesSchedule_BothDayFieldsRestricted(t *testing.T) {
	// "0 0 15 * 1" = midnight on the 15th OR on Mondays (OR logic)
	sched, err := ParseCronExpression("0 0 15 * 1")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// 2025-01-15 is Wednesday — matches dayOfMonth=15
	t1 := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	if !matchesSchedule(sched, t1) {
		t.Error("expected match on 15th (Wednesday)")
	}

	// 2025-01-13 is Monday — matches dayOfWeek=1
	t2 := time.Date(2025, 1, 13, 0, 0, 0, 0, time.UTC)
	if !matchesSchedule(sched, t2) {
		t.Error("expected match on Monday (13th)")
	}

	// 2025-01-14 is Tuesday, not 15th — should NOT match
	t3 := time.Date(2025, 1, 14, 0, 0, 0, 0, time.UTC)
	if matchesSchedule(sched, t3) {
		t.Error("expected no match on Tuesday 14th")
	}
}
