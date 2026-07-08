package cron

import (
	"testing"
	"time"
)

func TestLoadTimezone_EmptyDefaultsToUTC(t *testing.T) {
	loc := LoadTimezone("")
	if loc == nil {
		t.Fatal("LoadTimezone returned nil for empty tz; want UTC")
	}
	if loc.String() != "UTC" {
		t.Errorf("LoadTimezone(\"\") = %q, want UTC", loc.String())
	}
}

func TestLoadTimezone_ValidTimezone(t *testing.T) {
	loc := LoadTimezone("America/Denver")
	if loc == nil {
		t.Fatal("LoadTimezone returned nil for America/Denver")
	}
	if loc.String() != "America/Denver" {
		t.Errorf("LoadTimezone(\"America/Denver\") = %q, want America/Denver", loc.String())
	}

	// Sanity: convert a UTC time and confirm offset.
	// 2026-07-07 13:00 UTC should be 07:00 in Denver (MDT, -0600).
	utc := time.Date(2026, 7, 7, 13, 0, 0, 0, time.UTC)
	local := utc.In(loc)
	if local.Hour() != 7 {
		t.Errorf("13:00 UTC in Denver = %d:00, want 7:00", local.Hour())
	}
}

func TestLoadTimezone_InvalidDefaultsToUTC(t *testing.T) {
	loc := LoadTimezone("Not/A_Real_Zone_1234")
	if loc == nil {
		t.Fatal("LoadTimezone returned nil for invalid tz; want UTC fallback")
	}
	if loc.String() != "UTC" {
		t.Errorf("LoadTimezone(invalid) = %q, want UTC", loc.String())
	}
}
