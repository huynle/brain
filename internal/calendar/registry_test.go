package calendar

import (
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/pkg/marketcal"
)

func mustDate(t *testing.T, s string) marketcal.Date {
	t.Helper()
	d, err := marketcal.ParseDate(s)
	if err != nil {
		t.Fatalf("ParseDate(%q): %v", s, err)
	}
	return d
}

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	r, err := NewRegistry(map[string]config.CalendarConfig{
		"xnys": {
			Type: "builtin", Market: "XNYS",
			ExtraClosed: []string{"2026-11-27"},
			ExtraOpen:   []string{"2026-12-25"},
		},
		"team": {Type: "ics", URLEnv: "TEAM_CAL_URL"},
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return r
}

func TestRegistryKinds(t *testing.T) {
	r := testRegistry(t)
	tests := []struct {
		name     string
		wantKind string
		wantOK   bool
	}{
		{name: "xnys", wantKind: KindBuiltin, wantOK: true},
		{name: "team", wantKind: KindICS, wantOK: true},
		{name: "missing", wantOK: false},
		{name: "", wantOK: false},
	}
	for _, tt := range tests {
		kind, ok := r.Kind(tt.name)
		if ok != tt.wantOK || kind != tt.wantKind {
			t.Errorf("Kind(%q) = (%q, %v), want (%q, %v)", tt.name, kind, ok, tt.wantKind, tt.wantOK)
		}
	}
}

func TestRegistryDayCalendarOnlyForBuiltin(t *testing.T) {
	r := testRegistry(t)
	if _, ok := r.DayCalendar("xnys"); !ok {
		t.Fatal("DayCalendar(xnys) ok = false, want true")
	}
	if _, ok := r.DayCalendar("team"); ok {
		t.Fatal("DayCalendar(team) ok = true, want false: an ics source is not a day calendar")
	}
	if _, ok := r.DayCalendar("missing"); ok {
		t.Fatal("DayCalendar(missing) ok = true, want false")
	}
}

func TestRegistryDayCalendarAppliesMarketRulesAndExtras(t *testing.T) {
	cal, ok := testRegistry(t).DayCalendar("xnys")
	if !ok {
		t.Fatal("DayCalendar(xnys) ok = false")
	}
	tests := []struct {
		date     string
		wantOpen bool
		why      string
	}{
		{"2026-11-25", true, "Wednesday before Thanksgiving"},
		{"2026-11-26", false, "Thanksgiving (built-in holiday)"},
		{"2026-11-27", false, "extra_closed"},
		{"2026-11-28", false, "Saturday"},
		{"2026-12-25", true, "Christmas is a holiday, but extra_open overrides it"},
		{"2026-12-28", true, "Monday"},
	}
	for _, tt := range tests {
		if got := cal.IsOpen(mustDate(t, tt.date)); got != tt.wantOpen {
			t.Errorf("IsOpen(%s) = %v, want %v (%s)", tt.date, got, tt.wantOpen, tt.why)
		}
	}
}

func TestRegistryNilKnowsNoCalendars(t *testing.T) {
	var r *Registry
	if kind, ok := r.Kind("xnys"); ok || kind != "" {
		t.Fatalf("nil Kind = (%q, %v), want empty", kind, ok)
	}
	if _, ok := r.DayCalendar("xnys"); ok {
		t.Fatal("nil DayCalendar ok = true, want false")
	}
}

func TestRegistryEmptyConfig(t *testing.T) {
	r, err := NewRegistry(nil)
	if err != nil {
		t.Fatalf("NewRegistry(nil): %v", err)
	}
	if _, ok := r.Kind("xnys"); ok {
		t.Fatal("empty registry knows xnys")
	}
}

func TestNewRegistryRejectsBadSources(t *testing.T) {
	tests := []struct {
		name      string
		calendars map[string]config.CalendarConfig
		wantErr   string
	}{
		{
			name:      "unknown type",
			calendars: map[string]config.CalendarConfig{"x": {Type: "google"}},
			wantErr:   "unknown type",
		},
		{
			name:      "builtin with unsupported market",
			calendars: map[string]config.CalendarConfig{"x": {Type: "builtin", Market: "XLON"}},
			wantErr:   "XNYS",
		},
		{
			name: "builtin with a malformed extra date",
			calendars: map[string]config.CalendarConfig{"x": {
				Type: "builtin", Market: "XNYS", ExtraClosed: []string{"11/27/2026"},
			}},
			wantErr: "extra_closed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewRegistry(tt.calendars)
			if err == nil {
				t.Fatal("NewRegistry = nil error, want one")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not name %q", err, tt.wantErr)
			}
		})
	}
}
