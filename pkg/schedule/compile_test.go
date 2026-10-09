package schedule

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

var testAnchor = time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC) // a Monday

func TestCompile_Valid(t *testing.T) {
	tests := []struct {
		name string
		spec Spec
	}{
		{"cron", Spec{Cron: "0 3 * * *"}},
		{"cron with timezone and stagger", Spec{Cron: "0 3 * * *", Timezone: "America/New_York", Stagger: 2 * time.Hour}},
		{"cron ignores anchor", Spec{Cron: "*/5 * * * *", Anchor: testAnchor}},
		{"every days with at", Spec{Every: "4d", At: "03:00", Anchor: testAnchor}},
		{"every weeks without at", Spec{Every: "2w", Anchor: testAnchor}},
		{"every minutes", Spec{Every: "90m", Anchor: testAnchor}},
		{"every hours", Spec{Every: "6h", Anchor: testAnchor}},
		{"day filter", Spec{Every: "1d", At: "09:30", Anchor: testAnchor, DayFilters: []DayFilter{weekdaysOnly}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Compile(tt.spec)
			if err != nil {
				t.Fatalf("Compile(%+v) error: %v", tt.spec, err)
			}
			if s == nil {
				t.Fatal("Compile returned a nil schedule")
			}
		})
	}
}

func TestCompile_ErrorsNameTheField(t *testing.T) {
	tests := []struct {
		name  string
		spec  Spec
		field string
	}{
		{"neither cron nor every", Spec{Anchor: testAnchor}, "cron"},
		{"both cron and every", Spec{Cron: "0 3 * * *", Every: "1d", Anchor: testAnchor}, "every"},
		{"bad cron", Spec{Cron: "0 3 * *"}, "cron"},
		{"cron out of range", Spec{Cron: "0 24 * * *"}, "cron"},
		{"bad every", Spec{Every: "4days", Anchor: testAnchor}, "every"},
		{"zero every", Spec{Every: "0d", Anchor: testAnchor}, "every"},
		{"at with cron", Spec{Cron: "0 3 * * *", At: "03:00"}, "at"},
		{"at without every", Spec{At: "03:00"}, "at"},
		{"at with minutes", Spec{Every: "90m", At: "03:00", Anchor: testAnchor}, "at"},
		{"at with hours", Spec{Every: "6h", At: "03:00", Anchor: testAnchor}, "at"},
		{"bad at", Spec{Every: "1d", At: "3:00", Anchor: testAnchor}, "at"},
		{"every without anchor", Spec{Every: "1d", At: "03:00"}, "anchor"},
		{"negative stagger", Spec{Cron: "0 3 * * *", Stagger: -time.Minute}, "stagger"},
		{"nil day filter", Spec{Cron: "0 3 * * *", DayFilters: []DayFilter{weekdaysOnly, nil}}, "day_filters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Compile(tt.spec)
			if err == nil {
				t.Fatalf("Compile(%+v) = %v, want error naming %q", tt.spec, s, tt.field)
			}
			var fe *FieldError
			if !errors.As(err, &fe) {
				t.Fatalf("error %v (%T) is not a *FieldError", err, err)
			}
			if fe.Field != tt.field {
				t.Errorf("FieldError.Field = %q, want %q (error: %v)", fe.Field, tt.field, err)
			}
			if !strings.HasPrefix(err.Error(), tt.field+": ") {
				t.Errorf("error %q does not start with %q", err.Error(), tt.field+": ")
			}
		})
	}
}

func TestCompile_Timezone(t *testing.T) {
	tests := []struct {
		tz, want string
	}{
		{"", "UTC"},
		{"Not/AZone", "UTC"},
		{"America/New_York", "America/New_York"},
		{"Europe/London", "Europe/London"},
	}
	for _, tt := range tests {
		s, err := Compile(Spec{Cron: "0 3 * * *", Timezone: tt.tz})
		if err != nil {
			t.Fatalf("Compile(timezone %q) error: %v", tt.tz, err)
		}
		if got := s.Location().String(); got != tt.want {
			t.Errorf("timezone %q: Location() = %q, want %q", tt.tz, got, tt.want)
		}
	}
}

func TestSchedule_Offset(t *testing.T) {
	s, err := Compile(Spec{Cron: "0 3 * * *", Stagger: 2 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if s.Stagger() != 2*time.Hour {
		t.Errorf("Stagger() = %v, want 2h", s.Stagger())
	}
	want := StaggerOffset("dream", "hindsight", 2*time.Hour)
	if got := s.Offset("dream", "hindsight"); got != want || got == 0 {
		t.Errorf("Offset = %v, want StaggerOffset = %v (non-zero)", got, want)
	}
	unstaggered, err := Compile(Spec{Cron: "0 3 * * *"})
	if err != nil {
		t.Fatal(err)
	}
	if got := unstaggered.Offset("dream", "hindsight"); got != 0 {
		t.Errorf("Offset without stagger = %v, want 0", got)
	}
}

// weekdaysOnly allows Monday through Friday.
var weekdaysOnly = DayFilterFunc(func(_ context.Context, day time.Time) (bool, error) {
	wd := day.Weekday()
	return wd != time.Saturday && wd != time.Sunday, nil
})
